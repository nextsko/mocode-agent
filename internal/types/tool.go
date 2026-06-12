package types

// ToolResult is the unified result of a tool execution.
// This consolidates the three previous definitions that existed in
// session/message, proto, and agent/tools/mcp.
type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name,omitempty"`
	AgentName  string `json:"agent_name,omitempty"`
	Type       string `json:"type,omitempty"` // text/image/media
	Content    string `json:"content"`
	Data       string `json:"data,omitempty"` // base64 encoded
	MIMEType   string `json:"mime_type,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}
