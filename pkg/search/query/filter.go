package query

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// publishedDateLayouts are the date formats sourceTime tries when the
// publish date is not a relative age.
var publishedDateLayouts = []string{
	"2006-01-02",
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"January 2, 2006",
	"Jan 2, 2006",
	time.RFC1123,
	time.RFC1123Z,
}

// hostAndPath returns the lowercased hostname and pathname of a URL, or
// ok=false when the URL is unparsable or has no host.
func hostAndPath(rawURL string) (host, path string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	return strings.ToLower(u.Hostname()), u.Path, true
}

// MatchesSite is the site: matcher: exact host or subdomain of site;
// when site carries a path (github.com/anthropics), the URL path must
// start with it.
func MatchesSite(rawURL, site string) bool {
	host, path, ok := hostAndPath(rawURL)
	if !ok {
		return false
	}
	siteHost, sitePath := site, ""
	if slash := strings.IndexByte(site, '/'); slash >= 0 {
		siteHost, sitePath = site[:slash], site[slash:]
	}
	if host != siteHost && !strings.HasSuffix(host, "."+siteHost) {
		return false
	}
	if sitePath != "" && !strings.HasPrefix(strings.ToLower(path), strings.ToLower(sitePath)) {
		return false
	}
	return true
}

// matchesFiletype is the filetype: matcher: the URL pathname ends with
// .ext.
func matchesFiletype(rawURL, ext string) bool {
	_, path, ok := hostAndPath(rawURL)
	if !ok {
		return false
	}
	return strings.HasSuffix(strings.ToLower(path), "."+ext)
}

// sourceTime returns the best-effort publish time of a source from its
// ageSeconds (0 = unset), a relative age ("2 days ago"), or an ISO-ish
// date string. Undated sources return ok=false.
func sourceTime(source FilterableSource) (time.Time, bool) {
	if source.AgeSeconds != 0 {
		age := time.Duration(source.AgeSeconds) * time.Second
		return time.Now().UTC().Add(-age), true
	}
	published := strings.TrimSpace(source.PublishedDate)
	if published == "" {
		return time.Time{}, false
	}
	if rel := relativeAgePattern.FindStringSubmatch(published); rel != nil {
		count, _ := strconv.Atoi(rel[1])
		seconds := int64(count) * relativeUnitSeconds[strings.ToLower(rel[2])]
		if seconds > 0 {
			return time.Now().UTC().Add(-time.Duration(seconds) * time.Second), true
		}
		return time.Time{}, false
	}
	for _, layout := range publishedDateLayouts {
		if t, err := time.Parse(layout, published); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// constraintDimension is one post-filterable constraint of a query.
type constraintDimension struct {
	// label is the directive rendering for relaxation notes
	// (site:arxiv.org).
	label string
	// pred reports whether a source satisfies the dimension.
	pred func(source FilterableSource) bool
}

// joinPrefixed renders each value with a prefix (inurl:) and joins the
// results with sep.
func joinPrefixed(prefix string, values []string, sep string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = prefix + v
	}
	return strings.Join(out, sep)
}

// constraintDimensions lists the post-filterable dimensions of q in a
// stable order.
func constraintDimensions(q StructuredQuery) []constraintDimension {
	var dims []constraintDimension

	if len(q.Sites) > 0 {
		sites := q.Sites
		dims = append(dims, constraintDimension{
			label: joinPrefixed("site:", sites, " OR "),
			pred: func(src FilterableSource) bool {
				return slices.ContainsFunc(sites, func(site string) bool {
					return MatchesSite(src.URL, site)
				})
			},
		})
	}
	if len(q.ExcludedSites) > 0 {
		excluded := q.ExcludedSites
		dims = append(dims, constraintDimension{
			label: joinPrefixed("-site:", excluded, " "),
			pred: func(src FilterableSource) bool {
				return !slices.ContainsFunc(excluded, func(site string) bool {
					return MatchesSite(src.URL, site)
				})
			},
		})
	}
	if len(q.InURL) > 0 {
		values := q.InURL
		dims = append(dims, constraintDimension{
			label: joinPrefixed("inurl:", values, " "),
			pred: func(src FilterableSource) bool {
				return !slices.ContainsFunc(values, func(v string) bool {
					return !strings.Contains(strings.ToLower(src.URL), strings.ToLower(v))
				})
			},
		})
	}
	if len(q.ExcludedInURL) > 0 {
		excluded := q.ExcludedInURL
		dims = append(dims, constraintDimension{
			label: joinPrefixed("-inurl:", excluded, " "),
			pred: func(src FilterableSource) bool {
				return !slices.ContainsFunc(excluded, func(v string) bool {
					return strings.Contains(strings.ToLower(src.URL), strings.ToLower(v))
				})
			},
		})
	}
	if len(q.InTitle) > 0 {
		values := q.InTitle
		dims = append(dims, constraintDimension{
			label: joinPrefixed("intitle:", values, " "),
			pred: func(src FilterableSource) bool {
				return !slices.ContainsFunc(values, func(v string) bool {
					return !strings.Contains(strings.ToLower(src.Title), strings.ToLower(v))
				})
			},
		})
	}
	if len(q.ExcludedInTitle) > 0 {
		excluded := q.ExcludedInTitle
		dims = append(dims, constraintDimension{
			label: joinPrefixed("-intitle:", excluded, " "),
			pred: func(src FilterableSource) bool {
				return !slices.ContainsFunc(excluded, func(v string) bool {
					return strings.Contains(strings.ToLower(src.Title), strings.ToLower(v))
				})
			},
		})
	}
	if len(q.Filetypes) > 0 {
		filetypes := q.Filetypes
		dims = append(dims, constraintDimension{
			label: joinPrefixed("filetype:", filetypes, " OR "),
			pred: func(src FilterableSource) bool {
				return slices.ContainsFunc(filetypes, func(ext string) bool {
					return matchesFiletype(src.URL, ext)
				})
			},
		})
	}
	if len(q.ExcludedFiletypes) > 0 {
		excluded := q.ExcludedFiletypes
		dims = append(dims, constraintDimension{
			label: joinPrefixed("-filetype:", excluded, " "),
			pred: func(src FilterableSource) bool {
				return !slices.ContainsFunc(excluded, func(ext string) bool {
					return matchesFiletype(src.URL, ext)
				})
			},
		})
	}
	if q.After != nil || q.Before != nil {
		var afterTime, beforeTime time.Time
		if q.After != nil {
			afterTime, _ = time.Parse("2006-01-02", *q.After)
		}
		if q.Before != nil {
			beforeTime, _ = time.Parse("2006-01-02", *q.Before)
		}
		labelParts := make([]string, 0, 2)
		if q.After != nil {
			labelParts = append(labelParts, "after:"+*q.After)
		}
		if q.Before != nil {
			labelParts = append(labelParts, "before:"+*q.Before)
		}
		after, before := q.After, q.Before
		dims = append(dims, constraintDimension{
			label: strings.Join(labelParts, " "),
			pred: func(src FilterableSource) bool {
				t, ok := sourceTime(src)
				if !ok {
					return true // undated: cannot prove violation
				}
				if after != nil && t.Before(afterTime) {
					return false
				}
				if before != nil && !t.Before(beforeTime) {
					return false
				}
				return true
			},
		})
	}
	return dims
}

// MatchesQueryConstraints checks every filterable dimension of q against
// a single source strictly. Sources without a resolvable date pass date
// bounds (a missing date is not proof of violation). For custom provider
// flows; the standard path is ApplyQueryConstraints.
func MatchesQueryConstraints(source FilterableSource, q StructuredQuery) bool {
	for _, dim := range constraintDimensions(q) {
		if !dim.pred(source) {
			return false
		}
	}
	return true
}

// ApplyQueryConstraints leniently post-filters sources: it applies each
// constraint dimension of q in turn, skipping (and reporting) any
// dimension that would eliminate every remaining source. It guarantees a
// non-empty result for a non-empty input, so a mis-scoped directive
// degrades to unfiltered results plus a note instead of a dead search.
func ApplyQueryConstraints(sources []FilterableSource, q StructuredQuery) ConstraintFilterResult {
	current := make([]FilterableSource, len(sources))
	copy(current, sources)
	var dropped []string
	if len(current) == 0 {
		return ConstraintFilterResult{Sources: current, Dropped: dropped}
	}
	for _, dim := range constraintDimensions(q) {
		kept := make([]FilterableSource, 0, len(current))
		for _, src := range current {
			if dim.pred(src) {
				kept = append(kept, src)
			}
		}
		if len(kept) > 0 {
			current = kept
		} else {
			dropped = append(dropped, dim.label)
		}
	}
	return ConstraintFilterResult{Sources: current, Dropped: dropped}
}
