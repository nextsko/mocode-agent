package model

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/transport/workspace"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
)

// selectedLargeModel returns the currently selected large language model from
// the agent coordinator, if one exists.
func (m *UI) selectedLargeModel() *workspace.AgentModel {
	if m.com.Workspace.AgentIsReady() {
		model := m.com.Workspace.AgentModel()
		return &model
	}
	return nil
}

// landingView renders the landing page view showing the current working
// directory, model information, and LSP/MCP status in a two-column layout.
func (m *UI) landingView() string {
	width := m.layout.main.Dx()
	height := max(1, m.layout.main.Dy()-1)
	contentWidth := max(1, min(104, width-4))
	if width <= 6 {
		contentWidth = max(1, width)
	}
	compact := contentWidth < 72

	meta := landingMetadata(m, contentWidth)
	body := landingGrid(m, contentWidth, compact)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		meta,
		"",
		body,
	)
	content = lipgloss.NewStyle().Width(contentWidth).Render(content)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxHeight(height).
		PaddingTop(1).
		AlignHorizontal(lipgloss.Center).
		Render(content)
}

func landingGrid(m *UI, width int, compact bool) string {
	t := m.com.Styles
	if compact {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			landingSection(t, "Get Started", landingGetStarted(t, width), width),
			"",
			landingSection(t, "Workspace", landingStatus(m, width), width),
			"",
			landingSection(t, "Sessions", landingRecent(t, width), width),
		)
	}
	if width >= 96 {
		gap := 2
		leftWidth := (width - gap*2) / 3
		middleWidth := leftWidth
		rightWidth := max(24, width-leftWidth-middleWidth-gap*2)
		left := landingSection(t, "Get Started", landingGetStarted(t, leftWidth), leftWidth)
		middle := landingSection(t, "Workspace", landingStatus(m, middleWidth), middleWidth)
		right := landingSection(t, "Sessions", landingRecent(t, rightWidth), rightWidth)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), middle, strings.Repeat(" ", gap), right)
	}
	leftWidth := max(30, (width-2)/2)
	rightWidth := max(24, width-leftWidth-2)
	left := lipgloss.JoinVertical(
		lipgloss.Left,
		landingSection(t, "Get Started", landingGetStarted(t, leftWidth), leftWidth),
		"",
		landingSection(t, "Sessions", landingRecent(t, leftWidth), leftWidth),
	)
	right := landingSection(t, "Workspace", landingStatus(m, rightWidth), rightWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func landingSection(t *styles.Styles, title, body string, width int) string {
	title = common.Section(t, t.Resource.Heading.Render(title), width)
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, title, body))
}

func landingGetStarted(t *styles.Styles, width int) string {
	rows := []string{
		landingCommand(t, "/", "open slash menu", width),
		landingCommand(t, "/agents", "switch agent profile", width),
		landingCommand(t, "/models", "switch model", width),
		landingCommand(t, "ctrl+s", "open session history", width),
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func landingCommand(t *styles.Styles, key, desc string, width int) string {
	keyWidth := 12
	key = ansi.Truncate(key, keyWidth, "…")
	gap := strings.Repeat(" ", max(1, keyWidth-lipgloss.Width(key)+1))
	desc = ansi.Truncate(desc, max(0, width-keyWidth-4), "…")
	return t.Dialog.TitleText.Render(key) + gap + t.Dialog.ListItem.InfoBlurred.Render(desc)
}

func landingMetadata(m *UI, width int) string {
	t := m.com.Styles
	modelName := "model pending"
	if model := m.selectedLargeModel(); model != nil {
		modelName = model.CatwalkCfg.Name
	}
	agentCount := len(m.com.Workspace.AvailableAgents())
	parts := []string{
		landingPill(t, "agent", m.activeAgentMode()),
		landingPill(t, "agents", fmt.Sprintf("%d", agentCount)),
		landingPill(t, "model", modelName),
		landingPill(t, "skills", fmt.Sprintf("%d", len(m.skillStatusItems()))),
	}
	sep := t.Header.Separator.Render(" • ")
	return ansi.Truncate(strings.Join(parts, sep), width, "…")
}

func landingPill(t *styles.Styles, label, value string) string {
	return t.Dialog.ListItem.InfoBlurred.Render(label+" ") + t.Dialog.TitleText.Render(value)
}

func activeAgentMode(com *common.Common) string {
	if com == nil {
		return config.AgentDefault
	}
	if com.Workspace != nil {
		if agentID := com.Workspace.CurrentAgentID(); agentID != "" {
			return agentID
		}
	}
	cfg := com.Config()
	if cfg != nil && cfg.Options != nil && cfg.Options.ActiveMode != "" {
		return cfg.Options.ActiveMode
	}
	return config.AgentDefault
}

func (m *UI) activeAgentMode() string {
	return activeAgentMode(m.com)
}

func (m *UI) activeAgentLine(width int) string {
	t := m.com.Styles
	return common.Status(t, common.StatusOpts{
		Icon:        t.Resource.OnlineIcon.String(),
		Title:       t.Resource.Name.Render(m.activeAgentMode()),
		Description: t.Resource.StatusText.Render("active agent"),
	}, width)
}

func landingStatus(m *UI, width int) string {
	sectionWidth := max(18, width)
	rows := []string{
		m.lspInfo(sectionWidth, 2, false),
		"",
		m.mcpInfo(sectionWidth, 2, false),
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func landingRecent(t *styles.Styles, width int) string {
	rows := []string{
		t.Dialog.ListItem.InfoBlurred.Render("No recent sessions loaded in this view."),
		"",
		landingCommand(t, "/history", "browse previous sessions", width),
		landingCommand(t, "/new", "start a fresh session", width),
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
