package completions

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/ordered"

	"github.com/package-register/mocode/internal/core/agent/tools/mcp"
	"github.com/package-register/mocode/internal/ui/list"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/util/fsext"
)

const (
	minHeight = 1
	maxHeight = 10
	minWidth  = 10
	maxWidth  = 100

	slashMinWidth = 72
	slashMaxWidth = 88

	tierExactName = iota
	tierPrefixName
	tierPathSegment
	tierFallback
)

// SelectionMsg is sent when a completion is selected.
type SelectionMsg[T any] struct {
	Value    T
	KeepOpen bool // If true, insert without closing.
}

// ClosedMsg is sent when the completions are closed.
type ClosedMsg struct{}

// CompletionItemsLoadedMsg is sent when files have been loaded for completions.
type CompletionItemsLoadedMsg struct {
	Files     []FileCompletionValue
	Resources []ResourceCompletionValue
}

type AtCompletionItemsLoadedMsg struct {
	Items []AtCompletionValue
}

// Completions represents the completions popup component.
type Completions struct {
	// Popup dimensions
	width  int
	height int

	// State
	open  bool
	query string

	// Key bindings
	keyMap KeyMap

	// List component
	list *list.FilterableList

	// Styling
	normalStyle  lipgloss.Style
	focusedStyle lipgloss.Style
	matchStyle   lipgloss.Style

	allItems          []list.FilterableItem
	filtered          []list.FilterableItem
	slashHeaderHeight int // fallback header rows when slashStyles is nil

	// Slash mode: dialog-style container
	slashStyles     *styles.Styles // non-nil when slash popup is active
	slashForceWidth int            // outer popup width (set from editor layout)
	popupTitle      string
}

type namePriorityRule struct {
	tier  int
	match func(pathLower, baseLower, stemLower, queryLower string) bool
}

var namePriorityRules = []namePriorityRule{
	{
		tier: tierExactName,
		match: func(_ string, baseLower, stemLower, queryLower string) bool {
			return baseLower == queryLower || stemLower == queryLower
		},
	},
	{
		tier: tierPrefixName,
		match: func(_ string, baseLower, _ string, queryLower string) bool {
			return strings.HasPrefix(baseLower, queryLower)
		},
	},
	{
		tier: tierPathSegment,
		match: func(pathLower, _ string, _ string, queryLower string) bool {
			return hasPathSegment(pathLower, queryLower)
		},
	},
}

// New creates a new completions component.
func New(normalStyle, focusedStyle, matchStyle lipgloss.Style) *Completions {
	l := list.NewFilterableList()
	l.SetGap(0)
	l.SetReverse(true)

	return &Completions{
		keyMap:       DefaultKeyMap(),
		list:         l,
		normalStyle:  normalStyle,
		focusedStyle: focusedStyle,
		matchStyle:   matchStyle,
	}
}

// SetStyles updates the styles used when rendering completion items.
// Existing items are not restyled; subsequent SetItems calls pick up the
// new styles.
func (c *Completions) SetStyles(normalStyle, focusedStyle, matchStyle lipgloss.Style) {
	c.normalStyle = normalStyle
	c.focusedStyle = focusedStyle
	c.matchStyle = matchStyle
}

// IsOpen returns whether the completions popup is open.
func (c *Completions) IsOpen() bool {
	return c.open
}

// Query returns the current filter query.
func (c *Completions) Query() string {
	return c.query
}

// Size returns the outer popup dimensions (width × height in terminal rows).
func (c *Completions) Size() (width, height int) {
	if c.slashStyles != nil {
		frameV := c.slashStyles.Dialog.View.GetVerticalFrameSize()
		listRows := min(len(c.filtered), c.height)
		return c.slashForceWidth, frameV + 2 + listRows // +2: title + sep
	}
	visible := len(c.filtered)
	return c.width, min(visible, c.height) + c.slashHeaderHeight
}

// KeyMap returns the key bindings.
func (c *Completions) KeyMap() KeyMap {
	return c.keyMap
}

// Open opens the completions with file items from the filesystem.
func (c *Completions) Open(depth, limit int) tea.Cmd {
	return func() tea.Msg {
		var msg CompletionItemsLoadedMsg
		var wg sync.WaitGroup
		wg.Go(func() {
			msg.Files = loadFiles(depth, limit)
		})
		wg.Go(func() {
			msg.Resources = loadMCPResources()
		})
		wg.Wait()
		return msg
	}
}

// SetItems sets the files and MCP resources and rebuilds the merged list.
func (c *Completions) SetItems(files []FileCompletionValue, resources []ResourceCompletionValue) {
	c.slashHeaderHeight = 0
	c.slashStyles = nil
	c.slashForceWidth = 0
	c.list.SetReverse(true) // file completions: newest/best at bottom
	items := make([]list.FilterableItem, 0, len(files)+len(resources))

	// Add files first.
	for _, file := range files {
		item := NewCompletionItem(
			file.Path,
			file,
			c.normalStyle,
			c.focusedStyle,
			c.matchStyle,
		)
		items = append(items, item)
	}

	// Add MCP resources.
	for _, resource := range resources {
		item := NewCompletionItem(
			resource.MCPName+"/"+cmp.Or(resource.Title, resource.URI),
			resource,
			c.normalStyle,
			c.focusedStyle,
			c.matchStyle,
		)
		items = append(items, item)
	}

	c.open = true
	c.query = ""
	c.allItems = items
	c.filtered = append([]list.FilterableItem(nil), items...)
	c.list.SetItems(c.filtered...)
	c.list.SetFilter("")
	c.list.Focus()

	c.width = maxWidth
	c.height = ordered.Clamp(len(items), int(minHeight), int(maxHeight))
	c.list.SetSize(c.width, c.height)
	c.list.SelectFirst()
	c.list.ScrollToSelected()

	c.updateSize()
}

// SetSlashItems populates the completions with slash command items.
// t and outerWidth are the full-styles token and the editor width cap;
// the actual popup width is computed from item content so it is
// responsive rather than always spanning the full editor.
func (c *Completions) SetSlashItems(items []SlashCompletionValue, t *styles.Styles, outerWidth int) {
	c.slashStyles = t
	c.slashHeaderHeight = 0
	c.popupTitle = "Commands"

	// Slash menu flows top-to-bottom (not chat-style), so reverse is off.
	c.list.SetReverse(false)

	compItems := make([]list.FilterableItem, 0, len(items))
	for _, cmd := range items {
		compItems = append(compItems, NewSlashCompletionItem(cmd, t))
	}

	// Content-responsive width: measure all items, cap at editor width.
	maxTextW := 0
	for _, item := range compItems {
		if tx, ok := item.(interface{ Text() string }); ok {
			maxTextW = max(maxTextW, ansi.StringWidth(tx.Text()))
		}
	}
	frameH := t.Dialog.View.GetHorizontalFrameSize()
	itemPadH := t.Dialog.NormalItem.GetHorizontalFrameSize()
	computedW := max(slashMinWidth, maxTextW+frameH+itemPadH+2)
	computedW = min(slashMaxWidth, computedW)
	outerWidth = min(outerWidth, computedW)
	c.slashForceWidth = outerWidth

	innerW := max(int(minWidth), outerWidth-frameH)

	c.open = true
	c.query = ""
	c.allItems = compItems
	c.filtered = append([]list.FilterableItem(nil), compItems...)
	c.list.SetItems(c.filtered...)
	c.list.SetFilter("")
	c.list.Focus()
	c.width = outerWidth
	c.height = ordered.Clamp(len(items), int(minHeight), int(maxHeight))
	c.list.SetSize(innerW, c.height)
	c.selectFirstReal()
	c.list.ScrollToSelected()
}

// SetSlashGroups populates the completions with grouped slash command items.
func (c *Completions) SetSlashGroups(groups []SlashGroup, t *styles.Styles, outerWidth int) {
	c.slashStyles = t
	c.slashHeaderHeight = 0
	c.popupTitle = "Commands"
	c.list.SetReverse(false)

	var allItems []list.FilterableItem
	for _, g := range groups {
		if len(g.Items) == 0 {
			continue
		}
		allItems = append(allItems, NewSlashGroupItem(g.Label, c.normalStyle))
		for _, v := range g.Items {
			allItems = append(allItems, NewSlashCompletionItem(v, t))
		}
	}

	maxTextW := 0
	for _, item := range allItems {
		if tx, ok := item.(interface{ Text() string }); ok {
			maxTextW = max(maxTextW, ansi.StringWidth(tx.Text()))
		}
	}
	frameH := t.Dialog.View.GetHorizontalFrameSize()
	itemPadH := t.Dialog.NormalItem.GetHorizontalFrameSize()
	computedW := max(slashMinWidth, maxTextW+frameH+itemPadH+2)
	computedW = min(slashMaxWidth, computedW)
	outerWidth = min(outerWidth, computedW)
	c.slashForceWidth = outerWidth
	innerW := max(int(minWidth), outerWidth-frameH)

	c.open = true
	c.query = ""
	c.allItems = allItems
	c.filtered = append([]list.FilterableItem(nil), allItems...)
	c.list.SetItems(c.filtered...)
	c.list.SetFilter("")
	c.list.Focus()
	c.width = outerWidth
	c.height = ordered.Clamp(len(allItems), int(minHeight), int(maxHeight))
	c.list.SetSize(innerW, c.height)
	c.selectFirstReal()
	c.list.ScrollToSelected()
}

func (c *Completions) SetAtItems(items []AtCompletionValue, t *styles.Styles, outerWidth int) {
	c.slashStyles = t
	c.slashHeaderHeight = 0
	c.popupTitle = "@ Context"
	c.list.SetReverse(false)

	allItems := make([]list.FilterableItem, 0, len(items))
	// Group @ items into titled sections. Each category marker (IsCategory)
	// becomes a non-selectable SlashGroupItem header, so typing a bare "@"
	// shows MCP / Skills / Files / Dirs / Workflows as distinct sections
	// instead of one undifferentiated list. The category row itself is dropped
	// (its text is redundant with the header).
	for _, item := range items {
		if item.IsCategory {
			label := sectionLabel(item.Category, item.Token)
			if label != "" {
				allItems = append(allItems, NewSlashGroupItem(label, t.Dialog.NormalItem))
			}
			continue
		}
		allItems = append(allItems, NewAtCompletionItem(item, t))
	}

	maxTextW := 0
	for _, item := range allItems {
		if tx, ok := item.(interface{ Text() string }); ok {
			maxTextW = max(maxTextW, ansi.StringWidth(tx.Text()))
		}
	}
	frameH := t.Dialog.View.GetHorizontalFrameSize()
	itemPadH := t.Dialog.NormalItem.GetHorizontalFrameSize()
	computedW := max(slashMinWidth, maxTextW+frameH+itemPadH+2)
	computedW = min(slashMaxWidth, computedW)
	outerWidth = min(outerWidth, computedW)
	c.slashForceWidth = outerWidth
	innerW := max(int(minWidth), outerWidth-frameH)

	c.open = true
	c.query = ""
	c.allItems = allItems
	c.filtered = append([]list.FilterableItem(nil), allItems...)
	c.list.SetItems(c.filtered...)
	c.list.SetFilter("")
	c.list.Focus()
	c.width = outerWidth
	c.height = ordered.Clamp(len(allItems), int(minHeight), int(maxHeight))
	c.list.SetSize(innerW, c.height)
	c.selectFirstReal()
	c.list.ScrollToSelected()
}

// Close closes the completions popup.
func (c *Completions) Close() {
	c.open = false
	c.slashHeaderHeight = 0
	c.slashStyles = nil
	c.slashForceWidth = 0
	c.popupTitle = ""
}

// renderSlashContainer wraps the list output in a Dialog.View border and
// prepends a "Commands" title line and a separator — matching the dialog
// aesthetic brought inline above the editor input.
func (c *Completions) renderSlashContainer(listOutput string) string {
	t := c.slashStyles
	w := c.slashForceWidth
	viewStyle := t.Dialog.View.Width(w)
	innerW := max(0, w-viewStyle.GetHorizontalFrameSize())

	title := cmp.Or(c.popupTitle, "Commands")
	title = t.Dialog.TitleText.Bold(true).Width(innerW).Render(title)
	sep := t.Dialog.ListItem.InfoBlurred.Width(innerW).Render(strings.Repeat("─", innerW))

	content := title + "\n" + sep + "\n" + listOutput
	return viewStyle.Render(content)
}

// renderSlashHeader is the legacy header used when slashStyles is nil.
func (c *Completions) renderSlashHeader(w int) string {
	titleStyle := c.normalStyle.Bold(true).Padding(0, 1).Width(w)
	title := titleStyle.Render("Commands")

	innerW := max(0, w-2)
	sepStyle := lipgloss.NewStyle().
		Background(c.normalStyle.GetBackground()).
		Foreground(c.normalStyle.GetForeground()).
		Faint(true).Padding(0, 1).Width(w)
	separator := sepStyle.Render(strings.Repeat("─", innerW))

	return title + "\n" + separator
}

// Filter filters the completions with the given query.
func (c *Completions) Filter(query string) {
	if !c.open {
		return
	}

	if query == c.query {
		return
	}

	c.query = query
	c.applyNamePriorityFilter(query)

	c.updateSize()
}

func (c *Completions) applyNamePriorityFilter(query string) {
	if query == "" {
		c.filtered = append([]list.FilterableItem(nil), c.allItems...)
		c.list.SetItems(c.filtered...)
		return
	}

	c.list.SetItems(c.allItems...)
	c.list.SetFilter(query)
	raw := c.list.FilteredItems()
	filtered := make([]list.FilterableItem, 0, len(raw))
	for _, item := range raw {
		filterable, ok := item.(list.FilterableItem)
		if !ok {
			continue
		}
		filtered = append(filtered, filterable)
	}

	queryLower := strings.ToLower(strings.TrimSpace(query))
	slices.SortStableFunc(filtered, func(a, b list.FilterableItem) int {
		return namePriorityTier(a.Filter(), queryLower) - namePriorityTier(b.Filter(), queryLower)
	})
	c.filtered = filtered
	c.list.SetItems(c.filtered...)
}

func namePriorityTier(path, queryLower string) int {
	if queryLower == "" {
		return tierFallback
	}

	pathLower := strings.ToLower(path)
	baseLower := strings.ToLower(filepath.Base(strings.ReplaceAll(path, `\`, `/`)))
	stemLower := strings.TrimSuffix(baseLower, filepath.Ext(baseLower))
	for _, rule := range namePriorityRules {
		if rule.match(pathLower, baseLower, stemLower, queryLower) {
			return rule.tier
		}
	}
	return tierFallback
}

func hasPathSegment(pathLower, queryLower string) bool {
	return slices.Contains(strings.FieldsFunc(pathLower, func(r rune) bool {
		return r == '/' || r == '\\'
	}), queryLower)
}

func (c *Completions) updateSize() {
	items := c.filtered

	if c.slashStyles != nil {
		// In slash mode the width is fixed; only recalculate height + list size.
		frameH := c.slashStyles.Dialog.View.GetHorizontalFrameSize()
		innerW := max(int(minWidth), c.slashForceWidth-frameH)
		c.height = ordered.Clamp(len(items), int(minHeight), int(maxHeight))
		c.list.SetSize(innerW, c.height)
		c.selectFirstReal()
		return
	}

	// File completions: calculate width from visible items.
	start, end := c.list.VisibleItemIndices()
	width := 0
	for i := start; i <= end; i++ {
		item := c.list.ItemAt(i)
		if item == nil {
			continue
		}
		s := item.(interface{ Text() string }).Text()
		width = max(width, ansi.StringWidth(s))
	}
	c.width = ordered.Clamp(width+2, int(minWidth), int(maxWidth))
	c.height = ordered.Clamp(len(items), int(minHeight), int(maxHeight))
	c.list.SetSize(c.width, c.height)
	c.selectFirstReal()
}

// skippable is implemented by items that should be bypassed during navigation.
type skippable interface {
	Skip() bool
}

// SlashGroup is a named section of slash command completions.
type SlashGroup struct {
	Label string
	Items []SlashCompletionValue
}

// HasItems returns whether there are any selectable (non-group-header) items.
func (c *Completions) HasItems() bool {
	for _, item := range c.filtered {
		if sk, ok := item.(skippable); !ok || !sk.Skip() {
			return true
		}
	}
	return false
}

// Update handles key events for the completions.
func (c *Completions) Update(msg tea.KeyPressMsg) (tea.Msg, bool) {
	if !c.open {
		return nil, false
	}

	switch {
	case key.Matches(msg, c.keyMap.Up):
		c.selectPrev()
		return nil, true

	case key.Matches(msg, c.keyMap.Down):
		c.selectNext()
		return nil, true

	case key.Matches(msg, c.keyMap.UpInsert):
		c.selectPrev()
		return c.selectCurrent(true), true

	case key.Matches(msg, c.keyMap.DownInsert):
		c.selectNext()
		return c.selectCurrent(true), true

	case key.Matches(msg, c.keyMap.Select):
		return c.selectCurrent(false), true

	case key.Matches(msg, c.keyMap.Cancel):
		c.Close()
		return ClosedMsg{}, true
	}

	return nil, false
}

// selectPrev selects the previous selectable item, skipping group headers.
func (c *Completions) selectPrev() {
	items := c.filtered
	n := len(items)
	if n == 0 {
		return
	}
	cur := c.list.Selected()
	if cur < 0 {
		cur = n
	}
	for i := 1; i <= n; i++ {
		idx := (cur - i + n) % n
		if sk, ok := items[idx].(skippable); !ok || !sk.Skip() {
			c.list.SetSelected(idx)
			c.list.ScrollToSelected()
			return
		}
	}
}

// selectNext selects the next selectable item, skipping group headers.
func (c *Completions) selectNext() {
	items := c.filtered
	n := len(items)
	if n == 0 {
		return
	}
	cur := c.list.Selected()
	if cur < 0 {
		cur = -1
	}
	for i := 1; i <= n; i++ {
		idx := (cur + i) % n
		if sk, ok := items[idx].(skippable); !ok || !sk.Skip() {
			c.list.SetSelected(idx)
			c.list.ScrollToSelected()
			return
		}
	}
}

// selectFirstReal selects the first non-skippable item in the list.
func (c *Completions) selectFirstReal() {
	for i, item := range c.filtered {
		if sk, ok := item.(skippable); !ok || !sk.Skip() {
			c.list.SetSelected(i)
			c.list.ScrollToSelected()
			return
		}
	}
	c.list.SelectFirst()
}

// itemValuer is implemented by items that expose a typed value.
type itemValuer interface {
	Value() any
}

// selectCurrent returns a command with the currently selected item.
func (c *Completions) selectCurrent(keepOpen bool) tea.Msg {
	items := c.filtered
	if len(items) == 0 {
		return nil
	}

	selected := c.list.Selected()
	if selected < 0 || selected >= len(items) {
		return nil
	}

	valuer, ok := items[selected].(itemValuer)
	if !ok {
		return nil
	}

	if !keepOpen {
		c.open = false
	}

	switch v := valuer.Value().(type) {
	case ResourceCompletionValue:
		return SelectionMsg[ResourceCompletionValue]{
			Value:    v,
			KeepOpen: keepOpen,
		}
	case FileCompletionValue:
		return SelectionMsg[FileCompletionValue]{
			Value:    v,
			KeepOpen: keepOpen,
		}
	case SlashCompletionValue:
		return SelectionMsg[SlashCompletionValue]{
			Value: v,
		}
	case AtCompletionValue:
		return SelectionMsg[AtCompletionValue]{
			Value:    v,
			KeepOpen: keepOpen,
		}
	default:
		return nil
	}
}

// Render renders the completions popup.
func (c *Completions) Render() string {
	if !c.open || !c.HasItems() {
		return ""
	}

	listOutput := c.list.List.Render()

	if c.slashStyles != nil {
		return c.renderSlashContainer(listOutput)
	}

	if c.slashHeaderHeight > 0 {
		return c.renderSlashHeader(c.width) + "\n" + listOutput
	}

	return listOutput
}

func loadFiles(depth, limit int) []FileCompletionValue {
	files, _, _ := fsext.ListDirectory(".", nil, depth, limit)
	slices.Sort(files)
	result := make([]FileCompletionValue, 0, len(files))
	for _, file := range files {
		result = append(result, FileCompletionValue{
			Path: strings.TrimPrefix(file, "./"),
		})
	}
	return result
}

func loadMCPResources() []ResourceCompletionValue {
	var resources []ResourceCompletionValue
	for mcpName, mcpResources := range mcp.Resources() {
		for _, r := range mcpResources {
			resources = append(resources, ResourceCompletionValue{
				MCPName:  mcpName,
				URI:      r.URI,
				Title:    r.Name,
				MIMEType: r.MIMEType,
			})
		}
	}
	return resources
}

// sectionLabel maps an @ category to a human-readable section header. The
// header prefixes the items in that category when a bare "@" is typed, so the
// popup reads as grouped sections (MCP, Skills, Files, ...) rather than one
// flat list. Returns "" for unknown categories to suppress a header.
func sectionLabel(category, token string) string {
	switch category {
	case "mcp":
		return "MCP"
	case "skill":
		return "Skills"
	case "file":
		return "Files"
	case "dir":
		return "Directories"
	case "workflow":
		return "Workflows"
	default:
		return strings.TrimSuffix(strings.TrimPrefix(token, "@"), ":")
	}
}
