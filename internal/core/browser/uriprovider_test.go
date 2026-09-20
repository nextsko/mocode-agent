package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

func TestBrowserProvider_AuthorityAndSummary(t *testing.T) {
	t.Parallel()

	p := NewURIProvider(nil)
	assert.Equal(t, uri.AuthorityBrowser, p.Authority())
	assert.Contains(t, p.Summary(), "mocode://browser/")
}

// Listing must work with no session: it is how the agent discovers what the
// namespace offers, and "no session" is itself the answer it needs.
func TestBrowserProvider_ListsResourcesWithoutASession(t *testing.T) {
	t.Parallel()

	p := NewURIProvider(NewManager())
	content, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthorityBrowser})
	require.NoError(t, err)

	require.Equal(t, uri.KindListing, content.Kind)
	assert.Contains(t, content.Description, "No browser session")

	names := map[string]string{}
	for _, e := range content.Entries {
		names[e.Name] = e.Description
	}
	for _, want := range []string{"page", "snapshot", "text", "console", "screenshot"} {
		require.Contains(t, names, want)
		assert.Contains(t, names[want], "needs a running session",
			"a resource that cannot be read yet must say so")
	}
}

// Asking for a resource with no session running must point at the fix rather
// than reporting a bare failure.
func TestBrowserProvider_NoSessionExplainsHowToStart(t *testing.T) {
	t.Parallel()

	p := NewURIProvider(NewManager())
	for _, path := range []string{"page", "snapshot", "text", "console", "screenshot"} {
		_, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthorityBrowser, Path: path})
		require.Error(t, err, path)
		assert.True(t, errors.Is(err, uri.ErrNotFound), path)
		assert.Contains(t, err.Error(), "action=launch", path)
	}
}

func TestBrowserProvider_UnknownResourceListsWhatExists(t *testing.T) {
	t.Parallel()

	p := NewURIProvider(NewManager())
	_, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthorityBrowser, Path: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page, snapshot, text, console, screenshot")
}

func TestBrowserProvider_NestedPathIsRejected(t *testing.T) {
	t.Parallel()

	p := NewURIProvider(NewManager())
	_, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthorityBrowser, Path: "page/deeper"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a browser resource")
}

// ─── live session ────────────────────────────────────────────────────────────

// TestBrowserProvider_ReadsLiveSession drives the real stack: launch a browser,
// then read the page through the protocol. The pages the agent would debug are
// exactly the ones no fixture can stand in for.
func TestBrowserProvider_ReadsLiveSession(t *testing.T) {
	if os.Getenv("MOCODE_TEST_NO_BROWSER") != "" {
		t.Skip("MOCODE_TEST_NO_BROWSER is set")
	}
	if testing.Short() {
		t.Skip("skipping real-browser test in -short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err := DetectOrError(ctx, DetectOptions{}); err != nil {
		t.Skipf("no browser available: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8">
<title>URI E2E</title></head><body>
<h1>Visible heading</h1>
<button id="b">Press me</button>
<script>console.log('uri-marker');</script>
</body></html>`))
	}))
	defer srv.Close()

	manager := NewManager()
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = manager.Close(closeCtx)
	}()

	if _, err := manager.Ensure(ctx, EnsureOptions{Headless: true, URL: srv.URL}); err != nil {
		t.Skipf("could not launch a browser here: %v", err)
	}

	p := NewURIProvider(manager)

	// The listing now reports real availability.
	listing, err := p.Resolve(ctx, uri.URI{Authority: uri.AuthorityBrowser})
	require.NoError(t, err)
	for _, e := range listing.Entries {
		assert.NotContains(t, e.Description, "needs a running session")
	}

	// page
	page, err := p.Resolve(ctx, uri.URI{Authority: uri.AuthorityBrowser, Path: "page"})
	require.NoError(t, err)
	assert.Contains(t, page.Text, "URI E2E")

	// text — the reason to read a rendered page instead of its source.
	text, err := p.Resolve(ctx, uri.URI{Authority: uri.AuthorityBrowser, Path: "text"})
	require.NoError(t, err)
	assert.Contains(t, text.Text, "Visible heading")
	assert.NotContains(t, text.Text, "<h1>", "text is rendered, not markup")

	// snapshot
	snap, err := p.Resolve(ctx, uri.URI{Authority: uri.AuthorityBrowser, Path: "snapshot"})
	require.NoError(t, err)
	assert.Contains(t, snap.Text, "Press me")
	assert.Contains(t, snap.Text, "[e", "a snapshot must carry refs")

	// console
	var console *uri.Content
	require.Eventually(t, func() bool {
		c, cerr := p.Resolve(ctx, uri.URI{Authority: uri.AuthorityBrowser, Path: "console"})
		if cerr != nil {
			return false
		}
		console = c
		return strings.Contains(c.Text, "uri-marker")
	}, 10*time.Second, 200*time.Millisecond, "console output must reach the protocol")
	assert.Contains(t, console.Text, "uri-marker")

	// screenshot
	shot, err := p.Resolve(ctx, uri.URI{Authority: uri.AuthorityBrowser, Path: "screenshot"})
	require.NoError(t, err)
	assert.Equal(t, uri.KindImage, shot.Kind)
	assert.Equal(t, "image/png", shot.MIME)
	require.Greater(t, len(shot.Data), 1000)
	assert.Equal(t, []byte{0x89, 'P', 'N', 'G'}, shot.Data[:4])
}
