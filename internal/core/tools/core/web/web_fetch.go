package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	netcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/net"
)

//go:embed web_fetch.md
var webFetchToolDescription []byte

// NewWebFetchPortTool creates a simple web fetch port for sub-agents (no
// permissions needed).
func NewWebFetchPortTool(workingDir string, client *http.Client) contract.Tool {
	if client == nil {
		client = netcommon.DefaultHTTPClient()
	}

	return asParallel(contract.NewFunc(
		netcommon.WebFetchToolName,
		toolutil.FirstLineDescription(webFetchToolDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
			},
			"required": []string{"url"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params netcommon.WebFetchParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runWebFetch(ctx, workingDir, client, params)
		},
	))
}

// runWebFetch fetches a page as Markdown, spilling oversized pages to a file.
func runWebFetch(ctx context.Context, workingDir string, client *http.Client, params netcommon.WebFetchParams) (contract.ToolResult, error) {
	if params.URL == "" {
		return contract.Error("url is required"), nil
	}

	content, err := netcommon.FetchURLAndConvert(ctx, client, params.URL)
	if err != nil {
		return contract.Error(fmt.Sprintf("Failed to fetch URL: %s", err)), nil
	}

	hasLargeContent := len(content) > netcommon.LargeContentThreshold
	var result strings.Builder

	if hasLargeContent {
		tempFile, err := os.CreateTemp(workingDir, "page-*.md")
		if err != nil {
			return contract.Error(fmt.Sprintf("Failed to create temporary file: %s", err)), nil
		}
		tempFilePath := tempFile.Name()

		if _, err := tempFile.WriteString(content); err != nil {
			_ = tempFile.Close() // Best effort close
			return contract.Error(fmt.Sprintf("Failed to write content to file: %s", err)), nil
		}
		if err := tempFile.Close(); err != nil {
			return contract.Error(fmt.Sprintf("Failed to close temporary file: %s", err)), nil
		}

		fmt.Fprintf(&result, "Fetched content from %s (large page)\n\n", params.URL)
		fmt.Fprintf(&result, "Content saved to: %s\n\n", tempFilePath)
		result.WriteString("Use the view and grep tools to analyze this file.")
	} else {
		fmt.Fprintf(&result, "Fetched content from %s:\n\n", params.URL)
		result.WriteString(content)
	}

	return contract.Text(result.String()), nil
}
