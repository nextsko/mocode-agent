package web_search

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/core/agent/tools/plugins/netcommon"
)

// stubProvider is a controllable Provider for testing tool delegation without
// network access. It returns a fixed result set or an injected error.
type stubProvider struct {
	results []netcommon.SearchResult
	err     error
}

func (s stubProvider) Name() string { return "stub" }
func (s stubProvider) Search(_ context.Context, _ *http.Client, _ string, _ int) ([]netcommon.SearchResult, error) {
	return s.results, s.err
}

// TestNewWebSearchTool_DelegatesToProvider proves the tool delegates to the
// injected Provider and returns the provider's results formatted via
// FormatSearchResults. This closes the integration-test gap: the Provider
// abstraction has unit tests, but nothing verified the tool wiring itself
// (the same class of "built but not tested end-to-end" gap that the gates had).
func TestNewWebSearchTool_DelegatesToProvider(t *testing.T) {
	provider := stubProvider{
		results: []netcommon.SearchResult{
			{Title: "Go Reference", Link: "https://go.dev/ref/spec", Snippet: "The Go spec"},
			{Title: "Go Tour", Link: "https://go.dev/tour", Snippet: "A Tour of Go"},
		},
	}
	tool := NewWebSearchToolWithProvider(nil, provider)

	// The tool must be non-nil and report the correct name.
	info := tool.Info()
	require.Equal(t, netcommon.WebSearchToolName, info.Name)

	// Invoke with a query and verify the provider's results flow through.
	params := netcommon.WebSearchParams{Query: "golang", MaxResults: 5}
	input, _ := json.Marshal(params)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test",
		Name:  netcommon.WebSearchToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "Go Reference")
	require.Contains(t, resp.Content, "Go Tour")
	require.Contains(t, resp.Content, "Found 2 search results")
}

// TestNewWebSearchTool_EmptyQueryReturnsError verifies the tool validates its
// input before hitting the provider — an empty query is a client error, not a
// search attempt.
func TestNewWebSearchTool_EmptyQueryReturnsError(t *testing.T) {
	provider := stubProvider{results: []netcommon.SearchResult{{Title: "should not be called"}}}
	tool := NewWebSearchToolWithProvider(nil, provider)

	input, _ := json.Marshal(netcommon.WebSearchParams{Query: ""})
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test",
		Name:  netcommon.WebSearchToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Content)
	require.Contains(t, resp.Content, "query is required")
}

// TestNewWebSearchTool_NilProviderFallsBack confirms a nil provider does not
// panic — it falls back to DefaultSearchProvider (which will make a real
// network call, but that's acceptable for the construction test: we only
// verify the tool is built and named correctly).
func TestNewWebSearchTool_NilProviderFallsBack(t *testing.T) {
	tool := NewWebSearchToolWithProvider(nil, nil)
	require.NotNil(t, tool)
	require.Equal(t, netcommon.WebSearchToolName, tool.Info().Name)
}
