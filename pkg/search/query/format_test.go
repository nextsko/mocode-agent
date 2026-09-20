package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatQuery(t *testing.T) {
	t.Parallel()

	t.Run("re-emits full Google syntax", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery(
			`release notes site:github.com -site:gist.github.com filetype:md after:2024-05-01 intitle:"v2"`)
		require.Equal(t,
			"release notes site:github.com -site:gist.github.com intitle:v2 filetype:md after:2024-05-01",
			FormatQuery(q, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("emits OR-grouped sites for multi-site queries", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("cve site:nvd.nist.gov site:mitre.org")
		require.Equal(t,
			"cve (site:nvd.nist.gov OR site:mitre.org)",
			FormatQuery(q, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("produces plain keywords when the engine supports no syntax", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery(`(react OR vue) "state management" -redux site:dev.to filetype:pdf`)
		require.Equal(t, "react vue state management", FormatQuery(q, QuerySyntax{}))
	})

	t.Run("falls back to constraint values for directive-only queries", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("site:kubernetes.io filetype:yaml")
		require.Equal(t, "kubernetes.io yaml", FormatQuery(q, QuerySyntax{}))
	})
}

// The never-empty chain: main assembly, then constraint values as
// keywords, then the raw query.
func TestFormatQueryNeverReturnsEmpty(t *testing.T) {
	t.Parallel()

	t.Run("level 1: main assembly", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("rust async site:github.com")
		require.Equal(t, "rust async site:github.com", FormatQuery(q, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("level 2: constraint values as keywords", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("site:kubernetes.io filetype:yaml")
		require.Equal(t, "kubernetes.io yaml", FormatQuery(q, QuerySyntax{}))
	})

	t.Run("level 3: raw query fallback", func(t *testing.T) {
		t.Parallel()
		// lang and date bounds contribute no fallback keywords, so the
		// verbatim raw query must be returned.
		q := ParseSearchQuery("lang:en after:2024")
		require.Equal(t, "lang:en after:2024", FormatQuery(q, QuerySyntax{}))
	})
}

func TestFormatScraperQuery(t *testing.T) {
	t.Parallel()

	t.Run("demotes path-carrying site: and inurl: to plain terms, keeping bare-domain site:", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"17.1.1 release github.com/can1357/oh-my-pi releases site:github.com",
			FormatScraperQuery(
				"site:github.com/can1357/oh-my-pi inurl:releases site:github.com 17.1.1 release",
				nil, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("demotes every site in an OR-groupable multi-site query when all carry paths", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"cve nvd.nist.gov/vuln mitre.org/cgi-bin",
			FormatScraperQuery("cve site:nvd.nist.gov/vuln site:mitre.org/cgi-bin",
				nil, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("passes negated site:/inurl: through as operators", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"foo -site:github.com/x -inurl:bar",
			FormatScraperQuery("foo -site:github.com/x -inurl:bar", nil, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("passes directive-free queries through byte-identical", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"github.com/can1357/oh-my-pi 17.1.1 release",
			FormatScraperQuery("github.com/can1357/oh-my-pi 17.1.1 release",
				nil, GOOGLE_QUERY_SYNTAX))
	})

	t.Run("respects a narrower engine syntax while still demoting hostile operators", func(t *testing.T) {
		t.Parallel()
		require.Equal(t,
			"a y.com/z w site:x.com",
			FormatScraperQuery("a site:x.com site:y.com/z inurl:w", nil,
				QuerySyntax{Phrases: true, Negation: true, Site: true}))
	})
}
