package web

import "github.com/nextsko/mocode-agent/internal/core/tools/contract"

// parallelPort marks a port as safe to execute concurrently with other tools.
// Every tool in this package used to be built with fantasy.NewParallelAgentTool;
// the agent adapter reads contract.ParallelTool and reports it as
// fantasy.ToolInfo.Parallel, so wrapping the port keeps the runtime's
// scheduling behaviour unchanged.
type parallelPort struct{ contract.Tool }

// Parallel implements contract.ParallelTool.
func (parallelPort) Parallel() bool { return true }

// asParallel wraps t so the agent runtime may run it concurrently.
func asParallel(t contract.Tool) contract.Tool { return parallelPort{Tool: t} }

// permissionDeniedResult is the port form of toolutil.NewPermissionDeniedResponse:
// an error result that also asks the agent loop to stop the turn.
func permissionDeniedResult() contract.ToolResult {
	res := contract.Error("User denied permission")
	res.StopTurn = true
	return res
}
