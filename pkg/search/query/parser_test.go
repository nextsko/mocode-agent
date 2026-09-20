package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// termTexts extracts the plain text of each term.
func termTexts(terms []QueryTerm) []string {
	out := make([]string, len(terms))
	for i, t := range terms {
		out[i] = t.Text
	}
	return out
}

func TestParseSearchQuery(t *testing.T) {
	t.Parallel()

	t.Run("leaves plain queries untouched", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("rust async runtime comparison")
		require.Equal(t, "rust async runtime comparison", q.Text)
		require.False(t, q.HasDirectives)
		require.False(t, q.HasConstraints)
		require.Equal(t,
			[]string{"rust", "async", "runtime", "comparison"}, termTexts(q.Terms))
	})

	t.Run("keeps unknown colon tokens (URLs, paths, error codes) as text", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("error TS2345: https://example.com/a?b=c C:\\Users\\me")
		require.False(t, q.HasConstraints)
		require.Equal(t, []string{
			"error", "TS2345:", "https://example.com/a?b=c", "C:\\Users\\me",
		}, termTexts(q.Terms))
	})

	t.Run("parses site: aliases and exclusions with normalization", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery(
			"kubernetes site:HTTPS://Docs.K8s.IO/ domain:cncf.io -site:*.reddit.com host:github.com/kubernetes")
		require.Equal(t, []string{"docs.k8s.io", "cncf.io", "github.com/kubernetes"}, q.Sites)
		require.Equal(t, []string{"reddit.com"}, q.ExcludedSites)
		require.Equal(t, "kubernetes", q.Text)
		require.True(t, q.HasConstraints)
	})

	t.Run("adopts the next token when a directive value is space-separated", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("site: arxiv.org transformer scaling")
		require.Equal(t, []string{"arxiv.org"}, q.Sites)
		require.Equal(t, "transformer scaling", q.Text)
	})

	t.Run("does not adopt operators or directives as space-separated values", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("site: OR site:example.com")
		require.Equal(t, []string{"example.com"}, q.Sites)
	})

	t.Run("parses inurl/intitle/intext variants including quoted values", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery(
			`inurl:docs intitle:"getting started" -intitle:deprecated inbody:websocket handshake`)
		require.Equal(t, []string{"docs"}, q.InURL)
		require.Equal(t, []string{"getting started"}, q.InTitle)
		require.Equal(t, []string{"deprecated"}, q.ExcludedInTitle)
		require.Equal(t, []string{"websocket"}, q.InText)
		require.Equal(t, "handshake", q.Text)
	})

	t.Run("routes every following term for allintitle:", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("allintitle: budget planning tips")
		require.Equal(t, []string{"budget", "planning", "tips"}, q.InTitle)
		require.Equal(t, "", q.Text)
	})

	t.Run("parses filetype/ext with normalization", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("quarterly report filetype:PDF ext:.xlsx -filetype:doc")
		require.Equal(t, []string{"pdf", "xlsx"}, q.Filetypes)
		require.Equal(t, []string{"doc"}, q.ExcludedFiletypes)
	})

	t.Run("parses date bounds in common forms", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("llm evals after:2024 before:2025-06-15")
		require.Equal(t, "2024-01-01", *q.After)
		require.Equal(t, "2025-06-15", *q.Before)
		require.Equal(t, "llm evals", q.Text)

		require.Equal(t, "2023-07-01", *ParseSearchQuery("x since:2023/07/01").After)
		require.Equal(t, "2024-02-01", *ParseSearchQuery("x until:2024-02").Before)
		require.Equal(t, "2024-06-15", *ParseSearchQuery("x after:6/15/2024").After)
		require.Equal(t, "2024-06-15", *ParseSearchQuery("x after:15/6/2024").After)
	})

	t.Run("degrades unparseable date values to plain terms", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("release before:soon")
		require.Nil(t, q.Before)
		require.Equal(t, []string{"release", "before:soon"}, termTexts(q.Terms))
	})

	t.Run("parses quoted phrases including smart quotes and negated phrases", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("\"exact phrase\" \u201csmart quoted\u201d -\"not this\"")
		require.Equal(t, []QueryTerm{
			{Text: "exact phrase", Phrase: true},
			{Text: "smart quoted", Phrase: true},
			{Text: "not this", Phrase: true, Negated: true},
		}, q.Terms)
		require.Equal(t, "\"exact phrase\" \"smart quoted\" -\"not this\"", q.Text)
	})

	t.Run("groups OR alternatives and treats AND as default conjunction", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("(react OR vue OR svelte) AND hooks")
		require.Equal(t, []string{"react", "vue", "svelte", "hooks"}, termTexts(q.Terms))
		require.NotZero(t, q.Terms[0].GroupID)
		require.Equal(t, q.Terms[0].GroupID, q.Terms[1].GroupID)
		require.Equal(t, q.Terms[1].GroupID, q.Terms[2].GroupID)
		require.Zero(t, q.Terms[3].GroupID)
		require.Equal(t, "(react OR vue OR svelte) hooks", q.Text)
	})

	t.Run("supports pipe as OR and NOT as negation", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("deno | bun NOT node")
		require.Equal(t, q.Terms[0].GroupID, q.Terms[1].GroupID)
		require.Equal(t, QueryTerm{Text: "node", Negated: true}, q.Terms[2])
	})

	t.Run("swallows OR between directives instead of grouping terms", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("caching site:redis.io OR site:memcached.org")
		require.Equal(t, []string{"redis.io", "memcached.org"}, q.Sites)
		require.Equal(t, []QueryTerm{{Text: "caching"}}, q.Terms)
	})

	t.Run("keeps wikipedia-style parens inside directive values", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("site:en.wikipedia.org/wiki/Rust_(programming_language) borrow checker")
		require.Equal(t,
			[]string{"en.wikipedia.org/wiki/rust_(programming_language)"}, q.Sites)
		require.Equal(t, "borrow checker", q.Text)
	})

	t.Run("treats legacy +term as an exact phrase", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("+immutable data")
		require.Equal(t, QueryTerm{Text: "immutable", Phrase: true}, q.Terms[0])
	})

	t.Run("parses lang: into a language code", func(t *testing.T) {
		t.Parallel()
		q := ParseSearchQuery("documentation lang:EN-us")
		require.Equal(t, "en-us", *q.Lang)
		require.False(t, q.HasConstraints)
	})
}

func TestParseDateValueRejectsInvalidComponents(t *testing.T) {
	t.Parallel()
	require.Empty(t, ParseDateValue("2024-13-01"))
	require.Empty(t, ParseDateValue("2024-00-10"))
	require.Empty(t, ParseDateValue("notadate"))
	require.Empty(t, ParseDateValue("24-01-01"))
}

// OR-group ids must be unique across parsed queries (process-wide
// counter) while staying shared inside one query's group.
func TestParseSearchQueryORGroupIDUniqueness(t *testing.T) {
	t.Parallel()
	q1 := ParseSearchQuery("a OR b")
	q2 := ParseSearchQuery("c OR d")
	require.NotZero(t, q1.Terms[0].GroupID)
	require.Equal(t, q1.Terms[0].GroupID, q1.Terms[1].GroupID)
	require.Equal(t, q2.Terms[0].GroupID, q2.Terms[1].GroupID)
	require.NotEqual(t, q1.Terms[0].GroupID, q2.Terms[0].GroupID)
}

// The tokenizer must treat NBSP as whitespace and curly quotes as quote
// delimiters.
func TestTokenizeUnicodeWhitespaceAndSmartQuotes(t *testing.T) {
	t.Parallel()
	toks := tokenize("rust\u00a0async\u201csmart phrase\u201dgo")
	texts := make([]string, len(toks))
	for i, tok := range toks {
		texts[i] = tok.text
	}
	require.Equal(t, []string{"rust", "async", "smart phrase", "go"}, texts)
	require.True(t, toks[2].quoted)
}
