// Package types contains cross-module shared type definitions.
//
// # ToolResult 三层架构
//
// mocode 项目中存在三种不同的 ToolResult 类型，各自服务于不同场景：
//
//  1. session/message.ToolResult — 运行时类型，实现 ContentPart 接口，
//     用于 Session 消息存储。是业务代码中 ~80+ 处 type switch 的目标类型。
//
//  2. proto.ToolResult — 序列化类型，实现 ContentPart 接口，
//     用于 WebSocket/JSON 线协议序列化。包含完整的 8 字段。
//
//  3. types.ToolResult（本包）— 纯 DTO，跨模块传输用，
//     用于公开 API、SDK、插件、第三方工具。
//
// 三种类型**不能**用 type alias 统一，因为 ContentPart 接口是 sealed
// interface（依赖未导出方法 isPart()），Go 语言限制私有方法无法跨包实现。
// 强行 alias 会破坏 ~80+ 处 type switch 逻辑。
//
// 跨包转换使用 proto 包提供的转换函数：
//   - proto.ToMessageToolResult(p proto.ToolResult) message.ToolResult
//   - proto.FromMessageToolResult(m message.ToolResult) proto.ToolResult
//
// 这两个函数保留全部 8 个字段（修复了之前手动复制只复制 4 字段的 bug）。
//
// 公开 API 层：pkg/types.ToolResult 是本包类型的 type alias，
// 外部代码应 import pkg/types 而非 internal/types。
package types

// ToolResult represents the result of a tool execution as a public DTO.
//
// This is the cross-module DTO used for public APIs and SDK consumers.
// It contains 9 fields (see pkg/types for the public re-export).
//
// For the RUNTIME type used in Session message storage, see
// message.ToolResult (in internal/session/message).
// For the SERIALIZATION type used in wire protocol, see
// proto.ToolResult (in internal/proto).
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
