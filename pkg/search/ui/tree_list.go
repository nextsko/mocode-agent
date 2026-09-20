// Tree list rendering with collapse/expand semantics, mirroring the TS
// renderTreeList: dim branch/last prefixes, head or tail truncation, a
// "… N more" summary row when collapsed, and a "▴ collapse" tail row when
// expanded beyond the collapsed budget.
package ui

import (
	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// TreeItem is one row of a TreeList.
type TreeItem struct {
	// Title is the plain item headline; it is accent-colored, truncated
	// to the title budget, and wrapped in an OSC 8 hyperlink when Link
	// is set.
	Title string
	// Meta is a pre-styled suffix appended after the title separated by
	// one space (e.g. "(example.com) · 2d ago"). It may carry ANSI
	// sequences; only its visible width feeds the title budget.
	Meta string
	// Link is the OSC 8 hyperlink URL, or "" for a plain title.
	Link string
}

// TruncateFrom selects which end of the list survives collapsing.
type TruncateFrom int

// Truncation directions.
const (
	// TruncateFromEnd keeps the head items and appends the "… N more"
	// summary row at the tail (the TS default).
	TruncateFromEnd TruncateFrom = iota
	// TruncateFromStart keeps the tail items and prepends the "… N more"
	// summary row at the head.
	TruncateFromStart
)

// TreeList renders a flat list of items as tree rows.
//
// Collapsed rendering shows at most MaxCollapsedItems rows: when the list
// is longer, the first MaxCollapsedItems-1 items (or last, for
// TruncateFromStart) are shown plus one "… N more <itemType>" summary
// row. Expanded rendering shows every item and, when collapsing would
// hide rows, one "▴ collapse" tail row.
type TreeList struct {
	theme        theme.Theme
	maxCollapsed int
	itemType     string
	truncateFrom TruncateFrom
}

// TreeListOption customizes a TreeList at construction.
type TreeListOption func(*TreeList)

// WithMaxCollapsedItems bounds the collapsed row budget (items plus the
// summary row). Values below 1 fall back to theme.MaxCollapsedItems.
func WithMaxCollapsedItems(n int) TreeListOption {
	return func(tl *TreeList) {
		if n >= 1 {
			tl.maxCollapsed = n
		}
	}
}

// WithTreeItemType names the item type used in "… N more" summaries.
func WithTreeItemType(itemType string) TreeListOption {
	return func(tl *TreeList) {
		if itemType != "" {
			tl.itemType = itemType
		}
	}
}

// WithTruncateFrom selects the collapse direction.
func WithTruncateFrom(from TruncateFrom) TreeListOption {
	return func(tl *TreeList) { tl.truncateFrom = from }
}

// NewTreeList returns a tree renderer bound to t with the default budget
// of theme.MaxCollapsedItems and the "source" item type.
func NewTreeList(t theme.Theme, opts ...TreeListOption) *TreeList {
	tl := &TreeList{
		theme:        t,
		maxCollapsed: theme.MaxCollapsedItems,
		itemType:     "source",
		truncateFrom: TruncateFromEnd,
	}
	for _, opt := range opts {
		opt(tl)
	}
	return tl
}

// RenderItems returns one row per visible item (plus the summary or
// collapse-hint row). width is the enclosing block's outer width; it
// feeds theme.LineBudget for title truncation.
func (tl *TreeList) RenderItems(items []TreeItem, expanded bool, width int) []string {
	tree := tl.theme.Tree()
	branchPrefix := tl.theme.Dim(tree.Branch) + " "
	lastPrefix := tl.theme.Dim(tree.Last) + " "

	renderRow := func(item TreeItem) string {
		metaSuffix := ""
		if item.Meta != "" {
			metaSuffix = " " + item.Meta
		}
		lineBudget := theme.LineBudget(width)
		titleBudget := theme.TitleBudget(lineBudget, theme.VisibleWidth(metaSuffix))
		title := theme.TruncateToWidth(item.Title, titleBudget, theme.DefaultEllipsis)
		styled := tl.theme.FG(theme.ColorAccent, title)
		if item.Link != "" {
			styled = theme.Hyperlink(item.Link, styled)
		}
		return styled + metaSuffix
	}

	var lines []string
	switch {
	case len(items) == 0:
		return lines

	case expanded:
		for _, item := range items[:len(items)-1] {
			lines = append(lines, branchPrefix+renderRow(item))
		}
		last := renderRow(items[len(items)-1])
		if len(items) > tl.maxCollapsed {
			lines = append(lines,
				branchPrefix+last,
				lastPrefix+tl.theme.FG(theme.ColorMuted,
					theme.FormatExpandHint(true, true)))
		} else {
			lines = append(lines, lastPrefix+last)
		}
		return lines

	case len(items) <= tl.maxCollapsed:
		for _, item := range items[:len(items)-1] {
			lines = append(lines, branchPrefix+renderRow(item))
		}
		return append(lines, lastPrefix+renderRow(items[len(items)-1]))
	}

	// Collapsed with more rows than the budget: show maxCollapsed-1
	// items plus one summary row, honoring the truncation direction.
	shown := tl.maxCollapsed - 1
	remaining := len(items) - shown
	summary := tl.theme.FG(theme.ColorMuted,
		theme.FormatMoreItems(remaining, tl.itemType))

	if tl.truncateFrom == TruncateFromStart {
		lines = append(lines, branchPrefix+summary)
		tail := items[len(items)-shown:]
		for _, item := range tail[:len(tail)-1] {
			lines = append(lines, branchPrefix+renderRow(item))
		}
		return append(lines, lastPrefix+renderRow(tail[len(tail)-1]))
	}
	for _, item := range items[:shown] {
		lines = append(lines, branchPrefix+renderRow(item))
	}
	return append(lines, lastPrefix+summary)
}
