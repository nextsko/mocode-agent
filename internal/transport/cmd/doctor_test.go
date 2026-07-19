package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/util/csync"
)

func TestParseDoctorModulePath(t *testing.T) {
	t.Parallel()

	mod := parseDoctorModulePath([]byte("module github.com/nextsko/mocode-agent\n\ngo 1.26.0\n"))
	require.Equal(t, "github.com/nextsko/mocode-agent", mod)
}

func TestInspectDoctorPath(t *testing.T) {
	t.Parallel()

	existing := t.TempDir()
	existingState := inspectDoctorPath(existing)
	require.True(t, existingState.Exists)
	require.Equal(t, existing, existingState.NearestExisting)

	missing := filepath.Join(existing, "nested", "child")
	missingState := inspectDoctorPath(missing)
	require.False(t, missingState.Exists)
	require.Equal(t, existing, missingState.NearestExisting)
	require.Nil(t, missingState.Err)
}

func TestDoctorJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config-home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data-home"))
	t.Setenv("MOCODE_GLOBAL_CONFIG", "")
	t.Setenv("MOCODE_GLOBAL_DATA", "")

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "mocode.json"), []byte(`{"providers":{"demo":{}}}`), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	var out bytes.Buffer
	doctorCmd.SetOut(&out)
	doctorCmd.SetErr(&out)
	doctorCmd.SetIn(bytes.NewReader(nil))
	require.NoError(t, doctorCmd.Flags().Set("json", "true"))
	defer func() {
		_ = doctorCmd.Flags().Set("json", "false")
	}()

	err = doctorCmd.RunE(doctorCmd, nil)
	require.NoError(t, err)

	var report doctorReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &report))
	require.NotEmpty(t, report.Version)
	require.Equal(t, tmpDir, report.CWD)
	require.NotEmpty(t, report.Checks)

	var names []string
	for _, check := range report.Checks {
		names = append(names, check.Name)
	}
	require.Contains(t, names, "Binary")
	require.Contains(t, names, "Runtime")
	require.Contains(t, names, "Config files")
}

func TestClassifyDoctorProviderIssue(t *testing.T) {
	t.Parallel()

	issue := classifyDoctorProviderIssue(nil)
	require.Equal(t, doctorStatusOK, issue.Status)
	require.Equal(t, "reachable", issue.Code)

	issue = classifyDoctorProviderIssue(errors.New("dial tcp: lookup api.example.com: no such host"))
	require.Equal(t, doctorStatusWarn, issue.Status)
	require.Equal(t, "dns_unreachable", issue.Code)

	issue = classifyDoctorProviderIssue(fmt.Errorf("wrapped: %w", &net.DNSError{
		Name:   "api.z.ai",
		Server: "127.0.0.53:53",
		Err:    "socket: operation not permitted",
	}))
	require.Equal(t, doctorStatusWarn, issue.Status)
	require.Equal(t, "network_blocked", issue.Code)

	issue = classifyDoctorProviderIssue(errors.New("failed to connect to provider openai: 401 Unauthorized"))
	require.Equal(t, doctorStatusFail, issue.Status)
	require.Equal(t, "auth_failed", issue.Code)
}

func TestDoctorModelConfigCheck(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Options: &config.Options{},
		Providers: configProviders(map[string]config.ProviderConfig{
			"openai": {
				ID:   "openai",
				Name: "OpenAI",
				Models: []catwalk.Model{
					{ID: "gpt-4.1"},
					{ID: "gpt-4.1-mini"},
				},
			},
		}),
		Models: map[config.SelectedModelType]config.SelectedModel{
			config.SelectedModelTypeLarge: {Provider: "openai", Model: "gpt-4.1"},
			config.SelectedModelTypeSmall: {Provider: "openai", Model: "gpt-4.1-mini"},
		},
		Agents: map[string]config.Agent{
			config.AgentPlan:  {ID: config.AgentPlan, Model: config.SelectedModelTypeLarge},
			config.AgentCoder: {ID: config.AgentCoder, Model: config.SelectedModelTypeLarge},
		},
	}

	check := doctorModelConfigCheck(&doctorLoadedConfig{cfg: cfg}, nil)
	require.Equal(t, doctorStatusOK, check.Status)
}

func TestDoctorSubagentTUICheckFailsOnMissingActiveModeOrSubAgent(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Options: &config.Options{ActiveMode: "ghost"},
		Providers: configProviders(map[string]config.ProviderConfig{
			"openai": {ID: "openai"},
		}),
		Agents: map[string]config.Agent{
			config.AgentPlan: {
				ID:        config.AgentPlan,
				Model:     config.SelectedModelTypeLarge,
				SubAgents: []string{"missing"},
			},
		},
	}

	check := doctorSubagentTUICheck(&doctorLoadedConfig{cfg: cfg}, nil)
	require.Equal(t, doctorStatusFail, check.Status)
}

func TestDoctorSubagentSourceCheckWarnsOnIndexBasedPanelBinding(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	uiPath := filepath.Join(tmpDir, "internal", "ui", "model", "ui.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(uiPath), 0o755))
	require.NoError(t, os.WriteFile(uiPath, []byte(strings.Join([]string{
		"package model",
		"func (m *UI) loadNestedToolCalls() {}",
		"registerAgentToolTopology(toolItem.MessageID(), sessionIDOrEmpty(m.session), tc)",
		"m.loadNestedToolCalls(nestedMessageItems)",
		"registerAgentToolTopology(",
		"resolveAgentToolContainerID(",
		"containerID := m.resolveAgentToolContainerID(toolCallID)",
		"len(msg.ToolCalls()) == 0 && len(msg.ToolResults()) == 0 && msg.Role == message.Assistant",
		"summary := firstContentLine(msg.Content().Text)",
		"childSessionDescriptor(parentTool, childSessionID)",
		"func agentToolChildCallIDs(tc message.ToolCall) []string {",
		"fmt.Sprintf(\"%s-%d\", tc.ID, i+1)",
		"DependsOn",
		"fmt.Sprintf(\"%s-%s\", tc.ID, taskID)",
	}, "\n")), 0o644))

	chatAgentPath := filepath.Join(tmpDir, "internal", "ui", "chat", "agent.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(chatAgentPath), 0o755))
	require.NoError(t, os.WriteFile(chatAgentPath, []byte(strings.Join([]string{
		"package chat",
		"if i < len(r.agent.nestedTools) {",
		"content = r.agent.nestedTools[i].Render(80)",
		"}",
	}, "\n")), 0o644))

	check := doctorSubagentSourceCheck(tmpDir, doctorWorkspaceFacts{})
	require.Equal(t, doctorStatusWarn, check.Status)
	require.Equal(t, "TUI / subagent source", check.Name)
}

func TestDoctorSubagentSourceCheckFailsWhenCapabilitiesMissing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	uiPath := filepath.Join(tmpDir, "internal", "ui", "model", "ui.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(uiPath), 0o755))
	require.NoError(t, os.WriteFile(uiPath, []byte("package model\nregisterAgentToolTopology(\n"), 0o644))

	check := doctorSubagentSourceCheck(tmpDir, doctorWorkspaceFacts{})
	require.Equal(t, doctorStatusFail, check.Status)
	require.Equal(t, "TUI / subagent source", check.Name)
	require.Contains(t, check.Details[len(check.Details)-1], "missing:")
}

func configProviders(items map[string]config.ProviderConfig) *csync.Map[string, config.ProviderConfig] {
	providers := csync.NewMap[string, config.ProviderConfig]()
	for id, provider := range items {
		providers.Set(id, provider)
	}
	return providers
}
