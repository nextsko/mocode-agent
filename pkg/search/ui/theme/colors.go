package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// ThemeColor is a semantic color token resolved by a Theme implementation.
// Callers reference tokens, never concrete colors, so palettes can vary.
type ThemeColor string

// Semantic color tokens shared across all Theme implementations.
const (
	ColorAccent      ThemeColor = "accent"
	ColorText        ThemeColor = "text"
	ColorDim         ThemeColor = "dim"
	ColorMuted       ThemeColor = "muted"
	ColorError       ThemeColor = "error"
	ColorWarning     ThemeColor = "warning"
	ColorSuccess     ThemeColor = "success"
	ColorToolTitle   ThemeColor = "toolTitle"
	ColorBorderMuted ThemeColor = "borderMuted"
)

// palette maps semantic tokens to concrete colors. A nil entry means
// "terminal default": FG/BG render the text without any color escape,
// mirroring the empty-string convention of the TS theme JSON schema.
type palette map[ThemeColor]color.Color

// hex parses a hex color string via lipgloss.
func hex(s string) color.Color {
	return lipgloss.Color(s)
}

// darkPalette mirrors the oh-my-pi "dark" built-in theme. Text and
// toolTitle intentionally use the terminal default foreground.
func darkPalette() palette {
	return palette{
		ColorAccent:      hex("#febc38"),
		ColorText:        nil,
		ColorDim:         hex("#5f6673"),
		ColorMuted:       hex("#777d88"),
		ColorError:       hex("#fc3a4b"),
		ColorWarning:     hex("#e4c00f"),
		ColorSuccess:     hex("#89d281"),
		ColorToolTitle:   nil,
		ColorBorderMuted: hex("#3d424a"),
	}
}

// lightPalette mirrors the oh-my-pi "light" built-in theme.
func lightPalette() palette {
	return palette{
		ColorAccent:      hex("#5a8080"),
		ColorText:        nil,
		ColorDim:         hex("#767676"),
		ColorMuted:       hex("#6c6c6c"),
		ColorError:       hex("#aa5555"),
		ColorWarning:     hex("#9a7326"),
		ColorSuccess:     hex("#588458"),
		ColorToolTitle:   nil,
		ColorBorderMuted: hex("#b0b0b0"),
	}
}

// highContrastPalette uses maximally separated colors on the assumption of
// a dark terminal background.
func highContrastPalette() palette {
	return palette{
		ColorAccent:      hex("#00ffff"),
		ColorText:        hex("#ffffff"),
		ColorDim:         hex("#e0e0e0"),
		ColorMuted:       hex("#c0c0c0"),
		ColorError:       hex("#ff0000"),
		ColorWarning:     hex("#ffff00"),
		ColorSuccess:     hex("#00ff00"),
		ColorToolTitle:   hex("#ffffff"),
		ColorBorderMuted: hex("#a0a0a0"),
	}
}
