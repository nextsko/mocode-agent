package theme

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockTheme is a test-built Theme that wraps styled fragments in sentinel
// markers so composition can be asserted exactly.
type mockTheme struct{}

func (mockTheme) FG(c ThemeColor, text string) string {
	return "<" + string(c) + ":" + text + ">"
}

func (mockTheme) BG(c ThemeColor, text string) string {
	return "<bg:" + string(c) + ":" + text + ">"
}

func (mockTheme) Dim(text string) string { return "<dim:" + text + ">" }

func (mockTheme) StyledSymbol(tool, fallback string) string {
	return "<sym:" + tool + "|" + fallback + ">"
}

func (mockTheme) Box() BoxGlyphs   { return DefaultBoxGlyphs }
func (mockTheme) Tree() TreeGlyphs { return DefaultTreeGlyphs }
func (mockTheme) Sep() SepGlyphs   { return DefaultSepGlyphs }

func (mockTheme) StatusIcon(state State) string {
	return StatusIcon(state)
}

func (mockTheme) StatusColor(state State) ThemeColor {
	return StatusColor(state)
}

func (mockTheme) BorderColor(state State) ThemeColor {
	return BorderColor(state)
}

func TestDefaultThemeFGAddsANSI(t *testing.T) {
	t.Parallel()
	dark := NewDefaultDarkTheme()
	out := dark.FG(ColorAccent, "hi")
	require.NotEqual(t, "hi", out)
	require.Contains(t, out, "hi")
	require.Contains(t, out, "\x1b[")
	require.Greater(t, len(out), len("hi"))
}

func TestDefaultThemeBGAddsANSI(t *testing.T) {
	t.Parallel()
	dark := NewDefaultDarkTheme()
	out := dark.BG(ColorError, "boom")
	require.Contains(t, out, "boom")
	require.Contains(t, out, "\x1b[")
}

func TestDefaultThemeTerminalDefaultTokens(t *testing.T) {
	t.Parallel()
	// The TS dark/light themes leave text and toolTitle empty so the
	// terminal default foreground is used; dim is a real color.
	for _, theme := range []Theme{NewDefaultDarkTheme(), NewDefaultLightTheme()} {
		require.Equal(t, "plain", theme.FG(ColorText, "plain"))
		require.Equal(t, "plain", theme.FG(ColorToolTitle, "plain"))
		require.NotEqual(t, "plain", theme.Dim("plain"))
	}
}

func TestDarkAndLightPalettesDiffer(t *testing.T) {
	t.Parallel()
	dark := NewDefaultDarkTheme()
	light := NewDefaultLightTheme()
	require.NotEqual(t,
		dark.FG(ColorAccent, "x"),
		light.FG(ColorAccent, "x"),
	)
	require.NotEqual(t,
		dark.FG(ColorDim, "x"),
		light.FG(ColorDim, "x"),
	)
}

func TestHighContrastThemeStyles(t *testing.T) {
	t.Parallel()
	hc := NewHighContrastTheme()
	for _, token := range []ThemeColor{
		ColorText, ColorDim, ColorMuted, ColorError, ColorWarning,
		ColorSuccess, ColorToolTitle, ColorBorderMuted, ColorAccent,
	} {
		require.NotEqual(t, "x", hc.FG(token, "x"), "token %s", token)
	}
}

func TestNoColorThemeReturnsPlainText(t *testing.T) {
	t.Parallel()
	nc := NewNoColorTheme()
	require.Equal(t, "hi", nc.FG(ColorAccent, "hi"))
	require.Equal(t, "hi", nc.BG(ColorAccent, "hi"))
	require.Equal(t, "hi", nc.Dim("hi"))
	require.Equal(t, "fallback", nc.StyledSymbol("tool.unknown", "fallback"))
	require.Equal(t, "⌕", nc.StyledSymbol("tool.webSearch", "fallback"))
	require.NotContains(t, nc.FG(ColorError, "x"), "\x1b")
}

func TestNewDefaultThemeIsDark(t *testing.T) {
	t.Parallel()
	def := NewDefaultTheme()
	dark := NewDefaultDarkTheme()
	require.Equal(t, dark.FG(ColorAccent, "x"), def.FG(ColorAccent, "x"))
}

func TestGlyphSetsShared(t *testing.T) {
	t.Parallel()
	for _, theme := range []Theme{
		NewDefaultDarkTheme(),
		NewDefaultLightTheme(),
		NewHighContrastTheme(),
		NewNoColorTheme(),
		mockTheme{},
	} {
		box := theme.Box()
		require.Equal(t, "─", box.Horizontal)
		require.Equal(t, "│", box.Vertical)
		require.Equal(t, "┌", box.TopLeft)
		require.Equal(t, "┐", box.TopRight)
		require.Equal(t, "└", box.BottomLeft)
		require.Equal(t, "┘", box.BottomRight)
		require.Equal(t, "├", box.TeeRight)
		require.Equal(t, "┤", box.TeeLeft)

		tree := theme.Tree()
		require.Equal(t, "├─", tree.Branch)
		require.Equal(t, "└─", tree.Last)

		require.Equal(t, "·", theme.Sep().Dot)
	}
}

func TestStatusMappings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		state  State
		glyph  string
		fg     ThemeColor
		border ThemeColor
	}{
		{StatePending, "◐", ColorMuted, ColorAccent},
		{StateRunning, "◑", ColorAccent, ColorAccent},
		{StateSuccess, "✓", ColorSuccess, ColorBorderMuted},
		{StateWarning, "⚠", ColorWarning, ColorWarning},
		{StateError, "✗", ColorError, ColorError},
	}
	for _, tc := range cases {
		require.Equal(t, tc.glyph, StatusIcon(tc.state), tc.state)
		require.Equal(t, tc.fg, StatusColor(tc.state), tc.state)
		require.Equal(t, tc.border, BorderColor(tc.state), tc.state)
	}
	// Success must be intentionally weakened, not just different.
	require.Equal(t, ColorBorderMuted, BorderColor(StateSuccess))
}

func TestThemeStatusIconStyling(t *testing.T) {
	t.Parallel()
	dark := NewDefaultDarkTheme()
	styled := dark.StatusIcon(StateError)
	require.NotEqual(t, "✗", styled)
	require.Contains(t, styled, "✗")
	require.Contains(t, styled, "\x1b[")

	nc := NewNoColorTheme()
	require.Equal(t, "✗", nc.StatusIcon(StateError))
	require.Equal(t, ColorError, nc.StatusColor(StateError))
	require.Equal(t, ColorError, nc.BorderColor(StateError))
}

func TestStyledSymbol(t *testing.T) {
	t.Parallel()
	dark := NewDefaultDarkTheme()
	known := dark.StyledSymbol("tool.webSearch", "*")
	require.Contains(t, known, "⌕")
	require.Contains(t, known, "\x1b[")

	fallback := dark.StyledSymbol("tool.missing", "?")
	require.Contains(t, fallback, "?")
	require.Contains(t, fallback, "\x1b[")
}

func TestRenderStatusLineComposition(t *testing.T) {
	t.Parallel()
	got := RenderStatusLine(mockTheme{}, StateSuccess,
		"Web Search", "Tavily", "3 sources")
	require.Equal(t,
		"✓ <accent:Web Search>: <muted:Tavily> <dim:3 sources>",
		got,
	)
}

func TestRenderStatusLineOmitsEmptyFragments(t *testing.T) {
	t.Parallel()
	got := RenderStatusLine(mockTheme{}, StatePending, "Web Search", "", "")
	require.Equal(t, "◐ <accent:Web Search>", got)
}

func TestRenderStatusLinePlainTheme(t *testing.T) {
	t.Parallel()
	got := RenderStatusLine(NewNoColorTheme(), StateRunning,
		"Web Search", "hello world", "3 sources")
	require.Equal(t, "◑ Web Search: hello world 3 sources", got)
	require.NotContains(t, got, "\x1b")
}

func TestRenderStatusLineFlattensNewlines(t *testing.T) {
	t.Parallel()
	got := RenderStatusLine(NewNoColorTheme(), StateSuccess,
		"Web\r\nSearch", "de\nsc", "me\rt")
	require.NotContains(t, got, "\n")
	require.NotContains(t, got, "\r")
	require.Equal(t, "✓ Web Search: de sc me t", got)
}

func TestRenderStatusLineTruncatesToMaxWidth(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 200)
	got := RenderStatusLine(NewNoColorTheme(), StateSuccess, long, "", "")
	require.LessOrEqual(t, VisibleWidth(got), StatusLineMaxWidth)
	require.True(t, strings.HasSuffix(got, DefaultEllipsis))
}
