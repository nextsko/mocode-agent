// Glamour-backed Markdown answer rendering with a compact-mode line cap,
// a plain-wrap degradation path, and memoized per-style/width renderers.
package ui

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"

	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// DefaultMaxAnswerLines caps rendered answer lines in compact mode when
// MarkdownOptions.MaxAnswerLines is unset.
const DefaultMaxAnswerLines = 30

// minMarkdownWidth is the narrowest width glamour renders without garbling
// output (mirrors internal/ui/common).
const minMarkdownWidth = 20

// truncatedSuffix marks a compact-mode capped answer.
const truncatedSuffix = "… (truncated)"

// noAnswerText is the degradation line for empty answer payloads.
const noAnswerText = "No answer text returned"

// MarkdownOptions configures RenderMarkdown.
type MarkdownOptions struct {
	// Theme styles the degradation lines ("No answer text returned",
	// the truncation suffix). Nil renders them unstyled.
	Theme theme.Theme
	// Compact caps the output at MaxAnswerLines lines.
	Compact bool
	// MaxAnswerLines is the compact cap; values below 1 fall back to
	// DefaultMaxAnswerLines.
	MaxAnswerLines int
	// Style is the glamour standard style name ("dark", "light",
	// "notty", "auto"); "" means "dark".
	Style string
}

// markdownRendererKey identifies one memoized glamour renderer.
type markdownRendererKey struct {
	style string
	width int
}

var (
	mdRendererMu       sync.Mutex
	mdRendererCache    = map[markdownRendererKey]*glamour.TermRenderer{}
	mdRendererLocks    sync.Mutex
	mdRendererPerRLock = map[*glamour.TermRenderer]*sync.Mutex{}
)

// markdownRenderer returns a memoized glamour renderer for the style and
// width, or an error when the style is unknown. Renderers are shared, so
// every Render call must hold the renderer's own lock (glamour
// TermRenderers are not goroutine-safe).
func markdownRenderer(style string, width int) (*glamour.TermRenderer, error) {
	if style == "" {
		style = "dark"
	}
	if width < minMarkdownWidth {
		width = minMarkdownWidth
	}
	key := markdownRendererKey{style: style, width: width}

	mdRendererMu.Lock()
	r, ok := mdRendererCache[key]
	mdRendererMu.Unlock()
	if ok {
		return r, nil
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}

	mdRendererMu.Lock()
	if existing, ok := mdRendererCache[key]; ok {
		mdRendererMu.Unlock()
		return existing, nil
	}
	mdRendererCache[key] = r
	mdRendererMu.Unlock()
	return r, nil
}

// lockForRenderer returns a mutex that serializes Render calls on the
// shared renderer.
func lockForRenderer(r *glamour.TermRenderer) *sync.Mutex {
	mdRendererLocks.Lock()
	defer mdRendererLocks.Unlock()
	if mu, ok := mdRendererPerRLock[r]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	mdRendererPerRLock[r] = mu
	return mu
}

// RenderMarkdown renders text through glamour at the given width. Empty
// or blank text degrades to a single "No answer text returned" line;
// compact mode caps output at MaxAnswerLines lines plus a muted
// "… (truncated)" suffix; any glamour failure degrades to the original
// text wrapped to width.
func RenderMarkdown(text string, width int, opts MarkdownOptions) []string {
	muted := func(s string) string {
		if opts.Theme == nil {
			return s
		}
		return opts.Theme.FG(theme.ColorMuted, s)
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{muted(noAnswerText)}
	}

	lines, err := renderGlamour(trimmed, opts.Style, width)
	if err != nil {
		return degradeToWrapped(trimmed, width)
	}

	if !opts.Compact {
		return lines
	}
	maxLines := opts.MaxAnswerLines
	if maxLines < 1 {
		maxLines = DefaultMaxAnswerLines
	}
	if len(lines) <= maxLines {
		return lines
	}
	capped := make([]string, 0, maxLines+1)
	capped = append(capped, lines[:maxLines]...)
	return append(capped, muted(truncatedSuffix))
}

// renderGlamour renders text and splits the result into lines.
func renderGlamour(text, style string, width int) ([]string, error) {
	r, err := markdownRenderer(style, width)
	if err != nil {
		return nil, err
	}
	mu := lockForRenderer(r)
	mu.Lock()
	out, err := r.Render(text)
	mu.Unlock()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

// degradeToWrapped wraps plain text to width as the glamour fallback.
func degradeToWrapped(text string, width int) []string {
	if width < 1 {
		return []string{text}
	}
	wrapped := theme.WrapText(text, width)
	if wrapped == text && !strings.Contains(text, "\n") {
		return []string{text}
	}
	return strings.Split(wrapped, "\n")
}
