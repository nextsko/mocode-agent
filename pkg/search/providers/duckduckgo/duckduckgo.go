// Package duckduckgo implements a credential-free web search provider on
// top of DuckDuckGo's no-JS HTML frontend, mirroring the upstream
// TypeScript scraper: POST form-encoded queries with a desktop Chrome
// fingerprint, parse .result blocks with goquery, unwrap uddg redirect
// links, and cool down after bot-detection challenges.
package duckduckgo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// Provider identity constants.
const (
	// ProviderID is the stable provider identifier.
	ProviderID = "duckduckgo"

	// ProviderLabel is the human-readable display name.
	ProviderLabel = "DuckDuckGo"
)

// Request shape constants, mirroring the upstream scraper.
const (
	// htmlEndpoint is DuckDuckGo's no-JS HTML search frontend.
	htmlEndpoint = "https://html.duckduckgo.com/html/"

	// htmlReferer is the same-origin referer the upstream browserFetch
	// sends alongside its Sec-Fetch-Site override.
	htmlReferer = "https://html.duckduckgo.com/"

	// defaultKl is DuckDuckGo's region code, used until locale
	// negotiation lands with the structured query parser.
	defaultKl = "us-en"

	// defaultNumResults is the result count when the caller gives none.
	defaultNumResults = 10

	// maxNumResults caps the requested result count.
	maxNumResults = 20

	// requestTimeout bounds one outbound HTTP request, mirroring the
	// upstream SEARCH_HARD_TIMEOUT_MS default.
	requestTimeout = 60 * time.Second

	// maxBodyBytes caps how much response HTML is read.
	maxBodyBytes = 8 << 20
)

// chromeUserAgent is the deterministic desktop Mac Chrome fingerprint
// from the upstream CHROME_FALLBACK_HEADERS; the TS scraper randomizes
// profiles per request, which a plain Go client cannot reproduce.
const chromeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)" +
	" AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

// DDGProvider searches the web through DuckDuckGo's no-JS HTML frontend.
// It needs no credentials: availability is true unless a bot-detection
// challenge put the provider on cooldown. The HTTP client arrives by
// constructor injection, keeping SearchParams pure data.
type DDGProvider struct {
	client   *http.Client
	endpoint string
	cooldown *cooldownState
	now      func() time.Time
}

// Interface conformance check at compile time.
var _ search.ProviderV2 = (*DDGProvider)(nil)

// New creates a provider over the given HTTP client, defaulting to a
// plain client when nil. The instance shares the package-wide cooldown
// window with every other DDGProvider in the process, so concurrent
// instances cannot hammer past a bot challenge.
func New(client *http.Client) *DDGProvider {
	if client == nil {
		client = &http.Client{}
	}
	return &DDGProvider{
		client:   client,
		endpoint: htmlEndpoint,
		cooldown: packageCooldown,
		now:      time.Now,
	}
}

// Meta returns the registry metadata for this provider. The priority
// places the credential-free scraper after credentialed chain members as
// a last-resort fallback. Registration is explicit; there is no init
// hook, so importing the package never mutates a registry.
func Meta() search.ProviderMeta {
	return search.ProviderMeta{
		ID:       ProviderID,
		Label:    ProviderLabel,
		Priority: 50,
		NewFunc:  func() search.ProviderV2 { return New(nil) },
	}
}

// ID implements search.ProviderV2.
func (p *DDGProvider) ID() string { return ProviderID }

// Label implements search.ProviderV2.
func (p *DDGProvider) Label() string { return ProviderLabel }

// IsAvailable implements search.ProviderV2. The scraper has no
// credentials to probe, so it is available unless a bot-detection
// challenge keeps it on cooldown. The broker is unused and may be nil.
func (p *DDGProvider) IsAvailable(ctx context.Context, broker search.CredentialBroker) (bool, error) {
	if p.cooldown.active() {
		return false, cooldownError(p.cooldown.remaining())
	}
	return true, nil
}

// IsExplicitlyAvailable mirrors IsAvailable: an explicit user selection
// is still bound by the challenge cooldown, since the block applies to
// every request this process could send.
func (p *DDGProvider) IsExplicitlyAvailable(ctx context.Context, broker search.CredentialBroker) (bool, error) {
	return p.IsAvailable(ctx, broker)
}

// Search implements search.ProviderV2: it POSTs the query form to the
// HTML frontend, parses result rows, and follows the hidden continuation
// form while more sources are needed. An empty result set is an error so
// the chain falls through to the next provider.
func (p *DDGProvider) Search(ctx context.Context, params search.SearchParams) (*search.SearchResponse, error) {
	if p.cooldown.active() {
		return nil, cooldownError(p.cooldown.remaining())
	}
	if strings.TrimSpace(params.Query) == "" {
		return nil, &search.SearchProviderError{
			Provider: ProviderID,
			Message:  "empty query",
		}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	numResults := clampNumResults(params.NumSearchResults, params.Limit)
	seen := make(map[string]struct{})
	var sources []search.SearchSource
	form := newQueryForm(params.Query, params.Recency)

	for form != nil && len(sources) < numResults {
		html, err := p.fetchHTML(ctx, form)
		if err != nil {
			return nil, err
		}
		before := len(sources)
		for _, row := range parseResults(html) {
			if _, dup := seen[row.url]; dup {
				continue
			}
			seen[row.url] = struct{}{}
			sources = append(sources, row.toSource(p.clock()))
			if len(sources) >= numResults {
				break
			}
		}
		if len(sources) == before {
			break // page added nothing new; stop paging
		}
		if continuation, ok := parseContinuationForm(html); ok {
			form = nextPageForm(form, continuation)
		} else {
			form = nil
		}
	}

	if len(sources) == 0 {
		return nil, &search.SearchProviderError{
			Provider: ProviderID,
			Message:  "no results parsed from DuckDuckGo HTML",
		}
	}
	return &search.SearchResponse{Provider: ProviderID, Sources: sources}, nil
}

// clock returns the provider's current time, defaulting to time.Now.
func (p *DDGProvider) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// clampNumResults resolves the effective result count:
// NumSearchResults wins over Limit, absent values mean the default, and
// the count is clamped to [1, maxNumResults], mirroring the upstream
// clampNumResults helper.
func clampNumResults(primary, secondary int) int {
	value := primary
	if value == 0 {
		value = secondary
	}
	if value == 0 {
		return defaultNumResults
	}
	if value > maxNumResults {
		return maxNumResults
	}
	if value < 1 {
		return 1
	}
	return value
}

// newQueryForm builds the first-page POST form: the raw query, the us-en
// region code, the df recency filter, and the empty b marker a real
// browser form submission includes.
func newQueryForm(query, recency string) url.Values {
	form := url.Values{}
	form.Set("q", query)
	form.Set("kl", defaultKl)
	if df := recencyToDF(recency); df != "" {
		form.Set("df", df)
	}
	form.Set("b", "")
	return form
}

// recencyToDF maps the unified recency filter onto DuckDuckGo's df form
// parameter; unmapped values mean no filter.
func recencyToDF(recency string) string {
	switch recency {
	case "day":
		return "d"
	case "week":
		return "w"
	case "month":
		return "m"
	case "year":
		return "y"
	default:
		return ""
	}
}

// fetchHTML POSTs one form to the HTML frontend and returns the response
// body. Non-2xx statuses map onto classified provider errors; a
// bot-challenge page triggers the cooldown and fails with the challenge
// error.
func (p *DDGProvider) fetchHTML(ctx context.Context, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", &search.SearchProviderError{
			Provider: ProviderID,
			Message:  fmt.Sprintf("build request: %v", err),
		}
	}
	setNavigationHeaders(req.Header)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", &search.SearchProviderError{
			Provider: ProviderID,
			Message:  err.Error(),
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", &search.SearchProviderError{
			Provider: ProviderID,
			Message:  fmt.Sprintf("read response: %v", err),
		}
	}
	html := string(body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if classified := classifyStatus(resp.StatusCode, html); classified != nil {
			return "", classified
		}
		return "", &search.SearchProviderError{
			Provider:   ProviderID,
			Message:    fmt.Sprintf("DuckDuckGo HTML error (%d)", resp.StatusCode),
			HTTPStatus: resp.StatusCode,
		}
	}
	if isAnomalyResponse(html) {
		p.cooldown.trigger(cooldownDuration)
		return "", challengeError()
	}
	return html, nil
}

// setNavigationHeaders applies the deterministic desktop Chrome
// navigation fingerprint the upstream scraper sends, plus the same-origin
// referer overrides and the form content type. Accept-Encoding is left
// unset so the transport negotiates gzip and decompresses it itself.
func setNavigationHeaders(h http.Header) {
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
	h.Set("Referer", htmlReferer)
	h.Set("Content-Type", "application/x-www-form-urlencoded")
}

// creditBodyPattern mirrors the root search package's billing-signal
// detector; the root helper is unexported, so the scraper keeps a local
// replica with identical semantics.
var creditBodyPattern = regexp.MustCompile(
	`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

// classifyStatus maps a non-2xx status and body onto a classified
// provider error, mirroring search.classifyHTTPError. Body-level billing
// signals win over status codes. It returns nil when nothing matches,
// leaving the caller free to raise the generic error.
func classifyStatus(status int, body string) *search.SearchProviderError {
	if creditBodyPattern.MatchString(body) {
		return &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeBilling,
			Message:    "credits exhausted",
			HTTPStatus: status,
		}
	}
	switch {
	case status == 402:
		return &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeBilling,
			Message:    "402 credits exhausted",
			HTTPStatus: status,
		}
	case status == 401:
		return &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeUnauthorized,
			Message:    "401 unauthorized",
			HTTPStatus: status,
		}
	case status == 403:
		return &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeForbidden,
			Message:    "403 forbidden",
			HTTPStatus: status,
		}
	case status == 429:
		return &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeRateLimited,
			Message:    "429 rate limited",
			HTTPStatus: status,
		}
	case status >= 500:
		return &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeServerError,
			Message:    fmt.Sprintf("%d server error", status),
			HTTPStatus: status,
		}
	}
	return nil
}
