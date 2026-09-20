package duckduckgo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// loadFixture reads a testdata fixture as a string.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

// fakeClock is a manually advanced clock for cooldown and age tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the frozen time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the frozen time forward by d.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// recordedRequest captures one outbound POST for assertions.
type recordedRequest struct {
	method string
	header http.Header
	form   url.Values
}

// newFixtureServer serves the given pages in request order and records
// every request. Extra requests receive a 500.
func newFixtureServer(t *testing.T, pages ...string) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		form, err := url.ParseQuery(string(data))
		if err != nil {
			t.Errorf("parse request form: %v", err)
		}
		mu.Lock()
		idx := len(requests)
		requests = append(requests, recordedRequest{
			method: r.Method,
			header: r.Header.Clone(),
			form:   form,
		})
		mu.Unlock()
		if idx >= len(pages) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, pages[idx])
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), requests...)
	}
}

// newStatusServer always answers with the given status and body.
func newStatusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestProvider builds a provider pointed at srv with an isolated
// cooldown window and a controllable clock, keeping tests parallel-safe.
func newTestProvider(srv *httptest.Server) (*DDGProvider, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	p := New(srv.Client())
	p.endpoint = srv.URL
	p.cooldown = newCooldown(clock.Now)
	p.now = clock.Now
	return p, clock
}

func TestParseResultsFixture(t *testing.T) {
	t.Parallel()

	results := parseResults(loadFixture(t, "results.html"))

	require.Len(t, results, 6)

	require.Equal(t, "Effective Go - The Go Programming Language", results[0].title)
	require.Equal(t, "https://golang.org/docs/effective_go", results[0].url)
	require.Equal(t,
		"How to write clear, idiomatic Go code. Covers slices, maps, and interfaces.",
		results[0].snippet)
	require.Equal(t, "2026-09-01T10:30:00.0000000", results[0].publishedDate)

	require.Equal(t, "https://blog.golang.org/slices-intro", results[1].url)
	require.Equal(t, "Slices wrap arrays & give more flexibility.", results[1].snippet)
	require.Empty(t, results[1].publishedDate)

	require.Equal(t, "https://sponsor.example.com/special-offer?utm=ddg", results[2].url)
	require.Equal(t, "Interactive Go courses with a 30% discount.", results[2].snippet)

	require.Equal(t, "https://www.example.com/pricing", results[3].url)

	require.Equal(t, "https://pkg.go.dev/net/url", results[4].url)
	require.Empty(t, results[4].snippet)
	require.Empty(t, results[4].publishedDate)

	require.Equal(t, "https://example.com/date-only", results[5].url)
	require.Equal(t, "2026-08-31", results[5].publishedDate)
}

func TestUnwrapResultURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		href   string
		want   string
		wantOK bool
	}{
		{
			name:   "redirect wrapper",
			href:   "//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa%3Fq%3D1&rut=abc",
			want:   "https://example.com/a?q=1",
			wantOK: true,
		},
		{
			name:   "absolute https",
			href:   "https://example.com/direct",
			want:   "https://example.com/direct",
			wantOK: true,
		},
		{
			name:   "absolute http",
			href:   "http://example.com/insecure",
			want:   "http://example.com/insecure",
			wantOK: true,
		},
		{
			name:   "protocol relative",
			href:   "//example.com/relative",
			want:   "https://example.com/relative",
			wantOK: true,
		},
		{
			name:   "empty",
			href:   "",
			want:   "",
			wantOK: false,
		},
		{
			name:   "broken escape in uddg",
			href:   "//duckduckgo.com/l/?uddg=https%ZZbroken",
			want:   "",
			wantOK: false,
		},
		{
			name:   "relative garbage",
			href:   "results/page.html",
			want:   "",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := unwrapResultURL(tc.href)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestExtractVqdVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		html string
		want string
	}{
		{name: "bare", html: `var x=1;vqd=4-1699999999;y=2`, want: "4-1699999999"},
		{name: "double quoted", html: `vqd="quoted-token";`, want: "quoted-token"},
		{
			name: "entity quoted",
			html: `vqd=&quot;entity-token&quot;`,
			want: "entity-token",
		},
		{name: "empty value", html: `vqd=`, want: ""},
		{name: "absent", html: `<div class="result">no token</div>`, want: ""},
		{
			name: "first wins",
			html: `vqd=first-token vqd="second"`,
			want: "first-token",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ExtractVqd(tc.html))
		})
	}
}

func TestExtractVqdFromFixture(t *testing.T) {
	t.Parallel()

	require.Equal(t, "4-1699999999", ExtractVqd(loadFixture(t, "results.html")))
}

func TestParseContinuationForm(t *testing.T) {
	t.Parallel()

	t.Run("fixture form", func(t *testing.T) {
		t.Parallel()
		values, ok := parseContinuationForm(loadFixture(t, "results.html"))
		require.True(t, ok)
		require.Equal(t, "54", values.Get("s"))
		require.Equal(t, "55", values.Get("dc"))
		require.Equal(t, "continuation-vqd-77", values.Get("vqd"))
		require.Equal(t, "us-en", values.Get("kl"))
		require.Equal(t, "d.js", values.Get("api"))
		require.Equal(t, "", values.Get("nextParams"))
		require.Equal(t, "json", values.Get("o"))
	})

	t.Run("no form", func(t *testing.T) {
		t.Parallel()
		_, ok := parseContinuationForm(loadFixture(t, "results_page2.html"))
		require.False(t, ok)
	})

	t.Run("form missing vqd", func(t *testing.T) {
		t.Parallel()
		html := `<form><input name="s" value="10"><input name="dc" value="11"></form>`
		_, ok := parseContinuationForm(html)
		require.False(t, ok)
	})

	t.Run("form missing s", func(t *testing.T) {
		t.Parallel()
		html := `<form><input name="vqd" value="tok"></form>`
		_, ok := parseContinuationForm(html)
		require.False(t, ok)
	})

	t.Run("input without value is ignored", func(t *testing.T) {
		t.Parallel()
		html := `<form><input name="s" value="10"><input name="vqd"></form>`
		_, ok := parseContinuationForm(html)
		require.False(t, ok)
	})
}

func TestNextPageForm(t *testing.T) {
	t.Parallel()

	current := newQueryForm("go slices", "week")
	continuation := url.Values{}
	continuation.Set("s", "54")
	continuation.Set("dc", "55")
	continuation.Set("vqd", "continuation-vqd-77")
	continuation.Set("kl", "us-en")

	merged := nextPageForm(current, continuation)

	require.Equal(t, "go slices", merged.Get("q"))
	require.Equal(t, "w", merged.Get("df"))
	require.True(t, merged.Has("b"))
	require.Equal(t, "54", merged.Get("s"))
	require.Equal(t, "55", merged.Get("dc"))
	require.Equal(t, "continuation-vqd-77", merged.Get("vqd"))
}

func TestRecencyToDF(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"day": "d", "week": "w", "month": "m", "year": "y",
		"hour": "", "": "", "fortnight": "",
	}
	for recency, want := range cases {
		require.Equal(t, want, recencyToDF(recency), "recency %q", recency)
	}
}

func TestClampNumResults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		primary, secondary, want int
	}{
		{primary: 0, secondary: 0, want: defaultNumResults},
		{primary: 0, secondary: 7, want: 7},
		{primary: 5, secondary: 30, want: 5},
		{primary: 50, secondary: 0, want: maxNumResults},
		{primary: -3, secondary: 0, want: 1},
		{primary: 20, secondary: 0, want: 20},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, clampNumResults(tc.primary, tc.secondary),
			"primary=%d secondary=%d", tc.primary, tc.secondary)
	}
}

func TestIsAnomalyResponse(t *testing.T) {
	t.Parallel()

	require.True(t, isAnomalyResponse(loadFixture(t, "anomaly.html")))
	require.False(t, isAnomalyResponse(loadFixture(t, "results.html")))
}

func TestAgeSeconds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	require.EqualValues(t, 1171800,
		ageSeconds("2026-09-01T10:30:00.0000000", now))
	require.EqualValues(t, 1296000, ageSeconds("2026-08-31", now))
	require.EqualValues(t, 0, ageSeconds("", now))
	require.EqualValues(t, 0, ageSeconds("not a date", now))
	require.EqualValues(t, -432000, ageSeconds("2026-09-20", now))
}

func TestSearchEndToEndWithPagination(t *testing.T) {
	t.Parallel()

	srv, requests := newFixtureServer(t,
		loadFixture(t, "results.html"),
		loadFixture(t, "results_page2.html"))
	p, _ := newTestProvider(srv)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query: "go slices",
		Limit: 20,
	})

	require.NoError(t, err)
	require.Equal(t, ProviderID, resp.Provider)
	require.Empty(t, resp.Answer)
	require.Len(t, resp.Sources, 14) // 6 from page 1 + 8 new from page 2

	all := requests()
	require.Len(t, all, 2)

	// First request: POST with browser fingerprint and query form.
	require.Equal(t, http.MethodPost, all[0].method)
	require.Equal(t, "go slices", all[0].form.Get("q"))
	require.Equal(t, "us-en", all[0].form.Get("kl"))
	require.True(t, all[0].form.Has("b"))
	require.Empty(t, all[0].form.Get("b"))
	require.False(t, all[0].form.Has("df"))
	require.Equal(t, chromeUserAgent, all[0].header.Get("User-Agent"))
	require.Equal(t, htmlReferer, all[0].header.Get("Referer"))
	require.Equal(t, "same-origin", all[0].header.Get("Sec-Fetch-Site"))
	require.Equal(t, "document", all[0].header.Get("Sec-Fetch-Dest"))
	require.Equal(t, `"macOS"`, all[0].header.Get("Sec-Ch-Ua-Platform"))
	require.Contains(t, all[0].header.Get("Content-Type"),
		"application/x-www-form-urlencoded")
	require.Contains(t, all[0].header.Get("Accept"), "text/html")

	// Second request: continuation fields adopted, q preserved.
	require.Equal(t, "go slices", all[1].form.Get("q"))
	require.Equal(t, "54", all[1].form.Get("s"))
	require.Equal(t, "55", all[1].form.Get("dc"))
	require.Equal(t, "continuation-vqd-77", all[1].form.Get("vqd"))

	// Dedupe across pages and field mapping on the first source.
	seen := make(map[string]int)
	for _, source := range resp.Sources {
		seen[source.URL]++
	}
	for url, count := range seen {
		require.Equal(t, 1, count, "url %s appears once", url)
	}
	require.Equal(t, "Effective Go - The Go Programming Language",
		resp.Sources[0].Title)
	require.Equal(t, "https://golang.org/docs/effective_go", resp.Sources[0].URL)
	require.Equal(t, "2026-09-01T10:30:00.0000000",
		resp.Sources[0].PublishedDate)
	require.EqualValues(t, 1171800, resp.Sources[0].AgeSeconds)
}

func TestSearchLimitTruncatesOnFirstPage(t *testing.T) {
	t.Parallel()

	srv, requests := newFixtureServer(t, loadFixture(t, "results.html"))
	p, _ := newTestProvider(srv)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query: "go slices",
		Limit: 5,
	})

	require.NoError(t, err)
	require.Len(t, resp.Sources, 5)
	require.Len(t, requests(), 1)
}

func TestSearchRecencyMapsToDF(t *testing.T) {
	t.Parallel()

	t.Run("week", func(t *testing.T) {
		t.Parallel()
		srv, requests := newFixtureServer(t, loadFixture(t, "results.html"))
		p, _ := newTestProvider(srv)
		_, err := p.Search(context.Background(), search.SearchParams{
			Query:   "go slices",
			Limit:   3,
			Recency: "week",
		})
		require.NoError(t, err)
		require.Equal(t, "w", requests()[0].form.Get("df"))
	})

	t.Run("unknown recency omits df", func(t *testing.T) {
		t.Parallel()
		srv, requests := newFixtureServer(t, loadFixture(t, "results.html"))
		p, _ := newTestProvider(srv)
		_, err := p.Search(context.Background(), search.SearchParams{
			Query:   "go slices",
			Limit:   3,
			Recency: "hour",
		})
		require.NoError(t, err)
		require.False(t, requests()[0].form.Has("df"))
	})
}

func TestSearchEmptyResultsReturnsError(t *testing.T) {
	t.Parallel()

	srv, _ := newFixtureServer(t,
		`<html><body><div class="results">no matches</div></body></html>`)
	p, _ := newTestProvider(srv)

	resp, err := p.Search(context.Background(), search.SearchParams{
		Query: "zzz nonexistent",
		Limit: 5,
	})

	require.Nil(t, resp)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no results parsed")
}

func TestSearchEmptyQueryRejected(t *testing.T) {
	t.Parallel()

	srv, requests := newFixtureServer(t, loadFixture(t, "results.html"))
	p, _ := newTestProvider(srv)

	_, err := p.Search(context.Background(), search.SearchParams{Query: "  "})

	require.Error(t, err)
	require.Contains(t, err.Error(), "empty query")
	require.Len(t, requests(), 0)
}

func TestSearchHTTPErrorClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		status    int
		body      string
		wantCode  string
		wantInMsg string
	}{
		{
			name:      "429 rate limited",
			status:    429,
			body:      "slow down",
			wantCode:  search.ErrorCodeRateLimited,
			wantInMsg: "429 rate limited",
		},
		{
			name:      "403 forbidden",
			status:    403,
			body:      "",
			wantCode:  search.ErrorCodeForbidden,
			wantInMsg: "403 forbidden",
		},
		{
			name:      "500 server error",
			status:    500,
			body:      "boom",
			wantCode:  search.ErrorCodeServerError,
			wantInMsg: "500 server error",
		},
		{
			name:      "402 billing",
			status:    402,
			body:      "",
			wantCode:  search.ErrorCodeBilling,
			wantInMsg: "402 credits exhausted",
		},
		{
			name:      "body billing signal wins over 403",
			status:    403,
			body:      "insufficient credits quota",
			wantCode:  search.ErrorCodeBilling,
			wantInMsg: "credits exhausted",
		},
		{
			name:      "unclassified status falls back to generic",
			status:    400,
			body:      "bad request",
			wantCode:  "",
			wantInMsg: "DuckDuckGo HTML error (400)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newStatusServer(t, tc.status, tc.body)
			p, _ := newTestProvider(srv)
			resp, err := p.Search(context.Background(), search.SearchParams{
				Query: "go",
				Limit: 5,
			})
			require.Nil(t, resp)
			require.Error(t, err)
			var spe *search.SearchProviderError
			require.ErrorAs(t, err, &spe)
			require.Equal(t, tc.wantCode, spe.Code)
			require.Equal(t, tc.status, spe.HTTPStatus)
			require.Contains(t, spe.Message, tc.wantInMsg)
		})
	}
}

func TestSearchAnomalyTriggersCooldownFastFail(t *testing.T) {
	t.Parallel()

	srv, requests := newFixtureServer(t,
		loadFixture(t, "anomaly.html"),
		loadFixture(t, "anomaly.html"))
	p, clock := newTestProvider(srv)
	ctx := context.Background()
	params := search.SearchParams{Query: "go", Limit: 5}

	// First hit: the challenge page classifies as rate limited.
	_, err := p.Search(ctx, params)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)
	require.Contains(t, spe.Message, "bot-detection challenge")
	require.Len(t, requests(), 1)

	// Cooldown holds: both probes report unavailable.
	available, err := p.IsAvailable(ctx, nil)
	require.False(t, available)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cooldown active")
	explicit, err := p.IsExplicitlyAvailable(ctx, nil)
	require.False(t, explicit)
	require.Error(t, err)

	// Search fast-fails without touching the network.
	_, err = p.Search(ctx, params)
	require.Error(t, err)
	require.ErrorAs(t, err, &spe)
	require.Contains(t, spe.Message, "cooldown active")
	require.Len(t, requests(), 1)

	// Cooldown expiry re-admits the provider, which retries and hits the
	// challenge again.
	clock.Advance(cooldownDuration + time.Second)
	available, err = p.IsAvailable(ctx, nil)
	require.True(t, available)
	require.NoError(t, err)
	_, err = p.Search(ctx, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bot-detection challenge")
	require.Len(t, requests(), 2)
}

func TestCooldownStateWindow(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Unix(0, 0)}
	cs := newCooldown(clock.Now)

	require.False(t, cs.active())
	require.EqualValues(t, 0, cs.remaining())

	cs.trigger(cooldownDuration)
	require.True(t, cs.active())
	require.Equal(t, cooldownDuration, cs.remaining())

	clock.Advance(4 * time.Minute)
	require.True(t, cs.active())
	require.Equal(t, time.Minute, cs.remaining())

	clock.Advance(2 * time.Minute)
	require.False(t, cs.active())
	require.EqualValues(t, 0, cs.remaining())
}

func TestNewWiresPackageDefaults(t *testing.T) {
	t.Parallel()

	p := New(nil)

	require.Equal(t, htmlEndpoint, p.endpoint)
	require.Same(t, packageCooldown, p.cooldown)

	available, err := p.IsAvailable(context.Background(), nil)
	require.True(t, available)
	require.NoError(t, err)
}

func TestMeta(t *testing.T) {
	t.Parallel()

	meta := Meta()

	require.Equal(t, ProviderID, meta.ID)
	require.Equal(t, ProviderLabel, meta.Label)
	require.Positive(t, meta.Priority)
	require.NotNil(t, meta.NewFunc)

	var provider search.ProviderV2 = meta.NewFunc()
	require.Equal(t, ProviderID, provider.ID())
}
