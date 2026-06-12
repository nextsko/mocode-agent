package dialog

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"charm.land/lipgloss/v2"
	"charm.land/bubbles/v2/key"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
)

// commandsRadioView generates the command type selector radio buttons.
func commandsRadioView(sty *styles.Styles, selected CommandType, hasUserCmds bool, hasMCPPrompts bool) string {
	if !hasUserCmds && !hasMCPPrompts {
		return ""
	}

	selectedFn := func(t CommandType) string {
		if t == selected {
			return sty.Radio.On.Padding(0, 1).Render() + sty.Radio.Label.Render(t.String())
		}
		return sty.Radio.Off.Padding(0, 1).Render() + sty.Radio.Label.Render(t.String())
	}

	parts := []string{
		selectedFn(SystemCommands),
	}

	if hasUserCmds {
		parts = append(parts, selectedFn(UserCommands))
	}
	if hasMCPPrompts {
		parts = append(parts, selectedFn(MCPPrompts))
	}

	return strings.Join(parts, " ")
}

// Draw implements [Dialog].
func (c *Commands) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.com.Styles
	availableWidth := max(0, area.Dx()-t.Dialog.View.GetHorizontalBorderSize())
	availableHeight := max(0, area.Dy()-t.Dialog.View.GetVerticalBorderSize())
	width := min(commandPaletteMaxWidth, availableWidth)
	if availableWidth >= commandPaletteMinWidth {
		width = max(commandPaletteMinWidth, width)
	}
	height := min(commandPaletteMaxHeight, availableHeight)
	if availableHeight >= 12 {
		height = max(12, height)
	}
	if area.Dx() != c.windowWidth && c.selected == SystemCommands {
		c.windowWidth = area.Dx()
		// since some items in the list depend on width (e.g. toggle sidebar command),
		// we need to reset the command items when width changes
		c.setCommandItems(c.selected)
	}

	innerWidth := width - c.com.Styles.Dialog.View.GetHorizontalFrameSize()
	inputStyle := commandPaletteInputStyle(t)
	heightOffset := inputStyle.GetVerticalFrameSize() +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize() + 4

	c.input.SetWidth(max(0, innerWidth-inputStyle.GetHorizontalFrameSize()-1))

	c.list.SetSize(innerWidth, max(1, height-heightOffset))
	c.help.SetWidth(innerWidth)

	titleInfo := commandsRadioView(t, c.selected, len(c.customCommands) > 0, len(c.mcpPrompts) > 0)
	title := commandPaletteHeader(t, c.breadcrumb(), titleInfo, innerWidth)
	separator := commandPaletteSeparator(t, innerWidth)
	inputView := inputStyle.Width(innerWidth).Render(c.input.View())
	listView := t.Dialog.List.Height(c.list.Height()).Render(c.list.Render())
	helpView := c.help.View(c)

	if c.loading {
		helpView = c.spinner.View() + " Generating Prompt..."
	}
	helpView = t.Dialog.HelpView.Width(innerWidth).Render(helpView)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		separator,
		inputView,
		separator,
		listView,
		separator,
		helpView,
	)
	view := t.Dialog.View.Width(width).Render(content)

	// Store layout rects for mouse click handling.
	c.lastArea = common.CenterRect(area, width, height)
	c.lastListArea = image.Rect(
		c.lastArea.Min.X+t.Dialog.View.GetBorderLeftSize()+t.Dialog.View.GetPaddingLeft(),
		c.lastArea.Min.Y+t.Dialog.View.GetBorderTopSize()+t.Dialog.View.GetPaddingTop()+titleHeight(t, title, separator, inputView, innerWidth),
		c.lastArea.Max.X-t.Dialog.View.GetBorderRightSize()-t.Dialog.View.GetPaddingRight(),
		c.lastArea.Min.Y+t.Dialog.View.GetBorderTopSize()+t.Dialog.View.GetPaddingTop()+titleHeight(t, title, separator, inputView, innerWidth)+c.list.Height(),
	)

	cur := c.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

func titleHeight(t *styles.Styles, title, separator, inputView string, innerWidth int) int {
	total := 0
	total += lipgloss.Height(title)
	total += lipgloss.Height(separator)
	total += lipgloss.Height(inputView)
	total += lipgloss.Height(separator)
	return total
}

func commandPaletteInputStyle(t *styles.Styles) lipgloss.Style {
	return t.Dialog.InputPrompt.Margin(0, 1, 1, 1)
}

func commandPaletteHeader(t *styles.Styles, title, info string, width int) string {
	title = t.Dialog.TitleText.Render(title)
	if info == "" {
		return lipgloss.NewStyle().Width(width).Render(title)
	}
	gap := strings.Repeat(" ", max(1, width-lipgloss.Width(title)-lipgloss.Width(info)))
	return lipgloss.NewStyle().Width(width).Render(title + gap + info)
}


func commandPaletteSeparator(t *styles.Styles, width int) string {
	return t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", max(0, width)))
}

// ShortHelp implements [help.KeyMap].
func (c *Commands) ShortHelp() []key.Binding {
	if c.isInSubmenu() {
		return []key.Binding{
			c.keyMap.Left,
			c.keyMap.UpDown,
			c.keyMap.Select,
			c.keyMap.Close,
		}
	}
	return []key.Binding{
		c.keyMap.Tab,
		c.keyMap.UpDown,
		c.keyMap.Select,
		c.keyMap.Close,
	}
}

// FullHelp implements [help.KeyMap].
func (c *Commands) FullHelp() [][]key.Binding {
	if c.isInSubmenu() {
		return [][]key.Binding{
			{c.keyMap.Select, c.keyMap.Next, c.keyMap.Previous, c.keyMap.Left},
			{c.keyMap.Close},
		}
	}
	return [][]key.Binding{
		{c.keyMap.Select, c.keyMap.Next, c.keyMap.Previous, c.keyMap.Tab},
		{c.keyMap.Close},
	}
}

// breadcrumb returns the navigation path string.
func (c *Commands) breadcrumb() string {
	if len(c.navStack) == 0 {
		return "Commands"
	}
	parts := make([]string, 0, len(c.navStack)+1)
	parts = append(parts, "Commands")
	for _, item := range c.navStack {
		parts = append(parts, item.title)
	}
	return strings.Join(parts, " ▸ ")
}

