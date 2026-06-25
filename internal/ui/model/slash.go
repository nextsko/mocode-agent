package model

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/package-register/mocode/internal/core/agent/evo"
	wechat "github.com/package-register/mocode/internal/integration/wechat"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/ui/util"
	"github.com/package-register/mocode/internal/util/infra"
)

const (
	slashCmdWeChat   = "/wechat"
	slashCmdContext  = "/context"
	slashCmdRollback = "/rollback"
	slashCmdEvo      = "/evo"
)

// handleSlashCommand dispatches a slash command typed in the input box.
// It uses slashCompletionGroups() as the single source of truth for command
// lookup, with special handling only for commands that have sub-commands.
func (m *UI) handleSlashCommand(value string) (tea.Cmd, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "/") {
		return nil, false
	}

	// ── Special handling for commands with sub-commands ──
	cmd, args := splitSlashCommand(trimmed)

	switch cmd {
	case slashCmdWeChat:
		// "/wechat" by itself opens the unified WeChat Manager modal where
		// the user can list accounts, switch active, reconnect, start/stop
		// the poll loop, delete accounts, or open the QR login flow.
		if args == "" {
			return m.openWeChatManagerDialog(), true
		}
		// Sub-commands are still supported for power users / scripting.
		return m.handleWeChatCommand(args), true

	case slashCmdContext:
		return m.openContextDialog(), true

	case slashCmdRollback:
		if args == "" {
			return m.openRollbackDialog(), true
		}
		return m.rollbackSession(args), true
	case slashCmdEvo:
		return m.handleEvoCommand(args)
	}

	// ── Unified lookup: search completion groups by label ──
	for _, group := range m.slashCompletionGroups() {
		for _, item := range group.Items {
			if item.Command != trimmed || item.Msg == nil {
				continue
			}
			return m.handleDialogAction(item.Msg), true
		}
	}

	// ── Partial match: "/plan start" → find "/plan" then check children ──
	if args != "" {
		for _, group := range m.slashCompletionGroups() {
			for _, item := range group.Items {
				if item.Command != cmd || item.Msg == nil {
					continue
				}
				return m.handleDialogAction(item.Msg), true
			}
		}
	}

	return nil, false
}

// splitSlashCommand splits "/cmd args" into ("/cmd", "args").
func splitSlashCommand(value string) (cmd, args string) {
	parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
	cmd = parts[0]
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return
}

// handleWeChatCommand handles legacy /wechat sub-commands for power users.
//
// handleEvoCommand drives the /evo self-evolution session. /evo (no args)
// enters the mode and swaps to the EvoCrimson theme. "/evo exit" requests
// leaving; because a fixation may be in progress, the first request only
// arms a second confirmation — the user must run /evo exit again to confirm.
func (m *UI) handleEvoCommand(args string) (tea.Cmd, bool) {
	args = strings.TrimSpace(args)
	switch args {
	case "", "start", "enter":
		if m.evo.IsActive() {
			return infoCmd("already in /evo mode"), true
		}
		m.evoPrevTheme = *m.com.Styles
		m.applyTheme(styles.EvoCrimson())
		m.evo.Enter(evoSessionName(args))
		return infoCmd("entered /evo — self-evolution active (red/purple)"), true
	case "exit", "quit", "leave":
		if !m.evo.IsActive() {
			return infoCmd("not in /evo mode"), true
		}
		if m.evo.Mode == evo.ModeConfirmExit {
			return m.confirmEvoExit()
		}
		m.evo.RequestExit()
		return infoCmd("run /evo exit again to confirm and fix the agent"), true
	default:
		// A bare name is treated as the agent to tame.
		if !m.evo.IsActive() {
			m.evoPrevTheme = *m.com.Styles
			m.applyTheme(styles.EvoCrimson())
			m.evo.Enter(evoSessionName(args))
			return infoCmd("entered /evo as " + args), true
		}
		return infoCmd("unknown /evo subcommand: " + args), true
	}
}

// evoSessionName resolves the agent name for a new evo session.
func evoSessionName(arg string) string {
	if arg != "" && arg != "start" && arg != "enter" {
		return arg
	}
	return "evo-agent"
}

// confirmEvoExit completes the second-confirmation exit: restores the prior
// theme and, if a pending optimal-theory prompt was reconstructed, fixes it
// as a revision via the fixation store.
func (m *UI) confirmEvoExit() (tea.Cmd, bool) {
	rev := m.evo.ConfirmExit()
	m.applyTheme(m.evoPrevTheme)
	if strings.TrimSpace(rev.SystemPrompt) == "" {
		return infoCmd("left /evo (no fixation)"), true
	}
	store := evo.NewFixationStore(evoRoot())
	if _, err := store.Fix(context.Background(), rev.AgentName, rev.AgentName, "fixed via /evo exit", rev); err != nil {
		return infoCmd("left /evo but fixation failed: " + err.Error()), true
	}
	return infoCmd("left /evo — agent fixed as " + rev.AgentName), true
}

// infoCmd returns a command that surfaces a transient info toast.
func infoCmd(msg string) tea.Cmd {
	return func() tea.Msg { return util.InfoMsg{Type: util.InfoTypeInfo, Msg: msg} }
}

// evoRoot returns the directory under which fixed evo agents are persisted.
func evoRoot() string {
	return filepath.Join(infra.DataDir(), "evo")
}
// The recommended path is to use the modal via "/wechat" (no args) or the
// command palette, but these sub-commands remain for scripting.
func (m *UI) handleWeChatCommand(args string) tea.Cmd {
	mgr := wechat.GetManager()
	switch {
	case args == "" || args == "ui" || args == "manager":
		// Already handled by the caller, but keep for safety.
		return m.openWeChatManagerDialog()

	case args == "list":
		accounts := mgr.List()
		if len(accounts) == 0 {
			return util.CmdHandler(util.NewInfoMsg("No WeChat accounts. Use /wechat to add one."))
		}
		var msg strings.Builder
		for _, a := range accounts {
			marker := "  "
			if a.IsActive {
				marker = "▶ "
			}
			fmt.Fprintf(&msg, "%s%s (%s) polling=%v\n", marker, a.Name, a.Status, mgr.IsRunning(a.ID))
		}
		return util.CmdHandler(util.NewInfoMsg("WeChat accounts:\n" + msg.String()))

	case strings.HasPrefix(args, "switch "):
		id := strings.TrimSpace(strings.TrimPrefix(args, "switch "))
		if err := mgr.Switch(id); err != nil {
			return util.CmdHandler(util.NewErrorMsg(err))
		}
		if ch := mgr.GetActive(); ch != nil {
			injectWeChatButler(ch, m.com.Workspace)
		}
		return util.CmdHandler(util.NewInfoMsg("Switched to WeChat account: " + id))

	case strings.HasPrefix(args, "reconnect "):
		id := strings.TrimSpace(strings.TrimPrefix(args, "reconnect "))
		if err := mgr.Reconnect(context.Background(), id); err != nil {
			return util.CmdHandler(util.NewErrorMsg(err))
		}
		return util.CmdHandler(util.NewInfoMsg("Reconnected: " + id))

	case strings.HasPrefix(args, "start "):
		id := strings.TrimSpace(strings.TrimPrefix(args, "start "))
		if err := mgr.Start(context.Background(), id); err != nil {
			return util.CmdHandler(util.NewErrorMsg(err))
		}
		return util.CmdHandler(util.NewInfoMsg("Started: " + id))

	case strings.HasPrefix(args, "stop "):
		id := strings.TrimSpace(strings.TrimPrefix(args, "stop "))
		mgr.Stop(id)
		return util.CmdHandler(util.NewInfoMsg("Stopped: " + id))

	case strings.HasPrefix(args, "delete ") || strings.HasPrefix(args, "logout "):
		verb := args
		id := ""
		if strings.HasPrefix(args, "delete ") {
			id = strings.TrimSpace(strings.TrimPrefix(args, "delete "))
		} else {
			id = strings.TrimSpace(strings.TrimPrefix(args, "logout "))
			verb = "delete"
		}
		if err := mgr.Delete(id); err != nil {
			return util.CmdHandler(util.NewErrorMsg(err))
		}
		if ch := mgr.GetActive(); ch != nil {
			injectWeChatButler(ch, m.com.Workspace)
		}
		return util.CmdHandler(util.NewInfoMsg(verb + "d: " + id))

	case args == "select":
		// Legacy alias — open the new manager dialog.
		return m.openWeChatManagerDialog()

	default:
		return util.CmdHandler(util.NewInfoMsg(
			"Usage: /wechat [list|switch <id>|reconnect <id>|start <id>|stop <id>|delete <id>]"))
	}
}
