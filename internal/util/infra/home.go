// Package infra provides infrastructure utilities for dealing with the
// user's home directory and configuration paths.
package infra

import (
	"cmp"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var homedir, homedirErr = os.UserHomeDir()

func init() {
	if homedirErr != nil {
		slog.Error("Failed to get user home directory", "error", homedirErr)
	}
}

// Dir returns the user home directory.
func Dir() string {
	return homedir
}

// appName is the product/application directory name. It is duplicated from the
// config package so this util-layer package can resolve data paths without an
// upward dependency on core/config.
const appName = "mocode"

// DataDir returns the global data directory that holds all projects and the
// shared mocode.json. It mirrors config.GlobalConfigData()'s directory
// resolution but lives in the util layer so persistence (internal/store) can
// locate the data dir without depending upward on core/config. Resolution
// order:
//   - MOCODE_GLOBAL_DATA (root; the json sits directly inside)
//   - XDG_DATA_HOME/<appName>
//   - %LOCALAPPDATA%/<appName> on Windows
//   - ~/.local/share/<appName> elsewhere.
func DataDir() string {
	if root := os.Getenv("MOCODE_GLOBAL_DATA"); root != "" {
		return root
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName)
	}
	if runtime.GOOS == "windows" {
		localAppData := cmp.Or(
			os.Getenv("LOCALAPPDATA"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local"),
		)
		return filepath.Join(localAppData, appName)
	}
	return filepath.Join(Dir(), ".local", "share", appName)
}

// Config returns the user config directory.
func Config() string {
	return cmp.Or(
		os.Getenv("XDG_CONFIG_HOME"),
		filepath.Join(Dir(), ".config"),
	)
}

// Short replaces the actual home path from [Dir] with `~`.
func Short(p string) string {
	if homedir == "" || !strings.HasPrefix(p, homedir) {
		return p
	}
	return filepath.Join("~", strings.TrimPrefix(p, homedir))
}

// Long replaces the `~` with actual home path from [Dir].
func Long(p string) string {
	if homedir == "" || !strings.HasPrefix(p, "~") {
		return p
	}
	return strings.Replace(p, "~", homedir, 1)
}
