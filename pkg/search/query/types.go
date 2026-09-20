// Package query parses Google-style search directives into a structured
// form, rebuilds engine-specific query strings, and leniently filters
// search results against the parsed constraints.
//
// Agents habitually embed directives (site:, before:, inurl:, filetype:,
// quoted phrases, OR groups, -exclusions) regardless of whether the
// backing engine parses them. ParseSearchQuery decomposes such a query so
// each provider can map constraints onto native API parameters,
// FormatQuery rebuilds a query string containing only the syntax the
// target engine understands, and ApplyQueryConstraints post-filters
// returned sources leniently: a constraint dimension that would eliminate
// every result is dropped and reported rather than returning nothing.
package query

// QueryTerm is one free-text token of a query: everything that is not a
// recognized directive.
type QueryTerm struct {
	// Text holds the term without quotes or operator prefixes.
	Text string
	// Phrase marks a quoted exact phrase ("like this") or a verbatim
	// legacy +term.
	Phrase bool
	// Negated marks an exclusion via -term or NOT term.
	Negated bool
	// GroupID is the OR-group id. Terms sharing an id are alternatives
	// (a OR b); terms without a group (id 0) are implicitly AND-ed.
	// Groups are contiguous runs in StructuredQuery.Terms. Ids come from
	// a process-wide counter, so they are unique across parsed queries.
	GroupID int
}

// StructuredQuery is a raw query decomposed into free text plus every
// recognized constraint. All list fields are always present (possibly
// empty) so consumers can range over them without nil checks. Values are
// stored as typed by the user except for normalization noted per field.
type StructuredQuery struct {
	// Raw is the original query string, verbatim.
	Raw string
	// Text is the free-text remainder with all recognized directives
	// removed; phrases stay quoted, exclusions keep -, OR groups keep OR.
	// Empty when the query was directives only — use FormatQuery for a
	// never-empty engine query.
	Text string
	// Terms holds the ordered free-text terms (phrases, exclusions, OR
	// groups).
	Terms []QueryTerm
	// Sites holds site:/domain:/host: includes (any-of), lowercased with
	// the scheme stripped; may carry a path (github.com/anthropics).
	Sites []string
	// ExcludedSites holds -site: exclusions, same normalization as
	// Sites.
	ExcludedSites []string
	// InURL holds inurl:/url:/allinurl: substrings; all must appear in
	// the URL.
	InURL []string
	// ExcludedInURL holds -inurl: substrings; none may appear in the
	// URL.
	ExcludedInURL []string
	// InTitle holds intitle:/title:/allintitle: substrings; all must
	// appear in the title.
	InTitle []string
	// ExcludedInTitle holds -intitle: substrings; none may appear in the
	// title.
	ExcludedInTitle []string
	// InText holds intext:/inbody:/inanchor:/allintext: body substrings.
	// Not post-filterable (snippets are partial); query-building only.
	InText []string
	// ExcludedInText holds -intext: body exclusions. Query-building
	// only.
	ExcludedInText []string
	// Filetypes holds filetype:/ext: extensions (any-of), lowercased,
	// without the leading dot.
	Filetypes []string
	// ExcludedFiletypes holds -filetype: extensions; none may match.
	ExcludedFiletypes []string
	// After is the inclusive lower publish-date bound from after:/since:,
	// ISO YYYY-MM-DD.
	After *string
	// Before is the exclusive upper publish-date bound from
	// before:/until:, ISO YYYY-MM-DD.
	Before *string
	// Lang is the language code from lang:/language:, lowercased (en,
	// en-us).
	Lang *string
	// HasDirectives is true when any directive or boolean operator was
	// recognized.
	HasDirectives bool
	// HasConstraints is true when any post-filterable constraint is set
	// (sites, url/title terms, filetypes, date bounds).
	HasConstraints bool
}

// QuerySyntax declares the query-syntax capabilities of a target engine,
// used by FormatQuery to decide which parsed features are re-emitted as
// query text. The zero value produces plain keywords suitable for
// natural-language APIs.
type QuerySyntax struct {
	// Phrases emits "quoted phrases".
	Phrases bool
	// Negation emits -term exclusions (negated terms are dropped
	// otherwise).
	Negation bool
	// Or emits OR between alternatives (groups are flattened to keywords
	// otherwise).
	Or bool
	// Site emits site:/-site:.
	Site bool
	// InURL emits inurl:/-inurl:.
	InURL bool
	// InTitle emits intitle:/-intitle:.
	InTitle bool
	// InText emits intext:/-intext:.
	InText bool
	// Filetype emits filetype:/-filetype:.
	Filetype bool
	// DateRange emits before:/after: ISO date bounds.
	DateRange bool
}

// GOOGLE_QUERY_SYNTAX is the full Google-style syntax for engines that
// parse the classic operator set (Google, Startpage, Ecosia, Brave, Kagi,
// Mojeek, SearXNG, ...).
var GOOGLE_QUERY_SYNTAX = QuerySyntax{
	Phrases:   true,
	Negation:  true,
	Or:        true,
	Site:      true,
	InURL:     true,
	InTitle:   true,
	InText:    true,
	Filetype:  true,
	DateRange: true,
}

// LLM_QUERY_SYNTAX is the minimal syntax for LLM-mediated engines that
// only understand plain keywords plus quoted phrases.
var LLM_QUERY_SYNTAX = QuerySyntax{
	Phrases: true,
}

// FilterableSource is the minimal source shape the constraint filter
// operates on; providers bridge their native result type onto it.
type FilterableSource struct {
	Title         string
	URL           string
	Snippet       string
	PublishedDate string
	AgeSeconds    int64
}

// ConstraintFilterResult is the outcome of ApplyQueryConstraints.
type ConstraintFilterResult struct {
	// Sources holds the sources surviving the lenient filter — never
	// empty when the input was non-empty.
	Sources []FilterableSource
	// Dropped holds directive renderings (site:arxiv.org,
	// before:2024-01-01, ...) of the constraint dimensions that matched
	// zero sources and were therefore relaxed instead of enforced.
	Dropped []string
}
