package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abbayosua/skynet/internal/agent/commandcode"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/agent/hyper"
	"github.com/abbayosua/skynet/internal/agent/notify"
	promptpkg "github.com/abbayosua/skynet/internal/agent/prompt"
	"github.com/abbayosua/skynet/internal/agent/tools"
	"github.com/abbayosua/skynet/internal/config"
	"github.com/abbayosua/skynet/internal/event"
	"github.com/abbayosua/skynet/internal/filetracker"
	"github.com/abbayosua/skynet/internal/history"
	"github.com/abbayosua/skynet/internal/home"
	"github.com/abbayosua/skynet/internal/hooks"
	"github.com/abbayosua/skynet/internal/log"
	"github.com/abbayosua/skynet/internal/lsp"
	"github.com/abbayosua/skynet/internal/message"
	"github.com/abbayosua/skynet/internal/oauth/copilot"
	"github.com/abbayosua/skynet/internal/permission"
	"github.com/abbayosua/skynet/internal/pubsub"
	"github.com/abbayosua/skynet/internal/session"
	"github.com/abbayosua/skynet/internal/skills"
	"golang.org/x/sync/errgroup"

	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/azure"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	openaisdk "github.com/charmbracelet/openai-go/option"
	"github.com/qjebbs/go-jsons"
)

// Coordinator errors.
var (
	errCoderAgentNotConfigured         = errors.New("coder agent not configured")
	errModelProviderNotConfigured      = errors.New("model provider not configured")
	errLargeModelNotSelected           = errors.New("large model not selected")
	errSmallModelNotSelected           = errors.New("small model not selected")
	errLargeModelProviderNotConfigured = errors.New("large model provider not configured")
	errSmallModelProviderNotConfigured = errors.New("small model provider not configured")
	errLargeModelNotFound              = errors.New("large model not found in provider config")
	errSmallModelNotFound              = errors.New("small model not found in provider config")
)

// Copilot models that use the Responses API instead of Chat Completions.
var copilotResponsesModels = map[string]bool{
	"gpt-5.2":       true,
	"gpt-5.2-codex": true,
	"gpt-5.3-codex": true,
	"gpt-5.4-mini":  true,
	"gpt-5-mini":    true,
}

type Coordinator interface {
	// INFO: (kujtim) this is not used yet we will use this when we have multiple agents
	// SetMainAgent(string)
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
	Cancel(sessionID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string) error
	Model() Model
	UpdateModels(ctx context.Context) error
	RunAutoPilot(ctx context.Context, output io.Writer, mainSessionID string) error
}

type coordinator struct {
	cfg         *config.ConfigStore
	sessions    session.Service
	messages    message.Service
	permissions permission.Service
	history     history.Service
	filetracker filetracker.Service
	lspManager  *lsp.Manager
	notify      pubsub.Publisher[notify.Notification]

	currentAgent SessionAgent
	agents       map[string]SessionAgent

	// Skills discovery results (session-start snapshot).
	allSkills    []*skills.Skill // Pre-filter: all discovered after dedup.
	activeSkills []*skills.Skill // Post-filter: active skills only.
	skillTracker *skills.Tracker

	readyWg errgroup.Group
}

func NewCoordinator(
	ctx context.Context,
	cfg *config.ConfigStore,
	sessions session.Service,
	messages message.Service,
	permissions permission.Service,
	history history.Service,
	filetracker filetracker.Service,
	lspManager *lsp.Manager,
	notify pubsub.Publisher[notify.Notification],
) (Coordinator, error) {
	// Discover skills once at session start.
	allSkills, activeSkills := discoverSkills(cfg)
	skillTracker := skills.NewTracker(activeSkills)

	c := &coordinator{
		cfg:          cfg,
		sessions:     sessions,
		messages:     messages,
		permissions:  permissions,
		history:      history,
		filetracker:  filetracker,
		lspManager:   lspManager,
		notify:       notify,
		agents:       make(map[string]SessionAgent),
		allSkills:    allSkills,
		activeSkills: activeSkills,
		skillTracker: skillTracker,
	}

	agentCfg, ok := cfg.Config().Agents[config.AgentCoder]
	if !ok {
		return nil, errCoderAgentNotConfigured
	}

	// TODO: make this dynamic when we support multiple agents
	prompt, err := coderPrompt(promptpkg.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, prompt, agentCfg, false)
	if err != nil {
		return nil, err
	}
	c.currentAgent = agent
	c.agents[config.AgentCoder] = agent
	return c, nil
}

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	if err := c.readyWg.Wait(); err != nil {
		return nil, err
	}

	// refresh models before each run
	if err := c.UpdateModels(ctx); err != nil {
		return nil, fmt.Errorf("failed to update models: %w", err)
	}

	model := c.currentAgent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	if !model.CatwalkCfg.SupportsImages && attachments != nil {
		// filter out image attachments
		filteredAttachments := make([]message.Attachment, 0, len(attachments))
		for _, att := range attachments {
			if att.IsText() {
				filteredAttachments = append(filteredAttachments, att)
			}
		}
		attachments = filteredAttachments
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return nil, errModelProviderNotConfigured
	}

	mergedOptions, temp, topP, topK, freqPenalty, presPenalty := mergeCallOptions(model, providerCfg, sessionID)

	if err := c.refreshTokenIfExpired(ctx, providerCfg); err != nil {
		// NOTE(@andreynering): We don't return here because the event handling to ask the user to reauthenticate
		// depends on the flow below. If refresh fails, proceed with the token we have.
		slog.Error("Failed to refresh OAuth2 token. Proceeding with existing token.", "error", err)
	}

	// Detect /ralph-loop trigger prefix. When set, enables Ralph Loop for
	// this run regardless of config. The prefix is stripped from the prompt.
	// ALSO check the current config for Ralph Loop enabled state (not cached).
	enableRalphLoop := false
	cleanPrompt := prompt
	cfg := c.cfg.Config()
	if cfg != nil && cfg.Options != nil && cfg.Options.RalphLoop != nil && cfg.Options.RalphLoop.Enabled {
		enableRalphLoop = true
	}

	// Rebuild system prompt with current config so Task Planner toggle
	// (and other dynamic options) take effect immediately.
	if cfg != nil && c.currentAgent != nil {
		// Keep the answer_short flag in sync with the config file so
		// toggles from the UI (command palette) apply without restart.
		c.currentAgent.SetAnswerShort(cfg.Options != nil && cfg.Options.AnswerShort)
		if cfg.Options != nil {
			c.currentAgent.SetAnswerShortPrompt(cfg.Options.AnswerShortPrompt)
		}
		if p, err := coderPrompt(promptpkg.WithWorkingDir(c.cfg.WorkingDir())); err == nil {
			if systemPrompt, err := p.Build(ctx, model.Model.Provider(), model.Model.Model(), c.cfg); err == nil {
				c.currentAgent.SetSystemPrompt(systemPrompt)
			}
		}
	}

	if strings.HasPrefix(strings.TrimSpace(prompt), "/ralph-loop") {
		enableRalphLoop = true
		cleanPrompt = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prompt), "/ralph-loop"))
	}

	// Handle /answer-short command: toggle the brief-answer directive.
	if strings.HasPrefix(strings.TrimSpace(prompt), "/answer-short") {
		return c.handleAnswerShortCommand(ctx, prompt)
	}

	run := func() (*fantasy.AgentResult, error) {
		return c.currentAgent.Run(ctx, SessionAgentCall{
			SessionID:        sessionID,
			Prompt:           cleanPrompt,
			Attachments:      attachments,
			MaxOutputTokens:  maxTokens,
			ProviderOptions:  mergedOptions,
			Temperature:      temp,
			TopP:             topP,
			TopK:             topK,
			FrequencyPenalty: freqPenalty,
			PresencePenalty:  presPenalty,
			EnableRalphLoop:  enableRalphLoop,
		})
	}
	beforeLoaded := c.skillTracker.LoadedNames()
	result, originalErr := run()
	logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)

	if c.isUnauthorized(originalErr) {
		if err := c.retryAfterUnauthorized(ctx, providerCfg); err == nil {
			result, originalErr = run()
			logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)
			if originalErr == nil {
				return result, nil
			}
		}
	}

	// Auto-retry transient provider errors (including "Endpoint is
	// unavailable" / "Model is unavailable") up to 30 times with
	// exponential back-off. Fantasy already retries 5xx/429 internally
	// (2 attempts), this outer loop extends it to 30.
	const maxAutoRetries = 30
	for attempt := 1; attempt <= maxAutoRetries; attempt++ {
		if !shouldAutoRetry(originalErr) {
			break
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		delay := autoRetryDelay(originalErr, attempt)
		slog.Warn("Auto retrying provider error",
			"attempt", attempt,
			"max_retries", maxAutoRetries,
			"delay", delay,
			"error", originalErr,
		)
		// Publish activity so TUI spinner shows retry (otherwise looks stuck).
		if c.notify != nil {
			c.notify.Publish(pubsub.UpdatedEvent, notify.Notification{
				Type:     notify.TypeActivityUpdate,
				Activity: fmt.Sprintf("Provider error, retrying %d/%d in %s...", attempt, maxAutoRetries, delay),
			})
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return result, ctx.Err()
		}
		// Clean up last failed turn (user + error assistant) before retry
		// to avoid history pollution and duplicate prompts that require
		// manual retries. See agent.go deduplication as fallback.
		c.cleanupFailedTurn(ctx, sessionID, cleanPrompt)
		result, originalErr = run()
		logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)
		if originalErr == nil {
			return result, nil
		}
		if c.isUnauthorized(originalErr) {
			if err := c.retryAfterUnauthorized(ctx, providerCfg); err == nil {
				result, originalErr = run()
				logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)
				if originalErr == nil {
					return result, nil
				}
			}
		}
	}

	return result, originalErr
}

// shouldAutoRetry reports whether err is a transient provider error that
// should be retried automatically. It covers IsRetryable (5xx/429/408/409,
// net errors) plus opencode-specific "Endpoint is unavailable" /
// "Model is unavailable" which arrive as 400/404 but are transient.
func shouldAutoRetry(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var providerErr *fantasy.ProviderError
	if errors.As(err, &providerErr) {
		if providerErr.IsRetryable() {
			return true
		}
		msgLower := strings.ToLower(providerErr.Message)
		if strings.Contains(msgLower, "endpoint is unavailable") ||
			strings.Contains(msgLower, "model is unavailable") {
			return true
		}
		if isTransientNetworkMessage(msgLower) {
			return true
		}
	}
	var retryErr *fantasy.RetryError
	if errors.As(err, &retryErr) && len(retryErr.Errors) > 0 {
		return shouldAutoRetry(retryErr.Errors[len(retryErr.Errors)-1])
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	errLower := strings.ToLower(err.Error())
	if strings.Contains(errLower, "endpoint is unavailable") ||
		strings.Contains(errLower, "model is unavailable") {
		return true
	}
	if isTransientNetworkMessage(errLower) {
		return true
	}
	return false
}

func isTransientNetworkMessage(msgLower string) bool {
	transientSubstrings := []string{
		"no such host",
		"dial tcp",
		"connection refused",
		"connection reset",
		"broken pipe",
		"network is unreachable",
		"no route to host",
		"i/o timeout",
		"timeout",
		"client timeout",
		"tls handshake timeout",
		"unexpected eof",
		"eof",
		"lookup",
		"no internet",
		"temporary failure",
		"name resolution",
		"connection timed out",
		"net/http: request canceled",
		"proxyconnect tcp",
		"connection was forcibly closed",
		"connection abort",
		"stream error",
		"transport error",
		"remote error",
		"write: broken pipe",
		"read: connection reset",
	}
	for _, s := range transientSubstrings {
		if strings.Contains(msgLower, s) {
			return true
		}
	}
	return false
}

func (c *coordinator) cleanupFailedTurn(ctx context.Context, sessionID, prompt string) {
	msgs, err := c.messages.List(ctx, sessionID)
	if err != nil || len(msgs) < 2 {
		return
	}
	last := msgs[len(msgs)-1]
	secondLast := msgs[len(msgs)-2]
	if last.Role != message.Assistant || secondLast.Role != message.User {
		return
	}
	if strings.TrimSpace(secondLast.Content().Text) != strings.TrimSpace(prompt) {
		return
	}
	fr := last.FinishReason()
	isError := fr == message.FinishReasonError || fr == message.FinishReasonCanceled || fr == message.FinishReasonUnknown
	if !isError && !strings.Contains(strings.ToLower(last.Content().Text), "provider error") && !strings.Contains(strings.ToLower(last.Content().Text), "error") {
		return
	}
	// Best effort cleanup, ignore errors (e.g. already deleted).
	_ = c.messages.Delete(ctx, last.ID)
	_ = c.messages.Delete(ctx, secondLast.ID)
	slog.Debug("Cleaned up failed turn before retry", "session_id", sessionID)
}

func autoRetryDelay(err error, attempt int) time.Duration {
	// Respect Retry-After headers when present.
	var providerErr *fantasy.ProviderError
	if errors.As(err, &providerErr) && providerErr.ResponseHeaders != nil {
		if v, ok := lookupHeader(providerErr.ResponseHeaders, "retry-after-ms"); ok {
			if ms, err := parseFloatMs(v); err == nil && ms > 0 && ms < 60000 {
				return time.Duration(ms) * time.Millisecond
			}
		}
		if v, ok := lookupHeader(providerErr.ResponseHeaders, "retry-after"); ok {
			if d, err := parseRetryAfterHeader(v); err == nil && d > 0 && d < 60*time.Second {
				return d
			}
		}
	}
	delay := time.Duration(float64(2*time.Second) * math.Pow(1.5, float64(attempt-1)))
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

func lookupHeader(headers map[string]string, key string) (string, bool) {
	for k, v := range headers {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

func parseFloatMs(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}

func parseRetryAfterSeconds(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%f", &f)
	return f, err
}

func parseRetryAfterHeader(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if f, err := parseFloatMs(s); err == nil {
		return time.Duration(f * float64(time.Second)), nil
	}
	if t, err := time.Parse(time.RFC1123, s); err == nil {
		return time.Until(t), nil
	}
	return 0, fmt.Errorf("unparseable retry-after: %s", s)
}

// handleAnswerShortCommand toggles the answer_short directive via the
// /answer-short slash command. Returns an immediate text response without
// calling the LLM.
func (c *coordinator) handleAnswerShortCommand(ctx context.Context, prompt string) (*fantasy.AgentResult, error) {
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prompt), "/answer-short"))
	arg = strings.ToLower(strings.TrimSpace(arg))

	current := c.currentAgent.AnswerShort()
	var enabled bool
	switch arg {
	case "on", "enable", "true", "1":
		enabled = true
	case "off", "disable", "false", "0":
		enabled = false
	default:
		enabled = !current
	}

	c.currentAgent.SetAnswerShort(enabled)
	if err := c.cfg.SetAnswerShort(config.ScopeWorkspace, enabled); err != nil {
		slog.Warn("Failed to persist answer_short setting", "error", err)
	}

	status := "OFF"
	if enabled {
		status = "ON"
	}
	msg := fmt.Sprintf("Answer short mode is now **%s** — prompts will be appended with the brief-answer directive.", status)
	if enabled {
		msg += "\n\nSetiap prompt akan ditambahkan: *Jawab singkat, jangan perlu banyak mikir*"
	} else {
		msg += "\n\nMode normal tanpa direktif jawab singkat."
	}
	return &fantasy.AgentResult{
		Response: fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.TextContent{Text: msg},
			},
		},
	}, nil
}

func getProviderOptions(model Model, providerCfg config.ProviderConfig, sessionID string) fantasy.ProviderOptions {
	options := fantasy.ProviderOptions{}

	cfgOpts := []byte("{}")
	providerCfgOpts := []byte("{}")
	catwalkOpts := []byte("{}")

	if model.ModelCfg.ProviderOptions != nil {
		data, err := json.Marshal(model.ModelCfg.ProviderOptions)
		if err == nil {
			cfgOpts = data
		}
	}

	if providerCfg.ProviderOptions != nil {
		data, err := json.Marshal(providerCfg.ProviderOptions)
		if err == nil {
			providerCfgOpts = data
		}
	}

	if model.CatwalkCfg.Options.ProviderOptions != nil {
		data, err := json.Marshal(model.CatwalkCfg.Options.ProviderOptions)
		if err == nil {
			catwalkOpts = data
		}
	}

	readers := []io.Reader{
		bytes.NewReader(catwalkOpts),
		bytes.NewReader(providerCfgOpts),
		bytes.NewReader(cfgOpts),
	}

	got, err := jsons.Merge(readers)
	if err != nil {
		slog.Error("Could not merge call config", "err", err)
		return options
	}

	mergedOptions := make(map[string]any)

	err = json.Unmarshal([]byte(got), &mergedOptions)
	if err != nil {
		slog.Error("Could not create config for call", "err", err)
		return options
	}

	switch providerCfg.Type {
	case openai.Name, azure.Name:
		_, hasReasoningEffort := mergedOptions["reasoning_effort"]
		if !hasReasoningEffort && model.ModelCfg.ReasoningEffort != "" && model.CatwalkCfg.CanReason {
			mergedOptions["reasoning_effort"] = model.ModelCfg.ReasoningEffort
		}
		if openai.IsResponsesModel(model.CatwalkCfg.ID) {
			if openai.IsResponsesReasoningModel(model.CatwalkCfg.ID) {
				mergedOptions["reasoning_summary"] = "auto"
				mergedOptions["include"] = []openai.IncludeType{openai.IncludeReasoningEncryptedContent}
			}
			parsed, err := openai.ParseResponsesOptions(mergedOptions)
			if err == nil {
				options[openai.Name] = parsed
			}
		} else {
			parsed, err := openai.ParseOptions(mergedOptions)
			if err == nil {
				options[openai.Name] = parsed
			}
		}
	case anthropic.Name, bedrock.Name:
		var (
			_, hasEffort = mergedOptions["effort"]
			_, hasThink  = mergedOptions["thinking"]
		)
		switch {
		case !hasEffort && model.ModelCfg.ReasoningEffort != "" && model.CatwalkCfg.CanReason:
			mergedOptions["effort"] = model.ModelCfg.ReasoningEffort
		case !hasThink && model.ModelCfg.Think:
			mergedOptions["thinking"] = map[string]any{"budget_tokens": 2000}
		}
		parsed, err := anthropic.ParseOptions(mergedOptions)
		if err == nil {
			options[anthropic.Name] = parsed
		}

	case openrouter.Name:
		_, hasReasoning := mergedOptions["reasoning"]
		if !hasReasoning && model.ModelCfg.ReasoningEffort != "" {
			mergedOptions["reasoning"] = map[string]any{
				"enabled": true,
				"effort":  model.ModelCfg.ReasoningEffort,
			}
		}
		parsed, err := openrouter.ParseOptions(mergedOptions)
		if err == nil {
			options[openrouter.Name] = parsed
		}
	case vercel.Name:
		_, hasReasoning := mergedOptions["reasoning"]
		if !hasReasoning && model.ModelCfg.ReasoningEffort != "" {
			mergedOptions["reasoning"] = map[string]any{
				"enabled": true,
				"effort":  model.ModelCfg.ReasoningEffort,
			}
		}
		parsed, err := vercel.ParseOptions(mergedOptions)
		if err == nil {
			options[vercel.Name] = parsed
		}
	case google.Name:
		_, hasReasoning := mergedOptions["thinking_config"]
		if !hasReasoning {
			if strings.HasPrefix(model.CatwalkCfg.ID, "gemini-2") {
				mergedOptions["thinking_config"] = map[string]any{
					"thinking_budget":  2000,
					"include_thoughts": true,
				}
			} else {
				mergedOptions["thinking_config"] = map[string]any{
					"thinking_level":   model.ModelCfg.ReasoningEffort,
					"include_thoughts": true,
				}
			}
		}
		parsed, err := google.ParseOptions(mergedOptions)
		if err == nil {
			options[google.Name] = parsed
		}
	case openaicompat.Name, hyper.Name:
		extraBody := make(map[string]any)

		_, hasReasoningEffort := mergedOptions["reasoning_effort"]
		if !hasReasoningEffort && model.ModelCfg.ReasoningEffort != "" && model.CatwalkCfg.CanReason {
			switch providerCfg.ID {
			case string(catwalk.InferenceProviderIoNet):
				extraBody["reasoning"] = map[string]string{"effort": model.ModelCfg.ReasoningEffort}
			default:
				mergedOptions["reasoning_effort"] = model.ModelCfg.ReasoningEffort
			}
		}

		// "reasoning effort" is a standard OpenAI field, but "thinking" is not.
		// Setting it in the right way for each provider.
		// TODO: Abstract this in Fantasy somehow?
		// TODO: Allow custom providers to specify how to set this?
		switch providerCfg.ID {
		case hyper.Name:
			extraBody["thinking"] = model.ModelCfg.Think
		case string(catwalk.InferenceProviderIoNet):
			if _, ok := extraBody["reasoning"]; !ok && model.CatwalkCfg.CanReason {
				if model.ModelCfg.Think {
					extraBody["reasoning"] = map[string]string{"effort": "medium"}
				} else {
					extraBody["reasoning"] = map[string]string{"effort": "none"}
				}
			}
		case string(catwalk.InferenceProviderZAI), string(catwalk.InferenceProviderDeepSeek):
			if model.ModelCfg.Think || model.ModelCfg.ReasoningEffort != "" {
				extraBody["thinking"] = map[string]any{
					"type": "enabled",
				}
			} else {
				extraBody["thinking"] = map[string]any{
					"type": "disabled",
				}
			}
		}

		// Opencode gateway: group prompt cache per session, mirroring what
		// the opencode CLI itself sends (prompt_cache_key = session ID).
		if sessionID != "" && strings.HasPrefix(providerCfg.ID, "opencode") {
			extraBody["prompt_cache_key"] = sessionID
		}

		mergedOptions["extra_body"] = extraBody

		parsed, err := openaicompat.ParseOptions(mergedOptions)
		if err == nil {
			options[openaicompat.Name] = parsed
		}
	}

	return options
}

func mergeCallOptions(model Model, cfg config.ProviderConfig, sessionID string) (fantasy.ProviderOptions, *float64, *float64, *int64, *float64, *float64) {
	modelOptions := getProviderOptions(model, cfg, sessionID)
	temp := cmp.Or(model.ModelCfg.Temperature, model.CatwalkCfg.Options.Temperature)
	topP := cmp.Or(model.ModelCfg.TopP, model.CatwalkCfg.Options.TopP)
	topK := cmp.Or(model.ModelCfg.TopK, model.CatwalkCfg.Options.TopK)
	freqPenalty := cmp.Or(model.ModelCfg.FrequencyPenalty, model.CatwalkCfg.Options.FrequencyPenalty)
	presPenalty := cmp.Or(model.ModelCfg.PresencePenalty, model.CatwalkCfg.Options.PresencePenalty)
	return modelOptions, temp, topP, topK, freqPenalty, presPenalty
}

func (c *coordinator) buildAgent(ctx context.Context, prompt *promptpkg.Prompt, agent config.Agent, isSubAgent bool) (SessionAgent, error) {
	large, small, err := c.buildAgentModels(ctx, isSubAgent)
	if err != nil {
		return nil, err
	}

	largeProviderCfg, _ := c.cfg.Config().Providers.Get(large.ModelCfg.Provider)
	result := NewSessionAgent(SessionAgentOptions{
		LargeModel:           large,
		SmallModel:           small,
		SystemPromptPrefix:   largeProviderCfg.SystemPromptPrefix,
		SystemPrompt:         "",
		IsSubAgent:           isSubAgent,
		DisableAutoSummarize: c.cfg.Config().Options.DisableAutoSummarize,
		IsYolo:               c.permissions.SkipRequests(),
		Sessions:             c.sessions,
		Messages:             c.messages,
		Tools:                nil,
		Notify:               c.notify,
		RalphLoop:            c.cfg.Config().Options.RalphLoop,
		AnswerShort:          c.cfg.Config().Options.AnswerShort,
		AnswerShortPrompt:    c.cfg.Config().Options.AnswerShortPrompt,
	})

	c.readyWg.Go(func() error {
		systemPrompt, err := prompt.Build(ctx, large.Model.Provider(), large.Model.Model(), c.cfg)
		if err != nil {
			return err
		}
		result.SetSystemPrompt(systemPrompt)
		return nil
	})

	c.readyWg.Go(func() error {
		tools, err := c.buildTools(ctx, agent, isSubAgent)
		if err != nil {
			return err
		}
		result.SetTools(tools)
		return nil
	})

	return result, nil
}

func (c *coordinator) buildTools(ctx context.Context, agent config.Agent, isSubAgent bool) ([]fantasy.AgentTool, error) {
	var allTools []fantasy.AgentTool
	if slices.Contains(agent.AllowedTools, AgentToolName) {
		agentTool, err := c.agentTool(ctx)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agentTool)
	}

	if slices.Contains(agent.AllowedTools, tools.AgenticFetchToolName) {
		agenticFetchTool, err := c.agenticFetchTool(ctx, nil)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, agenticFetchTool)
	}

	// Get the model name for the agent
	modelID := ""
	if modelCfg, ok := c.cfg.Config().Models[agent.Model]; ok {
		if model := c.cfg.Config().GetModel(modelCfg.Provider, modelCfg.Model); model != nil {
			modelID = model.ID
		}
	}

	logFile := filepath.Join(c.cfg.Config().Options.DataDirectory, "logs", "skynet.log")

	// Build hook runners for PreToolUse and PostToolUse.
	var hookRunner *hooks.Runner
	if preToolHooks := c.cfg.Config().Hooks[hooks.EventPreToolUse]; len(preToolHooks) > 0 {
		hookRunner = hooks.NewRunner(preToolHooks, c.cfg.WorkingDir(), c.cfg.WorkingDir())
	}
	var postHookRunner *hooks.Runner
	if postToolHooks := c.cfg.Config().Hooks[hooks.EventPostToolUse]; len(postToolHooks) > 0 {
		postHookRunner = hooks.NewRunner(postToolHooks, c.cfg.WorkingDir(), c.cfg.WorkingDir())
	}

	// Initialize background agent manager once and reuse across builds.
	// This ensures spawned agents survive tool rebuilds (e.g. MCP updates).
	if backgroundAgentManager == nil {
		backgroundAgentManager = tools.NewBackgroundAgentManager(
			c.runBackgroundTask,
			tools.WithMaxConcurrentAgents(maxConcurrentAgents),
			tools.WithDefaultAgentTimeout(10*time.Minute),
			tools.WithOnComplete(func(agentID, sessionID, summary string) {
				// Enqueue a prompt to the main agent so it processes
				// the background task result on its next turn.
				c.currentAgent.EnqueuePrompt(sessionID,
					fmt.Sprintf("A background task has completed.\n\n"+
						"Agent: `%s`\nSummary: %s\n\n"+
						"Review the result and take appropriate action.",
						agentID, summary))
			}),
		)
	}

	// Initialize team orchestrator once and reuse across builds.
	if teamOrchestrator == nil {
		teamOrchestrator = tools.NewTeamOrchestrator(c.runBackgroundTask, maxConcurrentAgents)
	}

	allTools = append(allTools,
		tools.NewBashTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Options.Attribution, modelID),
		tools.NewCrushInfoTool(c.cfg, c.lspManager, c.allSkills, c.activeSkills, c.skillTracker),
		tools.NewCrushLogsTool(logFile),
		tools.NewJobOutputTool(),
		tools.NewJobKillTool(),
		tools.NewDownloadTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewHashlineEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewMultiEditTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
		tools.NewFetchTool(c.permissions, c.cfg.WorkingDir(), nil),
		tools.NewGlobTool(c.cfg.WorkingDir()),
		tools.NewGrepTool(c.cfg.WorkingDir(), c.cfg.Config().Tools.Grep),
		tools.NewLsTool(c.permissions, c.cfg.WorkingDir(), c.cfg.Config().Tools.Ls),
		tools.NewASTGrepSearchTool(c.cfg.WorkingDir()),
		tools.NewASTGrepReplaceTool(c.cfg.WorkingDir(), c.permissions),
		tools.NewPlannerTool(c.cfg.WorkingDir()),
		tools.NewSleepTool(),
		tools.NewTeamTool(c.cfg.WorkingDir(), teamOrchestrator),
		tools.NewSpawnAgentTool(c.cfg.WorkingDir(), backgroundAgentManager),
		tools.NewAgentStatusTool(),
		tools.NewCollectAgentTool(),
		tools.NewSourcegraphTool(nil),
		tools.NewTodosTool(c.sessions),
		tools.NewViewTool(c.lspManager, c.permissions, c.filetracker, c.skillTracker, c.cfg.WorkingDir(), c.cfg.Config().Options.SkillsPaths...),
		tools.NewWriteTool(c.lspManager, c.permissions, c.history, c.filetracker, c.cfg.WorkingDir()),
	)

	// Add LSP tools if user has configured LSPs or auto_lsp is enabled (nil or true).
	if len(c.cfg.Config().LSP) > 0 || c.cfg.Config().Options.AutoLSP == nil || *c.cfg.Config().Options.AutoLSP {
		allTools = append(allTools, tools.NewDiagnosticsTool(c.lspManager), tools.NewReferencesTool(c.lspManager), tools.NewLSPRestartTool(c.lspManager))
	}

	if len(c.cfg.Config().MCP) > 0 {
		allTools = append(
			allTools,
			tools.NewListMCPResourcesTool(c.cfg, c.permissions),
			tools.NewReadMCPResourceTool(c.cfg, c.permissions),
		)
	}

	var filteredTools []fantasy.AgentTool
	for _, tool := range allTools {
		if slices.Contains(agent.AllowedTools, tool.Info().Name) {
			filteredTools = append(filteredTools, tool)
		}
	}

	for _, tool := range tools.GetMCPTools(c.permissions, c.cfg, c.cfg.WorkingDir()) {
		if agent.AllowedMCP == nil {
			// No MCP restrictions
			filteredTools = append(filteredTools, tool)
			continue
		}
		if len(agent.AllowedMCP) == 0 {
			// No MCPs allowed
			slog.Debug("No MCPs allowed", "tool", tool.Name(), "agent", agent.Name)
			break
		}

		for mcp, tools := range agent.AllowedMCP {
			if mcp != tool.MCP() {
				continue
			}
			if len(tools) == 0 || slices.Contains(tools, tool.MCPToolName()) {
				filteredTools = append(filteredTools, tool)
				break
			}
			slog.Debug("MCP not allowed", "tool", tool.Name(), "agent", agent.Name)
		}
	}
	slices.SortFunc(filteredTools, func(a, b fantasy.AgentTool) int {
		return strings.Compare(a.Info().Name, b.Info().Name)
	})

	// Wrap tools with hook interception for the top-level agent only.
	// Sub-agents (the `agent` task tool, `agentic_fetch`, etc.) run
	// without hook interception to avoid firing the user's hook N times
	// per delegated turn. The top-level invocation of the sub-agent tool
	// itself is still wrapped from the coder's side.
	filteredTools = wrapToolsWithHooks(filteredTools, hookRunner, postHookRunner, isSubAgent)

	return filteredTools, nil
}

// TODO: when we support multiple agents we need to change this so that we pass in the agent specific model config
func (c *coordinator) buildAgentModels(ctx context.Context, isSubAgent bool) (Model, Model, error) {
	largeModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeLarge]
	if !ok {
		return Model{}, Model{}, errLargeModelNotSelected
	}
	smallModelCfg, ok := c.cfg.Config().Models[config.SelectedModelTypeSmall]
	if !ok {
		return Model{}, Model{}, errSmallModelNotSelected
	}

	largeProviderCfg, ok := c.cfg.Config().Providers.Get(largeModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errLargeModelProviderNotConfigured
	}

	largeProvider, err := c.buildProvider(largeProviderCfg, largeModelCfg, isSubAgent)
	if err != nil {
		return Model{}, Model{}, err
	}

	smallProviderCfg, ok := c.cfg.Config().Providers.Get(smallModelCfg.Provider)
	if !ok {
		return Model{}, Model{}, errSmallModelProviderNotConfigured
	}

	smallProvider, err := c.buildProvider(smallProviderCfg, smallModelCfg, true)
	if err != nil {
		return Model{}, Model{}, err
	}

	var largeCatwalkModel *catwalk.Model
	var smallCatwalkModel *catwalk.Model

	for _, m := range largeProviderCfg.Models {
		if m.ID == largeModelCfg.Model {
			largeCatwalkModel = &m
		}
	}
	for _, m := range smallProviderCfg.Models {
		if m.ID == smallModelCfg.Model {
			smallCatwalkModel = &m
		}
	}

	if largeCatwalkModel == nil {
		return Model{}, Model{}, errLargeModelNotFound
	}

	if smallCatwalkModel == nil {
		return Model{}, Model{}, errSmallModelNotFound
	}

	largeModelID := largeModelCfg.Model
	smallModelID := smallModelCfg.Model

	if largeModelCfg.Provider == openrouter.Name && isExactoSupported(largeModelID) {
		largeModelID += ":exacto"
	}

	if smallModelCfg.Provider == openrouter.Name && isExactoSupported(smallModelID) {
		smallModelID += ":exacto"
	}

	largeModel, err := largeProvider.LanguageModel(ctx, largeModelID)
	if err != nil {
		return Model{}, Model{}, err
	}
	smallModel, err := smallProvider.LanguageModel(ctx, smallModelID)
	if err != nil {
		return Model{}, Model{}, err
	}

	return Model{
			Model:      largeModel,
			CatwalkCfg: *largeCatwalkModel,
			ModelCfg:   largeModelCfg,
			FlatRate:   largeProviderCfg.FlatRate,
		}, Model{
			Model:      smallModel,
			CatwalkCfg: *smallCatwalkModel,
			ModelCfg:   smallModelCfg,
			FlatRate:   smallProviderCfg.FlatRate,
		}, nil
}

func (c *coordinator) buildAnthropicProvider(baseURL, apiKey string, headers map[string]string, providerID string) (fantasy.Provider, error) {
	var opts []anthropic.Option

	switch {
	case strings.HasPrefix(apiKey, "Bearer "):
		// NOTE: Prevent the SDK from picking up the API key from env.
		os.Setenv("ANTHROPIC_API_KEY", "")
		headers["Authorization"] = apiKey
	case providerID == string(catwalk.InferenceProviderMiniMax) || providerID == string(catwalk.InferenceProviderMiniMaxChina):
		// NOTE: Prevent the SDK from picking up the API key from env.
		os.Setenv("ANTHROPIC_API_KEY", "")
		headers["Authorization"] = "Bearer " + apiKey
	case apiKey != "":
		// X-Api-Key header
		opts = append(opts, anthropic.WithAPIKey(apiKey))
	}

	if len(headers) > 0 {
		opts = append(opts, anthropic.WithHeaders(headers))
	}

	if baseURL != "" {
		opts = append(opts, anthropic.WithBaseURL(baseURL))
	}

	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, anthropic.WithHTTPClient(httpClient))
	}
	return anthropic.New(opts...)
}

func (c *coordinator) buildOpenaiProvider(baseURL, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithUseResponsesAPI(),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, openai.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, openai.WithHeaders(headers))
	}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	return openai.New(opts...)
}

func (c *coordinator) buildOpenrouterProvider(_, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []openrouter.Option{
		openrouter.WithAPIKey(apiKey),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, openrouter.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, openrouter.WithHeaders(headers))
	}
	return openrouter.New(opts...)
}

func (c *coordinator) buildVercelProvider(_, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []vercel.Option{
		vercel.WithAPIKey(apiKey),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, vercel.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, vercel.WithHeaders(headers))
	}
	return vercel.New(opts...)
}

func (c *coordinator) buildOpenaiCompatProvider(baseURL, apiKey string, headers map[string]string, extraBody map[string]any, providerID string, isSubAgent bool) (fantasy.Provider, error) {
	opts := []openaicompat.Option{
		openaicompat.WithBaseURL(baseURL),
		openaicompat.WithAPIKey(apiKey),
	}

	// Set HTTP client based on provider and debug mode.
	var httpClient *http.Client
	isCommandCode := strings.Contains(baseURL, "commandcode.ai") || providerID == "commandcode"
	_ = os.WriteFile("/tmp/cc_build.log", []byte(fmt.Sprintf("buildOpenaiCompat providerID=%s baseURL=%s isCC=%v\n", providerID, baseURL, isCommandCode)), 0644)
	if isCommandCode {
		base := http.DefaultTransport
		if c.cfg.Config().Options.Debug {
			if dbg := log.NewHTTPClient(); dbg.Transport != nil {
				base = dbg.Transport
			}
		}
		httpClient = &http.Client{Transport: &commandcode.Transport{Base: base, WorkingDir: c.cfg.WorkingDir()}}
	} else if providerID == string(catwalk.InferenceProviderCopilot) {
		opts = append(opts,
			openaicompat.WithUseResponsesAPI(),
			openaicompat.WithResponsesAPIFunc(func(modelID string) bool {
				return copilotResponsesModels[modelID]
			}),
		)
		httpClient = copilot.NewClient(isSubAgent, c.cfg.Config().Options.Debug)
	} else if strings.HasPrefix(providerID, "opencode") {
		// Opencode gateway serves some model families (e.g. muse) only via
		// the Responses API; chat completions returns 500 for them while
		// /responses works. Route per-model like the opencode CLI does.
		opts = append(opts,
			openaicompat.WithUseResponsesAPI(),
			openaicompat.WithResponsesAPIFunc(opencodeNeedsResponsesAPI),
		)
	} else if c.cfg.Config().Options.Debug {
		httpClient = log.NewHTTPClient()
	}
	if httpClient != nil {
		opts = append(opts, openaicompat.WithHTTPClient(httpClient))
	}

	if len(headers) > 0 {
		opts = append(opts, openaicompat.WithHeaders(headers))
	}

	for extraKey, extraValue := range extraBody {
		opts = append(opts, openaicompat.WithSDKOptions(openaisdk.WithJSONSet(extraKey, extraValue)))
	}

	return openaicompat.New(opts...)
}

// opencodeNeedsResponsesAPI reports whether the opencode gateway serves the
// given model only via the Responses API. Per the official endpoint table:
// muse family, grok-4.6, and gpt-5.6-luna live on /responses; everything
// else is chat completions (or /messages, which also answers chat here).
func opencodeNeedsResponsesAPI(modelID string) bool {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "muse"):
		return true
	case strings.HasPrefix(id, "grok-4."), strings.HasPrefix(id, "gpt-5.6"):
		return true
	}
	return false
}

func (c *coordinator) buildAzureProvider(baseURL, apiKey string, headers map[string]string, options map[string]string) (fantasy.Provider, error) {
	opts := []azure.Option{
		azure.WithBaseURL(baseURL),
		azure.WithAPIKey(apiKey),
		azure.WithUseResponsesAPI(),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, azure.WithHTTPClient(httpClient))
	}
	if options == nil {
		options = make(map[string]string)
	}
	if apiVersion, ok := options["apiVersion"]; ok {
		opts = append(opts, azure.WithAPIVersion(apiVersion))
	}
	if len(headers) > 0 {
		opts = append(opts, azure.WithHeaders(headers))
	}

	return azure.New(opts...)
}

func (c *coordinator) buildBedrockProvider(apiKey string, headers map[string]string) (fantasy.Provider, error) {
	var opts []bedrock.Option
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, bedrock.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, bedrock.WithHeaders(headers))
	}
	switch {
	case apiKey != "":
		opts = append(opts, bedrock.WithAPIKey(apiKey))
	case os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "":
		opts = append(opts, bedrock.WithAPIKey(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")))
	default:
		// Skip, let the SDK do authentication.
	}
	return bedrock.New(opts...)
}

func (c *coordinator) buildGoogleProvider(baseURL, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []google.Option{
		google.WithBaseURL(baseURL),
		google.WithGeminiAPIKey(apiKey),
	}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, google.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, google.WithHeaders(headers))
	}
	return google.New(opts...)
}

func (c *coordinator) buildGoogleVertexProvider(headers map[string]string, options map[string]string) (fantasy.Provider, error) {
	opts := []google.Option{}
	if c.cfg.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, google.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, google.WithHeaders(headers))
	}

	project := options["project"]
	location := options["location"]

	opts = append(opts, google.WithVertex(project, location))

	return google.New(opts...)
}

func (c *coordinator) isAnthropicThinking(model config.SelectedModel) bool {
	if model.Think {
		return true
	}
	opts, err := anthropic.ParseOptions(model.ProviderOptions)
	return err == nil && opts.Thinking != nil
}

// openCodeSessionID returns a session identifier for opencode.ai providers.
// It mirrors the format used by the opencode CLI (ses_ followed by 26
// base62 characters). Requests to opencode.ai are routed to the upstream
// serving the selected model based on this session id; without it some
// models (e.g. deepseek-v4-flash on opencode-go) are reported as
// unavailable.
func openCodeSessionID() string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 26)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return "ses_" + string(b)
}

// providerHeaders returns the HTTP headers to send with requests to the
// given provider, merging user-configured extra headers with provider
// specific defaults.
func providerHeaders(providerCfg config.ProviderConfig, anthropicThinking bool) map[string]string {
	headers := maps.Clone(providerCfg.ExtraHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}

	// handle special headers for anthropic
	if providerCfg.Type == anthropic.Name && anthropicThinking {
		if v, ok := headers["anthropic-beta"]; ok {
			headers["anthropic-beta"] = v + ",interleaved-thinking-2025-05-14"
		} else {
			headers["anthropic-beta"] = "interleaved-thinking-2025-05-14"
		}
	}

	// opencode.ai routes requests to the upstream serving the selected
	// model based on the x-opencode-session header, so generate a fresh
	// session id per conversation the same way the opencode CLI does.
	switch providerCfg.ID {
	case string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
		headers["x-opencode-session"] = openCodeSessionID()
	}

	return headers
}

func (c *coordinator) buildProvider(providerCfg config.ProviderConfig, model config.SelectedModel, isSubAgent bool) (fantasy.Provider, error) {
	headers := providerHeaders(providerCfg, c.isAnthropicThinking(model))

	apiKey, _ := c.cfg.Resolve(providerCfg.APIKey)
	baseURL, _ := c.cfg.Resolve(providerCfg.BaseURL)

	switch providerCfg.Type {
	case openai.Name:
		return c.buildOpenaiProvider(baseURL, apiKey, headers)
	case anthropic.Name:
		return c.buildAnthropicProvider(baseURL, apiKey, headers, providerCfg.ID)
	case openrouter.Name:
		return c.buildOpenrouterProvider(baseURL, apiKey, headers)
	case vercel.Name:
		return c.buildVercelProvider(baseURL, apiKey, headers)
	case azure.Name:
		return c.buildAzureProvider(baseURL, apiKey, headers, providerCfg.ExtraParams)
	case bedrock.Name:
		return c.buildBedrockProvider(apiKey, headers)
	case google.Name:
		return c.buildGoogleProvider(baseURL, apiKey, headers)
	case "google-vertex":
		return c.buildGoogleVertexProvider(headers, providerCfg.ExtraParams)
	case openaicompat.Name, hyper.Name:
		switch providerCfg.ID {
		case hyper.Name:
			baseURL = hyper.BaseURL() + "/v1"
			headers["x-skynet-id"] = event.GetID()
		case string(catwalk.InferenceProviderZAI):
			if providerCfg.ExtraBody == nil {
				providerCfg.ExtraBody = map[string]any{}
			}
			providerCfg.ExtraBody["tool_stream"] = true
		}
		return c.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent)
	default:
		return nil, fmt.Errorf("provider type not supported: %q", providerCfg.Type)
	}
}

func isExactoSupported(modelID string) bool {
	supportedModels := []string{
		"moonshotai/kimi-k2-0905",
		"deepseek/deepseek-v3.1-terminus",
		"z-ai/glm-4.6",
		"openai/gpt-oss-120b",
		"qwen/qwen3-coder",
	}
	return slices.Contains(supportedModels, modelID)
}

func (c *coordinator) Cancel(sessionID string) {
	c.currentAgent.Cancel(sessionID)
}

func (c *coordinator) CancelAll() {
	c.currentAgent.CancelAll()
}

func (c *coordinator) ClearQueue(sessionID string) {
	c.currentAgent.ClearQueue(sessionID)
}

func (c *coordinator) IsBusy() bool {
	return c.currentAgent.IsBusy()
}

func (c *coordinator) IsSessionBusy(sessionID string) bool {
	return c.currentAgent.IsSessionBusy(sessionID)
}

func (c *coordinator) Model() Model {
	return c.currentAgent.Model()
}

func (c *coordinator) UpdateModels(ctx context.Context) error {
	// build the models again so we make sure we get the latest config
	large, small, err := c.buildAgentModels(ctx, false)
	if err != nil {
		return err
	}
	c.currentAgent.SetModels(large, small)

	agentCfg, ok := c.cfg.Config().Agents[config.AgentCoder]
	if !ok {
		return errCoderAgentNotConfigured
	}

	tools, err := c.buildTools(ctx, agentCfg, false)
	if err != nil {
		return err
	}
	c.currentAgent.SetTools(tools)
	return nil
}

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent.QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent.QueuedPromptsList(sessionID)
}

func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
	providerCfg, ok := c.cfg.Config().Providers.Get(c.currentAgent.Model().ModelCfg.Provider)
	if !ok {
		return errModelProviderNotConfigured
	}

	if err := c.refreshTokenIfExpired(ctx, providerCfg); err != nil {
		slog.Error("Failed to refresh OAuth2 token before summarize. Proceeding with existing token.", "error", err)
	}

	summarize := func() error {
		return c.currentAgent.Summarize(ctx, sessionID, getProviderOptions(c.currentAgent.Model(), providerCfg, sessionID))
	}

	err := summarize()
	if err != nil && c.isUnauthorized(err) {
		if retryErr := c.retryAfterUnauthorized(ctx, providerCfg); retryErr == nil {
			return summarize()
		}
	}

	return err
}

// refreshTokenIfExpired proactively refreshes the OAuth token if it has expired.
func (c *coordinator) refreshTokenIfExpired(ctx context.Context, providerCfg config.ProviderConfig) error {
	if providerCfg.OAuthToken == nil || !providerCfg.OAuthToken.IsExpired() {
		return nil
	}
	slog.Debug("Token needs to be refreshed", "provider", providerCfg.ID)
	return c.refreshOAuth2Token(ctx, providerCfg)
}

// retryAfterUnauthorized attempts to refresh credentials after receiving a 401
// and returns nil if retry should be attempted.
func (c *coordinator) retryAfterUnauthorized(ctx context.Context, providerCfg config.ProviderConfig) error {
	switch {
	case providerCfg.OAuthToken != nil:
		slog.Debug("Received 401. Refreshing token and retrying", "provider", providerCfg.ID)
		return c.refreshOAuth2Token(ctx, providerCfg)
	case strings.Contains(providerCfg.APIKeyTemplate, "$"):
		slog.Debug("Received 401. Refreshing API Key template and retrying", "provider", providerCfg.ID)
		return c.refreshApiKeyTemplate(ctx, providerCfg)
	default:
		return nil
	}
}

func (c *coordinator) isUnauthorized(err error) bool {
	var providerErr *fantasy.ProviderError
	return errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized
}

func (c *coordinator) refreshOAuth2Token(ctx context.Context, providerCfg config.ProviderConfig) error {
	if err := c.cfg.RefreshOAuthToken(ctx, config.ScopeGlobal, providerCfg.ID); err != nil {
		slog.Error("Failed to refresh OAuth token after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}
	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}

func (c *coordinator) refreshApiKeyTemplate(ctx context.Context, providerCfg config.ProviderConfig) error {
	newAPIKey, err := c.cfg.Resolve(providerCfg.APIKeyTemplate)
	if err != nil {
		slog.Error("Failed to re-resolve API key after 401 error", "provider", providerCfg.ID, "error", err)
		return err
	}

	providerCfg.APIKey = newAPIKey
	c.cfg.Config().Providers.Set(providerCfg.ID, providerCfg)

	if err := c.UpdateModels(ctx); err != nil {
		return err
	}
	return nil
}

// subAgentParams holds the parameters for running a sub-agent.
type subAgentParams struct {
	Agent          SessionAgent
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Prompt         string
	SessionTitle   string
	// SessionSetup is an optional callback invoked after session creation
	// but before agent execution, for custom session configuration.
	SessionSetup func(sessionID string)
}

// runSubAgent runs a sub-agent and handles session management and cost accumulation.
// It creates a sub-session, runs the agent with the given prompt, and propagates
// the cost to the parent session.
func (c *coordinator) runSubAgent(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, error) {
	// Create sub-session
	agentToolSessionID := c.sessions.CreateAgentToolSessionID(params.AgentMessageID, params.ToolCallID)
	session, err := c.sessions.CreateTaskSession(ctx, agentToolSessionID, params.SessionID, params.SessionTitle)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("create session: %w", err)
	}

	// Call session setup function if provided
	if params.SessionSetup != nil {
		params.SessionSetup(session.ID)
	}

	// Get model configuration
	model := params.Agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return fantasy.ToolResponse{}, errModelProviderNotConfigured
	}

	// Run the agent
	result, err := params.Agent.Run(ctx, SessionAgentCall{
		SessionID:        session.ID,
		Prompt:           params.Prompt,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg, session.ID),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
	})
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to generate response: %s", err)), nil
	}

	// Update parent session cost
	if err := c.updateParentSessionCost(ctx, session.ID, params.SessionID); err != nil {
		return fantasy.ToolResponse{}, err
	}

	return fantasy.NewTextResponse(result.Response.Content.Text()), nil
}

// runBackgroundTask runs an agent task in isolation, without a parent session.
// Used by the BackgroundAgentManager for async background agent execution.
// It creates a standalone session, builds a task agent, executes the prompt,
// and returns the text result. The caller is responsible for timeout/cancellation
// via the supplied context.
func (c *coordinator) runBackgroundTask(ctx context.Context, userPrompt string) (string, error) {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return "", errors.New("task agent not configured")
	}

	p, err := taskPrompt(promptpkg.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return "", err
	}

	agent, err := c.buildAgent(ctx, p, agentCfg, true)
	if err != nil {
		return "", err
	}

	// Create a standalone session for this background task.
	sess, err := c.sessions.Create(ctx, "Background Task")
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	// Auto-approve permissions for background tasks.
	c.permissions.AutoApproveSession(sess.ID)

	model := agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return "", errors.New("model provider not configured")
	}

	result, err := agent.Run(ctx, SessionAgentCall{
		SessionID:        sess.ID,
		Prompt:           userPrompt,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg, sess.ID),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
	})
	if err != nil {
		return "", err
	}

	return result.Response.Content.Text(), nil
}

// RunAutoPilot starts the autonomous coding mode.
func (c *coordinator) RunAutoPilot(ctx context.Context, output io.Writer, mainSessionID string) error {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentCoder]
	if !ok {
		return errors.New("autopilot: coder agent not configured")
	}

	p, err := autopilotPrompt(promptpkg.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return fmt.Errorf("autopilot: build prompt: %w", err)
	}

	agent, err := c.buildAgent(ctx, p, agentCfg, true)
	if err != nil {
		return fmt.Errorf("autopilot: build agent: %w", err)
	}

	sess, err := c.sessions.Create(ctx, "AutoPilot")
	if err != nil {
		return fmt.Errorf("autopilot: create session: %w", err)
	}
	c.permissions.AutoApproveSession(sess.ID)

	model := agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return errors.New("autopilot: model provider not configured")
	}

	// Read main session context for initial analysis.
	var mainCtx string
	if mainSessionID != "" {
		msgs, err := c.messages.List(ctx, mainSessionID)
		if err == nil && len(msgs) > 0 {
			var parts []string
			for _, msg := range msgs {
				content := msg.Content().Text
				if content == "" {
					continue
				}
				parts = append(parts, fmt.Sprintf("[%s] %s", string(msg.Role), content))
				if len(parts) >= 10 {
					break
				}
			}
			mainCtx = strings.Join(parts, "\n")
		}
	}

	// Setup file logging.
	logDir := filepath.Join(c.cfg.WorkingDir(), ".skynet", "autopilot", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Warn("autopilot: cannot create log dir", "error", err)
	}
	logPath := filepath.Join(logDir, sess.ID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Warn("autopilot: cannot open log file", "error", err)
	}
	if logFile != nil {
		defer logFile.Close()
	}

	logEvent := func(evType, detail string) {
		if logFile == nil {
			return
		}
		line := fmt.Sprintf(`{"time":"%s","session_id":"%s","type":"%s","detail":"%s"}`+"\n",
			time.Now().Format(time.RFC3339), sess.ID, evType, strings.ReplaceAll(detail, `"`, `\"`))
		logFile.WriteString(line)
	}

	writeOutput := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		fmt.Fprintln(output, line)
		logEvent("output", line)
	}

	opts := SessionAgentCall{
		SessionID:        sess.ID,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg, sess.ID),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
	}

	// Message streaming: subscribe to real-time message updates.
	msgCh := c.messages.Subscribe(ctx)
	seenParts := make(map[string]int)      // messageID -> parts printed count
	seenText := make(map[string]int)       // messageID -> text bytes printed
	seenThinking := make(map[string]int)   // messageID -> thinking bytes printed
	seenToolInput := make(map[string]bool) // toolCallID -> input already printed

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-msgCh:
				if !ok {
					return
				}
				msg := ev.Payload
				if msg.SessionID != sess.ID {
					continue
				}
				if msg.Role != message.Assistant && msg.Role != message.Tool {
					continue
				}

				// 1. Print new parts (tool calls, tool results).
				prevCount := seenParts[msg.ID]
				for i := prevCount; i < len(msg.Parts); i++ {
					switch p := msg.Parts[i].(type) {
					case message.ToolCall:
						seenToolInput[p.ID] = false
					case message.ToolResult:
						content := truncateMsg(p.Content, 200)
						if p.IsError {
							writeOutput("  ❌ %s: %s", p.Name, content)
						} else {
							writeOutput("  📦 %s", content)
						}
					case message.TextContent:
						t := strings.TrimSpace(p.Text)
						if t != "" {
							writeOutput("  💬 %s", truncateMsg(t, 200))
						}
						seenText[msg.ID] = len(p.Text)
					case message.ReasoningContent:
						t := strings.TrimSpace(p.Thinking)
						if t != "" {
							writeOutput("  🤔 %s", truncateMsg(t, 200))
						}
						seenThinking[msg.ID] = len(p.Thinking)
					}
				}

				// 2. Update existing parts (streaming text, reasoning, tool input).
				for i := 0; i < len(msg.Parts); i++ {
					switch p := msg.Parts[i].(type) {
					case message.TextContent:
						if len(p.Text) > seenText[msg.ID] {
							newText := p.Text[seenText[msg.ID]:]
							seenText[msg.ID] = len(p.Text)
							t := strings.TrimSpace(newText)
							if t != "" {
								writeOutput("  💬 %s", truncateMsg(t, 200))
							}
						}
					case message.ReasoningContent:
						if len(p.Thinking) > seenThinking[msg.ID] {
							newThink := p.Thinking[seenThinking[msg.ID]:]
							seenThinking[msg.ID] = len(p.Thinking)
							t := strings.TrimSpace(newThink)
							if t != "" {
								writeOutput("  🤔 %s", truncateMsg(t, 200))
							}
						}
					case message.ToolCall:
						if !seenToolInput[p.ID] && p.Input != "" {
							seenToolInput[p.ID] = true
							writeOutput("  🛠 %s(%s)", p.Name, truncateMsg(p.Input, 200))
						}
					}
				}
				seenParts[msg.ID] = len(msg.Parts)
			}
		}
	}()

	// Watchdog: detect stuck state (5 min no progress).
	watchdogCtx, watchdogCancel := context.WithCancel(ctx)
	defer watchdogCancel()
	lastActivity := time.Now()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchdogCtx.Done():
				return
			case <-ticker.C:
				if time.Since(lastActivity) > 5*time.Minute {
					writeOutput("  ⚠ No progress for 5 minutes. Checking in...")
					logEvent("watchdog", "No progress for 5 minutes")
					slog.Warn("AutoPilot watchdog triggered: no activity",
						"session_id", sess.ID)
				}
			}
		}
	}()

	writeOutput("── AutoPilot ──────────────────────────────────")
	logEvent("start", "AutoPilot session started")
	if mainCtx != "" {
		writeOutput("  📖 Loaded context from main session")
	}
	writeOutput("  🔍 Analyzing codebase...")

	// Initial analysis prompt.
	prompt := "Analyze the current codebase state. Read git log, check git status, review recent changes. Identify what needs improvement. Then create a plan and execute it."
	if mainCtx != "" {
		prompt += "\n\nRecent conversation context from the main session:\n" + mainCtx
	}

	iterations := 0
	iterationsPerSession := 0

	for {
		select {
		case <-ctx.Done():
			writeOutput("  ⛔ AutoPilot stopped (Ctrl+C)")
			logEvent("stop", "user cancelled")
			return ctx.Err()
		default:
		}

		iterations++
		iterationsPerSession++
		lastActivity = time.Now()
		logEvent("iteration", fmt.Sprintf("Iteration %d (session: %d)", iterations, iterationsPerSession))

		opts.Prompt = prompt
		result, err := agent.Run(ctx, opts)
		if err != nil {
			lastActivity = time.Now()
			logEvent("error", fmt.Sprintf("Agent error: %v", err))
			if ctx.Err() != nil {
				writeOutput("  ⛔ AutoPilot cancelled.")
				return ctx.Err()
			}
			writeOutput("  ⚠ Error: %v", err)
			prompt = "The previous operation encountered an error. Analyze what went wrong and try a different approach. Focus on making progress."
			continue
		}

		response := strings.TrimSpace(result.Response.Content.Text())
		logEvent("response", response)

		// Emergency rotation: context window exhausted.
		if result.Response.FinishReason == fantasy.FinishReasonLength {
			writeOutput("  🔄 Context window exhausted. Rotating session...")
			logEvent("rotation", "emergency rotate: context window full")

			summary := buildSessionSummary(result, response)
			newSess, err := c.sessions.Create(ctx, "AutoPilot (rotated)")
			if err != nil {
				writeOutput("  ⚠ Failed to create new session: %v", err)
				prompt = "Continue working. The previous session ended due to context limits."
				iterationsPerSession = 0
				continue
			}

			oldID := opts.SessionID
			opts.SessionID = newSess.ID
			c.permissions.AutoApproveSession(newSess.ID)
			iterationsPerSession = 0

			writeOutput("  📋 Session rotated: %s → %s", truncateMsg(oldID, 12), truncateMsg(newSess.ID, 12))
			logEvent("rotation", fmt.Sprintf("new_session=%s summary=%s", newSess.ID, summary))
			prompt = "Continue from the previous session. Summary of progress so far:\n" + summary + "\n\nAnalyze the current state and decide what to do next."
			continue
		}

		// Natural break: task complete and session old enough to rotate.
		taskDone := strings.Contains(response, "<autopilot>DONE</autopilot>") ||
			strings.Contains(response, "<autopilot>BLOCKED")

		if taskDone {
			writeOutput("  ✅ Task completed.")
			logEvent("phase", "task-complete")

			if iterationsPerSession >= 25 {
				writeOutput("  🔄 Session age %d iterations. Rotating to fresh context...", iterationsPerSession)
				logEvent("rotation", fmt.Sprintf("proactive rotate at %d iterations", iterationsPerSession))

				summary := buildSessionSummary(result, response)
				newSess, err := c.sessions.Create(ctx, "AutoPilot (rotated)")
				if err != nil {
					writeOutput("  ⚠ Failed to create new session: %v", err)
				} else {
					opts.SessionID = newSess.ID
					c.permissions.AutoApproveSession(newSess.ID)
					iterationsPerSession = 0

					writeOutput("  📋 Session rotated: fresh context with summary")
					logEvent("rotation", fmt.Sprintf("new_session=%s summary=%s", newSess.ID, summary))
					prompt = "Continue from the previous session. Summary of progress so far:\n" + summary + "\n\nAnalyze the codebase and identify the next most valuable improvement."
					continue
				}
			}

			prompt = "What should I improve next? Analyze the codebase and identify the next most valuable improvement."
			continue
		}

		// Continue working.
		prompt = "Continue working on the improvement. Review what you've done and decide: are you done with this task? If yes, say <autopilot>DONE</autopilot>. If blocked, say <autopilot>BLOCKED: reason</autopilot>. Otherwise, keep working."
	}
}

// buildSessionSummary extracts key progress info from the current iteration.
func buildSessionSummary(result *fantasy.AgentResult, response string) string {
	var parts []string
	parts = append(parts, "- Most recent action: "+truncateMsg(response, 200))

	if result != nil {
		for _, step := range result.Steps {
			content := step.Response.Content
			for _, call := range content.ToolCalls() {
				parts = append(parts, fmt.Sprintf("- Tool: %s", call.ToolName))
			}
			for _, tr := range content.ToolResults() {
				if text, ok := tr.Result.(fantasy.ToolResultOutputContentText); ok {
					t := strings.TrimSpace(text.Text)
					if t != "" {
						parts = append(parts, fmt.Sprintf("- Result: %s", truncateMsg(t, 150)))
					}
				}
			}
		}
	}

	if len(parts) > 10 {
		parts = parts[len(parts)-10:]
	}
	return strings.Join(parts, "\n")
}

func truncateMsg(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen] + "..."
}

// backgroundAgentManager is the shared background agent manager used by all tools.
// It is initialized once in buildTools and reused across spawn calls.
var backgroundAgentManager *tools.BackgroundAgentManager

// teamOrchestrator is the shared team orchestrator used by the team tool.
// It is initialized once in buildTools and reused across rebuilds.
var teamOrchestrator *tools.TeamOrchestrator

// maxConcurrentAgents controls how many background/team tasks can run simultaneously.
const maxConcurrentAgents = 5

// updateParentSessionCost accumulates the cost from a child session to its parent session.
func (c *coordinator) updateParentSessionCost(ctx context.Context, childSessionID, parentSessionID string) error {
	childSession, err := c.sessions.Get(ctx, childSessionID)
	if err != nil {
		return fmt.Errorf("get child session: %w", err)
	}

	parentSession, err := c.sessions.Get(ctx, parentSessionID)
	if err != nil {
		return fmt.Errorf("get parent session: %w", err)
	}

	parentSession.Cost += childSession.Cost

	if _, err := c.sessions.Save(ctx, parentSession); err != nil {
		return fmt.Errorf("save parent session: %w", err)
	}

	return nil
}

// discoverSkills runs the skill discovery pipeline and returns both the
// pre-filter (all discovered, after dedup) and post-filter (active) lists.
// It also emits a single diagnostic log line summarising the outcome to
// help track skill-loading health over time.
func discoverSkills(cfg *config.ConfigStore) (allSkills, activeSkills []*skills.Skill) {
	builtin, builtinStates := skills.DiscoverBuiltinWithStates()
	discovered := append([]*skills.Skill(nil), builtin...)

	var userStates []*skills.SkillState
	var userPaths []string

	opts := cfg.Config().Options
	if opts != nil && len(opts.SkillsPaths) > 0 {
		userPaths = make([]string, 0, len(opts.SkillsPaths))
		for _, pth := range opts.SkillsPaths {
			expanded := home.Long(pth)
			if strings.HasPrefix(expanded, "$") {
				if resolved, err := cfg.Resolver().ResolveValue(expanded); err == nil {
					expanded = resolved
				}
			}
			userPaths = append(userPaths, expanded)
		}
		var userSkills []*skills.Skill
		userSkills, userStates = skills.DiscoverWithStates(userPaths)
		discovered = append(discovered, userSkills...)
	}

	allSkills = skills.Deduplicate(discovered)
	var disabledSkills []string
	if opts != nil {
		disabledSkills = opts.DisabledSkills
	}
	activeSkills = skills.Filter(allSkills, disabledSkills)

	allStates := append([]*skills.SkillState(nil), builtinStates...)
	allStates = append(allStates, userStates...)

	allStates = skills.DeduplicateStates(allStates)

	slices.SortStableFunc(allStates, func(a, b *skills.SkillState) int {
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})
	skills.SetLatestStates(allStates)
	skills.PublishStates(allStates)

	logDiscoveryStats(builtin, builtinStates, userStates, userPaths, allSkills, activeSkills, disabledSkills)
	return allSkills, activeSkills
}

// logTurnSkillUsage emits a per-turn diagnostic line showing which skills
// (if any) were loaded during this turn and which looked relevant based on
// a cheap keyword match against the user prompt. The goal is to surface
// "should-have-loaded but didn't" situations for later analysis.
//
// Logged at Info level under component=skills; heavy fields are elided when
// there is nothing interesting to report.
func logTurnSkillUsage(
	sessionID string,
	prompt string,
	activeSkills []*skills.Skill,
	tracker *skills.Tracker,
	before []string,
) {
	if tracker == nil || len(activeSkills) == 0 {
		return
	}

	after := tracker.LoadedNames()

	beforeSet := make(map[string]bool, len(before))
	for _, n := range before {
		beforeSet[n] = true
	}
	var loadedThisTurn []string
	for _, n := range after {
		if !beforeSet[n] {
			loadedThisTurn = append(loadedThisTurn, n)
		}
	}

	slog.Info("Skill turn summary",
		"component", "skills",
		"session_id", sessionID,
		"prompt_len", len(prompt),
		"active_total", len(activeSkills),
		"loaded_total", len(after),
		"loaded_this_turn", loadedThisTurn,
	)
}

// logDiscoveryStats emits a single structured log line summarising skill
// discovery for the current session. It is intentionally low-volume: one
// line per session start.
func logDiscoveryStats(
	builtin []*skills.Skill,
	builtinStates, userStates []*skills.SkillState,
	userPaths []string,
	allSkills, activeSkills []*skills.Skill,
	disabled []string,
) {
	countErrors := func(states []*skills.SkillState) int {
		n := 0
		for _, s := range states {
			if s.State == skills.StateError {
				n++
			}
		}
		return n
	}

	userOK := 0
	for _, s := range userStates {
		if s.State == skills.StateNormal {
			userOK++
		}
	}

	activeNames := make([]string, 0, len(activeSkills))
	for _, s := range activeSkills {
		activeNames = append(activeNames, s.Name)
	}

	xml := skills.ToPromptXML(activeSkills)

	slog.Info("Skill discovery complete",
		"component", "skills",
		"builtin_ok", len(builtin),
		"builtin_errors", countErrors(builtinStates),
		"user_ok", userOK,
		"user_errors", countErrors(userStates),
		"user_paths", len(userPaths),
		"deduped_total", len(allSkills),
		"active", len(activeSkills),
		"disabled", len(disabled),
		"prompt_bytes", len(xml),
		"prompt_tok_est", skills.ApproxTokenCount(xml),
		"active_names", activeNames,
	)
}
