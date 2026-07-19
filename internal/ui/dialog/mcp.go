package dialog

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/ui/common"
	mcptools "github.com/package-register/mocode/tools/mcp"
)

const MCPID = "mcp"

type ActionToggleMCP struct {
	Name    string
	Enable  bool
	Current bool
}

type mcpPanelView int

const (
	mcpPanelServers mcpPanelView = iota
	mcpPanelDetails
	mcpPanelTools
)

type MCP struct {
	com        *common.Common
	states     map[string]mcptools.ClientInfo
	view       mcpPanelView
	selected   int
	scroll     int
	toolSel    int
	toolScroll int
	help       help.Model
	lastArea   image.Rectangle

	keyMap struct {
		UpDown,
		Next,
		Previous,
		Toggle,
		Enter,
		Left,
		Right,
		Close key.Binding
	}
}

var _ Dialog = (*MCP)(nil)

func NewMCP(com *common.Common, states map[string]mcptools.ClientInfo) *MCP {
	m := &MCP{com: com, states: states}
	m.help = help.New()
	m.help.Styles = com.Styles.DialogHelpStyles()
	m.keyMap.UpDown = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "choose"))
	m.keyMap.Next = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next"))
	m.keyMap.Previous = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "prev"))
	m.keyMap.Toggle = key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle"))
	m.keyMap.Enter = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "toggle"))
	m.keyMap.Left = key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "servers"))
	m.keyMap.Right = key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next tab"))
	closeKey := CloseKey
	closeKey.SetHelp("esc", "close")
	m.keyMap.Close = closeKey
	return m
}

func (m *MCP) ID() string {
	return MCPID
}

func (m *MCP) SetStates(states map[string]mcptools.ClientInfo) {
	m.states = states
	m.clampSelection()
}

func (m *MCP) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.Close):
			if m.view != mcpPanelServers {
				m.view = mcpPanelServers
				return nil
			}
			return ActionClose{}
		case key.Matches(msg, m.keyMap.Previous):
			m.moveSelection(-1)
		case key.Matches(msg, m.keyMap.Next):
			m.moveSelection(1)
		case key.Matches(msg, m.keyMap.Left):
			m.prevView()
		case key.Matches(msg, m.keyMap.Right):
			m.nextView()
		case key.Matches(msg, m.keyMap.Toggle, m.keyMap.Enter):
			if m.view != mcpPanelTools {
				return m.toggleAction()
			}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.moveSelection(-1)
		case tea.MouseWheelDown:
			m.moveSelection(1)
		}
	case tea.MouseClickMsg:
		if !image.Pt(msg.X, msg.Y).In(m.lastArea) {
			return nil
		}
		relY := msg.Y - m.lastArea.Min.Y - 5
		if relY >= 0 {
			idx := m.scroll + relY
			if idx >= 0 && idx < len(m.servers()) {
				m.selected = idx
				if msg.X > m.lastArea.Max.X-14 {
					return m.toggleAction()
				}
			}
		}
	}
	return nil
}

func (m *MCP) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	servers := m.servers()
	m.clampSelection()
	t := m.com.Styles
	width := min(92, max(48, area.Dx()-4))
	height := min(24, max(12, area.Dy()-4))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	innerHeight := height - t.Dialog.View.GetVerticalFrameSize()
	listHeight := max(1, innerHeight-6)
	m.ensureSelectedVisible(listHeight)

	titleInfo := t.Header.KeystrokeTip.Render(fmt.Sprintf("%d servers", len(servers)))
	if m.view == mcpPanelDetails {
		titleInfo = t.Header.KeystrokeTip.Render("details")
	}
	if m.view == mcpPanelTools {
		titleInfo = t.Header.KeystrokeTip.Render("tools")
	}
	rc := NewRenderContext(t, width)
	rc.Title = "MCP Servers"
	rc.TitleInfo = titleInfo
	rc.Gap = 1
	rc.AddPart(m.tabs(innerWidth))
	if m.view == mcpPanelDetails {
		rc.AddPart(m.detailsView(innerWidth, listHeight))
	} else if m.view == mcpPanelTools {
		rc.AddPart(m.toolsView(innerWidth, listHeight))
	} else {
		rc.AddPart(m.serversView(innerWidth, listHeight))
	}
	rc.Help = m.help.View(m)
	view := rc.Render()
	w, h := lipgloss.Size(view)
	center := common.CenterRect(area, w, h)
	m.lastArea = center
	uv.NewStyledString(view).Draw(scr, center)
	return nil
}

func (m *MCP) tabs(width int) string {
	t := m.com.Styles
	serversTab := t.Radio.Off.Padding(0, 1).Render() + t.Radio.Label.Render("Servers")
	detailsTab := t.Radio.Off.Padding(0, 1).Render() + t.Radio.Label.Render("Details")
	toolsTab := t.Radio.Off.Padding(0, 1).Render() + t.Radio.Label.Render("Tools")
	if m.view == mcpPanelServers {
		serversTab = t.Radio.On.Padding(0, 1).Render() + t.Radio.Label.Render("Servers")
	} else if m.view == mcpPanelDetails {
		detailsTab = t.Radio.On.Padding(0, 1).Render() + t.Radio.Label.Render("Details")
	} else {
		toolsTab = t.Radio.On.Padding(0, 1).Render() + t.Radio.Label.Render("Tools")
	}
	line := serversTab + " " + detailsTab + " " + toolsTab
	return ansi.Truncate(line, width, "…")
}

func (m *MCP) serversView(width, height int) string {
	servers := m.servers()
	if len(servers) == 0 {
		return m.com.Styles.Resource.AdditionalText.Render("No MCP servers configured")
	}
	end := min(len(servers), m.scroll+height)
	rows := make([]string, 0, end-m.scroll)
	for i := m.scroll; i < end; i++ {
		item := servers[i]
		rows = append(rows, m.serverRow(item, i == m.selected, width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *MCP) detailsView(width, height int) string {
	servers := m.servers()
	if len(servers) == 0 {
		return m.com.Styles.Resource.AdditionalText.Render("No MCP servers configured")
	}
	item := servers[m.selected]
	state, ok := m.states[item.Name]
	status := "configured"
	counts := mcptools.Counts{}
	if ok {
		status = state.State.String()
		counts = state.Counts
	}
	lines := []string{
		m.com.Styles.Resource.Name.Render(item.Name),
		m.com.Styles.Resource.StatusText.Render("status: " + status),
		m.com.Styles.Resource.CapabilityCount.Render(fmt.Sprintf("tools: %d", counts.Tools)),
		m.com.Styles.Resource.CapabilityCount.Render(fmt.Sprintf("prompts: %d", counts.Prompts)),
		m.com.Styles.Resource.CapabilityCount.Render(fmt.Sprintf("resources: %d", counts.Resources)),
		m.com.Styles.Resource.StatusText.Render("type: " + string(item.MCP.Type)),
	}
	if item.MCP.Command != "" {
		lines = append(lines, m.com.Styles.Resource.StatusText.Render("cmd: "+item.MCP.Command+" "+strings.Join(item.MCP.Args, " ")))
	}
	if item.MCP.URL != "" {
		lines = append(lines, m.com.Styles.Resource.StatusText.Render("url: "+item.MCP.URL))
	}
	if state.Error != nil {
		lines = append(lines, m.com.Styles.Tool.ErrorMessage.Render(state.Error.Error()))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "…")
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *MCP) toolsView(width, height int) string {
	item, ok := m.selectedServer()
	if !ok {
		return m.com.Styles.Resource.AdditionalText.Render("No MCP server selected")
	}
	tools := m.selectedTools(item.Name)
	if len(tools) == 0 {
		return m.com.Styles.Resource.AdditionalText.Render("No tools loaded for " + item.Name)
	}
	m.clampToolSelection(len(tools))
	if m.toolSel < m.toolScroll {
		m.toolScroll = m.toolSel
	}
	if m.toolSel >= m.toolScroll+height {
		m.toolScroll = m.toolSel - height + 1
	}
	m.toolScroll = max(0, m.toolScroll)
	end := min(len(tools), m.toolScroll+height)
	rows := make([]string, 0, end-m.toolScroll)
	for i := m.toolScroll; i < end; i++ {
		tool := tools[i]
		name := tool.Name
		if i == m.toolSel {
			name = m.com.Styles.Button.Focused.Render(name)
		} else {
			name = m.com.Styles.Tool.MCPToolName.Render(name)
		}
		description := strings.TrimSpace(tool.Description)
		if description == "" {
			description = "no description"
		}
		line := name + " " + m.com.Styles.Resource.StatusText.Render(description)
		if i == m.toolSel && tool.InputSchema != nil && width > 42 {
			if data, err := json.Marshal(tool.InputSchema); err == nil && len(data) > 0 {
				line += " " + m.com.Styles.Resource.CapabilityCount.Render(string(data))
			}
		}
		rows = append(rows, ansi.Truncate(line, width, "…"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *MCP) serverRow(item config.MCP, selected bool, width int) string {
	t := m.com.Styles
	state, ok := m.states[item.Name]
	icon := t.Resource.OfflineIcon.String()
	status := "configured"
	counts := ""
	if item.MCP.Disabled {
		icon = t.Resource.DisabledIcon.String()
		status = "disabled"
	}
	if ok {
		status = state.State.String()
		switch state.State {
		case mcptools.StateConnected:
			icon = t.Resource.OnlineIcon.String()
		case mcptools.StateStarting:
			icon = t.Resource.BusyIcon.String()
		case mcptools.StateError:
			icon = t.Resource.ErrorIcon.String()
		case mcptools.StateDisabled:
			icon = t.Resource.DisabledIcon.String()
		}
		counts = fmt.Sprintf(" %dt %dp %dr", state.Counts.Tools, state.Counts.Prompts, state.Counts.Resources)
	}
	toggle := "enable"
	if !item.MCP.Disabled && status != "disabled" {
		toggle = "disable"
	}
	name := t.Resource.Name.Render(item.Name)
	if selected {
		name = t.Button.Focused.Render(item.Name)
	}
	left := fmt.Sprintf("%s %s", icon, name)
	middle := t.Resource.StatusText.Render(status + counts)
	right := t.Header.Keystroke.Render(toggle)
	gap := strings.Repeat(" ", max(1, width-lipgloss.Width(left)-lipgloss.Width(middle)-lipgloss.Width(right)-2))
	return ansi.Truncate(left+" "+middle+gap+right, width, "…")
}

func (m *MCP) ShortHelp() []key.Binding {
	return []key.Binding{m.keyMap.UpDown, m.keyMap.Toggle, m.keyMap.Left, m.keyMap.Right, m.keyMap.Close}
}

func (m *MCP) FullHelp() [][]key.Binding {
	return [][]key.Binding{{m.keyMap.UpDown, m.keyMap.Toggle, m.keyMap.Left, m.keyMap.Right}, {m.keyMap.Close}}
}

func (m *MCP) servers() []config.MCP {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	return cfg.MCP.Sorted()
}

func (m *MCP) selectedServer() (config.MCP, bool) {
	servers := m.servers()
	if len(servers) == 0 || m.selected < 0 || m.selected >= len(servers) {
		return config.MCP{}, false
	}
	return servers[m.selected], true
}

func (m *MCP) toggleAction() Action {
	item, ok := m.selectedServer()
	if !ok {
		return nil
	}
	state, hasState := m.states[item.Name]
	enabled := !item.MCP.Disabled
	if hasState && state.State == mcptools.StateDisabled {
		enabled = false
	}
	return ActionToggleMCP{Name: item.Name, Enable: !enabled, Current: enabled}
}

func (m *MCP) moveSelection(delta int) {
	if m.view == mcpPanelTools {
		item, ok := m.selectedServer()
		if !ok {
			return
		}
		tools := m.selectedTools(item.Name)
		if len(tools) == 0 {
			m.toolSel = 0
			m.toolScroll = 0
			return
		}
		m.toolSel += delta
		if m.toolSel < 0 {
			m.toolSel = len(tools) - 1
		}
		if m.toolSel >= len(tools) {
			m.toolSel = 0
		}
		return
	}
	servers := m.servers()
	if len(servers) == 0 {
		m.selected = 0
		m.scroll = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = len(servers) - 1
	}
	if m.selected >= len(servers) {
		m.selected = 0
	}
	m.toolSel = 0
	m.toolScroll = 0
}

func (m *MCP) clampSelection() {
	servers := m.servers()
	if len(servers) == 0 {
		m.selected = 0
		m.scroll = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(servers) {
		m.selected = len(servers) - 1
	}
}

func (m *MCP) ensureSelectedVisible(height int) {
	if height <= 0 {
		m.scroll = 0
		return
	}
	if m.selected < m.scroll {
		m.scroll = m.selected
	}
	if m.selected >= m.scroll+height {
		m.scroll = m.selected - height + 1
	}
	m.scroll = max(0, m.scroll)
}

func (m *MCP) nextView() {
	m.view = (m.view + 1) % 3
}

func (m *MCP) prevView() {
	if m.view == mcpPanelServers {
		m.view = mcpPanelTools
		return
	}
	m.view--
}

func (m *MCP) selectedTools(name string) []*mcptools.Tool {
	for server, tools := range mcptools.Tools() {
		if server == name {
			return tools
		}
	}
	return nil
}

func (m *MCP) clampToolSelection(count int) {
	if count <= 0 {
		m.toolSel = 0
		m.toolScroll = 0
		return
	}
	if m.toolSel < 0 {
		m.toolSel = 0
	}
	if m.toolSel >= count {
		m.toolSel = count - 1
	}
}
