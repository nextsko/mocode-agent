package model

import (
	"cmp"
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	fimage "github.com/nextsko/mocode-agent/internal/ui/image"
	"github.com/nextsko/mocode-agent/internal/core/config"
	wechat "github.com/nextsko/mocode-agent/internal/integration/wechat"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/slash"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

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
		// Slash /summary is a user-driven action, while the LLM-driven
		// session_summary tool path is allowed to run during a busy turn.
		// Keeping the guard here is intentional: it prevents UI spam from
		// repeated Enter presses and avoids stacking duplicate summary
		// goroutines (the active sessionID is registered with activeRequests
		// by sessionAgent.Summarize so IsBusy() returns true while a
		// summary is in flight, which naturally rate-limits subsequent
		// triggers).
		sessionID := msg.SessionID
		cmds = append(cmds, func() tea.Msg { return util.NewInfoMsg("Summarizing session…") })
		cmds = append(cmds, func() tea.Msg {
			if err := m.com.Workspace.AgentEnqueueSummary(context.Background(), sessionID); err != nil {
				return util.ReportError(err)()
			}
			return nil
		})
		// Completion InfoMsg / ErrorMsg is delivered via SummaryCompletedMsg
		// on app.events, not synchronously here.
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
	case dialog.ActionReloadConfig:
		cmds = append(cmds, func() tea.Msg {
			if err := m.com.Workspace.ReloadConfig(context.Background()); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Config reloaded from disk")
		})
	case dialog.ActionReloadMCP:
		cmds = append(cmds, func() tea.Msg {
			cfg := m.com.Config()
			if cfg == nil {
				return util.ReportError(errors.New("configuration not found"))()
			}
			var lastErr error
			for name := range cfg.MCP {
				if err := m.com.Workspace.ReloadMCP(context.Background(), name); err != nil {
					lastErr = err
				}
			}
			if lastErr != nil {
				return util.ReportError(lastErr)()
			}
			return util.NewInfoMsg("All MCP servers reloaded")
		})
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
