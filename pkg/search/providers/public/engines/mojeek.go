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

// Mojeek engine identity and endpoints.
const (
	// MojeekID is the stable engine identifier.
	MojeekID = "mojeek"

	// mojeekHomeURL is the referer root with the engine's canonical
	// preference parameters.
	mojeekHomeURL = "https://www.mojeek.de/?arc=none&lang=en&lb=en&theme=dark"

	// mojeekSearchURL is the search endpoint.
	mojeekSearchURL = "https://www.mojeek.de/search"
)

// mojeekCooldown is the process-wide backoff window shared by every
// Mojeek engine instance.
var mojeekCooldown = &cooldownState{}

// mojeekBlockedMessage is the user-facing explanation for the ALTCHA
// wall, adapted from the upstream scraper.
const mojeekBlockedMessage = "Mojeek blocked the request with its " +
	"automated-queries wall (ALTCHA). Mojeek rate-limits scripted " +
	"searches from datacenter/shared-egress IPs; retry later or " +
	"configure another provider such as DuckDuckGo, Brave, Tavily, " +
	"Exa, or Kagi."

// mojeekRobotPattern matches the automated-queries refusal text.
var mojeekRobotPattern = regexp.MustCompile(`(?i)sending automated queries`)

// mojeekQuerySyntax re-emits only the directives Mojeek's support docs
// confirm: quoted phrases, - exclusions, and site:. Date bounds and
// in* operators are conservatively dropped, matching the upstream
// engine.
var mojeekQuerySyntax = query.QuerySyntax{
	Phrases:  true,
	Negation: true,
	Site:     true,
}

// MojeekEngine searches Mojeek's independent index over the standard
// HTML results page. The upstream scraper solves Mojeek's ALTCHA
// proof-of-work interstitial inside a headless browser; the HTTP-only
// Go port registers the engine and treats an ALTCHA page as a bot
// challenge instead of solving it, so the wall costs one engine of
// coverage rather than blocking the fan-out.
type MojeekEngine struct {
	homeURL   string
	searchURL string
	cooldown  *cooldownState
}

// NewMojeekEngine creates an engine sharing the process-wide cooldown
// window with every other Mojeek engine.
func NewMojeekEngine() *MojeekEngine {
	return &MojeekEngine{
		homeURL:   mojeekHomeURL,
		searchURL: mojeekSearchURL,
		cooldown:  mojeekCooldown,
	}
}

// ID implements SubEngine.
func (e *MojeekEngine) ID() string { return MojeekID }

// Fetch implements SubEngine: it GETs the search URL with the engine's
// stable preference parameters, classifies the ALTCHA robot wall, and
// parses the results-standard list in document order.
func (e *MojeekEngine) Fetch(ctx context.Context, client *http.Client, queryString string, limit int) ([]search.SearchSource, error) {
	if e.cooldown.active() {
		return nil, cooldownError(MojeekID, e.cooldown.remaining())
	}
	limit = clampEngineLimit(limit)
	engineQuery := query.FormatScraperQuery(queryString, nil, mojeekQuerySyntax)

	searchURL, err := url.Parse(e.searchURL)
	if err != nil {
		return nil, &search.SearchProviderError{
			Provider: MojeekID,
			Message:  fmt.Sprintf("parse search URL: %v", err),
		}
	}
	values := searchURL.Query()
	values.Set("q", engineQuery)
	values.Set("t", strconv.Itoa(limit))
	values.Set("arc", "none")
	values.Set("lang", "en")
	values.Set("lb", "en")
	values.Set("theme", "dark")
	searchURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
	if err != nil {
		return nil, &search.SearchProviderError{
			Provider: MojeekID,
			Message:  fmt.Sprintf("build request: %v", err),
		}
	}
	setNavigationHeaders(req.Header, e.homeURL)
	page, err := fetchPage(ctx, client, MojeekID, req)
	if err != nil {
		return nil, err
	}

	// Robot walls: the ALTCHA interstitial arrives as HTTP 200 and the
	// "automated queries" refusal as 403; both bodies are more
	// actionable than their raw statuses, so they are checked before
	// the generic status handling.
	if isMojeekRobotPage(page) {
		e.cooldown.trigger(cooldownDuration)
		return nil, challengeError(MojeekID, mojeekBlockedMessage)
	}
	if page.status < 200 || page.status >= 300 {
		if classified := classifyStatus(MojeekID, page.status, page.body); classified != nil {
			return nil, classified
		}
		return nil, httpStatusError(MojeekID, "Mojeek", page.status)
	}
	return collectSources(parseMojeekResults(page.body, e.homeURL), limit), nil
}

// isMojeekRobotPage reports whether the response is Mojeek's ALTCHA
// proof-of-work interstitial or automated-queries refusal rather than
// results: a challenge marker without the results list.
func isMojeekRobotPage(page enginePage) bool {
	challenge := strings.Contains(page.body, "altcha-widget") ||
		strings.Contains(page.body, "captcha-wrap") ||
		mojeekRobotPattern.MatchString(page.body)
	return challenge && !strings.Contains(page.body, "results-standard")
}

// parseMojeekResults pulls result blocks out of a Mojeek results page
// in document order. Each organic result renders as
// ul.results-standard > li with the title in h2 > a.title (the href is
// the direct target URL) and the preview in p.s. Clustered sub-results
// share the same shape; rows without a title anchor (infoboxes,
// spelling suggestions) are skipped.
func parseMojeekResults(html, homeURL string) []parsedResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	base, err := url.Parse(homeURL)
	if err != nil {
		return nil
	}
	var results []parsedResult
	doc.Find("ul.results-standard > li").Each(func(_ int, item *goquery.Selection) {
		anchor := item.Find("h2 a.title").First()
		if anchor.Length() == 0 {
			anchor = item.Find("a.title").First()
		}
		if anchor.Length() == 0 {
			return
		}
		href, _ := anchor.Attr("href")
		target, ok := normalizeMojeekURL(base, href)
		if !ok {
			return
		}
		title := normalizeText(anchor.Text())
		if title == "" {
			return
		}
		row := parsedResult{title: title, url: target}
		if snippet := item.Find("p.s").First(); snippet.Length() > 0 {
			row.snippet = normalizeText(snippet.Text())
		}
		results = append(results, row)
	})
	return results
}

// normalizeMojeekURL validates a result href. Mojeek links results
// directly to the target site, so this only filters non-HTTP schemes
// and intra-Mojeek navigation rows across its country domains.
func normalizeMojeekURL(base *url.URL, href string) (string, bool) {
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
	for _, domain := range []string{"mojeek.com", "mojeek.co.uk", "mojeek.fr", "mojeek.de"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return "", false
		}
	}
	return u.String(), true
}
