// Package notify defines domain notification types for agent events.
// These types are decoupled from UI concerns so the agent can publish
// events without importing UI packages.
package notify

// Type identifies the kind of agent notification.
type Type string

const (
	// TypeAgentFinished indicates the agent has completed its turn.
	TypeAgentFinished Type = "agent_finished"
	// TypeReAuthenticate indicates the agent encountered an
	// authentication error and the user needs to re-authenticate.
	TypeReAuthenticate Type = "re_authenticate"
	// TypeActivityUpdate indicates the agent started a new activity
	// (tool execution, thinking, etc.) during processing.
	TypeActivityUpdate Type = "activity_update"
	// TypeAgentResponded carries the final assistant message content
	// after the agent finishes processing. Used by Telegram mirror
	// as a reliable delivery path for the final response.
	TypeAgentResponded Type = "agent_responded"
	// TypeAgentError indicates the agent turn ended with an error.
	// Activity carries the error message.
	TypeAgentError Type = "agent_error"
	// TypeToolError indicates a tool execution produced an error result.
	// Activity carries the error message prefixed with ❌.
	TypeToolError Type = "tool_error"
	// TypeToolOutput carries a successful tool result's text output.
	// Activity carries the (truncated) output. Consumers gate on verbosity.
	TypeToolOutput Type = "tool_output"
	// TypeReasoning carries the model's reasoning/thinking text for a turn.
	// Activity carries the reasoning text. Consumers gate on verbosity.
	TypeReasoning Type = "reasoning"
	// TypeStreamDelta carries the accumulated assistant text so far during
	// streaming. Consumers throttle and edit a status message in place.
	TypeStreamDelta Type = "stream_delta"
)

// Notification represents a domain event published by the agent.
type Notification struct {
	SessionID    string
	SessionTitle string
	Type         Type
	ProviderID   string
	Activity     string // populated for TypeActivityUpdate
}
