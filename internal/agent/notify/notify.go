// Package notify defines domain notification types for agent events.
// These types are decoupled from UI concerns so the agent can publish
// events without importing UI packages.
package notify

// Type identifies the kind of agent notification.
type Type string

const (
	// TypeAgentThinking indicates the agent is processing/thinking.
	TypeAgentThinking Type = "agent_thinking"
	// TypeAgentToolExecuting indicates the agent is executing a tool.
	TypeAgentToolExecuting Type = "agent_tool_executing"
	// TypeAgentFinished indicates the agent has completed its turn.
	TypeAgentFinished Type = "agent_finished"
	// TypeReAuthenticate indicates the agent encountered an
	// authentication error and the user needs to re-authenticate.
	TypeReAuthenticate Type = "re_authenticate"
	// TypeRoundtableTurn indicates a roundtable meeting has completed
	// one participant turn.
	TypeRoundtableTurn Type = "roundtable_turn"
	// TypeRoundtableFinished indicates a roundtable meeting has ended.
	TypeRoundtableFinished Type = "roundtable_finished"
	// TypeSubagentCompleted indicates a sub-agent dispatched by the
	// Agent tool has finished. It carries a SubagentCompleted payload
	// describing the result so the web UI can refresh the parent tool
	// card in place.
	TypeSubagentCompleted Type = "subagent_completed"
)

// Notification represents a domain event published by the agent.
type Notification struct {
	SessionID    string
	SessionTitle string
	Type         Type
	ProviderID   string
	ToolName     string // Name of the tool being executed (for TypeAgentToolExecuting)

	// SubagentCompleted is set when Type == TypeSubagentCompleted and
	// describes the sub-agent run that just finished.
	SubagentCompleted *SubagentCompletedEvent
}

// SubagentStatus is the terminal status of a sub-agent run.
type SubagentStatus string

const (
	// SubagentStatusSuccess indicates the sub-agent completed without error.
	SubagentStatusSuccess SubagentStatus = "success"
	// SubagentStatusError indicates the sub-agent failed with an error.
	SubagentStatusError SubagentStatus = "error"
	// SubagentStatusCancelled indicates the sub-agent was cancelled.
	SubagentStatusCancelled SubagentStatus = "cancelled"
	// SubagentStatusBlocked indicates the sub-agent was blocked because a
	// dependency (in DAG mode) failed or was cancelled.
	SubagentStatusBlocked SubagentStatus = "blocked"
)

// SubagentTokenUsage summarises token consumption of a sub-agent run.
type SubagentTokenUsage struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
	Total         int64 `json:"total"`
}

// SubagentCompletedEvent is the payload for TypeSubagentCompleted.
type SubagentCompletedEvent struct {
	// ParentSessionID is the session that dispatched the sub-agent.
	ParentSessionID string `json:"parent_session_id"`
	// ParentToolCallID is the Agent tool call ID that owns the sub-agent.
	ParentToolCallID string `json:"parent_tool_call_id"`
	// AgentID is the optional sub-agent instance identifier.
	AgentID string `json:"agent_id,omitempty"`
	// SubagentType is the built-in type label (e.g. "coder" / "explore"
	// / "plan") or a custom agent name.
	SubagentType string `json:"subagent_type,omitempty"`
	// Status is the terminal status of the run.
	Status SubagentStatus `json:"status"`
	// DurationMs is the wall-clock duration of the sub-agent run.
	DurationMs int64 `json:"duration_ms"`
	// Usage is the aggregated token usage of the run.
	Usage SubagentTokenUsage `json:"usage"`
	// Summary is a short, human-readable summary of the result.
	Summary string `json:"summary,omitempty"`
	// Error is set when Status == SubagentStatusError or
	// SubagentStatusCancelled.
	Error string `json:"error,omitempty"`
}
