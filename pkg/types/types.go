// Package types re-exports the public type definitions from internal/types.
//
// This is the public API surface for external consumers (e.g., SDK clients,
// plugins, third-party tools) that want to interact with mocode types without
// depending on internal packages.
//
// Usage:
//
//	import "github.com/package-register/mocode/pkg/types"
//
//	r := types.ToolResult{...}
package types

import internal "github.com/package-register/mocode/internal/types"

// ToolResult is the unified result of a tool execution.
type ToolResult = internal.ToolResult
