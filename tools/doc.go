// Package tools is the framework-agnostic toolkit that hosts every
// LLM-callable tool in the mocode runtime. It is the new home for the
// 40+ built-in, plugin, MCP, and LSP tools that today live under
// internal/core/agent/tools, and it exposes them through a small,
// stable contract so the agent runtime can consume tools by name
// without depending on any particular LLM runtime.
//
// # Contract (forward reference)
//
// The public contract of this package is introduced in the next
// sub-tasks of the refactor (1.2 onward). It will define five core
// abstractions:
//
//   - Tool, the interface every LLM-callable tool implements.
//   - ToolProvider, a named bundle of related tools.
//   - ToolContext, the request-scoped runtime handle passed to Execute.
//   - ToolResult, the structured output returned from Execute.
//   - Schema, a framework-agnostic description of a tool's argument shape.
//
// This doc records their shape today so reviewers and downstream
// packages can reason about the surface without waiting for the
// follow-up commits. The concrete declarations will live in
// contracts.go alongside this package.
//
// # Framework-agnostic invariant
//
// This package, including every subpackage (builtin/, plugins/,
// mcp/, lsp/, filter/), MUST NOT import:
//
//   - charm.land/fantasy or any subpath thereof
//   - charm.land/catwalk
//   - internal/core/agent or any subpath
//   - internal/core/config or any subpath
//   - any future LLM runtime library (for example trpc-agent-go)
//
// All coupling to the LLM runtime is concentrated in exactly one
// file, internal/core/agent/agenttool_adapter.go, so a future kernel
// swap touches one file rather than 40+ tools. A CI dependency-boundary
// check enforces the rule; any forbidden import in this tree fails the
// build.
//
// # Adding a new tool
//
// The intended usage shape, once the contracts land, is:
//
//	package mytool
//
//	import (
//		"context"
//		"encoding/json"
//
//		"github.com/package-register/mocode/tools"
//	)
//
//	type myTool struct{ /* unexported fields */ }
//
//	func New() tools.Tool { return &myTool{} }
//
//	func (t *myTool) Name() string                { return "my_tool" }
//	func (t *myTool) Description() string         { return "Short summary sent to the LLM." }
//	func (t *myTool) Schema() tools.Schema        { return tools.Schema{ /* ... */ } }
//	func (t *myTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
//		// tool implementation
//	}
//
//	func init() { tools.Register("my_tool", New()) }
//
// Consumers opt in by blank-importing the umbrella packages that
// aggregate builtins, plugins, and bridges:
//
//	import (
//		_ "github.com/package-register/mocode/tools/builtin/all"
//		_ "github.com/package-register/mocode/tools/plugins/all"
//	)
//
// # Module
//
// This package lives in the same Go module as the agent runtime;
// the import path below is the only supported entry point.
package tools // import "github.com/package-register/mocode/tools"
