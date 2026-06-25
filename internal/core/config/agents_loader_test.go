package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAgentsFromDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	result := LoadAgentsFromDir(dir, nil)
	assert.Empty(t, result)
}

func TestLoadAgentsFromDir_NonExistentDir(t *testing.T) {
	result := LoadAgentsFromDir("/nonexistent/path/12345", nil)
	assert.Empty(t, result)
}

func TestLoadAgentsFromDir_EmptyString(t *testing.T) {
	result := LoadAgentsFromDir("", nil)
	assert.Empty(t, result)
}

func TestLoadAgentsFromDir_ValidToml(t *testing.T) {
	dir := t.TempDir()

	// Valid agent file
	err := os.WriteFile(filepath.Join(dir, "ask.toml"), []byte(`
name = "Ask"
description = "Answer questions"
tools = ["read_file", "grep", "glob"]
system_prompt = "You are helpful."
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1)
	agent, ok := result["ask"]
	require.True(t, ok)
	assert.Equal(t, "ask", agent.ID)
	assert.Equal(t, "Ask", agent.Name)
	assert.Equal(t, "Answer questions", agent.Description)
	assert.Equal(t, SelectedModelTypeLarge, agent.Model)
	assert.Contains(t, agent.AllowedTools, "view")
	assert.Contains(t, agent.AllowedTools, "grep")
	assert.Contains(t, agent.AllowedTools, "glob")
	assert.Equal(t, "You are helpful.", agent.SystemPrompt)
}

func TestLoadAgentsFromDir_MissingName_UsesID(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "custom.toml"), []byte(`
description = "No name"
tools = ["grep"]
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1)
	agent := result["custom"]
	assert.Equal(t, "custom", agent.Name, "should use filename as name when name is empty")
}

func TestLoadAgentsFromDir_InvalidToml_Skipped(t *testing.T) {
	dir := t.TempDir()

	// Valid file
	err := os.WriteFile(filepath.Join(dir, "valid.toml"), []byte(`
name = "Valid"
description = "OK"
`), 0o644)
	require.NoError(t, err)

	// Invalid TOML - should be skipped
	err = os.WriteFile(filepath.Join(dir, "invalid.toml"), []byte(`
name = "Invalid
invalid toml
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1, "only valid file should be loaded")
	_, ok := result["valid"]
	require.True(t, ok)
	_, ok = result["invalid"]
	assert.False(t, ok)
}

func TestLoadAgentsFromDir_IgnoresJson(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{}`), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "valid.toml"), []byte(`
name = "Valid"
description = "OK"
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1)
	_, ok := result["valid"]
	require.True(t, ok)
}

func TestLoadAgentsFromDir_ToolsMapping(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "tools.toml"), []byte(`
name = "Tools"
tools = ["read_file", "write_file", "unknown_tool", "grep"]
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1)
	agent := result["tools"]

	// read_file -> view, write_file -> write, unknown_tool dropped, grep -> grep
	assert.Contains(t, agent.AllowedTools, "view")
	assert.Contains(t, agent.AllowedTools, "write")
	assert.Contains(t, agent.AllowedTools, "grep")
	assert.NotContains(t, agent.AllowedTools, "unknown_tool")
}

func TestLoadAgentsFromDir_EmptyTools_UsesAllTools(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "all.toml"), []byte(`
name = "All"
description = "Uses all"
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1)
	agent := result["all"]
	assert.Equal(t, allToolNames(), agent.AllowedTools)
}

func TestLoadAgentsFromDir_DisabledTools(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "agent.toml"), []byte(`
name = "Agent"
tools = ["read_file", "grep", "edit"]
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, []string{"grep", "edit"})
	require.Len(t, result, 1)
	agent := result["agent"]
	// grep and edit disabled, only view (from read_file) remains
	assert.NotContains(t, agent.AllowedTools, "grep")
	assert.NotContains(t, agent.AllowedTools, "edit")
	assert.Contains(t, agent.AllowedTools, "view")
}

func TestResolveAgentsDir_EnvOverrides(t *testing.T) {
	envDir := t.TempDir()
	restore := setEnv("MOCODE_AGENTS_DIR", envDir)
	defer restore()

	cfg := &Config{Options: &Options{AgentsDir: "/other/path"}}
	got := resolveAgentsDir(cfg)
	assert.Equal(t, envDir, got)
}

func TestResolveAgentsDir_OptionsAgentsDir(t *testing.T) {
	restore := setEnv("MOCODE_AGENTS_DIR", "")
	defer restore()

	cfg := &Config{Options: &Options{AgentsDir: "/custom/agents"}}
	got := resolveAgentsDir(cfg)
	assert.Equal(t, "/custom/agents", got)
}

func TestResolveAgentsDir_ExpandTilde(t *testing.T) {
	restore := setEnv("MOCODE_AGENTS_DIR", "")
	defer restore()

	cfg := &Config{Options: &Options{AgentsDir: "~/.custom-agents"}}
	got := resolveAgentsDir(cfg)
	// Should expand ~ to home dir
	assert.NotContains(t, got, "~")
	assert.Contains(t, got, ".custom-agents")
}

func TestResolveAgentsDir_Default(t *testing.T) {
	restore := setEnv("MOCODE_AGENTS_DIR", "")
	defer restore()

	cfg := &Config{Options: nil}
	got := resolveAgentsDir(cfg)
	assert.NotEmpty(t, got)
	assert.Contains(t, got, ".mocode")
	assert.Contains(t, got, "agents")
}

func setEnv(key, value string) func() {
	old := os.Getenv(key)
	if value != "" {
		os.Setenv(key, value)
	} else {
		os.Unsetenv(key)
	}
	return func() {
		if old != "" {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	}
}

func TestLoadAgentsFromDir_ValidMD(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte(`---
id: "plan"
name: "Plan"
description: "Plan changes before implementing"
tools:
  - bash
  - view
---
You are a planning-only assistant.
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 1)
	agent, ok := result["plan"]
	require.True(t, ok)
	assert.Equal(t, "plan", agent.ID)
	assert.Equal(t, "Plan", agent.Name)
	assert.Equal(t, "Plan changes before implementing", agent.Description)
	assert.Contains(t, agent.AllowedTools, "bash")
	assert.Contains(t, agent.AllowedTools, "view")
	assert.Contains(t, agent.SystemPrompt, "planning-only assistant")
}

func TestLoadAgentsFromDir_MixedMDAndTOML(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "ask.toml"), []byte(`
name = "Ask"
description = "TOML agent"
tools = ["grep"]
system_prompt = "TOML prompt"
`), 0o644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(dir, "plan.md"), []byte(`---
id: "plan"
name: "Plan"
description: "MD agent"
---
MD prompt
`), 0o644)
	require.NoError(t, err)

	result := LoadAgentsFromDir(dir, nil)
	require.Len(t, result, 2)

	tomlAgent, ok := result["ask"]
	require.True(t, ok)
	assert.Equal(t, "TOML prompt", tomlAgent.SystemPrompt)

	mdAgent, ok := result["plan"]
	require.True(t, ok)
	assert.Contains(t, mdAgent.SystemPrompt, "MD prompt")
}

func TestSyncAgentsToDir_FirstRun(t *testing.T) {
	dir := t.TempDir()

	err := SyncAgentsToDir(dir)
	require.NoError(t, err)

	// Version file should exist.
	vData, err := os.ReadFile(filepath.Join(dir, agentsVersionFile))
	require.NoError(t, err)
	assert.Equal(t, AgentsVersion+"\n", string(vData))

	// At least some .md files should be present.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	hasMD := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			hasMD = true
			break
		}
	}
	assert.True(t, hasMD, "should have synced some .md files")
}

func TestSyncAgentsToDir_VersionMatchSkips(t *testing.T) {
	dir := t.TempDir()

	// First sync.
	err := SyncAgentsToDir(dir)
	require.NoError(t, err)

	// Remove a synced file to detect if second sync runs.
	entries, _ := os.ReadDir(dir)
	var removedFile string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			removedFile = filepath.Join(dir, e.Name())
			os.Remove(removedFile)
			break
		}
	}
	require.NotEmpty(t, removedFile, "should have found an .md file to remove")

	// Second sync should skip because version matches.
	err = SyncAgentsToDir(dir)
	require.NoError(t, err)

	// The removed file should still be absent.
	_, err = os.Stat(removedFile)
	assert.True(t, os.IsNotExist(err), "removed file should not be restored when version matches")
}

func TestSyncAgentsToDir_EmptyDir(t *testing.T) {
	err := SyncAgentsToDir("")
	require.NoError(t, err)
}
