package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// goldenResp is the 80-column golden fixture: an empty answer (avoids
// glamour-version brittleness), two sources, full usage.
func goldenResp() *search.SearchResponse {
	return &search.SearchResponse{
		Provider: "perplexity",
		Model:    "sonar-pro",
		AuthMode: "api_key",
		Sources: []search.SearchSource{
			{
				Title:      "Go Generics Guide",
				URL:        "https://example.com/go-generics",
				AgeSeconds: 172800,
			},
			{
				Title:         "Generics in Go",
				URL:           "https://www.news.org/generics",
				PublishedDate: "2026-09-01",
			},
		},
		Usage: &search.SearchUsage{
			InputTokens:    1240,
			OutputTokens:   87,
			TotalTokens:    1327,
			SearchRequests: 3,
		},
	}
}

// pad pads text with n trailing spaces.
func pad(n int) string { return strings.Repeat(" ", n) }

// row wraps inner into a no-color content row of the given outer width.
func row(inner string, width int) string {
	return "│ " + inner + pad(width-4-theme.VisibleWidth(inner)) + " │"
}

func TestRenderResultGoldenEightyColumns(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	lines := r.RenderResult(goldenResp(), RenderOptions{
		Width: 80,
		Query: "golang generics",
	})
	require.Len(t, lines, 11)

	// Top border: header embedded, full width.
	require.True(t, strings.HasPrefix(lines[0], "┌─── "), lines[0])
	require.True(t, strings.HasSuffix(lines[0], "┐"), lines[0])
	require.Contains(t, lines[0], "⌕ Web Search: Perplexity 2 sources")
	require.Equal(t, 80, theme.VisibleWidth(lines[0]))

	require.Equal(t,
		row("Query: golang generics", 80),
		lines[1],
	)

	require.Equal(t,
		"├─── Answer "+dashes(67)+"┤",
		lines[2],
	)
	require.Equal(t,
		row("No answer text returned", 80),
		lines[3],
	)

	require.Equal(t,
		"├─── Sources "+dashes(66)+"┤",
		lines[4],
	)
	link1 := "\x1b]8;;https://example.com/go-generics\x1b\\" +
		"Go Generics Guide" +
		"\x1b]8;;\x1b\\"
	require.Equal(t,
		row("├─ "+link1+" (example.com) · 2d ago", 80),
		lines[5],
	)
	link2 := "\x1b]8;;https://www.news.org/generics\x1b\\" +
		"Generics in Go" +
		"\x1b]8;;\x1b\\"
	require.Equal(t,
		row("└─ "+link2+" (news.org) · 2026-09-01", 80),
		lines[6],
	)

	require.Equal(t,
		"├─── Metadata "+dashes(65)+"┤",
		lines[7],
	)
	require.Equal(t,
		row("Provider: sonar-pro @ Perplexity (API)", 80),
		lines[8],
	)
	require.Equal(t,
		row("Usage: in 1240 · out 87 · total 1327 · search 3", 80),
		lines[9],
	)

	require.Equal(t,
		"└"+dashes(78)+"┘",
		lines[10],
	)
	for _, line := range lines {
		require.Equal(t, 80, theme.VisibleWidth(line))
	}
}

func TestRenderResultOneHundredTwentyColumns(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	resp := goldenResp()
	resp.Answer = "Some answer text."
	lines := r.RenderResult(resp, RenderOptions{Width: 120})
	require.NotEmpty(t, lines)
	for _, line := range lines {
		require.Equal(t, 120, theme.VisibleWidth(line))
	}
}

func TestRenderResultWarningStateWithoutSources(t *testing.T) {
	t.Parallel()
	r := New(mockTheme{}, RendererOptions{})
	resp := &search.SearchResponse{Provider: "perplexity"}
	lines := r.RenderResult(resp, RenderOptions{Width: 80, Query: "q"})

	require.True(t, strings.HasPrefix(lines[0], "<warning:┌"), lines[0])
	require.Contains(t, lines[0], "⚠")
	require.Contains(t, lines[0], "<accent:Web Search>")
	require.Contains(t, lines[0], "<muted:Perplexity>")
	require.Contains(t, lines[0], "<dim:0 sources>")
	joined := strings.Join(lines, "\n")
	require.Contains(t, joined, "<muted:No sources returned>")
	require.Contains(t, joined, "<muted:No answer text returned>")
}

func TestRenderResultAnswerCapInCompactMode(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{MaxAnswerLines: 5})
	resp := goldenResp()
	resp.Answer = paragraphs(20)
	lines := r.RenderResult(resp, RenderOptions{Width: 80})

	var answer, sources int
	for i, line := range lines {
		if strings.Contains(line, " Answer ") {
			answer = i
		}
		if strings.Contains(line, " Sources ") {
			sources = i
		}
	}
	answerRows := lines[answer+1 : sources]
	require.Len(t, answerRows, 6) // 5 capped + truncation suffix
	require.Contains(t, answerRows[len(answerRows)-1], "… (truncated)")

	// Expanded: no cap.
	lines = r.RenderResult(resp, RenderOptions{Width: 80, Expanded: true})
	require.NotContains(t, strings.Join(lines, "\n"), "… (truncated)")
}

func TestRenderResultSourceTreeCollapseAndExpand(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{MaxCollapsedItems: 4})
	resp := &search.SearchResponse{Provider: "exa"}
	for i := 1; i <= 7; i++ {
		resp.Sources = append(resp.Sources, search.SearchSource{
			Title: "S" + string(rune('0'+i)),
			URL:   "https://example.com/" + string(rune('0'+i)),
		})
	}

	findBars := func(lines []string) (int, int) {
		var sources, metadata int
		for i, line := range lines {
			if strings.Contains(line, " Sources ") {
				sources = i
			}
			if strings.Contains(line, " Metadata ") {
				metadata = i
			}
		}
		return sources, metadata
	}

	collapsed := r.RenderResult(resp, RenderOptions{Width: 80})
	s, m := findBars(collapsed)
	rows := collapsed[s+1 : m]
	require.Len(t, rows, 4) // 3 items + summary, budget 4
	require.Contains(t, rows[3], "… 4 more sources")

	expanded := r.RenderResult(resp, RenderOptions{Width: 80, Expanded: true})
	s, m = findBars(expanded)
	rows = expanded[s+1 : m]
	require.Len(t, rows, 8) // 7 items + collapse hint
	require.Contains(t, rows[7], "▴ collapse")
}

func TestRenderResultReusesBlockCache(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	resp := goldenResp()
	opts := RenderOptions{Width: 80, Query: "golang generics"}
	first := r.RenderResult(resp, opts)
	second := r.RenderResult(resp, opts)
	require.Equal(t, first, second)
	require.Equal(t, 1, r.block.CacheHits())

	r.Invalidate()
	require.Equal(t, 0, r.block.CacheSize())
}

func TestRenderResultNilResponseFallsBackToPlainTree(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	lines := r.RenderResult(nil, RenderOptions{
		Width: 80,
		Query: "first line\nsecond line\nthird line",
	})
	require.True(t, strings.HasPrefix(lines[0], "⚠ Response"), lines[0])
	require.Len(t, lines, 4)
	require.Contains(t, lines[1], "├─")
	require.Contains(t, lines[3], "└─ third line")

	empty := r.RenderResult(nil, RenderOptions{Width: 80})
	require.Contains(t, empty[len(empty)-1], "No response data")
}

func TestRenderCallShowsQueryAndParams(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	lines := r.RenderCall(map[string]any{
		"query":   "golang generics",
		"limit":   5.0,
		"recency": "week",
	})
	require.Len(t, lines, 1)
	require.Equal(t,
		"◐ Web Search: golang generics limit 5 recency week",
		lines[0],
	)
}

func TestRenderCallCountsDirectives(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	lines := r.RenderCall(map[string]any{
		"query": "site:github.com generics before:2026-01-01",
	})
	require.Contains(t, lines[0], "site:github.com generics before:2026-01-01")
	require.Contains(t, lines[0], "2 directives")
}

func TestRenderCallFallsBackToJSONFragment(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	lines := r.RenderCall(map[string]any{"foo": "bar"})
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], `{"foo":"bar"}`)

	nilLines := r.RenderCall(nil)
	require.Len(t, nilLines, 1)
	require.Contains(t, nilLines[0], "{}")
}

func TestRenderErrorFramedPanel(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	err := &search.SearchProviderError{
		Provider:   "perplexity",
		Code:       search.ErrorCodeRateLimited,
		Message:    "429 rate limited",
		HTTPStatus: 429,
	}
	lines := r.RenderError(err, 80)
	require.NotEmpty(t, lines)
	joined := strings.Join(lines, "\n")
	require.Contains(t, joined, "✗ Web Search: Perplexity")
	require.Contains(t, joined, "Error: perplexity: 429 rate limited")
	require.Contains(t, joined, "code rate_limited · http 429")
	for _, line := range lines {
		require.Equal(t, 80, theme.VisibleWidth(line))
	}
}

func TestRenderErrorFramedPanelUsesErrorBorder(t *testing.T) {
	t.Parallel()
	r := New(mockTheme{}, RendererOptions{})
	err := &search.SearchProviderError{Provider: "tavily", Message: "boom"}
	lines := r.RenderError(err, 60)
	require.True(t, strings.HasPrefix(lines[0], "<error:┌"), lines[0])
}

func TestRenderErrorSingleLineFallback(t *testing.T) {
	t.Parallel()
	r := New(mockTheme{}, RendererOptions{})
	err := &search.SearchProviderError{Provider: "tavily", Message: "boom"}
	lines := r.RenderError(err, 0)
	require.Equal(t,
		[]string{"<error:Error: tavily: boom>"},
		lines,
	)
}

func TestRenderErrorPlainError(t *testing.T) {
	t.Parallel()
	r := New(theme.NewNoColorTheme(), RendererOptions{})
	lines := r.RenderError(nil, 40)
	require.Contains(t, strings.Join(lines, "\n"), "Error: unknown error")
}

func TestMergeCallAndResult(t *testing.T) {
	t.Parallel()
	require.True(t, New(mockTheme{}, RendererOptions{}).MergeCallAndResult())
}

func TestProviderLabelAndAuthShort(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Perplexity", providerLabel("perplexity"))
	require.Equal(t, "SearXNG", providerLabel("searxng"))
	require.Equal(t, "Public Web", providerLabel("public"))
	require.Equal(t, "custom-x", providerLabel("custom-x"))

	require.Equal(t, "OAuth", authShort("oauth"))
	require.Equal(t, "API", authShort("api_key"))
	require.Equal(t, "", authShort(""))
	require.Equal(t, "pat", authShort("pat"))
}

func TestRenderStatusLineWithIconComposition(t *testing.T) {
	t.Parallel()
	got := RenderStatusLineWithIcon(mockTheme{},
		mockTheme{}.StyledSymbol("tool.webSearch", "⌕"),
		"Web Search", "Perplexity", "2 sources")
	require.Equal(t,
		"<sym:tool.webSearch|⌕> <accent:Web Search>: <muted:Perplexity> <dim:2 sources>",
		got,
	)
}

func TestRenderStatusLineDelegatesToTheme(t *testing.T) {
	t.Parallel()
	got := RenderStatusLine(theme.NewNoColorTheme(),
		theme.StateSuccess, "Web Search", "Perplexity", "2 sources")
	require.Equal(t, "✓ Web Search: Perplexity 2 sources", got)
}

func TestFormatCount(t *testing.T) {
	t.Parallel()
	require.Equal(t, "1 source", FormatCount("source", 1))
	require.Equal(t, "5 sources", FormatCount("source", 5))
	require.Equal(t, "2 entries", FormatCount("entry", 2))
	require.Equal(t, "1 directive", FormatCount("directive", 1))
}
