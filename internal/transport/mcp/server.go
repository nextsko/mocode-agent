// Package mcp implements a Model Context Protocol server over stdio.
//
// The server speaks newline-delimited JSON-RPC 2.0: one JSON object per line on
// stdin, one JSON object per line on stdout. Diagnostics must never be written
// to stdout, which is reserved for protocol frames.
//
// Two tools are exposed:
//
//	run     run one non-interactive mocode prompt and return its final answer
//	models  list the provider/model IDs mocode has configured
//
// This package deliberately depends on no other mocode package except the
// version helper: the transport/cmd layer supplies a [Runner] implementation
// backed by the workspace, which keeps the layering rule intact and makes the
// protocol testable in isolation.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	// fallbackProtocolVersion is answered when the client asks for an unknown
	// protocol revision.
	fallbackProtocolVersion = "2025-06-18"

	// maxRequestBytes bounds one request line so a malformed client cannot
	// exhaust memory.
	maxRequestBytes = 8 << 20

	// maxResultBytes bounds the text returned from a single tool call.
	maxResultBytes = 1 << 20
)

// supportedProtocols are the MCP revisions this server implements.
var supportedProtocols = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
	"2025-11-25": true,
	"2026-07-28": true,
}

// Runner executes the operations exposed as MCP tools.
//
// Implementations must write the tool's text output to out. The ctx passed to a
// tool call is cancelled when the server shuts down, not when a single request
// finishes, so a long prompt keeps running until the client cancels it or the
// server exits.
type Runner interface {
	// RunPrompt runs one non-interactive prompt rooted at cwd and writes the
	// final answer to out. An empty cwd means the server's startup directory.
	RunPrompt(ctx context.Context, prompt, cwd string, out io.Writer) error

	// ListModels writes one provider/model ID per line to out.
	ListModels(ctx context.Context, out io.Writer) error

	// Version reports the mocode version string.
	Version() string
}

// Server serves MCP requests for one [Runner].
type Server struct {
	runner Runner
}

// New creates a server backed by runner.
func New(runner Runner) *Server {
	return &Server{runner: runner}
}

// Serve reads newline-delimited JSON-RPC requests from in and writes responses
// to out until in reaches EOF or ctx is cancelled.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), maxRequestBytes)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("server stopped: %w", ctx.Err())
		default:
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// A malformed line has no usable id, so it cannot be answered.
			continue
		}

		resp, ok := s.handle(ctx, &req)
		if !ok {
			continue
		}
		if err := encoder.Encode(resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	return nil
}

// rpcRequest is one JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is one JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handle dispatches one request. It reports whether a response must be written:
// JSON-RPC notifications (no id) never get one.
func (s *Server) handle(ctx context.Context, req *rpcRequest) (*rpcResponse, bool) {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	if isNotification {
		return nil, false
	}

	switch req.Method {
	case "initialize":
		return result(req.ID, s.initialize(req.Params)), true
	case "ping":
		return result(req.ID, map[string]any{}), true
	case "tools/list":
		return result(req.ID, map[string]any{"tools": tools()}), true
	case "tools/call":
		return result(req.ID, s.callTool(ctx, req.Params)), true
	default:
		return failure(req.ID, -32601, "method not found: "+req.Method), true
	}
}

// initialize answers the MCP handshake with the server's capabilities.
func (s *Server) initialize(params json.RawMessage) map[string]any {
	requested := ""
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
		requested = p.ProtocolVersion
	}
	version := fallbackProtocolVersion
	if supportedProtocols[requested] {
		version = requested
	}

	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "mocode", "version": s.runner.Version()},
	}
}

// result wraps a successful response.
func result(id json.RawMessage, v any) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Result: v}
}

// failure wraps a JSON-RPC error response.
func failure(id json.RawMessage, code int, message string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

// tool is one MCP tool description.
type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// tools returns the tool catalogue advertised by tools/list.
func tools() []tool {
	return []tool{
		{
			Name: "run",
			Description: "Delegate one non-interactive coding task to mocode and return its final answer. " +
				"Use for self-contained tasks in a directory, e.g. \"add tests for package X\" or " +
				"\"explain why this test fails\".",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "The task or question for mocode.",
					},
					"cwd": map[string]any{
						"type":        "string",
						"description": "Working directory for the task. Defaults to the server's startup directory.",
					},
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
		{
			Name:        "models",
			Description: "List the provider/model IDs mocode has configured.",
			InputSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	}
}

// callResult is the MCP result of tools/call.
type callResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// contentItem is one entry of an MCP tool result.
type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// callTool dispatches tools/call to the runner.
func (s *Server) callTool(ctx context.Context, params json.RawMessage) callResult {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return errorResult("invalid tools/call params: " + err.Error())
	}

	switch p.Name {
	case "run":
		return s.runTool(ctx, p.Arguments)
	case "models":
		return s.modelsTool(ctx)
	default:
		return errorResult("unknown tool: " + p.Name)
	}
}

// runTool implements the run tool.
func (s *Server) runTool(ctx context.Context, arguments json.RawMessage) callResult {
	var args struct {
		Prompt string `json:"prompt"`
		Cwd    string `json:"cwd"`
	}
	if err := json.Unmarshal(arguments, &args); err != nil {
		return errorResult("invalid run arguments: " + err.Error())
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return errorResult("prompt is required")
	}

	var buf bytes.Buffer
	if err := s.runner.RunPrompt(ctx, args.Prompt, args.Cwd, &buf); err != nil {
		text := strings.TrimSpace(buf.String())
		if text == "" {
			text = err.Error()
		} else {
			text += "\n\n" + err.Error()
		}
		return callResult{Content: []contentItem{{Type: "text", Text: truncate(text)}}, IsError: true}
	}

	text := strings.TrimSpace(buf.String())
	if text == "" {
		text = "(mocode returned no output)"
	}
	return callResult{Content: []contentItem{{Type: "text", Text: truncate(text)}}}
}

// modelsTool implements the models tool.
func (s *Server) modelsTool(ctx context.Context) callResult {
	var buf bytes.Buffer
	if err := s.runner.ListModels(ctx, &buf); err != nil {
		return errorResult(err.Error())
	}
	text := strings.TrimSpace(buf.String())
	if text == "" {
		text = "(no models configured)"
	}
	return callResult{Content: []contentItem{{Type: "text", Text: truncate(text)}}}
}

// errorResult wraps a tool-level failure.
func errorResult(message string) callResult {
	return callResult{Content: []contentItem{{Type: "text", Text: truncate(message)}}, IsError: true}
}

// truncate bounds tool output so one huge answer cannot blow up the client.
func truncate(text string) string {
	if len(text) <= maxResultBytes {
		return text
	}
	// Cut on a rune boundary to keep the JSON valid UTF-8.
	cut := maxResultBytes
	for cut > 0 && !utf8Start(text[cut]) {
		cut--
	}
	return text[:cut] + "\n\n[output truncated]"
}

// utf8Start reports whether b starts a UTF-8 rune.
func utf8Start(b byte) bool {
	return b&0xC0 != 0x80
}
