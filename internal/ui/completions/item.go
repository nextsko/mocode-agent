package completions

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"github.com/sahilm/fuzzy"

	"github.com/nextsko/mocode-agent/internal/ui/list"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

// FileCompletionValue represents a file path completion value.
type FileCompletionValue struct {
	Path string
}

// ResourceCompletionValue represents a MCP resource completion value.
type ResourceCompletionValue struct {
	MCPName  string
	URI      string
	Title    string
	MIMEType string
}

type AtCompletionKind string

const (
	AtCompletionCategory AtCompletionKind = "category"
	AtCompletionMCP      AtCompletionKind = "mcp"
	AtCompletionSkill    AtCompletionKind = "skill"
	AtCompletionFile     AtCompletionKind = "file"
	AtCompletionDir      AtCompletionKind = "dir"
	AtCompletionWorkflow AtCompletionKind = "workflow"
)

// AtCompletionValue represents an @ context item.
type AtCompletionValue struct {
	Kind        AtCompletionKind
	Token       string
	Desc        string
	Path        string
	Content     string
	Category    string
	IsCategory  bool
	Resource    ResourceCompletionValue
	TrailingGap bool
}

// slashCmdColWidth is the width of the command column in slash completion items.
const slashCmdColWidth = 16

// SlashCompletionValue represents a slash command completion value.
type SlashCompletionValue struct {
	Command string
	Desc    string
	// Msg is an optional tea.Msg to dispatch when this item is selected.
	// If nil, handleSlashCommand is called with Command instead.
	Msg any
}

// SlashCompletionItem renders a two-column command/description row using the
// same styles as CommandItem so the inline popup matches the dialog aesthetic.
type SlashCompletionItem struct {
	command string
	desc    string
	value   SlashCompletionValue
	focused bool
	cache   map[int]string

	t *styles.Styles
}

// NewSlashCompletionItem creates a new SlashCompletionItem.
func NewSlashCompletionItem(v SlashCompletionValue, t *styles.Styles) *SlashCompletionItem {
	return &SlashCompletionItem{
		command: v.Command,
		desc:    v.Desc,
		value:   v,
		t:       t,
	}
}

// Text returns the display text used for popup width calculation.
func (s *SlashCompletionItem) Text() string {
	pad := strings.Repeat(" ", max(0, slashCmdColWidth-ansi.StringWidth(s.command)))
	return s.command + pad + s.desc
}

// Filter implements [list.FilterableItem].
func (s *SlashCompletionItem) Filter() string { return s.command + " " + s.desc }

// Value returns the underlying SlashCompletionValue.
func (s *SlashCompletionItem) Value() any { return s.value }

// SetFocused implements [list.Focusable].
func (s *SlashCompletionItem) SetFocused(focused bool) {
	if s.focused != focused {
		s.cache = nil
	}
	s.focused = focused
}

// SetMatch implements [list.MatchSettable] (no-op).
func (s *SlashCompletionItem) SetMatch(_ fuzzy.Match) { s.cache = nil }

// Render implements [list.Item] using the same two-column layout as CommandItem:
// bold label (command) on the left, dim description on the right.
func (s *SlashCompletionItem) Render(width int) string {
	if s.cache == nil {
		s.cache = make(map[int]string)
	}
	if cached, ok := s.cache[width]; ok {
		return cached
	}

	style := s.t.Dialog.NormalItem
	if s.focused {
		style = s.t.Dialog.SelectedItem
	}

	lineWidth := max(0, width-style.GetHorizontalFrameSize())
	labelWidth := min(20, max(12, lineWidth/4))
	label := ansi.Truncate(s.command, max(0, labelWidth), "\u2026")
	labelGap := strings.Repeat(" ", max(1, labelWidth-lipgloss.Width(label)+1))
	descWidth := max(0, lineWidth-labelWidth-len(labelGap))
	desc := ansi.Truncate(s.desc, descWidth, "\u2026")

	var row string
	if s.focused {
		gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-len(labelGap)-lipgloss.Width(desc)))
		row = label + labelGap + desc + gap
	} else {
		renderedLabel := s.t.Dialog.TitleText.Render(label)
		renderedDesc := s.t.Dialog.ListItem.InfoBlurred.Render(desc)
		gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-len(labelGap)-lipgloss.Width(desc)))
		row = renderedLabel + labelGap + renderedDesc + gap
	}

	result := style.Render(row)
	s.cache[width] = result
	return result
}

// Ensure SlashCompletionItem implements the required interfaces.
var (
	_ list.Item           = (*SlashCompletionItem)(nil)
	_ list.FilterableItem = (*SlashCompletionItem)(nil)
	_ list.MatchSettable  = (*SlashCompletionItem)(nil)
	_ list.Focusable      = (*SlashCompletionItem)(nil)
)

// AtCompletionItem renders a two-column @ context row.
type AtCompletionItem struct {
	value   AtCompletionValue
	focused bool
	cache   map[int]string
	t       *styles.Styles
}

func NewAtCompletionItem(v AtCompletionValue, t *styles.Styles) *AtCompletionItem {
	return &AtCompletionItem{value: v, t: t}
}

func (a *AtCompletionItem) Text() string {
	label := a.value.Token
	if a.value.IsCategory {
		label += " →"
	}
	pad := strings.Repeat(" ", max(0, slashCmdColWidth-ansi.StringWidth(label)))
	return label + pad + a.value.Desc
}

func (a *AtCompletionItem) Filter() string {
	return a.value.Token + " " + a.value.Desc + " " + string(a.value.Kind)
}

func (a *AtCompletionItem) Value() any { return a.value }

func (a *AtCompletionItem) SetFocused(focused bool) {
	if a.focused != focused {
		a.cache = nil
	}
	a.focused = focused
}

func (a *AtCompletionItem) SetMatch(_ fuzzy.Match) { a.cache = nil }

func (a *AtCompletionItem) Render(width int) string {
	if a.cache == nil {
		a.cache = make(map[int]string)
	}
	if cached, ok := a.cache[width]; ok {
		return cached
	}

	style := a.t.Dialog.NormalItem
	if a.focused {
		style = a.t.Dialog.SelectedItem
	}

	lineWidth := max(0, width-style.GetHorizontalFrameSize())
	labelWidth := min(22, max(14, lineWidth/3))
	label := a.value.Token
	if a.value.IsCategory {
		label += " →"
	}
	label = ansi.Truncate(label, max(0, labelWidth), "…")
	labelGap := strings.Repeat(" ", max(1, labelWidth-lipgloss.Width(label)+1))
	descWidth := max(0, lineWidth-labelWidth-len(labelGap))
	desc := ansi.Truncate(a.value.Desc, descWidth, "…")

	var row string
	if a.focused {
		gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-len(labelGap)-lipgloss.Width(desc)))
		row = label + labelGap + desc + gap
	} else {
		renderedLabel := a.t.Dialog.TitleText.Render(label)
		renderedDesc := a.t.Dialog.ListItem.InfoBlurred.Render(desc)
		gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-len(labelGap)-lipgloss.Width(desc)))
		row = renderedLabel + labelGap + renderedDesc + gap
	}

	result := style.Render(row)
	a.cache[width] = result
	return result
}

var (
	_ list.Item           = (*AtCompletionItem)(nil)
	_ list.FilterableItem = (*AtCompletionItem)(nil)
	_ list.MatchSettable  = (*AtCompletionItem)(nil)
	_ list.Focusable      = (*AtCompletionItem)(nil)
)

// SlashGroupItem is a non-selectable group label rendered between slash command
// sections. It returns an empty Filter() string so the fuzzy engine ignores it
// when the user types a query.
type SlashGroupItem struct {
	label       string
	normalStyle lipgloss.Style
}

// NewSlashGroupItem creates a new SlashGroupItem.
func NewSlashGroupItem(label string, normalStyle lipgloss.Style) *SlashGroupItem {
	return &SlashGroupItem{label: label, normalStyle: normalStyle}
}

// Filter implements [list.FilterableItem]. Returns "" so the item is invisible
// during fuzzy filtering but visible when no query is active.
func (g *SlashGroupItem) Filter() string { return "" }

// SetMatch implements [list.MatchSettable] (no-op).
func (g *SlashGroupItem) SetMatch(_ fuzzy.Match) {}

// Text returns the group label text used for width calculation.
func (g *SlashGroupItem) Text() string { return " " + strings.ToUpper(g.label) }

// Skip signals that navigation should bypass this row.
func (g *SlashGroupItem) Skip() bool { return true }

// Render implements [list.Item] as a faint uppercase section label.
func (g *SlashGroupItem) Render(width int) string {
	text := strings.ToUpper(g.label)
	style := lipgloss.NewStyle().
		Background(g.normalStyle.GetBackground()).
		Foreground(g.normalStyle.GetForeground()).
		Faint(true).
		Padding(0, 1).
		Width(width)
	return style.Render(text)
}

// Ensure SlashGroupItem implements the required interfaces.
var (
	_ list.Item           = (*SlashGroupItem)(nil)
	_ list.FilterableItem = (*SlashGroupItem)(nil)
	_ list.MatchSettable  = (*SlashGroupItem)(nil)
)

// CompletionItem represents an item in the completions list.
type CompletionItem struct {
	text    string
	value   any
	match   fuzzy.Match
	focused bool
	cache   map[int]string

	// Styles
	normalStyle  lipgloss.Style
	focusedStyle lipgloss.Style
	matchStyle   lipgloss.Style
}

// NewCompletionItem creates a new completion item.
func NewCompletionItem(text string, value any, normalStyle, focusedStyle, matchStyle lipgloss.Style) *CompletionItem {
	return &CompletionItem{
		text:         text,
		value:        value,
		normalStyle:  normalStyle,
		focusedStyle: focusedStyle,
		matchStyle:   matchStyle,
	}
}

// Text returns the display text of the item.
func (c *CompletionItem) Text() string {
	return c.text
}

// Value returns the value of the item.
func (c *CompletionItem) Value() any {
	return c.value
}

// Filter implements [list.FilterableItem].
func (c *CompletionItem) Filter() string {
	return c.text
}

// SetMatch implements [list.MatchSettable].
func (c *CompletionItem) SetMatch(m fuzzy.Match) {
	c.cache = nil
	c.match = m
}

// SetFocused implements [list.Focusable].
func (c *CompletionItem) SetFocused(focused bool) {
	if c.focused != focused {
		c.cache = nil
	}
	c.focused = focused
}

// Render implements [list.Item].
func (c *CompletionItem) Render(width int) string {
	return renderItem(
		c.normalStyle,
		c.focusedStyle,
		c.matchStyle,
		c.text,
		c.focused,
		width,
		c.cache,
		&c.match,
	)
}

func renderItem(
	normalStyle, focusedStyle, matchStyle lipgloss.Style,
	text string,
	focused bool,
	width int,
	cache map[int]string,
	match *fuzzy.Match,
) string {
	if cache == nil {
		cache = make(map[int]string)
	}

	cached, ok := cache[width]
	if ok {
		return cached
	}

	innerWidth := width - 2 // Account for padding
	// Truncate if needed.
	if ansi.StringWidth(text) > innerWidth {
		text = ansi.Truncate(text, innerWidth, "…")
	}

	// Select base style.
	style := normalStyle
	matchStyle = matchStyle.Background(style.GetBackground())
	if focused {
		style = focusedStyle
		matchStyle = matchStyle.Background(style.GetBackground())
	}

	// Render full-width text with background.
	content := style.Padding(0, 1).Width(width).Render(text)

	// Apply match highlighting using StyleRanges.
	if len(match.MatchedIndexes) > 0 {
		var ranges []lipgloss.Range
		for _, rng := range matchedRanges(match.MatchedIndexes) {
			start, stop := bytePosToVisibleCharPos(text, rng)
			// Offset by 1 for the padding space.
			ranges = append(ranges, lipgloss.NewRange(start+1, stop+2, matchStyle))
		}
		content = lipgloss.StyleRanges(content, ranges...)
	}

	cache[width] = content
	return content
}

// matchedRanges converts a list of match indexes into contiguous ranges.
func matchedRanges(in []int) [][2]int {
	if len(in) == 0 {
		return [][2]int{}
	}
	current := [2]int{in[0], in[0]}
	if len(in) == 1 {
		return [][2]int{current}
	}
	var out [][2]int
	for i := 1; i < len(in); i++ {
		if in[i] == current[1]+1 {
			current[1] = in[i]
		} else {
			out = append(out, current)
			current = [2]int{in[i], in[i]}
		}
	}
	out = append(out, current)
	return out
}

// bytePosToVisibleCharPos converts byte positions to visible character positions.
func bytePosToVisibleCharPos(str string, rng [2]int) (int, int) {
	bytePos, byteStart, byteStop := 0, rng[0], rng[1]
	pos, start, stop := 0, 0, 0
	gr := uniseg.NewGraphemes(str)
	for byteStart > bytePos {
		if !gr.Next() {
			break
		}
		bytePos += len(gr.Str())
		pos += max(1, gr.Width())
	}
	start = pos
	for byteStop > bytePos {
		if !gr.Next() {
			break
		}
		bytePos += len(gr.Str())
		pos += max(1, gr.Width())
	}
	stop = pos
	return start, stop
}

// Ensure CompletionItem implements the required interfaces.
var (
	_ list.Item           = (*CompletionItem)(nil)
	_ list.FilterableItem = (*CompletionItem)(nil)
	_ list.MatchSettable  = (*CompletionItem)(nil)
	_ list.Focusable      = (*CompletionItem)(nil)
)
