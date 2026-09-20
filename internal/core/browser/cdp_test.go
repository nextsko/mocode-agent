package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeChromium is a mock CDP endpoint.
//
// This is the load-bearing test surface for the browser runtime: it makes "our
// client understands the protocol" a regression-tested claim without requiring
// a browser to be installed, so CI stays green on machines that have none. The
// real-browser test in e2e_test.go then proves this mock is not lying.
type fakeChromium struct {
	t   *testing.T
	srv *httptest.Server

	upgrader websocket.Upgrader

	mu       sync.Mutex
	received []cdpMessage
	conn     *websocket.Conn

	// handlers respond to a method. A nil entry answers with {}.
	handlers map[string]func(params json.RawMessage) (any, error)

	// onConnect runs right after the socket opens, for pushing events.
	onConnect func(f *fakeChromium)
	// dropOn makes the connection close instead of answering that method.
	dropOn string
}

func newFakeChromium(t *testing.T) *fakeChromium {
	f := &fakeChromium{
		t:        t,
		upgrader: websocket.Upgrader{},
		handlers: map[string]func(json.RawMessage) (any, error){},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/devtools/browser/fake"
		_ = json.NewEncoder(w).Encode(cdpEndpoint{
			Browser:              "Chrome/152.0.7977.83",
			WebSocketDebuggerURL: wsURL,
		})
	})
	mux.HandleFunc("/devtools/browser/fake", f.serveWS)

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeChromium) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := f.upgrader.Upgrade(w, r, nil)
	if err != nil {
		f.t.Logf("upgrade failed: %v", err)
		return
	}

	f.mu.Lock()
	f.conn = conn
	f.mu.Unlock()

	defer func() { _ = conn.Close() }()

	if f.onConnect != nil {
		f.onConnect(f)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		f.mu.Lock()
		f.received = append(f.received, msg)
		drop := f.dropOn == msg.Method
		handler := f.handlers[msg.Method]
		f.mu.Unlock()

		if drop {
			_ = conn.Close()
			return
		}

		resp := cdpMessage{ID: msg.ID, SessionID: msg.SessionID}
		var result any = map[string]any{}
		if handler != nil {
			r, err := handler(msg.Params)
			if err != nil {
				resp.Error = &cdpError{Code: -32000, Message: err.Error()}
			} else if r != nil {
				result = r
			}
		}
		if resp.Error == nil {
			raw, _ := json.Marshal(result)
			resp.Result = raw
		}

		f.mu.Lock()
		writeErr := conn.WriteJSON(resp)
		f.mu.Unlock()
		if writeErr != nil {
			return
		}
	}
}

// push sends a server-initiated event.
//
// The write takes the same mutex serveWS uses for its responses: gorilla
// connections reject concurrent writers, and a route test pushes events while
// the handler goroutine's commands are being answered — without serialisation
// the frames interleave and both sides silently drop them.
func (f *fakeChromium) push(sessionID, method string, params any) {
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if conn == nil {
		f.t.Logf("push %s before connect", method)
		return
	}
	raw, _ := json.Marshal(params)
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = conn.WriteJSON(cdpMessage{Method: method, SessionID: sessionID, Params: raw})
}

func (f *fakeChromium) on(method string, fn func(json.RawMessage) (any, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[method] = fn
}

func (f *fakeChromium) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.received))
	for _, m := range f.received {
		out = append(out, m.Method)
	}
	return out
}

func (f *fakeChromium) find(method string) (cdpMessage, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.received {
		if m.Method == method {
			return m, true
		}
	}
	return cdpMessage{}, false
}

// withDefaultHandlers wires up the minimum a session needs to come up.
func (f *fakeChromium) withDefaultHandlers(sessionID, targetID string) {
	f.on("Target.getTargets", func(json.RawMessage) (any, error) {
		return map[string]any{"targetInfos": []map[string]any{
			{"targetId": targetID, "type": "page", "url": "about:blank", "attached": false},
		}}, nil
	})
	f.on("Target.attachToTarget", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": sessionID}, nil
	})
	f.on("Page.navigate", func(json.RawMessage) (any, error) {
		// Real Chrome emits this after the load completes.
		go func() {
			time.Sleep(10 * time.Millisecond)
			f.push(sessionID, "Page.loadEventFired", map[string]any{"timestamp": 1})
		}()
		return map[string]any{"frameId": "F1"}, nil
	})
}

func sessionEndpoint(f *fakeChromium) string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/devtools/browser/fake"
}

// ─── handshake ───────────────────────────────────────────────────────────────

func TestDiscoverEndpoint_AcceptsWebSocketURLUnchanged(t *testing.T) {
	t.Parallel()

	got, _, err := discoverEndpoint(context.Background(), "ws://127.0.0.1:9222/devtools/browser/abc")
	require.NoError(t, err)
	assert.Equal(t, "ws://127.0.0.1:9222/devtools/browser/abc", got,
		"a ws:// endpoint is already resolved; re-querying it would fail")
}

func TestDiscoverEndpoint_ResolvesHTTPViaJSONVersion(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	wsURL, product, err := discoverEndpoint(context.Background(), f.srv.URL)
	require.NoError(t, err)
	assert.Contains(t, wsURL, "/devtools/browser/fake")
	assert.Equal(t, "Chrome/152.0.7977.83", product)
}

func TestDiscoverEndpoint_ReportsUnreachableEndpoint(t *testing.T) {
	t.Parallel()

	// Port 1 is reserved and never listening.
	_, _, err := discoverEndpoint(context.Background(), "http://127.0.0.1:1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying")
}

// ─── session bring-up ────────────────────────────────────────────────────────

// TestAttach_EnablesTheDomainsWeRelyOn pins the setup contract: without
// Page.enable / Runtime.enable no load or console events are ever delivered,
// and the failure is silent — the tool would simply never report console output.
func TestAttach_EnablesTheDomainsWeRelyOn(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	assert.Equal(t, "T1", s.TargetID())

	calls := f.calls()
	for _, want := range []string{"Target.getTargets", "Target.attachToTarget", "Page.enable", "Runtime.enable", "DOM.enable"} {
		assert.Contains(t, calls, want)
	}

	// The sessionId the browser handed back must be used on session commands,
	// or a flattened connection routes them to the wrong target.
	msg, ok := f.find("Page.enable")
	require.True(t, ok)
	assert.Equal(t, "S1", msg.SessionID)
}

// Attaching to a browser with no page reuses an idle page rather than opening a
// new tab in the user's window.
func TestAttach_CreatesTargetWhenNoneExists(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.on("Target.getTargets", func(json.RawMessage) (any, error) {
		return map[string]any{"targetInfos": []map[string]any{}}, nil
	})
	f.on("Target.createTarget", func(json.RawMessage) (any, error) {
		return map[string]any{"targetId": "NEW"}, nil
	})
	f.on("Target.attachToTarget", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "S-NEW"}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	assert.Equal(t, "NEW", s.TargetID())
	assert.Contains(t, f.calls(), "Target.createTarget")
}

// ─── events ──────────────────────────────────────────────────────────────────

// TestConsole_EventsAreCaptured is the capability go-browser shipped broken:
// its console handler was never wired, so the command always printed nothing.
// Here the events are asserted to arrive, be ordered, and be bounded.
func TestConsole_EventsAreCaptured(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	f.push("S1", "Runtime.consoleAPICalled", map[string]any{
		"type": "log",
		"args": []map[string]any{
			{"type": "string", "value": "hello"},
			{"type": "number", "value": 42},
		},
		"stackTrace": map[string]any{"callFrames": []map[string]any{
			{"url": "http://localhost/app.js", "lineNumber": 9},
		}},
	})
	f.push("S1", "Runtime.consoleAPICalled", map[string]any{
		"type": "error",
		"args": []map[string]any{{"type": "string", "value": "boom"}},
	})
	f.push("S1", "Runtime.exceptionThrown", map[string]any{
		"exceptionDetails": map[string]any{
			"text":       "Uncaught TypeError",
			"url":        "http://localhost/app.js",
			"lineNumber": 20,
		},
	})

	require.Eventually(t, func() bool {
		entries, _ := s.Console()
		return len(entries) == 3
	}, 2*time.Second, 10*time.Millisecond)

	entries, dropped := s.Console()
	require.Len(t, entries, 3)
	assert.Equal(t, 0, dropped)

	assert.Equal(t, "log", entries[0].Level)
	assert.Equal(t, "hello 42", entries[0].Text)
	assert.Equal(t, "http://localhost/app.js", entries[0].URL)
	assert.Equal(t, 10, entries[0].Line, "CDP lines are 0-based; the report is 1-based")

	assert.Equal(t, "error", entries[1].Level)
	assert.Equal(t, "boom", entries[1].Text)

	// An uncaught error never reaches console.error, so without the separate
	// exception handler the page's actual bug would be invisible.
	assert.Equal(t, "exception", entries[2].Level)
	assert.Equal(t, "Uncaught TypeError", entries[2].Text)
	assert.Equal(t, 21, entries[2].Line)
}

func TestConsole_BufferIsBoundedAndReportsDrops(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	s.console = newConsoleBuffer(3)

	for i := range 10 {
		f.push("S1", "Runtime.consoleAPICalled", map[string]any{
			"type": "log",
			"args": []map[string]any{{"type": "string", "value": fmt.Sprintf("m%d", i)}},
		})
	}

	require.Eventually(t, func() bool {
		entries, dropped := s.Console()
		return len(entries) == 3 && dropped == 7
	}, 2*time.Second, 10*time.Millisecond)

	entries, dropped := s.Console()
	assert.Equal(t, 7, dropped, "drops must be counted so a gap is never silent")
	assert.Equal(t, "m7", entries[0].Text, "the oldest kept message")
	assert.Equal(t, "m9", entries[2].Text, "the newest messages are the ones kept")
}

func TestDrainConsole_ReturnsOnlyNewMessages(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	f.push("S1", "Runtime.consoleAPICalled", map[string]any{
		"type": "log", "args": []map[string]any{{"type": "string", "value": "once"}},
	})
	require.Eventually(t, func() bool {
		e, _ := s.Console()
		return len(e) == 1
	}, 2*time.Second, 10*time.Millisecond)

	assert.Len(t, s.DrainConsole(), 1)
	assert.Empty(t, s.DrainConsole(), "a drained buffer must not repeat messages")
}

// ─── commands ────────────────────────────────────────────────────────────────

func TestNavigate_WaitsForLoadEventAndSurvivesTimeout(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	start := time.Now()
	require.NoError(t, s.Navigate(context.Background(), "http://example.test"))
	assert.Less(t, time.Since(start), 5*time.Second)

	msg, ok := f.find("Page.navigate")
	require.True(t, ok)
	assert.JSONEq(t, `{"url":"http://example.test"}`, string(msg.Params))
}

// A navigation that never fires a load event must still return. Pages with
// long-polling or blocked subresources routinely never reach "load", and a tool
// that hangs there is worse than one that returns an imperfect result.
func TestNavigate_DoesNotHangWhenLoadEventNeverArrives(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.on("Target.getTargets", func(json.RawMessage) (any, error) {
		return map[string]any{"targetInfos": []map[string]any{
			{"targetId": "T1", "type": "page", "attached": false},
		}}, nil
	})
	f.on("Target.attachToTarget", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "S1"}, nil
	})
	// Page.navigate answers but no loadEventFired is ever pushed.

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = s.Navigate(ctx, "http://slow.test")
	// Either the internal 15s budget or the caller's context ends the wait; with
	// a 2s caller budget it must be the latter, and it must not hang past it.
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestEvaluate_SurfacesPageExceptions(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(json.RawMessage) (any, error) {
		return map[string]any{
			"result": map[string]any{"type": "undefined"},
			"exceptionDetails": map[string]any{
				"text": "Uncaught",
				"exception": map[string]any{
					"description": "ReferenceError: nope is not defined\n    at <anonymous>:1:1",
				},
			},
		}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	err = s.Evaluate(context.Background(), "nope()", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ReferenceError: nope is not defined")
}

func TestEvaluate_DecodesValuesIntoOut(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(json.RawMessage) (any, error) {
		return map[string]any{
			"result": map[string]any{"type": "object", "value": map[string]any{"n": 7}},
		}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	var out struct {
		N int `json:"n"`
	}
	require.NoError(t, s.Evaluate(context.Background(), "({n:7})", &out))
	assert.Equal(t, 7, out.N)
}

// A CDP-level error must become a Go error rather than an empty success.
func TestCall_ProtocolErrorIsSurfaced(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(json.RawMessage) (any, error) {
		return nil, fmt.Errorf("Cannot find context with specified id")
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	err = s.Evaluate(context.Background(), "1", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Cannot find context with specified id")
	assert.Contains(t, err.Error(), "cdp error -32000")
}

// A dropped connection must fail in-flight calls instead of blocking forever.
func TestCall_ConnectionDropFailsPendingCalls(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.dropOn = "Runtime.evaluate"

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	done := make(chan error, 1)
	go func() { done <- s.Evaluate(context.Background(), "1", nil) }()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("a call hung after the connection dropped")
	}
}

// ─── actions ─────────────────────────────────────────────────────────────────

// TestClick_DispatchesMouseEventsAtElementCentre checks that a click is a real
// pointer sequence at the element's centre, not a synthetic DOM click: the two
// differ for drag handling, canvas UIs and anything listening on pointer events.
func TestClick_DispatchesMouseEventsAtElementCentre(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(params json.RawMessage) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)

		switch {
		case strings.Contains(p.Expression, "getBoundingClientRect()") && strings.Contains(p.Expression, "scrollIntoView"):
			return map[string]any{
				"result": map[string]any{"type": "object", "value": map[string]any{"x": 120.0, "y": 240.0}},
			}, nil
		default:
			return map[string]any{"result": map[string]any{"type": "undefined"}}, nil
		}
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Click(context.Background(), "e3"))

	var types []string
	f.mu.Lock()
	for _, m := range f.received {
		if m.Method != "Input.dispatchMouseEvent" {
			continue
		}
		var p struct {
			Type string  `json:"type"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
		}
		_ = json.Unmarshal(m.Params, &p)
		types = append(types, p.Type)
		assert.Equal(t, 120.0, p.X)
		assert.Equal(t, 240.0, p.Y)
	}
	f.mu.Unlock()

	assert.Equal(t, []string{"mouseMoved", "mousePressed", "mouseReleased"}, types)
}

// The ref the model supplies is interpolated into a script, so it is validated
// rather than escaped — a ref must not be able to smuggle in syntax.
func TestResolveTarget_RejectsInjectedRefs(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		"e0']; alert(1); //",
		"e",
		"e12x",
		"e1234567",
		"e0');fetch('http://x')//",
	} {
		_, err := resolveTarget(bad)
		if strings.HasPrefix(bad, "e") && len(bad) <= 6 {
			assert.Error(t, err, "ref-like %q must be rejected", bad)
		}
	}
}

func TestResolveTarget_AcceptsRefsAndSelectors(t *testing.T) {
	t.Parallel()

	expr, err := resolveTarget("e12")
	require.NoError(t, err)
	assert.Equal(t, `document.querySelector('[data-mocode-ref="e12"]')`, expr)

	expr, err = resolveTarget("#submit")
	require.NoError(t, err)
	assert.Equal(t, `document.querySelector("#submit")`, expr)

	// Quotes in a selector must be escaped, not break the expression. Go's
	// json.Marshal escapes the double quotes; the literal is "..."-delimited, so
	// a bare apostrophe is safe and is deliberately left alone.
	expr, err = resolveTarget(`[name="it's"]`)
	require.NoError(t, err)
	assert.Equal(t, `document.querySelector("[name=\"it's\"]")`, expr)

	_, err = resolveTarget("   ")
	assert.Error(t, err)
}

func TestIsValidRef(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"e0", "e9", "e12", "e999"} {
		assert.True(t, isValidRef(ok), ok)
	}
	for _, bad := range []string{"", "e", "E0", "e-1", "e1a", "x1", "e12345"} {
		assert.False(t, isValidRef(bad), bad)
	}
}

// Filling via `el.value = x` is invisible to React/Vue, which keep their own
// copy of the value and would submit the stale one. The native setter plus
// input/change events is what makes the fill stick.
func TestFill_UsesNativeSetterAndDispatchesInputEvents(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	var seen string
	f.on("Runtime.evaluate", func(params json.RawMessage) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)
		seen = p.Expression
		if strings.Contains(p.Expression, "return 'ok'") {
			return map[string]any{"result": map[string]any{"type": "string", "value": "ok"}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "undefined"}}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Fill(context.Background(), "e1", `he said "hi"`, false))

	assert.Contains(t, seen, "getOwnPropertyDescriptor")
	assert.Contains(t, seen, "desc.set.call(el,")
	assert.Contains(t, seen, "new Event('input'")
	assert.Contains(t, seen, "new Event('change'")
	// The quote in the value must be escaped, not terminate the literal.
	assert.Contains(t, seen, `\"hi\"`)
}

func TestFill_SubmitPressesEnter(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(params json.RawMessage) (any, error) {
		return map[string]any{"result": map[string]any{"type": "string", "value": "ok"}}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Fill(context.Background(), "e1", "x", true))
	assert.Contains(t, f.calls(), "Input.dispatchKeyEvent")
}

func TestFill_ReportsMissingElement(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(json.RawMessage) (any, error) {
		return map[string]any{"result": map[string]any{"type": "string", "value": "not found"}}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	err = s.Fill(context.Background(), "e1", "x", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "take a new snapshot")
}

func TestKeyDescriptor(t *testing.T) {
	t.Parallel()

	code, vk, text := keyDescriptor("Enter")
	assert.Equal(t, "Enter", code)
	assert.Equal(t, 13, vk)
	assert.Equal(t, "\r", text)

	code, vk, _ = keyDescriptor("a")
	assert.Equal(t, "KeyA", code)
	assert.Equal(t, int('a'), vk)

	code, _, _ = keyDescriptor("F13")
	assert.Empty(t, code, "unmapped multi-character keys must be reported, not guessed")
}

func TestScreenshot_DecodesBase64(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Page.captureScreenshot", func(json.RawMessage) (any, error) {
		return map[string]any{"data": "aGVsbG8="}, nil // "hello"
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	data, err := s.Screenshot(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	msg, ok := f.find("Page.captureScreenshot")
	require.True(t, ok)
	assert.JSONEq(t, `{"format":"png"}`, string(msg.Params))
}

func TestScreenshot_FullPageSetsCaptureBeyondViewport(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Page.captureScreenshot", func(json.RawMessage) (any, error) {
		return map[string]any{"data": "aGVsbG8="}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	_, err = s.Screenshot(context.Background(), true)
	require.NoError(t, err)

	msg, _ := f.find("Page.captureScreenshot")
	assert.JSONEq(t, `{"format":"png","captureBeyondViewport":true}`, string(msg.Params))
}

// ─── snapshot ────────────────────────────────────────────────────────────────

func TestSnapshot_DecodesAndRenders(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	var script string
	f.on("Runtime.evaluate", func(params json.RawMessage) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		_ = json.Unmarshal(params, &p)
		script = p.Expression
		return map[string]any{"result": map[string]any{
			"type": "object",
			"value": map[string]any{
				"url":   "http://localhost:3000/",
				"title": "Test App",
				"elements": []map[string]any{
					{"ref": "e0", "tag": "button", "role": "button", "name": "Submit"},
					{"ref": "e1", "tag": "a", "role": "link", "name": "Home", "href": "/home"},
					{"ref": "e2", "tag": "input", "role": "email", "name": "Email", "value": "a@b.c"},
				},
			},
		}}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	snap, err := s.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Elements, 3)

	// The script must tag elements with the attribute we later resolve refs by.
	assert.Contains(t, script, refAttr)

	out := snap.Render()
	assert.Contains(t, out, "http://localhost:3000/")
	assert.Contains(t, out, "Test App")
	assert.Contains(t, out, `[e0] button "Submit"`)
	assert.Contains(t, out, `href="/home"`)
	assert.Contains(t, out, `value="a@b.c"`)

	el, ok := snap.Lookup("e1")
	require.True(t, ok)
	assert.Equal(t, "link", el.Role)

	_, ok = snap.Lookup("e99")
	assert.False(t, ok)
}

func TestSnapshot_RendersEmptyStateWithAHint(t *testing.T) {
	t.Parallel()

	snap := &Snapshot{URL: "http://x/", Elements: []Element{}}
	out := snap.Render()
	assert.Contains(t, out, "No addressable elements found")
	assert.Contains(t, out, "frames are not traversed")
}

func TestSnapshot_RendersTruncationNotice(t *testing.T) {
	t.Parallel()

	snap := &Snapshot{URL: "http://x/", Truncated: true, Elements: []Element{{Ref: "e0", Role: "button", Name: "x"}}}
	assert.Contains(t, snap.Render(), "truncated at")
}

// ─── lifecycle ───────────────────────────────────────────────────────────────

// Detaching must not close the user's own browser: killing a Chrome the user
// started themselves would lose their tabs.
func TestClose_OnAttachedSessionLeavesBrowserAlive(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Target.closeTarget", func(json.RawMessage) (any, error) {
		return map[string]any{"success": true}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)

	require.NoError(t, s.Close(context.Background()))

	assert.Contains(t, f.calls(), "Target.closeTarget", "an attached session closes only its own tab")
	assert.NotContains(t, f.calls(), "Browser.close", "the user's browser must survive")
}

func TestClose_IsIdempotent(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)

	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, s.Close(context.Background()))
}

func TestAlive_ReportsConnectionState(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	assert.True(t, s.Alive())

	require.NoError(t, s.Close(context.Background()))
	assert.False(t, s.Alive())
}

// ─── launch args (pure) ──────────────────────────────────────────────────────

// TestLaunchArgs pins the flag matrix. These flags encode hard-won behaviour:
// a self-chosen port avoids a race, --no-sandbox is what makes containers work,
// and the profile dir is what keeps the user's logins out of our session.
func TestLaunchArgs(t *testing.T) {
	t.Parallel()

	t.Run("headless default", func(t *testing.T) {
		t.Parallel()
		args := launchArgs(Candidate{Path: "/usr/bin/chromium"}, LaunchOptions{Headless: true}, "/tmp/p", false)

		assert.Contains(t, args, "--remote-debugging-port=0",
			"the browser must pick its own port; allocating one first is racy")
		assert.Contains(t, args, "--headless=new")
		assert.Contains(t, args, "--user-data-dir=/tmp/p")
		assert.Contains(t, args, "about:blank")
		assert.NotContains(t, args, "--no-sandbox")
	})

	t.Run("root disables the sandbox", func(t *testing.T) {
		t.Parallel()
		args := launchArgs(Candidate{Path: "/usr/bin/chromium"}, LaunchOptions{Headless: true}, "/tmp/p", true)

		assert.Contains(t, args, "--no-sandbox",
			"Chromium refuses to start as root with its sandbox enabled")
		assert.Contains(t, args, "--disable-dev-shm-usage")
	})

	t.Run("flatpak prefix comes first", func(t *testing.T) {
		t.Parallel()
		c := Candidate{Path: "/usr/bin/flatpak", PrefixArgs: []string{"run", "com.google.Chrome"}}
		args := launchArgs(c, LaunchOptions{Headless: true}, "/tmp/p", false)

		require.GreaterOrEqual(t, len(args), 2)
		assert.Equal(t, "run", args[0])
		assert.Equal(t, "com.google.Chrome", args[1])
	})

	t.Run("no headless flag when headed", func(t *testing.T) {
		t.Parallel()
		args := launchArgs(Candidate{}, LaunchOptions{Headless: false}, "/tmp/p", false)
		assert.NotContains(t, args, "--headless=new")
	})

	t.Run("extra args are appended", func(t *testing.T) {
		t.Parallel()
		args := launchArgs(Candidate{}, LaunchOptions{URL: "http://x/", ExtraArgs: []string{"--foo"}}, "", false)

		// The URL is never a launch argument: Chrome applies it asynchronously,
		// so the session would race it. Launch navigates instead.
		assert.NotContains(t, args, "http://x/")
		assert.Contains(t, args, "about:blank")
		assert.Equal(t, "--foo", args[len(args)-1])
		assert.NotContains(t, args, "--user-data-dir=")
	})
}

func TestParseDevToolsURL(t *testing.T) {
	t.Parallel()

	url, ok := parseDevToolsURL("DevTools listening on ws://127.0.0.1:54321/devtools/browser/abc-def")
	require.True(t, ok)
	assert.Equal(t, "ws://127.0.0.1:54321/devtools/browser/abc-def", url)

	assert.False(t, ok2(parseDevToolsURL("[123:456:ERROR] something else")))
	assert.False(t, ok2(parseDevToolsURL("DevTools listening on nothing useful")))
}

func ok2(_ string, ok bool) bool { return ok }
