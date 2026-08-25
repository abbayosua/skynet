package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/abbayosua/skynet/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSessionAgent is a minimal mock for the SessionAgent interface.
type mockSessionAgent struct {
	model     Model
	runFunc   func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error)
	cancelled []string
}

func (m *mockSessionAgent) Run(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	return m.runFunc(ctx, call)
}

func (m *mockSessionAgent) Model() Model                        { return m.model }
func (m *mockSessionAgent) SetModels(large, small Model)        {}
func (m *mockSessionAgent) SetTools(tools []fantasy.AgentTool)  {}
func (m *mockSessionAgent) SetSystemPrompt(systemPrompt string) {}
func (m *mockSessionAgent) Cancel(sessionID string) {
	m.cancelled = append(m.cancelled, sessionID)
}
func (m *mockSessionAgent) CancelAll()                                  {}
func (m *mockSessionAgent) IsSessionBusy(sessionID string) bool         { return false }
func (m *mockSessionAgent) IsBusy() bool                                { return false }
func (m *mockSessionAgent) QueuedPrompts(sessionID string) int          { return 0 }
func (m *mockSessionAgent) QueuedPromptsList(sessionID string) []string { return nil }
func (m *mockSessionAgent) ClearQueue(sessionID string)                 {}
func (m *mockSessionAgent) EnqueuePrompt(sessionID, prompt string)      {}
func (m *mockSessionAgent) SetAnswerShort(enabled bool)                 {}
func (m *mockSessionAgent) AnswerShort() bool                           { return false }
func (m *mockSessionAgent) SetAnswerShortPrompt(prompt string)          {}
func (m *mockSessionAgent) AnswerShortPrompt() string                   { return "" }
func (m *mockSessionAgent) Summarize(context.Context, string, fantasy.ProviderOptions) error {
	return nil
}

// newTestCoordinator creates a minimal coordinator for unit testing runSubAgent.
func newTestCoordinator(t *testing.T, env fakeEnv, providerID string, providerCfg config.ProviderConfig) *coordinator {
	cfg, err := config.Init(env.workingDir, "", false)
	require.NoError(t, err)
	cfg.Config().Providers.Set(providerID, providerCfg)
	return &coordinator{
		cfg:      cfg,
		sessions: env.sessions,
	}
}

// newMockAgent creates a mockSessionAgent with the given provider and run function.
func newMockAgent(providerID string, maxTokens int64, runFunc func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error)) *mockSessionAgent {
	return &mockSessionAgent{
		model: Model{
			CatwalkCfg: catwalk.Model{
				DefaultMaxTokens: maxTokens,
			},
			ModelCfg: config.SelectedModel{
				Provider: providerID,
			},
		},
		runFunc: runFunc,
	}
}

// agentResultWithText creates a minimal AgentResult with the given text response.
func agentResultWithText(text string) *fantasy.AgentResult {
	return &fantasy.AgentResult{
		Response: fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.TextContent{Text: text},
			},
		},
	}
}

func TestRunSubAgent(t *testing.T) {
	const providerID = "test-provider"
	providerCfg := config.ProviderConfig{ID: providerID}

	t.Run("happy path", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			assert.Equal(t, "do something", call.Prompt)
			assert.Equal(t, int64(4096), call.MaxOutputTokens)
			return agentResultWithText("done"), nil
		})

		resp, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "do something",
			SessionTitle:   "Test Session",
		})
		require.NoError(t, err)
		assert.Equal(t, "done", resp.Content)
		assert.False(t, resp.IsError)
	})

	t.Run("ModelCfg.MaxTokens overrides default", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := &mockSessionAgent{
			model: Model{
				CatwalkCfg: catwalk.Model{
					DefaultMaxTokens: 4096,
				},
				ModelCfg: config.SelectedModel{
					Provider:  providerID,
					MaxTokens: 8192,
				},
			},
			runFunc: func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
				assert.Equal(t, int64(8192), call.MaxOutputTokens)
				return agentResultWithText("ok"), nil
			},
		}

		resp, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.NoError(t, err)
		assert.Equal(t, "ok", resp.Content)
	})

	t.Run("session creation failure with canceled context", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, nil)

		// Use a canceled context to trigger CreateTaskSession failure.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err = coord.runSubAgent(ctx, subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.Error(t, err)
	})

	t.Run("provider not configured", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		// Agent references a provider that doesn't exist in config.
		agent := newMockAgent("unknown-provider", 4096, nil)

		_, err = coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model provider not configured")
	})

	t.Run("agent run error returns error response", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, func(_ context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
			return nil, errors.New("provider request failed")
		})

		resp, err := coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		// runSubAgent returns (errorResponse, nil) when agent.Run fails — not a Go error.
		require.NoError(t, err)
		assert.True(t, resp.IsError)
		assert.Equal(t, "Failed to generate response: provider request failed", resp.Content)
	})

	t.Run("session setup callback is invoked", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		var setupCalledWith string
		agent := newMockAgent(providerID, 4096, func(_ context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
			return agentResultWithText("ok"), nil
		})

		_, err = coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
			SessionSetup: func(sessionID string) {
				setupCalledWith = sessionID
			},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, setupCalledWith, "SessionSetup should have been called")
	})

	t.Run("cost propagation to parent session", func(t *testing.T) {
		env := testEnv(t)
		coord := newTestCoordinator(t, env, providerID, providerCfg)

		parentSession, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		agent := newMockAgent(providerID, 4096, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			// Simulate the agent incurring cost by updating the child session.
			childSession, err := env.sessions.Get(ctx, call.SessionID)
			if err != nil {
				return nil, err
			}
			childSession.Cost = 0.05
			_, err = env.sessions.Save(ctx, childSession)
			if err != nil {
				return nil, err
			}
			return agentResultWithText("ok"), nil
		})

		_, err = coord.runSubAgent(t.Context(), subAgentParams{
			Agent:          agent,
			SessionID:      parentSession.ID,
			AgentMessageID: "msg-1",
			ToolCallID:     "call-1",
			Prompt:         "test",
			SessionTitle:   "Test",
		})
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parentSession.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.05, updated.Cost, 1e-9)
	})
}

func TestUpdateParentSessionCost(t *testing.T) {
	t.Run("accumulates cost correctly", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		child, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child")
		require.NoError(t, err)

		// Set child cost.
		child.Cost = 0.10
		_, err = env.sessions.Save(t.Context(), child)
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child.ID, parent.ID)
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.10, updated.Cost, 1e-9)
	})

	t.Run("accumulates multiple child costs", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		child1, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child1")
		require.NoError(t, err)
		child1.Cost = 0.05
		_, err = env.sessions.Save(t.Context(), child1)
		require.NoError(t, err)

		child2, err := env.sessions.CreateTaskSession(t.Context(), "tool-2", parent.ID, "Child2")
		require.NoError(t, err)
		child2.Cost = 0.03
		_, err = env.sessions.Save(t.Context(), child2)
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child1.ID, parent.ID)
		require.NoError(t, err)
		err = coord.updateParentSessionCost(t.Context(), child2.ID, parent.ID)
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.08, updated.Cost, 1e-9)
	})

	t.Run("child session not found", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), "non-existent", parent.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get child session")
	})

	t.Run("parent session not found", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)
		child, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child")
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child.ID, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get parent session")
	})

	t.Run("zero cost handled correctly", func(t *testing.T) {
		env := testEnv(t)
		cfg, err := config.Init(env.workingDir, "", false)
		require.NoError(t, err)
		coord := &coordinator{cfg: cfg, sessions: env.sessions}

		parent, err := env.sessions.Create(t.Context(), "Parent")
		require.NoError(t, err)
		child, err := env.sessions.CreateTaskSession(t.Context(), "tool-1", parent.ID, "Child")
		require.NoError(t, err)

		err = coord.updateParentSessionCost(t.Context(), child.ID, parent.ID)
		require.NoError(t, err)

		updated, err := env.sessions.Get(t.Context(), parent.ID)
		require.NoError(t, err)
		assert.InDelta(t, 0.0, updated.Cost, 1e-9)
	})
}

func TestGetProviderOptionsReasoningEffort(t *testing.T) {
	// Bedrock is Fantasy's Anthropic under a different provider name; options
	// must land under anthropic.Name so the Anthropic language model picks them up.
	tests := []struct {
		name         string
		providerType catwalk.Type
	}{
		{"anthropic honors reasoning_effort", catwalk.Type(anthropic.Name)},
		{"bedrock honors reasoning_effort", catwalk.Type(bedrock.Name)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := Model{
				CatwalkCfg: catwalk.Model{ID: "claude-opus-4-7", CanReason: true},
				ModelCfg: config.SelectedModel{
					Provider:        "test",
					ReasoningEffort: "max",
				},
			}
			providerCfg := config.ProviderConfig{ID: "test", Type: tc.providerType}

			opts := getProviderOptions(model, providerCfg, "sess-test")

			raw, ok := opts[anthropic.Name]
			require.True(t, ok, "options should be keyed under anthropic.Name for type %q", tc.providerType)
			parsed, ok := raw.(*anthropic.ProviderOptions)
			require.True(t, ok)
			require.NotNil(t, parsed.Effort)
			assert.Equal(t, anthropic.Effort("max"), *parsed.Effort)
		})
	}
}

func TestOpenCodeSessionID(t *testing.T) {
	first := openCodeSessionID()
	second := openCodeSessionID()

	assert.NotEqual(t, first, second, "session ids should be unique per call")
	for _, id := range []string{first, second} {
		require.True(t, strings.HasPrefix(id, "ses_"), "id %q should start with ses_", id)
		assert.Len(t, id, 30, "id %q should be 30 characters", id)
		for _, r := range id[4:] {
			valid := r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
			assert.True(t, valid, "id %q contains non-base62 character %q", id, r)
		}
	}
}

func TestProviderHeadersOpencodeSession(t *testing.T) {
	for _, providerID := range []string{string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen)} {
		providerCfg := config.ProviderConfig{ID: providerID, Type: catwalk.Type("openai-compat")}
		headers := providerHeaders(providerCfg, false)
		assert.Regexp(t, `^ses_[0-9A-Za-z]{26}$`, headers["x-opencode-session"],
			"provider %q should get a generated session id", providerID)
	}

	other := providerHeaders(config.ProviderConfig{ID: "openai", Type: catwalk.Type("openai")}, false)
	_, ok := other["x-opencode-session"]
	assert.False(t, ok, "non-opencode providers should not get a session id")
}

func TestProviderHeadersUserExtraHeadersPreserved(t *testing.T) {
	providerCfg := config.ProviderConfig{
		ID:           string(catwalk.InferenceProviderOpenCodeGo),
		Type:         catwalk.Type("openai-compat"),
		ExtraHeaders: map[string]string{"X-Custom": "value"},
	}
	headers := providerHeaders(providerCfg, false)
	assert.Equal(t, "value", headers["X-Custom"], "user extra headers should be preserved")
	assert.Contains(t, headers, "x-opencode-session")
}

func TestGetProviderOptionsOpencodeCacheKey(t *testing.T) {
	providerCfg := config.ProviderConfig{
		ID:   string(catwalk.InferenceProviderOpenCodeGo),
		Type: catwalk.Type("openai-compat"),
	}
	model := Model{CatwalkCfg: catwalk.Model{ID: "muse-spark-1.2-contributor"}}

	opts := getProviderOptions(model, providerCfg, "sess-abc123")
	raw, ok := opts[openaicompat.Name]
	require.True(t, ok, "options should be keyed under openaicompat.Name")
	parsed, ok := raw.(*openaicompat.ProviderOptions)
	require.True(t, ok)
	require.Equal(t, "sess-abc123", parsed.ExtraBody["prompt_cache_key"],
		"opencode providers should get per-session prompt cache key")

	// No session ID -> no cache key.
	opts = getProviderOptions(model, providerCfg, "")
	parsed = opts[openaicompat.Name].(*openaicompat.ProviderOptions)
	require.NotContains(t, parsed.ExtraBody, "prompt_cache_key")

	// Non-opencode provider -> no cache key.
	otherCfg := config.ProviderConfig{ID: "deepseek", Type: catwalk.Type("openai-compat")}
	opts = getProviderOptions(model, otherCfg, "sess-abc123")
	parsed = opts[openaicompat.Name].(*openaicompat.ProviderOptions)
	require.NotContains(t, parsed.ExtraBody, "prompt_cache_key")
}

func TestOpencodeNeedsResponsesAPI(t *testing.T) {
	require.True(t, opencodeNeedsResponsesAPI("muse-spark-1.2-contributor"))
	require.True(t, opencodeNeedsResponsesAPI("MUSE-Glimmer-30B"))
	require.False(t, opencodeNeedsResponsesAPI("x-preview-f-free"))
	require.False(t, opencodeNeedsResponsesAPI("mimo-v2.5"))
	require.False(t, opencodeNeedsResponsesAPI("deepseek-v4-flash"))
}
