package public

import (
	"cmp"
	"net/url"
	"slices"
	"strings"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// mergedSource is the accumulator for one deduplicated URL across
// engines.
type mergedSource struct {
	source search.SearchSource

	// engines counts how many engines returned this URL — the primary
	// ranking signal.
	engines int

	// bestRank is the best (lowest) per-engine rank observed.
	bestRank int

	// order is the first-seen insertion index; the final tiebreak
	// keeps ordering deterministic.
	order int
}

// dedupKey is the canonical dedup key for a result URL: a
// case-normalized host without a leading www., a path without its
// trailing slash, the query preserved, and the fragment dropped.
// Engines disagree on exactly these variations for the same page.
// Unparseable URLs fall back to the raw string.
func dedupKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := u.Path
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	key := host + path
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key
}

// rankSources merges per-engine result lists and returns the
// consensus-ranked, limit-truncated source list. perEngine is indexed
// by engine priority order (the fan-out roster order), not settlement
// order, so ranking tiebreaks stay deterministic: URLs are
// deduplicated across engines, ranked by cross-engine consensus
// count, then by best per-engine rank, then by first-seen insertion
// order. Merged rows keep the best-ranked engine's title and URL, the
// longest snippet, and the first observed publication metadata.
func rankSources(perEngine [][]search.SearchSource, limit int) []search.SearchSource {
	merged := make(map[string]*mergedSource)
	next := 0
	for _, sources := range perEngine {
		for rank, source := range sources {
			key := dedupKey(source.URL)
			existing, ok := merged[key]
			if !ok {
				merged[key] = &mergedSource{
					source:   source,
					engines:  1,
					bestRank: rank,
					order:    next,
				}
				next++
				continue
			}
			existing.engines++
			if rank < existing.bestRank {
				existing.bestRank = rank
				existing.source.Title = source.Title
				existing.source.URL = source.URL
			}
			// Keep the most informative snippet regardless of which
			// engine ranked the URL best.
			if len(source.Snippet) > len(existing.source.Snippet) {
				existing.source.Snippet = source.Snippet
			}
			if existing.source.PublishedDate == "" {
				existing.source.PublishedDate = source.PublishedDate
			}
			if existing.source.AgeSeconds == 0 {
				existing.source.AgeSeconds = source.AgeSeconds
			}
		}
	}

	entries := make([]*mergedSource, 0, len(merged))
	for _, entry := range merged {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b *mergedSource) int {
		return cmp.Or(
			cmp.Compare(b.engines, a.engines),
			cmp.Compare(a.bestRank, b.bestRank),
			cmp.Compare(a.order, b.order),
		)
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	sources := make([]search.SearchSource, len(entries))
	for i, entry := range entries {
		sources[i] = entry.source
	}
	return sources
}
