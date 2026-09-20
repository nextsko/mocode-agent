package theme

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTruncateToWidthPlain(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hell…", TruncateToWidth("hello world", 5, "…"))
	require.Equal(t, "hello", TruncateToWidth("hello", 10, "…"))
	require.Equal(t, "hello", TruncateToWidth("hello", 5, "…"))
	require.Equal(t, "", TruncateToWidth("hello", 0, "…"))
}

func TestTruncateToWidthPreservesANSI(t *testing.T) {
	t.Parallel()
	colored := "\x1b[31mhello world\x1b[0m"
	got := TruncateToWidth(colored, 5, "…")
	// Visible width must respect the budget ...
	require.Equal(t, 5, VisibleWidth(got))
	// ... and the escape sequences must survive intact.
	require.Contains(t, got, "\x1b[31m")
	require.Contains(t, got, "…")
}

func TestTruncateToWidthCJK(t *testing.T) {
	t.Parallel()
	// Wide graphemes are never split: 2 CJK runes fill 4 cells, the
	// ellipsis takes the 5th.
	require.Equal(t, "你好…", TruncateToWidth("你好世界", 5, "…"))
	require.Equal(t, "你好世界", TruncateToWidth("你好世界", 8, "…"))
}

func TestVisibleWidthIgnoresANSI(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2, VisibleWidth("\x1b[31mhi\x1b[0m"))
	require.Equal(t, 2, VisibleWidth("\x1b[38;5;205mhi\x1b[0m"))
	require.Equal(t, 0, VisibleWidth(""))
	// OSC 8 escapes are zero-width; the display text stays visible.
	require.Equal(t, 4,
		VisibleWidth("\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\"))
}

func TestVisibleWidthWideGraphemes(t *testing.T) {
	t.Parallel()
	require.Equal(t, 4, VisibleWidth("你好"))
	require.Equal(t, 4, VisibleWidth("a👍b"))
	require.Equal(t, 5, VisibleWidth("你好x"))
}

func TestHyperlinkWidth(t *testing.T) {
	t.Parallel()
	link := Hyperlink("https://example.com", "example")
	// OSC 8 wrapper contributes zero width.
	require.Equal(t, 7, VisibleWidth(link))
}

func TestLineBudget(t *testing.T) {
	t.Parallel()
	require.Equal(t, 24, LineBudget(0))
	require.Equal(t, 24, LineBudget(10))
	require.Equal(t, 24, LineBudget(30))
	require.Equal(t, 34, LineBudget(40))
	require.Equal(t, 114, LineBudget(120))
}

func TestTitleBudget(t *testing.T) {
	t.Parallel()
	require.Equal(t, 24, TitleBudget(34, 10))
	require.Equal(t, 25, TitleBudget(30, 5))
	require.Equal(t, 12, TitleBudget(20, 15))
	require.Equal(t, 12, TitleBudget(10, 40))
}

func TestWrapTextPlain(t *testing.T) {
	t.Parallel()
	require.Equal(t, "abc def\nghi", WrapText("abc def ghi", 7))
	require.Equal(t, "abc def ghi", WrapText("abc def ghi", 20))
}

func TestWrapTextPreservesANSI(t *testing.T) {
	t.Parallel()
	colored := "\x1b[31mabc def ghi\x1b[0m"
	got := WrapText(colored, 7)
	require.Contains(t, got, "\x1b[31m")
	for _, line := range strings.Split(got, "\n") {
		require.LessOrEqual(t, VisibleWidth(line), 7)
	}
}

func TestWrapTextCJK(t *testing.T) {
	t.Parallel()
	got := WrapText("你好世界", 4)
	require.Equal(t, "你好\n世界", got)
}
