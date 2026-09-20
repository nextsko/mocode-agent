package engines

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/query"
)

// Startpage engine identity and endpoints.
const (
	// StartpageID is the stable engine identifier.
	StartpageID = "startpage"

	// startpageHomeURL is the homepage whose /sp/search form carries
	// the sc anti-bot token.
	startpageHomeURL = "https://www.startpage.com/"

	// startpageSearchURL is the search endpoint, reached by POST with
	// the form token or by GET as the tokenless fallback.
	startpageSearchURL = "https://www.startpage.com/sp/search"
)

// startpageCooldown is the process-wide backoff window shared by every
// Startpage engine instance.
var startpageCooldown = &cooldownState{}

// startpageChallengeMessage is the user-facing explanation, ported from
// the upstream scraper.
const startpageChallengeMessage = "Startpage blocked the request with " +
	"a CAPTCHA challenge. Startpage rate-limits automated searches " +
	"from datacenter/shared-egress IPs; try another provider such as " +
	"DuckDuckGo or Mojeek, or retry later."

// StartpageEngine searches Google's index through Startpage's privacy
// frontend. The robust flow mirrors a real browser: GET the homepage,
// lift the search form's hidden inputs (the sc session token), POST
// them back with the query; a missing or stale token 302s to the
// CAPTCHA shell, so homepage-fetch failures degrade to a tokenless
// GET. Startpage serves fully server-rendered pages — no JS challenge
// on the happy path.
type StartpageEngine struct {
	homeURL   string
	searchURL string
	cooldown  *cooldownState
}

// NewStartpageEngine creates an engine sharing the process-wide
// cooldown window with every other Startpage engine, so concurrent
// instances cannot hammer past a challenge.
func NewStartpageEngine() *StartpageEngine {
	return &StartpageEngine{
		homeURL:   startpageHomeURL,
		searchURL: startpageSearchURL,
		cooldown:  startpageCooldown,
	}
}

// ID implements SubEngine.
func (e *StartpageEngine) ID() string { return StartpageID }

// Fetch implements SubEngine: it runs the homepage-token form flow (or
// the tokenless GET fallback), detects the CAPTCHA shell, and parses
// div.result blocks in document order. The recency filter is not
// applied: the fan-out contract carries no recency field.
func (e *StartpageEngine) Fetch(ctx context.Context, client *http.Client, queryString string, limit int) ([]search.SearchSource, error) {
	if e.cooldown.active() {
		return nil, cooldownError(StartpageID, e.cooldown.remaining())
	}
	limit = clampEngineLimit(limit)
	// Startpage proxies Google, so the operator set works inline;
	// rebuilding via the shared scraper formatter canonicalizes aliases
	// (domain: -> site:, since: -> after:) and demotes scraper-hostile
	// operators. Directive-free queries pass through byte-identical.
	engineQuery := query.FormatScraperQuery(queryString, nil, query.GOOGLE_QUERY_SYNTAX)

	var page enginePage
	var err error
	if formInputs := e.fetchFormInputs(ctx, client); formInputs != nil {
		form := make(url.Values, len(formInputs)+1)
		for name, value := range formInputs {
			form.Set(name, value)
		}
		form.Set("query", engineQuery)
		page, err = e.postSearch(ctx, client, form)
	} else {
		page, err = e.getSearch(ctx, client, engineQuery)
	}
	if err != nil {
		return nil, err
	}

	if isStartpageChallenge(page) {
		e.cooldown.trigger(cooldownDuration)
		return nil, challengeError(StartpageID, startpageChallengeMessage)
	}
	if page.status < 200 || page.status >= 300 {
		if classified := classifyStatus(StartpageID, page.status, page.body); classified != nil {
			return nil, classified
		}
		return nil, httpStatusError(StartpageID, "Startpage", page.status)
	}
	return collectSources(parseStartpageResults(page.body, e.homeURL), limit), nil
}

// fetchFormInputs GETs the homepage and lifts the hidden inputs of the
// /sp/search form. Best effort: any failure (network, non-OK status,
// challenge shell, markup drift) yields nil and the caller falls back
// to a tokenless GET instead of posting a doomed form.
func (e *StartpageEngine) fetchFormInputs(ctx context.Context, client *http.Client) map[string]string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.homeURL, nil)
	if err != nil {
		return nil
	}
	setNavigationHeaders(req.Header, e.homeURL)
	page, err := fetchPage(ctx, client, StartpageID, req)
	if err != nil {
		return nil
	}
	if page.status < 200 || page.status >= 300 || isStartpageChallenge(page) {
		return nil
	}
	return parseStartpageFormInputs(page.body)
}

// parseStartpageFormInputs lifts the hidden inputs from the homepage's
// /sp/search form. It returns nil when the form or its sc anti-bot
// token cannot be found.
func parseStartpageFormInputs(html string) map[string]string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	form := doc.Find(`form[action="/sp/search"]`).First()
	if form.Length() == 0 {
		return nil
	}
	inputs := map[string]string{}
	form.Find(`input[type="hidden"]`).Each(func(_ int, input *goquery.Selection) {
		name, _ := input.Attr("name")
		if name == "" {
			return
		}
		value, _ := input.Attr("value")
		inputs[name] = value
	})
	if _, ok := inputs["sc"]; !ok {
		return nil
	}
	return inputs
}

// postSearch submits the token-bearing form to the search endpoint.
func (e *StartpageEngine) postSearch(ctx context.Context, client *http.Client, form url.Values) (enginePage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.searchURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return enginePage{}, &search.SearchProviderError{
			Provider: StartpageID,
			Message:  fmt.Sprintf("build request: %v", err),
		}
	}
	setNavigationHeaders(req.Header, e.homeURL)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return fetchPage(ctx, client, StartpageID, req)
}

// getSearch issues the tokenless GET fallback.
func (e *StartpageEngine) getSearch(ctx context.Context, client *http.Client, engineQuery string) (enginePage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.searchURL, nil)
	if err != nil {
		return enginePage{}, &search.SearchProviderError{
			Provider: StartpageID,
			Message:  fmt.Sprintf("build request: %v", err),
		}
	}
	setNavigationHeaders(req.Header, e.homeURL)
	values := req.URL.Query()
	values.Set("query", engineQuery)
	req.URL.RawQuery = values.Encode()
	return fetchPage(ctx, client, StartpageID, req)
}

// isStartpageChallenge reports whether Startpage answered with its
// CAPTCHA/error shell instead of results. Rejected requests 302 to
// /en/errors/ (legacy: /sp/captcha); body markers matter because the
// HTTP client follows redirects silently. A bare "captcha" substring
// is deliberately not used — result snippets for captcha-related
// queries would false-positive.
func isStartpageChallenge(page enginePage) bool {
	if strings.Contains(page.finalURL, "/errors/") ||
		strings.Contains(page.finalURL, "/captcha/") ||
		strings.Contains(page.finalURL, "/sp/captcha") {
		return true
	}
	return strings.Contains(page.body, "component---src-pages-captcha") ||
		strings.Contains(page.body, "/sp/captcha")
}

// parseStartpageResults walks the server-rendered results page in
// document order. Each organic hit lives in a div.result container
// holding the title anchor a.result-link (with an h2/h3 heading) and
// an optional p.description snippet. Hrefs are direct target URLs —
// Startpage does not wrap outbound clicks. The offscreen adblock
// honeypot (a-bg-result) and sponsored placements never match the
// container selector.
func parseStartpageResults(html, homeURL string) []parsedResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	base, err := url.Parse(homeURL)
	if err != nil {
		return nil
	}
	var results []parsedResult
	doc.Find("div.result").Each(func(_ int, block *goquery.Selection) {
		anchor := block.Find("a.result-link").First()
		if anchor.Length() == 0 {
			return
		}
		href, _ := anchor.Attr("href")
		target, ok := sanitizeStartpageURL(base, href)
		if !ok {
			return
		}
		heading := anchor.Find("h2, h3").First()
		titleText := anchor.Text()
		if heading.Length() > 0 {
			titleText = heading.Text()
		}
		title := normalizeText(titleText)
		if title == "" {
			return
		}
		row := parsedResult{title: title, url: target}
		if snippet := block.Find("p.description").First(); snippet.Length() > 0 {
			row.snippet = normalizeText(snippet.Text())
		}
		results = append(results, row)
	})
	return results
}

// sanitizeStartpageURL accepts only http(s) result targets resolved
// against the homepage that point away from Startpage itself.
func sanitizeStartpageURL(base *url.URL, href string) (string, bool) {
	if href == "" {
		return "", false
	}
	u, err := base.Parse(href)
	if err != nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "startpage.com" || strings.HasSuffix(host, ".startpage.com") {
		return "", false
	}
	return u.String(), true
}
