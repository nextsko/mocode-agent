// Status line assembly for the Web Search block header: a thin wrapper
// over theme.RenderStatusLine plus an icon-override variant and the
// "N sources" count formatter.
package ui

import (
	"strconv"
	"strings"

	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// RenderStatusLine assembles the standard status line via
// theme.RenderStatusLine: state icon, accent title, muted description,
// dim meta.
func RenderStatusLine(t theme.Theme, state theme.State, title, desc, meta string) string {
	return theme.RenderStatusLine(t, state, title, desc, meta)
}

// RenderStatusLineWithIcon mirrors theme.RenderStatusLine but substitutes
// the state icon with a caller-supplied pre-styled icon (e.g. the
// tool.webSearch magnifier that replaces the success check mark).
func RenderStatusLineWithIcon(t theme.Theme, icon string, title, desc, meta string) string {
	var b strings.Builder
	if icon != "" {
		b.WriteString(icon)
		b.WriteByte(' ')
	}
	b.WriteString(t.FG(theme.ColorAccent, flattenLine(title)))
	if d := flattenLine(desc); d != "" {
		b.WriteString(": ")
		b.WriteString(t.FG(theme.ColorMuted, d))
	}
	if m := flattenLine(meta); m != "" {
		b.WriteByte(' ')
		b.WriteString(t.FG(theme.ColorDim, m))
	}
	return theme.TruncateToWidth(b.String(), theme.StatusLineMaxWidth, theme.DefaultEllipsis)
}

// FormatCount renders "N <label>" with TS-compatible pluralization
// ("5 sources", "1 source", "2 entries").
func FormatCount(label string, count int) string {
	return strconv.Itoa(count) + " " + pluralizeCount(label, count)
}

// pluralizeCount pluralizes label for count with the same rules as the
// theme package: 1 keeps the label; ch/sh/s/x/z endings add "es"; a
// consonant+y ending becomes "ies"; anything else adds "s".
func pluralizeCount(label string, count int) string {
	if count == 1 {
		return label
	}
	lower := strings.ToLower(label)
	switch {
	case strings.HasSuffix(lower, "ch"),
		strings.HasSuffix(lower, "sh"),
		strings.HasSuffix(lower, "s"),
		strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "z"):
		return label + "es"
	case len(lower) >= 2 && !isVowelByte(lower[len(lower)-2]) &&
		strings.HasSuffix(lower, "y"):
		return label[:len(label)-1] + "ies"
	default:
		return label + "s"
	}
}

// isVowelByte reports whether the ASCII byte b is a vowel.
func isVowelByte(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}

// flattenLine collapses CR/LF runs to single spaces so a stray newline
// cannot expand the status line into multiple rows.
func flattenLine(text string) string {
	if !strings.ContainsAny(text, "\r\n") {
		return text
	}
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(text)
}
