// Package types is the public API surface for cross-module type definitions
// in mocode. It re-exports the core DTO types from internal/types via Go
// type aliases, allowing external consumers (SDK clients, plugins, third-party
// tools) to interact with mocode types without depending on internal packages.
//
// The re-exports are TYPE ALIASES (not new definitions), meaning the public
// types are 100% identical to their internal counterparts at compile time.
// No conversion is needed when crossing the public/private boundary.
//
// Layering model in the mocode project:
//
//	internal/session/message.ToolResult  — implements ContentPart (for messages)
//	internal/proto.ToolResult            — implements ContentPart (for wire format)
//	internal/types.ToolResult            — pure DTO (for cross-module exchange)
//	pkg/types.ToolResult                 — public re-export of internal/types.ToolResult
//
// Use this package when you need a stable, public type for SDK or plugin
// development. Use internal/* packages when working within the mocode codebase
// and need the full ContentPart integration.
//
// Example (external consumer):
//
//	import "github.com/package-register/mocode/pkg/types"
//
//	func handle(r types.ToolResult) error {
//	    if r.IsError {
//	        return fmt.Errorf("tool %s failed: %s", r.Name, r.Content)
//	    }
//	    return nil
//	}
package types

import internal "github.com/package-register/mocode/internal/types"

// ToolResult is the public, cross-module DTO for tool execution results.
// See internal/types.ToolResult for field documentation.
type ToolResult = internal.ToolResult
