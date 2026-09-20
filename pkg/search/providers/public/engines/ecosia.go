package engines

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/query"
)

// Ecosia engine identity and endpoints.
const (
	// EcosiaID is the stable engine identifier.
	EcosiaID = "ecosia"

	// ecosiaHomeURL is the referer root.
	ecosiaHomeURL = "https://www.ecosia.org/"

	// ecosiaSearchURL is the search endpoint.
	ecosiaSearchURL = "https://www.ecosia.org/search"
)

// ecosiaCooldown is the process-wide backoff window shared by every
// Ecosia engine instance.
var ecosiaCooldown = &cooldownState{}

// ecosiaBlockedMessage is the user-facing explanation for the
// Cloudflare wall, ported from the upstream scraper.
const ecosiaBlockedMessage = "Ecosia blocked the request with a " +
	"Cloudflare bot challenge. Ecosia's firewall throttles automated " +
	"searches from datacenter/shared-egress IPs; try another web " +
	"search provider such as DuckDuckGo, Brave, or Tavily."

// ecosiaRobotPattern matches the "confirm you're not a robot" body
// marker; the contraction may carry a typographic apostrophe.
var ecosiaRobotPattern = regexp.MustCompile(`(?i)confirm you.{0,3}re not a robot`)

// EcosiaEngine searches Ecosia's server-rendered results page. Ecosia
// fronts search with Cloudflare; the upstream scraper escalates to a
// stealth browser when blocked, while the HTTP-only Go port treats the
// managed challenge as an engine failure. Recency is ignored: Ecosia's
// web results expose no date filter, so the field is never
// approximated.
type EcosiaEngine struct {
	homeURL   string
	searchURL string
	cooldown  *cooldownState
}

// NewEcosiaEngine creates an engine sharing the process-wide cooldown
// window with every other Ecosia engine.
func NewEcosiaEngine() *EcosiaEngine {
	return &EcosiaEngine{
		homeURL:   ecosiaHomeURL,
		searchURL: ecosiaSearchURL,
		cooldown:  ecosiaCooldown,
	}
}

// ID implements SubEngine.
func (e *EcosiaEngine) ID() string { return EcosiaID }

// Fetch implements SubEngine: it GETs the search URL, classifies the
// Cloudflare challenge page, and parses the organic-result articles in
// document order.
func (e *EcosiaEngine) Fetch(ctx context.Context, client *http.Client, queryString string, limit int) ([]search.SearchSource, error) {
	if e.cooldown.active() {
		return nil, cooldownError(EcosiaID, e.cooldown.remaining())
	}
	limit = clampEngineLimit(limit)
	// Ecosia serves Google-backed results, so classic operators pass
	// through inline; rebuilding via the shared scraper formatter
	// canonicalizes aliases and demotes scraper-hostile operators.
	engineQuery := query.FormatScraperQuery(queryString, nil, query.GOOGLE_QUERY_SYNTAX)

	searchURL, err := url.Parse(e.searchURL)
	if err != nil {
		return nil, &search.SearchProviderError{
			Provider: EcosiaID,
			Message:  fmt.Sprintf("parse search URL: %v", err),
		}
	}
	values := searchURL.Query()
	values.Set("q", engineQuery)
	searchURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return nil, &search.SearchProviderError{
			Provider: EcosiaID,
			Message:  fmt.Sprintf("build request: %v", err),
		}
	}
	setNavigationHeaders(req.Header, e.homeURL)
	page, err := fetchPage(ctx, client, EcosiaID, req)
	if err != nil {
		return nil, err
	}

	if isEcosiaBlocked(page) {
		e.cooldown.trigger(cooldownDuration)
		return nil, challengeError(EcosiaID, ecosiaBlockedMessage)
	}
	if page.status < 200 || page.status >= 300 {
		if classified := classifyStatus(EcosiaID, page.status, page.body); classified != nil {
			return nil, classified
		}
		return nil, httpStatusError(EcosiaID, "Ecosia", page.status)
	}
	return collectSources(parseEcosiaResults(page.body, e.homeURL), limit), nil
}

// isEcosiaBlocked reports whether Ecosia's Cloudflare front answered
// with the managed challenge instead of results. The observed page is
// a 403 titled "Ecosia Firewall" carrying the _cf_chl_opt bootstrap
// and the challenge-platform loader.
func isEcosiaBlocked(page enginePage) bool {
	return page.status == 403 || page.status == 429 ||
		strings.Contains(page.body, "Ecosia Firewall") ||
		strings.Contains(page.body, "_cf_chl_opt") ||
		strings.Contains(page.body, "/cdn-cgi/challenge-platform/") ||
		ecosiaRobotPattern.MatchString(page.body)
}

// parseEcosiaResults pulls organic results out of the page in document
// order. Each result is an article[data-test-id="organic-result"] with
// the title inside [data-test-id="result-title"] wrapped by the target
// link, and the preview inside [data-test-id="web-result-description"]
// (falling back to its result-description container, which also holds
// screen-reader-only thumbnail captions on video rows). Ad slots and
// entity cards use different test-ids and never match.
func parseEcosiaResults(html, homeURL string) []parsedResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	base, err := url.Parse(homeURL)
	if err != nil {
		return nil
	}
	var results []parsedResult
	doc.Find(`article[data-test-id="organic-result"]`).Each(func(_ int, article *goquery.Selection) {
		heading := article.Find(`[data-test-id="result-title"]`).First()
		if heading.Length() == 0 {
			return
		}
		anchor := heading.Closest("a")
		if anchor.Length() == 0 {
			return
		}
		href, _ := anchor.Attr("href")
		target, ok := resolveEcosiaURL(base, href)
		if !ok {
			return
		}
		title := normalizeText(heading.Text())
		if title == "" {
			return
		}
		row := parsedResult{title: title, url: target}
		description := article.Find(`[data-test-id="web-result-description"]`).First()
		if description.Length() == 0 {
			description = article.Find(`[data-test-id="result-description"]`).First()
		}
		if description.Length() > 0 {
			row.snippet = normalizeText(description.Text())
		}
		results = append(results, row)
	})
	return results
}

// resolveEcosiaURL validates a result anchor href. Organic anchors
// carry the target URL directly; anything non-http(s) or pointing back
// at ecosia.org (internal navigation such as the images/news
// verticals) is rejected.
func resolveEcosiaURL(base *url.URL, href string) (string, bool) {
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
	if host == "ecosia.org" || host == "www.ecosia.org" {
		return "", false
	}
	return u.String(), true
}
