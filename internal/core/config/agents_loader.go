package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/package-register/mocode/internal/util/infra"
)

// andyCodeAgentConfig is the TOML structure for a legacy agent file.
type andyCodeAgentConfig struct {
	Name         string   `toml:"name"`
	Description  string   `toml:"description"`
	Tools        []string `toml:"tools"`
	SystemPrompt string   `toml:"system_prompt"`
}

// andyCodeToolMap maps legacy tool names to mocode tool names.
var andyCodeToolMap = map[string]string{
	"read_file":  "view",
	"list_dir":   "ls",
	"write_file": "write",
	"grep":       "grep",
	"glob":       "glob",
	"edit":       "edit",
	"web_fetch":  "fetch",
	"web_search": "sourcegraph",
	"todowrite":  "todos",
	"todoread":   "todos",
}

// mapAndyCodeTools maps legacy tool names to mocode tool names.
// Unknown tools are dropped. If result is empty, caller should use allToolNames().
func mapAndyCodeTools(tools []string) []string {
	if len(tools) == 0 {
		return nil // caller interprets as "all tools"
	}
	var result []string
	mocodeNames := allToolNames()
	mocodeSet := make(map[string]bool)
	for _, t := range mocodeNames {
		mocodeSet[t] = true
	}
	for _, t := range tools {
		if mapped, ok := andyCodeToolMap[t]; ok && mocodeSet[mapped] {
			result = append(result, mapped)
		}
	}
	return result
}

// AgentsVersion is the current version of the built-in agents.
// When this changes, built-in agents are re-synced to disk.
const AgentsVersion = "0.1.0"

const agentsVersionFile = ".agents_version"

// resolveAgentsDir returns the agents directory path.
// Priority: MOCODE_AGENTS_DIR env > Options.AgentsDir > ~/.mocode/agents
// ResolveAgentsDir is the exported form so the transport layer can compute the
// same directory when materializing /evo agents into it.
func ResolveAgentsDir(cfg *Config) string {
	if dir := os.Getenv("MOCODE_AGENTS_DIR"); dir != "" {
		return expandPath(dir)
	}
	if cfg.Options != nil && cfg.Options.AgentsDir != "" {
		return expandPath(cfg.Options.AgentsDir)
	}
	return expandPath(filepath.Join(infra.Dir(), ".mocode", "agents"))
}

// resolveAgentsDir is kept for in-package callers; delegates to the export.
func resolveAgentsDir(cfg *Config) string { return ResolveAgentsDir(cfg) }

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(infra.Dir(), p[2:])
	}
	return p
}

// SyncAgentsToDir writes built-in mode .md files to the agents directory.
// Skips if the version file matches the current AgentsVersion.
// Existing user-modified files are not overwritten.
func SyncAgentsToDir(dir string) error {
	if dir == "" {
		return nil
	}

	// Check version 鈥?skip if already current.
	versionData, err := os.ReadFile(filepath.Join(dir, agentsVersionFile))
	if err == nil && strings.TrimSpace(string(versionData)) == AgentsVersion {
		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}

	entries, err := embeddedModePrompts.ReadDir("templates/modes")
	if err != nil {
		return fmt.Errorf("read embedded modes: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		targetPath := filepath.Join(dir, e.Name())
		// Skip if user already has this file (preserve user edits).
		if _, err := os.Stat(targetPath); err == nil {
			continue
		}

		data, err := embeddedModePrompts.ReadFile("templates/modes/" + e.Name())
		if err != nil {
			slog.Warn("Failed to read embedded mode", "file", e.Name(), "error", err)
			continue
		}

		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			slog.Warn("Failed to write agent file", "path", targetPath, "error", err)
			continue
		}
	}

	if err := os.WriteFile(filepath.Join(dir, agentsVersionFile), []byte(AgentsVersion+"\n"), 0o644); err != nil {
		return fmt.Errorf("write version file: %w", err)
	}

	return nil
}

// LoadAgentsFromDir loads agent configurations from *.md and *.toml files in the given directory.
// .md files use YAML front matter (same format as embedded modes).
// .toml files use the legacy TOML format.
// Parsing failures are logged and skipped; only successfully parsed agents are returned. Never panics.
func LoadAgentsFromDir(dir string, disabledTools []string) map[string]Agent {
	result := make(map[string]Agent)
	if dir == "" {
		return result
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("Failed to read agents directory", "dir", dir, "error", err)
		}
		return result
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := strings.ToLower(e.Name())
		isTOML := strings.HasSuffix(name, ".toml")
		isMD := strings.HasSuffix(name, ".md")
		if !isTOML && !isMD {
			continue
		}
		if name == agentsVersionFile {
			continue
		}

		path := filepath.Join(dir, e.Name())
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if id == "" {
			continue
		}

		var agent Agent
		var agentErr error
		if isMD {
			agent, agentErr = loadAgentMDFile(path, id, disabledTools)
		} else {
			agent, agentErr = loadAgentFile(path, id, disabledTools)
		}
		if agentErr != nil {
			slog.Warn("Skipping agent file", "path", path, "error", agentErr)
			continue
		}
		result[id] = agent
	}

	return result
}

// loadAgentMDFile loads a single agent from a .md file with YAML front matter.
func loadAgentMDFile(path, id string, disabledTools []string) (Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Agent{}, err
	}

	info := parseModeFrontMatter(data)
	if info.id == "" {
		info.id = id
	}
	if info.name == "" {
		info.name = id
	}

	modelType := SelectedModelTypeLarge
	if info.model == "small" {
		modelType = SelectedModelTypeSmall
	}

	allowedTools := info.tools
	if allowedTools == nil {
		allowedTools = allToolNames()
	}
	allowedTools = resolveAllowedTools(allowedTools, disabledTools)

	return Agent{
		ID:            info.id,
		Name:          info.name,
		Description:   info.description,
		Model:         modelType,
		Disabled:      info.disabled,
		AllowedTools:  allowedTools,
		DisabledTools: info.disabledTools,
		AllowedMCP:    info.allowedMCP,
		ContextPaths:  info.contextPaths,
		SystemPrompt:  info.prompt,
		SubAgents:     info.subAgents,
		Temperature:   info.temperature,
		MaxTokens:     info.maxTokens,
	}, nil
}

// loadAgentFile loads a single agent from a TOML file.
func loadAgentFile(path, id string, disabledTools []string) (Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Agent{}, err
	}

	var raw andyCodeAgentConfig
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return Agent{}, err
	}

	name := raw.Name
	if name == "" {
		name = id
	}

	allowedTools := mapAndyCodeTools(raw.Tools)
	if allowedTools == nil {
		allowedTools = allToolNames()
	}
	allowedTools = resolveAllowedTools(allowedTools, disabledTools)
	if len(allowedTools) == 0 {
		allowedTools = allToolNames()
	}

	return Agent{
		ID:           id,
		Name:         name,
		Description:  raw.Description,
		Model:        SelectedModelTypeLarge,
		AllowedTools: allowedTools,
		SystemPrompt: strings.TrimSpace(raw.SystemPrompt),
	}, nil
}
