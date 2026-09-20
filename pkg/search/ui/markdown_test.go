package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// paragraphs builds n markdown paragraphs, each rendering to at least one
// line.
func paragraphs(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "Paragraph %d content here.\n\n", i)
	}
	return strings.TrimRight(b.String(), "\n")
}

func TestRenderMarkdownEmptyTextDegrades(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "   ", "\n\t\n "} {
		lines := RenderMarkdown(text, 60, MarkdownOptions{})
		require.Equal(t, []string{"No answer text returned"}, lines)
	}
}

func TestRenderMarkdownEmptyTextUsesThemeMuted(t *testing.T) {
	t.Parallel()
	lines := RenderMarkdown("", 60, MarkdownOptions{Theme: mockTheme{}})
	require.Equal(t, []string{"<muted:No answer text returned>"}, lines)
}

func TestRenderMarkdownRendersGlamour(t *testing.T) {
	t.Parallel()
	lines := RenderMarkdown("# Hello\n\nWorld **bold**", 60,
		MarkdownOptions{Style: "notty"})
	require.NotEmpty(t, lines)
	require.Contains(t, strings.Join(lines, "\n"), "Hello")
	require.Contains(t, strings.Join(lines, "\n"), "bold")
}

func TestRenderMarkdownCompactCapsAtMaxAnswerLines(t *testing.T) {
	t.Parallel()
	lines := RenderMarkdown(paragraphs(10), 60, MarkdownOptions{
		Compact:        true,
		MaxAnswerLines: 3,
	})
	require.Len(t, lines, 4)
	require.Contains(t, strings.Join(lines, "\n"), "Paragraph 1")
	require.Equal(t, "… (truncated)", lines[3])
}

func TestRenderMarkdownCompactCapStyledByTheme(t *testing.T) {
	t.Parallel()
	lines := RenderMarkdown(paragraphs(10), 60, MarkdownOptions{
		Theme:          mockTheme{},
		Compact:        true,
		MaxAnswerLines: 2,
	})
	require.Equal(t, "<muted:… (truncated)>", lines[len(lines)-1])
}

func TestRenderMarkdownNonCompactKeepsEverything(t *testing.T) {
	t.Parallel()
	lines := RenderMarkdown(paragraphs(10), 60, MarkdownOptions{
		Compact:        false,
		MaxAnswerLines: 3,
	})
	require.Greater(t, len(lines), 4)
	require.NotContains(t, strings.Join(lines, "\n"), "… (truncated)")
}

func TestRenderMarkdownDefaultCapIsThirtyLines(t *testing.T) {
	t.Parallel()
	lines := RenderMarkdown(paragraphs(40), 60, MarkdownOptions{
		Compact: true, // MaxAnswerLines unset -> DefaultMaxAnswerLines
	})
	require.Len(t, lines, DefaultMaxAnswerLines+1)
	require.Equal(t, "… (truncated)", lines[len(lines)-1])
}

func TestRenderMarkdownUnknownStyleFallsBackToWrappedText(t *testing.T) {
	t.Parallel()
	text := "plain answer words that must survive the fallback path"
	lines := RenderMarkdown(text, 30, MarkdownOptions{Style: "no-such-style"})
	require.NotEmpty(t, lines)
	joined := strings.Join(lines, "\n")
	require.Contains(t, joined, "plain answer")
	require.Contains(t, joined, "fallback path")
	for _, line := range lines {
		require.LessOrEqual(t, len([]rune(line)), 30)
	}
}
