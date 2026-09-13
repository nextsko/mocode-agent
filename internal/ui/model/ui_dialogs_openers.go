package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	wechat "github.com/nextsko/mocode-agent/internal/integration/wechat"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

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

func (m *UI) openHelpDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.HelpID) {
		m.dialog.BringToFront(dialog.HelpID)
		return nil
	}

	helpDialog := dialog.NewHelp(m.com)
	m.dialog.OpenDialog(helpDialog)
	return nil
}

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
