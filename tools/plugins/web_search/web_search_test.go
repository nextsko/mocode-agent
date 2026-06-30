package web_search_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/netcommon"
	"github.com/package-register/mocode/tools/plugins/web_search"
)

type stubProvider struct {
	results []netcommon.SearchResult
	err     error
	query   string
	limit   int
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Search(_ context.Context, _ *http.Client, query string, maxResults int) ([]netcommon.SearchResult, error) {
	s.query = query
	s.limit = maxResults
	return s.results, s.err
}

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(netcommon.WebSearchToolName)
	require.True(t, ok)
	require.Equal(t, netcommon.WebSearchToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, netcommon.WebSearchToolName, tool.Schema().Name)
}

func TestExecuteDelegatesToProvider(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{results: []netcommon.SearchResult{
		{Title: "Go Reference", Link: "https://go.dev/ref/spec", Snippet: "The Go spec", Position: 1},
		{Title: "Go Tour", Link: "https://go.dev/tour", Snippet: "A Tour of Go", Position: 2},
	}}
	result, err := web_search.NewWithProvider(nil, provider).Execute(context.Background(), nil, json.RawMessage(`{"query":"golang","max_results":5}`))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Equal(t, "golang", provider.query)
	require.Equal(t, 5, provider.limit)
	require.Contains(t, result.Content, "Go Reference")
	require.Contains(t, result.Content, "Go Tour")
	require.Contains(t, result.Content, "Found 2 search results")
}

func TestExecuteEmptyQueryReturnsError(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{results: []netcommon.SearchResult{{Title: "should not be called"}}}
	result, err := web_search.NewWithProvider(nil, provider).Execute(context.Background(), nil, json.RawMessage(`{"query":""}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "query is required")
	require.Empty(t, provider.query)
}

func TestExecuteProviderErrorReturnsToolError(t *testing.T) {
	t.Parallel()

	result, err := web_search.NewWithProvider(nil, &stubProvider{err: errors.New("rate limited")}).Execute(context.Background(), nil, json.RawMessage(`{"query":"golang"}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "failed to search")
}
