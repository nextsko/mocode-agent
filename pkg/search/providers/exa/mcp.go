package exa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tidwall/gjson"
)

// mcpSourceHeader identifies the caller to the public MCP endpoint, as
// the TypeScript port does.
const mcpSourceHeader = "x-exa-source"

// mcpSourceValue is the source identifier sent upstream.
const mcpSourceValue = "oh-my-pi"

// mcpSearchTool is the tool the public MCP endpoint filters for.
const mcpSearchTool = "web_search_exa"

// mcpClientName and mcpClientVersion identify this agent during the MCP
// initialize handshake.
const (
	mcpClientName    = "mocode-agent"
	mcpClientVersion = "0"
)

// mcpErrorBodyLimit caps how much of an HTTP error body is captured for
// classification.
const mcpErrorBodyLimit = 1 << 20

// mcpArguments is the arguments object of the tools/call request. Domain
// and date directives ride inline in the query because the public tool
// understands site:/-site:/after:/before: operators.
type mcpArguments struct {
	Query      string `json:"query"`
	NumResults int    `json:"numResults,omitempty"`
}

// buildMCPArguments inlines the input's domain and date filters into the
// query string next to the free-text query.
func buildMCPArguments(in searchInput) mcpArguments {
	parts := make([]string, 0, 5)
	parts = append(parts, in.query)
	for _, domain := range in.includeDomains {
		if d := strings.TrimSpace(domain); d != "" {
			parts = append(parts, "site:"+d)
		}
	}
	for _, domain := range in.excludeDomains {
		if d := strings.TrimSpace(domain); d != "" {
			parts = append(parts, "-site:"+d)
		}
	}
	if in.startPublishedDate != "" {
		parts = append(parts, "after:"+in.startPublishedDate)
	}
	if in.endPublishedDate != "" {
		parts = append(parts, "before:"+in.endPublishedDate)
	}
	args := mcpArguments{Query: strings.Join(parts, " ")}
	if in.numResults > 0 {
		args.NumResults = in.numResults
	}
	return args
}

// mcpEndpoint builds the public MCP endpoint URL. An empty apiKey keeps
// the request keyless: no exaApiKey query parameter is emitted, riding
// the public per-IP quota.
func mcpEndpoint(base, apiKey string) string {
	q := url.Values{}
	if apiKey != "" {
		q.Set("exaApiKey", apiKey)
	}
	q.Set("tools", mcpSearchTool)
	return base + "?" + q.Encode()
}

// mcpHTTPError carries the HTTP status and body of a failed endpoint
// request. The SDK's transport errors do not surface the numeric status,
// so mcpTransport captures it at the RoundTripper level instead.
type mcpHTTPError struct {
	Status int
	Body   string
}

func (e *mcpHTTPError) Error() string {
	return fmt.Sprintf("Exa MCP request failed (%d): %s", e.Status, e.Body)
}

// mcpTransport injects the x-exa-source header on every request and
// converts failing HTTP statuses into *mcpHTTPError values so they can
// be classified downstream.
type mcpTransport struct {
	next http.RoundTripper
}

func (t mcpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set(mcpSourceHeader, mcpSourceValue)
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, mcpErrorBodyLimit))
		resp.Body.Close()
		return nil, &mcpHTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	return resp, nil
}

// mcpSessionCache holds one connected MCP client session bound to the
// endpoint it was opened against, so a credential change (keyless vs
// keyed) forces a reconnect on the next call.
type mcpSessionCache struct {
	endpoint string
	session  *mcp.ClientSession
	cancel   context.CancelFunc
}

// callMCP executes the search through the public MCP endpoint using the
// official MCP Go SDK. The ClientSession is cached on the provider and
// reused while healthy; any Connect or CallTool failure drops it so the
// next call reconnects. HTTP failures are classified with the keyless
// 429 hint; tool-level errors (result.isError) surface the first
// content text.
func (p *Provider) callMCP(
	ctx context.Context,
	apiKey string,
	in searchInput,
) (*exaSearchResponse, error) {
	endpoint := mcpEndpoint(p.mcpURL, apiKey)
	if err := waitForSearchSlot(ctx); err != nil {
		return nil, err
	}
	session, err := p.mcpSession(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      mcpSearchTool,
		Arguments: buildMCPArguments(in),
	})
	if err != nil {
		p.dropMCPSession(endpoint)
		return nil, mapMCPError(err)
	}
	if result.IsError {
		return nil, mcpToolError(result)
	}
	if out := mcpStructuredResult(result); out != nil {
		return out, nil
	}
	if out := mcpTextJSONResult(result); out != nil {
		return out, nil
	}
	if parsed := parseExaMcpTextPayload(result); parsed != nil {
		return parsed, nil
	}
	return nil, errors.New(
		"exa: MCP search returned unexpected response shape")
}

// mcpSession returns a connected ClientSession for endpoint, reusing the
// cached session while its endpoint matches. The session's context is
// detached from the triggering request (WithoutCancel) so it survives
// across calls; its lifetime ends at dropMCPSession.
func (p *Provider) mcpSession(
	ctx context.Context,
	endpoint string,
) (*mcp.ClientSession, error) {
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	if p.mcpSess != nil && p.mcpSess.endpoint == endpoint {
		return p.mcpSess.session, nil
	}
	p.closeMCPSessionLocked()
	sessCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint,
		HTTPClient: &http.Client{
			Transport: mcpTransport{next: p.httpClient.Transport},
		},
		// The public endpoint never pushes server-initiated messages;
		// skipping the standalone SSE stream keeps the session to plain
		// request/response POSTs.
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    mcpClientName,
		Version: mcpClientVersion,
	}, nil)
	session, err := client.Connect(sessCtx, transport, nil)
	if err != nil {
		cancel()
		return nil, mapMCPError(err)
	}
	p.mcpSess = &mcpSessionCache{
		endpoint: endpoint,
		session:  session,
		cancel:   cancel,
	}
	return session, nil
}

// dropMCPSession discards the cached session for endpoint, if any, so
// the next call reconnects.
func (p *Provider) dropMCPSession(endpoint string) {
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	if p.mcpSess != nil && p.mcpSess.endpoint == endpoint {
		p.closeMCPSessionLocked()
	}
}

// closeMCPSessionLocked tears down any cached session; mcpMu must be
// held.
func (p *Provider) closeMCPSessionLocked() {
	cached := p.mcpSess
	p.mcpSess = nil
	if cached != nil {
		cached.cancel()
		_ = cached.session.Close()
	}
}

// mapMCPError converts SDK transport and protocol failures onto
// classified provider errors, keeping the 429 hint for keyless rate
// limits.
func mapMCPError(err error) error {
	var httpErr *mcpHTTPError
	if errors.As(err, &httpErr) {
		return classifyMCPHTTP(httpErr.Status, httpErr.Body)
	}
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		return searchError(0, fmt.Sprintf("MCP error: %s", rpcErr.Message))
	}
	return fmt.Errorf("exa: MCP call: %w", err)
}

// mcpToolError extracts the failure message of an IsError tool result
// from its first text content block.
func mcpToolError(result *mcp.CallToolResult) error {
	for _, text := range mcpContentTexts(result) {
		if text != "" {
			return searchError(0, text)
		}
	}
	return searchError(0, "Exa MCP returned an error")
}

// mcpContentTexts returns the normalized (newline-unified, trimmed) text
// of the result's non-empty text content blocks.
func mcpContentTexts(result *mcp.CallToolResult) []string {
	texts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		tc, ok := item.(*mcp.TextContent)
		if !ok {
			continue
		}
		text := strings.ReplaceAll(
			strings.ReplaceAll(tc.Text, "\r\n", "\n"), "\r", "\n")
		text = strings.TrimSpace(text)
		if text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// mcpStructuredResult decodes result.StructuredContent when it is shaped
// like an Exa search response.
func mcpStructuredResult(result *mcp.CallToolResult) *exaSearchResponse {
	if result.StructuredContent == nil {
		return nil
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil || !isSearchJSON(string(payload)) {
		return nil
	}
	var out exaSearchResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return &out
}

// mcpTextJSONResult recovers a search payload from content text blocks
// whose text is itself a JSON search response.
func mcpTextJSONResult(result *mcp.CallToolResult) *exaSearchResponse {
	for _, text := range mcpContentTexts(result) {
		if !json.Valid([]byte(text)) || !isSearchJSON(text) {
			continue
		}
		var out exaSearchResponse
		if err := json.Unmarshal([]byte(text), &out); err == nil {
			return &out
		}
	}
	return nil
}

// isSearchJSON reports whether a payload looks like an Exa search
// response: an object exposing any of the search payload markers.
func isSearchJSON(s string) bool {
	parsed := gjson.Parse(s)
	if !parsed.IsObject() {
		return false
	}
	for _, key := range []string{"results", "statuses", "costDollars", "searchTime"} {
		if parsed.Get(key).Exists() {
			return true
		}
	}
	return false
}

// parseExaMcpTextPayload recovers search results from the plain-text
// shape of the public MCP endpoint, whose text blocks carry sections of
// "Title:/URL:/Author:/Published Date:/Text:" lines separated by blank
// lines. It returns nil when no Title-led section parses.
func parseExaMcpTextPayload(
	result *mcp.CallToolResult,
) *exaSearchResponse {
	blocks := mcpContentTexts(result)
	if len(blocks) == 0 {
		return nil
	}
	joined := strings.Join(blocks, "\n\n")
	var results []exaResult
	for _, section := range splitTitleSections(joined) {
		title := sectionField(section, "Title")
		link := sectionField(section, "URL")
		text := sectionTextField(section)
		if title == "" && link == "" && text == "" {
			continue
		}
		results = append(results, exaResult{
			Title:         title,
			URL:           link,
			Author:        sectionField(section, "Author"),
			PublishedDate: sectionField(section, "Published Date"),
			Text:          text,
		})
	}
	if len(results) == 0 {
		return nil
	}
	return &exaSearchResponse{Results: results}
}

// sectionHeaderLabels are the labels that may directly follow a Title
// line at a section boundary.
var sectionHeaderLabels = []string{"URL:", "Author:", "Published Date:", "Text:"}

// splitTitleSections splits a joined text payload into Title-led
// sections. A boundary is one or more blank lines followed by a Title
// line whose next line starts a labeled field; blank lines elsewhere
// stay inside their section (mirroring the upstream lookahead split).
func splitTitleSections(joined string) []string {
	lines := strings.Split(joined, "\n")
	var sections []string
	var current []string
	blanks := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		section := strings.TrimSpace(strings.Join(current, "\n"))
		if strings.HasPrefix(section, "Title:") {
			sections = append(sections, section)
		}
		current = current[:0]
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blanks++
			}
			continue
		}
		if blanks > 0 && isSectionBoundary(lines, i) {
			flush()
		} else if blanks > 0 && len(current) > 0 {
			for ; blanks > 0; blanks-- {
				current = append(current, "")
			}
		}
		blanks = 0
		current = append(current, line)
	}
	flush()
	return sections
}

// isSectionBoundary reports whether lines[i] opens a new Title-led
// section: the line starts with "Title:" and the following line starts
// one of the labeled fields.
func isSectionBoundary(lines []string, i int) bool {
	if !strings.HasPrefix(lines[i], "Title:") {
		return false
	}
	if i+1 >= len(lines) {
		return false
	}
	for _, label := range sectionHeaderLabels {
		if strings.HasPrefix(lines[i+1], label) {
			return true
		}
	}
	return false
}

// sectionField extracts "Label: value" from a section, trimmed.
func sectionField(section, label string) string {
	prefix := label + ":"
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return ""
}

// sectionTextField extracts the remainder of the section after the
// first "Text:" line, preserving interior blank lines.
func sectionTextField(section string) string {
	lines := strings.Split(section, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Text:") {
			rest := strings.TrimPrefix(trimmed, "Text:")
			rest = strings.TrimSpace(rest)
			if i+1 < len(lines) {
				rest = strings.Join(append([]string{rest}, lines[i+1:]...), "\n")
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
