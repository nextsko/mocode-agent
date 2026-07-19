package config

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"github.com/nextsko/mocode-agent/internal/util/fsext"
	"github.com/nextsko/mocode-agent/internal/util/infra"
)

//go:embed templates/modes/*.md
var embeddedModePrompts embed.FS

const modesConfigName = "modes.toml"

// ModeConfig represents a single mode loaded from TOML.
type ModeConfig struct {
	ID           string              `toml:"id"`
	Name         string              `toml:"name"`
	Description  string              `toml:"description"`
	Model        string              `toml:"model"` // "large" or "small"
	AllowedTools []string            `toml:"allowed_tools"`
	AllowedMCP   map[string][]string `toml:"allowed_mcp"`
	ContextPaths []string            `toml:"context_paths"`
}

// ModesConfig is the root structure of modes.toml.
type ModesConfig struct {
	Modes map[string]ModeConfig `toml:"modes"`
}

// globalModesConfigPath returns the path to the global modes.toml.
func globalModesConfigPath() string {
	if MocodeGlobal := os.Getenv("MOCODE_GLOBAL_CONFIG"); MocodeGlobal != "" {
		return filepath.Join(MocodeGlobal, modesConfigName)
	}
	if xdgConfigHome := os.Getenv("XDG_CONFIG_HOME"); xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, appName, modesConfigName)
	}
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, appName, modesConfigName)
	}
	return filepath.Join(infra.Dir(), ".config", appName, modesConfigName)
}

// lookupModesConfigs searches for modes.toml from CWD up to FS root.
func lookupModesConfigs(cwd string) []string {
	configNames := []string{
		modesConfigName,
		"." + appName + "/" + modesConfigName,
	}
	found, err := fsext.Lookup(cwd, configNames...)
	if err != nil {
		return nil
	}
	slices.Reverse(found)

	// Prepend global config path if it exists
	globalPath := globalModesConfigPath()
	if data, err := os.ReadFile(globalPath); err == nil && len(data) > 0 {
		found = append([]string{globalPath}, found...)
	}
	return found
}

// LoadModesFromTOML loads mode configurations from modes.toml files.
func LoadModesFromTOML(cwd string, baseAgents map[string]Agent, disabledTools []string) map[string]Agent {
	paths := lookupModesConfigs(cwd)
	return LoadModesFromTOMLWithPaths(paths, baseAgents, disabledTools)
}

// LoadModesFromTOMLWithPaths loads mode configurations using pre-fetched file paths.
func LoadModesFromTOMLWithPaths(paths []string, baseAgents map[string]Agent, disabledTools []string) map[string]Agent {
	if len(paths) == 0 {
		return baseAgents
	}

	result := make(map[string]Agent)
	for k, v := range baseAgents {
		result[k] = v
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg ModesConfig
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			continue
		}

		for id, m := range cfg.Modes {
			if m.ID == "" {
				m.ID = id
			}
			if m.Name == "" {
				m.Name = id
			}
			if m.Model == "" {
				m.Model = string(SelectedModelTypeLarge)
			}
			modelType := SelectedModelTypeLarge
			if m.Model == "small" {
				modelType = SelectedModelTypeSmall
			}

			allowedTools := m.AllowedTools
			if allowedTools == nil {
				allowedTools = allToolNames()
			}
			allowedTools = resolveAllowedTools(allowedTools, disabledTools)

			agent := Agent{
				ID:           m.ID,
				Name:         m.Name,
				Description:  m.Description,
				Model:        modelType,
				AllowedTools: allowedTools,
				AllowedMCP:   m.AllowedMCP,
				ContextPaths: m.ContextPaths,
			}
			result[id] = agent
		}
	}

	return result
}

// DefaultModeConfigs returns built-in mode presets from embedded .md files.
func DefaultModeConfigs() map[string]Agent {
	agents := map[string]Agent{
		AgentCoder: {
			ID:           AgentCoder,
			Name:         "Coder",
			Description:  "执行编码任务",
			Model:        SelectedModelTypeLarge,
			AllowedTools: nil, // all tools
		},
		AgentTask: {
			ID:           AgentTask,
			Name:         "Task",
			Description:  "搜索与上下文分析",
			Model:        SelectedModelTypeLarge,
			AllowedTools: nil, // all tools
		},
	}

	// Load all embedded modes.
	embedded, err := loadEmbeddedModes()
	if err == nil {
		for id, agent := range embedded {
			agents[id] = agent
		}
	}

	return agents
}

// loadEmbeddedModes reads all embedded mode .md files with YAML front matter.
func loadEmbeddedModes() (map[string]Agent, error) {
	entries, err := embeddedModePrompts.ReadDir("templates/modes")
	if err != nil {
		return nil, fmt.Errorf("read embedded modes dir: %w", err)
	}

	result := make(map[string]Agent, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		data, err := embeddedModePrompts.ReadFile("templates/modes/" + e.Name())
		if err != nil {
			continue
		}

		info := parseModeFrontMatter(data)
		if info.id == "" {
			info.id = id
		}
		if info.name == "" {
			info.name = id
		}
		if info.description == "" {
			info.description = id
		}

		// Resolve model type.
		modelType := SelectedModelTypeLarge
		if info.model == "small" {
			modelType = SelectedModelTypeSmall
		}

		result[id] = Agent{
			ID:            id,
			Name:          info.name,
			Description:   info.description,
			Model:         modelType,
			Disabled:      info.disabled,
			AllowedTools:  info.tools,
			AllowedMCP:    info.allowedMCP,
			ContextPaths:  info.contextPaths,
			SystemPrompt:  info.prompt,
			SubAgents:     info.subAgents,
			Temperature:   info.temperature,
			MaxTokens:     info.maxTokens,
			DisabledTools: info.disabledTools,
		}
	}
	return result, nil
}

type modeFrontMatter struct {
	id            string
	name          string
	description   string
	model         string
	disabled      bool
	tools         []string
	disabledTools []string
	allowedMCP    map[string][]string
	contextPaths  []string
	subAgents     []string
	temperature   *float64
	maxTokens     *int64
	prompt        string
}

func parseModeFrontMatter(data []byte) modeFrontMatter {
	var m modeFrontMatter
	content := string(data)

	// Check for YAML front matter between --- markers.
	if strings.HasPrefix(content, "---\n") {
		endIdx := strings.Index(content[4:], "\n---\n")
		if endIdx > 0 {
			yamlBlock := content[4 : 4+endIdx]
			var fm struct {
				ID            string              `yaml:"id"`
				Name          string              `yaml:"name"`
				Description   string              `yaml:"description"`
				Model         string              `yaml:"model"`
				Disabled      bool                `yaml:"disabled"`
				Tools         []string            `yaml:"tools"`
				DisabledTools []string            `yaml:"disabled_tools"`
				AllowedMCP    map[string][]string `yaml:"allowed_mcp"`
				ContextPaths  []string            `yaml:"context_paths"`
				SubAgents     []string            `yaml:"sub_agents"`
				Temperature   *float64            `yaml:"temperature"`
				MaxTokens     *int64              `yaml:"max_tokens"`
			}
			if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err == nil {
				m.id = fm.ID
				m.name = fm.Name
				m.description = fm.Description
				m.model = fm.Model
				m.disabled = fm.Disabled
				m.tools = fm.Tools
				m.disabledTools = fm.DisabledTools
				m.allowedMCP = fm.AllowedMCP
				m.contextPaths = fm.ContextPaths
				m.subAgents = fm.SubAgents
				m.temperature = fm.Temperature
				m.maxTokens = fm.MaxTokens
			}
			// Prompt is everything after the second --- marker.
			rest := content[4+endIdx+5:]
			m.prompt = strings.TrimSpace(rest)
			return m
		}
	}

	// No front matter 鈥?use the raw content as prompt.
	m.prompt = strings.TrimSpace(content)
	return m
}

// ValidateModeID returns an error if the mode ID is unknown.
func (c *Config) ValidateModeID(modeID string) error {
	if _, ok := c.Agents[modeID]; ok {
		return nil
	}
	return fmt.Errorf("unknown mode: %s", modeID)
}
