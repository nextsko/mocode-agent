package dialog

import (
	"image"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/list"
	"github.com/package-register/mocode/internal/ui/slash"
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

// Commands represents a dialog that shows available slash.
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

	customCommands []slash.CustomCommand
	mcpPrompts     []slash.MCPPrompt
	registry       *slash.CommandRegistry

	dockerMCPAvailable     *bool
	dockerMCPCheckInFlight bool
}

var _ Dialog = (*Commands)(nil)

// NewCommands creates a new commands dialog.
func NewCommands(com *common.Common, sessionID string, hasSession, hasTodos, hasQueue bool, customCommands []slash.CustomCommand, mcpPrompts []slash.MCPPrompt) (*Commands, error) {
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

// nextCommandType returns the next command type in the cycle.

// previousCommandType returns the previous command type in the cycle.

// setCommandItems sets the command items based on the specified command type.

// flattenItems recursively collects all CommandItems (parents + children)
// into a flat slice for cross-level fuzzy search.

// defaultCommands returns the list of default system commands organized
// into hierarchical parent/child groups for the top-level command palette.

// SetCustomCommands sets the custom commands and refreshes the view if user commands are currently displayed.
func (c *Commands) SetCustomCommands(customCommands []slash.CustomCommand) {
	c.customCommands = customCommands
	if c.selected == UserCommands {
		c.setCommandItems(c.selected)
	}
}

// SetMCPPrompts sets the MCP prompts and refreshes the view if MCP prompts are currently displayed.
func (c *Commands) SetMCPPrompts(mcpPrompts []slash.MCPPrompt) {
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

// pushSubmenu enters the children list of parent.

// popSubmenu returns to the parent level or root.

// handleMouseClick processes mouse clicks on the command list.

// selectedCommandItem returns the currently selected CommandItem.
