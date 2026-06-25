package dialog

import (
	"image"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/list"
	"github.com/package-register/mocode/internal/ui/styles"
)

const (
	// HelpID is the identifier for the help dialog.
	HelpID = "help"

	helpDialogMaxWidth  = 96
	helpDialogMaxHeight = 26
	helpLeftPanelWidth  = 22
)

// helpCategory represents a category of key bindings in the help dialog.
type helpCategory struct {
	name     string
	bindings []helpBinding
}

// helpBinding is a single key + description row shown in the right panel.
type helpBinding struct {
	keys string
	desc string
}

// Help represents the help dialog showing key bindings organised by category.
type Help struct {
	com    *common.Common
	list   *list.FilterableList
	keyMap struct {
		UpDown   key.Binding
		Next     key.Binding
		Previous key.Binding
		Close    key.Binding
	}

	categories   []helpCategory
	selectedIdx  int
	lastArea     image.Rectangle
	lastListArea image.Rectangle
}

var _ Dialog = (*Help)(nil)

// NewHelp creates a new Help dialog.
func NewHelp(com *common.Common) *Help {
	h := &Help{com: com}

	h.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n", "j"),
		key.WithHelp("↓", "next"),
	)
	h.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p", "k"),
		key.WithHelp("↑", "previous"),
	)
	h.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	h.keyMap.Close = CloseKey

	h.categories = buildHelpCategories()
	h.buildList()

	return h
}

// ID implements Dialog.
func (h *Help) ID() string {
	return HelpID
}

// HandleMsg implements Dialog.
func (h *Help) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, h.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, h.keyMap.Previous):
			h.list.Focus()
			if h.list.IsSelectedFirst() {
				h.list.SelectLast()
			} else {
				h.list.SelectPrev()
			}
			h.list.ScrollToSelected()
			h.selectedIdx = h.list.Selected()
		case key.Matches(msg, h.keyMap.Next):
			h.list.Focus()
			if h.list.IsSelectedLast() {
				h.list.SelectFirst()
			} else {
				h.list.SelectNext()
			}
			h.list.ScrollToSelected()
			h.selectedIdx = h.list.Selected()
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if h.list.IsSelectedFirst() {
				h.list.SelectLast()
			} else {
				h.list.SelectPrev()
			}
			h.list.ScrollToSelected()
			h.selectedIdx = h.list.Selected()
		case tea.MouseWheelDown:
			if h.list.IsSelectedLast() {
				h.list.SelectFirst()
			} else {
				h.list.SelectNext()
			}
			h.list.ScrollToSelected()
			h.selectedIdx = h.list.Selected()
		}
	case tea.MouseClickMsg:
		if !image.Pt(msg.X, msg.Y).In(h.lastArea) {
			return nil
		}
		if !image.Pt(msg.X, msg.Y).In(h.lastListArea) {
			return nil
		}
		idx, _ := h.list.ItemIndexAtPosition(msg.X-h.lastListArea.Min.X, msg.Y-h.lastListArea.Min.Y)
		if idx >= 0 {
			h.list.SetSelected(idx)
			h.list.ScrollToSelected()
			h.selectedIdx = idx
		}
	}
	return nil
}

// Draw implements Dialog.
func (h *Help) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := h.com.Styles
	width := max(0, min(helpDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(helpDialogMaxHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	heightOffset := t.Dialog.Title.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize() +
		titleContentHeight + 1

	listHeight := max(1, height-heightOffset)

	leftWidth := min(helpLeftPanelWidth, max(16, innerWidth/4))
	rightWidth := max(24, innerWidth-leftWidth-1)
	compact := innerWidth < 60
	if compact {
		leftWidth = innerWidth
		rightWidth = innerWidth
		listHeight = max(3, listHeight/2)
	}

	h.list.SetSize(leftWidth, listHeight)
	if h.list.Height() < len(h.list.FilteredItems()) {
		h.list.ScrollToSelected()
	}

	rc := NewRenderContext(t, width)
	rc.Title = "Help & Key Bindings"

	listView := helpPanel(t, "Categories", h.list.Render(), leftWidth, listHeight)
	bindingsView := h.renderBindingsPanel(t, rightWidth, listHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, listView, " ", bindingsView)
	if compact {
		body = lipgloss.JoinVertical(lipgloss.Left, listView, bindingsView)
	}
	rc.AddPart(body)
	rc.Help = "esc close"

	view := rc.Render()
	w, hvv := lipgloss.Size(view)
	center := common.CenterRect(area, w, hvv)
	h.lastArea = center
	h.lastListArea = h.listArea(center, leftWidth, listHeight)

	uv.NewStyledString(view).Draw(scr, center)
	return nil
}

func (h *Help) listArea(dialogArea image.Rectangle, leftWidth, listHeight int) image.Rectangle {
	t := h.com.Styles
	x := dialogArea.Min.X + t.Dialog.View.GetBorderLeftSize() + t.Dialog.View.GetPaddingLeft()
	y := dialogArea.Min.Y +
		t.Dialog.View.GetBorderTopSize() + t.Dialog.View.GetPaddingTop() +
		t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight + 2
	return image.Rect(x, y, x+leftWidth, y+listHeight)
}

func (h *Help) buildList() {
	items := make([]list.FilterableItem, len(h.categories))
	for i, cat := range h.categories {
		items[i] = newHelpCategoryItem(h.com.Styles, cat.name)
	}
	h.list = list.NewFilterableList(items...)
	h.list.Focus()
	h.list.SetSelected(0)
}

func (h *Help) renderBindingsPanel(t *styles.Styles, width, height int) string {
	if h.selectedIdx < 0 || h.selectedIdx >= len(h.categories) {
		return lipgloss.NewStyle().Width(width).Height(height).Render("")
	}
	cat := h.categories[h.selectedIdx]
	contentWidth := max(16, width-2)

	header := t.Dialog.TitleText.Render(ansi.Truncate(cat.name, contentWidth, "…"))
	line := t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", contentWidth))

	var rows []string
	rows = append(rows, header, line)
	for _, b := range cat.bindings {
		rows = append(rows, renderHelpRow(t, b.keys, b.desc, contentWidth))
	}

	content := strings.Join(rows, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).PaddingLeft(1).Render(content)
}

func renderHelpRow(t *styles.Styles, keys, desc string, width int) string {
	keyStyle := t.Dialog.TitleAccent
	descStyle := t.Dialog.ListItem.InfoBlurred

	renderedKey := keyStyle.Render(keys)
	keyWidth := lipgloss.Width(renderedKey)
	gap := strings.Repeat(" ", max(1, 16-keyWidth))
	descWidth := max(0, width-keyWidth-len(gap)-1)
	renderedDesc := descStyle.Render(ansi.Truncate(desc, descWidth, "…"))
	return renderedKey + gap + renderedDesc
}

func helpPanel(t *styles.Styles, title, content string, width, height int) string {
	innerWidth := max(0, width-2)
	header := t.Dialog.TitleText.Render(ansi.Truncate(title, innerWidth, "…"))
	line := t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", innerWidth))
	body := lipgloss.JoinVertical(lipgloss.Left, header, line, content)
	return lipgloss.NewStyle().Width(width).Height(height).Render(body)
}

// helpCategoryItem is a selectable category row in the left sidebar.
type helpCategoryItem struct {
	name    string
	t       *styles.Styles
	focused bool
	cache   map[int]string
}

var _ ListItem = (*helpCategoryItem)(nil)

func newHelpCategoryItem(t *styles.Styles, name string) *helpCategoryItem {
	return &helpCategoryItem{name: name, t: t, cache: make(map[int]string)}
}

func (i *helpCategoryItem) Filter() string { return i.name }
func (i *helpCategoryItem) ID() string     { return i.name }

func (i *helpCategoryItem) SetFocused(focused bool) {
	if i.focused != focused {
		i.cache = nil
	}
	i.focused = focused
}

func (i *helpCategoryItem) SetMatch(_ fuzzy.Match) {}

func (i *helpCategoryItem) Render(width int) string {
	if i.cache == nil {
		i.cache = make(map[int]string)
	}
	if cached, ok := i.cache[width]; ok {
		return cached
	}

	style := i.t.Dialog.NormalItem
	if i.focused {
		style = i.t.Dialog.SelectedItem
	}

	lineWidth := max(0, width-style.GetHorizontalFrameSize())
	title := ansi.Truncate(i.name, lineWidth, "…")
	if !i.focused {
		title = i.t.Dialog.ListItem.InfoBlurred.Render(title)
	}
	rendered := style.Render(title)
	i.cache[width] = rendered
	return rendered
}

func buildHelpCategories() []helpCategory {
	return []helpCategory{
		{
			name: "Global",
			bindings: []helpBinding{
				{keys: "ctrl+c", desc: "quit"},
				{keys: "ctrl+p", desc: "slash"},
				{keys: "ctrl+l", desc: "switch model"},
				{keys: "ctrl+s", desc: "open sessions"},
				{keys: "ctrl+n", desc: "new session"},
				{keys: "ctrl+z", desc: "suspend"},
				{keys: "tab", desc: "change focus"},
			},
		},
		{
			name: "Editor",
			bindings: []helpBinding{
				{keys: "enter", desc: "send message"},
				{keys: "shift+enter", desc: "newline"},
				{keys: "ctrl+j", desc: "newline (alt)"},
				{keys: "@", desc: "mention file / resource"},
				{keys: "ctrl+f", desc: "add image"},
				{keys: "ctrl+v", desc: "paste image from clipboard"},
				{keys: "ctrl+o", desc: "open external editor"},
				{keys: "ctrl+a", desc: "select all"},
				{keys: "ctrl+x", desc: "cut"},
				{keys: "ctrl+r", desc: "attachment delete mode"},
				{keys: "↑/↓", desc: "prompt history"},
			},
		},
		{
			name: "Chat",
			bindings: []helpBinding{
				{keys: "↑↓", desc: "scroll"},
				{keys: "shift+↑↓", desc: "scroll one item"},
				{keys: "f / pgdn", desc: "page down"},
				{keys: "b / pgup", desc: "page up"},
				{keys: "d", desc: "half page down"},
				{keys: "u", desc: "half page up"},
				{keys: "g", desc: "home"},
				{keys: "G", desc: "end"},
				{keys: "c / y", desc: "copy message"},
				{keys: "space", desc: "expand / collapse"},
				{keys: "esc", desc: "cancel / clear selection"},
				{keys: "ctrl+t", desc: "toggle tasks / queue"},
			},
		},
		{
			name: "Slash / Agent",
			bindings: []helpBinding{
				{keys: "/plan", desc: "switch to SK Plan mode"},
				{keys: "/code", desc: "switch to Code mode"},
				{keys: "/agents", desc: "switch agent mode"},
				{keys: "/models", desc: "switch model"},
			},
		},
		{
			name: "Slash / Session",
			bindings: []helpBinding{
				{keys: "/new", desc: "new session"},
				{keys: "/history", desc: "browse past sessions"},
				{keys: "/init", desc: "initialize project"},
				{keys: "/summarize", desc: "summarize current session"},
				{keys: "/export-md", desc: "export session as Markdown"},
				{keys: "/export-html", desc: "export session as HTML"},
				{keys: "/sidebar", desc: "toggle sidebar (wide terminals)"},
				{keys: "/tasks", desc: "open Todo manager"},
				{keys: "/init_kng", desc: "initialize kng knowledge templates"},
			},
		},
		{
			name: "Slash / Tools",
			bindings: []helpBinding{
				{keys: "/mcps", desc: "open MCP servers panel"},
				{keys: "/wechat", desc: "connect WeChat"},
				{keys: "/file", desc: "open file picker (image models)"},
				{keys: "/editor", desc: "open external editor ($EDITOR)"},
			},
		},
		{
			name: "Slash / Settings",
			bindings: []helpBinding{
				{keys: "/approve", desc: "toggle auto-approve (Yolo mode)"},
				{keys: "/notifications", desc: "toggle notifications"},
				{keys: "/theme", desc: "toggle transparent background"},
				{keys: "/think", desc: "toggle thinking mode"},
				{keys: "/reasoning", desc: "select reasoning effort"},
				{keys: "/help", desc: "open this help dialog"},
				{keys: "/quit", desc: "quit the application"},
			},
		},
	}
}
