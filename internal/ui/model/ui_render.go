package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/pkg/browser"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/ui/dialog"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

func (m *UI) drawHeader(scr uv.Screen, area uv.Rectangle) {
	m.header.drawHeader(
		scr,
		area,
		m.session,
		m.isCompact,
		m.detailsOpen,
		area.Dx(),
		nil,
		m.headerStats(),
	)
}

func (m *UI) hudTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return hudTickMsg{}
	})
}

func (m *UI) headerStats() headerStats {
	stats := headerStats{
		startedAt: m.startedAt,
		frame:     m.hudFrame,
	}
	if m.session != nil {
		stats.cacheReadTokens = m.session.CacheReadTokens
		stats.cacheCreationTokens = m.session.CacheCreationTokens
		stats.hasCache = m.session.CacheReadTokens > 0 || m.session.CacheCreationTokens > 0
	}
	return stats
}

func (m *UI) statusLine(width int) string {
	if width <= 0 {
		return ""
	}
	t := m.com.Styles
	if m.state == uiLanding {
		return ansi.Truncate(strings.Join([]string{
			t.Header.Keystroke.Render("ctrl+p") + t.Header.KeystrokeTip.Render(" slash"),
		}, t.Header.Separator.Render(" │ ")), max(0, width), "…")
	}
	var parts []string

	// Show agent status if active
	if m.agentStatus != "" {
		parts = append(parts, t.Header.Keystroke.Render("status ")+t.Header.KeystrokeTip.Render(m.agentStatus))
	}

	if model := m.selectedLargeModel(); model != nil {
		parts = append(parts, t.Header.Keystroke.Render("model ")+t.Header.KeystrokeTip.Render(model.CatwalkCfg.Name))
	}
	if mode := activeAgentMode(m.com); mode != "" {
		parts = append(parts, t.Header.Keystroke.Render("agent ")+t.Header.KeystrokeTip.Render(mode))
	}
	servers, enabled, tools, prompts, resources := m.mcpSummaryCounts()
	if servers > 0 {
		parts = append(parts, t.Header.Keystroke.Render("mcp ")+t.Header.KeystrokeTip.Render(fmt.Sprintf("%d/%d servers %d tools", enabled, servers, tools)))
		if prompts+resources > 0 && width > 100 {
			parts = append(parts, t.Header.KeystrokeTip.Render(fmt.Sprintf("%d prompts %d res", prompts, resources)))
		}
	}
	if diagnostics := m.lspDiagnosticCount(); diagnostics > 0 {
		parts = append(parts, t.LSP.ErrorDiagnostic.Render(fmt.Sprintf("%s%d lsp", styles.LSPErrorIcon, diagnostics)))
	}
	if m.promptQueue > 0 {
		parts = append(parts, t.Header.Keystroke.Render("queue ")+t.Header.KeystrokeTip.Render(strconv.Itoa(m.promptQueue)))
	}
	parts = append(
		parts,
		t.Header.Keystroke.Render("ctrl+p")+t.Header.KeystrokeTip.Render(" slash"),
	)

	line := strings.Join(parts, t.Header.Separator.Render(" │ "))
	return ansi.Truncate(line, max(0, width), "…")
}

func (m *UI) lspDiagnosticCount() int {
	total := 0
	for _, state := range m.lspStates {
		total += state.DiagnosticCount
	}
	return total
}

func (m *UI) startAdminServer(open bool) tea.Cmd {
	return func() tea.Msg {
		url, err := m.adminServer.Start(context.Background(), 0)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if open {
			if err := browser.OpenURL(url); err != nil {
				return util.NewErrorMsg(err)
			}
			return util.NewInfoMsg("Admin panel opened: " + url)
		}
		return util.NewInfoMsg("Admin server started: " + url)
	}
}

func (m *UI) stopAdminServer() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.adminServer.Stop(ctx); err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Admin server stopped")
	}
}

func (m *UI) setProxyURL(action dialog.ActionSetProxyURL) tea.Cmd {
	return func() tea.Msg {
		proxyURL := strings.TrimSpace(action.URL)
		noProxy := "localhost,127.0.0.1"
		if !action.Enabled {
			if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.enabled", false); err != nil {
				return util.NewErrorMsg(err)
			}
			return util.NewInfoMsg("Network proxy disabled")
		}
		if action.Args != nil {
			proxyURL = strings.TrimSpace(action.Args["PROXY_URL"])
			if value := strings.TrimSpace(action.Args["NO_PROXY"]); value != "" {
				noProxy = value
			}
		}
		if proxyURL == "" {
			return util.NewErrorMsg(errors.New("proxy URL is required"))
		}
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.enabled", action.Enabled); err != nil {
			return util.NewErrorMsg(err)
		}
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.proxy_url", proxyURL); err != nil {
			return util.NewErrorMsg(err)
		}
		if err := m.com.Workspace.SetConfigField(config.ScopeGlobal, "options.network.no_proxy", noProxy); err != nil {
			return util.NewErrorMsg(err)
		}
		status := "disabled"
		if action.Enabled {
			status = "enabled"
		}
		return util.NewInfoMsg("Network proxy " + status + ": " + proxyURL)
	}
}

func (m *UI) currentModelSupportsImages() bool {
	cfg := m.com.Config()
	if cfg == nil {
		return false
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return false
	}
	model := cfg.GetModelByType(agentCfg.Model)
	return model != nil && model.SupportsImages
}
