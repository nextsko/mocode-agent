package theme

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxCollapsedItems(t *testing.T) {
	t.Parallel()
	require.Equal(t, 8, MaxCollapsedItems)
}

func TestFormatExpandHint(t *testing.T) {
	t.Parallel()
	require.Equal(t, "▴ collapse", FormatExpandHint(true, true))
	require.Equal(t, "▴ collapse", FormatExpandHint(true, false))
	require.Equal(t, "▾ expand", FormatExpandHint(false, true))
	require.Equal(t, "", FormatExpandHint(false, false))
}

func TestFormatMoreItems(t *testing.T) {
	t.Parallel()
	require.Equal(t, "… 3 more sources", FormatMoreItems(3, "source"))
	require.Equal(t, "… 1 more line", FormatMoreItems(1, "line"))
	require.Equal(t, "… 2 more entries", FormatMoreItems(2, "entry"))
	require.Equal(t, "… 0 more lines", FormatMoreItems(0, "line"))
	require.Equal(t, "… 0 more lines", FormatMoreItems(-4, "line"))
}

func TestHyperlinkFormat(t *testing.T) {
	t.Parallel()
	got := Hyperlink("https://example.com/a?b=1", "Example")
	require.Equal(t,
		"\x1b]8;;https://example.com/a?b=1\x1b\\Example\x1b]8;;\x1b\\",
		got,
	)
}

func TestHyperlinkPreservesText(t *testing.T) {
	t.Parallel()
	got := Hyperlink("https://example.com", "plain text")
	require.Contains(t, got, "plain text")
}
