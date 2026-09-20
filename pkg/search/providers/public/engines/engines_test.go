package engines

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// fakeClock is a manually advanced clock for cooldown tests.
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

// recordedRequest captures one outbound request for assertions.
type recordedRequest struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

// recorder collects requests served by a fixture server.
type recorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

// add records one request.
func (r *recorder) add(req recordedRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
}

// all returns a copy of the recorded requests.
func (r *recorder) all() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRequest(nil), r.requests...)
}

// count returns the number of recorded requests.
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// servePage writes one HTML page.
func servePage(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// captureRequest records the inbound request before the handler runs.
func captureRequest(r *recorder, w http.ResponseWriter, req *http.Request, next func(w http.ResponseWriter)) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	r.add(recordedRequest{
		method: req.Method,
		path:   req.URL.Path,
		query:  req.URL.RawQuery,
		header: req.Header.Clone(),
		body:   string(body),
	})
	next(w)
}

// newTestStartpage builds a startpage engine pointed at srv with an
// isolated cooldown window, keeping tests parallel-safe.
func newTestStartpage(srv *httptest.Server, clock *fakeClock) *StartpageEngine {
	engine := NewStartpageEngine()
	engine.homeURL = srv.URL + "/"
	engine.searchURL = srv.URL + "/sp/search"
	engine.cooldown = newCooldown(clock.Now)
	return engine
}

func TestParseStartpageResultsFixture(t *testing.T) {
	t.Parallel()

	results := parseStartpageResults(loadFixture(t, "startpage.html"), startpageHomeURL)
	require.NotEmpty(t, results)
	require.Equal(t, "A Tour of Go", results[0].title)
	require.Equal(t, "https://example.com/golang-tour", results[0].url)
	require.Equal(t, "Welcome to a Tour of Go, an interactive introduction to the language.", results[0].snippet)
	require.Equal(t, "The Go Programming Language", results[1].title)
	require.Equal(t, "https://go.dev/", results[1].url)
	// The captcha self-link, mailto scheme, anchor-less block, honeypot,
	// and sponsored container are all filtered out.
	require.Len(t, results, 2)
}

func TestStartpageHomepageTokenFormFlow(t *testing.T) {
	t.Parallel()

	const homeHTML = `<html><body>
<form action="/sp/search" method="post">
  <input type="hidden" name="sc" value="token-123"/>
  <input type="hidden" name="language" value="english"/>
  <input type="hidden" name="cat" value="web"/>
  <input type="text" name="query"/>
</form>
</body></html>`
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(rec, w, r, func(w http.ResponseWriter) {
			switch r.URL.Path {
			case "/":
				servePage(w, homeHTML)
			case "/sp/search":
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				servePage(w, loadFixture(t, "startpage.html"))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		})
	}))
	t.Cleanup(srv.Close)

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	engine := newTestStartpage(srv, clock)
	sources, err := engine.Fetch(context.Background(), srv.Client(), "go tour", 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	requests := rec.all()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodGet, requests[0].method) // homepage lift
	require.Equal(t, http.MethodPost, requests[1].method)
	require.Contains(t, requests[1].body, "sc=token-123")
	require.Contains(t, requests[1].body, "query=go+tour")
	require.NotEmpty(t, requests[1].header.Get("User-Agent"))
	require.Equal(t, "application/x-www-form-urlencoded",
		requests[1].header.Get("Content-Type"))
}

func TestStartpageTokenlessGetFallback(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(rec, w, r, func(w http.ResponseWriter) {
			if r.URL.Path == "/sp/search" {
				servePage(w, loadFixture(t, "startpage.html"))
				return
			}
			servePage(w, "<html><body>no form here</body></html>")
		})
	}))
	t.Cleanup(srv.Close)

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	engine := newTestStartpage(srv, clock)
	sources, err := engine.Fetch(context.Background(), srv.Client(), "go tour", 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	requests := rec.all()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, http.MethodGet, requests[1].method)
	require.Contains(t, requests[1].query, "query=go+tour")
}

func TestStartpageChallengeTriggersCooldown(t *testing.T) {
	t.Parallel()

	const homeHTML = `<form action="/sp/search"><input type="hidden" name="sc" value="t"/></form>`
	const captchaShell = `<html><head>
<script src="/apps/gatsby/chunk-map.js"></script>
</head><body>component---src-pages-captcha-tsx</body></html>`
	rec := &recorder{}
	searchCalls := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(rec, w, r, func(w http.ResponseWriter) {
			switch r.URL.Path {
			case "/":
				servePage(w, homeHTML)
			case "/sp/search":
				mu.Lock()
				searchCalls++
				calls := searchCalls
				mu.Unlock()
				if calls == 1 {
					http.Redirect(w, r, "/en/errors/captcha", http.StatusFound)
					return
				}
				servePage(w, loadFixture(t, "startpage.html"))
			case "/en/errors/captcha":
				servePage(w, captchaShell)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		})
	}))
	t.Cleanup(srv.Close)

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	engine := newTestStartpage(srv, clock)

	_, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, StartpageID, spe.Provider)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)

	// While the cooldown holds, Fetch fails fast without a request.
	before := rec.count()
	_, err = engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	require.ErrorAs(t, err, &spe)
	require.Contains(t, spe.Message, "cooldown active")
	require.Equal(t, before, rec.count())

	// After the window lapses the engine recovers.
	clock.Advance(cooldownDuration + time.Second)
	sources, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)
}

// newTestGoogle builds a google engine pointed at srv with an isolated
// cooldown window.
func newTestGoogle(srv *httptest.Server, clock *fakeClock) *GoogleEngine {
	engine := NewGoogleEngine()
	engine.homeURL = srv.URL + "/"
	engine.searchURL = srv.URL + "/search"
	engine.cooldown = newCooldown(clock.Now)
	return engine
}

func TestParseGoogleResultsFixture(t *testing.T) {
	t.Parallel()

	results := parseGoogleResults(loadFixture(t, "google.html"))
	require.NotEmpty(t, results)
	require.Equal(t, "Go Documentation", results[0].title)
	require.Equal(t, "https://example.com/go-docs", results[0].url)
	require.Equal(t, "The official documentation for the Go programming language.", results[0].snippet)
	// The /url wrapper is unwrapped to its q target.
	require.Equal(t, "The Go Blog: slices", results[1].title)
	require.Equal(t, "https://go.dev/blog/slices", results[1].url)
	require.Equal(t, "Blog post about the slices package.", results[1].snippet)
	// Internal host, mailto, empty redirect, and bare heading drop out.
	require.Len(t, results, 2)
}

func TestGoogleRequestShape(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(rec, w, r, func(w http.ResponseWriter) {
			servePage(w, loadFixture(t, "google.html"))
		})
	}))
	t.Cleanup(srv.Close)

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	engine := newTestGoogle(srv, clock)
	sources, err := engine.Fetch(context.Background(), srv.Client(), "go tour", 12)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	requests := rec.all()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/search", requests[0].path)
	for _, param := range []string{"q=go+tour", "num=12", "hl=en", "gl=us", "udm=14", "pws=0"} {
		require.Contains(t, requests[0].query, param)
	}
	require.Equal(t, chromeUserAgent, requests[0].header.Get("User-Agent"))
}

func TestGoogleTrafficWallByStatus(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "<html><body>Our systems have detected unusual traffic from your computer.</body></html>")
	}))
	t.Cleanup(srv.Close)

	engine := newTestGoogle(srv, clock)
	_, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, GoogleID, spe.Provider)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)
	require.True(t, engine.cooldown.active(), "traffic wall should arm the cooldown")
}

func TestGoogleTrafficWallBySorryRedirect(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			http.Redirect(w, r, "/sorry/index", http.StatusFound)
			return
		}
		servePage(w, "<html><body>sorry</body></html>")
	}))
	t.Cleanup(srv.Close)

	engine := newTestGoogle(srv, clock)
	_, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, 429, spe.HTTPStatus)
	require.Contains(t, spe.Message, "automated-traffic")
}

func TestGoogleJavaScriptWall(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		servePage(w, `<html><body>Please click <a href="/httpservice/retry/enablejs">here</a> if you are not redirected.</body></html>`)
	}))
	t.Cleanup(srv.Close)

	engine := newTestGoogle(srv, clock)
	_, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, 429, spe.HTTPStatus)
	require.Contains(t, spe.Message, "JavaScript challenge")
}

// newTestEcosia builds an ecosia engine pointed at srv with an
// isolated cooldown window.
func newTestEcosia(srv *httptest.Server, clock *fakeClock) *EcosiaEngine {
	engine := NewEcosiaEngine()
	engine.homeURL = srv.URL + "/"
	engine.searchURL = srv.URL + "/search"
	engine.cooldown = newCooldown(clock.Now)
	return engine
}

func TestParseEcosiaResultsFixture(t *testing.T) {
	t.Parallel()

	results := parseEcosiaResults(loadFixture(t, "ecosia.html"), ecosiaHomeURL)
	require.NotEmpty(t, results)
	require.Equal(t, "Ecosia result one", results[0].title)
	require.Equal(t, "https://example.com/ecosia-one", results[0].url)
	require.Equal(t, "First ecosia snippet with the preferred inner paragraph.", results[0].snippet)
	require.Equal(t, "Ecosia result two", results[1].title)
	require.Equal(t, "https://example.com/ecosia-two", results[1].url)
	require.Equal(t, "Second snippet from the fallback container.", results[1].snippet)
	// The internal vertical and the ad slot never survive.
	require.Len(t, results, 2)
}

func TestEcosiaRequestShape(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(rec, w, r, func(w http.ResponseWriter) {
			servePage(w, loadFixture(t, "ecosia.html"))
		})
	}))
	t.Cleanup(srv.Close)

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	engine := newTestEcosia(srv, clock)
	sources, err := engine.Fetch(context.Background(), srv.Client(), "go tour", 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	requests := rec.all()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/search", requests[0].path)
	require.Contains(t, requests[0].query, "q=go+tour")
}

func TestEcosiaCloudflareWall(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<html><head><title>Ecosia Firewall</title>
<script type="text/javascript">window._cf_chl_opt={}</script>
<script src="/cdn-cgi/challenge-platform/scripts/jsd/main.js"></script>
</head></html>`)
	}))
	t.Cleanup(srv.Close)

	engine := newTestEcosia(srv, clock)
	_, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, EcosiaID, spe.Provider)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)
	require.True(t, engine.cooldown.active(), "cloudflare wall should arm the cooldown")
}

// newTestMojeek builds a mojeek engine pointed at srv with an isolated
// cooldown window.
func newTestMojeek(srv *httptest.Server, clock *fakeClock) *MojeekEngine {
	engine := NewMojeekEngine()
	engine.homeURL = srv.URL + "/?arc=none&lang=en&lb=en&theme=dark"
	engine.searchURL = srv.URL + "/search"
	engine.cooldown = newCooldown(clock.Now)
	return engine
}

func TestParseMojeekResultsFixture(t *testing.T) {
	t.Parallel()

	results := parseMojeekResults(loadFixture(t, "mojeek.html"), mojeekHomeURL)
	require.NotEmpty(t, results)
	require.Equal(t, "Mojeek result one", results[0].title)
	require.Equal(t, "https://example.com/mojeek-one", results[0].url)
	require.Equal(t, "First mojeek snippet from the independent index.", results[0].snippet)
	require.Equal(t, "Mojeek clustered result", results[1].title)
	require.Equal(t, "https://example.com/mojeek-two", results[1].url)
	// The infobox row, the internal link, and the wrong list drop out.
	require.Len(t, results, 2)
}

func TestMojeekRequestShapeAndLimitClamp(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureRequest(rec, w, r, func(w http.ResponseWriter) {
			servePage(w, loadFixture(t, "mojeek.html"))
		})
	}))
	t.Cleanup(srv.Close)

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	engine := newTestMojeek(srv, clock)

	_, err := engine.Fetch(context.Background(), srv.Client(), "go tour", 50)
	require.NoError(t, err)
	_, err = engine.Fetch(context.Background(), srv.Client(), "go tour", 0)
	require.NoError(t, err)

	requests := rec.all()
	require.Len(t, requests, 2)
	require.Contains(t, requests[0].query, "q=go+tour")
	require.Contains(t, requests[0].query, "t=20") // clamped to the cap
	require.Contains(t, requests[0].query, "arc=none")
	require.Contains(t, requests[0].query, "lang=en")
	require.Contains(t, requests[0].query, "lb=en")
	require.Contains(t, requests[0].query, "theme=dark")
	require.Contains(t, requests[1].query, "t=10") // default when unset
}

func TestMojeekAltchaWall(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		servePage(w, `<html><head><title>Captcha</title></head><body>
<div class="captcha-wrap"><altcha-widget challengeurl="/altcha"></altcha-widget></div>
</body></html>`)
	}))
	t.Cleanup(srv.Close)

	engine := newTestMojeek(srv, clock)
	_, err := engine.Fetch(context.Background(), srv.Client(), "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, MojeekID, spe.Provider)
	require.Equal(t, search.ErrorCodeRateLimited, spe.Code)
	require.Equal(t, 429, spe.HTTPStatus)
	require.Contains(t, spe.Message, "ALTCHA")
	require.True(t, engine.cooldown.active(), "altcha wall should arm the cooldown")
}

// roundTripFunc adapts a function onto http.RoundTripper so the ddg
// adapter can be exercised without network access.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// htmlResponse builds a canned HTTP response for a fake transport.
func htmlResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestDuckDuckGoAdapterFetch(t *testing.T) {
	t.Parallel()

	const ddgHTML = `<!DOCTYPE html>
<html><body>
<div class="result results_links results_links_deep web-result">
  <h2 class="result__title">
    <a rel="nofollow" class="result__a"
       href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fgo%2Fddg&amp;rut=abc">Example DDG result</a>
  </h2>
  <a class="result__snippet" href="//duckduckgo.com/l/?kk=1">The snippet text.</a>
</div>
</body></html>`
	var postedQuery string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		postedQuery = values.Get("q")
		return htmlResponse(req, http.StatusOK, ddgHTML), nil
	})}

	engine := ddgEngine{}
	require.Equal(t, DuckDuckGoID, engine.ID())
	sources, err := engine.Fetch(context.Background(), client, "go tour", 10)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, "Example DDG result", sources[0].Title)
	require.Equal(t, "https://example.com/go/ddg", sources[0].URL)
	require.Equal(t, "The snippet text.", sources[0].Snippet)
	require.Equal(t, "go tour", postedQuery)
}

func TestDuckDuckGoAdapterMapsErrors(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("transport exploded")
	})}

	engine := ddgEngine{}
	_, err := engine.Fetch(context.Background(), client, "go", 10)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, DuckDuckGoID, spe.Provider)
	require.Contains(t, spe.Message, "transport exploded")
}

func TestDefaultEnginesRoster(t *testing.T) {
	t.Parallel()

	roster := DefaultEngines()
	ids := make([]string, 0, len(roster))
	for _, engine := range roster {
		ids = append(ids, engine.ID())
	}
	require.Equal(t, []string{StartpageID, GoogleID, DuckDuckGoID, EcosiaID, MojeekID}, ids)
}
