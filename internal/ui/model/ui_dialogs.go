package model

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/permission"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/domain/session/sessionexport"
	"github.com/package-register/mocode/internal/integration/wechat"
	"github.com/package-register/mocode/internal/ui/dialog"
	fimage "github.com/package-register/mocode/internal/ui/image"
	"github.com/package-register/mocode/internal/ui/slash"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/ui/util"
)

func (m *UI) handleDialogMsg(msg tea.Msg) tea.Cmd {
	action := m.dialog.Update(msg)
	if action == nil {
		return nil
	}
	return m.handleDialogAction(action)
}

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

	// /evo self-evolution mode toggle.
	case dialog.ActionEvo:
		var args string
		if !msg.Enter {
			args = "exit"
		}
		if cmd, handled := m.handleEvoCommand(args); handled && cmd != nil {
			cmds = append(cmds, cmd)
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
				[]slash.Argument{
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
