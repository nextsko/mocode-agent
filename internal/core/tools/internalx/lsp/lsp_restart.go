package lsp

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

const LSPRestartToolName = "lsp_restart"

//go:embed lsp_restart.md
var lspRestartDescription []byte

type LSPRestartParams struct {
	// Name is the optional name of a specific LSP client to restart.
	// If empty, all LSP clients will be restarted.
	Name string `json:"name,omitempty"`
}

// NewLSPRestartPortTool is the framework-agnostic form of the lsp_restart tool.
func NewLSPRestartPortTool(lspManager *Manager) contract.Tool {
	return contract.NewFunc(
		LSPRestartToolName,
		toolutil.FirstLineDescription(lspRestartDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type": "string",
				},
			},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params LSPRestartParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runLSPRestart(lspManager, params)
		},
	)
}

// runLSPRestart restarts one named LSP client or every connected client.
func runLSPRestart(lspManager *Manager, params LSPRestartParams) (contract.ToolResult, error) {
	if lspManager.Clients().Len() == 0 {
		return contract.Error("no LSP clients available to restart"), nil
	}

	clientsToRestart := make(map[string]*Client)
	if params.Name == "" {
		maps.Insert(clientsToRestart, lspManager.Clients().Seq2())
	} else {
		client, exists := lspManager.Clients().Get(params.Name)
		if !exists {
			return contract.Error(fmt.Sprintf("LSP client '%s' not found", params.Name)), nil
		}
		clientsToRestart[params.Name] = client
	}

	var restarted []string
	var failed []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, client := range clientsToRestart {
		wg.Go(func() {
			if err := client.Restart(); err != nil {
				slog.Error("Failed to restart LSP client", "name", name, "error", err)
				mu.Lock()
				failed = append(failed, name)
				mu.Unlock()
				return
			}
			mu.Lock()
			restarted = append(restarted, name)
			mu.Unlock()
		})
	}

	wg.Wait()

	var output string
	if len(restarted) > 0 {
		output = fmt.Sprintf("Successfully restarted %d LSP client(s): %s\n", len(restarted), strings.Join(restarted, ", "))
	}
	if len(failed) > 0 {
		output += fmt.Sprintf("Failed to restart %d LSP client(s): %s\n", len(failed), strings.Join(failed, ", "))
		return contract.Error(output), nil
	}

	return contract.Text(output), nil
}
