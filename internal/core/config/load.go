package config

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"

	powernapConfig "github.com/charmbracelet/x/powernap/pkg/config"

	"github.com/package-register/mocode/internal/util/csync"
)

const defaultCatwalkURL = "https://catwalk.charm.land"

// Load loads the configuration from the default paths and returns a
// ConfigStore that owns both the pure-data Config and all runtime state.
func Load(workingDir string, debug bool) (*ConfigStore, error) {
	return loadConfigStore(workingDir, debug, true, true)
}

// LoadReadOnly loads configuration for inspection without mutating persisted
// config state or syncing agent files to disk.
func LoadReadOnly(workingDir string, debug bool) (*ConfigStore, error) {
	return loadConfigStore(workingDir, debug, false, false)
}

func loadConfigStore(workingDir string, debug bool, persistModels bool, syncAgents bool) (*ConfigStore, error) {
	configPaths := lookupConfigs(workingDir)

	cfg, loadedPaths, err := loadFromConfigPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from paths %v: %w", configPaths, err)
	}

	cfg.setDefaults(workingDir)

	store := &ConfigStore{
		config:         cfg,
		workingDir:     workingDir,
		globalDataPath: GlobalConfigData(),
		loadedPaths:    loadedPaths,
	}

	if debug {
		cfg.Options.Debug = true
	}

	// Validate hooks after config merging is complete.
	if err := cfg.ValidateHooks(); err != nil {
		return nil, fmt.Errorf("invalid hook configuration: %w", err)
	}

	if !isInsideWorktree() {
		const depth = 2
		const items = 100
		slog.Warn("No git repository detected in working directory, will limit file walk operations", "depth", depth, "items", items)
		assignIfNil(&cfg.Tools.Ls.MaxDepth, depth)
		assignIfNil(&cfg.Tools.Ls.MaxItems, items)
		assignIfNil(&cfg.Options.TUI.Completions.MaxDepth, depth)
		assignIfNil(&cfg.Options.TUI.Completions.MaxItems, items)
	}

	if isAppleTerminal() {
		slog.Warn("Detected Apple Terminal, enabling transparent mode")
		assignIfNil(&cfg.Options.TUI.Transparent, true)
	}

	// Load known providers, this loads the config from catwalk
	providers, err := ProvidersWithCache(cfg)
	if err != nil {
		return nil, err
	}
	store.knownProviders = providers

	env := NewEnv()
	// Configure providers
	valueResolver := NewShellVariableResolver(env)
	store.resolver = valueResolver

	// Disable auto-reload during initial load to prevent nested calls from
	// config-modifying operations inside configureProviders.
	store.autoReloadDisabled = true
	defer func() { store.autoReloadDisabled = false }()

	if err := cfg.configureProviders(store, env, valueResolver, store.knownProviders); err != nil {
		return nil, fmt.Errorf("failed to configure providers: %w", err)
	}

	if !cfg.IsConfigured() {
		slog.Warn("No providers configured")
		return store, nil
	}

	if err := configureSelectedModels(store, store.knownProviders, persistModels); err != nil {
		return nil, fmt.Errorf("failed to configure selected models: %w", err)
	}
	if syncAgents {
		store.SetupAgents()
	} else {
		cfg.SetupAgents()
	}

	// Capture initial staleness snapshot
	store.captureStalenessSnapshot(loadedPaths)

	return store, nil
}

// mustMarshalConfig marshals the config to JSON bytes, returning empty JSON on
// error.
func mustMarshalConfig(cfg *Config) []byte {
	data, err := json.Marshal(cfg)
	if err != nil {
		return []byte("{}")
	}
	return data
}

func PushPopMocodeEnv() func() {
	var found []string
	for _, ev := range os.Environ() {
		if strings.HasPrefix(ev, "MOCODE_") {
			pair := strings.SplitN(ev, "=", 2)
			if len(pair) != 2 {
				continue
			}
			found = append(found, strings.TrimPrefix(pair[0], "MOCODE_"))
		}
	}
	backups := make(map[string]string)
	for _, ev := range found {
		backups[ev] = os.Getenv(ev)
	}

	for _, ev := range found {
		_ = os.Setenv(ev, os.Getenv("MOCODE_"+ev))
	}

	restore := func() {
		for k, v := range backups {
			_ = os.Setenv(k, v)
		}
	}
	return restore
}

func (c *Config) setDefaults(workingDir string) {
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	if c.Providers == nil {
		c.Providers = csync.NewMap[string, ProviderConfig]()
	}
	if c.Models == nil {
		c.Models = make(map[SelectedModelType]SelectedModel)
	}
	if c.RecentModels == nil {
		c.RecentModels = make(map[SelectedModelType][]SelectedModel)
	}
	if c.MCP == nil {
		c.MCP = make(map[string]MCPConfig)
	}
	if c.LSP == nil {
		c.LSP = make(map[string]LSPConfig)
	}

	// Apply defaults to LSP configurations
	c.applyLSPDefaults()

	// Add the default context paths if they are not already present
	c.Options.ContextPaths = append(defaultContextPaths, c.Options.ContextPaths...)
	slices.Sort(c.Options.ContextPaths)
	c.Options.ContextPaths = slices.Compact(c.Options.ContextPaths)

	// Add the default skills directories if not already present.
	for _, dir := range GlobalSkillsDirs() {
		if !slices.Contains(c.Options.SkillsPaths, dir) {
			c.Options.SkillsPaths = append(c.Options.SkillsPaths, dir)
		}
	}

	// Project specific skills dirs.
	c.Options.SkillsPaths = append(c.Options.SkillsPaths, ProjectSkillsDir(workingDir)...)

	if str, ok := os.LookupEnv("MOCODE_DISABLE_PROVIDER_AUTO_UPDATE"); ok {
		c.Options.DisableProviderAutoUpdate, _ = strconv.ParseBool(str)
	}

	if str, ok := os.LookupEnv("MOCODE_DISABLE_DEFAULT_PROVIDERS"); ok {
		c.Options.DisableDefaultProviders, _ = strconv.ParseBool(str)
	}

	if c.Options.Attribution == nil {
		c.Options.Attribution = &Attribution{
			TrailerStyle:  TrailerStyleNone,
			GeneratedWith: false,
		}
	} else if c.Options.Attribution.TrailerStyle == "" {
		// Migrate deprecated co_authored_by or apply default
		if c.Options.Attribution.CoAuthoredBy != nil {
			if *c.Options.Attribution.CoAuthoredBy {
				c.Options.Attribution.TrailerStyle = TrailerStyleCoAuthoredBy
			} else {
				c.Options.Attribution.TrailerStyle = TrailerStyleNone
			}
		} else {
			c.Options.Attribution.TrailerStyle = TrailerStyleNone
		}
	}
	c.Options.InitializeAs = cmp.Or(c.Options.InitializeAs, defaultInitializeAs)
}

// applyLSPDefaults applies default values from powernap to LSP configurations
func (c *Config) applyLSPDefaults() {
	// Get powernap's default configuration
	configManager := powernapConfig.NewManager()
	if err := configManager.LoadDefaults(); err != nil {
		slog.Warn("Failed to load LSP defaults", "error", err)
		return
	}

	// Apply defaults to each LSP configuration
	for name, cfg := range c.LSP {
		// Try to get defaults from powernap based on name or command name.
		base, ok := configManager.GetServer(name)
		if !ok {
			base, ok = configManager.GetServer(cfg.Command)
			if !ok {
				continue
			}
		}
		if cfg.Options == nil {
			cfg.Options = base.Settings
		}
		if cfg.InitOptions == nil {
			cfg.InitOptions = base.InitOptions
		}
		if len(cfg.FileTypes) == 0 {
			cfg.FileTypes = base.FileTypes
		}
		if len(cfg.RootMarkers) == 0 {
			cfg.RootMarkers = base.RootMarkers
		}
		cfg.Command = cmp.Or(cfg.Command, base.Command)
		if len(cfg.Args) == 0 {
			cfg.Args = base.Args
		}
		if len(cfg.Env) == 0 {
			cfg.Env = base.Environment
		}
		// Update the config in the map
		c.LSP[name] = cfg
	}
}

// lookupConfigs searches config files recursively from CWD up to FS root

// GlobalConfig returns the global configuration file path for the application.

// GlobalCacheDir returns the path to the global cache directory for the
// application.

// GlobalConfigData returns the path to the main data directory for the application.
// this config is used when the app overrides configurations instead of updating the global config.

// GlobalWorkspaceDir returns the path to the global server workspace
// directory. This directory acts as a meta-workspace for the server
// process, giving it a real workingDir so that config loading, scoped
// writes, and provider resolution behave identically to project
// workspaces.

// GlobalSkillsDirs returns the default directories for Agent Skills.
// Skills in these directories are auto-discovered and their files can be read
// without permission prompts.

// ProjectSkillsDir returns the default project directories for which Mocode
// will look for skills.

func isAppleTerminal() bool { return os.Getenv("TERM_PROGRAM") == "Apple_Terminal" }

// normalizeHookEvent maps user-provided event names to their canonical
// form. Matching is case-insensitive and accepts snake_case variants
// (e.g. "pre_tool_use" 闁?"PreToolUse").

// ValidateHooks normalizes event names and checks that every configured
// hook has a command and a syntactically valid matcher regex. Matcher
// compilation used for matching is owned by hooks.Runner; this function
// only validates up front so the user sees config errors at load time
// rather than on the first tool call.
