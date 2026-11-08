// Package agenttooladapter bridges the framework-agnostic contract.Tool port to
// the fantasy.AgentTool the agent runtime calls.
//
// It is the consolidation point promised in core/tools/doc.go: production tools
// migrate to contract.Tool one at a time, and this adapter wraps each migrated
// tool at the agent boundary. Tools that still return fantasy.AgentTool keep
// working unchanged until they are ported.
package agenttooladapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// Adapter wraps one contract.Tool as a fantasy.AgentTool.
type Adapter struct {
	tool contract.Tool
	// tctx is the runtime context handed to every Execute call. It may be nil
	// while the coordinator wiring is still in progress; in that case a
	// no-op context is supplied so context-free tools work safely.
	tctx contract.ToolContext
	// newCtx, when set, produces a fresh context per Execute call (the correct
	// shape for request-scoped state). It takes precedence over tctx.
	newCtx func(ctx context.Context) (contract.ToolContext, func())
	opts   fantasy.ProviderOptions
}

// New creates an adapter for tool. Pass nil tctx for context-free tools; pass
// a real contract.ToolContext once the coordinator wires per-call state.
func New(tool contract.Tool, tctx contract.ToolContext) *Adapter {
	return &Adapter{tool: tool, tctx: tctx}
}

// NewWithContext creates an adapter that builds a fresh contract.ToolContext for
// every Execute call. The returned cleanup func is invoked after the call.
func NewWithContext(tool contract.Tool, factory func(ctx context.Context) (contract.ToolContext, func())) *Adapter {
	return &Adapter{tool: tool, newCtx: factory}
}

// Info implements fantasy.AgentTool.
func (a *Adapter) Info() fantasy.ToolInfo {
	s := a.tool.Schema()
	params := s.Parameters
	if params == nil {
		params = map[string]any{}
	}
	required := requiredFields(params)
	params = flattenProperties(params)
	parallel := false
	if p, ok := a.tool.(contract.ParallelTool); ok {
		parallel = p.Parallel()
	}
	return fantasy.ToolInfo{
		Name:        firstNonEmpty(s.Name, a.tool.Name()),
		Description: firstNonEmpty(s.Description, a.tool.Description()),
		Parameters:  params,
		Required:    required,
		Parallel:    parallel,
	}
}

// Run implements fantasy.AgentTool.
func (a *Adapter) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	// The runtime passes the tool call id on the ToolCall; tools read it from
	// the context (permission prompts, telemetry), so inject it before Execute.
	if call.ID != "" {
		ctx = toolutil.WithToolCallID(ctx, call.ID)
	}
	tctx := a.tctx
	cleanup := func() {}
	if a.newCtx != nil {
		tctx, cleanup = a.newCtx(ctx)
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
		errResp := fantasy.NewTextErrorResponse(result.Error.Error())
		errResp.StopTurn = result.StopTurn
		return errResp, nil
	}

	resp := fantasy.NewTextResponse(result.Content)
	// The runtime carries at most one binary payload per tool response. A
	// single image attachment maps to an image response, other binary payloads
	// (audio/video/PDF from MCP servers) map to a media response; additional
	// attachments are reported in the text so nothing is silently dropped.
	if len(result.Attachments) > 0 && len(result.Attachments[0].Content) > 0 && result.Attachments[0].IsImage() {
		resp = fantasy.NewImageResponse(result.Attachments[0].Content, result.Attachments[0].MimeType)
		if len(result.Attachments) > 1 {
			resp.Content = fmt.Sprintf("%s\n(%d more attachments omitted)", result.Content, len(result.Attachments)-1)
		}
	} else if len(result.Attachments) > 0 && len(result.Attachments[0].Content) > 0 && result.Attachments[0].MimeType != "" {
		resp = fantasy.NewMediaResponse(result.Attachments[0].Content, result.Attachments[0].MimeType)
		resp.Content = result.Content
		if len(result.Attachments) > 1 {
			resp.Content = fmt.Sprintf("%s\n(%d more attachments omitted)", result.Content, len(result.Attachments)-1)
		}
	} else if len(result.Attachments) > 0 {
		resp.Content = fmt.Sprintf("%s\n(%d attachment(s) omitted)", result.Content, len(result.Attachments))
	}

	if result.StopTurn {
		resp.StopTurn = true
	}
	if len(result.Metadata) > 0 {
		payload := any(result.Metadata)
		if len(result.Metadata) == 1 {
			if inner, ok := result.Metadata["result"]; ok {
				payload = inner
			}
		}
		if b, err := json.Marshal(payload); err == nil {
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

// flattenProperties matches fantasy's ToolInfo.Parameters shape: a flat map of
// property name → property schema, with enum slices converted to []any so
// equality assertions and JSON encoding behave the same as generated schemas.
func flattenProperties(params map[string]any) map[string]any {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		return params
	}
	out := make(map[string]any, len(props))
	for name, raw := range props {
		spec, ok := raw.(map[string]any)
		if !ok {
			out[name] = raw
			continue
		}
		if enum, ok := spec["enum"].([]string); ok {
			values := make([]any, 0, len(enum))
			for _, v := range enum {
				values = append(values, v)
			}
			clone := make(map[string]any, len(spec))
			for k, v := range spec {
				clone[k] = v
			}
			clone["enum"] = values
			out[name] = clone
			continue
		}
		out[name] = spec
	}
	return out
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

// noContext is the placeholder contract.ToolContext used until the coordinator
// supplies per-call state. It never grants permissions and carries no MCP or
// callback handles, so a tool that needs them fails closed.
type noContext struct{}

func (noContext) SessionID() string                       { return "" }
func (noContext) WorkingDir() string                      { return "" }
func (noContext) Permissions() contract.PermissionChecker { return denyPermissions{} }
func (noContext) MCP() contract.MCPHandles                { return nopMCP{} }
func (noContext) Callbacks() contract.ToolCallbacks       { return nopCallbacks{} }

type denyPermissions struct{}

func (denyPermissions) Allow(string) bool { return false }

type nopMCP struct{}

func (nopMCP) Servers() []contract.MCPServerHandle { return nil }

type nopCallbacks struct{}

func (nopCallbacks) Before(string, json.RawMessage)           {}
func (nopCallbacks) After(string, contract.ToolResult, error) {}

// fantasyTool adapts a legacy fantasy.AgentTool to contract.Tool. It exists so
// framework-agnostic filters (and dynamic MCP tools) can be handled uniformly;
// new tools should be written as contract.Tool and wrapped with New instead.
type fantasyTool struct{ inner fantasy.AgentTool }

// WrapFantasy adapts a fantasy.AgentTool to contract.Tool. Text responses are
// preserved; metadata is dropped because the runtime already consumed it.
func WrapFantasy(t fantasy.AgentTool) contract.Tool { return fantasyTool{inner: t} }

func (w fantasyTool) Name() string        { return w.inner.Info().Name }
func (w fantasyTool) Description() string { return w.inner.Info().Description }

func (w fantasyTool) Schema() contract.Schema {
	info := w.inner.Info()
	return contract.Schema{
		Name:        info.Name,
		Description: info.Description,
		Parameters:  info.Parameters,
	}
}

func (w fantasyTool) Execute(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
	resp, err := w.inner.Run(ctx, fantasy.ToolCall{Input: string(args)})
	if err != nil {
		return contract.ToolResult{}, err
	}
	res := contract.ToolResult{Content: resp.Content, StopTurn: resp.StopTurn}
	if resp.IsError {
		res.Error = errors.New(resp.Content)
	}
	return res, nil
}
