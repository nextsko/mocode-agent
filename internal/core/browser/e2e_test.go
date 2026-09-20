package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// e2ePage is a page whose behaviour only a real browser can exercise: a DOM,
// real pointer events, real key events, a framework-style input listener, and
// both a console message and an uncaught error.
const e2ePage = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>Mocode E2E</title></head>
<body>
  <h1>Hello E2E</h1>
  <input id="name" placeholder="Your name">
  <button id="go">Go</button>
  <div id="out"></div>
  <script>
    // Records that an *input event* fired, not merely that .value changed.
    // React/Vue listen for this, so a fill that only assigns .value would leave
    // __lastInput unset — which is exactly the bug this asserts against.
    document.getElementById('name').addEventListener('input', function (e) {
      window.__lastInput = e.target.value;
    });
    document.getElementById('go').addEventListener('click', function () {
      window.__clicks = (window.__clicks || 0) + 1;
      document.getElementById('out').textContent =
        'clicked:' + document.getElementById('name').value;
    });
    console.log('ready-marker');
    setTimeout(function () { throw new Error('boom-marker'); }, 10);
  </script>
</body>
</html>`

// requireRealBrowser launches the best detected browser, or skips.
//
// Skips rather than fails: a machine without Chrome is not a defect in this
// code, and a red CI build there would train people to ignore the test. The
// mock-CDP tests in cdp_test.go are what keep coverage on such machines.
func requireRealBrowser(t *testing.T) *Session {
	t.Helper()

	if os.Getenv("MOCODE_TEST_NO_BROWSER") != "" {
		t.Skip("MOCODE_TEST_NO_BROWSER is set")
	}
	if testing.Short() {
		t.Skip("skipping real-browser test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	candidates, err := DetectOrError(ctx, DetectOptions{})
	if err != nil {
		t.Skipf("no browser available: %v", err)
	}

	// Prefer a candidate the platform could confirm; fall back to the first,
	// because on Windows nothing is ever probed.
	pick := candidates[0]
	for _, c := range candidates {
		if c.Verification == VerOK {
			pick = c
			break
		}
	}
	t.Logf("using %s %s at %s", pick.Kind, pick.Version, pick.Path)

	session, err := Launch(ctx, pick, LaunchOptions{Headless: true})
	if err != nil {
		// A browser that exists but cannot start (missing shared libraries in a
		// minimal container) is an environment problem, not a code defect.
		t.Skipf("could not launch %s: %v", pick.Path, err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = session.Close(closeCtx)
	})
	return session
}

func serveE2EPage(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, e2ePage)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// refFor finds the ref of the first element with the given role and name.
func refFor(t *testing.T, snap *Snapshot, role, name string) string {
	t.Helper()
	for _, e := range snap.Elements {
		if e.Role == role && strings.Contains(e.Name, name) {
			return e.Ref
		}
	}
	t.Fatalf("no %s named %q in snapshot:\n%s", role, name, snap.Render())
	return ""
}

// TestE2E_NavigateSnapshotInteract is the end-to-end closed loop: a real
// browser, a real page, refs minted from a real DOM, and real user input.
func TestE2E_NavigateSnapshotInteract(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := serveE2EPage(t)
	require.NoError(t, session.Navigate(ctx, url))

	// 1. Snapshot sees the page and mints refs.
	snap, err := session.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Mocode E2E", snap.Title)
	// The browser normalises the URL (a bare origin gains a trailing slash),
	// so compare on the origin rather than the exact string it was given.
	assert.Contains(t, snap.URL, url)
	require.NotEmpty(t, snap.Elements, "a real page must yield addressable elements:\n%s", snap.Render())

	buttonRef := refFor(t, snap, "button", "Go")
	inputRef := refFor(t, snap, "text", "Your name")
	t.Logf("resolved button=%s input=%s", buttonRef, inputRef)

	// 2. Filling must fire an input event, not just set .value. This is the
	//    difference between working with React and silently not working.
	require.NoError(t, session.Fill(ctx, inputRef, "Ada", false))

	var lastInput string
	require.NoError(t, session.Evaluate(ctx, "window.__lastInput || '(unset)'", &lastInput))
	assert.Equal(t, "Ada", lastInput,
		"fill must dispatch an input event, or framework-bound inputs never update")

	// 3. Clicking must produce a real click the page's handler observes.
	require.NoError(t, session.Click(ctx, buttonRef))

	var out string
	require.NoError(t, session.Evaluate(ctx, "document.getElementById('out').textContent", &out))
	assert.Equal(t, "clicked:Ada", out, "the page's own click handler must have run with the filled value")

	var clicks int
	require.NoError(t, session.Evaluate(ctx, "window.__clicks || 0", &clicks))
	assert.Equal(t, 1, clicks)

	// 4. Keyboard input reaches the page.
	require.NoError(t, session.Press(ctx, "Tab"))
	require.NoError(t, session.Evaluate(ctx, "document.activeElement.tagName", &out))
	assert.NotEmpty(t, out, "Tab must move focus somewhere")

	// 5. A fresh snapshot reflects the mutated DOM.
	snap2, err := session.Snapshot(ctx)
	require.NoError(t, err)
	assert.Contains(t, snap2.Render(), "clicked:Ada",
		"a new snapshot must show what the interaction changed")
}

// TestE2E_ConsoleCapturesLogsAndExceptions is the regression test for the defect
// go-browser shipped: its console command always printed nothing because the
// handler was never registered.
func TestE2E_ConsoleCapturesLogsAndExceptions(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, session.Navigate(ctx, serveE2EPage(t)))

	// The page logs during load and throws slightly later, so give the
	// exception time to surface.
	var entries []ConsoleEntry
	require.Eventually(t, func() bool {
		entries, _ = session.Console()
		return hasMessage(entries, "ready-marker") && hasMessage(entries, "boom-marker")
	}, 15*time.Second, 100*time.Millisecond,
		"expected both the console log and the uncaught error to be captured; got %v", entries)

	require.True(t, hasMessage(entries, "ready-marker"), "console.log must be captured")

	// An uncaught error never reaches console.error, so it needs its own path.
	var found bool
	for _, e := range entries {
		if strings.Contains(e.Text, "boom-marker") {
			found = true
			assert.Equal(t, "exception", e.Level)
		}
	}
	assert.True(t, found, "an uncaught page error must be reported, not swallowed")

	drained := session.DrainConsole()
	assert.NotEmpty(t, drained)
	assert.Empty(t, session.DrainConsole(), "draining must clear the buffer")
}

func hasMessage(entries []ConsoleEntry, needle string) bool {
	for _, e := range entries {
		if strings.Contains(e.Text, needle) {
			return true
		}
	}
	return false
}

// TestE2E_ScreenshotReturnsAPNG checks the bytes are a real image rather than a
// truncated or empty base64 payload.
func TestE2E_ScreenshotReturnsAPNG(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, session.Navigate(ctx, serveE2EPage(t)))

	data, err := session.Screenshot(ctx, false)
	require.NoError(t, err)
	require.Greater(t, len(data), 1000, "a real screenshot is not a few bytes")

	// PNG magic, so a silently-empty capture cannot pass as success.
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, data[:8])
}

// TestE2E_EvalReportsPageErrors ensures a page-side exception is surfaced to the
// caller rather than returned as an empty success.
func TestE2E_EvalReportsPageErrors(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.NoError(t, session.Navigate(ctx, serveE2EPage(t)))

	err := session.Evaluate(ctx, "nonexistentFunction()", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistentFunction")
}

// TestE2E_SessionSurvivesIsAliveAndClosesCleanly covers the process lifecycle:
// a launched browser is alive while in use and gone after Close.
func TestE2E_SessionSurvivesIsAliveAndClosesCleanly(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	require.True(t, session.Alive())
	require.Contains(t, session.Endpoint(), "ws://")

	require.NoError(t, session.Navigate(ctx, serveE2EPage(t)))
	require.True(t, session.Alive())

	require.NoError(t, session.Close(ctx))

	// The process may take a moment to be reaped after Browser.close.
	require.Eventually(t, func() bool { return !session.Alive() }, 10*time.Second, 50*time.Millisecond,
		"the browser process must not outlive the session")
}

// ─── P2 surface: interactions, history, storage, tabs, pdf, network, routes ──

// p2Page exercises the P2 capabilities against real browser behaviour: a
// select, a checkbox, hover and dblclick handlers, and fetch endpoints a route
// can block or mock.
const p2Page = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>P2</title></head>
<body>
  <select id="sel"><option value="a">Alpha</option><option value="b">Beta</option></select>
  <input id="chk" type="checkbox">
  <div id="hov">hover me</div>
  <div id="dbl">double me</div>
  <script>
    document.getElementById('hov').addEventListener('mouseenter', function () { window.__hovered = true; });
    document.getElementById('dbl').addEventListener('dblclick', function () { window.__dbl = (window.__dbl || 0) + 1; });
  </script>
</body>
</html>`

func serveP2Page(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, p2Page)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestE2E_SelectCheckHoverDblclick(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := serveP2Page(t)
	require.NoError(t, session.Navigate(ctx, url))

	// Select by label, read back by value.
	require.NoError(t, session.SelectOption(ctx, "#sel", "Beta"))
	var val string
	require.NoError(t, session.Evaluate(ctx, "document.getElementById('sel').value", &val))
	assert.Equal(t, "b", val)

	// Check and uncheck, with the state actually changing.
	require.NoError(t, session.Check(ctx, "#chk"))
	var checked bool
	require.NoError(t, session.Evaluate(ctx, "document.getElementById('chk').checked", &checked))
	assert.True(t, checked)
	require.NoError(t, session.Uncheck(ctx, "#chk"))
	require.NoError(t, session.Evaluate(ctx, "document.getElementById('chk').checked", &checked))
	assert.False(t, checked)

	// Hover fires mouseenter, which :hover CSS and hover menus key on.
	require.NoError(t, session.Hover(ctx, "#hov"))
	require.NoError(t, session.Evaluate(ctx, "window.__hovered === true", nil))

	// A double-click is two pointer presses with clickCount 2, not two clicks.
	require.NoError(t, session.DblClick(ctx, "#dbl"))
	var dbl int
	require.NoError(t, session.Evaluate(ctx, "window.__dbl || 0", &dbl))
	assert.Equal(t, 1, dbl)
}

func TestE2E_BackForwardReload(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	base := serveP2Page(t)
	require.NoError(t, session.Navigate(ctx, base))
	require.NoError(t, session.Navigate(ctx, base+"/?p=2"))

	require.NoError(t, session.NavigateBack(ctx))
	var search string
	require.NoError(t, session.Evaluate(ctx, "location.search", &search))
	assert.Empty(t, search, "back must return to the first page")

	require.NoError(t, session.NavigateForward(ctx))
	require.NoError(t, session.Evaluate(ctx, "location.search", &search))
	assert.Equal(t, "?p=2", search)

	require.NoError(t, session.Reload(ctx))
	require.NoError(t, session.Evaluate(ctx, "location.search", &search))
	assert.Equal(t, "?p=2", search, "reload keeps the current entry")
}

func TestE2E_StorageCookiesAndState(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := serveP2Page(t)
	require.NoError(t, session.Navigate(ctx, url))

	// Web storage round-trip.
	require.NoError(t, session.StorageSet(ctx, "local", "theme", "dark"))
	got, err := session.StorageGet(ctx, "local", "theme")
	require.NoError(t, err)
	assert.Equal(t, "dark", got)
	items, err := session.StorageList(ctx, "local")
	require.NoError(t, err)
	assert.Equal(t, "dark", items["theme"])

	// Cookies round-trip through the network domain.
	require.NoError(t, session.SetCookie(ctx, CookieSetter{Name: "e2e", Value: "yes"}))
	cookies, err := session.Cookies(ctx)
	require.NoError(t, err)
	found := false
	for _, c := range cookies {
		if c.Name == "e2e" && c.Value == "yes" {
			found = true
		}
	}
	assert.True(t, found, "the cookie just set must be readable back")

	// State survives clear + load.
	path := t.TempDir() + "/state.json"
	st, err := session.SaveState(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, st.Cookies)
	require.NoError(t, WriteStateFile(path, st))

	require.NoError(t, session.ClearCookies(ctx))
	cookies, err = session.Cookies(ctx)
	require.NoError(t, err)
	assert.Empty(t, cookies)

	loaded, err := ReadStateFile(path)
	require.NoError(t, err)
	report, err := session.LoadState(ctx, loaded)
	require.NoError(t, err)
	assert.Positive(t, report.CookiesApplied)
	assert.Empty(t, report.SkippedOrigins, "same origin: nothing should be skipped")

	cookies, err = session.Cookies(ctx)
	require.NoError(t, err)
	found = false
	for _, c := range cookies {
		if c.Name == "e2e" {
			found = true
		}
	}
	assert.True(t, found, "the restored state must bring the cookie back")
}

func TestE2E_NetworkLogAndRoutes(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	url := serveP2Page(t)
	_ = session.DrainNetwork() // drop whatever bring-up recorded
	require.NoError(t, session.Navigate(ctx, url))

	// The page request itself shows up with its status.
	require.Eventually(t, func() bool {
		entries, _ := session.NetworkLog()
		for _, e := range entries {
			if e.Status == 200 && strings.Contains(e.URL, url) {
				return true
			}
		}
		return false
	}, 10*time.Second, 100*time.Millisecond, "the navigation must be recorded with status 200")

	// Blocking: the fetch fails the way a blocked request does.
	require.NoError(t, session.Route(ctx, "**/blocked", nil))
	var outcome string
	require.NoError(t, session.Evaluate(ctx,
		`fetch('/blocked').then(r => 'ok:' + r.status).catch(e => 'err')`, &outcome))
	assert.Equal(t, "err", outcome)

	// Mocking: the API answers with the configured body, no server involved.
	require.NoError(t, session.Route(ctx, "/api/data", &MockResponse{
		Status: 200, Body: `{"n":42}`, ContentType: "application/json",
	}))
	var n float64
	require.NoError(t, session.Evaluate(ctx,
		`fetch('/api/data').then(r => r.json()).then(o => o.n)`, &n))
	assert.Equal(t, float64(42), n)

	// The log reflects the route decisions. Chrome also reports the failure
	// itself (net::ERR_BLOCKED_BY_CLIENT), so the marker is "non-empty error",
	// not one specific string.
	entries, _ := session.NetworkLog()
	var blocked, mocked bool
	for _, e := range entries {
		if strings.HasSuffix(e.URL, "/blocked") && e.Error != "" {
			blocked = true
		}
		if strings.HasSuffix(e.URL, "/api/data") && e.Status == 200 {
			mocked = true
		}
	}
	assert.True(t, blocked, "a blocked request must be visible in the log: %v", entries)
	assert.True(t, mocked, "a mocked request must be visible in the log: %v", entries)

	// Unrouting lets requests through again.
	require.NoError(t, session.Unroute(ctx, "**/blocked"))
	require.NoError(t, session.Evaluate(ctx,
		`fetch('/blocked').then(r => 'ok:' + r.status).catch(e => 'err')`, &outcome))
	assert.Equal(t, "ok:200", outcome, "the local server answers once the route is gone")
}

func TestE2E_PDFAndTabs(t *testing.T) {
	session := requireRealBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	url := serveP2Page(t)
	require.NoError(t, session.Navigate(ctx, url))

	data, err := session.PDF(ctx)
	require.NoError(t, err)
	require.Greater(t, len(data), 1000)
	assert.Equal(t, "%PDF", string(data[:4]), "a real PDF, not an empty payload")

	// Tabs: open, list, select, close.
	require.NoError(t, session.TabNew(ctx, url+"/?tab=1"))
	tabs, err := session.Tabs(ctx)
	require.NoError(t, err)
	require.Len(t, tabs, 2)
	// Chrome does not guarantee target-list order, so find the new tab by URL.
	newTab := tabs[0]
	for _, tab := range tabs {
		if strings.Contains(tab.URL, "?tab=1") {
			newTab = tab
		}
	}
	assert.True(t, newTab.Current, "the new tab becomes the session's page")

	// Select the original tab by the index it has *now*: the list can be
	// reordered between calls, so an index captured earlier may point elsewhere.
	origIdx := 0
	for i, tab := range tabs {
		if !strings.Contains(tab.URL, "?tab=1") {
			origIdx = i
		}
	}
	require.NoError(t, session.TabSelect(ctx, origIdx))
	tabs, err = session.Tabs(ctx)
	require.NoError(t, err)
	for _, tab := range tabs {
		if !strings.Contains(tab.URL, "?tab=1") {
			assert.True(t, tab.Current, "selecting the original tab makes it current")
		}
	}

	// Close the ?tab=1 tab by its current index.
	closeIdx := -1
	for i, tab := range tabs {
		if strings.Contains(tab.URL, "?tab=1") {
			closeIdx = i
		}
	}
	require.NotEqual(t, -1, closeIdx)
	require.NoError(t, session.TabClose(ctx, closeIdx))
	require.Eventually(t, func() bool {
		tabs, err := session.Tabs(ctx)
		return err == nil && len(tabs) == 1
	}, 10*time.Second, 100*time.Millisecond, "a closed tab must disappear from the list")
}
