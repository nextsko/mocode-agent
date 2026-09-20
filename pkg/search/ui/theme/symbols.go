package theme

// BoxGlyphs is the U+2500-series box-drawing glyph set used for framed
// output blocks.
type BoxGlyphs struct {
	Horizontal  string // ─
	Vertical    string // │
	TopLeft     string // ┌
	TopRight    string // ┐
	BottomLeft  string // └
	BottomRight string // ┘
	TeeRight    string // ├
	TeeLeft     string // ┤
}

// TreeGlyphs is the glyph set for tree-list item prefixes.
type TreeGlyphs struct {
	Branch string // ├─
	Last   string // └─
}

// SepGlyphs is the glyph set for inline separators.
type SepGlyphs struct {
	Dot string // ·
}

// DefaultBoxGlyphs is the standard U+2500 box-drawing set, shared by every
// Theme implementation.
var DefaultBoxGlyphs = BoxGlyphs{
	Horizontal:  "─",
	Vertical:    "│",
	TopLeft:     "┌",
	TopRight:    "┐",
	BottomLeft:  "└",
	BottomRight: "┘",
	TeeRight:    "├",
	TeeLeft:     "┤",
}

// DefaultTreeGlyphs is the standard tree-list prefix set.
var DefaultTreeGlyphs = TreeGlyphs{
	Branch: "├─",
	Last:   "└─",
}

// DefaultSepGlyphs is the standard separator set.
var DefaultSepGlyphs = SepGlyphs{
	Dot: "·",
}

// defaultToolSymbols maps tool symbol keys to glyphs, mirroring the
// oh-my-pi default symbol preset. Keys absent from the table fall back to
// the caller-supplied glyph.
var defaultToolSymbols = map[string]string{
	"tool.bash":      "❯",
	"tool.ssh":       "⇄",
	"tool.lsp":       "💡",
	"tool.gh":        "⎇",
	"tool.webSearch": "⌕",
	"tool.exa":       "🔭",
	"tool.browser":   "🌐",
	"tool.eval":      "▶",
	"tool.debug":     "🐞",
}
