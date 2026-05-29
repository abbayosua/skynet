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
)

// Notification represents a domain event published by the agent.
type Notification struct {
	SessionID    string
	SessionTitle string
	Type         Type
	ProviderID   string
	Activity     string // populated for TypeActivityUpdate
}
