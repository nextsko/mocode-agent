package duckduckgo

import (
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// parsedResult is one decoded DuckDuckGo result row before mapping onto
// search.SearchSource.
type parsedResult struct {
	title         string
	url           string
	snippet       string
	publishedDate string
}

// uddgPattern captures the encoded target URL inside a DuckDuckGo
// redirect wrapper (//duckduckgo.com/l/?uddg=<encoded>&rut=...).
var uddgPattern = regexp.MustCompile(`[?&]uddg=([^&]+)`)

// dateShapePattern matches the ISO-ish timestamps DuckDuckGo renders in
// a result row's URL extras line: a bare date, or a date plus HH:MM.
var dateShapePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}|$)`)

// ddgTimeLayouts parse the timestamp shapes DuckDuckGo emits, most
// specific first. Values without a zone parse as UTC, so derived ages
// stay deterministic across host locales (the upstream TS parses them as
// local time; the deviation is deliberate).
var ddgTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05.9999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseResults walks a DuckDuckGo HTML results page and returns decoded
// rows in document order. A row survives only when it has a result__a
// title link whose href resolves to a real target URL and whose text is
// non-empty; sponsored rows with absolute links and snippet-less rows
// are tolerated. The upstream TS scrapes the same structure with regex
// blocks; goquery expresses the same contract structurally.
func parseResults(html string) []parsedResult {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var results []parsedResult
	doc.Find("div.result").Each(func(_ int, block *goquery.Selection) {
		titleLink := block.Find("a.result__a").First()
		if titleLink.Length() == 0 {
			return
		}
		href, _ := titleLink.Attr("href")
		target, ok := unwrapResultURL(href)
		if !ok {
			return
		}
		title := normalizeText(titleLink.Text())
		if title == "" {
			return
		}
		row := parsedResult{title: title, url: target}
		if snippet := block.Find(".result__snippet").First(); snippet.Length() > 0 {
			row.snippet = normalizeText(snippet.Text())
		}
		row.publishedDate = extractPublishedDate(block)
		results = append(results, row)
	})
	return results
}

// unwrapResultURL resolves a DuckDuckGo result href back to its target.
// DDG routes outbound clicks through //duckduckgo.com/l/?uddg=<encoded>;
// that wrapper is decoded first (a failed decode drops the row).
// Protocol-relative links gain the https scheme, absolute http(s) links
// pass through, and anything else is rejected.
func unwrapResultURL(href string) (string, bool) {
	if href == "" {
		return "", false
	}
	if match := uddgPattern.FindStringSubmatch(href); match != nil {
		decoded, err := url.QueryUnescape(match[1])
		if err != nil {
			return "", false
		}
		return decoded, true
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href, true
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href, true
	}
	return "", false
}

// normalizeText collapses element text into one trimmed line. The DOM
// walk already drops tags and the parser already decoded entities, so
// only whitespace folding remains of the upstream decodeHtmlText.
func normalizeText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// extractPublishedDate lifts a result row's publication timestamp from
// its result__extras__url container. Scanning is restricted to that
// container so a date-shaped value inside a snippet is not attributed as
// publication metadata.
func extractPublishedDate(block *goquery.Selection) string {
	extras := block.Find(".result__extras__url").First()
	if extras.Length() == 0 {
		return ""
	}
	var found string
	extras.Find("span").EachWithBreak(func(_ int, span *goquery.Selection) bool {
		if text := normalizeText(span.Text()); dateShapePattern.MatchString(text) {
			found = text
			return false
		}
		return true
	})
	return found
}

// toSource maps a parsed row onto the unified SearchSource DTO, deriving
// AgeSeconds from the published date against now.
func (r parsedResult) toSource(now time.Time) search.SearchSource {
	return search.SearchSource{
		Title:         r.title,
		URL:           r.url,
		Snippet:       r.snippet,
		PublishedDate: r.publishedDate,
		AgeSeconds:    ageSeconds(r.publishedDate, now),
	}
}

// ageSeconds converts a DuckDuckGo published timestamp into whole
// seconds before now (negative for future dates), or 0 when the
// timestamp is absent or unparsable.
func ageSeconds(publishedDate string, now time.Time) int64 {
	parsed, ok := parseTimestamp(publishedDate)
	if !ok {
		return 0
	}
	return int64(math.Floor(now.Sub(parsed).Seconds()))
}

// parseTimestamp tries each known DuckDuckGo layout, most specific
// first, reporting success on the first match.
func parseTimestamp(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range ddgTimeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
