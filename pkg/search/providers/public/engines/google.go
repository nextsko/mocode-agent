package engines

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/query"
)

// Google engine identity and endpoints.
const (
	// GoogleID is the stable engine identifier.
	GoogleID = "google"

	// googleHomeURL is the referer root.
	googleHomeURL = "https://www.google.com/"

	// googleSearchURL is the search endpoint.
	googleSearchURL = "https://www.google.com/search"
)

// googleCooldown is the process-wide backoff window shared by every
// Google engine instance.
var googleCooldown = &cooldownState{}

// googleTrafficMessage is the user-facing explanation for the
// automated-traffic wall, ported from the upstream scraper.
const googleTrafficMessage = "Google blocked the search with an " +
	"automated-traffic challenge. Try another web search provider or " +
	"retry later."

// googleJSMessage is the user-facing explanation for the enable-JS
// shell, ported from the upstream scraper.
const googleJSMessage = "Google returned its JavaScript challenge " +
	"instead of rendered search results."

// googleTrafficPattern matches the automated-traffic markers Google
// embeds in block pages.
var googleTrafficPattern = regexp.MustCompile(
	`(?i)unusual traffic|detected unusual traffic|g-recaptcha`)

// readMorePattern strips the trailing "Read more" affordance Google
// appends to snippet blocks.
var readMorePattern = regexp.MustCompile(`(?i)\s*read more$`)

// googleSnippetSelectors are the snippet containers tried in order
// within each result block, ported from the upstream scraper.
var googleSnippetSelectors = []string{
	"[data-sncf='1'] .VwiC3b",
	".VwiC3b",
	".IsZvec",
	".BNeawe.s3v9rd",
	"[data-sncf='1']",
}

// googleBlockKind classifies a non-result Google response.
type googleBlockKind int

const (
	googleBlockNone googleBlockKind = iota
	googleBlockTraffic
	googleBlockJavaScript
)

// GoogleEngine searches Google's index over the plain HTML vertical
// (udm=14). The upstream scraper escalates to a headless browser on
// JavaScript or traffic walls; the Go port is HTTP-only and treats
// both walls as engine failures under a shared cooldown.
type GoogleEngine struct {
	homeURL   string
	searchURL string
	cooldown  *cooldownState
}

// NewGoogleEngine creates an engine sharing the process-wide cooldown
// window with every other Google engine.
func NewGoogleEngine() *GoogleEngine {
	return &GoogleEngine{
		homeURL:   googleHomeURL,
		searchURL: googleSearchURL,
		cooldown:  googleCooldown,
	}
}

// ID implements SubEngine.
func (e *GoogleEngine) ID() string { return GoogleID }

// Fetch implements SubEngine: it GETs the search URL with a desktop
// Chrome navigation fingerprint, classifies block walls, and parses
// every h3 result heading in document order.
func (e *GoogleEngine) Fetch(ctx context.Context, client *http.Client, queryString string, limit int) ([]search.SearchSource, error) {
	if e.cooldown.active() {
		return nil, cooldownError(GoogleID, e.cooldown.remaining())
	}
	limit = clampEngineLimit(limit)
	// Google parses the classic operator set inline; rebuilding via the
	// shared scraper formatter canonicalizes aliases and demotes
	// scraper-hostile operators. Directive-free queries pass through
	// byte-identical.
	engineQuery := query.FormatScraperQuery(queryString, nil, query.GOOGLE_QUERY_SYNTAX)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		buildGoogleSearchURL(e.searchURL, engineQuery, limit), nil)
	if err != nil {
		return nil, &search.SearchProviderError{
			Provider: GoogleID,
			Message:  fmt.Sprintf("build request: %v", err),
		}
	}
	setNavigationHeaders(req.Header, e.homeURL)
	page, err := fetchPage(ctx, client, GoogleID, req)
	if err != nil {
		return nil, err
	}

	switch googleBlockReason(page) {
	case googleBlockTraffic:
		e.cooldown.trigger(cooldownDuration)
		return nil, challengeError(GoogleID, googleTrafficMessage)
	case googleBlockJavaScript:
		e.cooldown.trigger(cooldownDuration)
		return nil, challengeError(GoogleID, googleJSMessage)
	}
	if page.status < 200 || page.status >= 300 {
		if classified := classifyStatus(GoogleID, page.status, page.body); classified != nil {
			return nil, classified
		}
		return nil, httpStatusError(GoogleID, "Google", page.status)
	}
	return collectSources(parseGoogleResults(page.body), limit), nil
}

// buildGoogleSearchURL assembles the search request: the rebuilt
// query, the result count, the English/US locale, the udm=14
// plain-HTML vertical, and personalization off.
func buildGoogleSearchURL(searchURL, engineQuery string, limit int) string {
	u, err := url.Parse(searchURL)
	if err != nil {
		return searchURL
	}
	values := u.Query()
	values.Set("q", engineQuery)
	values.Set("num", strconv.Itoa(limit))
	values.Set("hl", "en")
	values.Set("gl", "us")
	values.Set("udm", "14")
	values.Set("pws", "0")
	u.RawQuery = values.Encode()
	return u.String()
}

// googleBlockReason classifies non-result Google responses: the
// enable-JavaScript shell and the automated-traffic wall. The
// JavaScript wall additionally requires the absence of any h3 heading
// so result pages carrying the marker are not misclassified.
func googleBlockReason(page enginePage) googleBlockKind {
	hasHeading := strings.Contains(strings.ToLower(page.body), "<h3")
	if strings.Contains(page.body, "/httpservice/retry/enablejs") && !hasHeading {
		return googleBlockJavaScript
	}
	if page.status == 403 || page.status == 429 ||
		strings.Contains(page.finalURL, "/sorry/") ||
		googleTrafficPattern.MatchString(page.body) {
		return googleBlockTraffic
	}
	return googleBlockNone
}

// isGoogleHost reports whether u points at google.com or www.google.com
// exactly, matching the upstream host checks.
func isGoogleHost(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return host == "google.com" || host == "www.google.com"
}

// unwrapGoogleURL resolves a result href to its target. Google wraps
// outbound clicks in a /url redirect whose q (or url) parameter carries
// the target; the wrapper is decoded first. Non-http(s) schemes and
// google.com/www.google.com hosts are rejected.
func unwrapGoogleURL(base *url.URL, href string) (string, bool) {
	if href == "" {
		return "", false
	}
	u, err := base.Parse(href)
	if err != nil {
		return "", false
	}
	if isGoogleHost(u) && u.Path == "/url" {
		target := u.Query().Get("q")
		if target == "" {
			target = u.Query().Get("url")
		}
		if target == "" {
			return "", false
		}
		u, err = url.Parse(target)
		if err != nil {
			return "", false
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if isGoogleHost(u) {
		return "", false
	}
	return u.String(), true
}

// parseGoogleResults walks every h3 heading in document order: the
// closest anchor's href is unwrapped from Google's /url redirect
// wrapper, Google-internal targets are rejected, and the snippet is
// lifted from the enclosing result container. Hrefs resolve against
// the production google.com home even when the engine's endpoint is
// redirected, because the /url wrapper detection keys on the Google
// host.
func parseGoogleResults(html string) []parsedResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	base, err := url.Parse(googleHomeURL)
	if err != nil {
		return nil
	}
	var results []parsedResult
	doc.Find("h3").Each(func(_ int, heading *goquery.Selection) {
		anchor := heading.Closest("a")
		if anchor.Length() == 0 {
			return
		}
		href, _ := anchor.Attr("href")
		target, ok := unwrapGoogleURL(base, href)
		if !ok {
			return
		}
		title := normalizeText(heading.Text())
		if title == "" {
			return
		}
		results = append(results, parsedResult{
			title:   title,
			url:     target,
			snippet: findGoogleSnippet(heading),
		})
	})
	return results
}

// findGoogleSnippet lifts the result snippet near the heading. The
// enclosing block is the closest known result container, or the
// heading's grandparent when none matches; selectors are tried in
// order and a trailing "Read more" affordance is stripped.
func findGoogleSnippet(heading *goquery.Selection) string {
	container := heading.Closest(".tF2Cxc, .MjjYud, .Gx5Zad")
	if container.Length() == 0 {
		parent := heading.Parent()
		if parent.Length() > 0 {
			container = parent.Parent()
		}
	}
	if container.Length() == 0 {
		return ""
	}
	for _, selector := range googleSnippetSelectors {
		found := container.Find(selector).First()
		if found.Length() == 0 {
			continue
		}
		text := readMorePattern.ReplaceAllString(normalizeText(found.Text()), "")
		if text != "" {
			return text
		}
	}
	return ""
}
