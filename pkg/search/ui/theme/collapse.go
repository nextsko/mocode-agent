package theme

import "fmt"

// MaxCollapsedItems is the number of list items shown in a collapsed block
// before the remaining items are hidden behind a summary line.
const MaxCollapsedItems = 8

// FormatExpandHint returns the collapse affordance appended to a block
// header: "▴ collapse" when expanded, "▾ expand" when collapsed with
// hidden items remaining, and "" when collapsed with nothing more to show.
func FormatExpandHint(expanded, hasMore bool) string {
	if expanded {
		return "▴ collapse"
	}
	if !hasMore {
		return ""
	}
	return "▾ expand"
}

// FormatMoreItems renders the trailing "… N more <itemType>" summary line
// for truncated lists, pluralizing itemType like the TS source.
func FormatMoreItems(remaining int, itemType string) string {
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("… %d more %s", remaining, pluralize(itemType, remaining))
}
