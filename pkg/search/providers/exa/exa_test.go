package exa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// TestMain disables request pacing for every test except the pacing test
// itself, which re-enables it temporarily.
func TestMain(m *testing.M) {
	SetSearchDelayMs(0)
	resetSearchThrottle()
	os.Exit(m.Run())
}

// stubBroker resolves a fixed key or error.
type stubBroker struct {
	key string
	err error
}

func (b stubBroker) Resolve(
	ctx context.Context,
	providerID string,
	opts search.ResolveOpts,
) (string, error) {
	if b.err != nil {
		return "", b.err
	}
	return b.key, nil
}

func (b stubBroker) Refresh(
	ctx context.Context,
	providerID string,
	opts search.ResolveOpts,
) (string, error) {
	return b.Resolve(ctx, providerID, opts)
}

// mcpRequestRecord captures one MCP HTTP request hitting the mock.
type mcpRequestRecord struct {
	method string
	url    *url.URL
	header http.Header
	body   map[string]any
}

// mcpMock is a real in-process MCP server (go-sdk server side) behind
// an httptest server. The outer wrapper records every JSON-RPC request
// and can be flipped into a raw HTTP failure mode to exercise transport
// error handling.
type mcpMock struct {
	server *mcp.Server

	// failStatus, when > 0, makes the wrapper answer every request with
	// that HTTP status instead of forwarding it to the MCP handler.
	failStatus atomic.Int64

	mu      sync.Mutex
	records []mcpRequestRecord
}

// newMCPMock builds a mock whose web_search_exa tool replies with the
// result of resultFn (a non-nil error yields a JSON-RPC protocol
// error).
func newMCPMock(t *testing.T, resultFn func() (*mcp.CallToolResult, error)) *mcpMock {
	t.Helper()
	server := mcp.NewServer(
		&mcp.Implementation{Name: "exa-mock", Version: "0"}, nil)
	m := &mcpMock{server: server}
	server.AddTool(&mcp.Tool{
		Name:        mcpSearchTool,
		InputSchema: &jsonschema.Schema{Type: "object"},
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return resultFn()
	})
	return m
}

// setFail flips the wrapper into raw HTTP failure mode with the given
// status.
func (m *mcpMock) setFail(status int) { m.failStatus.Store(int64(status)) }

// clearFail restores normal MCP handling.
func (m *mcpMock) clearFail() { m.failStatus.Store(0) }

func (m *mcpMock) wrap(mcpHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status := m.failStatus.Load(); status > 0 {
			w.WriteHeader(int(status))
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		parsed := *r.URL
		m.mu.Lock()
		m.records = append(m.records, mcpRequestRecord{
			method: stringOrNil(decoded["method"]),
			url:    &parsed,
			header: r.Header.Clone(),
			body:   decoded,
		})
		m.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		mcpHandler.ServeHTTP(w, r)
	})
}

func (m *mcpMock) start(t *testing.T) *httptest.Server {
	t.Helper()
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return m.server }, nil)
	srv := httptest.NewServer(m.wrap(handler))
	t.Cleanup(srv.Close)
	return srv
}

func (m *mcpMock) recorded() []mcpRequestRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mcpRequestRecord, len(m.records))
	copy(out, m.records)
	return out
}

// countMethod counts recorded requests carrying the JSON-RPC method.
func (m *mcpMock) countMethod(method string) int {
	n := 0
	for _, rec := range m.recorded() {
		if rec.method == method {
			n++
		}
	}
	return n
}

// firstMethod returns the first recorded request for the JSON-RPC
// method.
func (m *mcpMock) firstMethod(method string) mcpRequestRecord {
	for _, rec := range m.recorded() {
		if rec.method == method {
			return rec
		}
	}
	return mcpRequestRecord{}
}

func stringOrNil(v any) string {
	s, _ := v.(string)
	return s
}

// restRecord captures one mock REST request.
type restRecord struct {
	header http.Header
	body   map[string]any
}

// restMock serves REST statuses and payloads.
type restMock struct {
	status int
	body   string
	mu     sync.Mutex
	calls  []restRecord
}

func (r *restMock) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		r.mu.Lock()
		r.calls = append(r.calls, restRecord{
			header: req.Header.Clone(), body: decoded})
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(r.status)
		fmt.Fprint(w, r.body)
	}
}

func (r *restMock) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(r.handler())
	t.Cleanup(srv.Close)
	return srv
}

func (r *restMock) recorded() []restRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]restRecord, len(r.calls))
	copy(out, r.calls)
	return out
}

// unusedMCPURL is a placeholder endpoint no request should ever reach.
const unusedMCPURL = "http://127.0.0.1:1/mcp"

// keylessBroker is the credential stub used by every keyless MCP test.
func keylessBroker() stubBroker {
	return stubBroker{err: search.ErrCredentialNotFound}
}

// structuredResultFn replies with a structuredContent search payload.
func structuredResultFn() func() (*mcp.CallToolResult, error) {
	return func() (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				"results": []any{map[string]any{
					"title":   "Go",
					"url":     "https://go.dev",
					"summary": "Go language",
				}},
			},
		}, nil
	}
}

func TestMetaAndProviderIdentity(t *testing.T) {
	t.Parallel()
	meta := Meta()
	require.Equal(t, "exa", meta.ID)
	require.Equal(t, "Exa", meta.Label)
	require.NotNil(t, meta.NewFunc)
	provider := New(nil)
	require.Equal(t, "exa", provider.ID())
	require.Equal(t, "Exa", provider.Label())
	var _ search.ProviderV2 = provider
}

func TestIsAvailableRequiresKey(t *testing.T) {
	t.Parallel()
	p := New(nil)
	ok, err := p.IsAvailable(context.Background(),
		stubBroker{key: "k1"})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = p.IsAvailable(context.Background(),
		stubBroker{err: fmt.Errorf("wrapped: %w", search.ErrCredentialNotFound)})
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = p.IsAvailable(context.Background(), nil)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIsExplicitlyAvailableAlwaysTrue(t *testing.T) {
	t.Parallel()
	p := New(nil)
	ok, err := p.IsExplicitlyAvailable(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestSearchRESTPath(t *testing.T) {
	t.Parallel()
	recent := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	rest := &restMock{status: http.StatusOK, body: fmt.Sprintf(`{
		"requestId": "req-1",
		"results": [
			{"title": "Go", "url": "https://go.dev", "summary": "Go language",
			 "publishedDate": %q, "author": "Rob"},
			{"title": "NoSummary", "url": "https://nosummary.dev", "text": "raw text"},
			{"title": "Highlights", "url": "https://hl.dev", "highlights": ["h1", "h2"]},
			{"title": "NoURL", "summary": "dropped"}
		]
	}`, recent)}
	srv := rest.start(t)
	p := newProvider(srv.Client(), srv.URL, unusedMCPURL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, "exa", resp.Provider)
	require.Equal(t, "api_key", resp.AuthMode)
	require.Equal(t, "req-1", resp.RequestID)

	calls := rest.recorded()
	require.Len(t, calls, 1)
	require.Equal(t, "k1", calls[0].header.Get("x-api-key"))
	require.Equal(t, "application/json", calls[0].header.Get("Content-Type"))
	body := calls[0].body
	require.Equal(t, "golang", body["query"])
	require.Equal(t, float64(10), body["numResults"])
	require.Equal(t, "auto", body["type"])
	contents := body["contents"].(map[string]any)
	summary := contents["summary"].(map[string]any)
	require.Equal(t, "golang", summary["query"])

	require.Len(t, resp.Sources, 3)
	first := resp.Sources[0]
	require.Equal(t, "Go", first.Title)
	require.Equal(t, "https://go.dev", first.URL)
	require.Equal(t, "Go language", first.Snippet)
	require.Equal(t, "Rob", first.Author)
	require.Equal(t, recent, first.PublishedDate)
	require.Greater(t, first.AgeSeconds, int64(0))
	require.Equal(t, "raw text", resp.Sources[1].Snippet)
	require.Equal(t, "h1 h2", resp.Sources[2].Snippet)

	require.Contains(t, resp.Answer, "**Go**: Go language")
	require.Equal(t, 0, strings.Count(resp.Answer, "\n\n"))
}

func TestSearchRESTLimitAndDirectives(t *testing.T) {
	t.Parallel()
	rest := &restMock{status: http.StatusOK,
		body: `{"results":[{"title":"A","url":"https://a","summary":"s1"}]}`}
	srv := rest.start(t)
	p := newProvider(srv.Client(), srv.URL, unusedMCPURL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:            "rust async site:github.com/rust-lang/rust after:2024-01-01",
		NumSearchResults: 3,
		AuthStorage:      stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	body := rest.recorded()[0].body
	require.Equal(t, "rust async", body["query"])
	require.Equal(t, []any{"github.com"}, body["includeDomains"])
	require.Equal(t, "2024-01-01", body["startPublishedDate"])
	require.Equal(t, float64(3), body["numResults"])
}

func TestSearchMCPKeyless(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, structuredResultFn())
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: keylessBroker(),
	})
	require.NoError(t, err)
	require.Equal(t, "keyless", resp.AuthMode)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, "Go", resp.Sources[0].Title)
	require.Contains(t, resp.Answer, "**Go**: Go language")

	// Every request to the endpoint carries the keyless query and the
	// source header; the SDK transport advertises both accept types.
	records := mock.recorded()
	require.NotEmpty(t, records)
	q := records[0].url.Query()
	require.Equal(t, mcpSearchTool, q.Get("tools"))
	require.Empty(t, q.Get("exaApiKey"))
	require.Equal(t, "oh-my-pi", records[0].header.Get("x-exa-source"))
	require.Equal(t, "application/json, text/event-stream",
		records[0].header.Get("Accept"))

	call := mock.firstMethod("tools/call")
	require.NotEmpty(t, call.body)
	params := call.body["params"].(map[string]any)
	require.Equal(t, mcpSearchTool, params["name"])
	args := params["arguments"].(map[string]any)
	require.Equal(t, "golang", args["query"])
}

func TestSearchRESTFailureFallsBackToMCP(t *testing.T) {
	t.Parallel()
	rest := &restMock{status: http.StatusUnauthorized, body: `{"error":"bad key"}`}
	restSrv := rest.start(t)
	mock := newMCPMock(t, structuredResultFn())
	mcpSrv := mock.start(t)
	p := newProvider(restSrv.Client(), restSrv.URL, mcpSrv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, "api_key", resp.AuthMode)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, 1, len(rest.recorded()))
	require.Equal(t, 1, mock.countMethod("tools/call"))
	// The fallback carries the resolved key in the endpoint query.
	call := mock.firstMethod("tools/call")
	require.Equal(t, "k1", call.url.Query().Get("exaApiKey"))
}

func TestSearchBothPathsFailAggregatesErrors(t *testing.T) {
	t.Parallel()
	rest := &restMock{status: http.StatusInternalServerError, body: "boom"}
	restSrv := rest.start(t)
	mock := newMCPMock(t, structuredResultFn())
	mock.setFail(http.StatusTooManyRequests)
	mcpSrv := mock.start(t)
	p := newProvider(restSrv.Client(), restSrv.URL, mcpSrv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "REST path failed")
	require.Contains(t, err.Error(), "MCP fallback failed")
	require.Contains(t, err.Error(),
		"configure an Exa API key for higher limits")
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, search.ErrorCodeServerError, spe.Code)
}

func TestSearchMCP429CarriesKeyHint(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, structuredResultFn())
	mock.setFail(http.StatusTooManyRequests)
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: keylessBroker(),
	})
	require.Nil(t, resp)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)
	require.Contains(t, spe.Message, "configure an Exa API key for higher limits")
}

// TestMCPPayloadShapes covers the payload shapes the extractor handles
// on top of the SDK's protocol layer: structuredContent, and a search
// JSON document embedded in a text content block.
func TestMCPPayloadShapes(t *testing.T) {
	t.Parallel()
	resultJSON := `{"title":"T","url":"https://x.dev","summary":"S"}`
	shapes := map[string]func() (*mcp.CallToolResult, error){
		"structuredContent": func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"results": []any{map[string]any{
						"title": "T", "url": "https://x.dev", "summary": "S",
					}},
				},
			}, nil
		},
		"content text json": func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{
					Text: `{"results":[` + resultJSON + `]}`,
				}},
			}, nil
		},
	}
	for name, resultFn := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mock := newMCPMock(t, resultFn)
			srv := mock.start(t)
			p := newProvider(srv.Client(), unusedMCPURL, srv.URL)
			resp, err := p.Search(context.Background(), search.SearchParams{
				Query:       "q",
				AuthStorage: keylessBroker(),
			})
			require.NoError(t, err)
			require.Len(t, resp.Sources, 1)
			require.Equal(t, "T", resp.Sources[0].Title)
			require.Equal(t, "https://x.dev", resp.Sources[0].URL)
			require.Equal(t, "S", resp.Sources[0].Snippet)
		})
	}
}

func TestMCPTextPayloadSections(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{
		"Some preamble about nothing",
		"",
		"Title: First",
		"URL: https://first.dev",
		"Author: Ada",
		"Published Date: 2024-01-02",
		"Text: hello world",
		"",
		"with a blank line inside",
		"",
		"",
		"Title: Second",
		"URL: https://second.dev",
		"Text: second text",
	}, "\n")
	mock := newMCPMock(t, func() (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: keylessBroker(),
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 2)
	first := resp.Sources[0]
	require.Equal(t, "First", first.Title)
	require.Equal(t, "https://first.dev", first.URL)
	require.Equal(t, "Ada", first.Author)
	require.Equal(t, "2024-01-02", first.PublishedDate)
	require.Contains(t, first.Snippet, "hello world")
	require.Contains(t, first.Snippet, "with a blank line inside")
	require.Equal(t, "Second", resp.Sources[1].Title)
	require.Contains(t, resp.Sources[1].Snippet, "second text")
}

func TestMCPResultIsErrorSurfacesText(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func() (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: "Exa error: monthly quota",
			}},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: keylessBroker(),
	})
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, "Exa error: monthly quota", spe.Message)
}

func TestMCPUnexpectedShapeErrors(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func() (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "plain notes"}},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: keylessBroker(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected response shape")
}

func TestMCPRPCErrorSurfaces(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func() (*mcp.CallToolResult, error) {
		return nil, fmt.Errorf("tool broken")
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: keylessBroker(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "MCP error")
	require.Contains(t, err.Error(), "tool broken")
}

// TestMCPSessionReuseAndRebuild asserts the cached session behavior: a
// healthy session is reused (initialize happens once), any transport
// failure drops it, and the next search reconnects.
func TestMCPSessionReuseAndRebuild(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, structuredResultFn())
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)
	broker := keylessBroker()

	search := func() error {
		_, err := p.Search(context.Background(), search.SearchParams{
			Query: "q", AuthStorage: broker})
		return err
	}

	require.NoError(t, search())
	require.NoError(t, search())
	require.Equal(t, 1, mock.countMethod("initialize"),
		"second search should reuse the session")
	require.Equal(t, 2, mock.countMethod("tools/call"))

	// A transport-level HTTP failure invalidates the cached session.
	mock.setFail(http.StatusInternalServerError)
	require.Error(t, search())
	require.Nil(t, p.mcpSess, "failed call must drop the session")

	mock.clearFail()
	require.NoError(t, search())
	require.Equal(t, 2, mock.countMethod("initialize"),
		"recovery must reconnect exactly once more")
	require.Equal(t, 3, mock.countMethod("tools/call"))
}

// TestMCPEndpointTracksCredential verifies a credential change rekeys
// the session endpoint instead of reusing the keyless one.
func TestMCPEndpointTracksCredential(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, structuredResultFn())
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query: "q", AuthStorage: keylessBroker()})
	require.NoError(t, err)
	require.Empty(t, mock.firstMethod("tools/call").url.Query().Get("exaApiKey"))

	_, err = p.Search(context.Background(), search.SearchParams{
		Query: "q", AuthStorage: stubBroker{key: "k1"}})
	require.NoError(t, err)
	calls := mock.recorded()
	var keyed *mcpRequestRecord
	for i := range calls {
		if calls[i].method == "tools/call" &&
			calls[i].url.Query().Get("exaApiKey") == "k1" {
			keyed = &calls[i]
		}
	}
	require.NotNil(t, keyed, "keyed search must hit a keyed endpoint")
	require.Equal(t, 2, mock.countMethod("initialize"),
		"credential change must force a reconnect")
}

func TestBuildMCPArgumentsInlinesOperators(t *testing.T) {
	t.Parallel()
	args := buildMCPArguments(searchInput{
		query:              "go",
		includeDomains:     []string{"github.com"},
		excludeDomains:     []string{"spam.com"},
		startPublishedDate: "2020-01-01",
		endPublishedDate:   "2021-06-30",
		numResults:         7,
	})
	require.Equal(t,
		"go site:github.com -site:spam.com after:2020-01-01 before:2021-06-30",
		args.Query)
	require.Equal(t, 7, args.NumResults)

	bare := buildMCPArguments(searchInput{query: "plain"})
	require.Equal(t, "plain", bare.Query)
	require.Zero(t, bare.NumResults)
}

func TestMCPArgumentsMarshalOmitsEmptyNumResults(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(buildMCPArguments(searchInput{query: "q"}))
	require.NoError(t, err)
	require.JSONEq(t, `{"query":"q"}`, string(b))

	b, err = json.Marshal(buildMCPArguments(searchInput{
		query: "q", numResults: 5}))
	require.NoError(t, err)
	require.JSONEq(t, `{"query":"q","numResults":5}`, string(b))
}

func TestMCPEndpointKeylessOmitsAPIKeyParam(t *testing.T) {
	t.Parallel()
	keyless := mcpEndpoint("https://mcp.exa.ai/mcp", "")
	require.NotContains(t, keyless, "exaApiKey")
	require.Contains(t, keyless, "tools=web_search_exa")

	keyed := mcpEndpoint("https://mcp.exa.ai/mcp", "k1")
	require.Contains(t, keyed, "exaApiKey=k1")
	require.Contains(t, keyed, "tools=web_search_exa")
}

// TestSynthesizeAnswer pins the TypeScript-aligned behavior: summaries
// are joined in order (up to three), the title falls back to the URL
// then "Untitled", and results without summaries are skipped.
func TestSynthesizeAnswer(t *testing.T) {
	t.Parallel()
	results := []exaResult{
		{Title: "A", URL: "https://a", Summary: " one "},
		{URL: "https://b", Summary: "two"},
		{Summary: "skipped, no summary"},
		{Title: "C", URL: "https://c", Summary: "three"},
		{Title: "D", URL: "https://d", Summary: "four is dropped"},
	}
	answer := synthesizeAnswer(results)
	parts := strings.Split(answer, "\n\n")
	require.Len(t, parts, 3)
	require.Equal(t, "**A**: one", parts[0])
	require.Equal(t, "**https://b**: two", parts[1])
	require.Equal(t, "**Untitled**: skipped, no summary", parts[2])
	require.Equal(t, "", synthesizeAnswer(nil))
	require.Equal(t, "", synthesizeAnswer([]exaResult{{Title: "x", URL: "u"}}))
}

func TestClassifyRESTHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		body   string
		code   string
	}{
		{"billing body", 400, "quota exceeded", search.ErrorCodeBilling},
		{"402", 402, "", search.ErrorCodeBilling},
		{"401", 401, "", search.ErrorCodeUnauthorized},
		{"403", 403, "", search.ErrorCodeForbidden},
		{"429", 429, "", search.ErrorCodeRateLimited},
		{"500", 500, "", search.ErrorCodeServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spe := classifyRESTHTTP(tc.status, tc.body)
			require.NotNil(t, spe)
			require.Equal(t, tc.code, spe.Code)
			require.Equal(t, tc.status, spe.HTTPStatus)
			require.Equal(t, "exa", spe.Provider)
		})
	}
	require.Nil(t, classifyRESTHTTP(418, "teapot"))
}

func TestClassifyMCPHTTP429Hint(t *testing.T) {
	t.Parallel()
	spe := classifyMCPHTTP(429, "")
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Contains(t, spe.Message, "configure an Exa API key for higher limits")
	billing := classifyMCPHTTP(429, "insufficient credits")
	require.Equal(t, search.ErrorCodeBilling, billing.Code)
	generic := classifyMCPHTTP(418, "teapot")
	require.Equal(t, "", generic.Code)
	require.Contains(t, generic.Message, "418")
}

// TestMapMCPError pins classification of the wrapped error types the
// SDK surfaces: captured HTTP statuses and JSON-RPC protocol errors.
func TestMapMCPError(t *testing.T) {
	t.Parallel()
	spe, ok := mapMCPError(&mcpHTTPError{
		Status: http.StatusTooManyRequests, Body: ""}).(*search.SearchProviderError)
	require.True(t, ok)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)

	spe, ok = mapMCPError(fmt.Errorf(
		"sending tools/call: %w", &mcpHTTPError{
			Status: http.StatusForbidden, Body: "denied"})).(*search.SearchProviderError)
	require.True(t, ok)
	require.Equal(t, search.ErrorCodeForbidden, spe.Code)

	wrapped := mapMCPError(fmt.Errorf(
		"call: %w", fmt.Errorf("boom")))
	require.ErrorContains(t, wrapped, "MCP call")
}

func TestHostPartsStripsPathsAndDedupes(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"github.com", "a.org"},
		hostParts([]string{"github.com/rust-lang", "github.com", "", "a.org"}))
	require.Nil(t, hostParts(nil))
}

func TestDateToAgeSecondsAndTruncate(t *testing.T) {
	t.Parallel()
	recent := time.Now().UTC().Add(-90 * time.Second).Format(time.RFC3339)
	require.InDelta(t, 90, dateToAgeSeconds(recent), 5)
	require.Positive(t, dateToAgeSeconds("2024-01-02"))
	require.Zero(t, dateToAgeSeconds("not a date"))
	require.Zero(t, dateToAgeSeconds(""))

	require.Equal(t, strings.Repeat("a", 500),
		truncateRunes(strings.Repeat("a", 600), 500))
	multibyte := strings.Repeat("é", 600)
	truncated := truncateRunes(multibyte, 500)
	require.Equal(t, 500, len([]rune(truncated)))
	require.True(t, utf8.ValidString(truncated))
}

func TestSearchRespectsNumResultsLimit(t *testing.T) {
	t.Parallel()
	rest := &restMock{status: http.StatusOK, body: `{"results":[
		{"title":"1","url":"https://1","summary":"s"},
		{"title":"2","url":"https://2","summary":"s"},
		{"title":"3","url":"https://3","summary":"s"}]}`}
	srv := rest.start(t)
	p := newProvider(srv.Client(), srv.URL, unusedMCPURL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		Limit:       2,
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 2)
	require.Equal(t, float64(2), rest.recorded()[0].body["numResults"])
}

func TestThrottlePacesSequentialSearches(t *testing.T) {
	// Not parallel: it mutates the global pacing other tests assume is 0.
	SetSearchDelayMs(120)
	t.Cleanup(func() {
		SetSearchDelayMs(0)
		resetSearchThrottle()
	})
	resetSearchThrottle()
	mock := newMCPMock(t, structuredResultFn())
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)
	broker := keylessBroker()

	start := time.Now()
	_, err := p.Search(context.Background(), search.SearchParams{
		Query: "a", AuthStorage: broker})
	require.NoError(t, err)
	_, err = p.Search(context.Background(), search.SearchParams{
		Query: "b", AuthStorage: broker})
	require.NoError(t, err)
	elapsed := time.Since(start)
	require.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestThrottleCancellationPropagates(t *testing.T) {
	// Not parallel: it mutates the global pacing other tests assume is 0.
	SetSearchDelayMs(10_000)
	t.Cleanup(func() {
		SetSearchDelayMs(0)
		resetSearchThrottle()
	})
	resetSearchThrottle()
	mock := newMCPMock(t, func() (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{StructuredContent: map[string]any{
			"results": []any{},
		}}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), unusedMCPURL, srv.URL)

	// Occupy the next slot, then cancel while the second search waits.
	ctx, cancel := context.WithCancel(context.Background())
	_, err := p.Search(ctx, search.SearchParams{
		Query: "a", AuthStorage: keylessBroker()})
	require.NoError(t, err)
	waitCtx, waitCancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		waitCancel()
	}()
	_, err = p.Search(waitCtx, search.SearchParams{
		Query: "b", AuthStorage: keylessBroker()})
	require.ErrorIs(t, err, context.Canceled)
	cancel()
}

func TestSearchAggregateErrorWrapsBoth(t *testing.T) {
	t.Parallel()
	rest := &restMock{status: http.StatusForbidden, body: "denied"}
	restSrv := rest.start(t)
	mock := newMCPMock(t, structuredResultFn())
	mock.setFail(http.StatusForbidden)
	mcpSrv := mock.start(t)
	p := newProvider(restSrv.Client(), restSrv.URL, mcpSrv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, search.ErrorCodeForbidden, spe.Code)
}
