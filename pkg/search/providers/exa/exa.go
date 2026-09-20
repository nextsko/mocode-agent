// Package exa implements the Exa web search provider with two paths: an
// authenticated REST path against api.exa.ai and an unauthenticated
// public MCP fallback driven by the official MCP Go SDK against
// mcp.exa.ai.
package exa

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/query"
)

// Default REST and public MCP endpoints.
const (
	exaAPIURL = "https://api.exa.ai/search"
	exaMCPURL = "https://mcp.exa.ai/mcp"

	// maxSnippetChars caps a source snippet like the TypeScript port.
	maxSnippetChars = 500

	// maxAnswerSummaries bounds summaries joined into the answer.
	maxAnswerSummaries = 3

	// defaultNumResults is the fallback result count.
	defaultNumResults = 10
)

// Provider implements search.ProviderV2 for Exa.
type Provider struct {
	httpClient *http.Client
	restURL    string
	mcpURL     string

	// mcpMu guards mcpSess, the cached MCP client session reused across
	// searches and rebuilt after transport failures.
	mcpMu   sync.Mutex
	mcpSess *mcpSessionCache
}

// New creates a provider over httpClient. A nil client selects a default
// transport; tests inject both a client and endpoint overrides via
// newProvider.
func New(httpClient *http.Client) *Provider {
	return newProvider(httpClient, exaAPIURL, exaMCPURL)
}

// newProvider wires concrete endpoints, used by tests to point both
// paths at httptest servers.
func newProvider(httpClient *http.Client, restURL, mcpURL string) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Provider{
		httpClient: httpClient,
		restURL:    restURL,
		mcpURL:     mcpURL,
	}
}

// Meta describes the provider registration entry. Actual registration is
// done by the integration layer, not by this package's init.
func Meta() search.ProviderMeta {
	return search.ProviderMeta{
		ID:       "exa",
		Label:    "Exa",
		Priority: 10,
		NewFunc:  func() search.ProviderV2 { return New(nil) },
	}
}

// ID implements search.ProviderV2.
func (p *Provider) ID() string { return "exa" }

// Label implements search.ProviderV2.
func (p *Provider) Label() string { return "Exa" }

// resolveOpts describes the credential lookup for this provider.
func resolveOpts(sessionID string) search.ResolveOpts {
	return search.ResolveOpts{
		EnvKey:    "EXA_API_KEY",
		AuthID:    "exa",
		SessionID: sessionID,
	}
}

// IsAvailable reports whether an API key is resolvable, gating automatic
// chain admission. Any resolution failure returns false without an error
// so the chain simply skips Exa.
func (p *Provider) IsAvailable(
	ctx context.Context,
	broker search.CredentialBroker,
) (bool, error) {
	if broker == nil {
		return false, nil
	}
	if _, err := broker.Resolve(ctx, "exa", resolveOpts("")); err != nil {
		return false, nil
	}
	return true, nil
}

// IsExplicitlyAvailable always reports true: an explicit user selection
// routes through the unauthenticated public MCP fallback even without a
// credential.
func (p *Provider) IsExplicitlyAvailable(
	ctx context.Context,
	broker search.CredentialBroker,
) (bool, error) {
	return true, nil
}

// searchInput is one decomposed search request shared by both paths.
type searchInput struct {
	query              string
	includeDomains     []string
	excludeDomains     []string
	startPublishedDate string
	endPublishedDate   string
	// numResults is the caller-requested cap, 0 when unspecified.
	numResults int
}

// buildInput maps SearchParams onto the provider input, decomposing
// Google-style directives: site:/-site: become domain filters (host part
// only) and after:/before: become ISO date bounds for the REST path.
func buildInput(params search.SearchParams) searchInput {
	num := params.NumSearchResults
	if num == 0 {
		num = params.Limit
	}
	parsed := query.ParseSearchQuery(params.Query)
	in := searchInput{query: parsed.Raw, numResults: num}
	if !parsed.HasDirectives {
		return in
	}
	in.query = query.FormatQuery(parsed, query.QuerySyntax{Phrases: true})
	in.includeDomains = hostParts(parsed.Sites)
	in.excludeDomains = hostParts(parsed.ExcludedSites)
	if parsed.After != nil {
		in.startPublishedDate = *parsed.After
	}
	if parsed.Before != nil {
		in.endPublishedDate = *parsed.Before
	}
	return in
}

// hostParts reduces site directives to bare hosts, deduplicated in order.
func hostParts(sites []string) []string {
	seen := make(map[string]struct{}, len(sites))
	out := make([]string, 0, len(sites))
	for _, site := range sites {
		if i := strings.IndexByte(site, '/'); i >= 0 {
			site = site[:i]
		}
		if site == "" {
			continue
		}
		if _, dup := seen[site]; dup {
			continue
		}
		seen[site] = struct{}{}
		out = append(out, site)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Search executes one Exa search. With a resolved key it drives the REST
// path first; on REST failure or without a key it falls back to the
// public MCP path. When both paths fail it returns an aggregate error.
func (p *Provider) Search(
	ctx context.Context,
	params search.SearchParams,
) (*search.SearchResponse, error) {
	in := buildInput(params)
	apiKey := ""
	if params.AuthStorage != nil {
		if key, err := params.AuthStorage.Resolve(ctx, "exa",
			resolveOpts(params.SessionID)); err == nil {
			apiKey = key
		}
	}
	if apiKey == "" {
		res, err := p.callMCP(ctx, "", in)
		if err != nil {
			return nil, fmt.Errorf("exa: %w", err)
		}
		return buildSearchResponse(res, in, "keyless"), nil
	}
	restRes, restErr := p.callREST(ctx, apiKey, in)
	if restErr == nil {
		return buildSearchResponse(restRes, in, "api_key"), nil
	}
	mcpRes, mcpErr := p.callMCP(ctx, apiKey, in)
	if mcpErr == nil {
		return buildSearchResponse(mcpRes, in, "api_key"), nil
	}
	return nil, fmt.Errorf(
		"exa: REST path failed: %w; MCP fallback failed: %w", restErr, mcpErr)
}

// searchError builds an unclassified provider error carrying the HTTP
// status when one exists.
func searchError(status int, message string) *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider: "exa", Message: message, HTTPStatus: status,
	}
}

// readAll drains a response body, guarding against runaway payloads.
func readAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// exaResult is one entry of the Exa search payload.
type exaResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Author        string   `json:"author"`
	PublishedDate string   `json:"publishedDate"`
	Text          string   `json:"text"`
	Highlights    []string `json:"highlights"`
	Summary       string   `json:"summary"`
}

// exaSearchResponse is the REST (and normalized MCP) search payload.
type exaSearchResponse struct {
	RequestID string      `json:"requestId"`
	Results   []exaResult `json:"results"`
}

// buildSearchResponse maps an Exa payload onto the unified response:
// only results with a URL become sources, snippets fall back through
// summary, text, then joined highlights, and the answer synthesizes the
// first non-empty summaries.
func buildSearchResponse(
	res *exaSearchResponse,
	in searchInput,
	authMode string,
) *search.SearchResponse {
	out := &search.SearchResponse{Provider: "exa", AuthMode: authMode, RequestID: res.RequestID}
	urlResults := make([]exaResult, 0, len(res.Results))
	for _, r := range res.Results {
		if r.URL == "" {
			continue
		}
		urlResults = append(urlResults, r)
		title := r.Title
		if title == "" {
			title = r.URL
		}
		out.Sources = append(out.Sources, search.SearchSource{
			Title:         title,
			URL:           r.URL,
			Snippet:       snippet(r),
			Author:        r.Author,
			PublishedDate: r.PublishedDate,
			AgeSeconds:    dateToAgeSeconds(r.PublishedDate),
		})
	}
	if in.numResults > 0 && len(out.Sources) > in.numResults {
		out.Sources = out.Sources[:in.numResults]
	}
	if answer := synthesizeAnswer(urlResults); answer != "" {
		out.Answer = answer
	}
	return out
}

// snippetFallbacks lists the snippet candidate fields in priority order.
func snippetFallbacks(r exaResult) []string {
	return []string{r.Summary, r.Text, strings.Join(r.Highlights, " ")}
}

// synthesizeAnswer joins up to three "**title**: summary" parts from the
// URL-bearing results, falling back to the URL then "Untitled" for
// titles. It returns "" when no result carries a summary.
func synthesizeAnswer(results []exaResult) string {
	parts := make([]string, 0, maxAnswerSummaries)
	for _, r := range results {
		if len(parts) >= maxAnswerSummaries {
			break
		}
		summary := strings.TrimSpace(r.Summary)
		if summary == "" {
			continue
		}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = r.URL
		}
		if title == "" {
			title = "Untitled"
		}
		parts = append(parts, fmt.Sprintf("**%s**: %s", title, summary))
	}
	return strings.Join(parts, "\n\n")
}

// snippet returns the first non-empty snippet candidate, truncated.
func snippet(r exaResult) string {
	for _, candidate := range snippetFallbacks(r) {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return truncateRunes(candidate, maxSnippetChars)
		}
	}
	return ""
}

// truncateRunes clamps s to at most max runes.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// dateLayouts lists the publish-date formats Exa emits.
var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// dateToAgeSeconds converts a publish date to its age in seconds,
// returning 0 for absent or unparseable dates.
func dateToAgeSeconds(date string) int64 {
	if date == "" {
		return 0
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, date); err == nil {
			return int64(time.Since(t) / time.Second)
		}
	}
	return 0
}

// creditBodyPattern matches billing-exhaustion signals in response
// bodies, mirroring the chain-level classifier.
var creditBodyPattern = regexp.MustCompile(
	`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

// classifyRESTHTTP maps a REST status and body onto a classified provider
// error, or nil when nothing matches.
func classifyRESTHTTP(status int, body string) *search.SearchProviderError {
	if creditBodyPattern.MatchString(body) {
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeBilling,
			Message: "credits exhausted", HTTPStatus: status,
		}
	}
	switch status {
	case 402:
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeBilling,
			Message: "402 credits exhausted", HTTPStatus: status,
		}
	case 401:
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeUnauthorized,
			Message: "401 unauthorized", HTTPStatus: status,
		}
	case 403:
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeForbidden,
			Message: "403 forbidden", HTTPStatus: status,
		}
	case 429:
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeRateLimited,
			Message: "429 rate limited", HTTPStatus: status,
		}
	}
	if status >= 500 {
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeServerError,
			Message: fmt.Sprintf("%d server error", status), HTTPStatus: status,
		}
	}
	return nil
}

// classifyMCPHTTP maps an MCP HTTP failure onto a classified provider
// error. Keyless 429s carry the hint pointing users at an API key.
func classifyMCPHTTP(status int, body string) *search.SearchProviderError {
	if creditBodyPattern.MatchString(body) {
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeBilling,
			Message: "credits exhausted", HTTPStatus: status,
		}
	}
	if status == 429 {
		return &search.SearchProviderError{
			Provider: "exa", Code: search.ErrorCodeRateLimited,
			Message: "MCP rate limit reached (429); configure an Exa API key" +
				" for higher limits",
			HTTPStatus: status,
		}
	}
	if spe := classifyRESTHTTP(status, body); spe != nil {
		return spe
	}
	return &search.SearchProviderError{
		Provider: "exa",
		Message: fmt.Sprintf("Exa MCP request failed (%d): %s",
			status, truncateRunes(body, maxSnippetChars)),
		HTTPStatus: status,
	}
}
