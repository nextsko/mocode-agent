package ssh

import (
	"errors"

	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// permissionDenied is the port form of the old permission-denied response: a
// tool-level error that also ends the turn. Tools must not construct fantasy
// responses themselves; the adapter owns that boundary.
func permissionDenied() contract.ToolResult {
	res := contract.ToolResult{Error: errors.New("User denied permission")} //nolint:staticcheck // user-facing denial text matches the legacy prompt.
	res.StopTurn = true
	return res
}
