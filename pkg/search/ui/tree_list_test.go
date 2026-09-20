package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// mkItems builds n titled tree items.
func mkItems(n int) []TreeItem {
	items := make([]TreeItem, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, TreeItem{
			Title: fmt.Sprintf("Item %d", i),
			Meta:  "<dim:(example.com)> · <muted:2d ago>",
		})
	}
	return items
}

func TestTreeListCollapsedWithinBudgetShowsAll(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{})
	lines := tl.RenderItems(mkItems(3), false, 80)
	require.Len(t, lines, 3)
	require.True(t, strings.HasPrefix(lines[0], "<dim:├─> "))
	require.True(t, strings.HasPrefix(lines[1], "<dim:├─> "))
	require.True(t, strings.HasPrefix(lines[2], "<dim:└─> "))
}

func TestTreeListCollapsedTruncatesWithMoreRow(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{})
	lines := tl.RenderItems(mkItems(10), false, 80)
	// 7 shown items + 1 summary row = 8 rows (the collapsed budget).
	require.Len(t, lines, theme.MaxCollapsedItems)
	for _, line := range lines[:7] {
		require.True(t, strings.HasPrefix(line, "<dim:├─> "), line)
		require.Contains(t, line, "Item ")
	}
	require.Equal(t, "<dim:└─> <muted:… 3 more sources>", lines[7])
	require.Contains(t, lines[0], "Item 1")
	require.Contains(t, lines[6], "Item 7")
}

func TestTreeListExpandedShowsAllPlusCollapseHint(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{})
	lines := tl.RenderItems(mkItems(10), true, 80)
	require.Len(t, lines, 11)
	// The last item keeps the branch prefix; the hint takes └─.
	require.True(t, strings.HasPrefix(lines[9], "<dim:├─> "))
	require.Contains(t, lines[9], "Item 10")
	require.Equal(t, "<dim:└─> <muted:▴ collapse>", lines[10])
}

func TestTreeListExpandedShortListHasNoHint(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{})
	lines := tl.RenderItems(mkItems(3), true, 80)
	require.Len(t, lines, 3)
	require.True(t, strings.HasPrefix(lines[2], "<dim:└─> "))
	require.NotContains(t, strings.Join(lines, "\n"), "collapse")
}

func TestTreeListTruncateFromStartKeepsTail(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{}, WithTruncateFrom(TruncateFromStart))
	lines := tl.RenderItems(mkItems(10), false, 80)
	require.Len(t, lines, 8)
	require.Equal(t, "<dim:├─> <muted:… 3 more sources>", lines[0])
	require.Contains(t, lines[1], "Item 4")
	require.True(t, strings.HasPrefix(lines[7], "<dim:└─> "))
	require.Contains(t, lines[7], "Item 10")
}

func TestTreeListCustomBudgetAndItemType(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{},
		WithMaxCollapsedItems(3),
		WithTreeItemType("entry"))
	lines := tl.RenderItems(mkItems(5), false, 80)
	require.Len(t, lines, 3)
	require.Equal(t, "<dim:└─> <muted:… 3 more entries>", lines[2])
}

func TestTreeListHyperlinkWrapsTitle(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{})
	lines := tl.RenderItems([]TreeItem{{
		Title: "Example",
		Link:  "https://example.com",
	}}, false, 80)
	require.Len(t, lines, 1)
	require.Contains(t, lines[0],
		"\x1b]8;;https://example.com\x1b\\<accent:Example>\x1b]8;;\x1b\\")
}

func TestTreeListTruncatesLongTitles(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(theme.NewNoColorTheme())
	long := strings.Repeat("abcdefghij", 30) // 300 cells
	lines := tl.RenderItems([]TreeItem{{
		Title: long,
		Meta:  "(example.com) · 2d ago",
	}}, false, 40)
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "…")
	// lineBudget = max(24, 40-6) = 34; meta suffix is 23 cells wide, so
	// the title gets TitleBudget(34, 24) = 12 cells (floor) + prefix 3.
	require.LessOrEqual(t, theme.VisibleWidth(lines[0]), 3+12+24)
}

func TestTreeListEmptyItems(t *testing.T) {
	t.Parallel()
	tl := NewTreeList(mockTheme{})
	require.Empty(t, tl.RenderItems(nil, false, 80))
	require.Empty(t, tl.RenderItems(nil, true, 80))
}
