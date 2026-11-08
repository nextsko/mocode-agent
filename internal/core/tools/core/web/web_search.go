package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	netcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/net"
)

//go:embed web_search.md
var webSearchToolDescription []byte

// NewWebSearchPortTool creates a web search port for sub-agents (no permissions
// needed). It uses the default search provider chain (DuckDuckGo HTML scraper
// with the Instant Answer API as a structured fallback), so a rate limit or
// empty result from one backend transparently falls through to the next.
func NewWebSearchPortTool(client *http.Client) contract.Tool {
	return NewWebSearchPortToolWithProvider(client, netcommon.DefaultSearchProvider())
}

// NewWebSearchPortToolWithProvider creates a web search port backed by an
// explicit search [netcommon.Provider]. Use this when you want a single backend
// or a custom fallback chain (e.g. an enterprise search API first, DuckDuckGo
// second). Callers that just want sensible defaults should use
// NewWebSearchPortTool.
func NewWebSearchPortToolWithProvider(client *http.Client, provider netcommon.Provider) contract.Tool {
	if client == nil {
		client = netcommon.DefaultHTTPClient()
	}
	if provider == nil {
		provider = netcommon.DefaultSearchProvider()
	}

	return asParallel(contract.NewFunc(
		netcommon.WebSearchToolName,
		toolutil.FirstLineDescription(webSearchToolDescription),
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query to find information on the web",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return (default: 10, max: 20)",
				},
			},
			"required": []string{"query"},
		},
		func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params netcommon.WebSearchParams
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
			}
			return runWebSearch(ctx, client, provider, params)
		},
	))
}

// runWebSearch runs the provider chain and formats the hits.
func runWebSearch(ctx context.Context, client *http.Client, provider netcommon.Provider, params netcommon.WebSearchParams) (contract.ToolResult, error) {
	if params.Query == "" {
		return contract.Error("query is required"), nil
	}

	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 20 {
		maxResults = 20
	}

	netcommon.MaybeDelaySearch()
	results, err := provider.Search(ctx, client, params.Query, maxResults)
	slog.Debug("Web search completed", "query", params.Query, "provider", provider.Name(), "results", len(results), "err", err)
	if err != nil {
		return contract.Error("Failed to search: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
	}

	return contract.Text(netcommon.FormatSearchResults(results)), nil
}
