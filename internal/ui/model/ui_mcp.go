package model

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/package-register/mocode/internal/transport/workspace"
	"github.com/package-register/mocode/internal/ui/util"
)

func (m *UI) runMCPPrompt(clientID, promptID string, arguments map[string]string) tea.Cmd {
	load := func() tea.Msg {
		prompt, err := m.com.Workspace.GetMCPPrompt(clientID, promptID, arguments)
		if err != nil {
			return util.ReportError(err)()
		}
		if prompt == "" {
			return nil
		}
		return sendMessageMsg{Content: prompt}
	}

	var cmds []tea.Cmd
	if cmd := m.dialog.StartLoading(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, load, func() tea.Msg { return closeDialogMsg{} })
	return tea.Sequence(cmds...)
}

func (m *UI) handleStateChanged() tea.Cmd {
	return func() tea.Msg {
		if err := m.com.Workspace.UpdateAgentModel(context.Background()); err != nil {
			return util.ReportError(err)()
		}
		return mcpStateChangedMsg{states: m.com.Workspace.MCPGetStates()}
	}
}

func handleMCPPromptsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshPrompts(context.Background(), name)
		return nil
	}
}

func handleMCPToolsEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.RefreshMCPTools(context.Background(), name)
		return nil
	}
}

func handleMCPResourcesEvent(ws workspace.Workspace, name string) tea.Cmd {
	return func() tea.Msg {
		ws.MCPRefreshResources(context.Background(), name)
		return nil
	}
}

func (m *UI) enableDockerMCP() tea.Msg {
	ctx := context.Background()
	if err := m.com.Workspace.EnableDockerMCP(ctx); err != nil {
		return util.ReportError(err)()
	}
	return util.NewInfoMsg("Docker MCP enabled and started successfully")
}

func (m *UI) disableDockerMCP() tea.Msg {
	if err := m.com.Workspace.DisableDockerMCP(); err != nil {
		return util.ReportError(err)()
	}
	return util.NewInfoMsg("Docker MCP disabled successfully")
}

func (m *UI) toggleMCP(name string, enable bool) tea.Cmd {
	return func() tea.Msg {
		if name == "" {
			return nil
		}
		ctx := context.Background()
		if enable {
			if err := m.com.Workspace.EnableMCP(ctx, name); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("MCP " + name + " enabled")
		}
		if err := m.com.Workspace.DisableMCP(name); err != nil {
			return util.ReportError(err)()
		}
		return util.NewInfoMsg("MCP " + name + " disabled")
	}
}
