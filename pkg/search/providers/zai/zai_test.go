package zai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
)

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

// keylessBroker is the no-credential stub.
func keylessBroker() stubBroker {
	return stubBroker{err: search.ErrCredentialNotFound}
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
// and can be flipped into a raw HTTP failure mode.
type mcpMock struct {
	server *mcp.Server

	// failStatus, when > 0, makes the wrapper answer every request with
	// that HTTP status instead of forwarding it to the MCP handler.
	failStatus atomic.Int64

	mu      sync.Mutex
	records []mcpRequestRecord
}

// newMCPMock builds a mock whose web_search_prime tool replies through
// toolFn with the decoded arguments.
func newMCPMock(
	t *testing.T,
	toolFn func(args map[string]any) (*mcp.CallToolResult, error),
) *mcpMock {
	t.Helper()
	server := mcp.NewServer(
		&mcp.Implementation{Name: "zai-mock", Version: "0"}, nil)
	m := &mcpMock{server: server}
	server.AddTool(&mcp.Tool{
		Name:        zaiToolName,
		InputSchema: &jsonschema.Schema{Type: "object"},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		return toolFn(args)
	})
	return m
}

// setFail flips the wrapper into raw HTTP failure mode.
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

// toolCalls returns the recorded tools/call requests.
func (m *mcpMock) toolCalls() []mcpRequestRecord {
	var calls []mcpRequestRecord
	for _, rec := range m.recorded() {
		if rec.method == "tools/call" {
			calls = append(calls, rec)
		}
	}
	return calls
}

// toolArguments returns the arguments of the n-th tools/call request.
func (m *mcpMock) toolArguments(t *testing.T, n int) map[string]any {
	t.Helper()
	calls := m.toolCalls()
	require.Greater(t, len(calls), n, "missing tools/call %d", n)
	params, ok := calls[n].body["params"].(map[string]any)
	require.True(t, ok, "tools/call params missing")
	args, ok := params["arguments"].(map[string]any)
	require.True(t, ok, "tools/call arguments missing")
	return args
}

func stringOrNil(v any) string {
	s, _ := v.(string)
	return s
}

// scriptedTool records every tool invocation and answers through fn
// with the zero-based call index.
type scriptedTool struct {
	mu    sync.Mutex
	calls []map[string]any
	fn    func(call int, args map[string]any) (*mcp.CallToolResult, error)
}

func (s *scriptedTool) handle(
	args map[string]any,
) (*mcp.CallToolResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, args)
	call := len(s.calls) - 1
	s.mu.Unlock()
	return s.fn(call, args)
}

func (s *scriptedTool) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// argumentErrorResult replies with the tool-level IsError shape Z.AI
// emits for rejected argument names.
func argumentErrorResult(field string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
			"MCP error 400: Invalid argument: unknown field `%s`", field)}},
	}
}

// structuredResult replies with a structuredContent search payload.
func structuredResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		StructuredContent: map[string]any{
			"request_id": "req-1",
			"search_result": []any{map[string]any{
				"title":        "Go",
				"link":         "https://go.dev",
				"content":      "Go language site",
				"publish_date": "2024-01-02",
				"media":        "go.dev",
			}},
		},
	}
}

func TestMetaAndProviderIdentity(t *testing.T) {
	t.Parallel()
	meta := Meta()
	require.Equal(t, "zai", meta.ID)
	require.Equal(t, "Z.AI", meta.Label)
	require.NotNil(t, meta.NewFunc)
	provider := New(nil)
	require.Equal(t, "zai", provider.ID())
	require.Equal(t, "Z.AI", provider.Label())
	var _ search.ProviderV2 = provider
}

func TestIsAvailableRequiresCredential(t *testing.T) {
	t.Parallel()
	p := New(nil)
	ok, err := p.IsAvailable(context.Background(), stubBroker{key: "k1"})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = p.IsAvailable(context.Background(),
		stubBroker{err: fmt.Errorf("wrapped: %w", search.ErrCredentialNotFound)})
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = p.IsAvailable(context.Background(), nil)
	require.NoError(t, err)
	require.False(t, ok)

	// No keyless mode: the explicit probe mirrors the automatic one.
	ok, err = p.IsExplicitlyAvailable(context.Background(), keylessBroker())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSearchFirstAttemptShape(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, "zai", resp.Provider)
	require.Equal(t, "api_key", resp.AuthMode)
	require.Equal(t, "req-1", resp.RequestID)
	require.Len(t, resp.Sources, 1)
	first := resp.Sources[0]
	require.Equal(t, "Go", first.Title)
	require.Equal(t, "https://go.dev", first.URL)
	require.Equal(t, "Go language site", first.Snippet)
	require.Equal(t, "2024-01-02", first.PublishedDate)
	require.Equal(t, "go.dev", first.Author)
	require.Positive(t, first.AgeSeconds)

	calls := mock.toolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "Bearer k1", calls[0].header.Get("Authorization"))
	require.Contains(t, calls[0].header.Get("Accept"), "application/json")
	args := mock.toolArguments(t, 0)
	require.Equal(t, "golang", args["query"])
	require.Equal(t, float64(10), args["count"])
	require.NotContains(t, args, "search_query")
	require.Equal(t, 1, mock.countMethod("initialize"))
}

func TestSearchDirectivesReformatted(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang site:go.dev -site:spam.com \"exact phrase\"",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	sent := mock.toolArguments(t, 0)["query"].(string)
	require.Contains(t, sent, "golang")
	require.Contains(t, sent, "site:go.dev")
	require.Contains(t, sent, "-site:spam.com")
	require.Contains(t, sent, `"exact phrase"`)

	// Directive-free queries pass through verbatim.
	_, err = p.Search(context.Background(), search.SearchParams{
		Query:       "plain query",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, "plain query", mock.toolArguments(t, 1)["query"])
}

func TestSearchRetryFallsBackOnToolArgumentError(t *testing.T) {
	t.Parallel()
	script := &scriptedTool{fn: func(
		call int,
		args map[string]any,
	) (*mcp.CallToolResult, error) {
		if _, ok := args["query"]; ok {
			return argumentErrorResult("query"), nil
		}
		return structuredResult(), nil
	}}
	mock := newMCPMock(t, script.handle)
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, 2, mock.countMethod("tools/call"))

	first := mock.toolArguments(t, 0)
	require.Equal(t, "golang", first["query"])
	require.NotContains(t, first, "search_query")
	second := mock.toolArguments(t, 1)
	require.Equal(t, "golang", second["search_query"])
	require.Equal(t, float64(10), second["count"])
	require.NotContains(t, second, "query")
	require.NotContains(t, second, "search_engine")
}

func TestSearchRetryReachesThirdAttempt(t *testing.T) {
	t.Parallel()
	script := &scriptedTool{fn: func(
		call int,
		args map[string]any,
	) (*mcp.CallToolResult, error) {
		if _, ok := args["search_engine"]; !ok {
			return argumentErrorResult("search_engine"), nil
		}
		return structuredResult(), nil
	}}
	mock := newMCPMock(t, script.handle)
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, 3, mock.countMethod("tools/call"))
	third := mock.toolArguments(t, 2)
	require.Equal(t, "golang", third["search_query"])
	require.Equal(t, "search-prime", third["search_engine"])
	require.Equal(t, float64(10), third["count"])
}

func TestSearchAllAttemptsFailReturnsLastError(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return argumentErrorResult("query"), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.Nil(t, resp)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, 400, spe.HTTPStatus)
	require.Contains(t, spe.Message, "Invalid argument")
	require.Equal(t, 3, mock.countMethod("tools/call"))
}

func TestSearchNonArgumentErrorDoesNotRetry(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: "MCP error 429: rate limit exceeded",
			}},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, 429, spe.HTTPStatus)
	require.Equal(t, 1, mock.countMethod("tools/call"))
}

func TestSearchRPCErrorRetries(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		if _, ok := args["query"]; ok {
			return nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: "Invalid params: unknown argument `query`",
			}
		}
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, 2, mock.countMethod("tools/call"))
	require.Equal(t, "golang", mock.toolArguments(t, 1)["search_query"])
}

func TestSearchHTTP401ClassifiedNoRetry(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	mock.setFail(http.StatusUnauthorized)
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "golang",
		AuthStorage: stubBroker{key: "bad"},
	})
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, search.ErrorCodeUnauthorized, spe.Code)
	require.Equal(t, 401, spe.HTTPStatus)
	require.Zero(t, mock.countMethod("tools/call"))
}

// TestPayloadShapes covers the layered payload extraction on top of the
// SDK's protocol layer: structuredContent (both field spellings, nested
// data), text content carrying single- or double-encoded JSON, and the
// answer assembly over non-result text parts.
func TestPayloadShapes(t *testing.T) {
	t.Parallel()
	resultJSON := `{"title":"T","link":"https://x.dev","content":"S"}`
	shapes := map[string]func() (*mcp.CallToolResult, error){
		"structured search_result": func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"search_result": []any{map[string]any{
						"title": "T", "link": "https://x.dev", "content": "S",
					}},
				},
			}, nil
		},
		"structured results": func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"requestId": "req-2",
					"results": []any{map[string]any{
						"title": "T", "url": "https://x.dev", "content": "S",
					}},
				},
			}, nil
		},
		"structured nested data": func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"data": map[string]any{
						"search_result": []any{map[string]any{
							"title": "T", "link": "https://x.dev", "content": "S",
						}},
					},
				},
			}, nil
		},
		"text json": func() (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{
					Text: `{"search_result":[` + resultJSON + `]}`,
				}},
			}, nil
		},
		"text double-encoded json": func() (*mcp.CallToolResult, error) {
			inner, err := json.Marshal(
				`{"search_result":[` + resultJSON + `]}`)
			require.NoError(t, err)
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{
					Text: string(inner),
				}},
			}, nil
		},
	}
	for name, resultFn := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
				return resultFn()
			})
			srv := mock.start(t)
			p := newProvider(srv.Client(), srv.URL)
			resp, err := p.Search(context.Background(), search.SearchParams{
				Query:       "q",
				AuthStorage: stubBroker{key: "k1"},
			})
			require.NoError(t, err)
			require.Len(t, resp.Sources, 1)
			require.Equal(t, "T", resp.Sources[0].Title)
			require.Equal(t, "https://x.dev", resp.Sources[0].URL)
			require.Equal(t, "S", resp.Sources[0].Snippet)
		})
	}
}

func TestPayloadRequestIDSpellings(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"request_id": "req-3",
		"requestId":  "req-4",
		"id":         "id-5",
	}
	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			obj := map[string]any{
				"search_result": []any{map[string]any{
					"title": "T", "link": "https://x.dev",
				}},
			}
			obj[key] = want
			mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{
					StructuredContent: obj,
				}, nil
			})
			srv := mock.start(t)
			p := newProvider(srv.Client(), srv.URL)
			resp, err := p.Search(context.Background(), search.SearchParams{
				Query:       "q",
				AuthStorage: stubBroker{key: "k1"},
			})
			require.NoError(t, err)
			require.Equal(t, want, resp.RequestID)
		})
	}
}

func TestPayloadAnswerJoinsNonResultText(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: `{"search_result":[` +
					`{"title":"T","link":"https://x.dev"}]}`},
				&mcp.TextContent{Text: "First summary paragraph."},
				&mcp.TextContent{Text: "  Second angle.  "},
			},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, "First summary paragraph.\n\nSecond angle.", resp.Answer)
}

func TestPayloadProseOnlyYieldsAnswerWithoutSources(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "No results, only prose."},
			},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Sources)
	require.Equal(t, "No results, only prose.", resp.Answer)
}

func TestDirectErrorPayload(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				"code":    1001,
				"success": false,
				"msg":     "invalid api token",
			},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Z.AI API error (1001): invalid api token")
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, 1001, spe.HTTPStatus)
}

// TestSessionReuseAndRebuild asserts the cached session behavior: a
// healthy session is reused (initialize happens once), any transport
// failure drops it, and the next search reconnects.
func TestSessionReuseAndRebuild(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)
	broker := stubBroker{key: "k1"}

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

	mock.setFail(http.StatusInternalServerError)
	require.Error(t, search())
	require.Nil(t, p.mcpSess, "failed call must drop the session")

	mock.clearFail()
	require.NoError(t, search())
	require.Equal(t, 2, mock.countMethod("initialize"),
		"recovery must reconnect exactly once more")
	require.Equal(t, 3, mock.countMethod("tools/call"))
}

// TestSessionCredentialSwitchReconnects verifies a credential change
// drops the cached session and reconnects with the new Authorization.
func TestSessionCredentialSwitchReconnects(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query: "q", AuthStorage: stubBroker{key: "k1"}})
	require.NoError(t, err)
	_, err = p.Search(context.Background(), search.SearchParams{
		Query: "q", AuthStorage: stubBroker{key: "k2"}})
	require.NoError(t, err)

	require.Equal(t, 2, mock.countMethod("initialize"),
		"credential change must force a reconnect")
	calls := mock.toolCalls()
	require.Len(t, calls, 2)
	require.Equal(t, "Bearer k1", calls[0].header.Get("Authorization"))
	require.Equal(t, "Bearer k2", calls[1].header.Get("Authorization"))
}

func TestSearchNoCredential(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:       "q",
		AuthStorage: keylessBroker(),
	})
	require.Nil(t, resp)
	require.Error(t, err)
	require.Contains(t, err.Error(), missingKeyMessage)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, search.ErrorCodeUnauthorized, spe.Code)
	require.Zero(t, mock.countMethod("tools/call"))
}

func TestSearchTruncatesSources(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			StructuredContent: map[string]any{
				"search_result": []any{
					map[string]any{"title": "1", "link": "https://1"},
					map[string]any{"title": "2", "link": "https://2"},
					map[string]any{"title": "3", "link": "https://3"},
				},
			},
		}, nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query:            "q",
		NumSearchResults: 2,
		AuthStorage:      stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 2)
	require.Equal(t, float64(2), mock.toolArguments(t, 0)["count"])
}

// TestSearchCountMapping pins the count resolution: NumSearchResults
// wins over Limit, Limit alone applies, and the default is 10.
func TestSearchCountMapping(t *testing.T) {
	t.Parallel()
	mock := newMCPMock(t, func(args map[string]any) (*mcp.CallToolResult, error) {
		return structuredResult(), nil
	})
	srv := mock.start(t)
	p := newProvider(srv.Client(), srv.URL)

	_, err := p.Search(context.Background(), search.SearchParams{
		Query:            "a",
		NumSearchResults: 3,
		Limit:            5,
		AuthStorage:      stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, float64(3), mock.toolArguments(t, 0)["count"])

	_, err = p.Search(context.Background(), search.SearchParams{
		Query:       "b",
		Limit:       4,
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, float64(4), mock.toolArguments(t, 1)["count"])

	_, err = p.Search(context.Background(), search.SearchParams{
		Query:       "c",
		AuthStorage: stubBroker{key: "k1"},
	})
	require.NoError(t, err)
	require.Equal(t, float64(10), mock.toolArguments(t, 2)["count"])
}

func TestBuildAttempts(t *testing.T) {
	t.Parallel()
	attempts := buildAttempts("q1", 7)
	require.Len(t, attempts, 3)
	require.Equal(t, map[string]any{"query": "q1", "count": 7}, attempts[0])
	require.Equal(t,
		map[string]any{"search_query": "q1", "count": 7}, attempts[1])
	require.Equal(t, map[string]any{
		"search_query":  "q1",
		"search_engine": "search-prime",
		"count":         7,
	}, attempts[2])
}

func TestIsArgumentError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"status 400", &search.SearchProviderError{
			HTTPStatus: 400}, true},
		{"invalid keyword", &search.SearchProviderError{
			Message: "Invalid params"}, true},
		{"argument keyword", &search.SearchProviderError{
			Message: "unknown argument"}, true},
		{"search_query keyword", &search.SearchProviderError{
			Message: "missing search_query"}, true},
		{"query keyword", &search.SearchProviderError{
			Message: "query field rejected"}, true},
		{"keyword matching is lowercase", &search.SearchProviderError{
			Message: "INVALID"}, true},
		{"rate limited", &search.SearchProviderError{
			HTTPStatus: 429, Message: "rate limit"}, false},
		{"unauthorized", &search.SearchProviderError{
			HTTPStatus: 401, Message: "unauthorized"}, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isArgumentError(tc.err))
		})
	}
	// Wrapped provider errors still qualify through errors.As.
	wrapped := fmt.Errorf("call: %w",
		&search.SearchProviderError{HTTPStatus: 400})
	require.True(t, isArgumentError(wrapped))
}

func TestToSources(t *testing.T) {
	t.Parallel()
	recent := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	results := []zaiResult{
		{Title: "A", Link: "https://a", URL: "https://ignored",
			Content: "ca", Media: "ma", PublishDate: recent},
		{Title: "", URL: "https://b", PublishedDate: "2024-01-02"},
		{Title: "NoLink", Content: "dropped"},
		{Link: "   ", URL: "  ", Title: "BlankValues", Content: "x"},
	}
	sources := toSources(results)
	require.Len(t, sources, 2)
	require.Equal(t, "https://a", sources[0].URL)
	require.Equal(t, "A", sources[0].Title)
	require.Equal(t, "ca", sources[0].Snippet)
	require.Equal(t, "ma", sources[0].Author)
	require.Equal(t, recent, sources[0].PublishedDate)
	require.Positive(t, sources[0].AgeSeconds)
	require.Equal(t, "https://b", sources[1].URL)
	require.Equal(t, "https://b", sources[1].Title)
	require.Equal(t, "2024-01-02", sources[1].PublishedDate)
	require.Positive(t, sources[1].AgeSeconds)
}

func TestDateToAgeSeconds(t *testing.T) {
	t.Parallel()
	recent := time.Now().UTC().Add(-90 * time.Second).Format(time.RFC3339)
	require.InDelta(t, 90, dateToAgeSeconds(recent), 5)
	require.Positive(t, dateToAgeSeconds("2024-01-02"))
	require.Zero(t, dateToAgeSeconds("not a date"))
	require.Zero(t, dateToAgeSeconds(""))
}

func TestClassifyZAIHTTP(t *testing.T) {
	t.Parallel()
	billing := classifyZAIHTTP(400, "quota exceeded")
	require.Equal(t, search.ErrorCodeBilling, billing.Code)
	require.Equal(t, search.ErrorCodeBilling,
		classifyZAIHTTP(402, "").Code)
	require.Equal(t, search.ErrorCodeUnauthorized,
		classifyZAIHTTP(401, "").Code)
	require.Equal(t, search.ErrorCodeForbidden,
		classifyZAIHTTP(403, "").Code)
	generic := classifyZAIHTTP(500, "boom")
	require.Empty(t, generic.Code)
	require.Equal(t, 500, generic.HTTPStatus)
	require.Contains(t, generic.Message, "Z.AI MCP error (500)")
}

// TestMapZAIError pins classification of the wrapped error types the
// SDK surfaces: captured HTTP statuses and JSON-RPC protocol errors.
func TestMapZAIError(t *testing.T) {
	t.Parallel()
	spe, ok := mapZAIError(&zaiHTTPError{
		Status: http.StatusUnauthorized, Body: ""}).(*search.SearchProviderError)
	require.True(t, ok)
	require.Equal(t, search.ErrorCodeUnauthorized, spe.Code)

	spe, ok = mapZAIError(&jsonrpc.Error{
		Code:    jsonrpc.CodeInvalidParams,
		Message: "Invalid params",
	}).(*search.SearchProviderError)
	require.True(t, ok)
	require.Equal(t, -32602, spe.HTTPStatus)
	require.Contains(t, spe.Message, "Z.AI MCP error (-32602)")

	wrapped := mapZAIError(fmt.Errorf("sending: %w", errors.New("boom")))
	require.ErrorContains(t, wrapped, "MCP call")
}
