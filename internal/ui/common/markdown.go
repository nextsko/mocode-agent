package common

import (
	"fmt"
	"image/color"
	"sync"

	"charm.land/glamour/v2"
	"github.com/alecthomas/chroma/v2/formatters"

	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/ui/xchroma"
)

const formatterName = "mocode"

// minMarkdownWidth is the minimum width glamour needs to render without
// producing garbled output or panicking.
const minMarkdownWidth = 20

func init() {
	// NOTE: Glamour does not offer us an option to pass the formatter
	// implementation directly. We need to register and use by name.
	var zero color.Color
	formatters.Register(formatterName, xchroma.Formatter(zero, nil))
}

var (
	mdCacheMu    sync.Mutex
	mdCache      = map[int]*glamour.TermRenderer{}
	quietMDCache = map[int]*glamour.TermRenderer{}
)

// MarkdownRenderer returns a glamour [glamour.TermRenderer] configured with
// the given styles and width. Renderers are memoized per width and shared
// across callers; call InvalidateMarkdownRendererCache when the active
// styles change.
func MarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
	if width < minMarkdownWidth {
		width = minMarkdownWidth
	}
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	if r, ok := mdCache[width]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(sty.Markdown),
		glamour.WithWordWrap(width),
		glamour.WithChromaFormatter(formatterName),
	)
	if err != nil {
		// DEBUG: Log renderer creation error
		fmt.Printf("[DEBUG] Failed to create glamour renderer: %v\nWidth: %d\n", err, width)
		return nil
	}
	mdCache[width] = r
	return r
}

// QuietMarkdownRenderer returns a glamour [glamour.TermRenderer] with no colors
// (plain text with structure) and the given width. Renderers are memoized per
// width and shared across callers.
func QuietMarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
	if width < minMarkdownWidth {
		width = minMarkdownWidth
	}
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	if r, ok := quietMDCache[width]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(sty.QuietMarkdown),
		glamour.WithWordWrap(width),
		glamour.WithChromaFormatter(formatterName),
	)
	if err != nil {
		fmt.Printf("[DEBUG] Failed to create quiet glamour renderer: %v\nWidth: %d\n", err, width)
		return nil
	}
	quietMDCache[width] = r
	return r
}

// InvalidateMarkdownRendererCache drops every cached renderer. Call this
// whenever the active styles change so subsequent renderers pick up the new
// ansi.StyleConfig.
func InvalidateMarkdownRendererCache() {
	mdCacheMu.Lock()
	defer mdCacheMu.Unlock()
	mdCache = map[int]*glamour.TermRenderer{}
	quietMDCache = map[int]*glamour.TermRenderer{}
}
