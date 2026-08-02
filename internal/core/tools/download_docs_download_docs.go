package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/crawler"
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

func NewDownloadDocsTool(proxyURL ...string) fantasy.AgentTool {
	return fantasy.NewParallelAgentTool(
		DownloadDocsToolName,
		toolutil.FirstLineDescription(downloadDocsDescription),
		func(ctx context.Context, params DownloadDocsParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.RepoURL == "" {
				return fantasy.NewTextErrorResponse("repo_url parameter is required"), nil
			}

			if !strings.Contains(params.RepoURL, "github.com") {
				return fantasy.NewTextErrorResponse("Only GitHub repositories are supported"), nil
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
			if len(proxyURL) > 0 && proxyURL[0] != "" {
				downhub = downhub.Proxy(proxyURL[0])
			}

			if params.DocsPath != "" {
				downhub = downhub.Path(params.DocsPath)
			}

			result, err := downhub.Fetch()
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Download failed: %v", err)), nil
			}

			if result.Count == 0 {
				return fantasy.NewTextResponse("No documentation files found in the repository."), nil
			}

			output := result.ToMarkdown()

			// Truncate if too large
			const maxOutput = 200000 // 200KB
			if len(output) > maxOutput {
				output = output[:maxOutput] + "\n\n[Content truncated - exceeded 200KB]"
			}

			return fantasy.NewTextResponse(output), nil
		})
}
