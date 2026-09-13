package model

import (
	tea "charm.land/bubbletea/v2"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
)

// randomizePlaceholders selects random placeholder text for the textarea's
// ready and working states.

// renderEditorView renders the editor view with attachments if any.

// applyTheme replaces the active styles with the given theme, drops the
// shared markdown renderer cache, and refreshes every component that
// caches style data.

// refreshStyles pushes the current *m.com.Styles into every subcomponent
// that copies or pre-renders style-dependent values at construction time.

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

// openQuestionDialog opens the AskUser question form (crush-compatible) for
// a question batch. The form blocks the tool call until answered.
func (m *UI) openQuestionDialog(batch question.Request) tea.Cmd {
	// Close any existing question form first to prevent stacking.
	m.dialog.CloseDialog(dialog.QuestionFormID)

	form := dialog.NewQuestionForm(m.com.Styles, batch)
	form.OnAnswer = func(responses []question.Answer) {
		m.com.Workspace.QuestionAnswer(responses)
		m.dialog.CloseDialog(dialog.QuestionFormID)
		m.textarea.Focus()
		m.updateLayoutAndSize()
	}
	form.OnCancel = func() {
		m.com.Workspace.QuestionCancel()
		m.dialog.CloseDialog(dialog.QuestionFormID)
		m.textarea.Focus()
		m.updateLayoutAndSize()
	}
	m.textarea.Blur()
	m.dialog.OpenDialog(dialog.NewQuestionDialog(form))
	form.SetFocused(true)
	m.updateLayoutAndSize()
	return nil
}

// handleQuestionNotification dismisses an open question form when any client
// resolved the pending batch (only one question can be pending at a time).
func (m *UI) handleQuestionNotification(_ question.Notification) {
	if m.dialog.ContainsDialog(dialog.QuestionFormID) {
		m.dialog.CloseDialog(dialog.QuestionFormID)
		m.textarea.Focus()
		m.updateLayoutAndSize()
	}
}
