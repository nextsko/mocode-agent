package model

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

func (m *UI) handleDialogMsg(msg tea.Msg) tea.Cmd {
	action := m.dialog.Update(msg)
	if action == nil {
		return nil
	}
	return m.handleDialogAction(action)
}

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

func substituteArgs(content string, args map[string]string) string {
	for name, value := range args {
		placeholder := "$" + name
		content = strings.ReplaceAll(content, placeholder, value)
	}
	return content
}

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

func shortNodeID(target message.Message) string {
	return shortID(target.ID)
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
