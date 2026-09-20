package zai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// mcpClientName and mcpClientVersion identify this agent during the MCP
// initialize handshake.
const (
	mcpClientName    = "mocode-agent"
	mcpClientVersion = "0"
)

// mcpErrorBodyLimit caps how much of an HTTP error body is captured for
// classification.
const mcpErrorBodyLimit = 1 << 20

// mcpErrorCodePattern extracts the numeric status Z.AI embeds in tool
// error text, mirroring the TypeScript port's /MCP error\s*(-?\d+)/i.
var mcpErrorCodePattern = regexp.MustCompile(`(?i)MCP error\s*(-?\d+)`)

// creditBodyPattern matches billing-exhaustion signals in response
// bodies, mirroring the TypeScript port's classifier.
var creditBodyPattern = regexp.MustCompile(
	`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

// zaiHTTPError carries the HTTP status and body of a failed endpoint
// request. The SDK's transport errors do not surface the numeric
// status, so zaiTransport captures it at the RoundTripper level.
type zaiHTTPError struct {
	Status int
	Body   string
}

func (e *zaiHTTPError) Error() string {
	return fmt.Sprintf("Z.AI MCP request failed (%d): %s", e.Status, e.Body)
}

// zaiTransport injects the Authorization Bearer header on every request
// and converts failing HTTP statuses into *zaiHTTPError values so they
// can be classified downstream.
type zaiTransport struct {
	apiKey string
	next   http.RoundTripper
}

func (t zaiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
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
		return nil, &zaiHTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	return resp, nil
}

// mcpSessionCache holds one connected MCP client session bound to the
// credential it was opened with, so a credential change forces a
// reconnect on the next call.
type mcpSessionCache struct {
	apiKey  string
	session *mcp.ClientSession
	cancel  context.CancelFunc
}

// mcpSession returns a connected ClientSession for apiKey, reusing the
// cached session while its credential matches. The session's context is
// detached from the triggering request (WithoutCancel) so it survives
// across calls; its lifetime ends at dropMCPSession.
func (p *Provider) mcpSession(
	ctx context.Context,
	apiKey string,
) (*mcp.ClientSession, error) {
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	if p.mcpSess != nil && p.mcpSess.apiKey == apiKey {
		return p.mcpSess.session, nil
	}
	p.closeMCPSessionLocked()
	sessCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	transport := &mcp.StreamableClientTransport{
		Endpoint: p.mcpURL,
		HTTPClient: &http.Client{
			Transport: zaiTransport{apiKey: apiKey, next: p.httpClient.Transport},
		},
		// The TypeScript port only issues plain request/response POSTs
		// against the remote endpoint; skipping the standalone SSE
		// stream matches that shape.
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    mcpClientName,
		Version: mcpClientVersion,
	}, nil)
	session, err := client.Connect(sessCtx, transport, nil)
	if err != nil {
		cancel()
		return nil, mapZAIError(err)
	}
	p.mcpSess = &mcpSessionCache{
		apiKey:  apiKey,
		session: session,
		cancel:  cancel,
	}
	return session, nil
}

// dropMCPSession discards the cached session for apiKey, if any, so the
// next call reconnects.
func (p *Provider) dropMCPSession(apiKey string) {
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	if p.mcpSess != nil && p.mcpSess.apiKey == apiKey {
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

// callTool executes one tools/call with the given arguments through the
// cached session. A transport or protocol failure drops the session so
// the next attempt reconnects; a result with IsError set surfaces the
// tool error text instead.
func (p *Provider) callTool(
	ctx context.Context,
	apiKey string,
	args map[string]any,
) (*mcp.CallToolResult, error) {
	session, err := p.mcpSession(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      zaiToolName,
		Arguments: args,
	})
	if err != nil {
		p.dropMCPSession(apiKey)
		return nil, mapZAIError(err)
	}
	if result.IsError {
		return nil, zaiToolError(result)
	}
	return result, nil
}

// mapZAIError converts SDK transport and protocol failures onto
// classified provider errors.
func mapZAIError(err error) error {
	var httpErr *zaiHTTPError
	if errors.As(err, &httpErr) {
		return classifyZAIHTTP(httpErr.Status, httpErr.Body)
	}
	var rpcErr *jsonrpc.Error
	if errors.As(err, &rpcErr) {
		return zaiRPCError(rpcErr)
	}
	return fmt.Errorf("MCP call: %w", err)
}

// zaiRPCError maps a JSON-RPC protocol error onto a provider error. The
// JSON-RPC code rides along as the HTTP status, defaulting to nothing
// beyond the raw code, as in the TypeScript port.
func zaiRPCError(rpcErr *jsonrpc.Error) *search.SearchProviderError {
	message := rpcErr.Message
	if message == "" {
		message = "Unknown error"
	}
	text := fmt.Sprintf("Z.AI MCP error: %s", message)
	if rpcErr.Code != 0 {
		text = fmt.Sprintf("Z.AI MCP error (%d): %s", rpcErr.Code, message)
	}
	return &search.SearchProviderError{
		Provider:   "zai",
		Message:    text,
		HTTPStatus: int(rpcErr.Code),
	}
}

// zaiToolError extracts the failure message of an IsError tool result
// from its text content blocks, recovering the numeric status Z.AI
// embeds as "MCP error <n>" and defaulting to 400, mirroring the
// TypeScript port (which also keeps the retry condition consistent).
func zaiToolError(result *mcp.CallToolResult) *search.SearchProviderError {
	texts := contentTexts(result)
	errorText := strings.TrimSpace(strings.Join(texts, "\n"))
	status := 400
	if match := mcpErrorCodePattern.FindStringSubmatch(errorText); match != nil {
		if code, err := strconv.Atoi(match[1]); err == nil {
			status = absInt(code)
		}
	}
	if errorText == "" {
		errorText = "Z.AI MCP tool call failed"
	}
	return &search.SearchProviderError{
		Provider:   "zai",
		Message:    errorText,
		HTTPStatus: status,
	}
}

// absInt returns the absolute value of n.
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// classifyZAIHTTP maps an endpoint HTTP failure onto a classified
// provider error, mirroring the TypeScript port's shared classifier:
// billing body signals win, then 402/401/403, and everything else falls
// back to a status-tagged Z.AI error.
func classifyZAIHTTP(status int, body string) *search.SearchProviderError {
	if creditBodyPattern.MatchString(body) {
		return &search.SearchProviderError{
			Provider: "zai", Code: search.ErrorCodeBilling,
			Message: "credits exhausted", HTTPStatus: status,
		}
	}
	switch status {
	case 402:
		return &search.SearchProviderError{
			Provider: "zai", Code: search.ErrorCodeBilling,
			Message: "402 credits exhausted", HTTPStatus: status,
		}
	case 401:
		return &search.SearchProviderError{
			Provider: "zai", Code: search.ErrorCodeUnauthorized,
			Message: "401 unauthorized", HTTPStatus: status,
		}
	case 403:
		return &search.SearchProviderError{
			Provider: "zai", Code: search.ErrorCodeForbidden,
			Message: "403 forbidden", HTTPStatus: status,
		}
	}
	return &search.SearchProviderError{
		Provider:   "zai",
		Message:    fmt.Sprintf("Z.AI MCP error (%d): %s", status, body),
		HTTPStatus: status,
	}
}
