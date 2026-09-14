package completions

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

func TestSplitMatchIndexes(t *testing.T) {
	// Filter text: "/model switch model" — cmdLen = len("/model") = 6.
	m := fuzzy.Match{MatchedIndexes: []int{1, 3, 7, 9}} // hits in cmd, sep, desc
	cmd, desc := splitMatchIndexes(m, 6)
	require.Equal(t, map[int]bool{1: true, 3: true}, cmd)
	require.Equal(t, map[int]bool{0: true, 2: true}, desc, "sep (6) skipped, desc indexes rebased")
}

func TestHighlightRunes(t *testing.T) {
	out := highlightRunes("abc", map[int]bool{1: true}, lipgloss.NewStyle(), lipgloss.NewStyle(), false)
	require.Contains(t, out, "\x1b[1mb\x1b[22m", "hit rune bolded")
	require.True(t, strings.HasPrefix(out, "a"))
	require.True(t, strings.HasSuffix(out, "c"))

	// No hits → untouched.
	require.Equal(t, "abc", highlightRunes("abc", nil, lipgloss.NewStyle(), lipgloss.NewStyle(), false))

	// With styled=true the base style must still be applied, because callers
	// hand us plain text and rely on us for the colouring. An early return here
	// would silently render the column unstyled.
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	require.Equal(t, styled.Render("abc"), highlightRunes("abc", nil, styled, styled, true))
}

// sgrResidue matches an ANSI colour code whose leading ESC byte was lost.
//
// Seeing one of these in *stripped* output always means the renderer split an
// escape sequence and leaked its parameters as literal text — the "[38;2;…m"
// garbage that used to show up in the slash popup. The three alternatives
// cover the shapes actually observed in the bug report: "[38;2;104;255;214m",
// ";221m" (a split that starts mid-list) and "8;2;104;255;214m" (a split that
// ate the leading "[3"). A bare "\d+m" is deliberately not matched, so digits
// in a real description cannot trip it; internal/ui/slash/sanitize.go cleans up
// the same shapes when they arrive from user-authored files.
var sgrResidue = regexp.MustCompile(`\[[0-9;]*m|;[0-9]{1,3}(?:;[0-9]{1,3})*m|[0-9]{1,3}(?:;[0-9]{1,3})+m`)

// leadingSGR extracts the escape sequence lipgloss writes before a string.
var leadingSGR = regexp.MustCompile(`^\x1b\[[0-9;]*m`)

const testRowWidth = 60

// newSlashItem builds an item the way the popup does, with the real theme.
func newSlashItem(t *testing.T, cmd, desc, query string, focused bool) *SlashCompletionItem {
	t.Helper()

	st := styles.ThemeForProvider("")
	item := NewSlashCompletionItem(SlashCompletionValue{Command: cmd, Desc: desc}, &st)
	item.SetFocused(focused)

	if query != "" {
		matches := fuzzy.Find(query, []string{cmd + " " + desc})
		require.NotEmpty(t, matches, "query %q must match %q", query, cmd)
		item.SetMatch(matches[0])
	}
	return item
}

// TestSlashCompletionItem_Render_NoAnsiResidue is the regression test for the
// literal colour codes in the slash popup. The commands and queries are taken
// from the original bug report, and each one used to render a different
// fragment of escape sequence ("[38;2;104;255;214m", ";221m", …).
func TestSlashCompletionItem_Render_NoAnsiResidue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cmd   string
		desc  string
		query string
	}{
		{"hit in label", "/agents", "Switch agent mode", "a"},
		{"hit late in label", "/notifications", "Toggle desktop notifications", "not"},
		{"hit at end of label", "/wechat", "Manage WeChat accounts", "we"},
		{"hit at start of desc", "/copy", "Copy the last assistant reply to clipboard", "a"},
		{"hit only in desc", "/copy", "Copy the last assistant reply to clipboard", "reply"},
		{"hit at start of desc after separator", "/init", "Initialize project", "in"},
		{"hit in both columns", "/plan", "Switch to SK Plan mode", "pn"},
		{"no query", "/agents", "Switch agent mode", ""},
		{"multibyte desc", "/think", "Enable thinking mode · 思考", "think"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, focused := range []bool{false, true} {
				out := newSlashItem(t, tt.cmd, tt.desc, tt.query, focused).Render(testRowWidth)
				visible := ansi.Strip(out)

				assert.NotRegexp(t, sgrResidue, visible,
					"escape parameters leaked as literal text (focused=%v): %q", focused, visible)
				assert.NotContains(t, visible, "\x1b",
					"stripped output must contain no escape bytes (focused=%v)", focused)
				assert.Equal(t, testRowWidth, lipgloss.Width(out),
					"row must stay exactly %d cells wide (focused=%v): %q",
					testRowWidth, focused, visible)

				// Exact cross-check, independent of the regex: highlighting is
				// decoration, so a matched row must show the same characters in
				// the same places as the same row with no query.
				baseline := newSlashItem(t, tt.cmd, tt.desc, "", focused).Render(testRowWidth)
				assert.Equal(t, ansi.Strip(baseline), visible,
					"query %q altered the visible row (focused=%v)", tt.query, focused)
			}
		})
	}
}

// TestSlashCompletionItem_Render_HighlightDoesNotChangeLayout pins the
// invariant behind the bug: highlighting is decoration, so it must never alter
// the visible text or the alignment. The broken version widened rows by 5–18
// cells, which is what pushed the popup into wrapping.
func TestSlashCompletionItem_Render_HighlightDoesNotChangeLayout(t *testing.T) {
	t.Parallel()

	plain := newSlashItem(t, "/agents", "Switch agent mode", "", false).Render(testRowWidth)

	for _, query := range []string{"a", "ag", "agents", "mode"} {
		highlighted := newSlashItem(t, "/agents", "Switch agent mode", query, false).Render(testRowWidth)
		assert.Equal(t, ansi.Strip(plain), ansi.Strip(highlighted),
			"query %q changed the visible text", query)
		assert.Equal(t, lipgloss.Width(plain), lipgloss.Width(highlighted),
			"query %q changed the row width", query)
	}
}

// TestSlashCompletionItem_Render_KeepsThemeStyling guards the other direction:
// highlighting now runs on plain text, so it owns applying the base style and
// must not leave a column unstyled when there is nothing to emphasize.
func TestSlashCompletionItem_Render_KeepsThemeStyling(t *testing.T) {
	t.Parallel()

	st := styles.ThemeForProvider("")
	labelPrefix := leadingSGR.FindString(st.Dialog.TitleText.Render("x"))
	descPrefix := leadingSGR.FindString(st.Dialog.ListItem.InfoBlurred.Render("x"))
	require.True(t, labelPrefix != "" || descPrefix != "",
		"theme styles nothing, so this test cannot verify anything")

	for _, query := range []string{"", "a"} {
		out := newSlashItem(t, "/agents", "Switch agent mode", query, false).Render(testRowWidth)

		if labelPrefix != "" {
			assert.Contains(t, out, labelPrefix, "label styling missing (query=%q)", query)
		}
		if descPrefix != "" {
			assert.Contains(t, out, descPrefix, "description styling missing (query=%q)", query)
		}
	}
}

// TestSlashCompletionItem_Render_EmphasizesHits makes sure the fix did not
// trade the residue bug for silently dropping the highlight itself.
func TestSlashCompletionItem_Render_EmphasizesHits(t *testing.T) {
	t.Parallel()

	st := styles.ThemeForProvider("")

	// Label hit: the matched rune is its own segment.
	out := newSlashItem(t, "/agents", "Switch agent mode", "a", false).Render(testRowWidth)
	assert.Contains(t, out, st.Completions.Match.Render("a"), "matched label rune must be emphasized")

	// Description-only hit: the label keeps its plain styling, the desc rune
	// is emphasized.
	out = newSlashItem(t, "/agents", "Switch agent mode", "mode", false).Render(testRowWidth)
	assert.Contains(t, out, st.Completions.Match.Render("mode"),
		"matched description run must be emphasized")
}

// TestSlashCompletionItem_Render_CachedResultIsStable covers the per-width
// cache: a hit row and a second identical render must agree.
func TestSlashCompletionItem_Render_CachedResultIsStable(t *testing.T) {
	t.Parallel()

	item := newSlashItem(t, "/agents", "Switch agent mode", "a", false)
	first := item.Render(testRowWidth)
	second := item.Render(testRowWidth)

	assert.Equal(t, first, second)
	assert.NotRegexp(t, sgrResidue, ansi.Strip(second))
}
