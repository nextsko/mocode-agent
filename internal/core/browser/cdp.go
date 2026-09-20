package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// cdpEndpoint is what Chrome publishes at /json/version: the browser-level
// WebSocket URL, plus the product string. This HTTP endpoint is CDP's own
// discovery mechanism, which is why nothing here guesses a port.
type cdpEndpoint struct {
	Browser              string `json:"Browser"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// httpDiscoveryTimeout keeps a wedged endpoint from hanging a tool call.
const httpDiscoveryTimeout = 5 * time.Second

// discoverEndpoint asks a running Chrome for its browser-level WebSocket URL.
//
// baseURL may be either the plain host:port ("http://127.0.0.1:9222") or an
// already-resolved ws:// URL, in which case it is returned unchanged. Accepting
// both means MOCODE_BROWSER_CDP works whether the user pastes the HTTP endpoint
// or the WebSocket one — the two look almost identical and are easy to confuse.
func discoverEndpoint(ctx context.Context, baseURL string) (string, string, error) {
	if strings.HasPrefix(baseURL, "ws://") || strings.HasPrefix(baseURL, "wss://") {
		return baseURL, "", nil
	}

	url := strings.TrimSuffix(baseURL, "/") + "/json/version"
	ctx, cancel := context.WithTimeout(ctx, httpDiscoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("querying %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("querying %s: unexpected status %s", url, resp.Status)
	}

	var ep cdpEndpoint
	if err := json.NewDecoder(resp.Body).Decode(&ep); err != nil {
		return "", "", fmt.Errorf("decoding %s: %w", url, err)
	}
	if ep.WebSocketDebuggerURL == "" {
		return "", "", fmt.Errorf("%s did not advertise a webSocketDebuggerUrl", url)
	}
	return ep.WebSocketDebuggerURL, ep.Browser, nil
}

// cdpError is a protocol-level error returned by the browser.
type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *cdpError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("cdp error %d: %s (%s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message)
}

// cdpMessage is the wire format in both directions. Sessions are flattened
// (Target.attachToTarget with flatten:true), so every message may carry a
// sessionId and the transport is a single WebSocket per browser.
type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
}

// cdpConn is a multiplexed Chrome DevTools Protocol connection.
//
// CDP is request/response with server-pushed events over one socket, so a
// reader goroutine fans messages out by id (responses) or method (events).
type cdpConn struct {
	ws     *websocket.Conn
	closed chan struct{}
	// closeOnce makes closed deterministically closed by whichever of Close or
	// the reader gets there first. Without it, Alive() right after Close() was a
	// race: Close returned before the reader had noticed the socket die, so a
	// closed session still reported itself alive.
	closeOnce sync.Once

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan cdpMessage
	subs    map[string][]*eventHandler
	lastErr error
}

// dialCDP opens a browser-level CDP connection.
func dialCDP(ctx context.Context, wsURL string) (*cdpConn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		// Chrome's devtools endpoint rejects requests carrying an Origin header.
		ReadBufferSize:  1 << 20,
		WriteBufferSize: 1 << 20,
	}

	ws, resp, err := dialer.DialContext(ctx, wsURL, http.Header{})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dialling %s: %w (http %s)", wsURL, err, resp.Status)
		}
		return nil, fmt.Errorf("dialling %s: %w", wsURL, err)
	}

	c := &cdpConn{
		ws:      ws,
		closed:  make(chan struct{}),
		pending: map[int64]chan cdpMessage{},
		subs:    map[string][]*eventHandler{},
	}
	go c.readLoop()
	return c, nil
}

// readLoop dispatches every inbound frame until the socket dies, then fails all
// outstanding calls so no caller can block forever on a dead connection.
func (c *cdpConn) readLoop() {
	defer c.markClosed()

	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.failAll(err)
			return
		}

		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			// A malformed frame must not kill the connection: CDP evolves and
			// an unknown shape is not a reason to tear down a live session.
			continue
		}

		switch {
		case msg.ID != 0:
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ok {
				ch <- msg
			}
		case msg.Method != "":
			c.dispatch(msg)
		}
	}
}

func (c *cdpConn) dispatch(msg cdpMessage) {
	c.mu.Lock()
	handlers := append([]*eventHandler{}, c.subs[msg.Method]...)
	c.mu.Unlock()

	for _, h := range handlers {
		h.fn(msg.SessionID, msg.Params)
	}
}

func (c *cdpConn) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastErr == nil {
		c.lastErr = err
	}
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
}

// eventHandler pairs a callback with its registration, so it can be
// unsubscribed by identity. Comparing func values (or their code pointers) is
// not reliable: two closures from the same literal share a code pointer.
type eventHandler struct {
	fn  func(sessionID string, params json.RawMessage)
	off bool
}

// On registers an event handler. It is safe to call before any commands are
// issued; handlers registered later simply miss earlier events.
//
// The returned func unsubscribes that one handler. Persistent handlers (console
// capture) ignore it; scoped waits (a load event for one navigation) must call
// it, or stale handlers accumulate on every call.
func (c *cdpConn) On(method string, fn func(sessionID string, params json.RawMessage)) func() {
	h := &eventHandler{fn: fn}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs[method] = append(c.subs[method], h)
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		h.off = true
		fns := c.subs[method]
		for i, e := range fns {
			if e == h {
				c.subs[method] = append(fns[:i:i], fns[i+1:]...)
				break
			}
		}
	}
}

// Call issues a command and decodes the result into out (which may be nil).
// An empty sessionID targets the browser itself.
func (c *cdpConn) Call(ctx context.Context, sessionID, method string, params any, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("encoding params for %s: %w", method, err)
	}
	if params == nil {
		raw = nil
	}

	c.mu.Lock()
	select {
	case <-c.closed:
		err := c.lastErr
		c.mu.Unlock()
		return fmt.Errorf("cdp connection is closed: %w", orEOF(err))
	default:
	}
	c.nextID++
	id := c.nextID
	ch := make(chan cdpMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := cdpMessage{ID: id, Method: method, Params: raw, SessionID: sessionID}

	c.writeMu.Lock()
	writeErr := c.ws.WriteJSON(msg)
	c.writeMu.Unlock()

	if writeErr != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("sending %s: %w", method, writeErr)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			c.mu.Lock()
			err := c.lastErr
			c.mu.Unlock()
			return fmt.Errorf("%s failed: connection closed: %w", method, orEOF(err))
		}
		if resp.Error != nil {
			return fmt.Errorf("%s: %w", method, resp.Error)
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("decoding %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.closed:
		return fmt.Errorf("%s failed: connection closed", method)
	}
}

func (c *cdpConn) Close() error {
	c.writeMu.Lock()
	err := c.ws.Close()
	c.writeMu.Unlock()

	// Mark closed here rather than waiting for the reader to notice, so that a
	// caller which has closed the connection can immediately observe it as
	// closed.
	c.markClosed()

	if err != nil && !errors.Is(err, websocket.ErrCloseSent) {
		return err
	}
	return nil
}

// markClosed closes the closed channel exactly once.
func (c *cdpConn) markClosed() {
	c.closeOnce.Do(func() { close(c.closed) })
}

func orEOF(err error) error {
	if err == nil {
		return errors.New("EOF")
	}
	return err
}
