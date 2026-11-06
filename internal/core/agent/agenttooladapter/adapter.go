// Package agenttooladapter bridges the framework-agnostic tools.Tool port to
// the fantasy.AgentTool the agent runtime calls.
//
// It is the consolidation point promised in core/tools/doc.go: production tools
// migrate to tools.Tool one at a time, and this adapter wraps each migrated
// tool at the agent boundary. Tools that still return fantasy.AgentTool keep
// working unchanged until they are ported.
package agenttooladapter

import (
	"context"
	"encoding/json"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/tools"
)

// Adapter wraps one tools.Tool as a fantasy.AgentTool.
type Adapter struct {
	tool tools.Tool
	// tctx is the runtime context handed to every Execute call. It may be nil
	// while the coordinator wiring is still in progress; in that case a
	// no-op context is supplied so context-free tools work safely.
	tctx tools.ToolContext
	// newCtx, when set, produces a fresh context per Execute call (the correct
	// shape for request-scoped state). It takes precedence over tctx.
	newCtx func() (tools.ToolContext, func())
	opts   fantasy.ProviderOptions
}

// New creates an adapter for tool. Pass nil tctx for context-free tools; pass
// a real tools.ToolContext once the coordinator wires per-call state.
func New(tool tools.Tool, tctx tools.ToolContext) *Adapter {
	return &Adapter{tool: tool, tctx: tctx}
}

// NewWithContext creates an adapter that builds a fresh tools.ToolContext for
// every Execute call. The returned cleanup func is invoked after the call.
func NewWithContext(tool tools.Tool, factory func() (tools.ToolContext, func())) *Adapter {
	return &Adapter{tool: tool, newCtx: factory}
}

// Info implements fantasy.AgentTool.
func (a *Adapter) Info() fantasy.ToolInfo {
	s := a.tool.Schema()
	params := s.Parameters
	if params == nil {
		params = map[string]any{}
	}
	return fantasy.ToolInfo{
		Name:        firstNonEmpty(s.Name, a.tool.Name()),
		Description: firstNonEmpty(s.Description, a.tool.Description()),
		Parameters:  params,
		Required:    requiredFields(params),
	}
}

// Run implements fantasy.AgentTool.
func (a *Adapter) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	tctx := a.tctx
	cleanup := func() {}
	if a.newCtx != nil {
		tctx, cleanup = a.newCtx()
	}
	defer cleanup()
	if tctx == nil {
		tctx = noContext{}
	}

	result, err := a.tool.Execute(ctx, tctx, json.RawMessage(call.Input))
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if result.Error != nil {
		return fantasy.NewTextErrorResponse(result.Error.Error()), nil
	}

	resp := fantasy.NewTextResponse(result.Content)
	// The runtime carries at most one binary payload per tool response. A
	// single image attachment maps to an image response; additional
	// attachments are reported in the text so nothing is silently dropped.
	if len(result.Attachments) > 0 && len(result.Attachments[0].Content) > 0 && result.Attachments[0].IsImage() {
		resp = fantasy.NewImageResponse(result.Attachments[0].Content, result.Attachments[0].MimeType)
		if len(result.Attachments) > 1 {
			resp.Content = fmt.Sprintf("%s\n(%d more attachments omitted)", result.Content, len(result.Attachments)-1)
		}
	} else if len(result.Attachments) > 0 {
		resp.Content = fmt.Sprintf("%s\n(%d attachment(s) omitted)", result.Content, len(result.Attachments))
	}

	if len(result.Metadata) > 0 {
		if b, err := json.Marshal(result.Metadata); err == nil {
			resp.Metadata = string(b)
		}
	}
	return resp, nil
}

// ProviderOptions implements fantasy.AgentTool.
func (a *Adapter) ProviderOptions() fantasy.ProviderOptions {
	return a.opts
}

// SetProviderOptions implements fantasy.AgentTool.
func (a *Adapter) SetProviderOptions(opts fantasy.ProviderOptions) {
	a.opts = opts
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// requiredFields reads the required list out of a JSON-Schema-shaped map.
func requiredFields(params map[string]any) []string {
	raw, ok := params["required"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// noContext is the placeholder tools.ToolContext used until the coordinator
// supplies per-call state. It never grants permissions and carries no MCP or
// callback handles, so a tool that needs them fails closed.
type noContext struct{}

func (noContext) SessionID() string                    { return "" }
func (noContext) WorkingDir() string                   { return "" }
func (noContext) Permissions() tools.PermissionChecker { return denyPermissions{} }
func (noContext) MCP() tools.MCPHandles                { return nopMCP{} }
func (noContext) Callbacks() tools.ToolCallbacks       { return nopCallbacks{} }

type denyPermissions struct{}

func (denyPermissions) Allow(string) bool { return false }

type nopMCP struct{}

func (nopMCP) Servers() []tools.MCPServerHandle { return nil }

type nopCallbacks struct{}

func (nopCallbacks) Before(string, json.RawMessage)        {}
func (nopCallbacks) After(string, tools.ToolResult, error) {}
