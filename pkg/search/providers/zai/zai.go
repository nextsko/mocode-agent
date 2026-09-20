// Package zai implements the Z.AI web search provider on top of the
// official MCP Go SDK: a cached Streamable HTTP client session carrying
// the Bearer credential, the three-stage argument retry the upstream
// web_search_prime endpoint needs, and the layered payload extraction
// of the TypeScript port.
package zai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/query"
)

// zaiMCPURL is the remote MCP endpoint of the webSearchPrime server.
const zaiMCPURL = "https://api.z.ai/api/mcp/web_search_prime/mcp"

// zaiToolName is the search tool exposed by the remote endpoint.
const zaiToolName = "web_search_prime"

// defaultNumResults is the fallback result count, mirroring the
// TypeScript port.
const defaultNumResults = 10

// missingKeyMessage mirrors the TypeScript port's withAuth hint.
const missingKeyMessage = "Z.AI credentials not found. Set ZAI_API_KEY."

// zaiQuerySyntax mirrors the TypeScript port: webSearchPrime exposes no
// native filter arguments, but its Bing-flavored backend parses the
// common inline operators. Dates and language are left to the central
// lenient post-filter.
var zaiQuerySyntax = query.QuerySyntax{
	Phrases:  true,
	Negation: true,
	Site:     true,
	InTitle:  true,
	InURL:    true,
	Filetype: true,
}

// Provider implements search.ProviderV2 for Z.AI.
type Provider struct {
	httpClient *http.Client
	mcpURL     string

	// mcpMu guards mcpSess, the cached MCP client session reused across
	// searches and rebuilt after failures or credential changes.
	mcpMu   sync.Mutex
	mcpSess *mcpSessionCache
}

// New creates a provider over httpClient. A nil client selects a
// default transport; tests inject both a client and an endpoint
// override via newProvider.
func New(httpClient *http.Client) *Provider {
	return newProvider(httpClient, zaiMCPURL)
}

// newProvider wires a concrete endpoint, used by tests to point the
// provider at an httptest server.
func newProvider(httpClient *http.Client, mcpURL string) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Provider{httpClient: httpClient, mcpURL: mcpURL}
}

// Meta describes the provider registration entry. Actual registration
// is done by the integration layer, not by this package's init.
func Meta() search.ProviderMeta {
	return search.ProviderMeta{
		ID:       "zai",
		Label:    "Z.AI",
		Priority: 15,
		NewFunc:  func() search.ProviderV2 { return New(nil) },
	}
}

// ID implements search.ProviderV2.
func (p *Provider) ID() string { return "zai" }

// Label implements search.ProviderV2.
func (p *Provider) Label() string { return "Z.AI" }

// resolveOpts describes the credential lookup for this provider.
func resolveOpts(sessionID string) search.ResolveOpts {
	return search.ResolveOpts{
		EnvKey:    "ZAI_API_KEY",
		AuthID:    "zai",
		SessionID: sessionID,
	}
}

// IsAvailable reports whether a Z.AI credential is resolvable, gating
// automatic chain admission. Any resolution failure returns false
// without an error so the chain simply skips Z.AI.
func (p *Provider) IsAvailable(
	ctx context.Context,
	broker search.CredentialBroker,
) (bool, error) {
	if broker == nil {
		return false, nil
	}
	if _, err := broker.Resolve(ctx, "zai", resolveOpts("")); err != nil {
		return false, nil
	}
	return true, nil
}

// IsExplicitlyAvailable mirrors IsAvailable: the remote endpoint has no
// unauthenticated mode, so an explicit user selection still requires a
// credential, as in the TypeScript port.
func (p *Provider) IsExplicitlyAvailable(
	ctx context.Context,
	broker search.CredentialBroker,
) (bool, error) {
	return p.IsAvailable(ctx, broker)
}

// zaiCredentialError reports the missing-key failure with the hint the
// TypeScript port surfaces through withAuth.
func zaiCredentialError() *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider: "zai",
		Code:     search.ErrorCodeUnauthorized,
		Message:  missingKeyMessage,
	}
}

// searchInput is one decomposed search request.
type searchInput struct {
	query      string
	numResults int
}

// buildInput maps SearchParams onto the provider input. The endpoint
// takes no native filter arguments, so directives are re-emitted as
// inline query syntax it understands; the count falls back through
// NumSearchResults, Limit, then the default.
func buildInput(params search.SearchParams) searchInput {
	num := params.NumSearchResults
	if num == 0 {
		num = params.Limit
	}
	if num == 0 {
		num = defaultNumResults
	}
	parsed := query.ParseSearchQuery(params.Query)
	q := params.Query
	if parsed.HasDirectives {
		q = query.FormatQuery(parsed, zaiQuerySyntax)
	}
	return searchInput{query: q, numResults: num}
}

// Search executes one Z.AI search through the remote MCP endpoint. A
// credential is mandatory; the tool call runs under the three-stage
// argument retry and the payload is extracted from whichever layer of
// the result carries it.
func (p *Provider) Search(
	ctx context.Context,
	params search.SearchParams,
) (*search.SearchResponse, error) {
	apiKey := ""
	if params.AuthStorage != nil {
		if key, err := params.AuthStorage.Resolve(
			ctx, "zai", resolveOpts(params.SessionID)); err == nil {
			apiKey = key
		}
	}
	if apiKey == "" {
		return nil, zaiCredentialError()
	}
	in := buildInput(params)
	result, err := p.callWebSearchPrime(ctx, apiKey, in)
	if err != nil {
		return nil, fmt.Errorf("zai: %w", err)
	}
	payload, err := parseSearchPayload(result)
	if err != nil {
		return nil, fmt.Errorf("zai: %w", err)
	}
	return buildSearchResponse(payload, in), nil
}

// buildSearchResponse maps the extracted payload onto the unified
// response: results without a URL are dropped, sources are clamped to
// the requested count, and unparsable text parts surface as the answer.
func buildSearchResponse(
	payload *zaiPayload,
	in searchInput,
) *search.SearchResponse {
	out := &search.SearchResponse{
		Provider:  "zai",
		Answer:    payload.Answer,
		RequestID: payload.RequestID,
		AuthMode:  "api_key",
		Sources:   toSources(payload.Results),
	}
	if in.numResults > 0 && len(out.Sources) > in.numResults {
		out.Sources = out.Sources[:in.numResults]
	}
	return out
}

// asNonEmpty trims s and returns it when non-empty, else "".
func asNonEmpty(s string) string {
	return strings.TrimSpace(s)
}
