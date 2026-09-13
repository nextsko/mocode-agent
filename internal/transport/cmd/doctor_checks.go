package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/util/version"
)

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
