package model

import (
	"bytes"
	"cmp"
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
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/editor"
	xstrings "github.com/charmbracelet/x/exp/strings"
	"github.com/package-register/mocode/internal/admin"
	agentcore "github.com/package-register/mocode/internal/agent"
	"github.com/package-register/mocode/internal/agent/notify"
	agenttools "github.com/package-register/mocode/internal/agent/tools"
	execbuiltin "github.com/package-register/mocode/internal/agent/tools/builtin/exec"
	"github.com/package-register/mocode/internal/agent/tools/mcp"
	"github.com/package-register/mocode/internal/app"
	"github.com/package-register/mocode/internal/commands"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/fsext"
	"github.com/package-register/mocode/internal/history"
	"github.com/package-register/mocode/internal/infra/home"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/pubsub"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/session/sessionexport"
	"github.com/package-register/mocode/internal/skills"
	"github.com/package-register/mocode/internal/tools/shell"
	"github.com/package-register/mocode/internal/ui/anim"
	"github.com/package-register/mocode/internal/ui/attachments"
	"github.com/package-register/mocode/internal/ui/chat"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/completions"
	"github.com/package-register/mocode/internal/ui/dialog"
	fimage "github.com/package-register/mocode/internal/ui/image"
	"github.com/package-register/mocode/internal/ui/notification"
	"github.com/package-register/mocode/internal/ui/panel"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/ui/util"
	"github.com/package-register/mocode/internal/version"
	wechat "github.com/package-register/mocode/internal/wechat"
	"github.com/package-register/mocode/internal/workspace"
	"github.com/pkg/browser"
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
		Commands []commands.CustomCommand
	}
	// breathTickMsg is sent periodically while the agent is busy to animate
	// the editor prompt with a pulsing breathing effect.
	breathTickMsg struct{}
	// mcpPromptsLoadedMsg is sent when mcp prompts are loaded.
	mcpPromptsLoadedMsg struct {
		Prompts []commands.MCPPrompt
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
	customCommands []commands.CustomCommand
	mcpPrompts     []commands.MCPPrompt

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
		customCommands, err := commands.LoadCustomCommands(m.com.Config())
		if err != nil {
			slog.Error("Failed to load custom commands", "error", err)
		}
		return userCommandsLoadedMsg{Commands: customCommands}
	}
}

// loadMCPrompts loads the MCP prompts asynchronously.
func (m *UI) loadMCPrompts() tea.Msg {
	prompts, err := commands.LoadMCPPrompts()
	if err != nil {
		slog.Error("Failed to load MCP prompts", "error", err)
	}
	if prompts == nil {
		// flag them as loaded even if there is none or an error
		prompts = []commands.MCPPrompt{}
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

		// Recursively load nested tool calls for any agent tools within.
		nestedMessageItems := make([]chat.MessageItem, len(nestedTools))
		for i, nt := range nestedTools {
			nestedMessageItems[i] = nt
		}
		m.loadNestedToolCalls(nestedMessageItems)

		// Set nested tools on the parent.
		nestedContainer.SetNestedTools(nestedTools)
	}
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

func (m *UI) resumeBackgroundJobPolling(msgs []*message.Message) tea.Cmd {
	var cmds []tea.Cmd
	for _, msg := range msgs {
		if msg == nil || msg.Role != message.Tool {
			continue
		}
		for _, tr := range msg.ToolResults() {
			if cmd := m.maybeStartBackgroundJobPoll(tr); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *UI) maybeStartBackgroundJobPoll(tr message.ToolResult) tea.Cmd {
	if tr.ToolCallID == "" || tr.Metadata == "" {
		return nil
	}

	var meta execbuiltin.BashResponseMetadata
	if err := json.Unmarshal([]byte(tr.Metadata), &meta); err != nil {
		return nil
	}
	if !meta.Background || strings.TrimSpace(meta.ShellID) == "" {
		return nil
	}

	bgShell, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	if !ok {
		return nil
	}
	if m.backgroundJobs == nil {
		m.backgroundJobs = make(map[string]string)
	}
	if existingToolCallID, ok := m.backgroundJobs[meta.ShellID]; ok && existingToolCallID == tr.ToolCallID && bgShell.IsDone() {
		return nil
	}
	m.backgroundJobs[meta.ShellID] = tr.ToolCallID
	return m.pollBackgroundJobCmd(meta.ShellID)
}

func (m *UI) pollBackgroundJobCmd(shellID string) tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		bgShell, ok := shell.GetBackgroundShellManager().Get(shellID)
		if !ok {
			return backgroundJobOutputMsg{ShellID: shellID, Done: true}
		}

		stdout, stderr, done, err := bgShell.GetOutput()
		output := formatBackgroundShellOutput(stdout, stderr, err)
		if output == "" {
			output = "no output"
		}

		status := "running"
		if done {
			status = "completed"
			if err != nil && shell.IsInterrupt(err) {
				status = "interrupted"
			}
		}

		return backgroundJobOutputMsg{
			ShellID: shellID,
			Output:  fmt.Sprintf("Status: %s\n\n%s", status, output),
			Done:    done,
			Err:     err,
		}
	})
}

func (m *UI) handleBackgroundJobOutput(msg backgroundJobOutputMsg) tea.Cmd {
	toolCallID, ok := m.backgroundJobs[msg.ShellID]
	if !ok {
		return nil
	}
	if msg.Done {
		delete(m.backgroundJobs, msg.ShellID)
	}

	toolItem := m.findToolMessageItem(toolCallID)
	if toolItem == nil {
		return nil
	}

	current := toolItem.ToolCall()
	current.Finished = msg.Done
	toolItem.SetToolCall(current)

	res := toolItemCurrentResult(toolItem)
	if res == nil {
		res = &message.ToolResult{
			ToolCallID: toolCallID,
			Name:       current.Name,
		}
	}
	res.Content = msg.Output
	toolItem.SetResult(res)

	if msg.Done {
		return nil
	}
	return m.pollBackgroundJobCmd(msg.ShellID)
}

func (m *UI) findToolMessageItem(toolCallID string) chat.ToolMessageItem {
	if toolCallID == "" {
		return nil
	}
	item := m.chat.MessageItem(toolCallID)
	if toolItem, ok := item.(chat.ToolMessageItem); ok && toolItem.ToolCall().ID == toolCallID {
		return toolItem
	}
	container, ok := item.(chat.NestedToolContainer)
	if !ok {
		return nil
	}
	for _, nested := range container.NestedTools() {
		if nested.ToolCall().ID == toolCallID {
			return nested
		}
	}
	return nil
}

func toolItemCurrentResult(item chat.ToolMessageItem) *message.ToolResult {
	if item == nil {
		return nil
	}
	if item.Result() == nil {
		return nil
	}
	result := *item.Result()
	return &result
}

func formatBackgroundShellOutput(stdout, stderr string, execErr error) string {
	stdout = truncateBackgroundShellOutput(stdout)
	stderr = truncateBackgroundShellOutput(stderr)

	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	if execErr != nil && !shell.IsInterrupt(execErr) {
		if output != "" {
			output += "\n"
		}
		output += execErr.Error()
	}
	return output
}

func truncateBackgroundShellOutput(content string) string {
	const maxOutputLength = 30000
	if len(content) <= maxOutputLength {
		return content
	}
	halfLength := maxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]
	return fmt.Sprintf("%s\n\n... output truncated ...\n\n%s", start, end)
}

func (m *UI) handleClickFocus(msg tea.MouseClickMsg) (cmd tea.Cmd) {
	switch {
	case m.state != uiChat:
		return nil
	case image.Pt(msg.X, msg.Y).In(m.layout.sidebar):
		return nil
	case m.focus != uiFocusEditor && image.Pt(msg.X, msg.Y).In(m.layout.editor):
		m.focus = uiFocusEditor
		cmd = m.textarea.Focus()
		m.chat.Blur()
	case m.focus != uiFocusMain && image.Pt(msg.X, msg.Y).In(m.layout.main):
		m.focus = uiFocusMain
		m.textarea.Blur()
		m.chat.Focus()
	}
	return cmd
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

func (m *UI) handleDialogMsg(msg tea.Msg) tea.Cmd {
	action := m.dialog.Update(msg)
	if action == nil {
		return nil
	}
	return m.handleDialogAction(action)
}

// handleDialogAction processes an action produced by a dialog or dispatched
// directly (e.g., from slash command completions without a dialog open).
func (m *UI) handleDialogAction(action tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	isOnboarding := m.state == uiOnboarding

	switch msg := action.(type) {
	// Generic dialog messages
	case dialog.ActionClose:
		if isOnboarding && m.dialog.ContainsDialog(dialog.ModelsID) {
			break
		}

		if m.dialog.ContainsDialog(dialog.FilePickerID) {
			defer fimage.ResetCache()
		}

		m.dialog.CloseFrontDialog()

		if isOnboarding {
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

		if m.focus == uiFocusEditor {
			cmds = append(cmds, m.textarea.Focus())
		}
	case dialog.ActionCmd:
		if msg.Cmd != nil {
			cmds = append(cmds, msg.Cmd)
		}

	// Session dialog messages.
	case dialog.ActionSelectSession:
		m.dialog.CloseDialog(dialog.SessionsID)
		cmds = append(cmds, m.loadSession(msg.Session.ID))

	// Open dialog message.
	case dialog.ActionOpenDialog:
		if cmd := m.openDialog(msg.DialogID); cmd != nil {
			cmds = append(cmds, cmd)
		}

	// Command dialog messages.
	case dialog.ActionToggleYoloMode:
		yolo := !m.com.Workspace.PermissionSkipRequests()
		m.com.Workspace.PermissionSetSkipRequests(yolo)
		m.setEditorPrompt(yolo)
	case dialog.ActionToggleNotifications:
		cfg := m.com.Config()
		if cfg != nil && cfg.Options != nil {
			disabled := !cfg.Options.DisableNotifications
			cfg.Options.DisableNotifications = disabled
			if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.disable_notifications", disabled); err != nil {
				cmds = append(cmds, util.ReportError(err))
			} else {
				status := "enabled"
				if disabled {
					status = "disabled"
				}
				cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Notifications "+status)))
			}
		}
	case dialog.ActionNewSession:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
			break
		}
		if cmd := m.newSession(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionSummarize:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
			break
		}
		cmds = append(cmds, func() tea.Msg {
			err := m.com.Workspace.AgentSummarize(context.Background(), msg.SessionID)
			if err != nil {
				return util.ReportError(err)()
			}
			pattern := filepath.Join(m.com.Workspace.WorkingDir(), sessionexport.SummaryDir, "summary-"+sessionexport.SanitizeName(msg.SessionID)+"-*.md")
			matches, _ := filepath.Glob(pattern)
			if len(matches) > 0 {
				return util.NewInfoMsg("Session summary saved: " + matches[len(matches)-1])
			}
			return util.NewInfoMsg("Session summarized")
		})
	case dialog.ActionRollback:
		m.dialog.CloseDialog(dialog.RollbackID)
		cmds = append(cmds, m.performRollback(msg.Target))
	case dialog.ActionExportSession:
		cmds = append(cmds, m.exportSession(msg))
	case dialog.ActionExternalEditor:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is working, please wait..."))
			break
		}
		cmds = append(cmds, m.openEditor(m.textarea.Value()))
	case dialog.ActionToggleCompactMode:
		cmds = append(cmds, m.toggleCompactMode())
	case dialog.ActionTogglePills:
		if cmd := m.togglePillsExpanded(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionSetSessionTodos:
		if cmd := m.saveSessionTodos(msg.Todos); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionToggleThinking:
		cmds = append(cmds, func() tea.Msg {
			cfg := m.com.Config()
			if cfg == nil {
				return util.ReportError(errors.New("configuration not found"))()
			}

			agentCfg, ok := cfg.Agents[config.AgentCoder]
			if !ok {
				return util.ReportError(errors.New("agent configuration not found"))()
			}

			currentModel := cfg.Models[agentCfg.Model]
			currentModel.Think = !currentModel.Think
			if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, agentCfg.Model, currentModel); err != nil {
				return util.ReportError(err)()
			}
			if err := m.com.Workspace.UpdateAgentModel(context.TODO()); err != nil {
				return util.ReportError(err)()
			}
			status := "disabled"
			if currentModel.Think {
				status = "enabled"
			}
			return util.NewInfoMsg("Thinking mode " + status)
		})
	case dialog.ActionToggleTransparentBackground:
		cmds = append(cmds, func() tea.Msg {
			cfg := m.com.Config()
			if cfg == nil {
				return util.ReportError(errors.New("configuration not found"))()
			}

			isTransparent := cfg.Options != nil && cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent
			newValue := !isTransparent
			if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.tui.transparent", newValue); err != nil {
				return util.ReportError(err)()
			}
			m.isTransparent = newValue

			status := "disabled"
			if newValue {
				status = "enabled"
			}
			return util.NewInfoMsg("Transparent background " + status)
		})
	case dialog.ActionStartAdmin:
		cmds = append(cmds, m.startAdminServer(false))
	case dialog.ActionOpenAdmin:
		cmds = append(cmds, m.startAdminServer(true))
	case dialog.ActionStopAdmin:
		cmds = append(cmds, m.stopAdminServer())
	case dialog.ActionShowQuota:
		cmds = append(cmds, m.showMiniMaxQuota())
	case dialog.ActionSetProxyURL:
		if !msg.Enabled {
			cmds = append(cmds, m.setProxyURL(msg))
			break
		}
		if msg.Args == nil && msg.URL == "" {
			cfg := m.com.Config()
			proxyURL := "http://127.0.0.1:7890"
			noProxy := "localhost,127.0.0.1"
			if cfg != nil && cfg.Options != nil && cfg.Options.Network != nil {
				if cfg.Options.Network.ProxyURL != "" {
					proxyURL = cfg.Options.Network.ProxyURL
				}
				if cfg.Options.Network.NoProxy != "" {
					noProxy = cfg.Options.Network.NoProxy
				}
			}
			m.dialog.OpenDialog(dialog.NewArguments(
				m.com,
				"Network Proxy",
				"Configure a local proxy for provider tests, fetch/download, web search, MCP HTTP/SSE, and admin APIs.",
				[]commands.Argument{
					{ID: "PROXY_URL", Title: "Proxy URL", Description: proxyURL, Required: true},
					{ID: "NO_PROXY", Title: "No Proxy", Description: noProxy},
				},
				msg,
			))
			break
		}
		cmds = append(cmds, m.setProxyURL(msg))
		m.dialog.CloseDialog(dialog.ArgumentsID)
	case dialog.ActionQuit:
		cmds = append(cmds, m.quitWithSummary())
	case dialog.ActionEnableDockerMCP:
		cmds = append(cmds, m.enableDockerMCP)
	case dialog.ActionDisableDockerMCP:
		cmds = append(cmds, m.disableDockerMCP)
	case dialog.ActionToggleMCP:
		cmds = append(cmds, m.toggleMCP(msg.Name, msg.Enable))
	case dialog.ActionInitKnowledge:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before initializing knowledge..."))
			break
		}
		cmds = append(cmds, func() tea.Msg {
			written, err := m.com.Workspace.InitKnowledge(context.Background())
			if err != nil {
				return util.ReportError(err)()
			}
			if len(written) == 0 {
				return util.NewInfoMsg("Knowledge templates already initialized")
			}
			return util.NewInfoMsg(fmt.Sprintf("Initialized %d knowledge template(s)", len(written)))
		})
	case dialog.ActionInitializeProject:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
			break
		}
		cmds = append(cmds, m.initializeProject())

	case dialog.ActionSelectModel:
		if cmd := m.handleSelectModel(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionSelectMode:
		m.dialog.CloseDialog(dialog.ModesID)
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
			break
		}
		targetID := msg.ModeID
		if targetID == "" {
			break
		}
		if targetID == m.com.Workspace.CurrentAgentID() {
			cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Agent already active: "+targetID)))
			break
		}
		cmds = append(cmds, func() tea.Msg {
			if err := m.com.Workspace.SwitchAgent(context.Background(), targetID); err != nil {
				return util.NewErrorMsg(err)
			}
			return util.NewInfoMsg("Agent switched to " + targetID)
		})

	case dialog.ActionWeChatLogin:
		if cmd := m.handleWeChatLogin(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionWeChatLogout:
		if cmd := m.handleWeChatLogout(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ActionSelectWeChat:
		m.dialog.CloseDialog(dialog.WeChatManagerID)
		mgr := wechat.GetManager()
		if err := mgr.Switch(msg.AccountID); err != nil {
			cmds = append(cmds, util.CmdHandler(util.NewErrorMsg(err)))
		} else {
			// Re-initialize butler + slash config for the newly active channel.
			ch := mgr.GetActive()
			if ch != nil {
				injectWeChatButler(ch, m.com.Workspace)
			}
			cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Switched to WeChat account: "+msg.AccountID)))
		}
	case dialog.ActionWeChatReconnect:
		mgr := wechat.GetManager()
		if err := mgr.Reconnect(context.Background(), msg.AccountID); err != nil {
			cmds = append(cmds, util.CmdHandler(util.NewErrorMsg(err)))
		} else {
			cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Reconnected WeChat account: "+msg.AccountID)))
		}
	case dialog.ActionWeChatStart:
		mgr := wechat.GetManager()
		if err := mgr.Start(context.Background(), msg.AccountID); err != nil {
			cmds = append(cmds, util.CmdHandler(util.NewErrorMsg(err)))
		} else {
			cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Started WeChat account: "+msg.AccountID)))
		}
	case dialog.ActionWeChatStop:
		mgr := wechat.GetManager()
		mgr.Stop(msg.AccountID)
		cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Stopped WeChat account: "+msg.AccountID)))
	case dialog.ActionWeChatDelete:
		mgr := wechat.GetManager()
		if err := mgr.Delete(msg.AccountID); err != nil {
			cmds = append(cmds, util.CmdHandler(util.NewErrorMsg(err)))
		} else {
			// Re-initialize butler if the deleted account was the active one.
			if ch := mgr.GetActive(); ch != nil {
				injectWeChatButler(ch, m.com.Workspace)
			}
			cmds = append(cmds, util.CmdHandler(util.NewInfoMsg("Deleted WeChat account: "+msg.AccountID)))
		}
	case dialog.ActionSelectReasoningEffort:
		if m.isAgentBusy() {
			cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
			break
		}

		cfg := m.com.Config()
		if cfg == nil {
			cmds = append(cmds, util.ReportError(errors.New("configuration not found")))
			break
		}

		agentCfg, ok := cfg.Agents[config.AgentCoder]
		if !ok {
			cmds = append(cmds, util.ReportError(errors.New("agent configuration not found")))
			break
		}

		currentModel := cfg.Models[agentCfg.Model]
		currentModel.ReasoningEffort = msg.Effort
		if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, agentCfg.Model, currentModel); err != nil {
			cmds = append(cmds, util.ReportError(err))
			break
		}

		cmds = append(cmds, func() tea.Msg {
			if err := m.com.Workspace.UpdateAgentModel(context.TODO()); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Reasoning effort set to " + msg.Effort)
		})
		m.dialog.CloseDialog(dialog.ReasoningID)
	case dialog.ActionPermissionResponse:
		m.dialog.CloseDialog(dialog.PermissionsID)
		switch msg.Action {
		case dialog.PermissionAllow:
			m.com.Workspace.PermissionGrant(msg.Permission)
		case dialog.PermissionAllowForSession:
			m.com.Workspace.PermissionGrantPersistent(msg.Permission)
		case dialog.PermissionDeny:
			m.com.Workspace.PermissionDeny(msg.Permission)
		}

	case dialog.ActionFilePickerSelected:
		cmds = append(cmds, tea.Sequence(
			msg.Cmd(),
			func() tea.Msg {
				m.dialog.CloseDialog(dialog.FilePickerID)
				return nil
			},
			func() tea.Msg {
				fimage.ResetCache()
				return nil
			},
		))

	case dialog.ActionRunCustomCommand:
		if len(msg.Arguments) > 0 && msg.Args == nil {
			m.dialog.CloseFrontDialog()
			argsDialog := dialog.NewArguments(
				m.com,
				"Custom Command Arguments",
				"",
				msg.Arguments,
				msg, // Pass the action as the result
			)
			m.dialog.OpenDialog(argsDialog)
			break
		}
		content := msg.Content
		if msg.Args != nil {
			content = substituteArgs(content, msg.Args)
		}
		cmds = append(cmds, m.sendMessage(content))
		m.dialog.CloseFrontDialog()
	case dialog.ActionRunMCPPrompt:
		if len(msg.Arguments) > 0 && msg.Args == nil {
			m.dialog.CloseFrontDialog()
			title := cmp.Or(msg.Title, "MCP Prompt Arguments")
			argsDialog := dialog.NewArguments(
				m.com,
				title,
				msg.Description,
				msg.Arguments,
				msg, // Pass the action as the result
			)
			m.dialog.OpenDialog(argsDialog)
			break
		}
		cmds = append(cmds, m.runMCPPrompt(msg.ClientID, msg.PromptID, msg.Args))
	default:
		cmds = append(cmds, util.CmdHandler(msg))
	}

	return tea.Batch(cmds...)
}

// substituteArgs replaces $ARG_NAME placeholders in content with actual values.
func substituteArgs(content string, args map[string]string) string {
	for name, value := range args {
		placeholder := "$" + name
		content = strings.ReplaceAll(content, placeholder, value)
	}
	return content
}

// handleSelectModel performs the model selection after any provider
// pre-checks have completed.
func (m *UI) handleSelectModel(msg dialog.ActionSelectModel) tea.Cmd {
	var cmds []tea.Cmd

	if m.isAgentBusy() {
		return util.ReportWarn("Agent is busy, please wait...")
	}

	cfg := m.com.Config()
	if cfg == nil {
		return util.ReportError(errors.New("configuration not found"))
	}

	var (
		providerID   = msg.Model.Provider
		isConfigured = func() bool { _, ok := cfg.Providers.Get(providerID); return ok }
		isOnboarding = m.state == uiOnboarding
	)

	if !isConfigured() || msg.ReAuthenticate {
		m.dialog.CloseDialog(dialog.ModelsID)
		if cmd := m.openAuthenticationDialog(msg.Provider, msg.Model, msg.ModelType); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	}

	if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, msg.ModelType, msg.Model); err != nil {
		cmds = append(cmds, util.ReportError(err))
	} else {
		if msg.ModelType == config.SelectedModelTypeLarge {
			// Swap the theme live based on the newly selected large
			// model's provider.
			m.applyTheme(styles.ThemeForProvider(providerID))
		}
		if _, ok := cfg.Models[config.SelectedModelTypeSmall]; !ok {
			// Ensure small model is set is unset.
			smallModel := m.com.Workspace.GetDefaultSmallModel(providerID)
			if err := m.com.Workspace.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeSmall, smallModel); err != nil {
				cmds = append(cmds, util.ReportError(err))
			}
		}
	}

	cmds = append(cmds, func() tea.Msg {
		if err := m.com.Workspace.UpdateAgentModel(context.TODO()); err != nil {
			return util.ReportError(err)
		}

		modelMsg := fmt.Sprintf("%s model changed to %s", msg.ModelType, msg.Model.Model)

		return util.NewInfoMsg(modelMsg)
	})

	m.dialog.CloseDialog(dialog.APIKeyInputID)
	m.dialog.CloseDialog(dialog.OAuthID)
	m.dialog.CloseDialog(dialog.ModelsID)

	if isOnboarding {
		m.setState(uiLanding, uiFocusEditor)
		m.com.Config().SetupAgents()
		if err := m.com.Workspace.InitCoderAgent(context.TODO()); err != nil {
			cmds = append(cmds, util.ReportError(err))
		}
	}

	return tea.Batch(cmds...)
}

func (m *UI) openAuthenticationDialog(provider catwalk.Provider, model config.SelectedModel, modelType config.SelectedModelType) tea.Cmd {
	var (
		dlg dialog.Dialog
		cmd tea.Cmd

		isOnboarding = m.state == uiOnboarding
	)

	dlg, cmd = dialog.NewAPIKeyInput(m.com, isOnboarding, provider, model, modelType)

	if m.dialog.ContainsDialog(dlg.ID()) {
		m.dialog.BringToFront(dlg.ID())
		return nil
	}

	m.dialog.OpenDialog(dlg)
	return cmd
}

func (m *UI) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	var cmds []tea.Cmd

	handleGlobalKeys := func(msg tea.KeyPressMsg) bool {
		switch {
		case key.Matches(msg, m.keyMap.Commands):
			if cmd := m.openSlashCompletions(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.Models):
			if cmd := m.openModelsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.Sessions):
			if cmd := m.openSessionsDialog(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return true
		case key.Matches(msg, m.keyMap.Chat.Details) && m.isCompact:
			m.detailsOpen = !m.detailsOpen
			m.updateLayoutAndSize()
			return true
		case key.Matches(msg, m.keyMap.Chat.TogglePills):
			if m.state == uiChat && m.hasSession() {
				if cmd := m.togglePillsExpanded(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return true
			}
		case key.Matches(msg, m.keyMap.Chat.PillLeft):
			if m.state == uiChat && m.hasSession() && m.pillsExpanded && m.focus != uiFocusEditor {
				if cmd := m.switchPillSection(-1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return true
			}
		case key.Matches(msg, m.keyMap.Chat.PillRight):
			if m.state == uiChat && m.hasSession() && m.pillsExpanded && m.focus != uiFocusEditor {
				if cmd := m.switchPillSection(1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				return true
			}
		case key.Matches(msg, m.keyMap.Suspend):
			if m.isAgentBusy() {
				cmds = append(cmds, util.ReportWarn("Agent is busy, please wait..."))
				return true
			}
			cmds = append(cmds, tea.Suspend)
			return true
		}
		// Handle ctrl+1 through ctrl+9 for quick agent switching.
		if msg.String() >= "ctrl+1" && msg.String() <= "ctrl+9" {
			agents := m.com.Workspace.AvailableAgents()
			idx := int(msg.String()[5] - '1') // "ctrl+1" → index 0
			if idx >= 0 && idx < len(agents) {
				targetID := agents[idx].ID
				if targetID != m.com.Workspace.CurrentAgentID() && !m.isAgentBusy() {
					cmds = append(cmds, func() tea.Msg {
						if err := m.com.Workspace.SwitchAgent(context.Background(), targetID); err != nil {
							return util.NewErrorMsg(err)
						}
						return util.NewInfoMsg("Switched to " + agents[idx].Name)
					})
				}
			}
			return true
		}
		return false
	}

	if key.Matches(msg, m.keyMap.Quit) && !m.dialog.ContainsDialog(dialog.QuitID) {
		// Always handle quit keys first
		if cmd := m.openQuitDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}

		return tea.Batch(cmds...)
	}

	// Route all messages to dialog if one is open.
	if m.dialog.HasDialogs() {
		return m.handleDialogMsg(msg)
	}

	// Handle cancel key when agent is busy.
	if key.Matches(msg, m.keyMap.Chat.Cancel) {
		if m.isAgentBusy() {
			if cmd := m.cancelAgent(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return tea.Batch(cmds...)
		}
	}

	switch m.state {
	case uiOnboarding:
		return tea.Batch(cmds...)
	case uiInitialize:
		cmds = append(cmds, m.updateInitializeView(msg)...)
		return tea.Batch(cmds...)
	case uiChat, uiLanding:
		switch m.focus {
		case uiFocusEditor:
			// Handle completions if open.
			if m.completionsOpen {
				if msg, ok := m.completions.Update(msg); ok {
					switch msg := msg.(type) {
					case completions.SelectionMsg[completions.FileCompletionValue]:
						cmds = append(cmds, m.insertFileCompletion(msg.Value.Path))
						if !msg.KeepOpen {
							m.closeCompletions()
						}
					case completions.SelectionMsg[completions.ResourceCompletionValue]:
						cmds = append(cmds, m.insertMCPResourceCompletion(msg.Value))
						if !msg.KeepOpen {
							m.closeCompletions()
						}
					case completions.SelectionMsg[completions.AtCompletionValue]:
						cmds = append(cmds, m.insertAtCompletion(msg.Value))
						if !msg.KeepOpen && !msg.Value.IsCategory {
							m.closeCompletions()
						}
					case completions.SelectionMsg[completions.SlashCompletionValue]:
						prevHeight := m.textarea.Height()
						m.textarea.Reset()
						m.editorAllSelected = false
						if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
							cmds = append(cmds, cmd)
						}
						m.closeCompletions()
						if msg.Value.Msg != nil {
							// Handle the action directly without going through the message
							// queue, so it works even when no dialog is currently open.
							if cmd := m.handleDialogAction(msg.Value.Msg); cmd != nil {
								cmds = append(cmds, cmd)
							}
						} else if cmd, handled := m.handleSlashCommand(msg.Value.Command); handled {
							if cmd != nil {
								cmds = append(cmds, cmd)
							}
						}
						return tea.Batch(cmds...)
					case completions.ClosedMsg:
						m.completionsOpen = false
					}
					return tea.Batch(cmds...)
				}
			}

			if ok := m.attachments.Update(msg); ok {
				return tea.Batch(cmds...)
			}

			switch {
			case key.Matches(msg, m.keyMap.Editor.AddImage):
				if !m.currentModelSupportsImages() {
					break
				}
				if cmd := m.openFilesDialog(); cmd != nil {
					cmds = append(cmds, cmd)
				}

			case key.Matches(msg, m.keyMap.Editor.PasteImage):
				if !m.currentModelSupportsImages() {
					break
				}
				cmds = append(cmds, m.pasteImageFromClipboard)

			case key.Matches(msg, m.keyMap.Editor.SelectAll):
				m.selectAllEditorText()

			case key.Matches(msg, m.keyMap.Editor.Cut):
				if cmd := m.cutEditorText(); cmd != nil {
					cmds = append(cmds, cmd)
				}

			case key.Matches(msg, m.keyMap.Editor.SendMessage):
				prevHeight := m.textarea.Height()
				value := m.textarea.Value()
				if before, ok := strings.CutSuffix(value, "\\"); ok {
					// If the last character is a backslash, remove it and add a newline.
					m.textarea.SetValue(before)
					if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
						cmds = append(cmds, cmd)
					}
					break
				}

				// Otherwise, send the message
				m.textarea.Reset()
				m.editorAllSelected = false
				if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
					cmds = append(cmds, cmd)
				}

				value = strings.TrimSpace(value)
				if value == "exit" || value == "quit" {
					return m.openQuitDialog()
				}

				attachments := m.attachments.List()
				m.attachments.Reset()
				if len(value) == 0 && !message.ContainsTextAttachment(attachments) {
					return nil
				}

				m.randomizePlaceholders()
				m.historyReset()

				if strings.HasPrefix(value, "/") {
					if cmd, handled := m.handleSlashCommand(value); handled {
						if cmd != nil {
							cmds = append(cmds, cmd)
						}
						return tea.Batch(cmds...)
					}
				}

				return tea.Batch(m.sendMessage(value, attachments...), m.loadPromptHistory())
			case key.Matches(msg, m.keyMap.Chat.NewSession):
				if !m.hasSession() {
					break
				}
				if m.isAgentBusy() {
					cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
					break
				}
				if cmd := m.newSession(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Tab):
				if m.state != uiLanding {
					m.setState(m.state, uiFocusMain)
					m.textarea.Blur()
					m.chat.Focus()
					m.chat.SetSelected(m.chat.Len() - 1)
				}
			case key.Matches(msg, m.keyMap.Editor.OpenEditor):
				if m.isAgentBusy() {
					cmds = append(cmds, util.ReportWarn("Agent is working, please wait..."))
					break
				}
				cmds = append(cmds, m.openEditor(m.textarea.Value()))
			case key.Matches(msg, m.keyMap.Editor.Newline):
				prevHeight := m.textarea.Height()
				if m.editorAllSelected {
					m.textarea.Reset()
					m.editorAllSelected = false
				}
				m.textarea.InsertRune('\n')
				m.closeCompletions()
				cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
			case key.Matches(msg, m.keyMap.Editor.HistoryPrev):
				cmd := m.handleHistoryUp(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.HistoryNext):
				cmd := m.handleHistoryDown(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.Escape):
				cmd := m.handleHistoryEscape(msg)
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Editor.Commands) && m.textarea.Value() == "":
				if cmd := m.openSlashCompletions(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			default:
				if handleGlobalKeys(msg) {
					// Handle global keys first before passing to textarea.
					break
				}

				// Check for @ trigger before passing to textarea.
				curValue := m.textarea.Value()
				curIdx := len(curValue)
				if m.editorAllSelected && editorKeyReplacesSelection(msg) {
					prevHeight := m.textarea.Height()
					m.textarea.Reset()
					m.editorAllSelected = false
					if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
						cmds = append(cmds, cmd)
					}
					curValue = ""
					curIdx = 0
				} else if m.editorAllSelected && !key.Matches(msg, m.keyMap.Editor.SelectAll) {
					m.editorAllSelected = false
				}

				// Trigger completions on @.
				if msg.String() == "@" && !m.completionsOpen {
					// Only show if beginning of prompt or after whitespace.
					if curIdx == 0 || (curIdx > 0 && isWhitespace(curValue[curIdx-1])) {
						m.completionsOpen = true
						m.completionsSlashMode = false
						m.completionsAtMode = true
						m.completionsQuery = ""
						m.completionsStartIndex = curIdx
						m.completionsPositionStart = m.completionsPosition()
						depth, limit := m.com.Config().Options.TUI.Completions.Limits()
						cmds = append(cmds, m.openAtCompletions(depth, limit))
					}
				}

				// remove the details if they are open when user starts typing
				if m.detailsOpen {
					m.detailsOpen = false
					m.updateLayoutAndSize()
				}

				prevHeight := m.textarea.Height()
				if msg.String() == "backspace" {
					if newValue, ok := deleteTrailingMention(curValue); ok {
						m.textarea.SetValue(newValue)
						m.textarea.MoveToEnd()
						cmds = append(cmds, m.handleTextareaHeightChange(prevHeight))
					} else {
						cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
					}
				} else {
					cmds = append(cmds, m.updateTextareaWithPrevHeight(msg, prevHeight))
				}

				// Any text modification becomes the current draft.
				m.updateHistoryDraft(curValue)

				// After updating textarea, check if we need to filter completions.
				// Skip filtering on the initial @ / / keystroke since items may load async.
				if m.completionsOpen && msg.String() != "@" && msg.String() != "/" {
					newValue := m.textarea.Value()
					newIdx := len(newValue)

					// Close completions if cursor moved before start.
					if newIdx <= m.completionsStartIndex {
						m.closeCompletions()
					} else if msg.String() == "space" {
						// Close on space.
						m.closeCompletions()
					} else {
						// Extract current word and filter.
						word := m.textareaWord()
						if m.completionsSlashMode {
							if strings.HasPrefix(word, "/") {
								m.completionsQuery = word[1:]
								m.completions.Filter(m.completionsQuery)
							} else {
								m.closeCompletions()
							}
						} else if m.completionsAtMode && strings.HasPrefix(word, "@") {
							m.completionsQuery = word[1:]
							m.completions.Filter(m.completionsQuery)
						} else if strings.HasPrefix(word, "@") {
							m.completionsQuery = word[1:]
							m.completions.Filter(m.completionsQuery)
						} else if m.completionsOpen {
							m.closeCompletions()
						}
					}
				}
			}
		case uiFocusMain:
			switch {
			case key.Matches(msg, m.keyMap.Tab):
				m.focus = uiFocusEditor
				cmds = append(cmds, m.textarea.Focus())
				m.chat.Blur()
			case key.Matches(msg, m.keyMap.Chat.NewSession):
				if !m.hasSession() {
					break
				}
				if m.isAgentBusy() {
					cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before starting a new session..."))
					break
				}
				m.focus = uiFocusEditor
				if cmd := m.newSession(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.Expand):
				m.chat.ToggleExpandedSelectedItem()
			case key.Matches(msg, m.keyMap.Chat.Up):
				if cmd := m.chat.ScrollByAndAnimate(-1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectPrev()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case key.Matches(msg, m.keyMap.Chat.Down):
				if cmd := m.chat.ScrollByAndAnimate(1); cmd != nil {
					cmds = append(cmds, cmd)
				}
				if !m.chat.SelectedItemInView() {
					m.chat.SelectNext()
					if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case key.Matches(msg, m.keyMap.Chat.UpOneItem):
				m.chat.SelectPrev()
				if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.DownOneItem):
				m.chat.SelectNext()
				if cmd := m.chat.ScrollToSelectedAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case key.Matches(msg, m.keyMap.Chat.HalfPageUp):
				if cmd := m.chat.ScrollByAndAnimate(-m.chat.Height() / 2); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectFirstInView()
			case key.Matches(msg, m.keyMap.Chat.HalfPageDown):
				if cmd := m.chat.ScrollByAndAnimate(m.chat.Height() / 2); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectLastInView()
			case key.Matches(msg, m.keyMap.Chat.PageUp):
				if cmd := m.chat.ScrollByAndAnimate(-m.chat.Height()); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectFirstInView()
			case key.Matches(msg, m.keyMap.Chat.PageDown):
				if cmd := m.chat.ScrollByAndAnimate(m.chat.Height()); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectLastInView()
			case key.Matches(msg, m.keyMap.Chat.Home):
				if cmd := m.chat.ScrollToTopAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectFirst()
			case key.Matches(msg, m.keyMap.Chat.End):
				if cmd := m.chat.ScrollToBottomAndAnimate(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				m.chat.SelectLast()
			default:
				if ok, cmd := m.chat.HandleKeyMsg(msg); ok {
					cmds = append(cmds, cmd)
				} else {
					handleGlobalKeys(msg)
				}
			}
		default:
			handleGlobalKeys(msg)
		}
	default:
		handleGlobalKeys(msg)
	}

	return tea.Sequence(cmds...)
}

// drawHeader draws the header section of the UI.
func (m *UI) drawHeader(scr uv.Screen, area uv.Rectangle) {
	m.header.drawHeader(
		scr,
		area,
		m.session,
		m.isCompact,
		m.detailsOpen,
		area.Dx(),
		nil,
		m.headerStats(),
	)
}

func (m *UI) hudTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return hudTickMsg{}
	})
}

func (m *UI) startAdminServer(open bool) tea.Cmd {
	return func() tea.Msg {
		url, err := m.adminServer.Start(context.Background(), 0)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if open {
			if err := browser.OpenURL(url); err != nil {
				return util.NewErrorMsg(err)
			}
			return util.NewInfoMsg("Admin panel opened: " + url)
		}
		return util.NewInfoMsg("Admin server started: " + url)
	}
}

func (m *UI) stopAdminServer() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.adminServer.Stop(ctx); err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Admin server stopped")
	}
}

func (m *UI) showMiniMaxQuota() tea.Cmd {
	return m.openMiniMaxQuotaDialog()
}

func (m *UI) setProxyURL(action dialog.ActionSetProxyURL) tea.Cmd {
	return func() tea.Msg {
		proxyURL := strings.TrimSpace(action.URL)
		noProxy := "localhost,127.0.0.1"
		if !action.Enabled {
			if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.enabled", false); err != nil {
				return util.NewErrorMsg(err)
			}
			return util.NewInfoMsg("Network proxy disabled")
		}
		if action.Args != nil {
			proxyURL = strings.TrimSpace(action.Args["PROXY_URL"])
			if value := strings.TrimSpace(action.Args["NO_PROXY"]); value != "" {
				noProxy = value
			}
		}
		if proxyURL == "" {
			return util.NewErrorMsg(errors.New("proxy URL is required"))
		}
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.enabled", action.Enabled); err != nil {
			return util.NewErrorMsg(err)
		}
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.proxy_url", proxyURL); err != nil {
			return util.NewErrorMsg(err)
		}
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.no_proxy", noProxy); err != nil {
			return util.NewErrorMsg(err)
		}
		status := "disabled"
		if action.Enabled {
			status = "enabled"
		}
		return util.NewInfoMsg("Network proxy " + status + ": " + proxyURL)
	}
}

func (m *UI) headerStats() headerStats {
	stats := headerStats{
		startedAt: m.startedAt,
		frame:     m.hudFrame,
	}
	if m.session != nil {
		stats.cacheReadTokens = m.session.CacheReadTokens
		stats.cacheCreationTokens = m.session.CacheCreationTokens
		stats.hasCache = m.session.CacheReadTokens > 0 || m.session.CacheCreationTokens > 0
	}
	return stats
}

func (m *UI) statusLine(width int) string {
	if width <= 0 {
		return ""
	}
	t := m.com.Styles
	if m.state == uiLanding {
		return ansi.Truncate(strings.Join([]string{
			t.Header.Keystroke.Render("ctrl+p") + t.Header.KeystrokeTip.Render(" slash"),
		}, t.Header.Separator.Render(" │ ")), max(0, width), "…")
	}
	var parts []string

	// Show agent status if active
	if m.agentStatus != "" {
		parts = append(parts, t.Header.Keystroke.Render("status ")+t.Header.KeystrokeTip.Render(m.agentStatus))
	}

	if model := m.selectedLargeModel(); model != nil {
		parts = append(parts, t.Header.Keystroke.Render("model ")+t.Header.KeystrokeTip.Render(model.CatwalkCfg.Name))
	}
	if mode := activeAgentMode(m.com); mode != "" {
		parts = append(parts, t.Header.Keystroke.Render("agent ")+t.Header.KeystrokeTip.Render(mode))
	}
	servers, enabled, tools, prompts, resources := m.mcpSummaryCounts()
	if servers > 0 {
		parts = append(parts, t.Header.Keystroke.Render("mcp ")+t.Header.KeystrokeTip.Render(fmt.Sprintf("%d/%d servers %d tools", enabled, servers, tools)))
		if prompts+resources > 0 && width > 100 {
			parts = append(parts, t.Header.KeystrokeTip.Render(fmt.Sprintf("%d prompts %d res", prompts, resources)))
		}
	}
	if diagnostics := m.lspDiagnosticCount(); diagnostics > 0 {
		parts = append(parts, t.LSP.ErrorDiagnostic.Render(fmt.Sprintf("%s%d lsp", styles.LSPErrorIcon, diagnostics)))
	}
	if m.promptQueue > 0 {
		parts = append(parts, t.Header.Keystroke.Render("queue ")+t.Header.KeystrokeTip.Render(strconv.Itoa(m.promptQueue)))
	}
	parts = append(
		parts,
		t.Header.Keystroke.Render("ctrl+p")+t.Header.KeystrokeTip.Render(" slash"),
	)

	line := strings.Join(parts, t.Header.Separator.Render(" │ "))
	return ansi.Truncate(line, max(0, width), "…")
}

func (m *UI) lspDiagnosticCount() int {
	total := 0
	for _, state := range m.lspStates {
		total += state.DiagnosticCount
	}
	return total
}

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
	v.WindowTitle = "mocode " + home.Short(m.com.Workspace.WorkingDir())

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

func (m *UI) currentModelSupportsImages() bool {
	cfg := m.com.Config()
	if cfg == nil {
		return false
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return false
	}
	model := cfg.GetModelByType(agentCfg.Model)
	return model != nil && model.SupportsImages
}

// toggleCompactMode toggles compact mode between uiChat and uiChatCompact states.
func (m *UI) toggleCompactMode() tea.Cmd {
	m.forceCompactMode = !m.forceCompactMode

	err := m.com.Workspace.SetCompactMode(config.ScopeGlobal, m.forceCompactMode)
	if err != nil {
		return util.ReportError(err)
	}

	m.updateLayoutAndSize()

	return nil
}

// updateLayoutAndSize updates the layout and sizes of UI components.
func (m *UI) updateLayoutAndSize() {
	// Determine if we should be in compact mode
	if m.state == uiChat {
		if m.forceCompactMode {
			m.isCompact = true
		} else if m.width < compactModeWidthBreakpoint || m.height < compactModeHeightBreakpoint {
			m.isCompact = true
		} else {
			m.isCompact = false
		}
	}

	// First pass sizes components from the current textarea height.
	m.layout = m.generateLayout(m.width, m.height)
	prevHeight := m.textarea.Height()
	m.updateSize()

	// SetWidth can change textarea height due to soft-wrap recalculation.
	// If that happens, run one reconciliation pass with the new height.
	if m.textarea.Height() != prevHeight {
		m.layout = m.generateLayout(m.width, m.height)
		m.updateSize()
	}
}

// handleTextareaHeightChange checks whether the textarea height changed and,
// if so, recalculates the layout. When the chat is in follow mode it keeps
// the view scrolled to the bottom. The returned command, if non-nil, must be
// batched by the caller.
func (m *UI) handleTextareaHeightChange(prevHeight int) tea.Cmd {
	if m.textarea.Height() == prevHeight {
		return nil
	}
	m.updateLayoutAndSize()
	if m.state == uiChat && m.chat.Follow() {
		return m.chat.ScrollToBottomAndAnimate()
	}
	return nil
}

// updateTextarea updates the textarea for msg and then reconciles layout if
// the textarea height changed as a result.
func (m *UI) updateTextarea(msg tea.Msg) tea.Cmd {
	return m.updateTextareaWithPrevHeight(msg, m.textarea.Height())
}

// updateTextareaWithPrevHeight is for cases when the height of the layout may
// have changed.
//
// Particularly, it's for cases where the textarea changes before
// textarea.Update is called (for example, SetValue, Reset, and InsertRune). We
// pass the height from before those changes took place so we can compare
// "before" vs "after" sizing and recalculate the layout if the textarea grew
// or shrank.
func (m *UI) updateTextareaWithPrevHeight(msg tea.Msg, prevHeight int) tea.Cmd {
	ta, cmd := m.textarea.Update(msg)
	m.textarea = ta
	return tea.Batch(cmd, m.handleTextareaHeightChange(prevHeight))
}

// updateSize updates the sizes of UI components based on the current layout.
func (m *UI) updateSize() {
	m.chat.SetSize(m.layout.main.Dx(), m.layout.main.Dy())
	m.textarea.MaxHeight = TextareaMaxHeight
	m.textarea.SetWidth(m.layout.editor.Dx())
	m.renderPills()
}

// generateLayout calculates the layout rectangles for all UI components based
// on the current UI state and terminal dimensions.
func (m *UI) generateLayout(w, h int) uiLayout {
	// The screen area we're working with
	area := image.Rect(0, 0, w, h)

	// The help height
	helpHeight := 1
	// The editor height: textarea height + margin for attachments and bottom spacing.
	editorHeight := m.textarea.Height() + editorHeightMargin
	// The sidebar width
	sidebarWidth := 30
	// The header height
	const headerHeight = 1

	// Add app margins
	var appRect, helpRect image.Rectangle
	layout.Vertical(
		layout.Len(area.Dy()-helpHeight),
		layout.Fill(1),
	).Split(area).Assign(&appRect, &helpRect)
	appRect.Min.Y += 1
	appRect.Max.Y -= 1
	helpRect.Min.Y -= 1
	appRect.Min.X += 1
	appRect.Max.X -= 1

	if slices.Contains([]uiState{uiOnboarding, uiInitialize, uiLanding}, m.state) {
		// extra padding on left and right for these states
		appRect.Min.X += 1
		appRect.Max.X -= 1
	}

	uiLayout := uiLayout{
		area:   area,
		status: helpRect,
	}

	// Handle different app states
	switch m.state {
	case uiOnboarding, uiInitialize:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// help

		var headerRect, mainRect image.Rectangle
		layout.Vertical(
			layout.Len(headerHeight),
			layout.Fill(1),
		).Split(appRect).Assign(&headerRect, &mainRect)
		uiLayout.header = headerRect
		uiLayout.main = mainRect

	case uiLanding:
		// Layout
		//
		// header
		// ------
		// main
		// ------
		// editor
		// ------
		// help
		var headerRect, mainRect image.Rectangle
		layout.Vertical(
			layout.Len(headerHeight),
			layout.Fill(1),
		).Split(appRect).Assign(&headerRect, &mainRect)
		var editorRect image.Rectangle
		layout.Vertical(
			layout.Len(mainRect.Dy()-editorHeight),
			layout.Fill(1),
		).Split(mainRect).Assign(&mainRect, &editorRect)
		// Remove extra padding from editor (but keep it for header and main)
		editorRect.Min.X -= 1
		editorRect.Max.X += 1
		uiLayout.header = headerRect
		uiLayout.main = mainRect
		uiLayout.editor = editorRect

	case uiChat:
		if m.isCompact {
			// Layout
			//
			// compact-header
			// ------
			// main
			// ------
			// editor
			// ------
			// help
			const compactHeaderHeight = 1
			var headerRect, mainRect image.Rectangle
			layout.Vertical(
				layout.Len(compactHeaderHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&headerRect, &mainRect)
			detailsHeight := min(sessionDetailsMaxHeight, area.Dy()-1) // One row for the header
			var sessionDetailsArea image.Rectangle
			layout.Vertical(
				layout.Len(detailsHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&sessionDetailsArea, new(image.Rectangle))
			uiLayout.sessionDetails = sessionDetailsArea
			uiLayout.sessionDetails.Min.Y += compactHeaderHeight // adjust for header
			// Add one line gap between header and main content
			mainRect.Min.Y += 1
			var editorRect image.Rectangle
			layout.Vertical(
				layout.Len(mainRect.Dy()-editorHeight),
				layout.Fill(1),
			).Split(mainRect).Assign(&mainRect, &editorRect)
			mainRect.Max.X -= 1 // Add padding right
			uiLayout.header = headerRect
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				var chatRect, pillsRect image.Rectangle
				layout.Vertical(
					layout.Len(mainRect.Dy()-pillsHeight),
					layout.Fill(1),
				).Split(mainRect).Assign(&chatRect, &pillsRect)
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor = editorRect
		} else {
			// Layout
			//
			// ------|---
			// main  |
			// ------| side
			// editor|
			// ----------
			// help

			var headerRect, bodyRect image.Rectangle
			layout.Vertical(
				layout.Len(headerHeight),
				layout.Fill(1),
			).Split(appRect).Assign(&headerRect, &bodyRect)
			var mainRect, sideRect image.Rectangle
			layout.Horizontal(
				layout.Len(bodyRect.Dx()-sidebarWidth),
				layout.Fill(1),
			).Split(bodyRect).Assign(&mainRect, &sideRect)
			uiLayout.header = headerRect
			// Add padding left
			sideRect.Min.X += 1
			var editorRect image.Rectangle
			layout.Vertical(
				layout.Len(mainRect.Dy()-editorHeight),
				layout.Fill(1),
			).Split(mainRect).Assign(&mainRect, &editorRect)
			mainRect.Max.X -= 1 // Add padding right
			uiLayout.sidebar = sideRect
			pillsHeight := m.pillsAreaHeight()
			if pillsHeight > 0 {
				pillsHeight = min(pillsHeight, mainRect.Dy())
				var chatRect, pillsRect image.Rectangle
				layout.Vertical(
					layout.Len(mainRect.Dy()-pillsHeight),
					layout.Fill(1),
				).Split(mainRect).Assign(&chatRect, &pillsRect)
				uiLayout.main = chatRect
				uiLayout.pills = pillsRect
			} else {
				uiLayout.main = mainRect
			}
			// Add bottom margin to main
			uiLayout.main.Max.Y -= 1
			uiLayout.editor = editorRect
		}
	}

	return uiLayout
}

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

func (m *UI) openEditor(value string) tea.Cmd {
	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	tmpPath := tmpfile.Name()
	defer tmpfile.Close() //nolint:errcheck
	if _, err := tmpfile.WriteString(value); err != nil {
		return util.ReportError(err)
	}
	cmd, err := editor.Command(
		"mocode",
		tmpPath,
		editor.AtPosition(
			m.textarea.Line()+1,
			m.textarea.Column()+1,
		),
	)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err != nil {
			return util.ReportError(err)
		}
		content, err := os.ReadFile(tmpPath)
		if err != nil {
			return util.ReportError(err)
		}
		if len(content) == 0 {
			return util.ReportWarn("Message is empty")
		}
		return openEditorMsg{
			Text: strings.TrimSpace(string(content)),
		}
	})
}

// setEditorPrompt configures the textarea prompt function based on whether
// yolo mode is enabled.
func (m *UI) setEditorPrompt(yolo bool) {
	if yolo {
		m.textarea.SetPromptFunc(4, m.yoloPromptFunc)
		return
	}
	m.textarea.SetPromptFunc(4, m.normalPromptFunc)
}

// normalPromptFunc returns the normal editor prompt style ("  > " on first
// line, "::: " on subsequent lines).
// When the agent is busy, the "::: " dots alternate between bright and dim
// colors to create a breathing/pulsing effect.
func (m *UI) normalPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return "  > "
		}
		return "::: "
	}
	if info.Focused {
		return t.Editor.PromptNormalFocused.Render()
	}
	// When the agent is busy, alternate between bright and dim (breathing).
	if m.breathOn {
		return t.Editor.PromptNormalFocused.Render()
	}
	return t.Editor.PromptNormalBlurred.Render()
}

// yoloPromptFunc returns the yolo mode editor prompt style with warning icon
// and colored dots.
func (m *UI) yoloPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.Editor.PromptYoloIconFocused.Render()
		} else {
			return t.Editor.PromptYoloIconBlurred.Render()
		}
	}
	if info.Focused {
		return t.Editor.PromptYoloDotsFocused.Render()
	}
	return t.Editor.PromptYoloDotsBlurred.Render()
}

func (m *UI) openSlashCompletions() tea.Cmd {
	noop := func() tea.Msg { return nil }
	prevHeight := m.textarea.Height()
	value := m.textarea.Value()
	word := m.textareaWord()
	needsSlash := value == "" || !strings.HasPrefix(word, "/")
	if needsSlash {
		m.textarea.InsertRune('/')
		if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
			m.completionsOpen = true
			m.completionsSlashMode = true
			m.completionsAtMode = false
			m.completionsQuery = ""
			m.completionsStartIndex = max(0, len(m.textarea.Value())-1)
			m.completionsPositionStart = m.completionsPosition()
			m.completions.SetSlashGroups(
				m.slashCompletionGroups(),
				m.com.Styles,
				max(m.layout.editor.Dx(), 10),
			)
			return cmd
		}
	}

	m.completionsOpen = true
	m.completionsSlashMode = true
	m.completionsAtMode = false
	if strings.HasPrefix(m.textareaWord(), "/") {
		m.completionsQuery = strings.TrimPrefix(m.textareaWord(), "/")
	} else {
		m.completionsQuery = ""
	}
	m.completionsStartIndex = max(0, len(m.textarea.Value())-len(m.textareaWord()))
	m.completionsPositionStart = m.completionsPosition()
	m.completions.SetSlashGroups(
		m.slashCompletionGroups(),
		m.com.Styles,
		max(m.layout.editor.Dx(), 10),
	)
	if m.completionsQuery != "" {
		m.completions.Filter(m.completionsQuery)
	}
	return noop
}

// closeCompletions closes the completions popup and resets state.
func (m *UI) closeCompletions() {
	m.completionsOpen = false
	m.completionsSlashMode = false
	m.completionsAtMode = false
	m.completionsQuery = ""
	m.completionsStartIndex = 0
	m.completions.Close()
}

// slashCompletionGroups returns slash commands organised into named groups.
// The set mirrors the Commands dialog and is filtered by the same context
// guards (session presence, model capabilities, environment variables).
func (m *UI) slashCompletionGroups() []completions.SlashGroup {
	hasSession := m.hasSession()
	hasTodos := hasSession && hasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0
	cfg := m.com.Config()

	v := func(cmd, desc string, msg any) completions.SlashCompletionValue {
		return completions.SlashCompletionValue{Command: cmd, Desc: desc, Msg: msg}
	}

	// ── Agent ──────────────────────────────────────────────────────────────
	agentItems := []completions.SlashCompletionValue{
		v("/plan", "Switch to SK Plan mode", dialog.ActionSelectMode{ModeID: "plan"}),
		v("/code", "Switch to Code mode", dialog.ActionSelectMode{ModeID: "coder"}),
		v("/agents", "Switch agent mode", dialog.ActionOpenDialog{DialogID: dialog.ModesID}),
		v("/models", "Switch model", dialog.ActionOpenDialog{DialogID: dialog.ModelsID}),
	}

	// ── Session ────────────────────────────────────────────────────────────
	sessionItems := []completions.SlashCompletionValue{
		v("/new", "New session", dialog.ActionNewSession{}),
		v("/history", "Browse past sessions", dialog.ActionOpenDialog{DialogID: dialog.SessionsID}),
		v("/init", "Initialize project", dialog.ActionInitializeProject{}),
		v("/init_kng", "Initialize kng knowledge templates", dialog.ActionInitKnowledge{}),
	}
	if hasSession {
		sessionItems = append(
			sessionItems,
			v("/context", "Browse current context messages",
				dialog.ActionOpenDialog{DialogID: dialog.ContextID}),
			v("/rollback", "Rollback files to a session node",
				dialog.ActionOpenDialog{DialogID: dialog.RollbackID}),
			v("/summarize", "Summarize current session",
				dialog.ActionSummarize{SessionID: m.session.ID}),
			v("/export-md", "Export session as Markdown",
				dialog.ActionExportSession{SessionID: m.session.ID, Format: "markdown", Scope: "all"}),
			v("/export-html", "Export session as HTML",
				dialog.ActionExportSession{SessionID: m.session.ID, Format: "html", Scope: "all"}),
		)
	}
	if hasSession && m.width >= compactModeWidthBreakpoint {
		sessionItems = append(sessionItems,
			v("/sidebar", "Toggle sidebar", dialog.ActionToggleCompactMode{}))
	}
	if hasTodos || hasQueue {
		tasksLabel := "Toggle To-Dos"
		switch {
		case hasTodos && hasQueue:
			tasksLabel = "Toggle To-Dos / Queue"
		case hasQueue:
			tasksLabel = "Toggle Queue"
		}
		sessionItems = append(sessionItems,
			v("/tasks", tasksLabel, dialog.ActionTogglePills{}))
	}

	// ── Tools ─────────────────────────────────────────────────────────────
	toolItems := []completions.SlashCompletionValue{
		v("/mcps", "MCP servers", dialog.ActionOpenDialog{DialogID: dialog.MCPID}),
		v("/wechat", "Manage WeChat accounts", dialog.ActionOpenDialog{DialogID: dialog.WeChatManagerID}),
	}
	if os.Getenv("EDITOR") != "" {
		toolItems = append(toolItems,
			v("/editor", "Open external editor", dialog.ActionExternalEditor{}))
	}
	if hasSession && cfg != nil {
		if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
			if model := cfg.GetModelByType(agentCfg.Model); model != nil && model.SupportsImages {
				toolItems = append(toolItems,
					v("/file", "Open file picker", dialog.ActionOpenDialog{DialogID: dialog.FilePickerID}))
			}
		}
	}

	// ── Settings ──────────────────────────────────────────────────────────
	settingsItems := []completions.SlashCompletionValue{
		v("/approve", "Toggle auto-approve (Yolo)", dialog.ActionToggleYoloMode{}),
		v("/notifications", "Toggle notifications", dialog.ActionToggleNotifications{}),
		v("/theme", "Toggle transparent background", dialog.ActionToggleTransparentBackground{}),
	}
	if cfg != nil {
		if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
			if model := cfg.GetModelByType(agentCfg.Model); model != nil && model.CanReason {
				sel := cfg.Models[agentCfg.Model]
				if len(model.ReasoningLevels) == 0 {
					status := "Enable"
					if sel.Think {
						status = "Disable"
					}
					settingsItems = append(settingsItems,
						v("/think", status+" thinking mode", dialog.ActionToggleThinking{}))
				} else {
					settingsItems = append(settingsItems,
						v("/reasoning", "Select reasoning effort",
							dialog.ActionOpenDialog{DialogID: dialog.ReasoningID}))
				}
			}
		}
	}

	// ── Help ──────────────────────────────────────────────────────────────
	helpItems := []completions.SlashCompletionValue{
		v("/help", "Show help & key bindings", nil),
	}

	// ── Admin ─────────────────────────────────────────────────────────────
	adminItems := []completions.SlashCompletionValue{
		v("/admin", "Open Admin Panel", dialog.ActionOpenAdmin{}),
		v("/admin-start", "Start Admin Server", dialog.ActionStartAdmin{}),
		v("/admin-stop", "Stop Admin Server", dialog.ActionStopAdmin{}),
		v("/minimax", "MiniMax Quota", dialog.ActionShowQuota{}),
		v("/quit", "Quit", dialog.ActionQuit{}),
	}

	groups := []completions.SlashGroup{
		{Label: "Agent", Items: agentItems},
		{Label: "Session", Items: sessionItems},
		{Label: "Tools", Items: toolItems},
		{Label: "Settings", Items: settingsItems},
		{Label: "Help", Items: helpItems},
		{Label: "Admin", Items: adminItems},
	}

	// ── Custom commands ────────────────────────────────────────────────────
	if len(m.customCommands) > 0 {
		customItems := make([]completions.SlashCompletionValue, 0, len(m.customCommands))
		for _, cmd := range m.customCommands {
			customItems = append(customItems, completions.SlashCompletionValue{
				Command: slashLabelFromCommandID(cmd.ID),
				Desc:    slashDescFromContent(cmd.Content),
				Msg: dialog.ActionRunCustomCommand{
					Content:   cmd.Content,
					Arguments: cmd.Arguments,
				},
			})
		}
		groups = append(groups, completions.SlashGroup{Label: "Custom", Items: customItems})
	}

	return groups
}

// slashLabelFromCommandID derives a "/name" label from a custom command ID
// like "user:my-feature" or "project:spec".
func slashLabelFromCommandID(id string) string {
	parts := strings.Split(id, ":")
	name := parts[len(parts)-1]
	name = strings.ReplaceAll(name, "_", "-")
	if name == "" {
		name = id
	}
	return "/" + name
}

// slashDescFromContent returns the first non-empty line of content as a short
// description, capped at 60 characters.
func slashDescFromContent(content string) string {
	for _, line := range strings.SplitN(content, "\n", 5) {
		line = strings.TrimSpace(strings.TrimLeft(line, "#> "))
		if line != "" {
			if len(line) > 60 {
				return line[:60] + "…"
			}
			return line
		}
	}
	return ""
}

// insertCompletionText replaces the @query in the textarea with the given text.
// Returns false if the replacement cannot be performed.
func (m *UI) insertCompletionText(text string) bool {
	return m.insertCompletionTextWithGap(text, true)
}

func (m *UI) insertCompletionTextWithGap(text string, trailingGap bool) bool {
	value := m.textarea.Value()
	if m.completionsStartIndex > len(value) {
		return false
	}

	word := m.textareaWord()
	endIdx := min(m.completionsStartIndex+len(word), len(value))
	newValue := value[:m.completionsStartIndex] + text + value[endIdx:]
	m.textarea.SetValue(newValue)
	m.textarea.MoveToEnd()
	if trailingGap {
		m.textarea.InsertRune(' ')
	}
	return true
}

// insertFileCompletion inserts the selected file path into the textarea,
// replacing the @query, and adds the file as an attachment.
func (m *UI) insertFileCompletion(path string) tea.Cmd {
	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(path) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	fileCmd := func() tea.Msg {
		absPath, _ := filepath.Abs(path)

		if m.hasSession() {
			// Skip attachment if file was already read and hasn't been modified.
			lastRead := m.com.Workspace.FileTrackerLastReadTime(context.Background(), m.session.ID, absPath)
			if !lastRead.IsZero() {
				if info, err := os.Stat(path); err == nil && !info.ModTime().After(lastRead) {
					return nil
				}
			}
		} else if slices.Contains(m.sessionFileReads, absPath) {
			return nil
		}

		m.sessionFileReads = append(m.sessionFileReads, absPath)

		// Add file as attachment.
		content, err := os.ReadFile(path)
		if err != nil {
			// If it fails, let the LLM handle it later.
			return nil
		}

		return message.Attachment{
			FilePath: path,
			FileName: filepath.Base(path),
			MimeType: mimeOf(content),
			Content:  content,
		}
	}
	return tea.Batch(heightCmd, fileCmd)
}

// insertMCPResourceCompletion inserts the selected resource into the textarea,
// replacing the @query, and adds the resource as an attachment.
func (m *UI) insertMCPResourceCompletion(item completions.ResourceCompletionValue) tea.Cmd {
	displayText := cmp.Or(item.Title, item.URI)

	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(displayText) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	resourceCmd := func() tea.Msg {
		contents, err := m.com.Workspace.ReadMCPResource(
			context.Background(),
			item.MCPName,
			item.URI,
		)
		if err != nil {
			slog.Warn("Failed to read MCP resource", "uri", item.URI, "error", err)
			return nil
		}
		if len(contents) == 0 {
			return nil
		}

		content := contents[0]
		var data []byte
		if content.Text != "" {
			data = []byte(content.Text)
		} else if len(content.Blob) > 0 {
			data = content.Blob
		}
		if len(data) == 0 {
			return nil
		}

		mimeType := item.MIMEType
		if mimeType == "" && content.MIMEType != "" {
			mimeType = content.MIMEType
		}
		if mimeType == "" {
			mimeType = "text/plain"
		}

		return message.Attachment{
			FilePath: item.URI,
			FileName: displayText,
			MimeType: mimeType,
			Content:  data,
		}
	}
	return tea.Batch(heightCmd, resourceCmd)
}

// completionsPosition returns the X and Y position for the completions popup.
func (m *UI) completionsPosition() image.Point {
	cur := m.textarea.Cursor()
	if cur == nil {
		return image.Point{
			X: m.layout.editor.Min.X,
			Y: m.layout.editor.Min.Y,
		}
	}
	return image.Point{
		X: cur.X + m.layout.editor.Min.X,
		Y: m.layout.editor.Min.Y + cur.Y,
	}
}

// textareaWord returns the current word at the cursor position.
func (m *UI) textareaWord() string {
	return m.textarea.Word()
}

// isWhitespace returns true if the byte is a whitespace character.
func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// isAgentBusy returns true if the agent coordinator exists and is currently
// busy processing a request.
func (m *UI) isAgentBusy() bool {
	return m.com.Workspace.AgentIsReady() &&
		m.com.Workspace.AgentIsBusy()
}

// breathingCmd returns a command that sends a breathTickMsg after 800ms.
// This drives the editor prompt breathing animation while the agent is busy.
func (m *UI) breathingCmd() tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg {
		return breathTickMsg{}
	})
}

// hasSession returns true if there is an active session with a valid ID.
func (m *UI) hasSession() bool {
	return m.session != nil && m.session.ID != ""
}

// mimeOf detects the MIME type of the given content.
func mimeOf(content []byte) string {
	mimeBufferSize := min(512, len(content))
	return http.DetectContentType(content[:mimeBufferSize])
}

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
func (m *UI) randomizePlaceholders() {
	m.workingPlaceholder = workingPlaceholders[randomIntn(len(workingPlaceholders))]
	m.readyPlaceholder = readyPlaceholders[randomIntn(len(readyPlaceholders))]
}

// renderEditorView renders the editor view with attachments if any.
func (m *UI) renderEditorView(width int) string {
	var attachmentsView string
	if len(m.attachments.List()) > 0 {
		attachmentsView = m.attachments.Render(width)
	}
	separator := m.com.Styles.Header.Separator.Render(strings.Repeat("─", max(0, width)))
	return strings.Join([]string{
		separator,
		attachmentsView,
		m.textarea.View(),
		"", // margin at bottom of editor
	}, "\n")
}

// applyTheme replaces the active styles with the given theme, drops the
// shared markdown renderer cache, and refreshes every component that
// caches style data.
func (m *UI) applyTheme(s styles.Styles) {
	*m.com.Styles = s
	common.InvalidateMarkdownRendererCache()
	m.chat.InvalidateRenderCaches()
	m.refreshStyles()
}

// refreshStyles pushes the current *m.com.Styles into every subcomponent
// that copies or pre-renders style-dependent values at construction time.
func (m *UI) refreshStyles() {
	t := m.com.Styles
	m.header.refresh()
	m.textarea.SetStyles(t.Editor.Textarea)
	m.completions.SetStyles(t.Completions.Normal, t.Completions.Focused, t.Completions.Match)
	m.attachments.Renderer().SetStyles(
		t.Attachments.Normal,
		t.Attachments.Deleting,
		t.Attachments.Image,
		t.Attachments.Text,
	)
	m.todoSpinner.Style = t.Pills.TodoSpinner
	m.chat.InvalidateRenderCaches()
}

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
func (m *UI) openDialog(id string) tea.Cmd {
	var cmds []tea.Cmd
	switch id {
	case dialog.SessionsID:
		if cmd := m.openSessionsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ModelsID:
		if cmd := m.openModelsDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.CommandsID:
		if cmd := m.openSlashCompletions(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ReasoningID:
		if cmd := m.openReasoningDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.FilePickerID:
		if cmd := m.openFilesDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ModesID:
		if cmd := m.openModesDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.HelpID:
		if cmd := m.openHelpDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.WeChatSelectID:
		m.dialog.OpenDialog(dialog.NewWeChatSelect(m.com))
	case dialog.WeChatManagerID:
		if cmd := m.openWeChatManagerDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.WeChatQRID:
		if cmd := m.openWeChatQRDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.MCPID:
		if cmd := m.openMCPDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.MiniMaxQuotaID:
		if cmd := m.openMiniMaxQuotaDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.ContextID:
		if cmd := m.openContextDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.RollbackID:
		if cmd := m.openRollbackDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.TodoID:
		if cmd := m.openTodoDialog(-1); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case dialog.QuitID:
		if cmd := m.openQuitDialog(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	default:
		// Unknown dialog
		break
	}
	return tea.Batch(cmds...)
}

// openQuitDialog opens the quit confirmation dialog.
func (m *UI) openQuitDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.QuitID) {
		// Bring to front
		m.dialog.BringToFront(dialog.QuitID)
		return nil
	}

	quitDialog := dialog.NewQuit(m.com)
	m.dialog.OpenDialog(quitDialog)
	return nil
}

// openModelsDialog opens the models dialog.
func (m *UI) openModelsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ModelsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.ModelsID)
		return nil
	}

	isOnboarding := m.state == uiOnboarding
	modelsDialog, err := dialog.NewModels(m.com, isOnboarding)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(modelsDialog)

	return nil
}

func (m *UI) openMCPDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.MCPID) {
		m.dialog.BringToFront(dialog.MCPID)
		return nil
	}
	m.dialog.OpenDialog(dialog.NewMCP(m.com, m.mcpStates))
	return nil
}

// openReasoningDialog opens the reasoning effort dialog.
func (m *UI) openReasoningDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ReasoningID) {
		m.dialog.BringToFront(dialog.ReasoningID)
		return nil
	}

	reasoningDialog, err := dialog.NewReasoning(m.com)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(reasoningDialog)
	return nil
}

// openSessionsDialog opens the sessions dialog. If the dialog is already open,
// it brings it to the front. Otherwise, it will list all the sessions and open
// the dialog.
func (m *UI) openSessionsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.SessionsID) {
		// Bring to front
		m.dialog.BringToFront(dialog.SessionsID)
		return nil
	}

	selectedSessionID := ""
	if m.session != nil {
		selectedSessionID = m.session.ID
	}

	dialog, err := dialog.NewSessions(m.com, selectedSessionID)
	if err != nil {
		return util.ReportError(err)
	}

	m.dialog.OpenDialog(dialog)
	return nil
}

// openFilesDialog opens the file picker dialog.
func (m *UI) openFilesDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.FilePickerID) {
		// Bring to front
		m.dialog.BringToFront(dialog.FilePickerID)
		return nil
	}

	filePicker, cmd := dialog.NewFilePicker(m.com)
	filePicker.SetImageCapabilities(&m.caps)
	m.dialog.OpenDialog(filePicker)

	return cmd
}

// openWeChatQRDialog opens the WeChat QR login dialog.
// openWeChatManagerDialog opens the unified WeChat Manager dialog (lists
// accounts, supports reconnect/start/stop/delete and opening the QR flow).
// The returned [tea.Cmd] is the initial periodic-refresh tick; the caller
// should schedule it so the dialog starts polling the manager for status
// updates. The dialog stops rescheduling its tick when it is closed.
func (m *UI) openWeChatManagerDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.WeChatManagerID) {
		m.dialog.BringToFront(dialog.WeChatManagerID)
		return nil
	}
	d, tickCmd := dialog.NewWeChatManager(m.com)
	m.dialog.OpenDialog(d)
	return tickCmd
}

func (m *UI) openWeChatQRDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.WeChatQRID) {
		m.dialog.BringToFront(dialog.WeChatQRID)
		return nil
	}

	qrDialog, err := dialog.NewWeChatQR(m.com)
	if err != nil {
		return util.ReportError(err)
	}
	qrDialog.SetHTTPClient(m.com.Config().HTTPClient(m.com.Workspace.Resolver(), 45*time.Second))
	m.dialog.OpenDialog(qrDialog)

	// Start the login flow via AccountManager (auto-registers account,
	// persists credentials, enables management from the manager dialog).
	qrDialog.StartLogin()

	// Wire the agent handler on the active channel so incoming messages
	// are processed once the poll loop starts.
	mgr := wechat.GetManager()
	if ch := mgr.GetActive(); ch != nil {
		injectWeChatButler(ch, m.com.Workspace)
	}

	return qrDialog.PollLoginCmd()
}

// openModesDialog opens the agent mode selection dialog.
func (m *UI) openModesDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ModesID) {
		m.dialog.BringToFront(dialog.ModesID)
		return nil
	}

	modesDialog, err := dialog.NewModes(m.com)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(modesDialog)

	return nil
}

// openHelpDialog opens the help key bindings dialog.
func (m *UI) openHelpDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.HelpID) {
		m.dialog.BringToFront(dialog.HelpID)
		return nil
	}

	helpDialog := dialog.NewHelp(m.com)
	m.dialog.OpenDialog(helpDialog)
	return nil
}

// openMiniMaxQuotaDialog opens the MiniMax quota dialog.
func (m *UI) openMiniMaxQuotaDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.MiniMaxQuotaID) {
		m.dialog.BringToFront(dialog.MiniMaxQuotaID)
		return nil
	}

	quotaDialog, err := dialog.NewMiniMaxQuota(m.com)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(quotaDialog)

	return quotaDialog.FetchQuotaCmd()
}

// openContextDialog opens the context message browser dialog.
func (m *UI) openContextDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.ContextID) {
		m.dialog.BringToFront(dialog.ContextID)
		return nil
	}
	if !m.hasSession() {
		return util.ReportWarn("No active session context to show")
	}

	contextDialog, err := dialog.NewContextMessages(m.com, m.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(contextDialog)
	return nil
}

// openRollbackDialog opens the rollback dialog.
func (m *UI) openRollbackDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.RollbackID) {
		m.dialog.BringToFront(dialog.RollbackID)
		return nil
	}
	if !m.hasSession() {
		return util.ReportWarn("No active session to rollback")
	}

	rollbackDialog, err := dialog.NewRollback(m.com, m.session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(rollbackDialog)
	return nil
}

// performRollback executes the rollback to the target message node.
func (m *UI) performRollback(target message.Message) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		sess := *m.session
		workingDir := m.com.Workspace.WorkingDir()

		files, err := m.com.Workspace.ListSessionHistory(context.Background(), sess.ID)
		if err != nil {
			return util.NewErrorMsg(fmt.Errorf("list session file history: %w", err))
		}

		// Truncate messages after the target
		deleted, err := m.com.Workspace.TruncateMessagesAfter(ctx, sess.ID, target.ID)
		if err != nil {
			return util.NewErrorMsg(fmt.Errorf("truncate messages: %w", err))
		}

		nodeIdx := findNodeIndex(m, target)

		if len(files) == 0 {
			// Only message truncation, no file history
			if deleted > 0 {
				return util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  fmt.Sprintf("Rolled back to node #%d (%s): removed %d message(s).", nodeIdx, shortID(target.ID), deleted),
				}
			}
			return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "No messages to remove and no file history found for this session"}
		}

		repoDir, err := rollbackRepoDir(sess, workingDir)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if err := commitRollbackState(repoDir, workingDir, currentTrackedFileState(files, workingDir), "pre-rollback to "+shortNodeID(target)); err != nil {
			return util.NewErrorMsg(fmt.Errorf("snapshot before rollback: %w", err))
		}

		restored, removed, err := restoreFilesAt(files, workingDir, target.UpdatedAt)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if err := commitRollbackState(repoDir, workingDir, currentTrackedFileState(files, workingDir), "rollback target "+shortNodeID(target)); err != nil {
			return util.NewErrorMsg(fmt.Errorf("snapshot after rollback: %w", err))
		}

		return util.InfoMsg{
			Type: util.InfoTypeInfo,
			Msg:  fmt.Sprintf("Rolled back to node #%d (%s): removed %d message(s), restored %d file(s), removed %d file(s). Snapshot: %s", nodeIdx, shortID(target.ID), deleted, restored, removed, repoDir),
		}
	}
}

// findNodeIndex returns the 1-based index of the target message in the session.
func findNodeIndex(m *UI, target message.Message) int {
	ctx := context.Background()
	messages, err := m.com.Workspace.ListMessages(ctx, m.session.ID)
	if err != nil {
		return 0
	}
	idx := 1
	for _, msg := range messages {
		if msg.IsSummaryMessage {
			continue
		}
		if msg.ID == target.ID {
			return idx
		}
		idx++
	}
	return 0
}

// shortNodeID returns a short representation of a message node for logging.
func shortNodeID(target message.Message) string {
	return shortID(target.ID)
}

func (m *UI) openTodoDialog(selected int) tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	if m.dialog.ContainsDialog(dialog.TodoID) {
		m.dialog.BringToFront(dialog.TodoID)
		return nil
	}
	todoDialog, err := dialog.NewTodo(m.com, *m.session, selected)
	if err != nil {
		return util.ReportError(err)
	}
	m.dialog.OpenDialog(todoDialog)
	return nil
}

func (m *UI) saveSessionTodos(todos []session.Todo) tea.Cmd {
	if !m.hasSession() {
		return nil
	}
	sess := *m.session
	sess.Todos = append([]session.Todo(nil), todos...)
	m.session = &sess
	m.updateTodoContinuationState(sess.ID, sess.Todos)
	m.renderPills()
	m.updateLayoutAndSize()
	return func() tea.Msg {
		_, err := m.com.Workspace.SaveSession(context.Background(), sess)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Todos updated")
	}
}

// openPermissionsDialog opens the permissions dialog for a permission request.
func (m *UI) openPermissionsDialog(perm permission.PermissionRequest) tea.Cmd {
	// Close any existing permissions dialog first.
	m.dialog.CloseDialog(dialog.PermissionsID)

	// Get diff mode from config.
	var opts []dialog.PermissionsOption
	if diffMode := m.com.Config().Options.TUI.DiffMode; diffMode != "" {
		opts = append(opts, dialog.WithDiffMode(diffMode == "split"))
	}

	permDialog := dialog.NewPermissions(m.com, perm, opts...)
	m.dialog.OpenDialog(permDialog)
	return nil
}

func (m *UI) ensureTodoContinuationState(sessionID string) *todoAutoContinueState {
	if sessionID == "" {
		return nil
	}
	if m.todoContinuations == nil {
		m.todoContinuations = make(map[string]*todoAutoContinueState)
	}
	if state, ok := m.todoContinuations[sessionID]; ok {
		return state
	}
	state := &todoAutoContinueState{}
	m.todoContinuations[sessionID] = state
	return state
}

func todoFingerprint(todos []session.Todo) string {
	if len(todos) == 0 {
		return ""
	}
	var b strings.Builder
	for _, todo := range todos {
		b.WriteString(string(todo.Status))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(todo.Content))
		b.WriteString("|")
		b.WriteString(strings.TrimSpace(todo.ActiveForm))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *UI) updateTodoContinuationState(sessionID string, todos []session.Todo) {
	state := m.ensureTodoContinuationState(sessionID)
	if state == nil {
		return
	}
	fp := todoFingerprint(todos)
	if !hasIncompleteTodos(todos) {
		state.InFlight = false
		state.Stalled = false
		state.ConsecutiveNoChanges = 0
		state.LastFingerprint = fp
		state.PendingPermission = false
		state.ReauthBlocked = false
		return
	}
	if state.LastFingerprint == fp {
		if state.InFlight {
			state.ConsecutiveNoChanges++
		}
	} else {
		state.ConsecutiveNoChanges = 0
		state.Stalled = false
	}
	state.LastFingerprint = fp
	state.InFlight = false
}

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

func (m *UI) runMCPPrompt(clientID, promptID string, arguments map[string]string) tea.Cmd {
	load := func() tea.Msg {
		prompt, err := m.com.Workspace.GetMCPPrompt(clientID, promptID, arguments)
		if err != nil {
			// TODO: make this better
			return util.ReportError(err)()
		}

		if prompt == "" {
			return nil
		}
		return sendMessageMsg{
			Content: prompt,
		}
	}

	var cmds []tea.Cmd
	if cmd := m.dialog.StartLoading(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, load, func() tea.Msg {
		return closeDialogMsg{}
	})

	return tea.Sequence(cmds...)
}

func (m *UI) handleStateChanged() tea.Cmd {
	return func() tea.Msg {
		if err := m.com.Workspace.UpdateAgentModel(context.Background()); err != nil {
			return util.ReportError(err)()
		}
		return mcpStateChangedMsg{
			states: m.com.Workspace.MCPGetStates(),
		}
	}
}

func handleMCPPromptsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshPrompts(context.Background(), name)
		return nil
	}
}

func handleMCPToolsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.RefreshMCPTools(context.Background(), name)
		return nil
	}
}

func handleMCPResourcesEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshResources(context.Background(), name)
		return nil
	}
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

func (m *UI) enableDockerMCP() tea.Msg {
	ctx := context.Background()
	if err := m.com.Workspace.EnableDockerMCP(ctx); err != nil {
		return util.ReportError(err)()
	}

	return util.NewInfoMsg("Docker MCP enabled and started successfully")
}

func (m *UI) disableDockerMCP() tea.Msg {
	if err := m.com.Workspace.DisableDockerMCP(); err != nil {
		return util.ReportError(err)()
	}

	return util.NewInfoMsg("Docker MCP disabled successfully")
}

func (m *UI) toggleMCP(name string, enable bool) tea.Cmd {
	return func() tea.Msg {
		if name == "" {
			return nil
		}
		ctx := context.Background()
		if enable {
			if err := m.com.Workspace.EnableMCP(ctx, name); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("MCP " + name + " enabled")
		}
		if err := m.com.Workspace.DisableMCP(name); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("MCP " + name + " disabled")
	}
}

// injectWeChatButler initializes the butler routing and slash config on a channel.
// Called both on initial login and on account switch.
func injectWeChatButler(ch *wechat.Channel, ws workspace.Workspace) {
	ch.SetSlashConfig(wechat.SlashConfig{
		CurrentModel: func() string {
			cfg := ws.Config()
			if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
				return large.Provider + "/" + large.Model
			}
			return ""
		},
		SmallModel: func() string {
			cfg := ws.Config()
			if small, ok := cfg.Models[config.SelectedModelTypeSmall]; ok {
				return small.Provider + "/" + small.Model
			}
			return ""
		},
		ListModels: func() []string {
			cfg := ws.Config()
			var result []string
			for id, item := range cfg.Providers.Seq2() {
				for _, m := range item.Models {
					result = append(result, id+"/"+m.ID)
				}
			}
			return result
		},
		SwitchModel: func(provider, model string) error {
			return ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
				Provider: provider,
				Model:    model,
			})
		},
		TestModel: func(provider, model string) error {
			return nil
		},
	})
	ch.InitButler(&tuiButlerWorkspace{ws})
}

// tuiButlerWorkspace adapts workspace.Workspace to wechat.ButlerWorkspace for TUI mode.
type tuiButlerWorkspace struct {
	ws workspace.Workspace
}

func (w *tuiButlerWorkspace) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := w.ws.CreateSession(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (w *tuiButlerWorkspace) GetSession(ctx context.Context, id string) (string, error) {
	sess, err := w.ws.GetSession(ctx, id)
	if err != nil {
		return "", err
	}
	return sess.Title, nil
}

func (w *tuiButlerWorkspace) ListSessions(ctx context.Context) ([]wechat.SessionInfo, error) {
	sessions, err := w.ws.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = wechat.SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: time.Unix(s.CreatedAt, 0).Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

func (w *tuiButlerWorkspace) DeleteSession(ctx context.Context, id string) error {
	return w.ws.DeleteSession(ctx, id)
}

func (w *tuiButlerWorkspace) AgentRun(ctx context.Context, id, prompt string) error {
	return w.ws.AgentRun(ctx, id, prompt)
}

func (w *tuiButlerWorkspace) ListMessages(ctx context.Context, id string) ([]wechat.MsgInfo, error) {
	msgs, err := w.ws.ListMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.MsgInfo, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, wechat.MsgInfo{Role: string(m.Role), Content: m.Content().Text})
	}
	return result, nil
}

func (w *tuiButlerWorkspace) CurrentModel() string {
	cfg := w.ws.Config()
	if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
		return large.Provider + "/" + large.Model
	}
	return ""
}

func (w *tuiButlerWorkspace) UpdateModel(provider, model string) error {
	return w.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider: provider,
		Model:    model,
	})
}

// handleWeChatLogin starts the WeChat QR login flow via AccountManager.
func (m *UI) handleWeChatLogin() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		mgr := wechat.GetManager()
		qrCh := make(chan string, 1)

		go func() {
			info, err := mgr.Login(ctx, true, wechat.LoginCallbacks{
				OnQRURL: func(qrURL string) {
					select {
					case qrCh <- qrURL:
					default:
					}
				},
			}, m.com.Config().HTTPClient(m.com.Workspace.Resolver(), 45*time.Second))
			if err != nil {
				slog.Error("WeChat login failed", "error", err)
				return
			}
			if info != nil {
				slog.Info("WeChat account registered", "userID", info.UserID, "accountID", info.ID)
				if ch := mgr.GetActive(); ch != nil {
					injectWeChatButler(ch, m.com.Workspace)
				}
			}
		}()

		select {
		case qrURL := <-qrCh:
			return util.NewInfoMsg("WeChat QR: " + qrURL)
		case <-time.After(5 * time.Second):
			return util.NewInfoMsg("WeChat login: waiting for QR code...")
		}
	}
}

// handleWeChatLogout stops all WeChat channels.
func (m *UI) handleWeChatLogout() tea.Cmd {
	return func() tea.Msg {
		wechat.GetManager().StopAll()
		return util.NewInfoMsg("WeChat disconnected")
	}
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
