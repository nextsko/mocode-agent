package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/nethttp"

	"github.com/nextsko/mocode-agent/internal/core/tools/core/web/crawler"
)

const (
	DownloadDocsToolName = "download_docs"
	MaxDownloadFiles     = 100
)

//go:embed download_docs.md
var downloadDocsDescription []byte

type DownloadDocsParams struct {
	RepoURL  string `json:"repo_url" description:"The GitHub repository URL to download docs from"`
	DocsPath string `json:"docs_path,omitempty" description:"Optional path prefix to filter files (e.g. 'docs', 'README.md')"`
	MaxFiles int    `json:"max_files,omitempty" description:"Maximum number of files to return (default: 50, max: 100)"`
}

// NewDownloadDocsPortTool builds the download_docs port tool. The factory is the
// single proxy source of truth: go-git clones through transport.ProxyOptions
// (not http.Client), so the tool reads the factory's resolved ProxyURL.
func NewDownloadDocsPortTool(factory *nethttp.Factory) contract.Tool {
	if factory == nil {
		factory = nethttp.NewFactory(nil, "")
	}
	return asParallel(contract.NewFunc(
		DownloadDocsToolName,
		toolutil.FirstLineDescription(downloadDocsDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_url": map[string]any{
					"type":        "string",
					"description": "The GitHub repository URL to download docs from",
				},
				"docs_path": map[string]any{
					"type":        "string",
					"description": "Optional path prefix to filter files (e.g. 'docs', 'README.md')",
				},
				"max_files": map[string]any{
					"type":        "integer",
					"description": "Maximum number of files to return (default: 50, max: 100)",
				},
			},
			"required": []string{"repo_url"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params DownloadDocsParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runDownloadDocs(factory, params)
		},
	))
}

// runDownloadDocs clones a GitHub repo's docs and renders them as Markdown.
func runDownloadDocs(factory *nethttp.Factory, params DownloadDocsParams) (contract.ToolResult, error) {
	if params.RepoURL == "" {
		return contract.Error("repo_url parameter is required"), nil
	}

	if !strings.Contains(params.RepoURL, "github.com") {
		return contract.Error("Only GitHub repositories are supported"), nil
	}

	// Clamp max_files
	if params.MaxFiles <= 0 {
		params.MaxFiles = 50
	} else if params.MaxFiles > MaxDownloadFiles {
		params.MaxFiles = MaxDownloadFiles
	}

	downhub := crawler.NewDownhub().
		URL(params.RepoURL).
		MaxFiles(params.MaxFiles)
	if proxy := factory.ProxyURL(); proxy != "" {
		downhub = downhub.Proxy(proxy)
	}

	if params.DocsPath != "" {
		downhub = downhub.Path(params.DocsPath)
	}

	result, err := downhub.Fetch()
	if err != nil {
		return contract.Error(fmt.Sprintf("Download failed: %v", err)), nil
	}

	if result.Count == 0 {
		return contract.Text("No documentation files found in the repository."), nil
	}

	output := result.ToMarkdown()

	// Truncate if too large
	const maxOutput = 200000 // 200KB
	if len(output) > maxOutput {
		output = output[:maxOutput] + "\n\n[Content truncated - exceeded 200KB]"
	}

	return contract.Text(output), nil
}
