package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"

	"github.com/nextsko/mocode-agent/internal/core/tools/core/web/crawler"
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

// NewCrawlPortTool is the framework-agnostic form of the crawl tool.
func NewCrawlPortTool(client ...*http.Client) contract.Tool {
	return asParallel(contract.NewFunc(
		CrawlToolName,
		toolutil.FirstLineDescription(crawlDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The starting URL to begin crawling",
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Maximum crawl depth (default: 2, max: 5)",
				},
				"max_pages": map[string]any{
					"type":        "integer",
					"description": "Maximum number of pages to crawl (default: 20, max: 50)",
				},
			},
			"required": []string{"url"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params CrawlParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			var httpClient *http.Client
			if len(client) > 0 {
				httpClient = client[0]
			}
			return runCrawl(ctx, httpClient, params)
		},
	))
}

// runCrawl clamps the crawl budget and renders the crawled pages as Markdown.
func runCrawl(ctx context.Context, client *http.Client, params CrawlParams) (contract.ToolResult, error) {
	if params.URL == "" {
		return contract.Error("URL parameter is required"), nil
	}

	if !strings.HasPrefix(params.URL, "http://") && !strings.HasPrefix(params.URL, "https://") {
		return contract.Error("URL must start with http:// or https://"), nil
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
	if client != nil {
		rc.SetClient(client)
	}
	result, err := rc.Crawl(ctx, params.URL)
	if err != nil {
		return contract.Error(fmt.Sprintf("Crawl failed: %v", err)), nil
	}

	if result.Total == 0 {
		return contract.Text("No pages were successfully crawled."), nil
	}

	// Format output
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Crawl Results\n\nCrawled %d pages from %s (max_depth: %d)\n\n", result.Total, params.URL, params.MaxDepth)

	for i, page := range result.Pages {
		fmt.Fprintf(&sb, "---\n\n## Page %d: %s\n\n", i+1, page.Title)
		fmt.Fprintf(&sb, "**URL:** %s\n\n", page.URL)
		sb.WriteString(page.Markdown)
		sb.WriteString("\n\n")
	}

	output := sb.String()

	// Truncate if too large
	const maxOutput = 200000 // 200KB
	if len(output) > maxOutput {
		output = output[:maxOutput] + "\n\n[Content truncated - exceeded 200KB]"
	}

	return contract.Text(output), nil
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
