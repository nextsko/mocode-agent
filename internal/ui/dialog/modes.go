package dialog

import (
	"fmt"
	"image"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/list"
	"github.com/package-register/mocode/internal/ui/styles"
)

const (
	// ModesID is the identifier for the agent mode selection dialog.
	ModesID              = "modes"
	modesDialogMaxWidth  = 104
	modesDialogMaxHeight = 26
)

// ActionSelectMode is a message indicating an agent mode has been selected.
type ActionSelectMode struct {
	ModeID string
}

// Modes represents a dialog for selecting the active agent mode.
type Modes struct {
	com   *common.Common
	list  *list.FilterableList
	input textinput.Model
	help  help.Model

	keyMap struct {
		UpDown   key.Binding
		Select   key.Binding
		Next     key.Binding
		Previous key.Binding
		Close    key.Binding
	}

	modeIDs      []string
	lastArea     image.Rectangle
	lastListArea image.Rectangle
}

var _ Dialog = (*Modes)(nil)

// NewModes creates a new Modes dialog.
func NewModes(com *common.Common) (*Modes, error) {
	m := &Modes{com: com}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	m.help = h

	m.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	m.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	m.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	m.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	m.keyMap.Close = CloseKey

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Type to filter agents"
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	if err := m.setModeItems(); err != nil {
		return nil, err
	}

	return m, nil
}

// ID implements Dialog.
func (m *Modes) ID() string {
	return ModesID
}

// HandleMsg implements Dialog.
func (m *Modes) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, m.keyMap.Previous):
			m.list.Focus()
			if m.list.IsSelectedFirst() {
				m.list.SelectLast()
			} else {
				m.list.SelectPrev()
			}
			m.list.ScrollToSelected()
		case key.Matches(msg, m.keyMap.Next):
			m.list.Focus()
			if m.list.IsSelectedLast() {
				m.list.SelectFirst()
			} else {
				m.list.SelectNext()
			}
			m.list.ScrollToSelected()
		case key.Matches(msg, m.keyMap.Select):
			selectedItem := m.list.SelectedItem()
			if selectedItem == nil {
				break
			}
			modeItem, ok := selectedItem.(*ModeItem)
			if !ok {
				break
			}
			return ActionSelectMode{ModeID: modeItem.ModeID()}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.list.SetFilter(m.input.Value())
			m.list.ScrollToTop()
			m.list.SetSelected(0)
			return ActionCmd{Cmd: cmd}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.moveSelection(-1)
		case tea.MouseWheelDown:
			m.moveSelection(1)
		}
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	}
	return nil
}

func (m *Modes) Cursor() *tea.Cursor {
	return InputCursor(m.com.Styles, m.input.Cursor())
}

// Draw implements Dialog.
func (m *Modes) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles
	width := max(0, min(modesDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(modesDialogMaxHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	heightOffset := t.Dialog.Title.GetVerticalFrameSize() +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize() + titleContentHeight

	listHeight := max(1, height-heightOffset)
	compact := innerWidth < 76
	leftWidth := min(34, max(24, innerWidth/3))
	rightWidth := max(24, innerWidth-leftWidth-1)
	if compact {
		leftWidth = innerWidth
		rightWidth = innerWidth
		listHeight = max(4, listHeight/2)
	}

	m.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	m.list.SetSize(leftWidth, listHeight)
	m.help.SetWidth(innerWidth)

	if m.list.Height() < len(m.list.FilteredItems()) {
		m.list.ScrollToSelected()
	}

	rc := NewRenderContext(t, width)
	rc.Title = "Switch Agent"

	inputView := t.Dialog.InputPrompt.Render(m.input.View())
	rc.AddPart(inputView)
	listView := modePanel(t, "Available Agents", m.list.Render(), leftWidth, listHeight)
	profileView := renderModeProfile(t, m.selectedModeItem(), rightWidth, listHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, listView, " ", profileView)
	if compact {
		body = lipgloss.JoinVertical(lipgloss.Left, listView, profileView)
	}
	rc.AddPart(body)

	rc.Help = m.help.View(m)

	view := rc.Render()
	w, h := lipgloss.Size(view)
	center := common.CenterRect(area, w, h)
	m.lastArea = center
	m.lastListArea = m.listArea(center, leftWidth, listHeight)
	cur := m.Cursor()
	if cur != nil {
		cur.X += center.Min.X
		cur.Y += center.Min.Y
	}
	uv.NewStyledString(view).Draw(scr, center)
	return cur
}

// ShortHelp implements help.KeyMap.
func (m *Modes) ShortHelp() []key.Binding {
	return []key.Binding{
		m.keyMap.UpDown,
		m.keyMap.Select,
		m.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (m *Modes) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{m.keyMap.Select, m.keyMap.Next, m.keyMap.Previous, m.keyMap.Close},
	}
}

func (m *Modes) moveSelection(delta int) {
	if delta < 0 {
		if m.list.IsSelectedFirst() {
			m.list.SelectLast()
		} else {
			m.list.SelectPrev()
		}
	} else if delta > 0 {
		if m.list.IsSelectedLast() {
			m.list.SelectFirst()
		} else {
			m.list.SelectNext()
		}
	}
	m.list.Focus()
	m.list.ScrollToSelected()
}

func (m *Modes) handleMouseClick(msg tea.MouseClickMsg) Action {
	if !image.Pt(msg.X, msg.Y).In(m.lastArea) {
		return nil
	}
	if !image.Pt(msg.X, msg.Y).In(m.lastListArea) {
		return nil
	}
	idx, _ := m.list.ItemIndexAtPosition(msg.X-m.lastListArea.Min.X, msg.Y-m.lastListArea.Min.Y)
	if idx < 0 {
		return nil
	}
	m.list.SetSelected(idx)
	m.list.ScrollToSelected()
	if item := m.selectedModeItem(); item != nil {
		return ActionSelectMode{ModeID: item.ModeID()}
	}
	return nil
}

func (m *Modes) listArea(dialogArea image.Rectangle, leftWidth, listHeight int) image.Rectangle {
	t := m.com.Styles
	x := dialogArea.Min.X + t.Dialog.View.GetBorderLeftSize() + t.Dialog.View.GetPaddingLeft()
	y := dialogArea.Min.Y +
		t.Dialog.View.GetBorderTopSize() + t.Dialog.View.GetPaddingTop() +
		t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		2
	return image.Rect(x, y, x+leftWidth, y+listHeight)
}

func (m *Modes) setModeItems() error {
	cfg := m.com.Config()
	activeMode := config.AgentDefault
	if cfg.Options != nil && cfg.Options.ActiveMode != "" {
		activeMode = cfg.Options.ActiveMode
	}

	ids := make([]string, 0, len(cfg.Agents))
	for id, agent := range cfg.Agents {
		if agent.Disabled {
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)

	items := make([]list.FilterableItem, 0, len(ids))
	selectedIndex := 0
	for i, id := range ids {
		agent := cfg.Agents[id]
		item := NewModeItem(m.com.Styles, id, agent, id == activeMode)
		items = append(items, item)
		if id == activeMode {
			selectedIndex = i
		}
	}

	m.modeIDs = ids
	m.list = list.NewFilterableList(items...)
	m.list.Focus()
	m.list.SetSelected(selectedIndex)
	m.list.ScrollToSelected()
	return nil
}

func (m *Modes) selectedModeItem() *ModeItem {
	selectedItem := m.list.SelectedItem()
	if selectedItem == nil {
		return nil
	}
	modeItem, ok := selectedItem.(*ModeItem)
	if !ok {
		return nil
	}
	return modeItem
}

func modePanel(t *styles.Styles, title, content string, width, height int) string {
	innerWidth := max(0, width-2)
	header := t.Dialog.TitleText.Render(ansi.Truncate(title, innerWidth, "…"))
	line := t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", innerWidth))
	body := lipgloss.JoinVertical(lipgloss.Left, header, line, content)
	return lipgloss.NewStyle().Width(width).Height(height).Render(body)
}

func renderModeProfile(t *styles.Styles, item *ModeItem, width, height int) string {
	if item == nil {
		return lipgloss.NewStyle().Width(width).Height(height).Render(t.Dialog.ListItem.InfoBlurred.Render("No agent selected"))
	}

	agent := item.Agent()
	contentWidth := max(16, width-2)
	description := agent.Description
	if description == "" {
		description = "No description configured for this agent."
	}

	tools := "All tools"
	if len(agent.AllowedTools) > 0 {
		tools = fmt.Sprintf("%d tools", len(agent.AllowedTools))
	}
	mcpScope := "Default MCP"
	if len(agent.AllowedMCP) > 0 {
		mcpScope = fmt.Sprintf("%d MCP servers", len(agent.AllowedMCP))
	}
	contextScope := "Workspace"
	if len(agent.ContextPaths) > 0 {
		contextScope = fmt.Sprintf("%d context paths", len(agent.ContextPaths))
	}

	toolScope := "All configured tools are available."
	if len(agent.AllowedTools) > 0 {
		visibleTools := agent.AllowedTools
		if len(visibleTools) > 5 {
			visibleTools = visibleTools[:5]
		}
		toolScope = strings.Join(visibleTools, ", ")
		if len(agent.AllowedTools) > len(visibleTools) {
			toolScope += fmt.Sprintf(", +%d more", len(agent.AllowedTools)-len(visibleTools))
		}
	}

	header := t.Dialog.TitleText.Render("Profile")
	if item.Active() {
		badge := t.Dialog.TitleAccent.Render("current")
		gap := strings.Repeat(" ", max(1, contentWidth-lipgloss.Width(header)-lipgloss.Width(badge)))
		header += gap + badge
	}
	line := t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", contentWidth))
	parts := []string{
		header,
		line,
		t.Dialog.TitleAccent.Render(ansi.Truncate(item.Title(), contentWidth, "…")),
		t.Dialog.ListItem.InfoBlurred.Render(ansi.Truncate(item.ModeID(), contentWidth, "…")),
		"",
		modeProfileSection(t, "Overview", contentWidth),
		t.Dialog.ListItem.InfoBlurred.Render(ansi.Truncate(description, contentWidth, "…")),
		"",
		modeProfileSection(t, "Quick Facts", contentWidth),
		modeProfileRow(t, "Model", string(agent.Model), contentWidth),
		modeProfileRow(t, "Tools", tools, contentWidth),
		modeProfileRow(t, "MCP", mcpScope, contentWidth),
		modeProfileRow(t, "Context", contextScope, contentWidth),
		"",
		modeProfileSection(t, "Tool Scope", contentWidth),
		t.Dialog.ListItem.InfoBlurred.Render(ansi.Truncate(toolScope, contentWidth, "…")),
	}

	return lipgloss.NewStyle().Width(width).Height(height).PaddingLeft(1).Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func modeProfileSection(t *styles.Styles, title string, width int) string {
	title = t.Dialog.TitleText.Render(title)
	lineWidth := max(0, width-lipgloss.Width(title)-1)
	return title + " " + t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", lineWidth))
}

func modeProfileRow(t *styles.Styles, label, value string, width int) string {
	label = t.Dialog.ListItem.InfoBlurred.Render(label)
	valueWidth := max(0, width-lipgloss.Width(label)-2)
	value = t.Dialog.TitleText.Render(ansi.Truncate(value, valueWidth, "…"))
	return label + ": " + value
}
