// Package theme implements the Web Search TUI theme system: semantic
// colors, border and tree glyphs, status visualization, collapse hints,
// age/domain formatting, OSC 8 hyperlinks, and ANSI-aware width helpers.
package theme

import (
	"charm.land/lipgloss/v2"
)

// Theme is the rendering contract shared by all search TUI components.
// Implementations resolve semantic color tokens and glyph sets so callers
// stay palette-agnostic.
type Theme interface {
	// FG renders text with the token's foreground color.
	FG(color ThemeColor, text string) string
	// BG renders text with the token's background color.
	BG(color ThemeColor, text string) string
	// Dim renders text with the dim foreground token.
	Dim(text string) string
	// StyledSymbol resolves a tool symbol key, falling back to the given
	// glyph when the key is unknown, and styles it with the accent token.
	StyledSymbol(tool, fallback string) string
	// Box returns the box-drawing glyph set.
	Box() BoxGlyphs
	// Tree returns the tree-list glyph set.
	Tree() TreeGlyphs
	// Sep returns the separator glyph set.
	Sep() SepGlyphs
	// StatusIcon returns the status glyph for state, styled.
	StatusIcon(state State) string
	// StatusColor maps state to its semantic foreground token.
	StatusColor(state State) ThemeColor
	// BorderColor maps state to its semantic border token.
	BorderColor(state State) ThemeColor
}

// DefaultTheme is the palette-backed Theme used by the dark, light, and
// high-contrast variants. A nil palette entry means "terminal default":
// the text renders without any color escape.
type DefaultTheme struct {
	colors palette
	tools  map[string]string
}

// Compile-time checks that every implementation satisfies Theme.
var (
	_ Theme = (*DefaultTheme)(nil)
	_ Theme = NoColorTheme{}
)

// NewDefaultTheme returns the default theme, currently the dark palette.
func NewDefaultTheme() Theme {
	return NewDefaultDarkTheme()
}

// NewDefaultDarkTheme returns the dark palette theme.
func NewDefaultDarkTheme() Theme {
	return &DefaultTheme{colors: darkPalette(), tools: defaultToolSymbols}
}

// NewDefaultLightTheme returns the light palette theme.
func NewDefaultLightTheme() Theme {
	return &DefaultTheme{colors: lightPalette(), tools: defaultToolSymbols}
}

// NewHighContrastTheme returns a maximally saturated palette theme for
// accessibility.
func NewHighContrastTheme() Theme {
	return &DefaultTheme{colors: highContrastPalette(), tools: defaultToolSymbols}
}

// FG implements Theme.
func (t *DefaultTheme) FG(c ThemeColor, text string) string {
	col, ok := t.colors[c]
	if !ok || col == nil {
		return text
	}
	return lipgloss.NewStyle().Foreground(col).Render(text)
}

// BG implements Theme.
func (t *DefaultTheme) BG(c ThemeColor, text string) string {
	col, ok := t.colors[c]
	if !ok || col == nil {
		return text
	}
	return lipgloss.NewStyle().Background(col).Render(text)
}

// Dim implements Theme.
func (t *DefaultTheme) Dim(text string) string {
	return t.FG(ColorDim, text)
}

// StyledSymbol implements Theme.
func (t *DefaultTheme) StyledSymbol(tool, fallback string) string {
	symbol, ok := t.tools[tool]
	if !ok || symbol == "" {
		symbol = fallback
	}
	return t.FG(ColorAccent, symbol)
}

// Box implements Theme.
func (t *DefaultTheme) Box() BoxGlyphs {
	return DefaultBoxGlyphs
}

// Tree implements Theme.
func (t *DefaultTheme) Tree() TreeGlyphs {
	return DefaultTreeGlyphs
}

// Sep implements Theme.
func (t *DefaultTheme) Sep() SepGlyphs {
	return DefaultSepGlyphs
}

// StatusIcon implements Theme.
func (t *DefaultTheme) StatusIcon(state State) string {
	return t.FG(StatusColor(state), StatusIcon(state))
}

// StatusColor implements Theme.
func (t *DefaultTheme) StatusColor(state State) ThemeColor {
	return StatusColor(state)
}

// BorderColor implements Theme.
func (t *DefaultTheme) BorderColor(state State) ThemeColor {
	return BorderColor(state)
}

// NoColorTheme renders plain text without any ANSI styling, for pipes,
// logs, and NO_COLOR environments. Glyph sets and state mappings are kept
// so layout and semantics stay identical to the styled themes.
type NoColorTheme struct{}

// NewNoColorTheme returns the plain-text theme.
func NewNoColorTheme() Theme {
	return NoColorTheme{}
}

// FG implements Theme.
func (NoColorTheme) FG(_ ThemeColor, text string) string {
	return text
}

// BG implements Theme.
func (NoColorTheme) BG(_ ThemeColor, text string) string {
	return text
}

// Dim implements Theme.
func (NoColorTheme) Dim(text string) string {
	return text
}

// StyledSymbol implements Theme. It resolves the symbol from the same
// table as DefaultTheme but returns it unstyled.
func (NoColorTheme) StyledSymbol(tool, fallback string) string {
	if symbol, ok := defaultToolSymbols[tool]; ok && symbol != "" {
		return symbol
	}
	return fallback
}

// Box implements Theme.
func (NoColorTheme) Box() BoxGlyphs {
	return DefaultBoxGlyphs
}

// Tree implements Theme.
func (NoColorTheme) Tree() TreeGlyphs {
	return DefaultTreeGlyphs
}

// Sep implements Theme.
func (NoColorTheme) Sep() SepGlyphs {
	return DefaultSepGlyphs
}

// StatusIcon implements Theme.
func (NoColorTheme) StatusIcon(state State) string {
	return StatusIcon(state)
}

// StatusColor implements Theme.
func (NoColorTheme) StatusColor(state State) ThemeColor {
	return StatusColor(state)
}

// BorderColor implements Theme.
func (NoColorTheme) BorderColor(state State) ThemeColor {
	return BorderColor(state)
}
