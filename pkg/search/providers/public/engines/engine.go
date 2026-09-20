// Package engines implements the credential-free HTML sub-engines the
// public aggregate fans out to: Startpage, Google, DuckDuckGo (via the
// standalone scraper package), Ecosia, and Mojeek. Each engine owns its
// request shape, bot-challenge detection, and cooldown window, mirroring
// the upstream TypeScript scrapers over plain HTTP fetches.
package engines

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/providers/duckduckgo"
)

// SubEngine is one credential-free search engine the public aggregate
// fans out to. Engines receive the HTTP client per call so the aggregate
// controls transport injection; cooldown and bot-challenge detection are
// self-managed, mirroring the standalone providers.
type SubEngine interface {
	// ID returns the stable engine identifier (e.g. "startpage").
	ID() string

	// Fetch executes one search and returns sources in engine rank
	// order. A nil error with zero sources is a valid empty answer; any
	// error fails only this engine inside the fan-out.
	Fetch(ctx context.Context, client *http.Client, queryString string, limit int) ([]search.SearchSource, error)
}

// DuckDuckGoID is the stable engine identifier for the adapted scraper.
const DuckDuckGoID = "duckduckgo"

// ddgEngine adapts the standalone DuckDuckGo scraper provider onto the
// SubEngine contract. The scraper owns its request flow (POST form,
// pagination, anomaly cooldown), so the adapter only forwards the call
// and unwraps the response.
type ddgEngine struct{}

// ID implements SubEngine.
func (ddgEngine) ID() string { return DuckDuckGoID }

// Fetch implements SubEngine by delegating to a fresh scraper provider
// over the injected client. The scraper shares its process-wide anomaly
// cooldown with every other instance, so a bot challenge blocks later
// calls without hammering the endpoint.
func (ddgEngine) Fetch(ctx context.Context, client *http.Client, queryString string, limit int) ([]search.SearchSource, error) {
	resp, err := duckduckgo.New(client).Search(ctx, search.SearchParams{
		Query: queryString,
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return resp.Sources, nil
}

// DefaultEngines returns the fan-out roster in engine-priority order:
// Google-index engines (startpage, google) lead, DuckDuckGo and Ecosia
// follow, and Mojeek's independent index breaks remaining ties — the
// same ordering the upstream aggregate uses as the ranking tiebreak
// (measured 2026-07).
func DefaultEngines() []SubEngine {
	return []SubEngine{
		NewStartpageEngine(),
		NewGoogleEngine(),
		ddgEngine{},
		NewEcosiaEngine(),
		NewMojeekEngine(),
	}
}

// Per-engine result-count bounds, mirroring the upstream per-engine
// clamps.
const (
	// defaultEngineLimit is the result count when the caller gives none.
	defaultEngineLimit = 10

	// maxEngineLimit caps the per-engine result count.
	maxEngineLimit = 20
)

// maxBodyBytes caps how much response HTML one engine reads.
const maxBodyBytes = 8 << 20

// cooldownDuration is how long an engine stays unavailable after a
// bot-challenge page. The upstream TS has no backoff; this window is
// the Go port's addition so the fan-out stops hammering a blocked
// engine.
const cooldownDuration = 5 * time.Minute

// clampEngineLimit resolves the per-engine result count: absent values
// mean the default and the count is clamped to [1, max], mirroring the
// upstream clampNumResults helper.
func clampEngineLimit(limit int) int {
	if limit == 0 {
		return defaultEngineLimit
	}
	if limit > maxEngineLimit {
		return maxEngineLimit
	}
	if limit < 1 {
		return 1
	}
	return limit
}

// chromeUserAgent is the deterministic desktop Mac Chrome fingerprint
// shared by every HTML engine, matching the standalone DuckDuckGo
// scraper; the TS engines randomize profiles per request, which a
// plain Go client cannot reproduce.
const chromeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)" +
	" AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

// setNavigationHeaders applies the deterministic desktop Chrome
// navigation fingerprint every engine sends, plus the engine's own
// referer. POST callers set the form content type themselves.
// Accept-Encoding is left unset so the transport negotiates gzip and
// decompresses it itself.
func setNavigationHeaders(h http.Header, referer string) {
	h.Set("User-Agent", chromeUserAgent)
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,"+
		"image/avif,image/webp,image/apng,*/*;q=0.8,"+
		"application/signed-exchange;v=b3;q=0.7")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Cache-Control", "max-age=0")
	h.Set("Priority", "u=0, i")
	h.Set("Sec-Ch-Ua", `"Google Chrome";v="149", "Chromium";v="149",`+
		` ";Not A Brand";v="99"`)
	h.Set("Sec-Ch-Ua-Mobile", "?0")
	h.Set("Sec-Ch-Ua-Platform", `"macOS"`)
	h.Set("Sec-Fetch-Dest", "document")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Set("Sec-Fetch-User", "?1")
	h.Set("Upgrade-Insecure-Requests", "1")
	h.Set("Referer", referer)
}

// enginePage is one fetched HTML page: the final URL after client-side
// redirects, the HTTP status, and the capped body.
type enginePage struct {
	status   int
	finalURL string
	body     string
}

// fetchPage executes req with client and returns the response page.
// Transport-level failures map onto a provider error tagged with
// providerID so the fan-out records a classified engine failure.
func fetchPage(ctx context.Context, client *http.Client, providerID string, req *http.Request) (enginePage, error) {
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return enginePage{}, &search.SearchProviderError{
			Provider: providerID,
			Message:  err.Error(),
		}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return enginePage{}, &search.SearchProviderError{
			Provider: providerID,
			Message:  fmt.Sprintf("read response: %v", err),
		}
	}
	finalURL := req.URL.String()
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return enginePage{
		status:   resp.StatusCode,
		finalURL: finalURL,
		body:     string(body),
	}, nil
}

// creditBodyPattern mirrors the root search package's billing-signal
// detector; the root helper is unexported, so the engines keep a local
// replica with identical semantics.
var creditBodyPattern = regexp.MustCompile(
	`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

// classifyStatus maps a non-2xx status and body onto a classified
// provider error, mirroring search.classifyHTTPError. Body-level
// billing signals win over status codes. It returns nil when nothing
// matches, leaving the caller free to raise a generic error.
func classifyStatus(providerID string, status int, body string) *search.SearchProviderError {
	if creditBodyPattern.MatchString(body) {
		return &search.SearchProviderError{
			Provider:   providerID,
			Code:       search.ErrorCodeBilling,
			Message:    "credits exhausted",
			HTTPStatus: status,
		}
	}
	switch {
	case status == 402:
		return &search.SearchProviderError{
			Provider:   providerID,
			Code:       search.ErrorCodeBilling,
			Message:    "402 credits exhausted",
			HTTPStatus: status,
		}
	case status == 401:
		return &search.SearchProviderError{
			Provider:   providerID,
			Code:       search.ErrorCodeUnauthorized,
			Message:    "401 unauthorized",
			HTTPStatus: status,
		}
	case status == 403:
		return &search.SearchProviderError{
			Provider:   providerID,
			Code:       search.ErrorCodeForbidden,
			Message:    "403 forbidden",
			HTTPStatus: status,
		}
	case status == 429:
		return &search.SearchProviderError{
			Provider:   providerID,
			Code:       search.ErrorCodeRateLimited,
			Message:    "429 rate limited",
			HTTPStatus: status,
		}
	case status >= 500:
		return &search.SearchProviderError{
			Provider:   providerID,
			Code:       search.ErrorCodeServerError,
			Message:    fmt.Sprintf("%d server error", status),
			HTTPStatus: status,
		}
	}
	return nil
}

// httpStatusError builds the generic non-2xx error for an engine page.
func httpStatusError(providerID string, label string, status int) *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider:   providerID,
		Message:    fmt.Sprintf("%s HTML error (%d)", label, status),
		HTTPStatus: status,
	}
}

// cooldownState tracks one engine's backoff window with an injectable
// clock for tests.
type cooldownState struct {
	mu    sync.Mutex
	until time.Time
	now   func() time.Time
}

// newCooldown creates an isolated backoff window ticking with now; a
// nil now falls back to time.Now.
func newCooldown(now func() time.Time) *cooldownState {
	return &cooldownState{now: now}
}

// clock returns the current time, defaulting to time.Now.
func (c *cooldownState) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// trigger starts or restarts the backoff window lasting d from now.
func (c *cooldownState) trigger(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.until = c.clock().Add(d)
}

// active reports whether the backoff window still holds.
func (c *cooldownState) active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clock().Before(c.until)
}

// remaining reports how long the backoff window still holds, zero once
// expired.
func (c *cooldownState) remaining() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.until.Sub(c.clock())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// cooldownError builds the fast-fail error raised while an engine's
// backoff window after a bot challenge still holds. It carries 429
// semantics so the fan-out records the engine as rate-limited.
func cooldownError(providerID string, remaining time.Duration) *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider: providerID,
		Code:     search.ErrorCodeRateLimited,
		Message: fmt.Sprintf("cooldown active after bot-detection "+
			"challenge, retry in %s", remaining.Round(time.Second)),
		HTTPStatus: 429,
	}
}

// challengeError builds the classified error raised when an engine
// answers with a bot-challenge page instead of results.
func challengeError(providerID, message string) *search.SearchProviderError {
	return &search.SearchProviderError{
		Provider:   providerID,
		Code:       search.ErrorCodeRateLimited,
		Message:    message,
		HTTPStatus: 429,
	}
}

// parsedResult is one decoded engine row before mapping onto
// search.SearchSource.
type parsedResult struct {
	title   string
	url     string
	snippet string
}

// normalizeText collapses element text into one trimmed line. The DOM
// walk already drops tags and the parser already decoded entities, so
// only whitespace folding remains of the upstream decodeHtmlText.
func normalizeText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// collectSources dedupes parsed results by URL in document order and
// truncates to limit, mirroring the per-engine TS collection loop.
func collectSources(results []parsedResult, limit int) []search.SearchSource {
	sources := make([]search.SearchSource, 0, limit)
	seen := make(map[string]struct{}, limit)
	for _, result := range results {
		if _, dup := seen[result.url]; dup {
			continue
		}
		seen[result.url] = struct{}{}
		sources = append(sources, search.SearchSource{
			Title:   result.title,
			URL:     result.url,
			Snippet: result.snippet,
		})
		if len(sources) >= limit {
			break
		}
	}
	return sources
}
