package types

// ToolResult represents the result of a tool execution as a public DTO.
//
// This is the cross-module DTO used for public APIs and SDK consumers.
// It contains 9 fields.
//
// For the RUNTIME type used in Session message storage, see
// message.ToolResult (in internal/session/message).
// Field semantics:
//   - ToolCallID: identifier matching the originating ToolCall.ID
//   - Name: tool name (for display/lookup)
//   - AgentName: sub-agent name when delegated (empty for direct calls)
//   - Type: result media type hint (text/image/media)
//   - Content: human-readable textual content
//   - Data: base64-encoded binary payload (e.g. images)
//   - MIMEType: MIME type for Data
//   - Metadata: arbitrary JSON string for tool-specific metadata
//   - IsError: true if the tool call failed
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
	Type       string `json:"type,omitempty"`
	Content    string `json:"content"`
	Data       string `json:"data,omitempty"`
	MIMEType   string `json:"mime_type,omitempty"`
	Metadata   string `json:"metadata,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}
