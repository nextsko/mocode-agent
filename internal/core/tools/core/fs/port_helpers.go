package fs

import "github.com/nextsko/mocode-agent/internal/core/tools/contract"

// permissionDeniedResult is the framework-free form of
// toolutil.NewPermissionDeniedResponse: a tool error that also asks the agent
// runtime to end the turn.
func permissionDeniedResult() contract.ToolResult {
	res := contract.Error("User denied permission")
	res.StopTurn = true
	return res
}

// parallelPort tags a port tool as safe to run in parallel with other tools.
// The agent adapter surfaces it through fantasy.ToolInfo.Parallel, preserving
// the old fantasy.NewParallelAgentTool registration.
type parallelPort struct{ contract.Tool }

// Parallel implements contract.ParallelTool.
func (parallelPort) Parallel() bool { return true }
