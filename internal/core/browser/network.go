package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// defaultNetworkBuffer bounds the request log. A media-heavy page fires
// hundreds of requests; an unbounded log would grow with them.
const defaultNetworkBuffer = 500

// RequestEntry is one request in the network log.
type RequestEntry struct {
	ID     string    `json:"id"`
	Method string    `json:"method"`
	URL    string    `json:"url"`
	Status int       `json:"status"`
	MIME   string    `json:"mime"`
	Error  string    `json:"error,omitempty"`
	Time   time.Time `json:"time"`
}

// networkBuffer records requests in order and lets responses update their
// entries by request id, mirroring consoleBuffer's bounded-drop behaviour.
type networkBuffer struct {
	mu      sync.Mutex
	order   []string
	byID    map[string]*RequestEntry
	limit   int
	dropped int
}

func newNetworkBuffer(limit int) *networkBuffer {
	if limit <= 0 {
		limit = defaultNetworkBuffer
	}
	return &networkBuffer{byID: map[string]*RequestEntry{}, limit: limit}
}

func (b *networkBuffer) add(e RequestEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.order) >= b.limit {
		delete(b.byID, b.order[0])
		b.order = b.order[1:]
		b.dropped++
	}
	b.order = append(b.order, e.ID)
	b.byID[e.ID] = &e
}

func (b *networkBuffer) update(id string, fn func(*RequestEntry)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e, ok := b.byID[id]; ok {
		fn(e)
	}
}

func (b *networkBuffer) snapshot() ([]RequestEntry, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]RequestEntry, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, *b.byID[id])
	}
	return out, b.dropped
}

func (b *networkBuffer) drain() []RequestEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]RequestEntry, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, *b.byID[id])
	}
	b.order, b.byID, b.dropped = nil, map[string]*RequestEntry{}, 0
	return out
}

// NetworkLog returns buffered requests without clearing them.
func (s *Session) NetworkLog() ([]RequestEntry, int) { return s.network.snapshot() }

// DrainNetwork returns buffered requests and clears the log.
func (s *Session) DrainNetwork() []RequestEntry { return s.network.drain() }

// onRequestWillBeSent records a request as soon as the browser issues it, so a
// hanging or failed request is visible with status 0 rather than missing.
func (s *Session) onRequestWillBeSent(sessionID string, params json.RawMessage) {
	var p struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL    string `json:"url"`
			Method string `json:"method"`
		} `json:"request"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.RequestID == "" {
		return
	}
	s.network.add(RequestEntry{
		ID:     p.RequestID,
		Method: p.Request.Method,
		URL:    p.Request.URL,
		Time:   time.Now(),
	})
}

// onResponseReceived fills in the status of a recorded request.
func (s *Session) onResponseReceived(sessionID string, params json.RawMessage) {
	var p struct {
		RequestID string `json:"requestId"`
		Response  struct {
			Status    int    `json:"status"`
			MimeType  string `json:"mimeType"`
			FromCache bool   `json:"fromDiskCache"`
		} `json:"response"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.RequestID == "" {
		return
	}
	s.network.update(p.RequestID, func(e *RequestEntry) {
		e.Status = p.Response.Status
		e.MIME = p.Response.MimeType
	})
}

// onLoadingFailed marks a request that never got a response.
func (s *Session) onLoadingFailed(sessionID string, params json.RawMessage) {
	var p struct {
		RequestID string `json:"requestId"`
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.RequestID == "" {
		return
	}
	s.network.update(p.RequestID, func(e *RequestEntry) {
		e.Error = p.ErrorText
	})
}

// MockResponse is what a mocked route answers with.
type MockResponse struct {
	Status      int
	Body        string
	ContentType string
}

// route is one interception rule: URLs matching pattern are either answered
// with mock or, when mock is nil, blocked.
type route struct {
	pattern string
	re      *regexp.Regexp
	mock    *MockResponse
}

// routeTable holds the interception rules.
type routeTable struct {
	mu      sync.Mutex
	entries []route
}

func (r *routeTable) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func (r *routeTable) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.entries))
	for _, e := range r.entries {
		if e.mock != nil {
			out = append(out, e.pattern+" -> mock "+fmt.Sprintf("(status %d, %d bytes)", e.mock.Status, len(e.mock.Body)))
		} else {
			out = append(out, e.pattern+" -> blocked")
		}
	}
	return out
}

// match returns the first route whose pattern matches url.
func (r *routeTable) match(url string) *route {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.entries {
		if r.entries[i].re.MatchString(url) {
			return &r.entries[i]
		}
	}
	return nil
}

// compileRoutePattern translates a URL glob (* and ?) into a regexp.
//
// A pattern naming a scheme (http://, https://, ws://…) must match the whole
// URL. Anything else gets an implicit "anything before" prefix, must still end
// at the end of the path, and tolerates a trailing query or hash — so
// "**/api/stats" and "/api/stats" both hit "https://site.com/api/stats?x=1",
// but neither hits "/api/stats/extra". That mirrors what people expect from the
// globs Playwright made familiar.
func compileRoutePattern(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString(`\Q`)
	for _, ch := range glob {
		switch ch {
		case '*':
			b.WriteString(`\E.*\Q`)
		case '?':
			b.WriteString(`\E.\Q`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteString(`\E`)
	expr := b.String()
	if !strings.Contains(glob, "://") {
		expr = `.*` + expr + `(?:[?#].*)?`
	}
	return regexp.Compile(`^` + expr + `$`)
}

// Route installs or replaces an interception rule for pattern.
//
// A mock with a body answers matching requests with it; a nil mock blocks them
// the way an ad blocker would. Installing the first rule starts pausing all
// traffic (every paused request that matches no rule is passed through
// untouched, so unrelated requests keep working).
func (s *Session) Route(ctx context.Context, pattern string, mock *MockResponse) error {
	re, err := compileRoutePattern(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}

	s.routes.mu.Lock()
	// Re-routing the same pattern replaces the rule; stacking two rules for one
	// pattern would make "which one wins?" unanswerable.
	kept := s.routes.entries[:0]
	for _, e := range s.routes.entries {
		if e.pattern != pattern {
			kept = append(kept, e)
		}
	}
	s.routes.entries = append(kept, route{pattern: pattern, re: re, mock: mock})
	s.routes.mu.Unlock()

	return s.installFetch(ctx)
}

// Unroute removes the rule for pattern, stopping interception when none remain.
func (s *Session) Unroute(ctx context.Context, pattern string) error {
	s.routes.mu.Lock()
	found := false
	kept := s.routes.entries[:0]
	for _, e := range s.routes.entries {
		if e.pattern == pattern {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	s.routes.entries = kept
	remaining := len(kept)
	s.routes.mu.Unlock()

	if !found {
		return fmt.Errorf("no route for pattern %q", pattern)
	}
	if remaining == 0 {
		return s.conn.Call(ctx, s.sessionID, "Fetch.disable", nil, nil)
	}
	return nil
}

// Routes lists the installed rules as human-readable lines.
func (s *Session) Routes() []string { return s.routes.list() }

// installFetch (re)enables the Fetch domain so routes take effect.
func (s *Session) installFetch(ctx context.Context) error {
	return s.conn.Call(ctx, s.sessionID, "Fetch.enable", map[string]any{
		// Pause everything and decide locally: CDP's own urlPattern globbing has
		// quirks, and one catch-all pattern keeps the semantics in our hands.
		"patterns": []map[string]any{{"urlPattern": "*"}},
	}, nil)
}

// onRequestPaused decides the fate of a paused request.
//
// It must not answer inline: the handler runs on the connection's reader
// goroutine, and answering means issuing a CDP call whose response can only
// arrive through that same goroutine — a deadlock. Handling happens on its own
// goroutine instead.
func (s *Session) onRequestPaused(sessionID string, params json.RawMessage) {
	var p struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.RequestID == "" {
		return
	}

	requestID, pageSession := p.RequestID, sessionID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		r := s.routes.match(p.Request.URL)
		switch {
		case r == nil:
			_ = s.conn.Call(ctx, pageSession, "Fetch.continueRequest", map[string]any{"requestId": requestID}, nil)
		case r.mock == nil:
			_ = s.conn.Call(ctx, pageSession, "Fetch.failRequest", map[string]any{
				"requestId": requestID, "errorReason": "BlockedByClient",
			}, nil)
			// Chrome also reports the failure itself (loadingFailed); whoever
			// gets there first wins, and neither overwrites the other.
			s.network.update(requestID, func(e *RequestEntry) {
				if e.Error == "" {
					e.Error = "blocked by route"
				}
			})
		default:
			headers := []map[string]string{{"name": "Content-Type", "value": contentTypeOrDefault(r.mock.ContentType)}}
			_ = s.conn.Call(ctx, pageSession, "Fetch.fulfillRequest", map[string]any{
				"requestId":       requestID,
				"responseCode":    r.mock.Status,
				"body":            base64.StdEncoding.EncodeToString([]byte(r.mock.Body)),
				"responseHeaders": headers,
			}, nil)
			s.network.update(requestID, func(e *RequestEntry) {
				e.Status = r.mock.Status
				e.MIME = contentTypeOrDefault(r.mock.ContentType)
			})
		}
	}()
}

func contentTypeOrDefault(ct string) string {
	if strings.TrimSpace(ct) == "" {
		return "text/plain; charset=utf-8"
	}
	return ct
}
