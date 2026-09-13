package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/config"

	agentcore "github.com/nextsko/mocode-agent/internal/core/agent"
)

func doctorProviderConnectivityCheck(loaded *doctorLoadedConfig, loadErr error) doctorCheck {
	if loadErr != nil {
		return doctorCheck{
			Name:    "Provider connectivity",
			Status:  doctorStatusSkip,
			Summary: "skipping provider connectivity because effective config failed to load",
		}
	}
	if loaded == nil || loaded.cfg == nil {
		return doctorCheck{
			Name:    "Provider connectivity",
			Status:  doctorStatusSkip,
			Summary: "no effective config available for provider connectivity checks",
		}
	}

	cfg := loaded.cfg
	enabled := cfg.EnabledProviders()
	if len(enabled) == 0 {
		return doctorCheck{
			Name:    "Provider connectivity",
			Status:  doctorStatusSkip,
			Summary: "no enabled providers configured",
		}
	}

	slices.SortFunc(enabled, func(a, b config.ProviderConfig) int {
		return strings.Compare(a.ID, b.ID)
	})

	var details []string
	categoryCounts := make(map[string]int)
	var okCount, warnCount, failCount int
	for _, provider := range enabled {
		// Route the probe through the config-derived client so the
		// options.network proxy applies; a bare default client reported
		// false connectivity failures behind a proxy.
		err := provider.TestConnection(
			loaded.resolver,
			loaded.cfg.HTTPClient(loaded.resolver, 10*time.Second),
		)
		issue := classifyDoctorProviderIssue(err)
		label := provider.ID
		if provider.Name != "" && provider.Name != provider.ID {
			label += " (" + provider.Name + ")"
		}
		categoryCounts[issue.Code]++
		switch issue.Status {
		case doctorStatusOK:
			okCount++
			details = append(details, label+": "+issue.Label)
		case doctorStatusWarn:
			warnCount++
			details = append(details, label+": "+issue.Label+": "+trimDoctorError(err))
		default:
			failCount++
			details = append(details, label+": "+issue.Label+": "+trimDoctorError(err))
		}
	}

	switch {
	case failCount > 0:
		return doctorCheck{
			Name:    "Provider connectivity",
			Status:  doctorStatusFail,
			Summary: doctorProviderConnectivitySummary(okCount, warnCount, failCount, categoryCounts),
			Details: details,
		}
	case warnCount > 0:
		return doctorCheck{
			Name:    "Provider connectivity",
			Status:  doctorStatusWarn,
			Summary: doctorProviderConnectivitySummary(okCount, warnCount, failCount, categoryCounts),
			Details: details,
		}
	default:
		return doctorCheck{
			Name:    "Provider connectivity",
			Status:  doctorStatusOK,
			Summary: doctorProviderConnectivitySummary(okCount, warnCount, failCount, categoryCounts),
			Details: details,
		}
	}
}

func doctorModelConfigCheck(loaded *doctorLoadedConfig, loadErr error) doctorCheck {
	if loadErr != nil {
		return doctorCheck{
			Name:    "Model configuration",
			Status:  doctorStatusSkip,
			Summary: "skipping model checks because effective config failed to load",
		}
	}
	if loaded == nil || loaded.cfg == nil {
		return doctorCheck{
			Name:    "Model configuration",
			Status:  doctorStatusSkip,
			Summary: "no effective config available for model checks",
		}
	}

	cfg := loaded.cfg
	if !cfg.IsConfigured() {
		return doctorCheck{
			Name:    "Model configuration",
			Status:  doctorStatusSkip,
			Summary: "no enabled providers configured, skipping model checks",
		}
	}

	var details []string
	var failures []string
	for _, modelType := range []config.SelectedModelType{config.SelectedModelTypeLarge, config.SelectedModelTypeSmall} {
		selected, ok := cfg.Models[modelType]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s model is not configured", modelType))
			continue
		}
		providerCfg := cfg.GetProviderForModel(modelType)
		modelCfg := cfg.GetModelByType(modelType)
		switch {
		case providerCfg == nil:
			failures = append(failures, fmt.Sprintf("%s model references missing provider %q", modelType, selected.Provider))
		case modelCfg == nil:
			failures = append(failures, fmt.Sprintf("%s model %q is not present in provider %q", modelType, selected.Model, selected.Provider))
		default:
			details = append(details, fmt.Sprintf("%s: %s/%s", modelType, selected.Provider, selected.Model))
		}
	}

	ensureDoctorAgentsConfigured(cfg)
	enabledAgents := 0
	for id, agent := range cfg.Agents {
		if agent.Disabled {
			continue
		}
		enabledAgents++
		if _, ok := cfg.Models[agent.Model]; !ok {
			failures = append(failures, fmt.Sprintf("agent %q references unconfigured model type %q", id, agent.Model))
		}
	}
	details = append(details, fmt.Sprintf("enabled agents: %d", enabledAgents))

	if len(failures) > 0 {
		details = append(details, failures...)
		return doctorCheck{
			Name:    "Model configuration",
			Status:  doctorStatusFail,
			Summary: "one or more configured models failed to resolve",
			Details: details,
		}
	}
	return doctorCheck{
		Name:    "Model configuration",
		Status:  doctorStatusOK,
		Summary: "selected large/small models resolve successfully",
		Details: details,
	}
}

func doctorSubagentTUICheck(loaded *doctorLoadedConfig, loadErr error) doctorCheck {
	if loadErr != nil {
		return doctorCheck{
			Name:    "TUI / subagent",
			Status:  doctorStatusSkip,
			Summary: "skipping TUI/subagent checks because effective config failed to load",
		}
	}
	if loaded == nil || loaded.cfg == nil {
		return doctorCheck{
			Name:    "TUI / subagent",
			Status:  doctorStatusSkip,
			Summary: "no effective config available for TUI/subagent checks",
		}
	}

	cfg := loaded.cfg
	ensureDoctorAgentsConfigured(cfg)

	enabledAgents := make(map[string]config.Agent)
	agentIDs := make([]string, 0, len(cfg.Agents))
	for id, agent := range cfg.Agents {
		if agent.Disabled {
			continue
		}
		enabledAgents[id] = agent
		agentIDs = append(agentIDs, id)
	}
	if len(enabledAgents) == 0 {
		return doctorCheck{
			Name:    "TUI / subagent",
			Status:  doctorStatusFail,
			Summary: "no enabled agent modes are available",
		}
	}
	slices.Sort(agentIDs)

	activeMode := config.AgentDefault
	if cfg.Options != nil && strings.TrimSpace(cfg.Options.ActiveMode) != "" {
		activeMode = strings.TrimSpace(cfg.Options.ActiveMode)
	}
	activeAgent, activeOK := enabledAgents[activeMode]
	var details []string
	var failures []string
	var warnings []string
	delegationAgents := 0
	transferAgents := 0
	for _, id := range agentIDs {
		agent := enabledAgents[id]
		if slices.Contains(agent.AllowedTools, agentcore.AgentToolName) {
			delegationAgents++
		}
		if len(agent.SubAgents) > 0 {
			transferAgents++
		}
		for _, sub := range agent.SubAgents {
			sub = strings.TrimSpace(sub)
			if sub == "" {
				continue
			}
			if sub == id {
				warnings = append(warnings, fmt.Sprintf("agent %q includes itself in sub_agents", id))
				continue
			}
			if _, ok := enabledAgents[sub]; !ok {
				failures = append(failures, fmt.Sprintf("agent %q references missing or disabled sub_agent %q", id, sub))
			}
		}
	}

	if !activeOK {
		failures = append(failures, fmt.Sprintf("active mode %q is missing or disabled", activeMode))
	} else {
		details = append(details, "active mode: "+activeMode)
		if len(activeAgent.SubAgents) == 0 && !slices.Contains(activeAgent.AllowedTools, agentcore.AgentToolName) {
			warnings = append(warnings, fmt.Sprintf("active mode %q does not currently expose subagent workflows", activeMode))
		}
	}

	details = append(
		details,
		fmt.Sprintf("enabled agents: %d", len(enabledAgents)),
		fmt.Sprintf("agent-tool modes: %d", delegationAgents),
		fmt.Sprintf("transfer-capable modes: %d", transferAgents),
	)
	if len(failures) > 0 {
		details = append(details, failures...)
		return doctorCheck{
			Name:    "TUI / subagent",
			Status:  doctorStatusFail,
			Summary: "detected invalid active-mode or subagent topology",
			Details: details,
		}
	}
	if delegationAgents == 0 && transferAgents == 0 {
		warnings = append(warnings, "no enabled mode currently supports agent/subagent delegation")
	}
	if len(warnings) > 0 {
		details = append(details, warnings...)
		return doctorCheck{
			Name:    "TUI / subagent",
			Status:  doctorStatusWarn,
			Summary: "subagent topology is valid, but some TUI/delegation caveats were detected",
			Details: details,
		}
	}
	return doctorCheck{
		Name:    "TUI / subagent",
		Status:  doctorStatusOK,
		Summary: "active mode and subagent topology look healthy",
		Details: details,
	}
}

func doctorSubagentSourceCheck(cwd string, facts doctorWorkspaceFacts) doctorCheck {
	uiPath := filepath.Join(cwd, "internal", "ui", "model", "ui.go")
	if !fileExists(uiPath) {
		return doctorCheck{
			Name:    "TUI / subagent source",
			Status:  doctorStatusSkip,
			Summary: "not running inside a mocode source checkout, skipping source-aware TUI checks",
		}
	}

	data, err := os.ReadFile(uiPath)
	if err != nil {
		return doctorCheck{
			Name:    "TUI / subagent source",
			Status:  doctorStatusFail,
			Summary: "failed to inspect UI source for subagent display support",
			Details: []string{uiPath, err.Error()},
		}
	}

	content := string(data)
	capabilities := []struct {
		Name    string
		Markers []string
	}{
		{
			Name: "child-topology mapping",
			Markers: []string{
				"registerAgentToolTopology(",
				"resolveAgentToolContainerID(",
				"containerID := m.resolveAgentToolContainerID(toolCallID)",
			},
		},
		{
			Name: "history hydration",
			Markers: []string{
				"func (m *UI) loadNestedToolCalls(",
				"registerAgentToolTopology(toolItem.MessageID(), tc)",
				"m.loadNestedToolCalls(nestedMessageItems)",
			},
		},
		{
			Name: "text-only child summary propagation",
			Markers: []string{
				"len(event.Payload.ToolCalls()) == 0 && len(event.Payload.ToolResults()) == 0",
				"summary := firstContentLine(event.Payload.Content().Text)",
				"summaryEntry.SetStatusSummary(summary)",
			},
		},
		{
			Name: "batch child-session IDs",
			Markers: []string{
				"func agentToolChildCallIDs(tc message.ToolCall) []string",
				"fmt.Sprintf(\"%s-%d\", tc.ID, i+1)",
			},
		},
		{
			Name: "DAG child-session IDs",
			Markers: []string{
				"DependsOn",
				"fmt.Sprintf(\"%s-%s\", tc.ID, taskID)",
			},
		},
	}

	details := []string{
		"ui source: " + filepath.ToSlash(filepath.Join("internal", "ui", "model", "ui.go")),
	}
	var missing []string
	for _, capability := range capabilities {
		if doctorSourceHasMarkers(content, capability.Markers...) {
			details = append(details, capability.Name+": supported")
			continue
		}
		missing = append(missing, capability.Name)
	}
	if len(missing) > 0 {
		details = append(details, "missing: "+strings.Join(missing, ", "))
		return doctorCheck{
			Name:    "TUI / subagent source",
			Status:  doctorStatusFail,
			Summary: "current UI source is missing one or more required subagent display paths",
			Details: details,
		}
	}

	_ = facts
	return doctorCheck{
		Name:    "TUI / subagent source",
		Status:  doctorStatusOK,
		Summary: "batch, DAG, history, and text-only child-session display paths are present in the UI source",
		Details: details,
	}
}
