package tools

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
)

//go:embed background_agent.md
var backgroundAgentDescription string

const (
	SpawnAgentToolName   = "spawn_agent"
	CollectAgentToolName = "collect_agent"
	AgentStatusToolName  = "agent_status"
)

// BackgroundAgentResult stores the result of a completed background agent.
type BackgroundAgentResult struct {
	ID        string
	Prompt    string
	Result    string
	Error     string
	Status    string // "queued", "running", "completed", "error", "cancelled"
	SessionID string // session that spawned this agent (for notification)
	CreatedAt time.Time
	DoneAt    time.Time
}

// backgroundAgentStore holds results of background agents.
var backgroundAgentStore struct {
	mu      sync.Mutex
	agents  map[string]*BackgroundAgentResult
	counter atomic.Int64
}

func init() {
	backgroundAgentStore.agents = make(map[string]*BackgroundAgentResult)
}

// SpawnAgentParams defines parameters for spawning a background agent.
type SpawnAgentParams struct {
	Prompt      string `json:"prompt" description:"The task for the background agent to perform"`
	Description string `json:"description,omitempty" description:"A short description of what this agent is doing"`
	Timeout     int    `json:"timeout_seconds,omitempty" description:"Maximum execution time in seconds (default: 600, max: 3600)"`
}

// CollectAgentParams defines parameters for collecting a background agent result.
type CollectAgentParams struct {
	AgentID string `json:"agent_id" description:"The agent ID returned by spawn_agent"`
}

// NewSpawnAgentTool creates a tool that spawns a background agent for async execution.
// The agent runs in its own goroutine via the BackgroundAgentManager, which manages
// concurrency limits, timeouts, and result storage.
func NewSpawnAgentTool(workingDir string, manager *BackgroundAgentManager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SpawnAgentToolName,
		"Spawn a background agent to perform a task asynchronously. You can continue working while it runs.",
		func(ctx context.Context, params SpawnAgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			ReportActivity(ctx, "Spawning agent: "+params.Description)

			// Validate timeout: cap at 1 hour.
			timeout := time.Duration(params.Timeout) * time.Second
			if params.Timeout <= 0 {
				timeout = 0 // use manager default
			} else if timeout > 3600*time.Second {
				timeout = 3600 * time.Second
			}

			sessionID := GetSessionFromContext(ctx)

			id, err := manager.Spawn(ctx, SpawnParams{
				Prompt:      params.Prompt,
				Description: params.Description,
				Timeout:     timeout,
				SessionID:   sessionID,
			})
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to spawn agent: %s", err)), nil
			}

			return fantasy.NewTextResponse(fmt.Sprintf(
				"## Agent Spawned\n\n**Agent ID**: `%s`\n**Task**: %s\n**Session**: %s\n\n"+
					"The agent is now running in the background.\n"+
					"Use `agent_status` with agent_id=\"%s\" to check progress.\n"+
					"Use `collect_agent` with agent_id=\"%s\" to retrieve results when done.\n\n"+
					"In the meantime, you can continue working on other tasks.",
				id, params.Prompt, sessionID, id, id,
			)), nil
		})
}

// NewAgentStatusTool creates a tool that checks the status of a background agent.
func NewAgentStatusTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		AgentStatusToolName,
		"Check the status of a previously spawned background agent. Use the agent_id returned by spawn_agent.",
		func(ctx context.Context, params CollectAgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.AgentID == "" {
				return fantasy.NewTextErrorResponse("agent_id is required"), nil
			}

			backgroundAgentStore.mu.Lock()
			agent, ok := backgroundAgentStore.agents[params.AgentID]
			backgroundAgentStore.mu.Unlock()

			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("agent not found: %s", params.AgentID)), nil
			}

			var duration string
			if agent.Status == "running" || agent.Status == "queued" {
				duration = fmt.Sprintf("running for %s", time.Since(agent.CreatedAt).Round(time.Second))
			} else {
				duration = fmt.Sprintf("took %s", agent.DoneAt.Sub(agent.CreatedAt).Round(time.Second))
			}

			statusLine := ""
			switch agent.Status {
			case "queued":
				statusLine = "⏳ Queued (waiting for a concurrency slot)"
			case "running":
				statusLine = "🔄 Running"
			case "completed":
				statusLine = "✅ Completed"
			case "error":
				statusLine = "❌ Error"
			case "cancelled":
				statusLine = "⏹️ Cancelled"
			default:
				statusLine = "❓ Unknown"
			}

			return fantasy.NewTextResponse(fmt.Sprintf(
				"## Agent Status: %s\n\n**Agent ID**: `%s`\n**Task**: %s\n**Duration**: %s\n\n"+
					"%s\n\n"+
					"Use `collect_agent` to retrieve the full result when the agent is done.",
				statusLine, agent.ID, agent.Prompt, duration, statusLine,
			)), nil
		})
}

// NewCollectAgentTool creates a tool that collects the result of a background agent.
func NewCollectAgentTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		CollectAgentToolName,
		"Collect and retrieve the result of a previously spawned background agent. Use the agent_id returned by spawn_agent.",
		func(ctx context.Context, params CollectAgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.AgentID == "" {
				return fantasy.NewTextErrorResponse("agent_id is required"), nil
			}

			backgroundAgentStore.mu.Lock()
			agent, ok := backgroundAgentStore.agents[params.AgentID]
			if ok {
				delete(backgroundAgentStore.agents, params.AgentID)
			}
			backgroundAgentStore.mu.Unlock()

			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("agent not found: %s", params.AgentID)), nil
			}

			if agent.Status == "queued" || agent.Status == "running" {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"agent %s is still %s (started %s). Use `agent_status` to check progress.",
					agent.ID, agent.Status, agent.CreatedAt.Format(time.RFC3339),
				)), nil
			}

			if agent.Status == "error" {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"agent %s failed: %s", agent.ID, agent.Error,
				)), nil
			}

			if agent.Status == "cancelled" {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"agent %s was cancelled: %s", agent.ID, agent.Error,
				)), nil
			}

			return fantasy.NewTextResponse(fmt.Sprintf(
				"## Background Agent Result\n\n**Agent ID**: `%s`\n**Task**: %s\n**Duration**: %s\n\n```\n%s\n```",
				agent.ID, agent.Prompt,
				agent.DoneAt.Sub(agent.CreatedAt).Round(time.Second),
				agent.Result,
			)), nil
		})
}

// notifiedAgents tracks which background agent completions have been
// reported to the user via auto-notification, to avoid duplicates.
var notifiedAgents = struct {
	mu   sync.Mutex
	seen map[string]bool
}{seen: make(map[string]bool)}

// GetBackgroundNotifications checks for completed background agents that
// belong to the given session and returns formatted notification messages
// for any that have not yet been reported. Each agent is marked as notified
// so it is reported only once.
func GetBackgroundNotifications(sessionID string) []string {
	backgroundAgentStore.mu.Lock()
	defer backgroundAgentStore.mu.Unlock()

	notifiedAgents.mu.Lock()
	defer notifiedAgents.mu.Unlock()

	var notifications []string
	for _, agent := range backgroundAgentStore.agents {
		if agent.SessionID != sessionID {
			continue
		}
		if notifiedAgents.seen[agent.ID] {
			continue
		}

		switch agent.Status {
		case "completed":
			notifiedAgents.seen[agent.ID] = true
			summary := agent.Result
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			notifications = append(notifications, fmt.Sprintf(
				"<background_task_complete>\n**Agent**: `%s`\n**Task**: %s\n**Status**: ✅ Completed\n**Result**: %s\n\n"+
					"Use `collect_agent` with `agent_id=\"%s\"` to retrieve the full result.\n</background_task_complete>",
				agent.ID, agent.Prompt, summary, agent.ID,
			))

		case "error":
			notifiedAgents.seen[agent.ID] = true
			notifications = append(notifications, fmt.Sprintf(
				"<background_task_complete>\n**Agent**: `%s`\n**Task**: %s\n**Status**: ❌ Failed\n**Error**: %s\n</background_task_complete>",
				agent.ID, agent.Prompt, agent.Error,
			))

		case "cancelled":
			notifiedAgents.seen[agent.ID] = true
			notifications = append(notifications, fmt.Sprintf(
				"<background_task_complete>\n**Agent**: `%s`\n**Task**: %s\n**Status**: ⏹️ Cancelled\n</background_task_complete>",
				agent.ID, agent.Prompt,
			))
		}
	}

	return notifications
}

// StoreBackgroundAgentResult stores the result of a background agent for later retrieval.
func StoreBackgroundAgentResult(id, result string, err error) {
	backgroundAgentStore.mu.Lock()
	defer backgroundAgentStore.mu.Unlock()

	agent, ok := backgroundAgentStore.agents[id]
	if !ok {
		return
	}

	if err != nil {
		agent.Status = "error"
		agent.Error = err.Error()
	} else {
		agent.Status = "completed"
		agent.Result = result
	}
	agent.DoneAt = time.Now()
}
