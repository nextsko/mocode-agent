// Package types contains cross-module shared type definitions.
//
// types.ToolResult is a unified DTO (Data Transfer Object) for tool execution
// results. It is intentionally decoupled from the ContentPart interface in
// internal/session/message so it can be used by external consumers (SDK,
// plugins, WebSocket clients) without depending on the internal message system.
//
// Usage notes:
//   - For messages stored in a Session, use message.ToolResult (implements
//     ContentPart).
//   - For wire protocol serialization, use proto.ToolResult (implements
//     ContentPart with custom JSON).
//   - For public APIs and SDK consumers, use types.ToolResult (this type).
package types

// ToolResult represents the result of a tool execution.
//
// This is the unified DTO used for public APIs and cross-module data exchange.
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
