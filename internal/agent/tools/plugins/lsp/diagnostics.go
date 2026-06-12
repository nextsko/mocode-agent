package lsp

import (
	"context"
	_ "embed"

	"charm.land/fantasy"
	lsputil "github.com/package-register/mocode/internal/agent/toolutil/lsputil"
	"github.com/package-register/mocode/internal/agent/toolutil/shared"
	"github.com/package-register/mocode/internal/lsp"
)

type DiagnosticsParams struct {
	FilePath string `json:"file_path,omitempty" description:"The path to the file to get diagnostics for (leave empty for project diagnostics)"`
}

const DiagnosticsToolName = "lsp_diagnostics"

//go:embed diagnostics.md
var diagnosticsDescription []byte

func NewDiagnosticsTool(lspManager *lsp.Manager) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		DiagnosticsToolName,
		shared.FirstLineDescription(diagnosticsDescription),
		func(ctx context.Context, params DiagnosticsParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if lspManager.Clients().Len() == 0 {
				return fantasy.NewTextErrorResponse("no LSP clients available"), nil
			}
			lsputil.NotifyLSPs(ctx, lspManager, params.FilePath)
			output := lsputil.GetDiagnostics(params.FilePath, lspManager)
			return fantasy.NewTextResponse(output), nil
		})
}
