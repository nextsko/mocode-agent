package model

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	xstrings "github.com/charmbracelet/x/exp/strings"

	agentcore "github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/internal/core/agent/evo"
	"github.com/package-register/mocode/internal/core/agent/notify"
	agenttools "github.com/package-register/mocode/internal/core/agent/tools"
	"github.com/package-register/mocode/internal/core/agent/tools/mcp"
	"github.com/package-register/mocode/internal/core/app"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/permission"
	"github.com/package-register/mocode/internal/core/skills"
	"github.com/package-register/mocode/internal/domain/history"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/transport/admin"
	"github.com/package-register/mocode/internal/ui/attachments"
	"github.com/package-register/mocode/internal/ui/chat"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/completions"
	"github.com/package-register/mocode/internal/ui/dialog"
	"github.com/package-register/mocode/internal/ui/notification"
	"github.com/package-register/mocode/internal/ui/panel"
	"github.com/package-register/mocode/internal/ui/slash"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/ui/util"
	"github.com/package-register/mocode/internal/util/anim"
	"github.com/package-register/mocode/internal/util/fsext"
	"github.com/package-register/mocode/internal/util/infra"
	"github.com/package-register/mocode/internal/util/pubsub"
	"github.com/package-register/mocode/internal/util/version"
)

// MouseScrollThreshold defines how many lines to scroll the chat when a mouse
// wheel event occurs.
const MouseScrollThreshold = 5

// Compact mode breakpoints.
const (
	compactModeWidthBreakpoint  = 120
	compactModeHeightBreakpoint = 30
)

// If pasted text has more than 10 newlines, treat it as a file attachment.
const pasteLinesThreshold = 10

// If pasted text has more than 1000 columns, treat it as a file attachment.
const pasteColsThreshold = 1000

// Session details panel max height.
const sessionDetailsMaxHeight = 20

// TextareaMaxHeight is the maximum height of the prompt textarea.
const TextareaMaxHeight = 15

// editorHeightMargin is the vertical margin added to the textarea height to
// account for the divider, attachments row, and bottom margin.
const editorHeightMargin = 3

// TextareaMinHeight is the minimum height of the prompt textarea.
const TextareaMinHeight = 3

// uiFocusState represents the current focus state of the UI.
type uiFocusState uint8

// Possible uiFocusState values.
const (
	uiFocusNone uiFocusState = iota
	uiFocusEditor
	uiFocusMain
)

type uiState uint8

// Possible uiState values.
const (
	uiOnboarding uiState = iota
	uiInitialize
	uiLanding
	uiChat
)

type openEditorMsg struct {
	Text string
}

type (
	// cancelTimerExpiredMsg is sent when the cancel timer expires.
	cancelTimerExpiredMsg struct{}
	// userCommandsLoadedMsg is sent when user commands are loaded.
	userCommandsLoadedMsg struct {
		Commands []slash.CustomCommand
	}
	// breathTickMsg is sent periodically while the agent is busy to animate
	// the editor prompt with a pulsing breathing effect.
	breathTickMsg struct{}
	// mcpPromptsLoadedMsg is sent when mcp prompts are loaded.
	mcpPromptsLoadedMsg struct {
		Prompts []slash.MCPPrompt
	}
	// mcpStateChangedMsg is sent when there is a change in MCP client states.
	mcpStateChangedMsg struct {
		states map[string]mcp.ClientInfo
	}
	// sendMessageMsg is sent to send a message.
	// currently only used for mcp prompts.
	sendMessageMsg struct {
		Content     string
		Attachments []message.Attachment
	}

	// closeDialogMsg is sent to close the current dialog.
	closeDialogMsg struct{}

	// copyChatHighlightMsg is sent to copy the current chat highlight to clipboard.
	copyChatHighlightMsg struct{}

	// sessionFilesUpdatesMsg is sent when the files for this session have been updated
	sessionFilesUpdatesMsg struct {
		sessionFiles []SessionFile
	}
	hudTickMsg struct{}

	backgroundJobOutputMsg struct {
		ShellID string
		Output  string
		Done    bool
		Err     error
	}
)

type todoAutoContinueState struct {
	InFlight             bool
	Stalled              bool
	LastFingerprint      string
	ConsecutiveNoChanges int
	PendingPermission    bool
	ReauthBlocked        bool
}

// UI represents the main user interface model.
type UI struct {
	com          *common.Common
	session      *session.Session
	sessionFiles []SessionFile

	// keeps track of read files while we don't have a session id
	sessionFileReads []string

	// initialSessionID is set when loading a specific session on startup.
	initialSessionID string
	// continueLastSession is set to continue the most recent session on startup.
	continueLastSession bool

	lastUserMessageTime int64

	// The width and height of the terminal in cells.
	width  int
	height int
	layout uiLayout

	isTransparent bool

	focus uiFocusState
	state uiState

	keyMap KeyMap
	keyenh tea.KeyboardEnhancementsMsg

	dialog *dialog.Overlay
	status *Status
	toast  *Toast

	// isCanceling tracks whether the user has pressed escape once to cancel.
	isCanceling bool

	header *header

	// sendProgressBar instructs the TUI to send progress bar updates to the
	// terminal.
	sendProgressBar    bool
	progressBarEnabled bool

	// caps hold different terminal capabilities that we query for.
	caps common.Capabilities

	// Editor components
	textarea          textarea.Model
	editorAllSelected bool

	// Attachment list
	attachments *attachments.Attachments

	readyPlaceholder   string
	workingPlaceholder string

	// Completions state
	completions              *completions.Completions
	completionsOpen          bool
	completionsSlashMode     bool // true when completions were triggered by '/'
	completionsAtMode        bool // true when completions were triggered by '@'
	completionsStartIndex    int
	completionsQuery         string
	completionsPositionStart image.Point // x,y where user typed '@' or '/'

	// Chat components
	chat *Chat

	// onboarding state
	onboarding struct {
		yesInitializeSelected bool
	}

	// lsp
	lspStates map[string]app.LSPClientInfo

	// mcp
	mcpStates map[string]mcp.ClientInfo

	// skills
	skillStates []*skills.SkillState

	// Notification state
	notifyBackend       notification.Backend
	notifyWindowFocused bool
	// custom commands & mcp commands
	customCommands []slash.CustomCommand
	mcpPrompts     []slash.MCPPrompt

	// evo holds the /evo self-evolution session state. When active, the evo
	// theme is applied and the agent iterates on itself; exiting requires a
	// second confirmation. See internal/core/agent/evo.
	evo evo.State
	// evoPrevTheme is the theme active before entering /evo, restored on exit.
	evoPrevTheme styles.Styles

	// forceCompactMode tracks whether compact mode is forced by user toggle
	forceCompactMode bool

	// isCompact tracks whether we're currently in compact layout mode (either
	// by user toggle or auto-switch based on window size)
	isCompact bool

	// detailsOpen tracks whether the details panel is open (in compact mode)
	detailsOpen bool

	// pills state
	pillsExpanded      bool
	focusedPillSection pillSection
	promptQueue        int
	pillsView          string

	// Agent status tracking
	agentStatus        string    // Current agent status text (e.g., "thinking", "executing tool")
	agentStatusTime    time.Time // When the status was last updated
	agentRuntimes      map[string]*sessionAgentRuntimeState
	agentToolParents   map[string]string
	agentToolChildren  map[string]string
	agentToolTaskIDs   map[string]string
	agentToolSummaries map[string]map[string]string
	todoContinuations  map[string]*todoAutoContinueState
	backgroundJobs     map[string]string

	// Todo spinner
	todoSpinner    spinner.Model
	todoIsSpinning bool

	// Breath effect for the editor prompt (pulse when agent is busy)
	breathOn bool

	// mouse highlighting related state
	lastClickTime  time.Time
	lastPillClick  time.Time
	lastPillTarget pillClickTarget

	startedAt time.Time
	hudFrame  int

	// pendingQuitSummary holds the session summary to print after the TUI exits.
	pendingQuitSummary *quitSummary

	adminServer *admin.Server

	// Prompt history for up/down navigation through previous messages.
	promptHistory struct {
		messages []string
		index    int
		draft    string
	}
}

type agentRuntimeStatus string

const (
	agentRuntimeThinking  agentRuntimeStatus = "thinking"
	agentRuntimeExecuting agentRuntimeStatus = "executing"
	agentRuntimeStopped   agentRuntimeStatus = "stopped"
)

type agentRuntimeEntry struct {
	ID             string
	DisplayName    string
	Status         agentRuntimeStatus
	ToolName       string
	LatestActivity time.Time
	Summary        string
	FirstSeenOrder int
}

type sessionAgentRuntimeState struct {
	order   []string
	entries map[string]*agentRuntimeEntry
}

// New creates a new instance of the [UI] model.
func New(com *common.Common, initialSessionID string, continueLast bool) *UI {
	// Editor components
	ta := textarea.New()
	ta.SetStyles(com.Styles.Editor.Textarea)
	ta.ShowLineNumbers = false
	ta.CharLimit = -1
	ta.SetVirtualCursor(false)
	ta.DynamicHeight = true
	ta.MinHeight = TextareaMinHeight
	ta.MaxHeight = TextareaMaxHeight
	ta.Focus()

	ch := NewChat(com)
	chat.SetToolPanelView(com.Panels)

	keyMap := DefaultKeyMap()

	// Completions component
	comp := completions.New(
		com.Styles.Completions.Normal,
		com.Styles.Completions.Focused,
		com.Styles.Completions.Match,
	)

	todoSpinner := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(com.Styles.Pills.TodoSpinner),
	)

	// Attachments component
	attachments := attachments.New(
		attachments.NewRenderer(
			com.Styles.Attachments.Normal,
			com.Styles.Attachments.Deleting,
			com.Styles.Attachments.Image,
			com.Styles.Attachments.Text,
		),
		attachments.Keymap{
			DeleteMode: keyMap.Editor.AttachmentDeleteMode,
			DeleteAll:  keyMap.Editor.DeleteAllAttachments,
			Escape:     keyMap.Editor.Escape,
		},
	)

	header := newHeader(com)

	ui := &UI{
		com:                 com,
		dialog:              dialog.NewOverlay(),
		keyMap:              keyMap,
		textarea:            ta,
		chat:                ch,
		header:              header,
		completions:         comp,
		attachments:         attachments,
		todoSpinner:         todoSpinner,
		lspStates:           make(map[string]app.LSPClientInfo),
		mcpStates:           make(map[string]mcp.ClientInfo),
		agentRuntimes:       make(map[string]*sessionAgentRuntimeState),
		agentToolChildren:   make(map[string]string),
		agentToolTaskIDs:    make(map[string]string),
		agentToolSummaries:  make(map[string]map[string]string),
		todoContinuations:   make(map[string]*todoAutoContinueState),
		backgroundJobs:      make(map[string]string),
		notifyBackend:       notification.NoopBackend{},
		notifyWindowFocused: true,
		initialSessionID:    initialSessionID,
		continueLastSession: continueLast,
		startedAt:           time.Now(),
		adminServer:         admin.New(com.Workspace),
	}
	chat.SetAgentPanelResolver(ui.agentTaskPanelsForRender)

	status := NewStatus(com)

	ui.setEditorPrompt(false)
	ui.randomizePlaceholders()
	ui.textarea.Placeholder = ui.readyPlaceholder
	ui.status = status
	ui.toast = NewToast(com)

	// Initialize compact mode from config
	ui.forceCompactMode = com.Config().Options.TUI.CompactMode

	// set onboarding state defaults
	ui.onboarding.yesInitializeSelected = true

	desiredState := uiLanding
	desiredFocus := uiFocusEditor
	if !com.Config().IsConfigured() {
		desiredState = uiOnboarding
	} else if n, _ := com.Workspace.ProjectNeedsInitialization(); n {
		desiredState = uiInitialize
	}

	// set initial state
	ui.setState(desiredState, desiredFocus)

	opts := com.Config().Options

	// disable indeterminate progress bar
	ui.progressBarEnabled = opts.Progress == nil || *opts.Progress
	// enable transparent mode
	ui.isTransparent = opts.TUI.Transparent != nil && *opts.TUI.Transparent

	return ui
}

// Init initializes the UI model.
func (m *UI) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.state == uiOnboarding {
		if cmd := m.openModelsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// load the user commands async
	cmds = append(cmds, m.loadCustomCommands())
	// load prompt history async
	cmds = append(cmds, m.loadPromptHistory())
	// load initial session if specified
	if cmd := m.loadInitialSession(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, m.hudTickCmd())
	return tea.Batch(cmds...)
}

// loadInitialSession loads the initial session if one was specified on startup.
func (m *UI) loadInitialSession() tea.Cmd {
	switch {
	case m.state != uiLanding:
		// Only load if we're in landing state (i.e., fully configured)
		return nil
	case m.initialSessionID != "":
		return m.loadSession(m.initialSessionID)
	case m.continueLastSession:
		return func() tea.Msg {
			sessions, err := m.com.Workspace.ListSessions(context.Background())
			if err != nil || len(sessions) == 0 {
				return nil
			}
			return m.loadSession(sessions[0].ID)()
		}
	default:
		return nil
	}
}

// sendNotification returns a command that sends a notification if allowed by policy.
func (m *UI) sendNotification(n notification.Notification) tea.Cmd {
	if !m.shouldSendNotification() {
		return nil
	}

	backend := m.notifyBackend
	return func() tea.Msg {
		if err := backend.Send(n); err != nil {
			slog.Error("Failed to send notification", "error", err)
		}
		return nil
	}
}

// shouldSendNotification returns true if notifications should be sent based on
// current state. Focus reporting must be supported, window must not focused,
// and notifications must not be disabled in config.
func (m *UI) shouldSendNotification() bool {
	cfg := m.com.Config()
	if cfg != nil && cfg.Options != nil && cfg.Options.DisableNotifications {
		return false
	}
	return m.caps.ReportFocusEvents && !m.notifyWindowFocused
}

// setState changes the UI state and focus.
func (m *UI) setState(state uiState, focus uiFocusState) {
	if state == uiLanding {
		// Always turn off compact mode when going to landing
		m.isCompact = false
	}
	m.state = state
	m.focus = focus
	// Changing the state may change layout, so update it.
	m.updateLayoutAndSize()
}

// loadCustomCommands loads the custom commands asynchronously.
func (m *UI) loadCustomCommands() tea.Cmd {
	return func() tea.Msg {
		customCommands, err := slash.LoadCustomCommands(m.com.Config())
		if err != nil {
			slog.Error("Failed to load custom commands", "error", err)
		}
		return userCommandsLoadedMsg{Commands: customCommands}
	}
}

// loadMCPrompts loads the MCP prompts asynchronously.
func (m *UI) loadMCPrompts() tea.Msg {
	prompts, err := slash.LoadMCPPrompts()
	if err != nil {
		slog.Error("Failed to load MCP prompts", "error", err)
	}
	if prompts == nil {
		// flag them as loaded even if there is none or an error
		prompts = []slash.MCPPrompt{}
	}
	return mcpPromptsLoadedMsg{Prompts: prompts}
}

// Update handles updates to the UI model.
func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.hasSession() && m.isAgentBusy() {
		queueSize := m.com.Workspace.AgentQueuedPrompts(m.session.ID)
		if queueSize != m.promptQueue {
			m.promptQueue = queueSize
			m.updateLayoutAndSize()
		}
	}
	// Update terminal capabilities
	m.caps.Update(msg)
	switch msg := msg.(type) {
	case tea.EnvMsg:
		// Is this Windows Terminal?
		if !m.sendProgressBar {
			m.sendProgressBar = slices.Contains(msg, "WT_SESSION")
		}
		cmds = append(cmds, common.QueryCmd(uv.Environ(msg)))
	case tea.ModeReportMsg:
		if m.caps.ReportFocusEvents {
			m.notifyBackend = notification.NewNativeBackend(notification.Icon, config.GetAppName(m.com.Config()))
		}
	case tea.FocusMsg:
		m.notifyWindowFocused = true
	case tea.BlurMsg:
		m.notifyWindowFocused = false
	case pubsub.Event[notify.Notification]:
		if cmd := m.handleAgentNotification(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case loadSessionMsg:
		if m.forceCompactMode {
			m.isCompact = true
		}
		m.setState(uiChat, m.focus)
		m.session = msg.session
		m.sessionFiles = msg.files
		m.ensureAgentRuntimeState(m.session.ID)
		cmds = append(cmds, m.startLSPs(msg.lspFilePaths()))
		msgs, err := m.com.Workspace.ListMessages(context.Background(), m.session.ID)
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
			break
		}
		if cmd := m.setSessionMessages(msgs); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if hasInProgressTodo(m.session.Todos) {
			// only start spinner if there is an in-progress todo
			if m.isAgentBusy() {
				m.todoIsSpinning = true
				m.breathOn = true
				cmds = append(cmds, m.todoSpinner.Tick, m.breathingCmd())
			}
			m.updateLayoutAndSize()
		}
		// Reload prompt history for the new session.
		m.historyReset()
		cmds = append(cmds, m.loadPromptHistory())
		m.updateLayoutAndSize()

	case sessionFilesUpdatesMsg:
		m.sessionFiles = msg.sessionFiles
		var paths []string
		for _, f := range msg.sessionFiles {
			paths = append(paths, f.LatestVersion.Path)
		}
		cmds = append(cmds, m.startLSPs(paths))

	case sendMessageMsg:
		cmds = append(cmds, m.sendMessage(msg.Content, msg.Attachments...))

	case userCommandsLoadedMsg:
		m.customCommands = msg.Commands

	case mcpStateChangedMsg:
		m.mcpStates = msg.states
		if dia := m.dialog.Dialog(dialog.MCPID); dia != nil {
			if mcpDialog, ok := dia.(*dialog.MCP); ok {
				mcpDialog.SetStates(m.mcpStates)
			}
		}
	case mcpPromptsLoadedMsg:
		m.mcpPrompts = msg.Prompts

	case promptHistoryLoadedMsg:
		m.promptHistory.messages = msg.messages
		m.promptHistory.index = -1
		m.promptHistory.draft = ""

	case closeDialogMsg:
		m.dialog.CloseFrontDialog()

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.DeletedEvent {
			if m.session != nil && m.session.ID == msg.Payload.ID {
				if cmd := m.newSession(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			break
		}
		if m.session != nil && msg.Payload.ID == m.session.ID {
			prevHasInProgress := hasInProgressTodo(m.session.Todos)
			m.session = &msg.Payload
			m.ensureAgentRuntimeState(m.session.ID)
			m.updateTodoContinuationState(m.session.ID, msg.Payload.Todos)
			if !prevHasInProgress && hasInProgressTodo(m.session.Todos) {
				m.todoIsSpinning = true
				cmds = append(cmds, m.todoSpinner.Tick)
				m.updateLayoutAndSize()
			}
		}
	case pubsub.Event[message.Message]:
		// Check if this is a child session message for an agent tool.
		if m.session == nil {
			break
		}
		if msg.Payload.SessionID != m.session.ID {
			// This might be a child session message from an agent tool.
			if cmd := m.handleChildSessionMessage(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			break
		}
		switch msg.Type {
		case pubsub.CreatedEvent:
			cmds = append(cmds, m.appendSessionMessage(msg.Payload))
		case pubsub.UpdatedEvent:
			cmds = append(cmds, m.updateSessionMessage(msg.Payload))
		case pubsub.DeletedEvent:
			m.chat.RemoveMessage(msg.Payload.ID)
		}
		// start the spinner if there is a new message
		if hasInProgressTodo(m.session.Todos) && m.isAgentBusy() && !m.todoIsSpinning {
			m.todoIsSpinning = true
			m.breathOn = true
			cmds = append(cmds, m.todoSpinner.Tick, m.breathingCmd())
		}
		// stop the spinner if the agent is not busy anymore
		if m.todoIsSpinning && !m.isAgentBusy() {
			m.todoIsSpinning = false
			m.breathOn = false
		}
		// there is a number of things that could change the pills here so we want to re-render
		m.renderPills()
	case pubsub.Event[history.File]:
		cmds = append(cmds, m.handleFileEvent(msg.Payload))
	case pubsub.Event[app.LSPEvent]:
		m.lspStates = app.GetLSPStates()
	case pubsub.Event[skills.Event]:
		m.skillStates = msg.Payload.States
	case pubsub.Event[mcp.Event]:
		switch msg.Payload.Type {
		case mcp.EventStateChanged:
			return m, tea.Batch(
				m.handleStateChanged(),
				m.loadMCPrompts,
			)
		case mcp.EventPromptsListChanged:
			return m, handleMCPPromptsEvent(m.com.Workspace, msg.Payload.Name)
		case mcp.EventToolsListChanged:
			return m, handleMCPToolsEvent(m.com.Workspace, msg.Payload.Name)
		case mcp.EventResourcesListChanged:
			return m, handleMCPResourcesEvent(m.com.Workspace, msg.Payload.Name)
		}
	case pubsub.Event[permission.PermissionRequest]:
		if msg.Payload.SessionID != "" {
			m.ensureTodoContinuationState(msg.Payload.SessionID).PendingPermission = true
		}
		if cmd := m.openPermissionsDialog(msg.Payload); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.sendNotification(notification.Notification{
			Title:   fmt.Sprintf("%s is waiting...", config.GetAppName(m.com.Config())),
			Message: fmt.Sprintf("Permission required to execute \"%s\"", msg.Payload.ToolName),
		}); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case pubsub.Event[permission.PermissionNotification]:
		m.handlePermissionNotification(msg.Payload)
	case cancelTimerExpiredMsg:
		m.isCanceling = false
	case tea.TerminalVersionMsg:
		termVersion := strings.ToLower(msg.Name)
		// Only enable progress bar for the following terminals.
		if !m.sendProgressBar {
			m.sendProgressBar = xstrings.ContainsAnyOf(termVersion, "ghostty", "iterm2", "rio")
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.updateLayoutAndSize()
		if m.state == uiChat && m.chat.Follow() {
			if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case tea.KeyboardEnhancementsMsg:
		m.keyenh = msg
		if msg.SupportsKeyDisambiguation() {
			m.keyMap.Models.SetHelp("ctrl+m", "models")
			m.keyMap.Editor.Newline.SetHelp("shift+enter", "newline")
		}
	case copyChatHighlightMsg:
		cmds = append(cmds, m.copyChatHighlight())
	case hudTickMsg:
		m.hudFrame++
		cmds = append(cmds, m.hudTickCmd())
	case backgroundJobOutputMsg:
		if cmd := m.handleBackgroundJobOutput(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case DelayedClickMsg:
		// Handle delayed single-click action (e.g., expansion).
		m.chat.HandleDelayedClick(msg)
	case chat.OpenTodoDialogMsg:
		if cmd := m.openTodoDialog(msg.Selected); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case tea.MouseClickMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		if cmd := m.handleClickFocus(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

		switch m.state {
		case uiChat:
			if cmd := m.handlePillsMouseClick(msg); cmd != nil {
				cmds = append(cmds, cmd)
				break
			}
			x, y := msg.X, msg.Y
			// Adjust for chat area position
			x -= m.layout.main.Min.X
			y -= m.layout.main.Min.Y
			if !image.Pt(msg.X, msg.Y).In(m.layout.sidebar) {
				if handled, cmd := m.chat.HandleMouseDown(x, y); handled {
					m.lastClickTime = time.Now()
					if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			} else if cmd := m.handleSidebarAgentClick(msg.Y); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseMotionMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		switch m.state {
		case uiChat:
			if msg.Y <= 0 {
				if cmd := m.chat.ScrollByAndAnimate(-1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectPrev()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			} else if msg.Y >= m.chat.Height()-1 {
				if cmd := m.chat.ScrollByAndAnimate(1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectNext()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}

			x, y := msg.X, msg.Y
			// Adjust for chat area position
			x -= m.layout.main.Min.X
			y -= m.layout.main.Min.Y
			m.chat.HandleMouseDrag(x, y)
		}

	case tea.MouseReleaseMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		switch m.state {
		case uiChat:
			x, y := msg.X, msg.Y
			// Adjust for chat area position
			x -= m.layout.main.Min.X
			y -= m.layout.main.Min.Y
			if m.chat.HandleMouseUp(x, y) && m.chat.HasHighlight() {
				cmds = append(cmds, tea.Tick(doubleClickThreshold, func(t time.Time) tea.Msg {
					if time.Since(m.lastClickTime) >= doubleClickThreshold {
						return copyChatHighlightMsg{}
					}
					return nil
				}))
			}
		}
	case tea.MouseWheelMsg:
		// Pass mouse events to dialogs first if any are open.
		if m.dialog.HasDialogs() {
			m.dialog.Update(msg)
			return m, tea.Batch(cmds...)
		}

		// Otherwise handle mouse wheel for chat.
		switch m.state {
		case uiChat:
			switch msg.Button {
			case tea.MouseWheelUp:
				if cmd := m.chat.ScrollByAndAnimate(-MouseScrollThreshold); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectPrev()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case tea.MouseWheelDown:
				if cmd := m.chat.ScrollByAndAnimate(MouseScrollThreshold); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					if m.chat.AtBottom() {
						m.chat.SelectLast()
					} else {
						m.chat.SelectNext()
					}
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
		}
	case anim.StepMsg:
		if m.state == uiChat {
			if cmd := m.chat.Animate(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if m.chat.Follow() {
				if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	case spinner.TickMsg:
		if m.dialog.HasDialogs() {
			// route to dialog
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.state == uiChat && m.hasSession() && hasInProgressTodo(m.session.Todos) && m.todoIsSpinning {
			var cmd tea.Cmd
			m.todoSpinner, cmd = m.todoSpinner.Update(msg)
			if cmd != nil {
				m.renderPills()
				cmds = append(cmds, cmd)
			}
		}

	case breathTickMsg:
		m.breathOn = !m.breathOn
		if m.isAgentBusy() {
			cmds = append(cmds, m.breathingCmd())
		}

	case tea.KeyPressMsg:
		if cmd := m.handleKeyPressMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case tea.PasteMsg:
		if cmd := m.handlePasteMsg(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case openEditorMsg:
		prevHeight := m.textarea.Height()
		m.textarea.SetValue(msg.Text)
		m.editorAllSelected = false
		m.textarea.MoveToEnd()
		cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
	case util.InfoMsg:
		if msg.Type == util.InfoTypeError {
			slog.Error("Error reported", "error", msg.Msg)
		}
		m.toast.Show(msg)
	case util.ClearStatusMsg:
		m.toast.Hide()
	case completions.CompletionItemsLoadedMsg:
		if m.completionsOpen {
			m.completions.SetItems(msg.Files, msg.Resources)
		}
	case completions.AtCompletionItemsLoadedMsg:
		if m.completionsOpen && m.completionsAtMode {
			m.completions.SetAtItems(msg.Items, m.com.Styles, m.layout.editor.Dx())
			if m.completionsQuery != "" {
				m.completions.Filter(m.completionsQuery)
			}
		}
	case uv.KittyGraphicsEvent:
		if !bytes.HasPrefix(msg.Payload, []byte("OK")) {
			slog.Warn("Unexpected Kitty graphics response",
				"response", string(msg.Payload),
				"options", msg.Options)
		}
	default:
		if m.dialog.HasDialogs() {
			if cmd := m.handleDialogMsg(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	// This logic gets triggered on any message type, but should it?
	switch m.focus {
	case uiFocusMain:
	case uiFocusEditor:
		// Textarea placeholder logic
		if m.isAgentBusy() {
			m.textarea.Placeholder = m.workingPlaceholder
		} else {
			m.textarea.Placeholder = m.readyPlaceholder
		}
		if m.com.Workspace.PermissionSkipRequests() {
			m.textarea.Placeholder = "Yolo mode!"
		}
	}

	// at this point this can only handle [message.Attachment] message, and we
	// should return all cmds anyway.
	_ = m.attachments.Update(msg)
	return m, tea.Batch(cmds...)
}

// setSessionMessages sets the messages for the current session in the chat
func (m *UI) setSessionMessages(msgs []message.Message) tea.Cmd {
	var cmds []tea.Cmd
	// Build tool result map to link tool calls with their results
	msgPtrs := make([]*message.Message, len(msgs))
	for i := range msgs {
		msgPtrs[i] = &msgs[i]
	}
	toolResultMap := chat.BuildToolResultMap(msgPtrs)
	if len(msgPtrs) > 0 {
		m.lastUserMessageTime = msgPtrs[0].CreatedAt
	}

	// Add messages to chat with linked tool results
	items := make([]chat.MessageItem, 0, len(msgs)*2)
	for _, msg := range msgPtrs {
		switch msg.Role {
		case message.User:
			m.lastUserMessageTime = msg.CreatedAt
			items = append(items, chat.ExtractMessageItems(m.com.Styles, msg, toolResultMap)...)
		case message.Assistant:
			items = append(items, chat.ExtractMessageItems(m.com.Styles, msg, toolResultMap)...)
			if msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn {
				infoItem := chat.NewAssistantInfoItem(m.com.Styles, msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
				items = append(items, infoItem)
			}
		default:
			items = append(items, chat.ExtractMessageItems(m.com.Styles, msg, toolResultMap)...)
		}
	}

	// Load nested tool calls for agent/agentic_fetch tools.
	m.loadNestedToolCalls(items)

	// If the user switches between sessions while the agent is working we want
	// to make sure the animations are shown.
	for _, item := range items {
		if animatable, ok := item.(chat.Animatable); ok {
			if cmd := animatable.StartAnimation(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	m.chat.SetMessages(items...)
	if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.chat.SelectLast()
	if cmd := m.resumeBackgroundJobPolling(msgPtrs); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Sequence(cmds...)
}

// loadNestedToolCalls recursively loads nested tool calls for agent/agentic_fetch tools.
func (m *UI) loadNestedToolCalls(items []chat.MessageItem) {
	for _, item := range items {
		nestedContainer, ok := item.(chat.NestedToolContainer)
		if !ok {
			continue
		}
		toolItem, ok := item.(chat.ToolMessageItem)
		if !ok {
			continue
		}

		m.reloadNestedToolsForItem(toolItem)

		// Recursively load nested tool calls for any agent tools within.
		nestedMessageItems := make([]chat.MessageItem, len(nestedContainer.NestedTools()))
		for i, nt := range nestedContainer.NestedTools() {
			nestedMessageItems[i] = nt
		}
		m.loadNestedToolCalls(nestedMessageItems)
	}
}

// reloadNestedToolsForItem loads nested tool calls from child sessions for a
// single agent/agentic_fetch tool item and sets them on the container.
func (m *UI) reloadNestedToolsForItem(toolItem chat.ToolMessageItem) {
	nestedContainer, ok := toolItem.(chat.NestedToolContainer)
	if !ok {
		return
	}

	tc := toolItem.ToolCall()
	var nestedTools []chat.ToolMessageItem
	for _, childToolCallID := range m.registerAgentToolTopology(toolItem.MessageID(), sessionIDOrEmpty(m.session), tc) {
		agentSessionID := m.com.Workspace.CreateAgentToolSessionID(toolItem.MessageID(), childToolCallID)
		taskID := childSessionTaskID(&tc, agentSessionID)
		nestedMsgs, err := m.com.Workspace.ListMessages(context.Background(), agentSessionID)
		if err != nil || len(nestedMsgs) == 0 {
			continue
		}

		nestedMsgPtrs := make([]*message.Message, len(nestedMsgs))
		for i := range nestedMsgs {
			nestedMsgPtrs[i] = &nestedMsgs[i]
			if m.hasSession() {
				m.trackChildSessionRuntime(m.session.ID, agentSessionID, &tc, nestedMsgs[i])
			}
		}
		nestedToolResultMap := chat.BuildToolResultMap(nestedMsgPtrs)

		for _, nestedMsg := range nestedMsgPtrs {
			nestedItems := chat.ExtractMessageItems(m.com.Styles, nestedMsg, nestedToolResultMap)
			for _, nestedItem := range nestedItems {
				nestedToolItem, ok := nestedItem.(chat.ToolMessageItem)
				if !ok {
					continue
				}
				m.registerAgentToolTaskID(nestedToolItem.ToolCall().ID, taskID)
				if simplifiable, ok := nestedToolItem.(chat.Compactable); ok {
					simplifiable.SetCompact(true)
				}
				nestedTools = append(nestedTools, nestedToolItem)
			}
		}
	}

	nestedContainer.SetNestedTools(nestedTools)
}

// appendSessionMessage appends a new message to the current session in the chat
// if the message is a tool result it will update the corresponding tool call message
func (m *UI) appendSessionMessage(msg message.Message) tea.Cmd {
	var cmds []tea.Cmd

	existing := m.chat.MessageItem(msg.ID)
	if existing != nil {
		// message already exists, skip
		return nil
	}

	switch msg.Role {
	case message.User:
		m.lastUserMessageTime = msg.CreatedAt
		items := chat.ExtractMessageItems(m.com.Styles, &msg, nil)
		for _, item := range items {
			if animatable, ok := item.(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.chat.AppendMessages(items...)
		if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case message.Assistant:
		for _, tc := range msg.ToolCalls() {
			m.registerAgentToolTopology(msg.ID, msg.SessionID, tc)
		}
		items := chat.ExtractMessageItems(m.com.Styles, &msg, nil)
		for _, item := range items {
			if animatable, ok := item.(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
		m.chat.AppendMessages(items...)
		if m.chat.Follow() {
			if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn {
			infoItem := chat.NewAssistantInfoItem(m.com.Styles, &msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
			m.chat.AppendMessages(infoItem)
			if m.chat.Follow() {
				if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	case message.Tool:
		for _, tr := range msg.ToolResults() {
			toolItem := m.chat.MessageItem(tr.ToolCallID)
			if toolItem == nil {
				// we should have an item!
				continue
			}
			if toolMsgItem, ok := toolItem.(chat.ToolMessageItem); ok {
				toolMsgItem.SetResult(&tr)
				// When an agent tool completes, reload nested tools from child
				// sessions as a fallback. Live pubsub events may have been
				// missed (e.g. client/server mode, timing issues) leaving the
				// nested tool tree empty even though the sub-agent did work.
				if nestedContainer, ok := toolItem.(chat.NestedToolContainer); ok {
					if len(nestedContainer.NestedTools()) == 0 {
						m.reloadNestedToolsForItem(toolMsgItem)
					}
				}
				if m.chat.Follow() {
					if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				if cmd := m.maybeStartBackgroundJobPoll(tr); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	return tea.Sequence(cmds...)
}

// updateSessionMessage updates an existing message in the current session in
// the chat when an assistant message is updated it may include updated tool
// calls as well that is why we need to handle creating/updating each tool call
// message too.
func (m *UI) updateSessionMessage(msg message.Message) tea.Cmd {
	var cmds []tea.Cmd
	existingItem := m.chat.MessageItem(msg.ID)

	if existingItem != nil {
		if assistantItem, ok := existingItem.(*chat.AssistantMessageItem); ok {
			assistantItem.SetMessage(&msg)
		}
	}

	shouldRenderAssistant := chat.ShouldRenderAssistantMessage(&msg)
	isEndTurn := msg.FinishPart() != nil && msg.FinishPart().Reason == message.FinishReasonEndTurn
	// If the message of the assistant does not have any response just tool
	// calls we need to remove it, but keep the info item for end-of-turn
	// renders so the footer (model/provider/duration) remains visible when,
	// for example, a hook halts the turn.
	if !shouldRenderAssistant && len(msg.ToolCalls()) > 0 && existingItem != nil {
		m.chat.RemoveMessage(msg.ID)
		if !isEndTurn {
			if infoItem := m.chat.MessageItem(chat.AssistantInfoID(msg.ID)); infoItem != nil {
				m.chat.RemoveMessage(chat.AssistantInfoID(msg.ID))
			}
		}
	}

	if isEndTurn {
		if infoItem := m.chat.MessageItem(chat.AssistantInfoID(msg.ID)); infoItem == nil {
			newInfoItem := chat.NewAssistantInfoItem(m.com.Styles, &msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
			m.chat.AppendMessages(newInfoItem)
		}
	}

	var items []chat.MessageItem
	for _, tc := range msg.ToolCalls() {
		m.registerAgentToolTopology(msg.ID, msg.SessionID, tc)
		existingToolItem := m.chat.MessageItem(tc.ID)
		if toolItem, ok := existingToolItem.(chat.ToolMessageItem); ok {
			existingToolCall := toolItem.ToolCall()
			// only update if finished state changed or input changed
			// to avoid clearing the cache
			if (tc.Finished && !existingToolCall.Finished) || tc.Input != existingToolCall.Input {
				toolItem.SetToolCall(tc)
				// When an agent/agentic_fetch tool transitions to finished,
				// ensure nested tools are loaded from child sessions. This is
				// a fallback for cases where live pubsub events were missed.
				if tc.Finished && !existingToolCall.Finished {
					if nestedContainer, ok := toolItem.(chat.NestedToolContainer); ok {
						if len(nestedContainer.NestedTools()) == 0 {
							m.reloadNestedToolsForItem(toolItem)
						}
					}
				}
			}
		}
		if existingToolItem == nil {
			items = append(items, chat.NewToolMessageItem(m.com.Styles, msg.ID, tc, nil, false))
		}
	}

	for _, item := range items {
		if animatable, ok := item.(chat.Animatable); ok {
			if cmd := animatable.StartAnimation(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	m.chat.AppendMessages(items...)
	if m.chat.Follow() {
		if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat.SelectLast()
	}

	return tea.Sequence(cmds...)
}

// handleChildSessionMessage handles messages from child sessions (agent tools).
func (m *UI) handleChildSessionMessage(event pubsub.Event[message.Message]) tea.Cmd {
	var cmds []tea.Cmd

	// Check if this is an agent tool session and parse it.
	childSessionID := event.Payload.SessionID
	parentMessageID, toolCallID, ok := m.com.Workspace.ParseAgentToolSessionID(childSessionID)
	if !ok {
		return nil
	}

	parentSessionID := m.parentSessionIDForChild(childSessionID, parentMessageID, toolCallID)
	containerID := m.resolveAgentToolContainerID(toolCallID)
	agentItem, parentToolItem := m.findAgentToolItem(containerID)
	parentTaskID := ""
	if parentSessionID != "" {
		var parentTool *message.ToolCall
		if parentToolItem != nil {
			tc := parentToolItem.ToolCall()
			parentTool = &tc
			parentTaskID = childSessionTaskID(parentTool, childSessionID)
		}
		m.trackChildSessionRuntime(parentSessionID, childSessionID, parentTool, event.Payload)
	}

	if len(event.Payload.ToolCalls()) == 0 && len(event.Payload.ToolResults()) == 0 {
		// Even though there are no tool calls/results to render as nested
		// items, the sub-agent may have produced text (thinking/reasoning).
		// Propagate the latest status summary to the agent tool item so the
		// user can see what the sub-agent is doing.
		if agentItem != nil {
			if summaryEntry, ok := agentItem.(interface{ SetStatusSummary(string) }); ok {
				summary := firstContentLine(event.Payload.Content().Text)
				if summary == "" && event.Payload.IsThinking() {
					summary = "thinking"
				}
				if summary != "" {
					summaryEntry.SetStatusSummary(summary)
				}
			}
		}
		return nil
	}
	if agentItem == nil {
		return nil
	}

	// Get existing nested tools.
	nestedTools := agentItem.NestedTools()

	// Update or create nested tool calls.
	for _, tc := range event.Payload.ToolCalls() {
		found := false
		for _, existingTool := range nestedTools {
			if existingTool.ToolCall().ID == tc.ID {
				existingTool.SetToolCall(tc)
				found = true
				break
			}
		}
		if !found {
			// Create a new nested tool item.
			nestedItem := chat.NewToolMessageItem(m.com.Styles, event.Payload.ID, tc, nil, false)
			m.registerAgentToolTaskID(tc.ID, parentTaskID)
			if simplifiable, ok := nestedItem.(chat.Compactable); ok {
				simplifiable.SetCompact(true)
			}
			if animatable, ok := nestedItem.(chat.Animatable); ok {
				if cmd := animatable.StartAnimation(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			nestedTools = append(nestedTools, nestedItem)
		}
	}

	// Update nested tool results.
	for _, tr := range event.Payload.ToolResults() {
		for _, nestedTool := range nestedTools {
			if nestedTool.ToolCall().ID == tr.ToolCallID {
				nestedTool.SetResult(&tr)
				break
			}
		}
	}

	// Update the agent item with the new nested tools.
	agentItem.SetNestedTools(nestedTools)

	// Update the chat so it updates the index map for animations to work as expected
	m.chat.UpdateNestedToolIDs(containerID)

	if m.chat.Follow() {
		if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		m.chat.SelectLast()
	}

	return tea.Sequence(cmds...)
}

// handleDialogAction processes an action produced by a dialog or dispatched
// directly (e.g., from slash command completions without a dialog open).

// substituteArgs replaces $ARG_NAME placeholders in content with actual values.

// handleSelectModel performs the model selection after any provider
// pre-checks have completed.

// drawHeader draws the header section of the UI.

// Draw implements [uv.Drawable] and draws the UI model.
func (m *UI) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	layout := m.generateLayout(area.Dx(), area.Dy())

	if m.layout != layout {
		m.layout = layout
		m.updateSize()
	}

	// Clear the screen first
	screen.Clear(scr)

	switch m.state {
	case uiOnboarding:
		m.drawHeader(scr, layout.header)

		// NOTE: Onboarding flow will be rendered as dialogs below, but
		// positioned at the bottom left of the screen.

	case uiInitialize:
		m.drawHeader(scr, layout.header)

		main := uv.NewStyledString(m.initializeView())
		main.Draw(scr, layout.main)

	case uiLanding:
		m.drawHeader(scr, layout.header)
		main := uv.NewStyledString(m.landingView())
		main.Draw(scr, layout.main)

		editor := uv.NewStyledString(m.renderEditorView(scr.Bounds().Dx()))
		editor.Draw(scr, layout.editor)

	case uiChat:
		m.drawHeader(scr, layout.header)
		if !m.isCompact {
			m.drawSidebar(scr, layout.sidebar)
		}

		m.chat.Draw(scr, layout.main)
		if layout.pills.Dy() > 0 && m.pillsView != "" {
			uv.NewStyledString(m.pillsView).Draw(scr, layout.pills)
		}

		editorWidth := scr.Bounds().Dx()
		if !m.isCompact {
			editorWidth -= layout.sidebar.Dx()
		}
		editor := uv.NewStyledString(m.renderEditorView(editorWidth))
		editor.Draw(scr, layout.editor)

		// Draw details overlay in compact mode when open
		if m.isCompact && m.detailsOpen {
			m.drawSessionDetails(scr, layout.sessionDetails)
		}
	}

	isOnboarding := m.state == uiOnboarding

	// Add status and help layer
	m.status.SetHideHelp(isOnboarding)
	m.status.SetLine(m.statusLine(layout.status.Dx()))
	m.status.Draw(scr, layout.status)

	// Draw toast notification if visible
	if m.toast.IsVisible() {
		m.toast.Draw(scr, layout.status)
	}

	// Draw completions popup if open
	if !isOnboarding && m.completionsOpen && m.completions.HasItems() {
		w, h := m.completions.Size()
		y := m.completionsPositionStart.Y - h

		var x int
		if m.completionsSlashMode || m.completionsAtMode {
			// Dialog-style popups span the full editor width.
			x = m.layout.editor.Min.X
		} else {
			x = m.completionsPositionStart.X
			screenW := area.Dx()
			if x+w > screenW {
				x = screenW - w
			}
			x = max(0, x)
		}
		y = max(0, y+2) // Offset for divider and attachments row

		completionsView := uv.NewStyledString(m.completions.Render())
		completionsView.Draw(scr, image.Rectangle{
			Min: image.Pt(x, y),
			Max: image.Pt(x+w, y+h),
		})
	}

	// Debugging rendering (visually see when the tui rerenders)
	if os.Getenv("MOCODE_UI_DEBUG") == "true" {
		debugView := lipgloss.NewStyle().Background(randomANSIColor()).Width(4).Height(2)
		debug := uv.NewStyledString(debugView.String())
		debug.Draw(scr, image.Rectangle{
			Min: image.Pt(4, 1),
			Max: image.Pt(8, 3),
		})
	}

	// Panel overlay: render split panel view for parallel agent tasks
	if m.com.Panels != nil && m.com.Panels.IsVisible() {
		panelArea := layout.main
		// Shrink main area to make room; panels take bottom 40%
		splitY := panelArea.Min.Y + panelArea.Dy()*3/5
		chatArea := uv.Rectangle{
			Min: image.Pt(panelArea.Min.X, panelArea.Min.Y),
			Max: image.Pt(panelArea.Max.X, splitY),
		}
		panelRect := uv.Rectangle{
			Min: image.Pt(panelArea.Min.X, splitY+1),
			Max: image.Pt(panelArea.Max.X, panelArea.Max.Y),
		}
		m.com.Panels.Draw(scr, panelRect)
		// Re-render chat into reduced area
		m.chat.Draw(scr, chatArea)
	}

	// This needs to come last to overlay on top of everything. We always pass
	// the full screen bounds because the dialogs will position themselves
	// accordingly.
	if m.dialog.HasDialogs() {
		return m.dialog.Draw(scr, scr.Bounds())
	}

	switch m.focus {
	case uiFocusEditor:
		if m.layout.editor.Dy() <= 0 {
			// Don't show cursor if editor is not visible
			return nil
		}
		if m.detailsOpen && m.isCompact {
			// Don't show cursor if details overlay is open
			return nil
		}

		if m.textarea.Focused() {
			cur := m.textarea.Cursor()
			cur.X++                            // Adjust for app margins
			cur.Y += m.layout.editor.Min.Y + 2 // Offset for divider and attachments row
			return cur
		}
	}
	return nil
}

// View renders the UI model's view.
func (m *UI) View() tea.View {
	var v tea.View
	v.AltScreen = true
	if !m.isTransparent {
		v.BackgroundColor = m.com.Styles.Background
	}
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = m.caps.ReportFocusEvents
	v.WindowTitle = "mocode " + infra.Short(m.com.Workspace.WorkingDir())

	canvas := uv.NewScreenBuffer(m.width, m.height)
	v.Cursor = m.Draw(canvas, canvas.Bounds())

	content := strings.ReplaceAll(canvas.Render(), "\r\n", "\n") // normalize newlines
	contentLines := strings.Split(content, "\n")
	for i, line := range contentLines {
		// Trim trailing spaces for concise rendering
		contentLines[i] = strings.TrimRight(line, " ")
	}

	content = strings.Join(contentLines, "\n")

	v.Content = content
	if m.progressBarEnabled && m.sendProgressBar && m.isAgentBusy() {
		// HACK: use a random percentage to prevent ghostty from hiding it
		// after a timeout.
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarIndeterminate, randomIntn(100))
	}

	return v
}

// ShortHelp implements [help.KeyMap].
func (m *UI) ShortHelp() []key.Binding {
	var binds []key.Binding
	k := &m.keyMap
	tab := k.Tab
	commands := k.Commands
	if m.focus == uiFocusEditor && m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "slash")
	}

	switch m.state {
	case uiInitialize:
		binds = append(binds, k.Quit)
	case uiChat:
		// Show cancel binding if agent is busy.
		if m.isAgentBusy() {
			cancelBinding := k.Chat.Cancel
			if m.isCanceling {
				cancelBinding.SetHelp("esc", "press again to cancel")
			} else if m.com.Workspace.AgentQueuedPrompts(m.session.ID) > 0 {
				cancelBinding.SetHelp("esc", "clear queue")
			}
			binds = append(binds, cancelBinding)
		}

		if m.focus == uiFocusEditor {
			tab.SetHelp("tab", "focus chat")
		} else {
			tab.SetHelp("tab", "focus editor")
		}

		binds = append(
			binds,
			tab,
			commands,
			k.Models,
		)

		switch m.focus {
		case uiFocusEditor:
			binds = append(
				binds,
				k.Editor.Newline,
			)
		case uiFocusMain:
			binds = append(
				binds,
				k.Chat.UpDown,
				k.Chat.UpDownOneItem,
				k.Chat.PageUp,
				k.Chat.PageDown,
				k.Chat.Copy,
			)
			if m.pillsExpanded && hasIncompleteTodos(m.session.Todos) && m.promptQueue > 0 {
				binds = append(binds, k.Chat.PillLeft)
			}
		}
	default:
		// TODO: other states
		// if m.session == nil {
		// no session selected
		binds = append(
			binds,
			commands,
			k.Models,
			k.Editor.Newline,
		)
	}

	binds = append(
		binds,
		k.Quit,
	)

	return binds
}

// FullHelp implements [help.KeyMap].
func (m *UI) FullHelp() [][]key.Binding {
	var binds [][]key.Binding
	k := &m.keyMap
	hasAttachments := len(m.attachments.List()) > 0
	hasSession := m.hasSession()
	commands := k.Commands
	if m.focus == uiFocusEditor && m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "slash")
	}

	switch m.state {
	case uiInitialize:
		binds = append(binds,
			[]key.Binding{
				k.Quit,
			})
	case uiChat:
		// Show cancel binding if agent is busy.
		if m.isAgentBusy() {
			cancelBinding := k.Chat.Cancel
			if m.isCanceling {
				cancelBinding.SetHelp("esc", "press again to cancel")
			} else if m.com.Workspace.AgentQueuedPrompts(m.session.ID) > 0 {
				cancelBinding.SetHelp("esc", "clear queue")
			}
			binds = append(binds, []key.Binding{cancelBinding})
		}

		mainBinds := []key.Binding{}
		tab := k.Tab
		if m.focus == uiFocusEditor {
			tab.SetHelp("tab", "focus chat")
		} else {
			tab.SetHelp("tab", "focus editor")
		}

		mainBinds = append(
			mainBinds,
			tab,
			commands,
			k.Models,
			k.Sessions,
		)
		if hasSession {
			mainBinds = append(mainBinds, k.Chat.NewSession)
		}

		binds = append(binds, mainBinds)

		switch m.focus {
		case uiFocusEditor:
			editorBinds := []key.Binding{
				k.Editor.Newline,
				k.Editor.MentionFile,
				k.Editor.OpenEditor,
			}
			if m.currentModelSupportsImages() {
				editorBinds = append(editorBinds, k.Editor.AddImage, k.Editor.PasteImage)
			}
			binds = append(binds, editorBinds)
			if hasAttachments {
				binds = append(
					binds,
					[]key.Binding{
						k.Editor.AttachmentDeleteMode,
						k.Editor.DeleteAllAttachments,
						k.Editor.Escape,
					},
				)
			}
		case uiFocusMain:
			binds = append(
				binds,
				[]key.Binding{
					k.Chat.UpDown,
					k.Chat.UpDownOneItem,
					k.Chat.PageUp,
					k.Chat.PageDown,
				},
				[]key.Binding{
					k.Chat.HalfPageUp,
					k.Chat.HalfPageDown,
					k.Chat.Home,
					k.Chat.End,
				},
				[]key.Binding{
					k.Chat.Copy,
					k.Chat.ClearHighlight,
				},
			)
			if m.pillsExpanded && hasIncompleteTodos(m.session.Todos) && m.promptQueue > 0 {
				binds = append(binds, []key.Binding{k.Chat.PillLeft})
			}
		}
	default:
		if m.session == nil {
			// no session selected
			binds = append(
				binds,
				[]key.Binding{
					commands,
					k.Models,
					k.Sessions,
				},
			)
			editorBinds := []key.Binding{
				k.Editor.Newline,
				k.Editor.MentionFile,
				k.Editor.OpenEditor,
			}
			if m.currentModelSupportsImages() {
				editorBinds = append(editorBinds, k.Editor.AddImage, k.Editor.PasteImage)
			}
			binds = append(binds, editorBinds)
			if hasAttachments {
				binds = append(
					binds,
					[]key.Binding{
						k.Editor.AttachmentDeleteMode,
						k.Editor.DeleteAllAttachments,
						k.Editor.Escape,
					},
				)
			}
		}
	}

	binds = append(
		binds,
		[]key.Binding{
			k.Quit,
		},
	)

	return binds
}

// toggleCompactMode toggles compact mode between uiChat and uiChatCompact states.

// updateLayoutAndSize updates the layout and sizes of UI components.

// handleTextareaHeightChange checks whether the textarea height changed and,
// if so, recalculates the layout. When the chat is in follow mode it keeps
// the view scrolled to the bottom. The returned command, if non-nil, must be
// batched by the caller.

// updateTextarea updates the textarea for msg and then reconciles layout if
// the textarea height changed as a result.

// updateTextareaWithPrevHeight is for cases when the height of the layout may
// have changed.
//
// Particularly, it's for cases where the textarea changes before
// textarea.Update is called (for example, SetValue, Reset, and InsertRune). We
// pass the height from before those changes took place so we can compare
// "before" vs "after" sizing and recalculate the layout if the textarea grew
// or shrank.

// updateSize updates the sizes of UI components based on the current layout.

// generateLayout calculates the layout rectangles for all UI components based
// on the current UI state and terminal dimensions.

// uiLayout defines the positioning of UI elements.
type uiLayout struct {
	// area is the overall available area.
	area uv.Rectangle

	// header is the header shown in special cases
	// e.x when the sidebar is collapsed
	// or when in the landing page
	// or in init/config
	header uv.Rectangle

	// main is the area for the main pane. (e.x chat, configure, landing)
	main uv.Rectangle

	// pills is the area for the pills panel.
	pills uv.Rectangle

	// editor is the area for the editor pane.
	editor uv.Rectangle

	// sidebar is the area for the sidebar.
	sidebar uv.Rectangle

	// status is the area for the status view.
	status uv.Rectangle

	// session details is the area for the session details overlay in compact mode.
	sessionDetails uv.Rectangle
}

// setEditorPrompt configures the textarea prompt function based on whether
// yolo mode is enabled.

// normalPromptFunc returns the normal editor prompt style ("  > " on first
// line, "::: " on subsequent lines).
// When the agent is busy, the "::: " dots alternate between bright and dim
// colors to create a breathing/pulsing effect.

// yoloPromptFunc returns the yolo mode editor prompt style with warning icon
// and colored dots.

// closeCompletions closes the completions popup and resets state.

// slashCompletionGroups returns slash commands organised into named groups.
// The set mirrors the Commands dialog and is filtered by the same context
// guards (session presence, model capabilities, environment variables).

// slashLabelFromCommandID derives a "/name" label from a custom command ID
// like "user:my-feature" or "project:spec".

// slashDescFromContent returns the first non-empty line of content as a short
// description, capped at 60 characters.

// insertCompletionText replaces the @query in the textarea with the given text.
// Returns false if the replacement cannot be performed.

// insertFileCompletion inserts the selected file path into the textarea,
// replacing the @query, and adds the file as an attachment.

// insertMCPResourceCompletion inserts the selected resource into the textarea,
// replacing the @query, and adds the resource as an attachment.

// completionsPosition returns the X and Y position for the completions popup.

// textareaWord returns the current word at the cursor position.

// isWhitespace returns true if the byte is a whitespace character.

// isAgentBusy returns true if the agent coordinator exists and is currently
// busy processing a request.

// breathingCmd returns a command that sends a breathTickMsg after 800ms.
// This drives the editor prompt breathing animation while the agent is busy.

// hasSession returns true if there is an active session with a valid ID.

// mimeOf detects the MIME type of the given content.

var readyPlaceholders = [...]string{
	"Ready!",
	"Ready...",
	"Ready?",
	"Ready for instructions",
}

var workingPlaceholders = [...]string{
	"Working!",
	"Working...",
	"Brrrrr...",
	"Prrrrrrrr...",
	"Processing...",
	"Thinking...",
}

// randomizePlaceholders selects random placeholder text for the textarea's
// ready and working states.

// renderEditorView renders the editor view with attachments if any.

// applyTheme replaces the active styles with the given theme, drops the
// shared markdown renderer cache, and refreshes every component that
// caches style data.

// refreshStyles pushes the current *m.com.Styles into every subcomponent
// that copies or pre-renders style-dependent values at construction time.

// sendMessage sends a message with the given content and attachments.
func (m *UI) sendMessage(content string, attachments ...message.Attachment) tea.Cmd {
	if !m.com.Workspace.AgentIsReady() {
		return util.ReportError(fmt.Errorf("coder agent is not initialized"))
	}

	var cmds []tea.Cmd
	if !m.hasSession() {
		newSession, err := m.com.Workspace.CreateSession(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}
		if m.forceCompactMode {
			m.isCompact = true
		}
		if newSession.ID != "" {
			m.session = &newSession
			cmds = append(cmds, m.loadSession(newSession.ID))
		}
		m.setState(uiChat, m.focus)
	}

	ctx := context.Background()
	cmds = append(cmds, func() tea.Msg {
		for _, path := range m.sessionFileReads {
			m.com.Workspace.FileTrackerRecordRead(ctx, m.session.ID, path)
			m.com.Workspace.LSPStart(ctx, path)
		}
		return nil
	})

	// Enable follow mode when user sends a message so new responses auto-scroll
	m.chat.ScrollToBottom()

	// Capture session ID to avoid race with main goroutine updating m.session.
	sessionID := m.session.ID
	cmds = append(cmds, func() tea.Msg {
		err := m.com.Workspace.AgentRun(context.Background(), sessionID, content, attachments...)
		if err != nil {
			isCancelErr := errors.Is(err, context.Canceled)
			if isCancelErr {
				return nil
			}
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("%v", err),
			}
		}
		return nil
	})
	return tea.Batch(cmds...)
}

const cancelTimerDuration = 2 * time.Second

// cancelTimerCmd creates a command that expires the cancel timer.
func cancelTimerCmd() tea.Cmd {
	return tea.Tick(cancelTimerDuration, func(time.Time) tea.Msg {
		return cancelTimerExpiredMsg{}
	})
}

// cancelAgent handles the cancel key press. The first press sets isCanceling to true
// and starts a timer. The second press (before the timer expires) actually
// cancels the agent.
func (m *UI) cancelAgent() tea.Cmd {
	if !m.hasSession() {
		return nil
	}

	if !m.com.Workspace.AgentIsReady() {
		return nil
	}

	if m.isCanceling {
		// Second escape press - actually cancel the agent.
		m.isCanceling = false
		m.com.Workspace.AgentCancel(m.session.ID)
		// Stop the spinning todo indicator.
		m.todoIsSpinning = false
		m.breathOn = false
		m.renderPills()
		return nil
	}

	// Check if there are queued prompts - if so, clear the queue.
	if m.com.Workspace.AgentQueuedPrompts(m.session.ID) > 0 {
		m.com.Workspace.AgentClearQueue(m.session.ID)
		return nil
	}

	// First escape press - set canceling state and start timer.
	m.isCanceling = true
	return cancelTimerCmd()
}

// openDialog opens a dialog by its ID.

// openQuitDialog opens the quit confirmation dialog.

// openModelsDialog opens the models dialog.

// openReasoningDialog opens the reasoning effort dialog.

// openSessionsDialog opens the sessions dialog. If the dialog is already open,
// it brings it to the front. Otherwise, it will list all the sessions and open
// the dialog.

// openFilesDialog opens the file picker dialog.

// openWeChatQRDialog opens the WeChat QR login dialog.
// openWeChatManagerDialog opens the unified WeChat Manager dialog (lists
// accounts, supports reconnect/start/stop/delete and opening the QR flow).
// The returned [tea.Cmd] is the initial periodic-refresh tick; the caller
// should schedule it so the dialog starts polling the manager for status
// updates. The dialog stops rescheduling its tick when it is closed.

// openModesDialog opens the agent mode selection dialog.

// openHelpDialog opens the help key bindings dialog.

// openContextDialog opens the context message browser dialog.

// openRollbackDialog opens the rollback dialog.

// performRollback executes the rollback to the target message node.

// findNodeIndex returns the 1-based index of the target message in the session.

// shortNodeID returns a short representation of a message node for logging.

// openPermissionsDialog opens the permissions dialog for a permission request.

func (m *UI) maybeAutoContinueTodos(sessionID string) tea.Cmd {
	if sessionID == "" || !m.com.Workspace.AgentIsReady() {
		return nil
	}
	sess, err := m.com.Workspace.GetSession(context.Background(), sessionID)
	if err != nil {
		return nil
	}
	if !hasIncompleteTodos(sess.Todos) {
		m.updateTodoContinuationState(sessionID, sess.Todos)
		return nil
	}
	state := m.ensureTodoContinuationState(sessionID)
	if state == nil || state.PendingPermission || state.ReauthBlocked || state.Stalled || state.InFlight || m.isCanceling {
		return nil
	}
	if m.com.Workspace.AgentIsSessionBusy(sessionID) || m.com.Workspace.AgentQueuedPrompts(sessionID) > 0 {
		return nil
	}
	if state.ConsecutiveNoChanges >= 3 {
		state.Stalled = true
		if m.hasSession() && m.session.ID == sessionID {
			m.renderPills()
		}
		return util.CmdHandler(util.NewWarnMsg("Todo stalled, manual intervention required"))
	}

	state.InFlight = true
	prompt := "Continue executing the remaining Todo items. Keep the Todo list updated as you work. If blocked, clearly state the blocker and leave unfinished items incomplete."
	return func() tea.Msg {
		if err := m.com.Workspace.AgentRun(context.Background(), sessionID, prompt); err != nil {
			state.InFlight = false
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Auto-continuing remaining Todo items")
	}
}

// handlePermissionNotification updates tool items when permission state changes.
func (m *UI) handlePermissionNotification(notification permission.PermissionNotification) {
	if m.hasSession() {
		m.ensureTodoContinuationState(m.session.ID).PendingPermission = false
	}
	toolItem := m.chat.MessageItem(notification.ToolCallID)
	if toolItem == nil {
		return
	}

	if permItem, ok := toolItem.(chat.ToolMessageItem); ok {
		if notification.Granted {
			permItem.SetStatus(chat.ToolStatusRunning)
		} else {
			permItem.SetStatus(chat.ToolStatusAwaitingPermission)
		}
	}
}

// handleAgentNotification translates domain agent events into desktop
// notifications using the UI notification backend.
func (m *UI) handleAgentNotification(n notify.Notification) tea.Cmd {
	if n.SessionID != "" {
		switch n.Type {
		case notify.TypeAgentThinking:
			m.updateAgentRuntime(n.SessionID, m.com.Workspace.CurrentAgentID(), "", agentRuntimeThinking, "", time.Now())
		case notify.TypeAgentToolExecuting:
			m.updateAgentRuntime(n.SessionID, m.com.Workspace.CurrentAgentID(), "", agentRuntimeExecuting, n.ToolName, time.Now())
		case notify.TypeAgentFinished:
			m.updateAgentRuntime(n.SessionID, m.com.Workspace.CurrentAgentID(), "", agentRuntimeStopped, "", time.Now())
		}
	}

	switch n.Type {
	case notify.TypeAgentThinking:
		if m.hasSession() && n.SessionID == m.session.ID {
			m.agentStatus = "thinking..."
			m.agentStatusTime = time.Now()
		}
		return nil
	case notify.TypeAgentToolExecuting:
		if m.hasSession() && n.SessionID == m.session.ID {
			if n.ToolName != "" {
				m.agentStatus = fmt.Sprintf("executing %s...", n.ToolName)
			} else {
				m.agentStatus = "executing tool..."
			}
			m.agentStatusTime = time.Now()
		}
		return nil
	case notify.TypeAgentFinished:
		if m.hasSession() && n.SessionID == m.session.ID {
			m.agentStatus = ""
			m.agentStatusTime = time.Time{}
		}
		var cmds []tea.Cmd
		if cmd := m.maybeAutoContinueTodos(n.SessionID); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, m.sendNotification(notification.Notification{
			Title:   fmt.Sprintf("%s is waiting...", config.GetAppName(m.com.Config())),
			Message: fmt.Sprintf("Agent's turn completed in \"%s\"", n.SessionTitle),
		}))
		return tea.Batch(cmds...)
	case notify.TypeReAuthenticate:
		if n.SessionID != "" {
			m.ensureTodoContinuationState(n.SessionID).ReauthBlocked = true
		} else if m.hasSession() {
			m.ensureTodoContinuationState(m.session.ID).ReauthBlocked = true
		}
		return m.handleReAuthenticate(n.ProviderID)
	case notify.TypeRoundtableTurn:
		if m.hasSession() {
			m.agentStatus = fmt.Sprintf("roundtable turn: %s", n.SessionTitle)
			m.agentStatusTime = time.Now()
		}
		return nil
	case notify.TypeRoundtableFinished:
		if m.hasSession() {
			m.agentStatus = ""
			m.agentStatusTime = time.Time{}
		}
		return m.sendNotification(notification.Notification{
			Title:   fmt.Sprintf("%s roundtable finished", config.GetAppName(m.com.Config())),
			Message: fmt.Sprintf("Roundtable %q completed", n.SessionTitle),
		})
	default:
		return nil
	}
}

func (m *UI) handleReAuthenticate(providerID string) tea.Cmd {
	cfg := m.com.Config()
	if cfg == nil {
		return nil
	}
	providerCfg, ok := cfg.Providers.Get(providerID)
	if !ok {
		return nil
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil
	}
	return m.openAuthenticationDialog(providerCfg.ToProvider(), cfg.Models[agentCfg.Model], agentCfg.Model)
}

// newSession clears the current session state and prepares for a new session.
// The actual session creation happens when the user sends their first message.
// Returns a command to reload prompt history.
func (m *UI) newSession() tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	sessionID := m.session.ID

	m.session = nil
	m.sessionFiles = nil
	m.sessionFileReads = nil
	m.setState(uiLanding, uiFocusEditor)
	m.textarea.Focus()
	m.chat.Blur()
	m.chat.ClearMessages()
	m.pillsExpanded = false
	m.promptQueue = 0
	m.pillsView = ""
	m.historyReset()
	if len(m.agentRuntimes) > 0 {
		delete(m.agentRuntimes, sessionID)
	}
	if len(m.todoContinuations) > 0 {
		delete(m.todoContinuations, sessionID)
	}
	if len(m.agentToolParents) > 0 {
		for toolCallID, parentSessionID := range m.agentToolParents {
			if parentSessionID == sessionID {
				delete(m.agentToolParents, toolCallID)
			}
		}
	}
	if len(m.agentToolChildren) > 0 {
		clear(m.agentToolChildren)
	}
	if len(m.agentToolTaskIDs) > 0 {
		clear(m.agentToolTaskIDs)
	}
	if len(m.agentToolSummaries) > 0 {
		clear(m.agentToolSummaries)
	}
	if len(m.backgroundJobs) > 0 {
		clear(m.backgroundJobs)
	}
	agenttools.ResetCache()
	return tea.Batch(
		func() tea.Msg {
			m.com.Workspace.LSPStopAll(context.Background())
			return nil
		},
		m.loadPromptHistory(),
	)
}

func (m *UI) ensureAgentRuntimeState(sessionID string) *sessionAgentRuntimeState {
	if sessionID == "" {
		return nil
	}
	if m.agentRuntimes == nil {
		m.agentRuntimes = make(map[string]*sessionAgentRuntimeState)
	}
	if state, ok := m.agentRuntimes[sessionID]; ok {
		return state
	}
	state := &sessionAgentRuntimeState{
		order:   make([]string, 0, 4),
		entries: make(map[string]*agentRuntimeEntry),
	}
	m.agentRuntimes[sessionID] = state
	return state
}

func (m *UI) registerAgentToolParent(toolCallID, sessionID string) {
	toolCallID = strings.TrimSpace(toolCallID)
	sessionID = strings.TrimSpace(sessionID)
	if toolCallID == "" || sessionID == "" {
		return
	}
	if m.agentToolParents == nil {
		m.agentToolParents = make(map[string]string)
	}
	m.agentToolParents[toolCallID] = sessionID
}

func (m *UI) registerAgentToolChild(parentToolCallID, childToolCallID string) {
	parentToolCallID = strings.TrimSpace(parentToolCallID)
	childToolCallID = strings.TrimSpace(childToolCallID)
	if parentToolCallID == "" || childToolCallID == "" || parentToolCallID == childToolCallID {
		return
	}
	if m.agentToolChildren == nil {
		m.agentToolChildren = make(map[string]string)
	}
	m.agentToolChildren[childToolCallID] = parentToolCallID
}

func (m *UI) registerAgentToolTaskID(toolCallID, taskID string) {
	toolCallID = strings.TrimSpace(toolCallID)
	taskID = strings.TrimSpace(taskID)
	if toolCallID == "" || taskID == "" {
		return
	}
	if m.agentToolTaskIDs == nil {
		m.agentToolTaskIDs = make(map[string]string)
	}
	m.agentToolTaskIDs[toolCallID] = taskID
}

func (m *UI) registerAgentToolTaskSummary(parentToolCallID, taskID, summary string) {
	parentToolCallID = strings.TrimSpace(parentToolCallID)
	taskID = strings.TrimSpace(taskID)
	summary = strings.TrimSpace(summary)
	if parentToolCallID == "" || taskID == "" || summary == "" {
		return
	}
	if m.agentToolSummaries == nil {
		m.agentToolSummaries = make(map[string]map[string]string)
	}
	taskSummaries := m.agentToolSummaries[parentToolCallID]
	if taskSummaries == nil {
		taskSummaries = make(map[string]string)
		m.agentToolSummaries[parentToolCallID] = taskSummaries
	}
	taskSummaries[taskID] = summary
}

func (m *UI) registerAgentToolTopology(messageID, sessionID string, tc message.ToolCall) []string {
	if messageID == "" {
		return nil
	}
	childToolCallIDs := agentToolChildCallIDs(tc)
	for _, childToolCallID := range childToolCallIDs {
		if sessionID != "" {
			m.registerAgentToolParent(childToolCallID, sessionID)
		}
		m.registerAgentToolChild(tc.ID, childToolCallID)
	}
	return childToolCallIDs
}

func (m *UI) agentTaskPanelsForRender(parentToolCallID string, params agentcore.AgentParams, nestedTools []chat.ToolMessageItem) []panel.AgentPanelData {
	panels := chat.BuildAgentTaskPanels(parentToolCallID, params, nestedTools, func(toolCallID string) string {
		if m.agentToolTaskIDs == nil {
			return ""
		}
		return strings.TrimSpace(m.agentToolTaskIDs[toolCallID])
	}, func(taskID string) string {
		if m.agentToolSummaries == nil {
			return ""
		}
		return strings.TrimSpace(m.agentToolSummaries[parentToolCallID][taskID])
	})
	if len(panels) == 0 {
		return chat.BuildLegacyAgentPanels(parentToolCallID, params, nestedTools)
	}
	return panels
}

func (m *UI) updateAgentRuntime(sessionID, agentID, displayName string, status agentRuntimeStatus, toolName string, activity time.Time) *agentRuntimeEntry {
	if sessionID == "" {
		return nil
	}
	state := m.ensureAgentRuntimeState(sessionID)
	if state == nil {
		return nil
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "main"
	}
	entry, ok := state.entries[agentID]
	if !ok {
		name := strings.TrimSpace(displayName)
		if name == "" {
			name = m.lookupAgentDisplayName(agentID)
		}
		entry = &agentRuntimeEntry{
			ID:             agentID,
			DisplayName:    name,
			FirstSeenOrder: len(state.order),
		}
		state.entries[agentID] = entry
		state.order = append(state.order, agentID)
	}
	if displayName != "" {
		entry.DisplayName = displayName
	}
	if entry.DisplayName == "" {
		entry.DisplayName = m.lookupAgentDisplayName(agentID)
	}
	entry.Status = status
	entry.ToolName = strings.TrimSpace(toolName)
	entry.LatestActivity = activity
	switch status {
	case agentRuntimeThinking:
		entry.Summary = "thinking"
	case agentRuntimeExecuting:
		if entry.ToolName != "" {
			entry.Summary = "executing " + entry.ToolName
		} else {
			entry.Summary = "executing"
		}
	case agentRuntimeStopped:
		if entry.Summary == "" {
			entry.Summary = "stopped"
		}
	}
	return entry
}

func (m *UI) parentSessionIDForChild(childSessionID, parentMessageID, toolCallID string) string {
	if toolCallID != "" && m.agentToolParents != nil {
		if sessionID := strings.TrimSpace(m.agentToolParents[toolCallID]); sessionID != "" {
			return sessionID
		}
	}
	_ = childSessionID
	_ = parentMessageID
	return ""
}

func (m *UI) trackChildSessionRuntime(parentSessionID, childSessionID string, parentTool *message.ToolCall, msg message.Message) {
	if parentSessionID == "" || childSessionID == "" {
		return
	}
	parentToolCallID := ""
	if parentTool != nil {
		parentToolCallID = strings.TrimSpace(parentTool.ID)
	}
	taskID := childSessionTaskID(parentTool, childSessionID)

	now := time.Now()
	if msg.UpdatedAt > 0 {
		now = time.Unix(msg.UpdatedAt, 0)
	} else if msg.CreatedAt > 0 {
		now = time.Unix(msg.CreatedAt, 0)
	}
	if len(msg.ToolCalls()) == 0 && len(msg.ToolResults()) == 0 && msg.Role == message.Assistant {
		displayName, fallbackSummary := childSessionDescriptor(parentTool, childSessionID)
		summary := firstContentLine(msg.Content().Text)
		if summary == "" {
			summary = fallbackSummary
		}
		status := agentRuntimeExecuting
		if msg.IsThinking() {
			status = agentRuntimeThinking
			if summary == "" {
				summary = "thinking"
			}
		}
		if msg.IsFinished() {
			status = agentRuntimeStopped
			if summary == "" {
				summary = "completed"
			}
		}
		entry := m.updateAgentRuntime(parentSessionID, childSessionID, displayName, status, "", now)
		if entry != nil && summary != "" {
			entry.Summary = summary
		}
		m.registerAgentToolTaskSummary(parentToolCallID, taskID, summary)
	}
	for _, tc := range msg.ToolCalls() {
		m.registerAgentToolParent(tc.ID, parentSessionID)
		m.registerAgentToolTaskID(tc.ID, taskID)
		displayName, summary := childAgentIdentity(tc)
		entry := m.updateAgentRuntime(parentSessionID, childSessionID, displayName, agentRuntimeExecuting, tc.Name, now)
		if entry == nil {
			continue
		}
		if summary != "" {
			entry.Summary = summary
			m.registerAgentToolTaskSummary(parentToolCallID, taskID, summary)
		}
	}
	for _, tr := range msg.ToolResults() {
		entry := m.updateAgentRuntime(parentSessionID, childSessionID, "", agentRuntimeStopped, tr.Name, now)
		if entry == nil {
			continue
		}
		if text := strings.TrimSpace(tr.Content); text != "" {
			entry.Summary = text
			m.registerAgentToolTaskSummary(parentToolCallID, taskID, text)
		} else if entry.Summary == "" {
			entry.Summary = "stopped"
		}
	}
}

func (m *UI) lookupAgentDisplayName(agentID string) string {
	for _, info := range m.com.Workspace.AvailableAgents() {
		if info.ID == agentID {
			if info.Name != "" {
				return info.Name
			}
			break
		}
	}
	if agentID == "" {
		return "Agent"
	}
	return agentID
}

func childAgentIdentity(tc message.ToolCall) (displayName string, summary string) {
	displayName = strings.TrimSpace(tc.AgentName)
	summary = strings.TrimSpace(tc.Name)

	if tc.Name != "agent" {
		if displayName == "" {
			displayName = "Agent"
		}
		return displayName, summary
	}

	var params struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(tc.Input), &params); err == nil {
		if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
			summary = prompt
		}
	}

	if displayName == "" {
		displayName = "Agent"
	}
	return displayName, summary
}

func (m *UI) currentSessionAgentEntries() []*agentRuntimeEntry {
	if !m.hasSession() {
		return nil
	}
	state := m.agentRuntimes[m.session.ID]
	if state == nil {
		return nil
	}
	entries := make([]*agentRuntimeEntry, 0, len(state.order))
	for _, id := range state.order {
		if entry := state.entries[id]; entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (m *UI) resolveAgentToolContainerID(toolCallID string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	if item := m.chat.MessageItem(toolCallID); item != nil {
		if _, ok := item.(chat.NestedToolContainer); ok {
			return toolCallID
		}
	}
	if m.agentToolChildren != nil {
		if parentToolCallID := strings.TrimSpace(m.agentToolChildren[toolCallID]); parentToolCallID != "" {
			return parentToolCallID
		}
	}
	return ""
}

func (m *UI) findAgentToolItem(containerID string) (chat.NestedToolContainer, chat.ToolMessageItem) {
	if containerID == "" {
		return nil, nil
	}
	item := m.chat.MessageItem(containerID)
	if item == nil {
		return nil, nil
	}
	agentItem, ok := item.(chat.NestedToolContainer)
	if !ok {
		return nil, nil
	}
	toolItem, ok := item.(chat.ToolMessageItem)
	if !ok {
		return nil, nil
	}
	return agentItem, toolItem
}

func agentToolChildCallIDs(tc message.ToolCall) []string {
	if tc.ID == "" {
		return nil
	}
	if tc.Name != agentcore.AgentToolName {
		return []string{tc.ID}
	}

	var params agentcore.AgentParams
	if err := json.Unmarshal([]byte(tc.Input), &params); err != nil || len(params.Tasks) == 0 {
		return []string{tc.ID}
	}

	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	childIDs := make([]string, 0, len(params.Tasks))
	if !hasDeps {
		for i := range params.Tasks {
			childIDs = append(childIDs, fmt.Sprintf("%s-%d", tc.ID, i+1))
		}
		return childIDs
	}

	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}
		childIDs = append(childIDs, fmt.Sprintf("%s-%s", tc.ID, taskID))
	}
	return childIDs
}

func childSessionDescriptor(parentTool *message.ToolCall, childSessionID string) (displayName string, summary string) {
	displayName = "Agent"
	if parentTool == nil || parentTool.Name != agentcore.AgentToolName {
		return displayName, summary
	}

	var params agentcore.AgentParams
	if err := json.Unmarshal([]byte(parentTool.Input), &params); err != nil {
		return displayName, summary
	}
	if len(params.Tasks) == 0 {
		if prompt := strings.TrimSpace(params.Prompt); prompt != "" {
			summary = prompt
		}
		return config.AgentTask, summary
	}

	_, childToolCallID, ok := parseChildSessionID(childSessionID)
	if !ok {
		childToolCallID = childSessionID
	}
	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}
	for i, task := range params.Tasks {
		var expectedID string
		if hasDeps {
			taskID := strings.TrimSpace(task.ID)
			if taskID == "" {
				taskID = fmt.Sprintf("task-%d", i+1)
			}
			expectedID = fmt.Sprintf("%s-%s", parentTool.ID, taskID)
		} else {
			expectedID = fmt.Sprintf("%s-%d", parentTool.ID, i+1)
		}
		if expectedID != childToolCallID {
			continue
		}
		if agentName := strings.TrimSpace(task.AgentName); agentName != "" {
			displayName = agentName
		} else {
			displayName = config.AgentTask
		}
		return displayName, strings.TrimSpace(task.Prompt)
	}

	return displayName, summary
}

func childSessionTaskID(parentTool *message.ToolCall, childSessionID string) string {
	if parentTool == nil || parentTool.Name != agentcore.AgentToolName {
		return ""
	}

	var params agentcore.AgentParams
	if err := json.Unmarshal([]byte(parentTool.Input), &params); err != nil || len(params.Tasks) == 0 {
		return ""
	}

	_, childToolCallID, ok := parseChildSessionID(childSessionID)
	if !ok {
		childToolCallID = childSessionID
	}

	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}

	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}

		expectedID := fmt.Sprintf("%s-%d", parentTool.ID, i+1)
		if hasDeps {
			expectedID = fmt.Sprintf("%s-%s", parentTool.ID, taskID)
		}
		if expectedID == childToolCallID {
			return taskID
		}
	}

	return ""
}

func parseChildSessionID(sessionID string) (messageID string, toolCallID string, ok bool) {
	parts := strings.Split(sessionID, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func firstContentLine(text string) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func sessionIDOrEmpty(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	return sess.ID
}

// handlePasteMsg handles a paste message.
func (m *UI) handlePasteMsg(msg tea.PasteMsg) tea.Cmd {
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg)
	}

	if m.focus != uiFocusEditor {
		return nil
	}

	// When the clipboard contains an image (not text), the terminal's
	// bracketed paste sends an empty string. Detect this case and read
	// the native clipboard image data instead.
	if strings.TrimSpace(msg.Content) == "" && m.currentModelSupportsImages() {
		return m.pasteImageFromClipboard
	}

	if hasPasteExceededThreshold(msg) {
		return func() tea.Msg {
			content := []byte(msg.Content)
			if int64(len(content)) > common.MaxAttachmentSize {
				return util.ReportWarn("Paste is too big (>5mb)")
			}
			name := fmt.Sprintf("paste_%d.txt", m.pasteIdx())
			mimeBufferSize := min(512, len(content))
			mimeType := http.DetectContentType(content[:mimeBufferSize])
			return message.Attachment{
				FileName: name,
				FilePath: name,
				MimeType: mimeType,
				Content:  content,
			}
		}
	}

	// Attempt to parse pasted content as file paths. If possible to parse,
	// all files exist and are valid, add as attachments.
	// Otherwise, paste as text.
	paths := fsext.ParsePastedFiles(msg.Content)
	allExistsAndValid := func() bool {
		if len(paths) == 0 {
			return false
		}
		for _, path := range paths {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return false
			}

			lowerPath := strings.ToLower(path)
			isValid := false
			for _, ext := range common.AllowedImageTypes {
				if strings.HasSuffix(lowerPath, ext) {
					isValid = true
					break
				}
			}
			if !isValid {
				return false
			}
		}
		return true
	}
	if !allExistsAndValid() {
		prevHeight := m.textarea.Height()
		return m.updateTextareaWithPrevHeight(msg, prevHeight)
	}

	var cmds []tea.Cmd
	for _, path := range paths {
		cmds = append(cmds, m.handleFilePathPaste(path))
	}
	return tea.Batch(cmds...)
}

func hasPasteExceededThreshold(msg tea.PasteMsg) bool {
	var (
		lineCount = 0
		colCount  = 0
	)
	for line := range strings.SplitSeq(msg.Content, "\n") {
		lineCount++
		colCount = max(colCount, len(line))

		if lineCount > pasteLinesThreshold || colCount > pasteColsThreshold {
			return true
		}
	}
	return false
}

// handleFilePathPaste handles a pasted file path.
func (m *UI) handleFilePathPaste(path string) tea.Cmd {
	return func() tea.Msg {
		fileInfo, err := os.Stat(path)
		if err != nil {
			return util.ReportError(err)
		}
		if fileInfo.IsDir() {
			return util.ReportWarn("Cannot attach a directory")
		}
		if fileInfo.Size() > common.MaxAttachmentSize {
			return util.ReportWarn("File is too big (>5mb)")
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return util.ReportError(err)
		}

		mimeBufferSize := min(512, len(content))
		mimeType := http.DetectContentType(content[:mimeBufferSize])
		fileName := filepath.Base(path)
		return message.Attachment{
			FilePath: path,
			FileName: fileName,
			MimeType: mimeType,
			Content:  content,
		}
	}
}

// pasteImageFromClipboard reads image data from the system clipboard and
// creates an attachment. If no image data is found, it falls back to
// interpreting clipboard text as a file path.
func (m *UI) pasteImageFromClipboard() tea.Msg {
	imageData, err := readClipboard(clipboardFormatImage)
	if int64(len(imageData)) > common.MaxAttachmentSize {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  "File too large, max 5MB",
		}
	}
	name := fmt.Sprintf("paste_%d.png", m.pasteIdx())
	if err == nil {
		return message.Attachment{
			FilePath: name,
			FileName: name,
			MimeType: mimeOf(imageData),
			Content:  imageData,
		}
	}

	textData, textErr := readClipboard(clipboardFormatText)
	if textErr != nil || len(textData) == 0 {
		return nil // Clipboard is empty or does not contain an image
	}

	path := strings.TrimSpace(string(textData))
	path = strings.ReplaceAll(path, "\\ ", " ")
	if _, statErr := os.Stat(path); statErr != nil {
		return nil // Clipboard does not contain an image or valid file path
	}

	lowerPath := strings.ToLower(path)
	isAllowed := false
	for _, ext := range common.AllowedImageTypes {
		if strings.HasSuffix(lowerPath, ext) {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		return util.NewInfoMsg("File type is not a supported image format")
	}

	fileInfo, statErr := os.Stat(path)
	if statErr != nil {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  fmt.Sprintf("Unable to read file: %v", statErr),
		}
	}
	if fileInfo.Size() > common.MaxAttachmentSize {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  "File too large, max 5MB",
		}
	}

	content, readErr := os.ReadFile(path)
	if readErr != nil {
		return util.InfoMsg{
			Type: util.InfoTypeError,
			Msg:  fmt.Sprintf("Unable to read file: %v", readErr),
		}
	}

	return message.Attachment{
		FilePath: path,
		FileName: filepath.Base(path),
		MimeType: mimeOf(content),
		Content:  content,
	}
}

var pasteRE = regexp.MustCompile(`paste_(\d+).txt`)

func (m *UI) pasteIdx() int {
	result := 0
	for _, at := range m.attachments.List() {
		found := pasteRE.FindStringSubmatch(at.FileName)
		if len(found) == 0 {
			continue
		}
		idx, err := strconv.Atoi(found[1])
		if err == nil {
			result = max(result, idx)
		}
	}
	return result + 1
}

// drawSessionDetails draws the session details in compact mode.
func (m *UI) drawSessionDetails(scr uv.Screen, area uv.Rectangle) {
	if m.session == nil {
		return
	}

	s := m.com.Styles

	width := area.Dx() - s.CompactDetails.View.GetHorizontalFrameSize()
	height := area.Dy() - s.CompactDetails.View.GetVerticalFrameSize()

	title := s.CompactDetails.Title.Width(width).MaxHeight(2).Render(m.session.Title)
	blocks := []string{
		title,
		"",
		m.modelInfo(width),
		"",
	}

	detailsHeader := lipgloss.JoinVertical(
		lipgloss.Left,
		blocks...,
	)

	version := s.CompactDetails.Version.Width(width).AlignHorizontal(lipgloss.Right).Render(version.Version)

	remainingHeight := height - lipgloss.Height(detailsHeader) - lipgloss.Height(version)

	const maxSectionWidth = 50
	sectionWidth := max(1, min(maxSectionWidth, width/4-2)) // account for spacing between sections
	maxItemsPerSection := remainingHeight - 3               // Account for section title and spacing

	lspSection := m.lspInfo(sectionWidth, maxItemsPerSection, false)
	mcpSection := m.mcpInfo(sectionWidth, maxItemsPerSection, false)
	skillsSection := m.skillsInfo(sectionWidth, maxItemsPerSection, false)
	filesSection := m.filesInfo(m.com.Workspace.WorkingDir(), sectionWidth, maxItemsPerSection, false)
	sections := lipgloss.JoinHorizontal(lipgloss.Top, filesSection, " ", lspSection, " ", mcpSection, " ", skillsSection)
	uv.NewStyledString(
		s.CompactDetails.View.
			Width(area.Dx()).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Left,
					detailsHeader,
					sections,
					version,
				),
			),
	).Draw(scr, area)
}

func (m *UI) copyChatHighlight() tea.Cmd {
	text := m.chat.HighlightContent()
	return common.CopyToClipboardWithCallback(
		text,
		"Selected text copied to clipboard",
		func() tea.Msg {
			m.chat.ClearMouse()
			return nil
		},
	)
}

func randomIntn(max int) int {
	if max <= 1 {
		return 0
	}

	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}

	return int(n.Int64())
}

func randomANSIColor() color.Color {
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(256))
	if err != nil {
		return lipgloss.Color("0")
	}

	return lipgloss.Color(strconv.FormatInt(n.Int64(), 10))
}
