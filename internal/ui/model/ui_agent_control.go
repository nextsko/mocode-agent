package model

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/package-register/mocode/internal/core/agent/notify"
	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/permission"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/chat"
	"github.com/package-register/mocode/internal/ui/notification"
	"github.com/package-register/mocode/internal/ui/util"
	agenttools "github.com/package-register/mocode/tools"
)

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
