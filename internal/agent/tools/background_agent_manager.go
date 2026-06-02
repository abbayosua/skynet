package tools

import (
	"context"
	"fmt"
	"time"
)

// AgentRunFunc is a function that runs an agent task synchronously and returns the result.
// Implemented by the coordinator which has access to session management, agent building,
// and model configuration.
type AgentRunFunc func(ctx context.Context, prompt string) (string, error)

// OnBackgroundComplete is a callback invoked when a background agent completes
// (either successfully or with error). The sessionID identifies which session
// spawned the agent, allowing the coordinator to inject a notification message.
type OnBackgroundComplete func(agentID, sessionID, summary string)

// BackgroundAgentManager manages async execution of background agent tasks.
// It provides a semaphore-based concurrency limiter and handles per-task
// lifecycle: queued → running → completed/error/cancelled.
//
// Each spawned agent runs in its own goroutine with an independent context
// and configurable timeout. When a task completes, the OnComplete callback
// is invoked with the agent ID and spawning session ID so the coordinator
// can notify the user.
type BackgroundAgentManager struct {
	runFn      AgentRunFunc
	semaphore  chan struct{}
	timeout    time.Duration
	onComplete OnBackgroundComplete
}

// BackgroundAgentOption configures the BackgroundAgentManager.
type BackgroundAgentOption func(*BackgroundAgentManager)

// WithMaxConcurrentAgents limits how many background agents can run in parallel.
// Additional spawns are queued until a slot opens. Default: 5.
func WithMaxConcurrentAgents(n int) BackgroundAgentOption {
	return func(m *BackgroundAgentManager) {
		if n > 0 {
			m.semaphore = make(chan struct{}, n)
		}
	}
}

// WithDefaultAgentTimeout sets the default timeout for each background agent.
// Individual spawns can override this via SpawnParams.Timeout. Default: 10 min.
func WithDefaultAgentTimeout(d time.Duration) BackgroundAgentOption {
	return func(m *BackgroundAgentManager) {
		if d > 0 {
			m.timeout = d
		}
	}
}

// WithOnComplete sets a callback invoked when a background agent completes.
func WithOnComplete(cb OnBackgroundComplete) BackgroundAgentOption {
	return func(m *BackgroundAgentManager) {
		m.onComplete = cb
	}
}

// NewBackgroundAgentManager creates a new manager that uses the given runner.
// By default allows 5 concurrent agents with a 10-minute timeout.
func NewBackgroundAgentManager(runFn AgentRunFunc, opts ...BackgroundAgentOption) *BackgroundAgentManager {
	m := &BackgroundAgentManager{
		runFn:     runFn,
		semaphore: make(chan struct{}, 5),
		timeout:   10 * time.Minute,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// SpawnParams defines parameters for spawning a single background agent.
type SpawnParams struct {
	Prompt      string
	Description string
	Timeout     time.Duration // 0 = use manager default
	SessionID   string        // session that spawned this agent (for notification)
}

// Spawn enqueues a background agent task and returns immediately with an
// agent ID. The task transitions queued → running → completed/error.
// The caller can check status with agent_status and collect the result
// with collect_agent.
func (m *BackgroundAgentManager) Spawn(ctx context.Context, params SpawnParams) (string, error) {
	if params.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	id := fmt.Sprintf("bg_%d_%d", time.Now().UnixNano(), backgroundAgentStore.counter.Add(1))

	// Record initial queued state.
	backgroundAgentStore.mu.Lock()
	backgroundAgentStore.agents[id] = &BackgroundAgentResult{
		ID:        id,
		Prompt:    params.Prompt,
		Status:    "queued",
		SessionID: params.SessionID,
		CreatedAt: time.Now(),
	}
	backgroundAgentStore.mu.Unlock()

	// Acquire a concurrency slot or block until one is available.
	select {
	case m.semaphore <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	go m.execute(ctx, id, params)
	return id, nil
}

// execute runs the background agent in a goroutine and stores the result.
// If the first attempt fails, it retries once with a simplified prompt that
// includes the error context (intelligent retry).
func (m *BackgroundAgentManager) execute(parentCtx context.Context, id string, params SpawnParams) {
	defer func() { <-m.semaphore }() // Release concurrency slot.

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = m.timeout
	}

	// Create an independent context so the agent isn't cancelled when the
	// spawning conversation ends. Use the parent only for initial guidance.
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Mark as running.
	backgroundAgentStore.mu.Lock()
	if a, ok := backgroundAgentStore.agents[id]; ok {
		a.Status = "running"
	}
	backgroundAgentStore.mu.Unlock()

	// Attempt 1: Execute the agent task with the original prompt.
	result, err := m.runFn(runCtx, params.Prompt)
	if err == nil {
		StoreBackgroundAgentResult(id, result, nil)
		m.notifyComplete(id, params.SessionID, result)
		return
	}

	// Check if context was cancelled (timeout or external cancel) - don't retry.
	if runCtx.Err() != nil {
		errMsg := fmt.Errorf("task cancelled: %w", runCtx.Err())
		StoreBackgroundAgentResult(id, "", errMsg)
		m.notifyComplete(id, params.SessionID, errMsg.Error())
		return
	}

	// Intelligent retry: on non-context errors, try once more with a modified
	// prompt that includes the error context for a more focused attempt.
	retryPrompt := fmt.Sprintf(
		`I previously asked you to do this task but got an error:

Original task: %s

Error: %s

Please retry this task. Consider simplifying your approach or breaking it down further.`,
		params.Prompt, err.Error(),
	)

	// Small delay before retry to avoid immediate re-failure.
	select {
	case <-time.After(2 * time.Second):
	case <-runCtx.Done():
		errMsg := fmt.Errorf("retry cancelled: %w", runCtx.Err())
		StoreBackgroundAgentResult(id, "", errMsg)
		m.notifyComplete(id, params.SessionID, errMsg.Error())
		return
	}

	// Check context again after delay.
	if runCtx.Err() != nil {
		errMsg := fmt.Errorf("retry cancelled: %w", runCtx.Err())
		StoreBackgroundAgentResult(id, "", errMsg)
		m.notifyComplete(id, params.SessionID, errMsg.Error())
		return
	}

	// Attempt 2: Retry with simplified prompt.
	result, retryErr := m.runFn(runCtx, retryPrompt)
	if retryErr != nil {
		errMsg := fmt.Errorf("attempt 1: %s; attempt 2: %s", err, retryErr)
		StoreBackgroundAgentResult(id, "", errMsg)
		m.notifyComplete(id, params.SessionID, errMsg.Error())
		return
	}

	StoreBackgroundAgentResult(id, result, nil)
	m.notifyComplete(id, params.SessionID, result)
}

// notifyComplete calls the OnComplete callback if set.
func (m *BackgroundAgentManager) notifyComplete(agentID, sessionID, summary string) {
	if m.onComplete != nil && sessionID != "" {
		m.onComplete(agentID, sessionID, summary)
	}
}

// Cancel cancels a running background agent by agent ID.
// The agent must have been spawned by this manager.
func (m *BackgroundAgentManager) Cancel(agentID string) {
	backgroundAgentStore.mu.Lock()
	defer backgroundAgentStore.mu.Unlock()

	agent, ok := backgroundAgentStore.agents[agentID]
	if !ok {
		return
	}
	if agent.Status != "running" && agent.Status != "queued" {
		return
	}
	agent.Status = "cancelled"
	agent.DoneAt = time.Now()
	agent.Error = "cancelled by user"
}

// RunningCount returns the number of currently running or queued agents.
func (m *BackgroundAgentManager) RunningCount() int {
	backgroundAgentStore.mu.Lock()
	defer backgroundAgentStore.mu.Unlock()

	count := 0
	for _, a := range backgroundAgentStore.agents {
		if a.Status == "running" || a.Status == "queued" {
			count++
		}
	}
	return count
}
