package public

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
)

func TestDedupKey(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		// Host is case-normalized and loses a leading www.; the path
		// loses its trailing slash; the query survives; the fragment
		// drops.
		"https://www.Example.com/A/?q=1#frag": "example.com/A?q=1",
		"https://example.com/A?q=1":           "example.com/A?q=1",
		"http://Example.com/a":                "example.com/a",
		// A bare root path keeps its slash.
		"https://example.com/": "example.com/",
		// Queries are preserved verbatim, not canonicalized.
		"https://example.com/p?b=2&a=1": "example.com/p?b=2&a=1",
		// Unparseable URLs fall back to the raw string.
		"http://[::1": "http://[::1",
	}
	for rawURL, want := range cases {
		require.Equal(t, want, dedupKey(rawURL), "raw URL %q", rawURL)
	}
}

func TestRankSourcesConsensusWins(t *testing.T) {
	t.Parallel()

	shared := "https://shared.example.com/consensus"
	unique := "https://unique.example.com/only-one-engine"
	perEngine := [][]search.SearchSource{
		{
			{Title: "Unique first", URL: unique},
			{Title: "Shared from A", URL: shared},
		},
		{
			{Title: "Shared from B", URL: shared},
		},
		{
			{Title: "Shared from C", URL: "https://shared.example.com/consensus/"},
		},
	}

	sources := rankSources(perEngine, 10)
	require.Len(t, sources, 2)
	// Three-engine consensus outranks a rank-0 single hit.
	require.Equal(t, shared, sources[0].URL)
	require.Equal(t, unique, sources[1].URL)
	// Engine 0 lists the shared URL at rank 1, engines 1 and 2 at rank
	// 0 (engine 2's trailing slash dedups away); the first rank-0
	// engine wins the merged row's title and URL spelling.
	require.Equal(t, "Shared from B", sources[0].Title)
	require.Equal(t, "https://shared.example.com/consensus", sources[0].URL)
}

func TestRankSourcesBestRankTiebreak(t *testing.T) {
	t.Parallel()

	perEngine := [][]search.SearchSource{
		{
			{Title: "A", URL: "https://a.example.com/"},
			{Title: "B", URL: "https://b.example.com/"},
		},
		{
			{Title: "B", URL: "https://b.example.com/"},
			{Title: "A", URL: "https://a.example.com/"},
		},
	}

	sources := rankSources(perEngine, 10)
	// Both URLs hit two engines; A's best rank is 0 versus B's 1.
	require.Equal(t, "https://a.example.com/", sources[0].URL)
	require.Equal(t, "https://b.example.com/", sources[1].URL)
}

func TestRankSourcesFirstSeenTiebreak(t *testing.T) {
	t.Parallel()

	perEngine := [][]search.SearchSource{
		{
			{Title: "First seen", URL: "https://first.example.com/"},
			{Title: "Second seen", URL: "https://second.example.com/"},
		},
		{
			{Title: "Other engine", URL: "https://third.example.com/"},
		},
	}

	sources := rankSources(perEngine, 10)
	// Every URL hits one engine; engine 0's rank-0 hit leads, engine
	// 1's rank-0 hit beats engine 0's rank-1 hit (bestRank sorts
	// before insertion order), and the rank-1 hit trails last.
	require.Equal(t, "https://first.example.com/", sources[0].URL)
	require.Equal(t, "https://third.example.com/", sources[1].URL)
	require.Equal(t, "https://second.example.com/", sources[2].URL)
}

func TestRankSourcesMergedFieldRules(t *testing.T) {
	t.Parallel()

	perEngine := [][]search.SearchSource{
		{
			{
				Title:         "Long-title from engine A",
				URL:           "https://example.com/page",
				Snippet:       "short",
				PublishedDate: "2026-01-01",
				AgeSeconds:    100,
			},
		},
		{
			{
				Title:         "B",
				URL:           "https://www.example.com/page/",
				Snippet:       "a much longer snippet from engine B",
				PublishedDate: "2025-01-01",
				AgeSeconds:    999,
			},
		},
	}

	sources := rankSources(perEngine, 10)
	require.Len(t, sources, 1)
	merged := sources[0]
	// Engine A holds the better rank (0 in both), so its title and URL
	// win; engine B contributes only its longer snippet; first-observed
	// publication metadata sticks.
	require.Equal(t, "Long-title from engine A", merged.Title)
	require.Equal(t, "https://example.com/page", merged.URL)
	require.Equal(t, "a much longer snippet from engine B", merged.Snippet)
	require.Equal(t, "2026-01-01", merged.PublishedDate)
	require.Equal(t, int64(100), merged.AgeSeconds)
}

func TestRankSourcesLimitTruncates(t *testing.T) {
	t.Parallel()

	perEngine := [][]search.SearchSource{
		{
			{Title: "One", URL: "https://one.example.com/"},
			{Title: "Two", URL: "https://two.example.com/"},
			{Title: "Three", URL: "https://three.example.com/"},
		},
	}

	sources := rankSources(perEngine, 2)
	require.Len(t, sources, 2)
	require.Equal(t, "https://one.example.com/", sources[0].URL)
	require.Equal(t, "https://two.example.com/", sources[1].URL)
}

func TestRankSourcesEmptyInput(t *testing.T) {
	t.Parallel()

	require.Empty(t, rankSources(nil, 10))
	require.Empty(t, rankSources([][]search.SearchSource{nil, nil}, 10))
}
