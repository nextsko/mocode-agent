package theme

import "github.com/charmbracelet/x/ansi"

// DefaultEllipsis is the standard tail appended by truncation helpers.
const DefaultEllipsis = "…"

// TruncateToWidth truncates s to width display cells, appending tail when
// truncation happens. ANSI escape sequences are preserved intact and wide
// graphemes are never split.
func TruncateToWidth(s string, width int, tail string) string {
	return ansi.Truncate(s, width, tail)
}

// VisibleWidth returns the display width of s in terminal cells, ignoring
// ANSI escape sequences and counting wide graphemes (CJK, emoji) as two.
func VisibleWidth(s string) int {
	return ansi.StringWidth(s)
}

// LineBudget reserves room inside a framed block for the box borders, the
// tree gutter, and padding: max(24, width-6).
func LineBudget(width int) int {
	if b := width - 6; b > 24 {
		return b
	}
	return 24
}

// TitleBudget subtracts the meta suffix length from the line budget so a
// source title plus its "· (domain) · age" suffix fits one row, flooring
// at 12 cells.
func TitleBudget(lineBudget, metaLen int) int {
	if b := lineBudget - metaLen; b > 12 {
		return b
	}
	return 12
}

// WrapText wraps s to width display cells, breaking on word boundaries and
// hyphens; ANSI escape sequences are preserved.
func WrapText(s string, width int) string {
	return ansi.Wrap(s, width, "")
}
