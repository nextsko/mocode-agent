package web_search

import (
	"context"
	_ "embed"
	"log/slog"
	"net/http"

	"github.com/package-register/mocode/internal/core/agent/tools/plugins/netcommon"
	"github.com/package-register/mocode/internal/core/agent/toolutil"

	"charm.land/fantasy"
)

//go:embed web_search.md
var webSearchToolDescription []byte

// NewWebSearchTool creates a web search tool for sub-agents (no permissions
// needed). It uses the default search provider chain (DuckDuckGo HTML scraper
// with the Instant Answer API as a structured fallback), so a rate limit or
// empty result from one backend transparently falls through to the next.
func NewWebSearchTool(client *http.Client) fantasy.AgentTool {
	return NewWebSearchToolWithProvider(client, netcommon.DefaultSearchProvider())
}

// NewWebSearchToolWithProvider creates a web search tool backed by an explicit
// search [netcommon.Provider]. Use this when you want a single backend or a
// custom fallback chain (e.g. an enterprise search API first, DuckDuckGo
// second). Callers that just want sensible defaults should use NewWebSearchTool.
func NewWebSearchToolWithProvider(client *http.Client, provider netcommon.Provider) fantasy.AgentTool {
	if client == nil {
		client = netcommon.DefaultHTTPClient()
	}
	if provider == nil {
		provider = netcommon.DefaultSearchProvider()
	}

	return fantasy.NewParallelAgentTool(
		netcommon.WebSearchToolName,
		toolutil.FirstLineDescription(webSearchToolDescription),
		func(ctx context.Context, params netcommon.WebSearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Query == "" {
				return fantasy.NewTextErrorResponse("query is required"), nil
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
				return fantasy.NewTextErrorResponse("Failed to search: " + err.Error()), nil
			}

			return fantasy.NewTextResponse(netcommon.FormatSearchResults(results)), nil
		},
	)
}
