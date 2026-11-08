package model

import (
	"context"
	"log/slog"

	tea "charm.land/bubbletea/v2"

	"github.com/nextsko/mocode-agent/internal/ui/notification"
	"github.com/nextsko/mocode-agent/internal/ui/slash"
)

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
