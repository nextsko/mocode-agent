package tools

import (
	"context"
	"encoding/json"
	"strings"
)

// Capability tags a tool with what it can do (fs.read, fs.write, net.fetch, ...).
// Empty list = tool advertises no capabilities.
type Capability string

// Attachment is the toolkit's framework-agnostic file/asset payload.
// It is intentionally defined here (not imported from internal/domain/session/message)
// because that package transitively imports charm.land/fantasy and charm.land/catwalk,
// which would violate the toolkit's framework-agnostic invariant. The agent
// package's adapter (agenttool_adapter.go) is responsible for converting
// between tools.Attachment and message.Attachment at the LLM-runtime boundary.
type Attachment struct {
	FilePath string
	FileName string
	MimeType string
	Content  []byte
}

// IsText reports whether the attachment's MIME type is a text/* type.
func (a Attachment) IsText() bool { return strings.HasPrefix(a.MimeType, "text/") }

// IsImage reports whether the attachment's MIME type is an image/* type.
func (a Attachment) IsImage() bool { return strings.HasPrefix(a.MimeType, "image/") }

// Tool is the contract every LLM-callable tool implements.
//
// Implementations MUST be safe for concurrent Execute calls across goroutines
// if the tool is registered more than once or shared across sessions.
type Tool interface {
	Name() string
	Description() string
	Schema() Schema
	Execute(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error)
}

// Capable is an optional interface a Tool may implement to advertise what
// it can do. Tools that do not implement Capable advertise no capabilities.
type Capable interface {
	Capabilities() []Capability
}

// ToolProvider groups related tools behind a named bundle. Providers
// (e.g. the MCP bridge) may return a fresh slice from Tools() each call
// to reflect live state; static providers may return a stable slice.
type ToolProvider interface {
	Name() string
	Tools() []Tool
}

// ToolContext is the request-scoped runtime handle passed to Execute.
// A fresh value is constructed for each Execute call. Implementations
// MUST NOT share mutable state across calls.
type ToolContext interface {
	SessionID() string
	WorkingDir() string
	Permissions() PermissionChecker
	MCP() MCPHandles
	Callbacks() ToolCallbacks
}

// PermissionChecker is consulted before Execute runs a tool. Allow(name)
// returns true if the tool call is permitted in the current session.
type PermissionChecker interface {
	Allow(name string) bool
}

// MCPHandles exposes connected MCP server state to tools that need it.
type MCPHandles interface {
	Servers() []MCPServerHandle
}

// MCPServerHandle is a read-only handle to one connected MCP server.
type MCPServerHandle struct {
	Name  string
	State string // "connected" | "disconnected" | ...
	Tools []Tool // tools this server provides, may be nil
}

// ToolCallbacks lets tools hook into the agent runtime (telemetry, audit
// log, before/after notifications). Implementations may be no-op.
type ToolCallbacks interface {
	Before(name string, args json.RawMessage)
	After(name string, result ToolResult, err error)
}

// Schema describes a tool's argument shape in a framework-agnostic way.
// Parameters is a JSON-Schema-shaped map[string]any; the agent's adapter
// converts it to the LLM runtime's expected format.
type Schema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToolResult is the structured output returned by Execute. When Error is
// non-nil, the agent runtime treats the call as failed and surfaces the
// error to the LLM, suppressing Content.
type ToolResult struct {
	Content     string
	Error       error
	Attachments []Attachment
	Metadata    map[string]any
}
