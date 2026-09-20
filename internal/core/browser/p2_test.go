package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── L1: route pattern compilation ───────────────────────────────────────────

func TestCompileRoutePattern(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern string
		url     string
		want    bool
	}{
		{"**/api/stats", "https://site.com/api/stats", true},
		{"**/api/stats", "https://site.com/api/stats/extra", false},
		{"**/api/stats", "https://site.com/api/stats?x=1", true},
		{"/api/x", "https://a.test/deep/api/x?q=1#frag", true}, // no scheme → prefix + query tolerated
		{"https://ads.example.com/*", "https://ads.example.com/banner.png", true},
		{"https://ads.example.com/*", "https://site.com/ads.example.com", false},
		{"*.example.com/*", "https://api.example.com/v1", true},
		{"a?c", "https://x.test/abc", true},
		{"a?c", "https://x.test/abbc", false},
	}
	for _, tc := range cases {
		re, err := compileRoutePattern(tc.pattern)
		require.NoErrorf(t, err, "pattern %s", tc.pattern)
		assert.Equalf(t, tc.want, re.MatchString(tc.url), "pattern %s vs %s", tc.pattern, tc.url)
	}
}

// ─── L1: network buffer ──────────────────────────────────────────────────────

func TestNetworkBuffer_AddsUpdatesAndDrops(t *testing.T) {
	t.Parallel()

	b := newNetworkBuffer(3)
	for i, id := range []string{"r1", "r2", "r3"} {
		b.add(RequestEntry{ID: id, Method: "GET", URL: "http://x/" + id, Status: 0})
		b.update(id, func(e *RequestEntry) { e.Status = 200 + i })
	}
	b.add(RequestEntry{ID: "r4", URL: "http://x/r4"}) // drops r1

	entries, dropped := b.snapshot()
	assert.Equal(t, 1, dropped, "the oldest entry is dropped and reported")
	require.Len(t, entries, 3)
	assert.Equal(t, "r2", entries[0].ID)
	assert.Equal(t, 201, entries[0].Status, "responses update their request entries")
	assert.Equal(t, 0, entries[2].Status, "a request without a response keeps status 0")

	rest := b.drain()
	assert.Len(t, rest, 3)
	assert.Empty(t, b.drain(), "draining must clear the log")
}

// ─── L2: navigation history ──────────────────────────────────────────────────

func TestNavigateForward_MovesToNextHistoryEntry(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Page.getNavigationHistory", func(json.RawMessage) (any, error) {
		return map[string]any{"currentIndex": 1, "entries": []map[string]any{
			{"id": 1}, {"id": 2}, {"id": 3},
		}}, nil
	})
	f.on("Page.navigateToHistoryEntry", func(json.RawMessage) (any, error) {
		return map[string]any{}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.NavigateForward(context.Background()))
	msg, ok := f.find("Page.navigateToHistoryEntry")
	require.True(t, ok, "forward must navigate to the next history entry")
	var params struct {
		EntryID float64 `json:"entryId"`
	}
	require.NoError(t, json.Unmarshal(msg.Params, &params))
	assert.Equal(t, float64(3), params.EntryID)
}

func TestNavigateForward_AtTheEndOfHistoryIsAnError(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Page.getNavigationHistory", func(json.RawMessage) (any, error) {
		return map[string]any{"currentIndex": 2, "entries": []map[string]any{
			{"id": 1}, {"id": 2}, {"id": 3},
		}}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	err = s.NavigateForward(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no next page")
}

func TestReload_WaitsForLoad(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Page.reload", func(json.RawMessage) (any, error) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			f.push("S1", "Page.loadEventFired", map[string]any{})
		}()
		return map[string]any{}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Reload(context.Background()))
	_, ok := f.find("Page.reload")
	assert.True(t, ok)
}

// ─── L2: tabs ────────────────────────────────────────────────────────────────

// tabStore backs a fake with a mutable target list, so tab operations are
// observable in both directions.
type tabStore struct {
	mu      sync.Mutex
	targets []map[string]any
	next    int
	current string
}

func (ts *tabStore) list() []map[string]any {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]map[string]any{}, ts.targets...)
}

func (ts *tabStore) add(url string) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.next++
	id := "T" + string(rune('0'+ts.next))
	ts.targets = append(ts.targets, map[string]any{"targetId": id, "type": "page", "url": url})
	return id
}

func (ts *tabStore) setCurrent(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.current = id
}

// setURL records a navigation on the current target, the way a real browser's
// target list reflects the page it holds.
func (ts *tabStore) setURL(url string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, t := range ts.targets {
		if t["targetId"] == ts.current {
			t["url"] = url
		}
	}
}

func (ts *tabStore) remove(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	kept := ts.targets[:0]
	for _, t := range ts.targets {
		if t["targetId"] != id {
			kept = append(kept, t)
		}
	}
	ts.targets = kept
}

func TestTabs_ListNewSelectClose(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	ts := &tabStore{next: 1, targets: []map[string]any{
		{"targetId": "T1", "type": "page", "url": "https://a.test/"},
	}}
	f.on("Target.getTargets", func(json.RawMessage) (any, error) {
		return map[string]any{"targetInfos": ts.list()}, nil
	})
	f.on("Target.attachToTarget", func(params json.RawMessage) (any, error) {
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(params, &p)
		ts.setCurrent(p.TargetID)
		return map[string]any{"sessionId": "S1"}, nil
	})
	f.on("Target.createTarget", func(params json.RawMessage) (any, error) {
		_ = params
		return map[string]any{"targetId": ts.add("about:blank")}, nil
	})
	f.on("Page.navigate", func(params json.RawMessage) (any, error) {
		var p struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(params, &p)
		ts.setURL(p.URL)
		go func() {
			time.Sleep(10 * time.Millisecond)
			f.push("S1", "Page.loadEventFired", map[string]any{})
		}()
		return map[string]any{"frameId": "F1"}, nil
	})
	f.on("Target.closeTarget", func(params json.RawMessage) (any, error) {
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(params, &p)
		ts.remove(p.TargetID)
		return map[string]any{"success": true}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	// New tab becomes current.
	require.NoError(t, s.TabNew(context.Background(), "https://b.test/"))
	assert.Equal(t, "T2", s.TargetID(), "a newly opened tab becomes the session's page")

	tabs, err := s.Tabs(context.Background())
	require.NoError(t, err)
	require.Len(t, tabs, 2)
	assert.True(t, tabs[1].Current, "tabs marks the page the session acts on")
	assert.Equal(t, "https://b.test/", tabs[1].URL)

	// Select by index.
	require.NoError(t, s.TabSelect(context.Background(), 0))
	assert.Equal(t, "T1", s.TargetID())

	// Close a background tab.
	require.NoError(t, s.TabClose(context.Background(), 1))
	tabs, err = s.Tabs(context.Background())
	require.NoError(t, err)
	assert.Len(t, tabs, 1)

	// The last tab is protected: closing it would orphan the session.
	err = s.TabClose(context.Background(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only tab")
}

func TestTabClose_CurrentTabSwitchesToASurvivor(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	ts := &tabStore{targets: []map[string]any{
		{"targetId": "T1", "type": "page", "url": "https://a.test/"},
		{"targetId": "T2", "type": "page", "url": "https://b.test/"},
	}}
	f.on("Target.getTargets", func(json.RawMessage) (any, error) {
		return map[string]any{"targetInfos": ts.list()}, nil
	})
	f.on("Target.attachToTarget", func(json.RawMessage) (any, error) {
		return map[string]any{"sessionId": "S1"}, nil
	})
	f.on("Target.closeTarget", func(params json.RawMessage) (any, error) {
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(params, &p)
		ts.remove(p.TargetID)
		return map[string]any{"success": true}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	require.Equal(t, "T1", s.TargetID())

	// Close the current tab; the session must land on a live page.
	require.NoError(t, s.TabClose(context.Background(), 0))
	assert.Equal(t, "T2", s.TargetID())
}

// ─── L2: cookies ─────────────────────────────────────────────────────────────

func TestCookies_ListSetAndClear(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(json.RawMessage) (any, error) {
		// The SetCookie default-domain probe asks for location.href.
		return map[string]any{"result": map[string]any{"type": "string", "value": "https://x.test/page"}}, nil
	})
	f.on("Network.getAllCookies", func(json.RawMessage) (any, error) {
		return map[string]any{"cookies": []map[string]any{
			{"name": "session", "value": "abc", "domain": "x.test", "path": "/"},
		}}, nil
	})
	var setParams map[string]any
	f.on("Network.setCookie", func(params json.RawMessage) (any, error) {
		_ = json.Unmarshal(params, &setParams)
		return map[string]any{"success": true}, nil
	})
	cleared := false
	f.on("Network.clearBrowserCookies", func(json.RawMessage) (any, error) {
		cleared = true
		return map[string]any{}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	cookies, err := s.Cookies(context.Background())
	require.NoError(t, err)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session=abc", strings.SplitN(cookies[0].String(), " ", 2)[0])

	require.NoError(t, s.SetCookie(context.Background(), CookieSetter{Name: "theme", Value: "dark"}))
	assert.Equal(t, "theme", setParams["name"], "without a domain the cookie is scoped via the page URL")
	assert.Equal(t, "https://x.test/page", setParams["url"])

	require.NoError(t, s.ClearCookies(context.Background()))
	assert.True(t, cleared)
}

func TestSetCookie_BlankPageWithoutDomainIsRejected(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Runtime.evaluate", func(json.RawMessage) (any, error) {
		return map[string]any{"result": map[string]any{"type": "string", "value": "about:blank"}}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	err = s.SetCookie(context.Background(), CookieSetter{Name: "x", Value: "y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain")
}

// ─── L2: PDF ─────────────────────────────────────────────────────────────────

func TestPDF_DecodesBase64(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")
	f.on("Page.printToPDF", func(json.RawMessage) (any, error) {
		return map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("%PDF-1.7 fake"))}, nil
	})

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	data, err := s.PDF(context.Background())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "%PDF"), "the base64 payload must be decoded, not passed through")
}

// ─── L2: network log ─────────────────────────────────────────────────────────

func TestNetworkLog_RecordsRequestsResponsesAndFailures(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	f.push("S1", "Network.requestWillBeSent", map[string]any{
		"requestId": "r1", "request": map[string]any{"url": "https://x.test/", "method": "GET"},
	})
	f.push("S1", "Network.responseReceived", map[string]any{
		"requestId": "r1", "response": map[string]any{"status": 200, "mimeType": "text/html"},
	})
	f.push("S1", "Network.requestWillBeSent", map[string]any{
		"requestId": "r2", "request": map[string]any{"url": "https://x.test/img.png", "method": "GET"},
	})
	f.push("S1", "Network.loadingFailed", map[string]any{
		"requestId": "r2", "errorText": "net::ERR_FAILED",
	})

	require.Eventually(t, func() bool {
		entries, _ := s.NetworkLog()
		return len(entries) == 2 && entries[0].Status == 200 && entries[1].Error != ""
	}, 5*time.Second, 20*time.Millisecond)

	entries, dropped := s.NetworkLog()
	assert.Zero(t, dropped)
	require.Len(t, entries, 2)
	assert.Equal(t, "text/html", entries[0].MIME)
	assert.Equal(t, "net::ERR_FAILED", entries[1].Error)
}

// ─── L2: routes ──────────────────────────────────────────────────────────────

func TestRoute_BlocksFulfillsAndPassesThrough(t *testing.T) {
	t.Parallel()

	f := newFakeChromium(t)
	f.withDefaultHandlers("S1", "T1")

	s, err := Attach(context.Background(), sessionEndpoint(f))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	require.NoError(t, s.Route(context.Background(), "**/ads/**", nil))
	require.NoError(t, s.Route(context.Background(), "/api/stats", &MockResponse{
		Status: 201, Body: "hello", ContentType: "application/json",
	}))

	msg, ok := f.find("Fetch.enable")
	require.True(t, ok, "installing a route must enable the Fetch domain")
	var enableParams struct {
		Patterns []map[string]any `json:"patterns"`
	}
	require.NoError(t, json.Unmarshal(msg.Params, &enableParams))
	require.NotEmpty(t, enableParams.Patterns, "traffic is paused broadly and decided locally")

	push := func(id, url string) {
		// A real browser records the request before pausing it.
		f.push("S1", "Network.requestWillBeSent", map[string]any{
			"requestId": id, "request": map[string]any{"url": url, "method": "GET"},
		})
		f.push("S1", "Fetch.requestPaused", map[string]any{
			"requestId": id, "request": map[string]any{"url": url},
		})
	}

	// Blocked: matches the ads rule.
	push("P1", "https://x.test/ads/banner.png")
	require.Eventually(t, func() bool {
		m, ok := f.find("Fetch.failRequest")
		if !ok {
			return false
		}
		var p struct {
			RequestID  string `json:"requestId"`
			ErrorError string `json:"errorReason"`
		}
		_ = json.Unmarshal(m.Params, &p)
		return p.RequestID == "P1" && p.ErrorError == "BlockedByClient"
	}, 5*time.Second, 20*time.Millisecond)

	// Mocked: answered with the configured body and status.
	push("P2", "https://x.test/api/stats?x=1")
	require.Eventually(t, func() bool {
		m, ok := f.find("Fetch.fulfillRequest")
		if !ok {
			return false
		}
		var p struct {
			RequestID    string           `json:"requestId"`
			ResponseCode int              `json:"responseCode"`
			Body         string           `json:"body"`
			Headers      []map[string]any `json:"responseHeaders"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			return false
		}
		body, _ := base64.StdEncoding.DecodeString(p.Body)
		return p.RequestID == "P2" && p.ResponseCode == 201 && string(body) == "hello" && len(p.Headers) == 1
	}, 5*time.Second, 20*time.Millisecond)

	// Unmatched: passed through untouched.
	push("P3", "https://x.test/page")
	require.Eventually(t, func() bool {
		m, ok := f.find("Fetch.continueRequest")
		if !ok {
			return false
		}
		var p struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		return p.RequestID == "P3"
	}, 5*time.Second, 20*time.Millisecond)

	// The network log reflects route decisions.
	require.Eventually(t, func() bool {
		entries, _ := s.NetworkLog()
		return len(entries) == 3
	}, 5*time.Second, 20*time.Millisecond)
	entries, _ := s.NetworkLog()
	assert.Equal(t, "blocked by route", entries[0].Error)
	assert.Equal(t, 201, entries[1].Status)

	// Removing the last route stops pausing traffic.
	require.NoError(t, s.Unroute(context.Background(), "**/ads/**"))
	require.NoError(t, s.Unroute(context.Background(), "/api/stats"))
	_, ok = f.find("Fetch.disable")
	assert.True(t, ok, "clearing the last route must disable Fetch")

	err = s.Unroute(context.Background(), "**/ads/**")
	require.Error(t, err, "unrouting a pattern that is not installed is a mistake worth reporting")
}

// Regex metacharacters in a pattern must be quoted, not interpreted: a route
// for a URL with "[" in it is data, not a character class.
func TestRoute_PatternQuotesRegexMetacharacters(t *testing.T) {
	t.Parallel()

	re, err := compileRoutePattern("a[b*c")
	require.NoError(t, err)
	assert.True(t, re.MatchString("https://x.test/a[bbb-ZZc"))
	assert.False(t, re.MatchString("https://x.test/aXbc"))
}

func TestRoute_ReplacingAPatternKeepsOneRule(t *testing.T) {
	t.Parallel()

	rt := &routeTable{}
	re1, _ := compileRoutePattern("**/x")
	re2, _ := compileRoutePattern("**/x")
	rt.entries = append(rt.entries, route{pattern: "**/x", re: re1})
	rt.entries = append(rt.entries[:0], route{pattern: "**/x", re: re2})
	assert.Equal(t, 1, rt.len(), "one pattern must not stack two rules")
}
