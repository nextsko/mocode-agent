package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// mockTheme wraps styled fragments in sentinel markers so composition can
// be asserted exactly (same trick as the theme package tests).
type mockTheme struct{}

func (mockTheme) FG(c theme.ThemeColor, text string) string {
	return "<" + string(c) + ":" + text + ">"
}

func (mockTheme) BG(c theme.ThemeColor, text string) string {
	return "<bg:" + string(c) + ":" + text + ">"
}

func (mockTheme) Dim(text string) string { return "<dim:" + text + ">" }

func (mockTheme) StyledSymbol(tool, fallback string) string {
	return "<sym:" + tool + "|" + fallback + ">"
}

func (mockTheme) Box() theme.BoxGlyphs   { return theme.DefaultBoxGlyphs }
func (mockTheme) Tree() theme.TreeGlyphs { return theme.DefaultTreeGlyphs }
func (mockTheme) Sep() theme.SepGlyphs   { return theme.DefaultSepGlyphs }

func (mockTheme) StatusIcon(state theme.State) string {
	return theme.StatusIcon(state)
}

func (mockTheme) StatusColor(state theme.State) theme.ThemeColor {
	return theme.StatusColor(state)
}

func (mockTheme) BorderColor(state theme.State) theme.ThemeColor {
	return theme.BorderColor(state)
}

// dashes builds n horizontal box glyphs.
func dashes(n int) string { return strings.Repeat("─", n) }

// bar builds a mock-theme bar row: corner glyph + dash cap + embedded
// label (or plain fill) + closing glyph, all border-colored per token.
func bar(token, left, label, right string, lineWidth int) string {
	esc := func(s string) string { return "<" + token + ":" + s + ">" }
	leftWidth := 1 + barCapLen // corner glyph plus the dash cap
	leftGlyphs := left + dashes(barCapLen)
	if label == "" {
		fill := max(0, lineWidth-leftWidth-1)
		return esc(leftGlyphs) + esc(dashes(fill)) + esc(right)
	}
	rawLabel := " " + label + " "
	fill := max(0, lineWidth-leftWidth-len([]rune(rawLabel))-1)
	return esc(leftGlyphs) + rawLabel + esc(dashes(fill)) + esc(right)
}

// contentRow builds a mock-theme content row: vertical bars, one space of
// padding each side, and inner padded to the content width.
func testContentRow(token, inner string, lineWidth int) string {
	esc := func(s string) string { return "<" + token + ":" + s + ">" }
	contentWidth := max(0, lineWidth-2-2*contentPadding)
	pad := contentWidth - theme.VisibleWidth(inner)
	return esc("│") + " " + inner + strings.Repeat(" ", max(0, pad)) + " " + esc("│")
}

func TestOutputBlockFiveStateBorderColors(t *testing.T) {
	t.Parallel()
	cases := map[theme.State]string{
		theme.StatePending: "accent",
		theme.StateRunning: "accent",
		theme.StateSuccess: "borderMuted",
		theme.StateWarning: "warning",
		theme.StateError:   "error",
	}
	for state, token := range cases {
		block := NewOutputBlock(mockTheme{})
		lines := block.Render(OutputBlockOptions{
			Header:   "H",
			State:    state,
			Sections: []Section{{Lines: []string{"x"}}},
			Width:    24,
		})
		require.Len(t, lines, 3, "state %s", state)
		require.True(t, strings.HasPrefix(lines[0], "<"+token+":┌"),
			"state %s top: %q", state, lines[0])
		require.True(t, strings.HasPrefix(lines[len(lines)-1], "<"+token+":└"),
			"state %s bottom: %q", state, lines[len(lines)-1])
	}
}

func TestOutputBlockHeaderEmbeddedInTopBorder(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	lines := block.Render(OutputBlockOptions{Header: "H", Width: 20})
	require.Equal(t, bar("borderMuted", "┌", "H", "┐", 20), lines[0])
}

func TestOutputBlockHeaderMetaJoinedWithDot(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	lines := block.Render(OutputBlockOptions{Header: "H", HeaderMeta: "M", Width: 24})
	require.Contains(t, lines[0], " H · M ")
}

func TestOutputBlockSectionBarsAndContentRows(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	lines := block.Render(OutputBlockOptions{
		Sections: []Section{
			{Label: "A", Lines: []string{"hi"}},
			{Label: "B"},
		},
		Width: 24,
	})
	require.Len(t, lines, 5)
	require.Equal(t, bar("borderMuted", "├", "A", "┤", 24), lines[1])
	require.Equal(t, testContentRow("borderMuted", "hi", 24), lines[2])
	require.Equal(t, bar("borderMuted", "├", "B", "┤", 24), lines[3])
	require.Equal(t, bar("borderMuted", "└", "", "┘", 24), lines[4])
}

func TestOutputBlockSeparatorOnlyBetweenSections(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	lines := block.Render(OutputBlockOptions{
		Sections: []Section{
			{Lines: []string{"a"}},
			{Lines: []string{"b"}, Separator: true},
		},
		Width: 20,
	})
	// Rows: top, "a", plain divider, "b", bottom.
	require.Len(t, lines, 5)
	require.Equal(t, bar("borderMuted", "├", "", "┤", 20), lines[2])
}

func TestOutputBlockWrapsWideContent(t *testing.T) {
	t.Parallel()
	// Real wrap math needs real ANSI handling, so use the no-color theme.
	block := NewOutputBlock(theme.NewNoColorTheme())
	long := strings.Repeat("word ", 10)
	lines := block.Render(OutputBlockOptions{
		Sections: []Section{{Lines: []string{long}}},
		Width:    24,
	})
	content := lines[1 : len(lines)-1]
	require.Greater(t, len(content), 1, "should wrap into multiple rows")
	for _, line := range lines {
		require.Equal(t, 24, theme.VisibleWidth(line))
	}
	require.Contains(t, strings.Join(content, "\n"), "word")
}

func TestOutputBlockSplitsEmbeddedNewlines(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	lines := block.Render(OutputBlockOptions{
		Sections: []Section{{Lines: []string{"one\ntwo"}}},
		Width:    20,
	})
	require.Len(t, lines, 4)
	require.Contains(t, lines[1], "one")
	require.Contains(t, lines[2], "two")
}

func TestOutputBlockCacheHitOnSameOptions(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	opts := OutputBlockOptions{
		Header:   "H",
		Sections: []Section{{Label: "S", Lines: []string{"a"}}},
		Width:    30,
	}
	first := block.Render(opts)
	require.Equal(t, 0, block.CacheHits())
	require.Equal(t, 1, block.CacheMisses())

	second := block.Render(opts)
	require.Equal(t, 1, block.CacheHits())
	require.Equal(t, 1, block.CacheMisses())
	require.Equal(t, first, second)
}

func TestOutputBlockCacheKeyIncludesWidthStateAndContent(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	base := OutputBlockOptions{Sections: []Section{{Lines: []string{"a"}}}, Width: 30}
	block.Render(base)

	wider := base
	wider.Width = 31
	block.Render(wider)

	otherContent := base
	otherContent.Sections = []Section{{Lines: []string{"b"}}}
	block.Render(otherContent)

	otherState := base
	otherState.State = theme.StateError
	block.Render(otherState)

	require.Equal(t, 0, block.CacheHits())
	require.Equal(t, 4, block.CacheMisses())
}

func TestOutputBlockCacheEvictsAtCapacity(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	for i := 0; i < outputBlockCapacity+8; i++ {
		block.Render(OutputBlockOptions{
			Sections: []Section{{Lines: []string{"x"}}},
			Width:    20 + i,
		})
	}
	require.Equal(t, outputBlockCapacity, block.CacheSize())

	// The earliest renders were evicted: re-rendering them misses again.
	before := block.CacheMisses()
	block.Render(OutputBlockOptions{
		Sections: []Section{{Lines: []string{"x"}}},
		Width:    20,
	})
	require.Equal(t, before+1, block.CacheMisses())
}

func TestOutputBlockInvalidateClearsCache(t *testing.T) {
	t.Parallel()
	block := NewOutputBlock(mockTheme{})
	opts := OutputBlockOptions{Width: 20}
	block.Render(opts)
	require.Equal(t, 1, block.CacheSize())
	block.Invalidate()
	require.Equal(t, 0, block.CacheSize())

	misses := block.CacheMisses()
	block.Render(opts)
	require.Equal(t, misses+1, block.CacheMisses())
	require.Equal(t, 1, block.CacheSize())
}

func TestOutputBlockContentWidth(t *testing.T) {
	t.Parallel()
	require.Equal(t, 76, OutputBlockContentWidth(80))
	require.Equal(t, 16, OutputBlockContentWidth(20))
	require.Equal(t, 1, OutputBlockContentWidth(5))
	require.Equal(t, 1, OutputBlockContentWidth(0))
}
