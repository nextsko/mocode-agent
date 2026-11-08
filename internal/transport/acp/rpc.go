// Package acp implements the Agent Client Protocol (v1) over stdio.
//
// ACP is JSON-RPC 2.0 with newline-delimited frames. The agent side owns a
// session (cwd, conversation) and streams progress as session/update
// notifications; the client side renders them and answers permission, fs and
// terminal requests. See https://agentclientprotocol.com.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	jsonrpcVersion = "2.0"
	keyJSONRPC     = "jsonrpc"
)

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

// rpcMessage is an incoming frame: request, response or notification.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func (m rpcMessage) isRequest() bool      { return m.Method != "" && len(m.ID) > 0 }
func (m rpcMessage) isNotification() bool { return m.Method != "" && len(m.ID) == 0 }
func (m rpcMessage) isResponse() bool     { return m.Method == "" && len(m.ID) > 0 }

type response struct {
	result json.RawMessage
	err    *Error
}

// Peer is one end of a JSON-RPC connection.
type Peer struct {
	in  io.Reader
	out io.Writer

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan response
	nextID  int64
	handler func(ctx context.Context, method string, params json.RawMessage) (any, *Error)
	notify  func(method string, params json.RawMessage)
	closed  chan struct{}
	err     error
}

// NewPeer builds a peer over r/w.
func NewPeer(r io.Reader, w io.Writer) *Peer {
	return &Peer{in: r, out: w, pending: map[string]chan response{}, closed: make(chan struct{})}
}

// OnRequest registers the handler for client→agent requests.
func (p *Peer) OnRequest(fn func(ctx context.Context, method string, params json.RawMessage) (any, *Error)) {
	p.handler = fn
}

// OnNotification registers the handler for client→agent notifications.
func (p *Peer) OnNotification(fn func(method string, params json.RawMessage)) {
	p.notify = fn
}

// Done is closed when the input stream ends.
func (p *Peer) Done() <-chan struct{} { return p.closed }

// Err reports why the peer stopped.
func (p *Peer) Err() error { return p.err }

// Serve reads frames until EOF or a fatal write error.
func (p *Peer) Serve(ctx context.Context) error {
	sc := bufio.NewScanner(p.in)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			p.fail(&Error{Code: CodeParseError, Message: "invalid JSON: " + err.Error()})
			continue
		}
		switch {
		case msg.isResponse():
			p.deliver(msg)
		case msg.isRequest():
			go p.handleRequest(ctx, msg)
		case msg.isNotification():
			if p.notify != nil {
				p.notify(msg.Method, msg.Params)
			}
		default:
			p.fail(&Error{Code: CodeInvalidRequest, Message: "frame has neither method nor id"})
		}
	}
	err := sc.Err()
	close(p.closed)
	if err != nil && !errors.Is(err, io.EOF) {
		p.err = err
		return fmt.Errorf("read frames: %w", err)
	}
	return nil
}

func (p *Peer) handleRequest(ctx context.Context, msg rpcMessage) {
	var (
		result any
		rerr   *Error
	)
	if p.handler == nil {
		rerr = &Error{Code: CodeMethodNotFound, Message: "method not found: " + msg.Method}
	} else {
		result, rerr = p.handler(ctx, msg.Method, msg.Params)
	}
	if rerr != nil {
		_ = p.write(map[string]any{keyJSONRPC: jsonrpcVersion, "id": msg.ID, "error": rerr})
		return
	}
	_ = p.write(map[string]any{keyJSONRPC: jsonrpcVersion, "id": msg.ID, "result": result})
}

func (p *Peer) deliver(msg rpcMessage) {
	key := string(msg.ID)
	p.mu.Lock()
	ch, ok := p.pending[key]
	if ok {
		delete(p.pending, key)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	ch <- response{result: msg.Result, err: msg.Error}
	close(ch)
}

// Call sends a request to the client and waits for its response.
func (p *Peer) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := p.nextID
	p.nextID++
	rawID := json.RawMessage(fmt.Sprintf("%d", id))
	ch := make(chan response, 1)
	p.mu.Lock()
	p.pending[string(rawID)] = ch
	p.mu.Unlock()

	if err := p.write(map[string]any{keyJSONRPC: jsonrpcVersion, "id": rawID, "method": method, "params": params}); err != nil {
		p.mu.Lock()
		delete(p.pending, string(rawID))
		p.mu.Unlock()
		return nil, err
	}
	select {
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, string(rawID))
		p.mu.Unlock()
		return nil, fmt.Errorf("request cancelled: %w", ctx.Err())
	case <-p.closed:
		return nil, errors.New("connection closed")
	case resp := <-ch:
		if resp.err != nil {
			return nil, resp.err
		}
		return resp.result, nil
	}
}

// Notify sends a notification to the client.
func (p *Peer) Notify(method string, params any) error {
	return p.write(map[string]any{keyJSONRPC: jsonrpcVersion, "method": method, "params": params})
}

func (p *Peer) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := p.out.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

func (p *Peer) fail(e *Error) {
	_ = p.write(map[string]any{keyJSONRPC: jsonrpcVersion, "error": e})
}
