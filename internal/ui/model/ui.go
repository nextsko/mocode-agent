package model

import (
	"image"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/nextsko/mocode-agent/internal/core/app"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/transport/admin"
	"github.com/nextsko/mocode-agent/internal/ui/attachments"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/completions"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/notification"
	"github.com/nextsko/mocode-agent/internal/ui/slash"
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
	// lastAssistantReplyText is the text of the most recent assistant message,
	// maintained on message append so /copy-recent can copy it without IO.
	lastAssistantReplyText string

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
	agentStatus       string    // Current agent status text (e.g., "thinking", "executing tool")
	agentStatusTime   time.Time // When the status was last updated
	agentToolChildren map[string]string
	todoContinuations map[string]*todoAutoContinueState
	backgroundJobs    map[string]string

	// To-do spinner
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
		agentToolChildren:   make(map[string]string),
		todoContinuations:   make(map[string]*todoAutoContinueState),
		backgroundJobs:      make(map[string]string),
		notifyBackend:       notification.NoopBackend{},
		notifyWindowFocused: true,
		initialSessionID:    initialSessionID,
		continueLastSession: continueLast,
		startedAt:           time.Now(),
		adminServer:         admin.New(com.Workspace),
	}

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
