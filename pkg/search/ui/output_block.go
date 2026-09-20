// Package ui renders web search tool calls and results as bordered TUI
// blocks: an OutputBlock framed container with an LRU render cache, a
// collapsible TreeList for sources, a glamour-backed Markdown renderer,
// and the WebSearchRenderer entry point that assembles them.
package ui

import (
	"container/list"
	"io"
	"strings"
	"sync"

	"github.com/zeebo/xxh3"

	"github.com/nextsko/mocode-agent/pkg/search/ui/theme"
)

// Section is one labeled region inside an OutputBlock. Label, when set,
// draws a titled separator bar; a label-less section can instead request a
// plain divider via Separator (only honored between sections, matching the
// TS output-block).
type Section struct {
	// Label is the section title embedded in its separator bar.
	Label string
	// Lines are the section's content lines.
	Lines []string
	// Separator draws a plain divider bar for label-less sections.
	Separator bool
}

// OutputBlockOptions configures one framed block render.
type OutputBlockOptions struct {
	// Header is the status line embedded in the top border.
	Header string
	// HeaderMeta is an extra fragment joined after Header with the
	// separator dot.
	HeaderMeta string
	// State selects the border color via theme.BorderColor.
	State theme.State
	// Sections lists the content regions in display order.
	Sections []Section
	// Width is the outer block width in terminal cells.
	Width int
	// ApplyBG reserves the flag in the render cache key; the theme
	// contract exposes no state background token, so no background is
	// painted (documented deviation from the TS output-block).
	ApplyBG bool
}

// outputBlockCapacity bounds the per-instance LRU render cache.
const outputBlockCapacity = 32

// contentPadding is the fixed cell padding on each side of block content.
const contentPadding = 1

// barCapLen is the dash run following each bar's left corner glyph.
const barCapLen = 3

// OutputBlock is a bordered container renderer with an LRU cache keyed on
// the full options hash (width + state + header + every section line). A
// single instance is safe for concurrent use; the Theme is fixed at
// construction, so cached lines stay consistent with it.
type OutputBlock struct {
	theme theme.Theme

	mu      sync.Mutex
	entries map[uint64]*list.Element // hash -> lru element
	lru     *list.List               // front = most recent
	hits    int
	misses  int
}

// cacheEntry is one LRU node holding a rendered block.
type cacheEntry struct {
	key   uint64
	lines []string
}

// NewOutputBlock returns a cached framed-block renderer bound to t.
func NewOutputBlock(t theme.Theme) *OutputBlock {
	return &OutputBlock{
		theme:   t,
		entries: make(map[uint64]*list.Element, outputBlockCapacity),
		lru:     list.New(),
	}
}

// CacheHits reports how many Render calls were served from the cache.
func (b *OutputBlock) CacheHits() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hits
}

// CacheMisses reports how many Render calls rebuilt the block.
func (b *OutputBlock) CacheMisses() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.misses
}

// CacheSize reports the number of cached renders (for tests and stats).
func (b *OutputBlock) CacheSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// Invalidate drops every cached render, forcing a rebuild on the next
// Render call (e.g. after a theme switch).
func (b *OutputBlock) Invalidate() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = make(map[uint64]*list.Element, outputBlockCapacity)
	b.lru = list.New()
}

// Render returns the framed block lines for opts, consulting the cache
// first. Callers must treat the returned slice as immutable.
func (b *OutputBlock) Render(opts OutputBlockOptions) []string {
	key := hashOutputBlockOptions(opts)

	b.mu.Lock()
	if elem, ok := b.entries[key]; ok {
		b.lru.MoveToFront(elem)
		b.hits++
		lines := elem.Value.(*cacheEntry).lines
		b.mu.Unlock()
		return lines
	}
	b.misses++
	b.mu.Unlock()

	lines := renderOutputBlock(b.theme, opts)

	b.mu.Lock()
	defer b.mu.Unlock()
	if elem, ok := b.entries[key]; ok { // lost a race: keep first build
		b.lru.MoveToFront(elem)
		return elem.Value.(*cacheEntry).lines
	}
	elem := b.lru.PushFront(&cacheEntry{key: key, lines: lines})
	b.entries[key] = elem
	if len(b.entries) > outputBlockCapacity {
		oldest := b.lru.Back()
		if oldest != nil {
			b.lru.Remove(oldest)
			delete(b.entries, oldest.Value.(*cacheEntry).key)
		}
	}
	return lines
}

// hashOutputBlockOptions mixes every visually significant field into one
// xxh3 hash, mirroring the TS CachedOutputBlock key. Strings are
// length-prefixed so delimiters inside content cannot collide.
func hashOutputBlockOptions(opts OutputBlockOptions) uint64 {
	h := xxh3.New()
	hashInt(h, opts.Width)
	hashBool(h, opts.ApplyBG)
	hashString(h, string(opts.State))
	hashString(h, opts.Header)
	hashString(h, opts.HeaderMeta)
	for _, s := range opts.Sections {
		hashString(h, s.Label)
		hashBool(h, s.Separator)
		for _, line := range s.Lines {
			hashString(h, line)
		}
		hashString(h, "") // section terminator
	}
	return h.Sum64()
}

// hashInt hashes an int with a fixed-width little-endian encoding.
func hashInt(h io.Writer, v int) {
	var buf [8]byte
	for i := range buf {
		buf[i] = byte(v >> (8 * i))
	}
	_, _ = h.Write(buf[:])
}

// hashBool hashes a bool as one byte.
func hashBool(h io.Writer, v bool) {
	b := byte(0)
	if v {
		b = 1
	}
	_, _ = h.Write([]byte{b})
}

// hashString hashes a length-prefixed string.
func hashString(h io.Writer, s string) {
	hashInt(h, len(s))
	_, _ = io.WriteString(h, s)
}

// OutputBlockContentWidth returns the inner width renderOutputBlock wraps
// content to: outer width minus both vertical borders and the symmetric
// one-cell padding, floored at 1 (mirroring TS outputBlockContentWidth
// with its default padding of 1).
func OutputBlockContentWidth(width int) int {
	if w := width - 2 - 2*contentPadding; w > 1 {
		return w
	}
	return 1
}

// barRow describes one horizontal border row: corner glyphs plus an
// optional embedded label, or the bottom border when bottom is set.
type barRow struct {
	left, right string
	label, meta string
	bottom      bool
}

// contentRow is one wrapped and padded content line.
type contentRow struct{ inner string }

// renderOutputBlock lays out the framed block: a top border embedding the
// header, one titled separator bar per labeled section, wrapped and padded
// content rows, and a bottom border. Border glyphs carry the state's
// border color; embedded labels keep their own styling.
func renderOutputBlock(t theme.Theme, opts OutputBlockOptions) []string {
	box := t.Box()
	h := box.Horizontal
	v := box.Vertical
	borderColor := t.BorderColor(opts.State)
	border := func(s string) string { return t.FG(borderColor, s) }

	lineWidth := max(0, opts.Width)
	contentWidth := max(0, lineWidth-2-2*contentPadding)

	var rows []any
	rows = append(rows, barRow{
		left: box.TopLeft, right: box.TopRight,
		label: opts.Header, meta: opts.HeaderMeta,
	})

	sections := opts.Sections
	if len(sections) == 0 {
		sections = []Section{{}}
	}
	for i, section := range sections {
		switch {
		case section.Label != "":
			rows = append(rows, barRow{
				left: box.TeeRight, right: box.TeeLeft,
				label: section.Label,
			})
		case section.Separator && i > 0:
			rows = append(rows, barRow{left: box.TeeRight, right: box.TeeLeft})
		}
		for _, line := range section.Lines {
			for _, sub := range strings.Split(line, "\n") {
				trimmed := strings.TrimRight(sub, " \t")
				for _, wrapped := range splitWrapped(t, trimmed, contentWidth) {
					pad := max(0, contentWidth-theme.VisibleWidth(wrapped))
					rows = append(rows, contentRow{
						inner: wrapped + strings.Repeat(" ", pad),
					})
				}
			}
		}
	}
	rows = append(rows, barRow{left: box.BottomLeft, right: box.BottomRight, bottom: true})

	capRun := strings.Repeat(h, barCapLen)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		switch r := row.(type) {
		case barRow:
			if r.bottom {
				fill := max(0, lineWidth-theme.VisibleWidth(r.left)-barCapLen-1)
				lines = append(lines, border(r.left+capRun)+
					border(strings.Repeat(h, fill))+
					border(r.right))
				continue
			}
			lines = append(lines, renderBar(t, border, h, capRun, lineWidth, r))
		case contentRow:
			lines = append(lines, border(v)+" "+r.inner+" "+border(v))
		}
	}
	return lines
}

// renderBar emits one horizontal bar row, embedding the label (kept in
// its own styling) between the border-colored corner glyphs, dash cap,
// and trailing fill, mirroring the TS renderBar.
func renderBar(
	t theme.Theme,
	border func(string) string,
	h, capRun string,
	lineWidth int,
	r barRow,
) string {
	leftGlyphs := r.left + capRun
	rightWidth := theme.VisibleWidth(r.right)

	labelParts := make([]string, 0, 2)
	if r.label != "" {
		labelParts = append(labelParts, r.label)
	}
	if r.meta != "" {
		labelParts = append(labelParts, r.meta)
	}
	if len(labelParts) == 0 {
		fill := max(0, lineWidth-theme.VisibleWidth(leftGlyphs)-rightWidth)
		return border(leftGlyphs) + border(strings.Repeat(h, fill)) + border(r.right)
	}

	// The TS sep.dot glyph is " · "; the Go theme stores the bare "·", so
	// the join re-adds the surrounding spaces for the same visual.
	labelText := strings.Join(labelParts, " "+t.Sep().Dot+" ")
	rawLabel := " " + labelText + " "
	maxLabelWidth := max(0, lineWidth-theme.VisibleWidth(leftGlyphs)-rightWidth)
	trimmed := theme.TruncateToWidth(rawLabel, maxLabelWidth, theme.DefaultEllipsis)
	fill := max(0, lineWidth-
		theme.VisibleWidth(leftGlyphs)-
		theme.VisibleWidth(trimmed)-
		rightWidth)
	return border(leftGlyphs) + trimmed + border(strings.Repeat(h, fill)) + border(r.right)
}

// splitWrapped wraps one logical line to width, returning the original as
// a single element when no wrapping is needed.
func splitWrapped(_ theme.Theme, line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	wrapped := theme.WrapText(line, width)
	if wrapped == line {
		return []string{line}
	}
	return strings.Split(wrapped, "\n")
}
