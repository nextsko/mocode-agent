package web_search

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/netcommon"
)

//go:embed web_search.md
var webSearchToolDescription []byte

type webSearchTool struct {
	client   *http.Client
	provider netcommon.Provider
}

func New(client *http.Client) tools.Tool {
	return NewWithProvider(client, netcommon.DefaultSearchProvider())
}

func NewWithProvider(client *http.Client, provider netcommon.Provider) tools.Tool {
	if client == nil {
		client = netcommon.DefaultHTTPClient()
	}
	if provider == nil {
		provider = netcommon.DefaultSearchProvider()
	}
	return &webSearchTool{client: client, provider: provider}
}

func (t *webSearchTool) Name() string { return netcommon.WebSearchToolName }

func (t *webSearchTool) Description() string { return firstLine(webSearchToolDescription) }

func (t *webSearchTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        netcommon.WebSearchToolName,
		Description: t.Description(),
		Parameters: map[string]any{
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
	}
}

func (t *webSearchTool) Execute(ctx context.Context, _ tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params netcommon.WebSearchParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if params.Query == "" {
		return tools.ToolResult{Error: errors.New("query is required")}, nil
	}

	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 20 {
		maxResults = 20
	}

	netcommon.MaybeDelaySearch()
	results, err := t.provider.Search(ctx, t.client, params.Query, maxResults)
	slog.Debug("Web search completed", "query", params.Query, "provider", t.provider.Name(), "results", len(results), "err", err)
	if err != nil {
		return tools.ErrorResult(errors.New("failed to search: " + err.Error()))
	}

	return tools.ToolResult{Content: netcommon.FormatSearchResults(results)}, nil
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

func init() { tools.Register(netcommon.WebSearchToolName, New(nil)) }
