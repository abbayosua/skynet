package agent

import (
	"context"
	"strings"
	"sync"

	"charm.land/fantasy"
)

// getMaxRetries returns higher max retries for B.AI/DeepSeek providers
// to handle rate limits gracefully. Other providers use default (2).
func getMaxRetries(model Model) *int {
	if strings.HasPrefix(model.ModelCfg.Provider, "b-ai") {
		n := 10 // More retries for rate-limited providers
		return &n
	}
	return nil // Use fantasy default (2)
}

// deepseekProvider wraps a fantasy.Provider and ensures reasoning_content
// is always included in assistant messages when thinking mode is active.
// This works around DeepSeek/B.AI requiring reasoning_content to be
// echoed back in ALL assistant messages, not just ones with tool calls.
//
// The provider also captures reasoning_content from responses and injects
// it back into the conversation history, enabling DeepSeek prefix cache
// to work correctly (exact prefix match requires consistent payload).
type deepseekProvider struct {
	inner fantasy.Provider

	// lastReasoning stores the reasoning_content from the most recent
	// assistant response, keyed by a simple turn counter.
	mu              sync.Mutex
	lastReasoning   string
	turnCounter     int
	reasoningByTurn map[int]string
}

func newDeepseekProvider(inner fantasy.Provider) *deepseekProvider {
	return &deepseekProvider{
		inner:           inner,
		reasoningByTurn: make(map[int]string),
	}
}

func (p *deepseekProvider) Name() string {
	return p.inner.Name()
}

func (p *deepseekProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	lm, err := p.inner.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return &deepseekLanguageModel{
		inner:    lm,
		provider: p,
	}, nil
}

type deepseekLanguageModel struct {
	inner    fantasy.LanguageModel
	provider *deepseekProvider
}

func (m *deepseekLanguageModel) Provider() string { return m.inner.Provider() }
func (m *deepseekLanguageModel) Model() string     { return m.inner.Model() }

func (m *deepseekLanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	call.Prompt = m.provider.ensureReasoningInPrompt(call.Prompt)
	resp, err := m.inner.Generate(ctx, call)
	if err == nil {
		m.provider.captureReasoning(resp)
	}
	return resp, err
}

func (m *deepseekLanguageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	call.Prompt = m.provider.ensureReasoningInPrompt(call.Prompt)
	resp, err := m.inner.Stream(ctx, call)
	if err == nil {
		// For streaming, capture reasoning from the final response
		// The actual capture happens in the agent loop after stream completes
	}
	return resp, err
}

func (m *deepseekLanguageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return m.inner.GenerateObject(ctx, call)
}

func (m *deepseekLanguageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.inner.StreamObject(ctx, call)
}

// captureReasoning extracts reasoning_content from the response and stores it.
func (p *deepseekProvider) captureReasoning(resp *fantasy.Response) {
	if resp == nil {
		return
	}

	// Extract reasoning from response content
	for _, part := range resp.Content {
		if rc, ok := part.(fantasy.ReasoningPart); ok {
			if rc.Text != "" {
				p.mu.Lock()
				p.lastReasoning = rc.Text
				p.turnCounter++
				p.reasoningByTurn[p.turnCounter] = rc.Text
				p.mu.Unlock()
				return
			}
		}
	}
}

// ensureReasoningInPrompt adds reasoning_content to assistant messages
// that don't have one. It extracts reasoning from the most recent assistant
// message in the prompt (from history), so it works for both streaming and
// non-streaming modes.
//
// IMPORTANT: Never inject empty reasoning_content — DeepSeek rejects
// requests with "reasoning_content": "". Only inject when we have a
// non-empty reasoning string from a previous turn.
func (p *deepseekProvider) ensureReasoningInPrompt(prompt fantasy.Prompt) fantasy.Prompt {
	// First, try to extract reasoning from the prompt's assistant messages
	// This is more reliable than capturing from responses (which doesn't work for streaming)
	reasoningFromHistory := extractLastReasoning(prompt)

	// Fall back to captured reasoning from non-streaming responses
	p.mu.Lock()
	capturedReasoning := p.lastReasoning
	p.mu.Unlock()

	// Use whichever we have (prefer history as it's more complete)
	reasoning := reasoningFromHistory
	if reasoning == "" {
		reasoning = capturedReasoning
	}

	// No reasoning found anywhere — nothing to inject
	if reasoning == "" {
		return prompt
	}

	patched := make(fantasy.Prompt, len(prompt))
	for i, msg := range prompt {
		if msg.Role != fantasy.MessageRoleAssistant {
			patched[i] = msg
			continue
		}

		// Check if this message already has reasoning content
		hasReasoning := false
		for _, c := range msg.Content {
			if c.GetType() == fantasy.ContentTypeReasoning {
				hasReasoning = true
				break
			}
		}

		if hasReasoning {
			patched[i] = msg
			continue
		}

		newMsg := msg
		newMsg.Content = append([]fantasy.MessagePart{
			fantasy.ReasoningPart{Text: reasoning},
		}, msg.Content...)
		patched[i] = newMsg
	}

	return patched
}

// extractLastReasoning scans assistant messages in the prompt and returns
// the most recent non-empty reasoning_content. Works for both streaming
// and non-streaming modes since it reads from the history.
func extractLastReasoning(prompt fantasy.Prompt) string {
	var lastReasoning string
	for _, msg := range prompt {
		if msg.Role != fantasy.MessageRoleAssistant {
			continue
		}
		for _, c := range msg.Content {
			if rc, ok := c.(fantasy.ReasoningPart); ok {
				if rc.Text != "" {
					lastReasoning = rc.Text
				}
			}
		}
	}
	return lastReasoning
}
