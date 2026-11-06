package tools

import "github.com/nextsko/mocode-agent/internal/core/tools/contract"

// The toolkit's contract types live in the leaf package "contract" so that
// tool implementations (which this package imports through the registry) can
// reference them without an import cycle. These aliases keep the public
// "tools.X" names stable for the rest of the codebase.
type (
	Capability        = contract.Capability
	Attachment        = contract.Attachment
	Tool              = contract.Tool
	Capable           = contract.Capable
	ToolProvider      = contract.ToolProvider
	ToolContext       = contract.ToolContext
	PermissionChecker = contract.PermissionChecker
	MCPHandles        = contract.MCPHandles
	MCPServerHandle   = contract.MCPServerHandle
	ToolCallbacks     = contract.ToolCallbacks
	Schema            = contract.Schema
	ToolResult        = contract.ToolResult
)
