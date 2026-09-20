package duckduckgo

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// vqdPattern extracts the per-query vqd token DuckDuckGo embeds in its
// pages. The token appears bare (vqd=abc123), double-quoted
// (vqd="abc123"), or HTML-entity quoted (vqd=&quot;abc123&quot;); the
// alternatives are ordered so the quoted shapes win over the bare run.
var vqdPattern = regexp.MustCompile(
	`vqd=(?:"([^"]+)"|&quot;([^&]+)&quot;|([A-Za-z0-9_-]+))`)

// ExtractVqd returns the first vqd token embedded in a DuckDuckGo HTML
// page, or "" when the page carries none. The token authorizes follow-up
// paginated requests for the same query.
func ExtractVqd(html string) string {
	match := vqdPattern.FindStringSubmatch(html)
	if match == nil {
		return ""
	}
	for _, group := range match[1:] {
		if group != "" {
			return group
		}
	}
	return ""
}

// parseContinuationForm lifts the hidden next-page form fields (s, dc,
// vqd, kl, ...) from a results page, mirroring the upstream
// parseContinuationForm: the first form carrying both s and vqd wins,
// and inputs without a name or value attribute are ignored. It reports
// false when no such form exists.
func parseContinuationForm(html string) (url.Values, bool) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, false
	}
	var continuation url.Values
	doc.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		values := url.Values{}
		form.Find("input").Each(func(_ int, input *goquery.Selection) {
			name, hasName := input.Attr("name")
			if !hasName || name == "" {
				return
			}
			value, hasValue := input.Attr("value")
			if !hasValue {
				return
			}
			values.Add(name, value)
		})
		if values.Has("s") && values.Has("vqd") {
			continuation = values
			return false
		}
		return true
	})
	if continuation == nil {
		return nil, false
	}
	return continuation, true
}

// nextPageForm merges a continuation form over the current query form so
// a follow-up POST still carries q, df, and b while adopting the s/dc/vqd
// pagination fields DuckDuckGo emitted. The upstream TS posts the
// continuation form verbatim, but its test fixture omits q; merging keeps
// the follow-up query-anchored.
func nextPageForm(current, continuation url.Values) url.Values {
	merged := url.Values{}
	for key, values := range current {
		merged[key] = slices.Clone(values)
	}
	for key, values := range continuation {
		merged[key] = slices.Clone(values)
	}
	return merged
}
