// Package types is the public API surface for cross-module type definitions
// in mocode. It re-exports the core DTO types from internal/types via Go
// type aliases, allowing external consumers (SDK clients, plugins, third-party
// tools) to interact with mocode types without depending on internal packages.
//
// The re-exports are TYPE ALIASES (not new definitions), meaning the public
// types are 100% identical to their internal counterparts at compile time.
// No conversion is needed when crossing the public/private boundary.
//
// # When to Use This Package
//
// Use pkg/types (this package) for:
//
//   - SDK development
//   - Plugin development
//   - External tools that need to interact with mocode
//   - HTTP/WebSocket client code
//
// Use internal/* packages (within the mocode codebase) for:
//
//   - Direct integration with the message system
//   - Code that needs ContentPart or other sealed interfaces
//   - Any code that already imports internal/ packages
//
// # Currently Exports
//
//   - ToolResult — unified tool execution result DTO
//
// See internal/types/doc.go for the full decision tree on when to add
// new types to this layer.
//
// # Example (external consumer)
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
