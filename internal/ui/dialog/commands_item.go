package dialog

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/package-register/mocode/internal/ui/styles"
)

// CommandItem wraps a uicmd.Command to implement the ListItem interface.
type CommandItem struct {
	id       string
	title    string
	shortcut string
	action   Action
	t        *styles.Styles
	m        fuzzy.Match
	cache    map[int]string
	focused  bool

	// parent is set when this item is a child of another CommandItem.
	// nil for root-level items.
	parent *CommandItem

	// Children holds submenu items. When non-nil, this item acts as a
	// parent folder; pressing Enter/Right or clicking opens the submenu.
	Children []*CommandItem
}

var _ ListItem = &CommandItem{}

// NewCommandItem creates a new CommandItem.
func NewCommandItem(t *styles.Styles, id, title, shortcut string, action Action) *CommandItem {
	return &CommandItem{
		id:       id,
		t:        t,
		title:    title,
		shortcut: shortcut,
		action:   action,
	}
}

// NewParentCommandItem creates a parent CommandItem whose children
// appear as a submenu. Pressing Enter/Right or clicking opens children.
func NewParentCommandItem(t *styles.Styles, id, title, shortcut string, children ...*CommandItem) *CommandItem {
	parent := &CommandItem{
		id:       id,
		t:        t,
		title:    title,
		shortcut: shortcut,
		action:   ActionOpenSubmenu{},
		Children: children,
	}
	for _, child := range children {
		child.t = t
		child.parent = parent
	}
	return parent
}

// HasChildren reports whether this item contains submenu children.
func (c *CommandItem) HasChildren() bool {
	return len(c.Children) > 0
}

// Filter implements ListItem.
func (c *CommandItem) Filter() string {
	if c.parent != nil {
		return c.parent.title + " " + c.title + " " + c.id + " " + c.Label()
	}
	return c.title + " " + c.id + " " + c.Label()
}

// ID implements ListItem.
func (c *CommandItem) ID() string {
	return c.id
}

// SetFocused implements ListItem.
func (c *CommandItem) SetFocused(focused bool) {
	if c.focused != focused {
		c.cache = nil
	}
	c.focused = focused
}

// SetMatch implements ListItem.
func (c *CommandItem) SetMatch(m fuzzy.Match) {
	c.cache = nil
	c.m = m
}

// Action returns the action associated with the command item.
func (c *CommandItem) Action() Action {
	return c.action
}

// Shortcut returns the shortcut associated with the command item.
func (c *CommandItem) Shortcut() string {
	return c.shortcut
}

func (c *CommandItem) Label() string {
	switch c.id {
	case "new_session":
		return "/new"
	case "switch_session":
		return "/history"
	case "switch_model":
		return "/models"
	case "switch_mode":
		return "/agents"
	case "skplan":
		return "/plan"
	case "skplan_start":
		return "/plan start"
	case "skplan_code":
		return "/plan code"
	case "code":
		return "/code"
	case "summarize":
		return "/summarize"
	case "export_markdown":
		return "/export-md"
	case "export_html":
		return "/export-html"
	case "toggle_thinking":
		return "/think"
	case "select_reasoning_effort":
		return "/reasoning"
	case "toggle_sidebar":
		return "/sidebar"
	case "file_picker":
		return "/file"
	case "open_external_editor":
		return "/editor"
	case "enable_docker_mcp":
		return "/mcp-enable"
	case "disable_docker_mcp":
		return "/mcp-disable"
	case "toggle_pills":
		return "/tasks"
	case "toggle_notifications":
		return "/notifications"
	case "toggle_yolo":
		return "/approve"
	case "wechat_login":
		return "/wechat"
	case "admin_panel":
		return "/admin"
	case "admin_start":
		return "/admin-start"
	case "admin_stop":
		return "/admin-stop"
	case "toggle_help":
		return "/help"
	case "init":
		return "/init"
	case "toggle_transparent":
		return "/theme"
	case "quit":
		return "/quit"
	}
	label := strings.TrimPrefix(c.id, "custom_")
	label = strings.TrimPrefix(label, "mcp_")
	label = strings.ReplaceAll(label, "_", "-")
	if label == "" {
		label = strings.ToLower(strings.ReplaceAll(c.title, " ", "-"))
	}
	return "/" + label
}

// Render implements ListItem.
func (c *CommandItem) Render(width int) string {
	if c.cache == nil {
		c.cache = make(map[int]string)
	}
	if cached, ok := c.cache[width]; ok {
		return cached
	}

	style := c.t.Dialog.NormalItem
	if c.focused {
		style = c.t.Dialog.SelectedItem
	}

	lineWidth := max(0, width-style.GetHorizontalFrameSize())
	labelWidth := 16
	label := ansi.Truncate(c.Label(), max(0, labelWidth), "…")
	labelGap := strings.Repeat(" ", max(1, labelWidth-lipgloss.Width(label)+1))

	shortcut := c.shortcut
	shortcutWidth := lipgloss.Width(shortcut)
	if shortcutWidth > 0 {
		shortcutWidth += 2
	}
	descWidth := max(0, lineWidth-labelWidth-shortcutWidth-2)
	if c.HasChildren() {
		descWidth -= 2 // space for " ▸" indicator
	}
	desc := ansi.Truncate(c.title, descWidth, "…")
	if c.HasChildren() {
		desc += " ▸"
	}

	var row string
	if c.focused {
		gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-lipgloss.Width(labelGap)-lipgloss.Width(desc)-shortcutWidth))
		if shortcut != "" {
			shortcut = " " + shortcut + " "
		}
		row = label + labelGap + desc + gap + shortcut
	} else {
		renderedLabel := c.t.Dialog.TitleText.Render(label)
		renderedDesc := c.t.Dialog.ListItem.InfoBlurred.Render(desc)
		renderedShortcut := ""
		if shortcut != "" {
			renderedShortcut = c.t.Dialog.ListItem.InfoBlurred.Render(" " + shortcut + " ")
		}
		gap := strings.Repeat(" ", max(0, lineWidth-lipgloss.Width(label)-lipgloss.Width(labelGap)-lipgloss.Width(desc)-shortcutWidth))
		row = renderedLabel + labelGap + renderedDesc + gap + renderedShortcut
	}

	rendered := style.Render(row)
	c.cache[width] = rendered
	return rendered
}
