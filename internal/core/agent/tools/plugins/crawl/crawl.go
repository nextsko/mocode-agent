package crawl

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/package-register/mocode/internal/core/agent/toolutil"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/crawler"
)

const (
	CrawlToolName = "crawl"
	MaxCrawlPages = 50
	MaxCrawlDepth = 5
)

//go:embed crawl.md
var crawlDescription []byte

type CrawlParams struct {
	URL      string `json:"url" description:"The starting URL to begin crawling"`
	MaxDepth int    `json:"max_depth,omitempty" description:"Maximum crawl depth (default: 2, max: 5)"`
	MaxPages int    `json:"max_pages,omitempty" description:"Maximum number of pages to crawl (default: 20, max: 50)"`
}

func NewCrawlTool(client ...*http.Client) fantasy.AgentTool {
	return fantasy.NewParallelAgentTool(
		CrawlToolName,
		toolutil.FirstLineDescription(crawlDescription),
		func(ctx context.Context, params CrawlParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.URL == "" {
				return fantasy.NewTextErrorResponse("URL parameter is required"), nil
			}

			if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
				return fantasy.NewTextErrorResponse("URL must start with http:// or https://"), nil
			}

			// Clamp parameters
			if params.MaxDepth <= 0 {
				params.MaxDepth = 2
			} else if params.MaxDepth > MaxCrawlDepth {
				params.MaxDepth = MaxCrawlDepth
			}

			if params.MaxPages <= 0 {
				params.MaxPages = 20
			} else if params.MaxPages > MaxCrawlPages {
				params.MaxPages = MaxCrawlPages
			}

			opts := &crawler.CrawlOptions{
				MaxDepth:   params.MaxDepth,
				MaxPages:   params.MaxPages,
				SameDomain: true,
			}

			rc := crawler.NewRecursiveCrawler(opts)
			if len(client) > 0 && client[0] != nil {
				rc.SetClient(client[0])
			}
			result, err := rc.Crawl(params.URL)
			if err != nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("Crawl failed: %v", err)), nil
			}

			if result.Total == 0 {
				return fantasy.NewTextResponse("No pages were successfully crawled."), nil
			}

			// Format output
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("# Crawl Results\n\nCrawled %d pages from %s (max_depth: %d)\n\n", result.Total, params.URL, params.MaxDepth))

			for i, page := range result.Pages {
				sb.WriteString(fmt.Sprintf("---\n\n## Page %d: %s\n\n", i+1, page.Title))
				sb.WriteString(fmt.Sprintf("**URL:** %s\n\n", page.URL))
				sb.WriteString(page.Markdown)
				sb.WriteString("\n\n")
			}

			output := sb.String()

			// Truncate if too large
			const maxOutput = 200000 // 200KB
			if len(output) > maxOutput {
				output = output[:maxOutput] + "\n\n[Content truncated - exceeded 200KB]"
			}

			return fantasy.NewTextResponse(output), nil
		})
}

// CrawlPermissionsParams returns the permission parameters for the crawl tool.
func CrawlPermissionsParams(params CrawlParams) map[string]any {
	return map[string]any{
		"url":       params.URL,
		"max_depth": params.MaxDepth,
		"max_pages": params.MaxPages,
	}
}

// formatCrawlResultJSON formats the crawl result as JSON for structured output.
func formatCrawlResultJSON(result *crawler.CrawlResult) string {
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data)
}
