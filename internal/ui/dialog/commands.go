package dialog

import (
	"image"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/package-register/mocode/internal/capability"
	"github.com/package-register/mocode/internal/commands"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/list"
	"github.com/package-register/mocode/internal/ui/styles"
)

// CommandsID is the identifier for the commands dialog.
const CommandsID = "commands"

// CommandType represents the type of commands being displayed.
type CommandType uint

// String returns the string representation of the CommandType.
func (c CommandType) String() string { return []string{"System", "User", "MCP"}[c] }

const (
	sidebarCompactModeBreakpoint = 120
	commandPaletteMaxWidth       = 84
	commandPaletteMaxHeight      = 24
	commandPaletteMinWidth       = 56
)

const (
	SystemCommands CommandType = iota
	UserCommands
	MCPPrompts
)

// Commands represents a dialog that shows available commands.
type dockerMCPAvailabilityCheckedMsg struct {
	available bool
}

type Commands struct {
	com    *common.Common
	keyMap struct {
		Select,
		UpDown,
		Next,
		Previous,
		Tab,
		ShiftTab,
		Close,
		Left,
		Right key.Binding
	}

	sessionID  string
	hasSession bool
	hasTodos   bool
	hasQueue   bool
	selected   CommandType

	// navStack tracks the breadcrumb path for submenu navigation.
	// When non-empty, the last element is the current parent item.
	navStack []*CommandItem

	spinner spinner.Model
	loading bool

	help  help.Model
	input textinput.Model
	list  *list.FilterableList

	// allItems stores a flattened list of all commands (parents + children)
	// for cross-level fuzzy search when the user types a filter query.
	allItems     []list.FilterableItem
	filterActive bool

	windowWidth  int
	lastArea     image.Rectangle
	lastListArea image.Rectangle

	customCommands []commands.CustomCommand
	mcpPrompts     []commands.MCPPrompt
	registry       *capability.CommandRegistry

	dockerMCPAvailable     *bool
	dockerMCPCheckInFlight bool
}

var _ Dialog = (*Commands)(nil)

// NewCommands creates a new commands dialog.
func NewCommands(com *common.Common, sessionID string, hasSession, hasTodos, hasQueue bool, customCommands []commands.CustomCommand, mcpPrompts []commands.MCPPrompt) (*Commands, error) {
	c := &Commands{
		com:            com,
		selected:       SystemCommands,
		sessionID:      sessionID,
		hasSession:     hasSession,
		hasTodos:       hasTodos,
		hasQueue:       hasQueue,
		customCommands: customCommands,
		mcpPrompts:     mcpPrompts,
	}

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()

	c.help = help

	c.list = list.NewFilterableList()
	c.list.Focus()
	c.list.SetSelected(0)

	c.input = textinput.New()
	c.input.SetVirtualCursor(false)
	c.input.Placeholder = "Type to filter"
	c.input.SetStyles(com.Styles.TextInput)
	c.input.Focus()

	c.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "confirm"),
	)
	c.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	c.keyMap.Next = key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next item"),
	)
	c.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	c.keyMap.Tab = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch selection"),
	)
	c.keyMap.ShiftTab = key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "switch selection prev"),
	)
	closeKey := CloseKey
	closeKey.SetHelp("esc", "cancel")
	c.keyMap.Close = closeKey

	c.keyMap.Left = key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "back"),
	)
	c.keyMap.Right = key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "open"),
	)

	if available, known := config.DockerMCPAvailabilityCached(); known {
		c.dockerMCPAvailable = &available
	}

	// Set initial commands
	c.setCommandItems(c.selected)

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = com.Styles.Dialog.Spinner
	c.spinner = s

	return c, nil
}

// ID implements Dialog.
func (c *Commands) ID() string {
	return CommandsID
}

// HandleMsg implements [Dialog].
func (c *Commands) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case dockerMCPAvailabilityCheckedMsg:
		c.dockerMCPAvailable = &msg.available
		c.dockerMCPCheckInFlight = false
		if c.selected == SystemCommands {
			c.setCommandItems(c.selected)
		}
		return nil
	case spinner.TickMsg:
		if c.loading {
			var cmd tea.Cmd
			c.spinner, cmd = c.spinner.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if c.list.IsSelectedFirst() {
				c.list.SelectLast()
			} else {
				c.list.SelectPrev()
			}
			c.list.ScrollToSelected()
		case tea.MouseWheelDown:
			if c.list.IsSelectedLast() {
				c.list.SelectFirst()
			} else {
				c.list.SelectNext()
			}
			c.list.ScrollToSelected()
		}
	case tea.MouseClickMsg:
		return c.handleMouseClick(msg)
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, c.keyMap.Close):
			if c.isInSubmenu() {
				c.popSubmenu()
				return nil
			}
			return ActionClose{}
		case key.Matches(msg, c.keyMap.Left):
			if c.isInSubmenu() {
				c.popSubmenu()
			}
			return nil
		case key.Matches(msg, c.keyMap.Right):
			if selectedItem := c.list.SelectedItem(); selectedItem != nil {
				if item, ok := selectedItem.(*CommandItem); ok && item != nil && item.HasChildren() {
					c.pushSubmenu(item)
				}
			}
			return nil
		case key.Matches(msg, c.keyMap.Previous):
			c.list.Focus()
			if c.list.IsSelectedFirst() {
				c.list.SelectLast()
			} else {
				c.list.SelectPrev()
			}
			c.list.ScrollToSelected()
		case key.Matches(msg, c.keyMap.Next):
			c.list.Focus()
			if c.list.IsSelectedLast() {
				c.list.SelectFirst()
			} else {
				c.list.SelectNext()
			}
			c.list.ScrollToSelected()
		case key.Matches(msg, c.keyMap.Select):
			if selectedItem := c.list.SelectedItem(); selectedItem != nil {
				if item, ok := selectedItem.(*CommandItem); ok && item != nil {
					// If filtering at root level and selected a child item,
					// navigate into its parent submenu first.
					if c.filterActive && item.parent != nil && !c.isInSubmenu() {
						c.navStack = append(c.navStack, item.parent)
						c.pushSubmenu(item.parent)
						// Find and select the child in the submenu.
						for i, filterable := range c.list.FilteredItems() {
							if child, ok := filterable.(*CommandItem); ok && child == item {
								c.list.SetSelected(i)
								break
							}
						}
						c.list.ScrollToSelected()
						c.input.SetValue("")
						c.filterActive = false
						// If the selected child itself has children, let the user
						// navigate deeper; otherwise execute its action directly.
						if item.HasChildren() {
							return nil
						}
						return item.Action()
					}
					if item.HasChildren() {
						c.pushSubmenu(item)
						return nil
					}
					return item.Action()
				}
			}
		case key.Matches(msg, c.keyMap.Tab):
			if len(c.customCommands) > 0 || len(c.mcpPrompts) > 0 {
				c.selected = c.nextCommandType()
				c.setCommandItems(c.selected)
			}
		case key.Matches(msg, c.keyMap.ShiftTab):
			if len(c.customCommands) > 0 || len(c.mcpPrompts) > 0 {
				c.selected = c.previousCommandType()
				c.setCommandItems(c.selected)
			}
		default:
			var cmd tea.Cmd
			for _, item := range c.list.FilteredItems() {
				if item, ok := item.(*CommandItem); ok && item != nil {
					if msg.String() == item.Shortcut() {
						if item.HasChildren() {
							c.pushSubmenu(item)
							return nil
						}
						return item.Action()
					}
				}
			}
			c.input, cmd = c.input.Update(msg)
			value := c.input.Value()

			// Cross-level fuzzy search: when typing a filter at root level,
			// swap to the flattened allItems list so children are searchable.
			if !c.isInSubmenu() && c.allItems != nil {
				if value != "" && !c.filterActive {
					c.list.SetItems(c.allItems...)
					c.filterActive = true
				} else if value == "" && c.filterActive {
					// Restore root-level items when filter is cleared.
					items := c.defaultCommands()
					filterableItems := make([]list.FilterableItem, len(items))
					for i, item := range items {
						filterableItems[i] = item
					}
					c.list.SetItems(filterableItems...)
					c.filterActive = false
				}
			}

			c.list.SetFilter(value)
			c.list.ScrollToTop()
			c.list.SetSelected(0)
			return ActionCmd{cmd}
		}
	}
	return nil
}

func checkDockerMCPAvailabilityCmd() tea.Cmd {
	return func() tea.Msg {
		return dockerMCPAvailabilityCheckedMsg{available: config.RefreshDockerMCPAvailability()}
	}
}

func (c *Commands) InitialCmd() tea.Cmd {
	if c.dockerMCPAvailable != nil || c.dockerMCPCheckInFlight {
		return nil
	}
	c.dockerMCPCheckInFlight = true
	return checkDockerMCPAvailabilityCmd()
}

// Cursor returns the cursor position relative to the dialog.
func (c *Commands) Cursor() *tea.Cursor {
	cur := c.input.Cursor()
	if cur != nil {
		t := c.com.Styles
		dialogStyle := t.Dialog.View
		inputStyle := commandPaletteInputStyle(t)
		cur.X += inputStyle.GetBorderLeftSize() +
			inputStyle.GetMarginLeft() +
			inputStyle.GetPaddingLeft() +
			dialogStyle.GetBorderLeftSize() +
			dialogStyle.GetPaddingLeft() +
			dialogStyle.GetMarginLeft()
		cur.Y += dialogStyle.GetBorderTopSize() +
			dialogStyle.GetPaddingTop() +
			dialogStyle.GetMarginTop() +
			inputStyle.GetBorderTopSize() +
			inputStyle.GetPaddingTop() +
			inputStyle.GetMarginTop() + 2
	}
	return cur
}

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

// nextCommandType returns the next command type in the cycle.
func (c *Commands) nextCommandType() CommandType {
	switch c.selected {
	case SystemCommands:
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		fallthrough
	case UserCommands:
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		fallthrough
	case MCPPrompts:
		return SystemCommands
	default:
		return SystemCommands
	}
}

// previousCommandType returns the previous command type in the cycle.
func (c *Commands) previousCommandType() CommandType {
	switch c.selected {
	case SystemCommands:
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		return SystemCommands
	case UserCommands:
		return SystemCommands
	case MCPPrompts:
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		return SystemCommands
	default:
		return SystemCommands
	}
}

// setCommandItems sets the command items based on the specified command type.
func (c *Commands) setCommandItems(commandType CommandType) {
	c.selected = commandType

	if commandType == SystemCommands {
		// System commands use the hierarchical defaultCommands() directly
		// to preserve parent/child grouping structure.
		items := c.defaultCommands()
		filterableItems := make([]list.FilterableItem, len(items))
		for i, item := range items {
			filterableItems[i] = item
		}
		c.list.SetItems(filterableItems...)
		c.list.SetFilter("")
		c.list.ScrollToTop()
		c.list.SetSelected(0)
		c.input.SetValue("")
		c.filterActive = false

		// Build flattened list for cross-level fuzzy search.
		c.allItems = flattenItems(items)
		return
	}

	c.refreshRegistry()

	commandItems := []list.FilterableItem{}
	category := commandCategoryForType(c.selected)
	if c.registry != nil {
		descriptors, _ := c.registry.Commands(capability.CommandContext{})
		for _, cmd := range descriptors {
			if cmd.Category != category {
				continue
			}
			commandItems = append(commandItems, NewCommandItem(c.com.Styles, cmd.ID, cmd.Title, cmd.Shortcut, cmd.Action))
		}
	}

	c.list.SetItems(commandItems...)
	c.list.SetFilter("")
	c.list.ScrollToTop()
	c.list.SetSelected(0)
	c.input.SetValue("")
	c.filterActive = false
	c.allItems = nil
}

// flattenItems recursively collects all CommandItems (parents + children)
// into a flat slice for cross-level fuzzy search.
func flattenItems(items []*CommandItem) []list.FilterableItem {
	var result []list.FilterableItem
	for _, item := range items {
		result = append(result, item)
		if item.HasChildren() {
			result = append(result, flattenItems(item.Children)...)
		}
	}
	return result
}

func commandCategoryForType(commandType CommandType) capability.CommandCategory {
	switch commandType {
	case UserCommands:
		return capability.CommandCategoryUser
	case MCPPrompts:
		return capability.CommandCategoryMCP
	default:
		return capability.CommandCategorySystem
	}
}

func (c *Commands) refreshRegistry() {
	c.registry = capability.NewCommandRegistry(
		capability.StaticCommandProvider{
			Info:  capability.ProviderInfo{ID: "builtin", Name: "Built-in Commands", Kind: capability.ProviderKindBuiltin},
			Items: c.defaultCommandDescriptors(),
		},
		capability.StaticCommandProvider{
			Info:  capability.ProviderInfo{ID: "session-actions", Name: "Session Actions", Kind: capability.ProviderKindSession},
			Items: c.sessionCommandDescriptors(),
		},
		capability.StaticCommandProvider{
			Info:  capability.ProviderInfo{ID: "custom", Name: "Custom Commands", Kind: capability.ProviderKindCustomCommand},
			Items: c.customCommandDescriptors(),
		},
		capability.StaticCommandProvider{
			Info:  capability.ProviderInfo{ID: "mcp-prompts", Name: "MCP Prompts", Kind: capability.ProviderKindMCP},
			Items: c.mcpPromptCommandDescriptors(),
		},
	)
}

func (c *Commands) defaultCommandDescriptors() []capability.CommandDescriptor {
	return descriptorsFromCommandItems(c.defaultCommands(), capability.CommandCategorySystem, capability.RiskLevelRead)
}

func (c *Commands) sessionCommandDescriptors() []capability.CommandDescriptor {
	if !c.hasSession {
		return nil
	}
	return []capability.CommandDescriptor{
		{
			ID:          "summarize",
			Title:       "Summarize Session",
			Description: "Compress the active session context.",
			Category:    capability.CommandCategorySystem,
			Risk:        capability.RiskLevelWrite,
			Action:      ActionSummarize{SessionID: c.sessionID},
		},
		{
			ID:          "export_markdown",
			Title:       "Export Session as Markdown",
			Description: "Export all active session messages to .mocode/export/ as Markdown.",
			Category:    capability.CommandCategorySystem,
			Risk:        capability.RiskLevelWrite,
			Action:      ActionExportSession{SessionID: c.sessionID, Format: "markdown", Scope: "all"},
		},
		{
			ID:          "export_html",
			Title:       "Export Session as HTML",
			Description: "Export all active session messages to .mocode/export/ as HTML.",
			Category:    capability.CommandCategorySystem,
			Risk:        capability.RiskLevelWrite,
			Action:      ActionExportSession{SessionID: c.sessionID, Format: "html", Scope: "all"},
		},
	}
}

func (c *Commands) customCommandDescriptors() []capability.CommandDescriptor {
	descriptors := make([]capability.CommandDescriptor, 0, len(c.customCommands))
	for _, cmd := range c.customCommands {
		action := ActionRunCustomCommand{
			Content:   cmd.Content,
			Arguments: cmd.Arguments,
		}
		descriptors = append(descriptors, capability.CommandDescriptor{
			ID:        "custom_" + cmd.ID,
			Title:     cmd.Name,
			Category:  capability.CommandCategoryUser,
			Arguments: cmd.Arguments,
			Risk:      capability.RiskLevelRead,
			Action:    action,
		})
	}
	return descriptors
}

func (c *Commands) mcpPromptCommandDescriptors() []capability.CommandDescriptor {
	descriptors := make([]capability.CommandDescriptor, 0, len(c.mcpPrompts))
	for _, cmd := range c.mcpPrompts {
		action := ActionRunMCPPrompt{
			Title:       cmd.Title,
			Description: cmd.Description,
			PromptID:    cmd.PromptID,
			ClientID:    cmd.ClientID,
			Arguments:   cmd.Arguments,
		}
		descriptors = append(descriptors, capability.CommandDescriptor{
			ID:          "mcp_" + cmd.ID,
			Title:       cmd.PromptID,
			Description: cmd.Description,
			Category:    capability.CommandCategoryMCP,
			Arguments:   cmd.Arguments,
			Risk:        capability.RiskLevelNetwork,
			Action:      action,
		})
	}
	return descriptors
}

func descriptorsFromCommandItems(items []*CommandItem, category capability.CommandCategory, risk capability.RiskLevel) []capability.CommandDescriptor {
	descriptors := make([]capability.CommandDescriptor, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		desc := capability.CommandDescriptor{
			ID:       item.id,
			Title:    item.title,
			Shortcut: item.shortcut,
			Category: category,
			Risk:     risk,
			Action:   item.action,
		}
		// If it has children, record its ID so completions can render a sub-group hint.
		if len(item.Children) > 0 {
			desc.ParentID = item.id
		}
		descriptors = append(descriptors, desc)
		// Flatten children into top-level list (no nesting in completions).
		for _, child := range item.Children {
			if child == nil {
				continue
			}
			descriptors = append(descriptors, capability.CommandDescriptor{
				ID:          child.id,
				Title:       child.title,
				Shortcut:    child.shortcut,
				Category:    category,
				Risk:        risk,
				Action:      child.action,
				ParentID:    item.id, // non-empty = child of this parent
			})
		}
	}
	return descriptors
}

// defaultCommands returns the list of default system commands organized
// into hierarchical parent/child groups for the top-level command palette.
func (c *Commands) defaultCommands() []*CommandItem {
	cfg := c.com.Config()
	t := c.com.Styles
	var commands []*CommandItem

	// ── Agent ──
	agentCommands := []*CommandItem{
		NewParentCommandItem(t, "skplan", "SK Plan", "/plan",
			NewCommandItem(t, "skplan_start", "Start New Plan", "", ActionSelectMode{ModeID: "plan"}),
			NewCommandItem(t, "skplan_code", "Quick Code Mode", "", ActionSelectMode{ModeID: "coder"}),
		),
		NewCommandItem(t, "code", "Quick Code Mode", "/code", ActionSelectMode{ModeID: "coder"}),
		NewCommandItem(t, "switch_mode", "Switch Agent Mode...", "", ActionOpenDialog{ModesID}),
		NewCommandItem(t, "switch_model", "Switch Model...", "ctrl+l", ActionOpenDialog{ModelsID}),
	}
	commands = append(commands, NewParentCommandItem(t, "group_agent", "Agent", "", agentCommands...))

	// ── Session ──
	sessionCommands := []*CommandItem{
		NewCommandItem(t, "new_session", "New Session", "ctrl+n", ActionNewSession{}),
		NewCommandItem(t, "switch_session", "Open Sessions...", "ctrl+s", ActionOpenDialog{SessionsID}),
		NewCommandItem(t, "init", "Initialize Project", "", ActionInitializeProject{}),
	}
	// Summarize and export only when there is an active session.
	if c.hasSession {
		sessionCommands = append(sessionCommands,
			NewCommandItem(t, "context", "Browse Context Messages...", "", ActionOpenDialog{ContextID}),
			NewCommandItem(t, "summarize", "Summarize Session", "", ActionSummarize{SessionID: c.sessionID}),
			NewCommandItem(t, "export_markdown", "Export as Markdown", "", ActionExportSession{SessionID: c.sessionID, Format: "markdown", Scope: "all"}),
			NewCommandItem(t, "export_html", "Export as HTML", "", ActionExportSession{SessionID: c.sessionID, Format: "html", Scope: "all"}),
		)
	}
	if c.windowWidth >= sidebarCompactModeBreakpoint && c.hasSession {
		sessionCommands = append(sessionCommands, NewCommandItem(t, "toggle_sidebar", "Toggle Sidebar", "", ActionToggleCompactMode{}))
	}
	if c.hasTodos || c.hasQueue {
		var label string
		switch {
		case c.hasTodos && c.hasQueue:
			label = "Toggle To-Dos/Queue"
		case c.hasQueue:
			label = "Toggle Queue"
		default:
			label = "Toggle To-Dos"
		}
		sessionCommands = append(sessionCommands, NewCommandItem(t, "toggle_pills", label, "ctrl+t", ActionTogglePills{}))
	}
	commands = append(commands, NewParentCommandItem(t, "group_session", "Session", "", sessionCommands...))

	// ── Tools ──
	toolsCommands := []*CommandItem{
		NewCommandItem(t, "mcp_servers", "MCP Servers...", "", ActionOpenDialog{MCPID}),
	}
	if c.hasSession {
		agentCfg, cfgOk := cfg.Agents[config.AgentCoder]
		if cfgOk {
			model := cfg.GetModelByType(agentCfg.Model)
			if model != nil && model.SupportsImages {
				toolsCommands = append(toolsCommands, NewCommandItem(t, "file_picker", "Open File Picker", "ctrl+f", ActionOpenDialog{FilePickerID}))
			}
		}
	}
	if os.Getenv("EDITOR") != "" {
		toolsCommands = append(toolsCommands, NewCommandItem(t, "open_external_editor", "Open External Editor", "ctrl+o", ActionExternalEditor{}))
	}
	if !cfg.IsDockerMCPEnabled() && c.dockerMCPAvailable != nil && *c.dockerMCPAvailable {
		toolsCommands = append(toolsCommands, NewCommandItem(t, "enable_docker_mcp", "Enable Docker MCP", "", ActionEnableDockerMCP{}))
	}
	if cfg.IsDockerMCPEnabled() {
		toolsCommands = append(toolsCommands, NewCommandItem(t, "disable_docker_mcp", "Disable Docker MCP", "", ActionDisableDockerMCP{}))
	}
	commands = append(commands, NewParentCommandItem(t, "group_tools", "Tools", "", toolsCommands...))

	// ── Settings ──
	settingsCommands := []*CommandItem{
		NewCommandItem(t, "toggle_yolo", "Toggle Yolo Mode", "", ActionToggleYoloMode{}),
		NewCommandItem(t, "toggle_help", "Show Help", "", ActionOpenDialog{HelpID}),
	}
	notificationsDisabled := cfg != nil && cfg.Options != nil && cfg.Options.DisableNotifications
	notificationLabel := "Disable Notifications"
	if notificationsDisabled {
		notificationLabel = "Enable Notifications"
	}
	settingsCommands = append(settingsCommands, NewCommandItem(t, "toggle_notifications", notificationLabel, "", ActionToggleNotifications{}))

	transparentLabel := "Disable Transparency"
	if cfg != nil && cfg.Options != nil && cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent {
		transparentLabel = "Enable Background"
	}
	settingsCommands = append(settingsCommands, NewCommandItem(t, "toggle_transparent", transparentLabel, "", ActionToggleTransparentBackground{}))

	settingsCommands = append(settingsCommands,
		NewCommandItem(t, "proxy_configure", "Configure Proxy...", "", ActionSetProxyURL{Enabled: true}),
		NewCommandItem(t, "proxy_disable", "Disable Proxy", "", ActionSetProxyURL{Enabled: false}),
	)

	if agentCfg, cfgOk := cfg.Agents[config.AgentCoder]; cfgOk {
		providerCfg := cfg.GetProviderForModel(agentCfg.Model)
		model := cfg.GetModelByType(agentCfg.Model)
		if providerCfg != nil && model != nil && model.CanReason {
			selectedModel := cfg.Models[agentCfg.Model]
			if model.CanReason && len(model.ReasoningLevels) == 0 {
				status := "Enable"
				if selectedModel.Think {
					status = "Disable"
				}
				settingsCommands = append(settingsCommands, NewCommandItem(t, "toggle_thinking", status+" Thinking Mode", "", ActionToggleThinking{}))
			}
			if len(model.ReasoningLevels) > 0 {
				settingsCommands = append(settingsCommands, NewCommandItem(t, "select_reasoning_effort", "Select Reasoning Effort...", "", ActionOpenDialog{ReasoningID}))
			}
		}
	}
	commands = append(commands, NewParentCommandItem(t, "group_settings", "Settings", "", settingsCommands...))

	// ── Admin ──
	adminCommands := []*CommandItem{
		NewCommandItem(t, "admin_panel", "Open Admin Panel", "", ActionOpenAdmin{}),
		NewCommandItem(t, "admin_start", "Start Admin Server", "", ActionStartAdmin{}),
		NewCommandItem(t, "admin_stop", "Stop Admin Server", "", ActionStopAdmin{}),
		NewCommandItem(t, "minimax_quota", "MiniMax Quota", "", ActionShowQuota{}),
		NewCommandItem(t, "wechat_login", "Connect WeChat", "", ActionOpenDialog{WeChatQRID}),
		NewCommandItem(t, "wechat_manager", "Manage WeChat accounts", "", ActionOpenDialog{WeChatManagerID}),
	}
	commands = append(commands, NewParentCommandItem(t, "group_admin", "Admin", "", adminCommands...))

	// ── Exit ──
	commands = append(commands, NewCommandItem(t, "quit", "Quit Ctrl+C", "ctrl+c", tea.QuitMsg{}))

	return commands
}

// SetCustomCommands sets the custom commands and refreshes the view if user commands are currently displayed.
func (c *Commands) SetCustomCommands(customCommands []commands.CustomCommand) {
	c.customCommands = customCommands
	if c.selected == UserCommands {
		c.setCommandItems(c.selected)
	}
}

// SetMCPPrompts sets the MCP prompts and refreshes the view if MCP prompts are currently displayed.
func (c *Commands) SetMCPPrompts(mcpPrompts []commands.MCPPrompt) {
	c.mcpPrompts = mcpPrompts
	if c.selected == MCPPrompts {
		c.setCommandItems(c.selected)
	}
}

// StartLoading implements [LoadingDialog].
func (a *Commands) StartLoading() tea.Cmd {
	if a.loading {
		return nil
	}
	a.loading = true
	return a.spinner.Tick
}

// StopLoading implements [LoadingDialog].
func (a *Commands) StopLoading() {
	a.loading = false
}

// -- Submenu navigation --

// isInSubmenu reports whether the dialog is currently showing a submenu.
func (c *Commands) isInSubmenu() bool {
	return len(c.navStack) > 0
}

// pushSubmenu enters the children list of parent.
func (c *Commands) pushSubmenu(parent *CommandItem) {
	if !parent.HasChildren() {
		return
	}
	c.navStack = append(c.navStack, parent)
	items := make([]list.FilterableItem, len(parent.Children))
	for i, child := range parent.Children {
		items[i] = child
	}
	c.list.SetItems(items...)
	c.list.SetFilter("")
	c.list.SetSelected(0)
	c.list.ScrollToTop()
	c.input.SetValue("")
	c.filterActive = false
}

// popSubmenu returns to the parent level or root.
func (c *Commands) popSubmenu() {
	if len(c.navStack) == 0 {
		return
	}
	c.navStack = c.navStack[:len(c.navStack)-1]
	c.filterActive = false
	if len(c.navStack) == 0 {
		c.setCommandItems(c.selected)
	} else {
		// Restore parent's children directly without modifying navStack again.
		parent := c.navStack[len(c.navStack)-1]
		items := make([]list.FilterableItem, len(parent.Children))
		for i, child := range parent.Children {
			items[i] = child
		}
		c.list.SetItems(items...)
		c.list.SetFilter("")
		c.list.SetSelected(0)
		c.list.ScrollToTop()
		c.input.SetValue("")
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

// handleMouseClick processes mouse clicks on the command list.
func (c *Commands) handleMouseClick(msg tea.MouseClickMsg) Action {
	if !image.Pt(msg.X, msg.Y).In(c.lastArea) {
		return nil
	}
	if !image.Pt(msg.X, msg.Y).In(c.lastListArea) {
		return nil
	}
	idx, _ := c.list.ItemIndexAtPosition(msg.X-c.lastListArea.Min.X, msg.Y-c.lastListArea.Min.Y)
	if idx < 0 {
		return nil
	}
	c.list.SetSelected(idx)
	c.list.ScrollToSelected()
	if item := c.selectedCommandItem(); item != nil {
		if item.HasChildren() {
			c.pushSubmenu(item)
			return nil
		}
		return item.Action()
	}
	return nil
}

// selectedCommandItem returns the currently selected CommandItem.
func (c *Commands) selectedCommandItem() *CommandItem {
	selected := c.list.SelectedItem()
	if selected == nil {
		return nil
	}
	item, ok := selected.(*CommandItem)
	if !ok || item == nil {
		return nil
	}
	return item
}
