package agenttools

import (
	"context"
	_ "embed"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
)

type DiagnosticsParams struct {
	FilePath string `json:"file_path,omitempty" description:"The path to the file to get diagnostics for (leave empty for project diagnostics)"`
}

const DiagnosticsToolName = "lsp_diagnostics"

//go:embed diagnostics.md
var diagnosticsDescription []byte

// NewDiagnosticsTool is the legacy fantasy-typed constructor; new code should
// use NewDiagnosticsPortTool and wrap it at the agent boundary.
func NewDiagnosticsTool(lspManager *lsp.Manager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		DiagnosticsToolName,
		toolutil.FirstLineDescription(diagnosticsDescription),
		func(ctx context.Context, params DiagnosticsParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			out, err := runDiagnostics(ctx, lspManager, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.NewTextResponse(out), nil
		})
}
