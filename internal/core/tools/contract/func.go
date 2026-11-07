package contract

import (
	"context"
	"encoding/json"
)

// Func builds a Tool from a function. It exists so migrating a builtin tool to
// the framework-agnostic port is a small, mechanical edit rather than a new
// type per tool.
type Func struct {
	ToolName        string
	ToolDescription string
	Parameters      map[string]any
	Fn              func(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error)
}

// Name implements Tool.
func (f *Func) Name() string { return f.ToolName }

// Description implements Tool.
func (f *Func) Description() string { return f.ToolDescription }

// Schema implements Tool.
func (f *Func) Schema() Schema {
	return Schema{Name: f.ToolName, Description: f.ToolDescription, Parameters: f.Parameters}
}

// Execute implements Tool.
func (f *Func) Execute(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error) {
	return f.Fn(ctx, tctx, args)
}

// NewFunc returns a Tool backed by fn.
func NewFunc(
	name string,
	description string,
	parameters map[string]any,
	fn func(ctx context.Context, tctx ToolContext, args json.RawMessage) (ToolResult, error),
) Tool {
	return &Func{ToolName: name, ToolDescription: description, Parameters: parameters, Fn: fn}
}
