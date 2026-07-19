package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	agentcore "github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/util/version"
)

type doctorStatus string

const (
	doctorStatusOK   doctorStatus = "ok"
	doctorStatusWarn doctorStatus = "warn"
	doctorStatusFail doctorStatus = "fail"
	doctorStatusSkip doctorStatus = "skip"
)

type doctorCheck struct {
	Name    string       `json:"name"`
	Status  doctorStatus `json:"status"`
	Summary string       `json:"summary"`
	Details []string     `json:"details,omitempty"`
}

type doctorReport struct {
	Version string        `json:"version"`
	CWD     string        `json:"cwd"`
	Checks  []doctorCheck `json:"checks"`
}

type doctorOptions struct {
	JSON  bool
	Build bool
}

type doctorPathState struct {
	Path            string
	Exists          bool
	Readable        bool
	Writable        bool
	NearestExisting string
	Err             error
}

type doctorWorkspaceFacts struct {
	HasGoMod      bool
	ModulePath    string
	HasTaskfile   bool
	HasAgents     bool
	HasProjectCfg bool
	GitWorktree   bool
}

type doctorLoadedConfig struct {
	cfg         *config.Config
	resolver    config.VariableResolver
	loadedPaths []string
}

type doctorProviderIssue struct {
	Status doctorStatus
	Code   string
	Label  string
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run local environment diagnostics",
	Long: `Run local diagnostics for mocode.

This command checks the local runtime, config/data directories, config files,
common provider environment variables, source-checkout toolchain, and optional
build health.`,
	Example: `
# Run standard local diagnostics
mocode doctor

# Include a source build check when running inside the mocode repository
mocode doctor --build

# Emit machine-readable JSON
mocode doctor --json
`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		opts := doctorOptions{}
		opts.JSON, _ = cmd.Flags().GetBool("json")
		opts.Build, _ = cmd.Flags().GetBool("build")
		return runDoctor(cmd, opts)
	},
}

func init() {
	doctorCmd.Flags().Bool("json", false, "Emit JSON diagnostics")
	doctorCmd.Flags().Bool("build", false, "Run an additional source build check when applicable")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, opts doctorOptions) error {
	report, err := collectDoctorReport(cmd.Context(), cmd, opts)
	if err != nil {
		return err
	}

	if opts.JSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	okCount, warnCount, failCount, skipCount := doctorStatusCounts(report.Checks)
	fmt.Fprintln(cmd.OutOrStdout(), "mocode doctor")
	fmt.Fprintln(cmd.OutOrStdout(), "=============")
	fmt.Fprintf(cmd.OutOrStdout(), "Version: %s\n", report.Version)
	fmt.Fprintf(cmd.OutOrStdout(), "CWD: %s\n\n", report.CWD)

	for _, check := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s: %s\n", strings.ToUpper(string(check.Status)), check.Name, check.Summary)
		for _, detail := range check.Details {
			fmt.Fprintf(cmd.OutOrStdout(), "      - %s\n", detail)
		}
	}

	fmt.Fprintf(
		cmd.OutOrStdout(),
		"\nSummary: %d passed, %d warnings, %d failed, %d skipped\n",
		okCount, warnCount, failCount, skipCount,
	)
	if failCount > 0 {
		return fmt.Errorf("doctor found %d failing check(s)", failCount)
	}
	return nil
}

func collectDoctorReport(ctx context.Context, cmd *cobra.Command, opts doctorOptions) (doctorReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return doctorReport{}, err
	}

	report := doctorReport{
		Version: version.Version,
		CWD:     cwd,
	}

	facts := collectDoctorWorkspaceFacts(ctx, cwd)
	loadedCfg, storeErr := loadDoctorConfigStore(cwd, cmd)
	report.Checks = append(
		report.Checks,
		doctorBinaryCheck(),
		doctorRuntimeCheck(cwd, facts),
		doctorConfigDirCheck(filepath.Dir(config.GlobalConfig())),
		doctorDataDirCheck(filepath.Dir(config.GlobalConfigData())),
		doctorConfigFilesCheck(cwd),
		doctorMergedConfigCheck(loadedCfg, storeErr),
		doctorEnvVarsCheck(),
		doctorProviderConnectivityCheck(loadedCfg, storeErr),
		doctorModelConfigCheck(loadedCfg, storeErr),
		doctorSubagentTUICheck(loadedCfg, storeErr),
		doctorSubagentSourceCheck(cwd, facts),
		doctorToolchainCheck(cwd, facts),
	)

	if opts.Build {
		report.Checks = append(report.Checks, doctorBuildCheck(ctx, cwd, facts))
	}

	return report, nil
}

func loadDoctorConfigStore(cwd string, cmd *cobra.Command) (*doctorLoadedConfig, error) {
	debug, _ := cmd.Flags().GetBool("debug")
	store, err := config.LoadReadOnly(cwd, debug)
	if err != nil {
		return nil, err
	}
	return &doctorLoadedConfig{
		cfg:         store.Config(),
		resolver:    store.Resolver(),
		loadedPaths: store.LoadedPaths(),
	}, nil
}

func doctorBinaryCheck() doctorCheck {
	exe, err := os.Executable()
	if err != nil {
		return doctorCheck{
			Name:    "Binary",
			Status:  doctorStatusWarn,
			Summary: "unable to resolve executable path",
			Details: []string{err.Error()},
		}
	}
	return doctorCheck{
		Name:    "Binary",
		Status:  doctorStatusOK,
		Summary: fmt.Sprintf("mocode %s", version.Version),
		Details: []string{exe},
	}
}

func doctorRuntimeCheck(cwd string, facts doctorWorkspaceFacts) doctorCheck {
	details := []string{
		"OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH,
		"Go runtime: " + runtime.Version(),
	}
	found := make([]string, 0, 4)
	if facts.HasGoMod {
		found = append(found, "go.mod")
	}
	if facts.HasTaskfile {
		found = append(found, "Taskfile.yaml")
	}
	if facts.HasAgents {
		found = append(found, "AGENTS.md")
	}
	if facts.HasProjectCfg {
		found = append(found, "mocode.json")
	}
	if len(found) > 0 {
		details = append(details, "Workspace files: "+strings.Join(found, ", "))
	}
	if facts.ModulePath != "" {
		details = append(details, "Module: "+facts.ModulePath)
	}
	if facts.HasGoMod {
		if facts.GitWorktree {
			details = append(details, "Git worktree: yes")
		} else {
			details = append(details, "Git worktree: no")
		}
	}

	status := doctorStatusOK
	summary := "runtime and workspace detected"
	if facts.HasGoMod && !facts.GitWorktree {
		status = doctorStatusWarn
		summary = "workspace detected, but current directory is not inside a git worktree"
	}

	return doctorCheck{
		Name:    "Runtime",
		Status:  status,
		Summary: summary,
		Details: details,
	}
}

func doctorConfigDirCheck(path string) doctorCheck {
	state := inspectDoctorPath(path)
	return doctorPathCheck("Config directory", state)
}

func doctorDataDirCheck(path string) doctorCheck {
	state := inspectDoctorPath(path)
	return doctorPathCheck("Data directory", state)
}

func doctorPathCheck(name string, state doctorPathState) doctorCheck {
	if state.Err != nil {
		return doctorCheck{
			Name:    name,
			Status:  doctorStatusFail,
			Summary: "failed to inspect path",
			Details: []string{state.Path, state.Err.Error()},
		}
	}

	details := []string{state.Path}
	if state.NearestExisting != "" && state.NearestExisting != state.Path {
		details = append(details, "nearest existing parent: "+state.NearestExisting)
	}

	switch {
	case state.Exists && state.Readable && state.Writable:
		return doctorCheck{Name: name, Status: doctorStatusOK, Summary: "exists and appears readable/writable", Details: details}
	case state.Exists && state.Readable:
		return doctorCheck{Name: name, Status: doctorStatusWarn, Summary: "exists but does not appear writable", Details: details}
	case state.Exists:
		return doctorCheck{Name: name, Status: doctorStatusWarn, Summary: "exists but permission bits look restricted", Details: details}
	case state.Writable:
		return doctorCheck{Name: name, Status: doctorStatusWarn, Summary: "missing, but parent path appears writable", Details: details}
	default:
		return doctorCheck{Name: name, Status: doctorStatusFail, Summary: "missing and parent path does not appear writable", Details: details}
	}
}

func doctorConfigFilesCheck(cwd string) doctorCheck {
	paths := []string{
		config.GlobalConfig(),
		config.GlobalConfigData(),
		filepath.Join(cwd, "mocode.json"),
	}
	seen := make(map[string]struct{}, len(paths))
	paths = slices.DeleteFunc(paths, func(path string) bool {
		if _, ok := seen[path]; ok {
			return true
		}
		seen[path] = struct{}{}
		return false
	})

	var details []string
	var foundAny bool
	var invalid []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				details = append(details, fmt.Sprintf("%s: read error: %v", path, err))
				invalid = append(invalid, path)
			}
			continue
		}
		foundAny = true
		if !json.Valid(data) {
			details = append(details, fmt.Sprintf("%s: invalid JSON", path))
			invalid = append(invalid, path)
			continue
		}
		providers := countJSONMapField(data, "providers")
		details = append(details, fmt.Sprintf("%s: valid JSON (providers=%d)", path, providers))
	}

	switch {
	case len(invalid) > 0:
		return doctorCheck{
			Name:    "Config files",
			Status:  doctorStatusFail,
			Summary: "one or more config files are unreadable or invalid",
			Details: details,
		}
	case foundAny:
		return doctorCheck{
			Name:    "Config files",
			Status:  doctorStatusOK,
			Summary: "detected valid config file candidates",
			Details: details,
		}
	default:
		return doctorCheck{
			Name:    "Config files",
			Status:  doctorStatusSkip,
			Summary: "no config files found yet",
		}
	}
}

func doctorMergedConfigCheck(loaded *doctorLoadedConfig, loadErr error) doctorCheck {
	if loadErr != nil {
		return doctorCheck{
			Name:    "Merged config",
			Status:  doctorStatusFail,
			Summary: "failed to load effective configuration",
			Details: []string{loadErr.Error()},
		}
	}
	if loaded == nil || loaded.cfg == nil {
		return doctorCheck{
			Name:    "Merged config",
			Status:  doctorStatusFail,
			Summary: "effective configuration is unavailable",
		}
	}

	cfg := loaded.cfg
	details := make([]string, 0, len(loaded.loadedPaths)+3)
	for _, path := range loaded.loadedPaths {
		details = append(details, "loaded: "+path)
	}
	details = append(details, fmt.Sprintf("enabled providers: %d", len(cfg.EnabledProviders())))
	if cfg.Options != nil {
		activeMode := cfg.Options.ActiveMode
		if activeMode == "" {
			activeMode = config.AgentDefault
		}
		details = append(details, "active mode: "+activeMode)
	}
	return doctorCheck{
		Name:    "Merged config",
		Status:  doctorStatusOK,
		Summary: "effective configuration loaded successfully",
		Details: details,
	}
}

func doctorEnvVarsCheck() doctorCheck {
	keys := []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"MINIMAX_API_KEY",
		"WECHAT_APP_ID",
		"WECHAT_APP_SECRET",
	}

	var setKeys []string
	var details []string
	for _, key := range keys {
		if _, ok := os.LookupEnv(key); ok {
			setKeys = append(setKeys, key)
			details = append(details, key+"=SET")
		} else {
			details = append(details, key+"=UNSET")
		}
	}

	if len(setKeys) == 0 {
		return doctorCheck{
			Name:    "Provider environment",
			Status:  doctorStatusSkip,
			Summary: "no common provider environment variables are set",
			Details: details,
		}
	}

	return doctorCheck{
		Name:    "Provider environment",
		Status:  doctorStatusOK,
		Summary: fmt.Sprintf("%d common provider variable(s) detected", len(setKeys)),
		Details: details,
	}
}

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
		err := provider.TestConnection(loaded.resolver)
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
				"registerAgentToolTopology(toolItem.MessageID(), sessionIDOrEmpty(m.session), tc)",
				"m.loadNestedToolCalls(nestedMessageItems)",
			},
		},
		{
			Name: "text-only child runtime tracking",
			Markers: []string{
				"len(msg.ToolCalls()) == 0 && len(msg.ToolResults()) == 0 && msg.Role == message.Assistant",
				"summary := firstContentLine(msg.Content().Text)",
				"childSessionDescriptor(parentTool, childSessionID)",
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

	chatAgentPath := filepath.Join(cwd, "internal", "ui", "chat", "agent.go")
	if chatAgentData, err := os.ReadFile(chatAgentPath); err == nil &&
		doctorSourceHasMarkers(string(chatAgentData), "if i < len(r.agent.nestedTools)", "content = r.agent.nestedTools[i].Render(80)") {
		details = append(details, "caveat: panel task rendering still maps nestedTools by index in internal/ui/chat/agent.go")
		return doctorCheck{
			Name:    "TUI / subagent source",
			Status:  doctorStatusWarn,
			Summary: "subagent display paths are present, but panel binding still has an index-based caveat",
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

func doctorToolchainCheck(cwd string, facts doctorWorkspaceFacts) doctorCheck {
	if !facts.HasGoMod {
		return doctorCheck{
			Name:    "Toolchain",
			Status:  doctorStatusSkip,
			Summary: "no source checkout detected, skipping developer tool checks",
		}
	}

	required := []string{"go", "git"}
	optional := []string{"task", "gofumpt", "golangci-lint"}

	var missingRequired []string
	var missingOptional []string
	var details []string

	for _, name := range required {
		if path, err := exec.LookPath(name); err == nil {
			details = append(details, fmt.Sprintf("%s: %s", name, path))
		} else {
			missingRequired = append(missingRequired, name)
			details = append(details, fmt.Sprintf("%s: missing", name))
		}
	}
	for _, name := range optional {
		if path, err := exec.LookPath(name); err == nil {
			details = append(details, fmt.Sprintf("%s: %s", name, path))
		} else {
			missingOptional = append(missingOptional, name)
			details = append(details, fmt.Sprintf("%s: missing", name))
		}
	}

	switch {
	case len(missingRequired) > 0:
		return doctorCheck{
			Name:    "Toolchain",
			Status:  doctorStatusFail,
			Summary: "required developer tools are missing",
			Details: details,
		}
	case len(missingOptional) > 0:
		return doctorCheck{
			Name:    "Toolchain",
			Status:  doctorStatusWarn,
			Summary: "required tools found, but some optional developer tools are missing",
			Details: details,
		}
	default:
		_ = cwd
		return doctorCheck{
			Name:    "Toolchain",
			Status:  doctorStatusOK,
			Summary: "required and optional developer tools are available",
			Details: details,
		}
	}
}

func doctorBuildCheck(ctx context.Context, cwd string, facts doctorWorkspaceFacts) doctorCheck {
	if !facts.HasGoMod {
		return doctorCheck{
			Name:    "Build",
			Status:  doctorStatusSkip,
			Summary: "no source checkout detected, skipping build check",
		}
	}
	if _, err := exec.LookPath("go"); err != nil {
		return doctorCheck{
			Name:    "Build",
			Status:  doctorStatusFail,
			Summary: "go is required for the build check",
			Details: []string{err.Error()},
		}
	}

	cacheDir := filepath.Join(os.TempDir(), "mocode-doctor-gocache")
	tmpDir := filepath.Join(os.TempDir(), "mocode-doctor-gotmp")
	outPath := filepath.Join(os.TempDir(), fmt.Sprintf("mocode-doctor-%d", time.Now().UnixNano()))
	_ = os.MkdirAll(cacheDir, 0o700)
	_ = os.MkdirAll(tmpDir, 0o700)
	defer os.Remove(outPath)

	buildCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	buildCmd := exec.CommandContext(buildCtx, "go", "build", "-buildvcs=false", "-o", outPath, ".")
	buildCmd.Dir = cwd
	buildCmd.Env = append(
		os.Environ(),
		"GOCACHE="+cacheDir,
		"GOTMPDIR="+tmpDir,
		"CGO_ENABLED=0",
		"GOEXPERIMENT=greenteagc",
	)
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		details := []string{
			"command: go build -buildvcs=false -o <temp> .",
			"env: CGO_ENABLED=0 GOEXPERIMENT=greenteagc",
		}
		if text := strings.TrimSpace(string(out)); text != "" {
			details = append(details, text)
		}
		return doctorCheck{
			Name:    "Build",
			Status:  doctorStatusFail,
			Summary: "source build failed",
			Details: details,
		}
	}

	return doctorCheck{
		Name:    "Build",
		Status:  doctorStatusOK,
		Summary: "source build succeeded",
		Details: []string{
			"command: go build -buildvcs=false -o <temp> .",
			"env: CGO_ENABLED=0 GOEXPERIMENT=greenteagc",
		},
	}
}

func collectDoctorWorkspaceFacts(ctx context.Context, cwd string) doctorWorkspaceFacts {
	facts := doctorWorkspaceFacts{
		HasGoMod:      fileExists(filepath.Join(cwd, "go.mod")),
		HasTaskfile:   fileExists(filepath.Join(cwd, "Taskfile.yaml")),
		HasAgents:     fileExists(filepath.Join(cwd, "AGENTS.md")),
		HasProjectCfg: fileExists(filepath.Join(cwd, "mocode.json")),
	}

	if facts.HasGoMod {
		if data, err := os.ReadFile(filepath.Join(cwd, "go.mod")); err == nil {
			facts.ModulePath = parseDoctorModulePath(data)
		}
	}

	gitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	gitCmd := exec.CommandContext(gitCtx, "git", "rev-parse", "--is-inside-work-tree")
	gitCmd.Dir = cwd
	out, err := gitCmd.Output()
	facts.GitWorktree = err == nil && strings.TrimSpace(string(out)) == "true"

	return facts
}

func inspectDoctorPath(path string) doctorPathState {
	state := doctorPathState{Path: path}
	info, err := os.Stat(path)
	if err == nil {
		state.Exists = true
		state.NearestExisting = path
		state.Readable = modeReadable(info.Mode())
		state.Writable = modeWritable(info.Mode())
		return state
	}
	if !os.IsNotExist(err) {
		state.Err = err
		return state
	}

	parent := nearestExistingDir(filepath.Dir(path))
	state.NearestExisting = parent
	if parent == "" {
		return state
	}
	parentInfo, parentErr := os.Stat(parent)
	if parentErr != nil {
		state.Err = parentErr
		return state
	}
	state.Readable = modeReadable(parentInfo.Mode())
	state.Writable = modeWritable(parentInfo.Mode())
	return state
}

func nearestExistingDir(path string) string {
	path = filepath.Clean(path)
	for path != "" && path != string(filepath.Separator) && path != "." {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	if _, err := os.Stat(string(filepath.Separator)); err == nil {
		return string(filepath.Separator)
	}
	return ""
}

func parseDoctorModulePath(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "module "))
	}
	return ""
}

func countJSONMapField(data []byte, key string) int {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return 0
	}
	raw, ok := root[key]
	if !ok {
		return 0
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return 0
	}
	return len(values)
}

func modeReadable(mode os.FileMode) bool {
	return mode.Perm()&0o444 != 0
}

func modeWritable(mode os.FileMode) bool {
	return mode.Perm()&0o222 != 0
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func ensureDoctorAgentsConfigured(cfg *config.Config) {
	if cfg == nil {
		return
	}
	if len(cfg.Agents) > 0 {
		return
	}
	cfg.SetupAgents()
}

func classifyDoctorProviderError(err error) doctorStatus {
	return classifyDoctorProviderIssue(err).Status
}

func classifyDoctorProviderIssue(err error) doctorProviderIssue {
	if err == nil {
		return doctorProviderIssue{
			Status: doctorStatusOK,
			Code:   "reachable",
			Label:  "reachable",
		}
	}

	text := strings.ToLower(err.Error())
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		dnsText := strings.ToLower(dnsErr.Err)
		if strings.Contains(dnsText, "operation not permitted") || strings.Contains(text, "operation not permitted") {
			return doctorProviderIssue{
				Status: doctorStatusWarn,
				Code:   "network_blocked",
				Label:  "network access blocked",
			}
		}
		if dnsErr.IsTimeout {
			return doctorProviderIssue{
				Status: doctorStatusWarn,
				Code:   "timeout",
				Label:  "timed out",
			}
		}
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "dns_unreachable",
			Label:  "DNS lookup failed",
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "timeout",
			Label:  "timed out",
		}
	}
	switch {
	case strings.Contains(text, "unauthorized"),
		strings.Contains(text, "status 401"),
		strings.Contains(text, ": 401"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "auth_failed",
			Label:  "authentication failed",
		}
	case strings.Contains(text, "forbidden"),
		strings.Contains(text, "status 403"),
		strings.Contains(text, ": 403"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "forbidden",
			Label:  "access forbidden",
		}
	case strings.Contains(text, "not a valid"),
		strings.Contains(text, "invalid api key"),
		strings.Contains(text, "invalid key"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "invalid_credentials",
			Label:  "credentials look invalid",
		}
	case strings.Contains(text, "deadline exceeded"),
		strings.Contains(text, "timeout"),
		strings.Contains(text, "tls handshake timeout"):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "timeout",
			Label:  "timed out",
		}
	case strings.Contains(text, "network is unreachable"),
		strings.Contains(text, "operation not permitted"),
		strings.Contains(text, "proxyconnect"):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "network_blocked",
			Label:  "network access blocked",
		}
	case strings.Contains(text, "no such host"),
		strings.Contains(text, "lookup "):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "dns_unreachable",
			Label:  "DNS lookup failed",
		}
	case strings.Contains(text, "connection refused"),
		strings.Contains(text, "connection reset"),
		strings.Contains(text, "tls handshake"),
		strings.Contains(text, "x509"),
		strings.Contains(text, "eof"):
		return doctorProviderIssue{
			Status: doctorStatusWarn,
			Code:   "transport_error",
			Label:  "transport failed",
		}
	case strings.Contains(text, "failed to create request for provider"):
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "request_error",
			Label:  "request setup failed",
		}
	default:
		return doctorProviderIssue{
			Status: doctorStatusFail,
			Code:   "unknown_error",
			Label:  "failed",
		}
	}
}

func trimDoctorError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 180 {
		text = text[:180] + "…"
	}
	return text
}

func doctorProviderConnectivitySummary(okCount, warnCount, failCount int, categoryCounts map[string]int) string {
	parts := make([]string, 0, 4)
	if okCount > 0 {
		parts = append(parts, fmt.Sprintf("%d reachable", okCount))
	}

	type categorySummary struct {
		Code  string
		Label string
	}
	for _, category := range []categorySummary{
		{Code: "auth_failed", Label: "auth failed"},
		{Code: "forbidden", Label: "forbidden"},
		{Code: "invalid_credentials", Label: "invalid credentials"},
		{Code: "dns_unreachable", Label: "DNS issues"},
		{Code: "network_blocked", Label: "network blocked"},
		{Code: "timeout", Label: "timed out"},
		{Code: "transport_error", Label: "transport issues"},
		{Code: "request_error", Label: "request setup failures"},
		{Code: "unknown_error", Label: "other failures"},
	} {
		if count := categoryCounts[category.Code]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, category.Label))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "no provider checks ran")
	}
	switch {
	case failCount > 0:
		return "provider checks completed with failures: " + strings.Join(parts, ", ")
	case warnCount > 0:
		return "provider checks completed with warnings: " + strings.Join(parts, ", ")
	default:
		return "provider connectivity healthy: " + strings.Join(parts, ", ")
	}
}

func doctorSourceHasMarkers(content string, markers ...string) bool {
	for _, marker := range markers {
		if !strings.Contains(content, marker) {
			return false
		}
	}
	return true
}

func doctorStatusCounts(checks []doctorCheck) (ok, warn, fail, skip int) {
	for _, check := range checks {
		switch check.Status {
		case doctorStatusOK:
			ok++
		case doctorStatusWarn:
			warn++
		case doctorStatusFail:
			fail++
		case doctorStatusSkip:
			skip++
		}
	}
	return ok, warn, fail, skip
}
