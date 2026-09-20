package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// urls extracts the URL of each source.
func urls(sources []FilterableSource) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = s.URL
	}
	return out
}

// constraintTestSources mirrors the TS test fixture.
func constraintTestSources() []FilterableSource {
	return []FilterableSource{
		{Title: "K8s docs — Install", URL: "https://kubernetes.io/docs/setup/install.pdf"},
		{Title: "Random blog", URL: "https://blog.example.com/k8s", PublishedDate: "2023-01-15"},
		{Title: "Reddit thread", URL: "https://www.reddit.com/r/kubernetes/post"},
		{Title: "K8s blog", URL: "https://kubernetes.io/blog/2024", AgeSeconds: 3600},
	}
}

func TestMatchesSite(t *testing.T) {
	t.Parallel()
	t.Run("matches exact hosts, subdomains, and path prefixes", func(t *testing.T) {
		t.Parallel()
		require.True(t, MatchesSite("https://docs.k8s.io/setup", "k8s.io"))
		require.True(t, MatchesSite("https://k8s.io/", "k8s.io"))
		require.False(t, MatchesSite("https://notk8s.io/", "k8s.io"))
		require.True(t, MatchesSite("https://github.com/anthropics/sdk", "github.com/anthropics"))
		require.False(t, MatchesSite("https://github.com/other/sdk", "github.com/anthropics"))
		require.False(t, MatchesSite("not a url", "k8s.io"))
	})
}

func TestApplyQueryConstraints(t *testing.T) {
	t.Parallel()
	sources := constraintTestSources()

	t.Run("filters by included site", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("install site:kubernetes.io")
		res := ApplyQueryConstraints(sources, q)
		require.Equal(t, []string{
			"https://kubernetes.io/docs/setup/install.pdf",
			"https://kubernetes.io/blog/2024",
		}, urls(res.Sources))
		require.Empty(t, res.Dropped)
	})

	t.Run("drops a constraint that would eliminate every result and reports it", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("install site:nonexistent.example")
		res := ApplyQueryConstraints(sources, q)
		require.Len(t, res.Sources, len(sources))
		require.Equal(t, []string{"site:nonexistent.example"}, res.Dropped)
	})

	t.Run("relaxes dimensions independently", func(t *testing.T) {
		t.Parallel()
		// site matches two sources, filetype:docx matches none of those;
		// only filetype is relaxed.
		q := ParseSearchQuery("install site:kubernetes.io filetype:docx")
		res := ApplyQueryConstraints(sources, q)
		require.Equal(t, []string{
			"https://kubernetes.io/docs/setup/install.pdf",
			"https://kubernetes.io/blog/2024",
		}, urls(res.Sources))
		require.Equal(t, []string{"filetype:docx"}, res.Dropped)
	})

	t.Run("applies exclusions and filetype filters", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("k8s -site:reddit.com filetype:pdf")
		res := ApplyQueryConstraints(sources, q)
		require.Equal(t,
			[]string{"https://kubernetes.io/docs/setup/install.pdf"}, urls(res.Sources))
		require.Empty(t, res.Dropped)
	})

	t.Run("filters by date bounds while letting undated sources pass", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("k8s after:2024-01-01")
		res := ApplyQueryConstraints(sources, q)
		// The 2023 blog post is provably too old; undated sources
		// survive.
		require.Equal(t, []string{
			"https://kubernetes.io/docs/setup/install.pdf",
			"https://www.reddit.com/r/kubernetes/post",
			"https://kubernetes.io/blog/2024",
		}, urls(res.Sources))
	})

	t.Run("parses relative published dates", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("news before:2020")
		relative := []FilterableSource{
			{Title: "old", URL: "https://a.example/1", PublishedDate: "2 days ago"},
			{Title: "undated", URL: "https://a.example/2"},
		}
		res := ApplyQueryConstraints(relative, q)
		// "2 days ago" is provably after 2020 → violates before:2020 →
		// only the undated source survives.
		require.Equal(t, []string{"https://a.example/2"}, urls(res.Sources))
		require.Empty(t, res.Dropped)
	})

	t.Run("returns empty input untouched", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("x site:a.com")
		res := ApplyQueryConstraints(nil, q)
		require.Empty(t, res.Sources)
		require.Empty(t, res.Dropped)
	})
}

func TestMatchesQueryConstraints(t *testing.T) {
	t.Parallel()
	t.Run("checks all dimensions strictly", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("site:kubernetes.io inurl:docs")
		require.True(t, MatchesQueryConstraints(
			FilterableSource{Title: "t", URL: "https://kubernetes.io/docs/setup"}, q))
		require.False(t, MatchesQueryConstraints(
			FilterableSource{Title: "t", URL: "https://kubernetes.io/blog/x"}, q))
	})
}

// before: is exclusive: a source published exactly on the bound date is
// excluded, one published strictly earlier survives.
func TestApplyQueryConstraintsBeforeExclusive(t *testing.T) {
	t.Parallel()
	q := ParseSearchQuery("x before:2024-01-01")
	sources := []FilterableSource{
		{Title: "on the day", URL: "https://a.example/day", PublishedDate: "2024-01-01"},
		{Title: "earlier", URL: "https://a.example/early", PublishedDate: "2023-12-31"},
	}
	res := ApplyQueryConstraints(sources, q)
	require.Equal(t, []string{"https://a.example/early"}, urls(res.Sources))
	require.Empty(t, res.Dropped)
}

// after: is inclusive: a source published exactly on the bound date
// survives.
func TestApplyQueryConstraintsAfterInclusive(t *testing.T) {
	t.Parallel()
	q := ParseSearchQuery("x after:2024-01-01")
	sources := []FilterableSource{
		{Title: "on the day", URL: "https://a.example/day", PublishedDate: "2024-01-01"},
		{Title: "earlier", URL: "https://a.example/early", PublishedDate: "2023-12-31"},
	}
	res := ApplyQueryConstraints(sources, q)
	require.Equal(t, []string{"https://a.example/day"}, urls(res.Sources))
	require.Empty(t, res.Dropped)
}
