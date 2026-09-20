package query

import (
	"strings"
	"unicode"
)

// quoteValue quotes a directive value when it contains whitespace.
func quoteValue(value string) string {
	for _, r := range value {
		if unicode.IsSpace(r) {
			return `"` + value + `"`
		}
	}
	return value
}

// renderTerms renders the free-text terms per the target syntax.
func renderTerms(terms []QueryTerm, syntax QuerySyntax) string {
	var parts []string
	for i := 0; i < len(terms); i++ {
		term := terms[i]
		if term.GroupID != 0 && syntax.Or {
			var members []string
			j := i
			for ; j < len(terms) && terms[j].GroupID == term.GroupID; j++ {
				if rendered := renderTerm(terms[j], syntax); rendered != "" {
					members = append(members, rendered)
				}
			}
			i = j - 1
			switch len(members) {
			case 0:
			case 1:
				parts = append(parts, members[0])
			default:
				parts = append(parts, "("+strings.Join(members, " OR ")+")")
			}
			continue
		}
		if rendered := renderTerm(term, syntax); rendered != "" {
			parts = append(parts, rendered)
		}
	}
	return strings.Join(parts, " ")
}

// renderTerm renders a single term, or "" when the target syntax drops
// it.
func renderTerm(term QueryTerm, syntax QuerySyntax) string {
	if term.Negated && !syntax.Negation {
		return ""
	}
	body := term.Text
	if term.Phrase && syntax.Phrases {
		body = `"` + body + `"`
	}
	if term.Negated {
		return "-" + body
	}
	return body
}

// FormatQuery rebuilds a query string for an engine with the given
// QuerySyntax capabilities.
//
// Constraints whose syntax the engine lacks are omitted; the caller maps
// them onto API parameters or relies on ApplyQueryConstraints. Never
// returns an empty string for a non-empty input: a directives-only query
// falls back to the constraint values as keywords, then to the raw query
// — an engine searching something beats an empty-query error.
func FormatQuery(q StructuredQuery, syntax QuerySyntax) string {
	var parts []string
	if text := renderTerms(q.Terms, syntax); text != "" {
		parts = append(parts, text)
	}

	if syntax.Site {
		if len(q.Sites) > 1 && syntax.Or {
			grouped := make([]string, len(q.Sites))
			for i, s := range q.Sites {
				grouped[i] = "site:" + s
			}
			parts = append(parts, "("+strings.Join(grouped, " OR ")+")")
		} else {
			for _, s := range q.Sites {
				parts = append(parts, "site:"+s)
			}
		}
		for _, s := range q.ExcludedSites {
			parts = append(parts, "-site:"+s)
		}
	}
	if syntax.InURL {
		for _, v := range q.InURL {
			parts = append(parts, "inurl:"+quoteValue(v))
		}
		for _, v := range q.ExcludedInURL {
			parts = append(parts, "-inurl:"+quoteValue(v))
		}
	}
	if syntax.InTitle {
		for _, v := range q.InTitle {
			parts = append(parts, "intitle:"+quoteValue(v))
		}
		for _, v := range q.ExcludedInTitle {
			parts = append(parts, "-intitle:"+quoteValue(v))
		}
	}
	if syntax.InText {
		for _, v := range q.InText {
			parts = append(parts, "intext:"+quoteValue(v))
		}
		for _, v := range q.ExcludedInText {
			parts = append(parts, "-intext:"+quoteValue(v))
		}
	}
	if syntax.Filetype {
		if len(q.Filetypes) > 1 && syntax.Or {
			grouped := make([]string, len(q.Filetypes))
			for i, f := range q.Filetypes {
				grouped[i] = "filetype:" + f
			}
			parts = append(parts, "("+strings.Join(grouped, " OR ")+")")
		} else {
			for _, f := range q.Filetypes {
				parts = append(parts, "filetype:"+f)
			}
		}
		for _, f := range q.ExcludedFiletypes {
			parts = append(parts, "-filetype:"+f)
		}
	}
	if syntax.DateRange {
		if q.After != nil {
			parts = append(parts, "after:"+*q.After)
		}
		if q.Before != nil {
			parts = append(parts, "before:"+*q.Before)
		}
	}

	result := strings.TrimSpace(strings.Join(parts, " "))
	if result == "" {
		// Directives-only query and no directive syntax: search the
		// constraint values as plain keywords so the engine still gets a
		// meaningful query.
		fallback := make([]string, 0,
			len(q.Sites)+len(q.InTitle)+len(q.InURL)+len(q.InText)+len(q.Filetypes))
		fallback = append(fallback, q.Sites...)
		fallback = append(fallback, q.InTitle...)
		fallback = append(fallback, q.InURL...)
		fallback = append(fallback, q.InText...)
		fallback = append(fallback, q.Filetypes...)
		result = strings.TrimSpace(strings.Join(fallback, " "))
	}
	if result == "" {
		return strings.TrimSpace(q.Raw)
	}
	return result
}

// FormatScraperQuery builds the engine query for a credential-free HTML
// engine (Google, Startpage, DuckDuckGo, Ecosia, Mojeek, SearXNG, and
// the public-web fan-out over them).
//
// It canonicalizes directives via FormatQuery with the engine syntax
// (pass nil as parsedQuery to parse the query on the fly), after demoting
// the operators that zero-match across the scraper set: engines only
// match site: against a bare domain (a path yields zero results
// everywhere), and DuckDuckGo ignores inurl: entirely. Bare-domain site:
// filters are kept while path-carrying site: values and all inurl:
// values become plain keywords; the demotion is structural (before
// formatting), so OR-grouped and quoted directives are covered. Negated
// forms (-site:, -inurl:) pass through untouched — demoting them would
// invert an exclusion into a search term; ApplyQueryConstraints enforces
// every demoted or unsupported constraint on the returned sources.
// Directive-free queries pass through byte-identical.
func FormatScraperQuery(query string, parsedQuery *StructuredQuery, syntax QuerySyntax) string {
	parsed := ParseSearchQuery(query)
	if parsedQuery != nil {
		parsed = *parsedQuery
	}
	if !parsed.HasDirectives {
		return query
	}
	var demoted []string
	var bareSites []string
	for _, site := range parsed.Sites {
		if strings.Contains(site, "/") {
			demoted = append(demoted, site)
		} else {
			bareSites = append(bareSites, site)
		}
	}
	demoted = append(demoted, parsed.InURL...)
	terms := make([]QueryTerm, 0, len(parsed.Terms)+len(demoted))
	terms = append(terms, parsed.Terms...)
	for _, text := range demoted {
		terms = append(terms, QueryTerm{Text: text})
	}
	downgraded := parsed
	downgraded.Sites = bareSites
	downgraded.InURL = nil
	downgraded.Terms = terms
	return FormatQuery(downgraded, syntax)
}
