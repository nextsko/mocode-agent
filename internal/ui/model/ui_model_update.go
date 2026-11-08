package model

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	xstrings "github.com/charmbracelet/x/exp/strings"

	"github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/core/app"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/completions"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/notification"
	"github.com/nextsko/mocode-agent/internal/ui/pills"
	"github.com/nextsko/mocode-agent/internal/ui/util"
	"github.com/nextsko/mocode-agent/internal/util/anim"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

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
		cmds = append(cmds, m.startLSPs(msg.lspFilePaths()))
		msgs, err := m.com.Workspace.ListMessages(context.Background(), m.session.ID)
		if err != nil {
			cmds = append(cmds, util.ReportError(err))
			break
		}
		if cmd := m.setSessionMessages(msgs); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if pills.HasInProgressTodo(m.session.Todos) {
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
		paths := make([]string, 0, len(msg.sessionFiles))
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

	case pubsub.Event[session.Session],
		pubsub.Event[message.Message],
		pubsub.Event[history.File],
		pubsub.Event[app.LSPEvent],
		pubsub.Event[skills.Event],
		pubsub.Event[mcp.Event],
		pubsub.Event[permission.PermissionRequest],
		pubsub.Event[permission.PermissionNotification],
		pubsub.Event[question.Request],
		pubsub.Event[question.Notification]:
		if cs := m.handleServiceEventMsg(msg); len(cs) > 0 {
			cmds = append(cmds, cs...)
		}
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
	case tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg, DelayedClickMsg:
		if cs := m.handleMouseMsg(msg); len(cs) > 0 {
			cmds = append(cmds, cs...)
		}
	case anim.StepMsg, spinner.TickMsg, breathTickMsg:
		if cs := m.handleAnimMsg(msg); len(cs) > 0 {
			cmds = append(cmds, cs...)
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
	case agent.SummaryCompletedMsg:
		// Delivered by the coordinator's summaryDone broker (forwarded
		// via app.events) when an asynchronous /summary run finishes.
		if msg.Err != nil {
			cmds = append(cmds, util.ReportError(fmt.Errorf("failed to summarize: %w", msg.Err)))
		} else if msg.Path != "" {
			cmds = append(cmds, func() tea.Msg {
				return util.NewInfoMsg("Session summary saved: " + msg.Path)
			})
		} else {
			cmds = append(cmds, func() tea.Msg {
				return util.NewInfoMsg("Session summarized")
			})
		}
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
			// Busy: surface the send-mode triad so ctrl/alt+enter are
			// discoverable exactly when they matter.
			m.textarea.Placeholder = m.workingPlaceholder + "  (enter 排队 · ctrl+enter 引导 · alt+enter 强制)"
		} else {
			m.textarea.Placeholder = m.readyPlaceholder
		}
		if m.com.Workspace.PermissionSkipRequests() {
			m.textarea.Placeholder = "Yolo mode!"
		}
	default:
		// No focus-specific placeholder logic without a focused pane.
	}

	// at this point this can only handle [message.Attachment] message, and we
	// should return all cmds anyway.
	_ = m.attachments.Update(msg)
	return m, tea.Batch(cmds...)
}
