package web_fetch

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/netcommon"
)

//go:embed web_fetch.md
var webFetchToolDescription []byte

type webFetchTool struct {
	client *http.Client
}

func New(client *http.Client) tools.Tool {
	if client == nil {
		client = netcommon.DefaultHTTPClient()
	}
	return &webFetchTool{client: client}
}

func (t *webFetchTool) Name() string { return netcommon.WebFetchToolName }

func (t *webFetchTool) Description() string { return firstLine(webFetchToolDescription) }

func (t *webFetchTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        netcommon.WebFetchToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (t *webFetchTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params netcommon.WebFetchParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if params.URL == "" {
		return tools.ToolResult{Error: errors.New("url is required")}, nil
	}

	content, err := netcommon.FetchURLAndConvert(ctx, t.client, params.URL)
	if err != nil {
		return tools.ErrorResult(fmt.Errorf("failed to fetch URL: %w", err))
	}

	workingDir := ""
	if tctx != nil {
		workingDir = tctx.WorkingDir()
	}
	if workingDir == "" {
		workingDir = os.TempDir()
	}

	hasLargeContent := len(content) > netcommon.LargeContentThreshold
	var result strings.Builder
	if hasLargeContent {
		tempFile, err := os.CreateTemp(workingDir, "page-*.md")
		if err != nil {
			return tools.ErrorResult(fmt.Errorf("failed to create temporary file: %w", err))
		}
		tempFilePath := tempFile.Name()

		if _, err := tempFile.WriteString(content); err != nil {
			_ = tempFile.Close()
			return tools.ErrorResult(fmt.Errorf("failed to write content to file: %w", err))
		}
		if err := tempFile.Close(); err != nil {
			return tools.ErrorResult(fmt.Errorf("failed to close temporary file: %w", err))
		}

		fmt.Fprintf(&result, "Fetched content from %s (large page)\n\n", params.URL)
		fmt.Fprintf(&result, "Content saved to: %s\n\n", tempFilePath)
		result.WriteString("Use the view and grep tools to analyze this file.")
	} else {
		fmt.Fprintf(&result, "Fetched content from %s:\n\n", params.URL)
		result.WriteString(content)
	}

	return tools.ToolResult{Content: result.String()}, nil
}

func firstLine(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func init() { tools.Register(netcommon.WebFetchToolName, New(nil)) }
