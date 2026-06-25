package model

import (
	"cmp"
	"fmt"
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"

	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
)

// modelInfo renders the current model information including reasoning
// settings and context usage/cost for the sidebar.
func (m *UI) modelInfo(width int) string {
	model := m.selectedLargeModel()
	reasoningInfo := ""
	providerName := ""

	if model != nil {
		// Get provider name first
		providerConfig, ok := m.com.Config().Providers.Get(model.ModelCfg.Provider)
		if ok {
			providerName = providerConfig.Name

			// Only check reasoning if model can reason
			if model.CatwalkCfg.CanReason {
				if len(model.CatwalkCfg.ReasoningLevels) == 0 {
					if model.ModelCfg.Think {
						reasoningInfo = "Thinking On"
					} else {
						reasoningInfo = "Thinking Off"
					}
				} else {
					reasoningEffort := cmp.Or(model.ModelCfg.ReasoningEffort, model.CatwalkCfg.DefaultReasoningEffort)
					reasoningInfo = fmt.Sprintf("Reasoning %s", common.FormatReasoningEffort(reasoningEffort))
				}
			}
		}
	}

	var modelContext *common.ModelContextInfo
	if model != nil && m.session != nil {
		modelContext = &common.ModelContextInfo{
			ContextUsed:  m.session.CompletionTokens + m.session.PromptTokens,
			Cost:         m.session.Cost,
			ModelContext: model.CatwalkCfg.ContextWindow,
		}
	}
	var modelName string
	if model != nil {
		modelName = model.CatwalkCfg.Name
	}
	return common.ModelInfo(m.com.Styles, modelName, providerName, reasoningInfo, modelContext, width, nil)
}

// getDynamicHeightLimits will give us the num of items to show in each section based on the height
// some items are more important than others.
func getDynamicHeightLimits(availableHeight, fileCount, lspCount, mcpCount, skillCount, agentCount int) (maxFiles, maxLSPs, maxMCPs, maxSkills, maxAgents int) {
	const (
		minItemsPerSection      = 2
		defaultMaxFilesShown    = 1000
		defaultMaxLSPsShown     = 1000
		defaultMaxMCPsShown     = 1000
		defaultMaxSkillsShown   = 1000
		defaultMaxAgentsShown   = 1000
		minAvailableHeightLimit = 10
	)

	if availableHeight < minAvailableHeightLimit {
		return minItemsPerSection, minItemsPerSection, minItemsPerSection, minItemsPerSection, minItemsPerSection
	}

	maxFiles = minItemsPerSection
	maxLSPs = minItemsPerSection
	maxMCPs = minItemsPerSection
	maxSkills = minItemsPerSection
	maxAgents = minItemsPerSection

	remainingHeight := max(0, availableHeight-(minItemsPerSection*5))

	sectionValues := []*int{&maxFiles, &maxLSPs, &maxMCPs, &maxSkills, &maxAgents}
	sectionCaps := []int{defaultMaxFilesShown, defaultMaxLSPsShown, defaultMaxMCPsShown, defaultMaxSkillsShown, defaultMaxAgentsShown}
	sectionNeeds := []int{max(0, fileCount-maxFiles), max(0, lspCount-maxLSPs), max(0, mcpCount-maxMCPs), max(0, skillCount-maxSkills), max(0, agentCount-maxAgents)}

	for remainingHeight > 0 {
		allocated := false
		for i, section := range sectionValues {
			if remainingHeight == 0 {
				break
			}
			if sectionNeeds[i] == 0 || *section >= sectionCaps[i] {
				continue
			}
			*section = *section + 1
			sectionNeeds[i]--
			remainingHeight--
			allocated = true
		}
		if !allocated {
			break
		}
	}

	for remainingHeight > 0 {
		allocated := false
		for i, section := range sectionValues {
			if remainingHeight == 0 {
				break
			}
			if *section >= sectionCaps[i] {
				continue
			}
			*section = *section + 1
			remainingHeight--
			allocated = true
		}
		if !allocated {
			break
		}
	}

	return maxFiles, maxLSPs, maxMCPs, maxSkills, maxAgents
}

// sidebar renders the chat sidebar containing session title, working
// directory, model info, file list, LSP status, and MCP status.
func (m *UI) drawSidebar(scr uv.Screen, area uv.Rectangle) {
	if m.session == nil {
		return
	}

	t := m.com.Styles
	width := area.Dx()
	height := area.Dy()

	title := t.Sidebar.SessionTitle.Width(width).MaxHeight(2).Render(m.session.Title)
	cwd := common.PrettyPath(t, m.com.Workspace.WorkingDir(), width)
	blocks := []string{
		title,
		"",
		cwd,
		m.activeAgentLine(width),
		"",
		m.modelInfo(width),
		"",
	}

	sidebarHeader := lipgloss.JoinVertical(
		lipgloss.Left,
		blocks...,
	)

	var remainingHeightArea image.Rectangle
	layout.Vertical(
		layout.Len(lipgloss.Height(sidebarHeader)),
		layout.Fill(1),
	).Split(m.layout.sidebar).Assign(new(image.Rectangle), &remainingHeightArea)
	remainingHeight := remainingHeightArea.Dy() - 6
	filesCount := 0
	for _, f := range m.sessionFiles {
		if f.Additions == 0 && f.Deletions == 0 {
			continue
		}
		filesCount++
	}

	lspsCount := len(m.lspStates)

	mcpsCount := 0
	for _, mcpCfg := range m.com.Config().MCP.Sorted() {
		if _, ok := m.mcpStates[mcpCfg.Name]; ok {
			mcpsCount++
		}
	}

	skillsCount := len(m.skillStatusItems())

	agentsCount := len(m.currentSessionAgentEntries())

	maxFiles, maxLSPs, maxMCPs, maxSkills, maxAgents := getDynamicHeightLimits(remainingHeight, filesCount, lspsCount, mcpsCount, skillsCount, agentsCount)

	lspSection := m.lspInfo(width, maxLSPs, true)
	mcpSection := m.mcpInfo(width, maxMCPs, true)
	skillsSection := m.skillsInfo(width, maxSkills, true)
	filesSection := m.filesInfo(m.com.Workspace.WorkingDir(), width, maxFiles, true)
	agentsSection := m.agentInfo(width, maxAgents)

	uv.NewStyledString(
		lipgloss.NewStyle().
			MaxWidth(width).
			MaxHeight(height).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Left,
					sidebarHeader,
					filesSection,
					"",
					agentsSection,
					"",
					lspSection,
					"",
					mcpSection,
					"",
					skillsSection,
				),
			),
	).Draw(scr, area)
}

// agentInfo renders the current session agent runtime section.
func (m *UI) agentInfo(width, maxItems int) string {
	t := m.com.Styles

	title := t.Resource.Heading.Render("Agents")
	title = common.Section(t, title, width)

	agents := m.currentSessionAgentEntries()
	if len(agents) == 0 {
		return lipgloss.NewStyle().Width(width).Render(title + "\n\n" + t.Resource.AdditionalText.Render("No activity yet"))
	}

	if maxItems <= 0 {
		return lipgloss.NewStyle().Width(width).Render(title + "\n\n" + t.Resource.AdditionalText.Render("No activity yet"))
	}

	var rendered []string
	displayCount := min(len(agents), maxItems)
	for i := 0; i < displayCount; i++ {
		entry := agents[i]
		badgeLabel := entry.DisplayName
		if strings.TrimSpace(badgeLabel) == "" {
			badgeLabel = "Agent"
		}
		badge := styles.AgentBadgeStyleFor(entry.ID).Render(badgeLabel)
		icon := t.Resource.OfflineIcon.String()
		statusText := "stopped"
		switch entry.Status {
		case agentRuntimeThinking:
			icon = t.Resource.BusyIcon.String()
			statusText = "thinking"
		case agentRuntimeExecuting:
			icon = t.Resource.BusyIcon.String()
			if entry.ToolName != "" {
				statusText = "executing " + entry.ToolName
			} else {
				statusText = "executing"
			}
		}
		summary := strings.TrimSpace(entry.Summary)
		line := lipgloss.JoinHorizontal(
			lipgloss.Left,
			icon,
			" ",
			badge,
			" ",
			t.Resource.Name.Render(statusText),
		)
		if summary != "" && summary != statusText {
			line += "\n" + t.Resource.AdditionalText.Render(summary)
		}
		rendered = append(rendered, line)
	}

	list := lipgloss.JoinVertical(lipgloss.Left, rendered...)

	if len(agents) > maxItems {
		remaining := len(agents) - maxItems
		list = list + "\n" + t.Resource.AdditionalText.Render(fmt.Sprintf("…and %d more", remaining))
	}

	return lipgloss.NewStyle().Width(width).Render(title + "\n\n" + list)
}

// handleSidebarAgentClick handles a mouse click in the sidebar area.
func (m *UI) handleSidebarAgentClick(y int) tea.Cmd {
	_ = y
	return nil
}
