package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/charmbracelet/x/etag"

	"github.com/nextsko/mocode-agent/internal/util/infra"
)

// providersCacheFile is the cache file name for the provider list.
const providersCacheFile = "providers"

// cachePathFor returns the cache file path for the provider list.
func cachePathFor() string {
	name := providersCacheFile
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName, name+".json")
	}

	// return the path to the main data directory
	// for windows, it should be in `%LOCALAPPDATA%/Mocode/`
	// for linux and macOS, it should be in `$HOME/.local/share/mocode/`
	if runtime.GOOS == goosWindows {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, appName, name+".json")
	}

	return filepath.Join(infra.Dir(), ".local", "share", appName, name+".json")
}

// ProviderCachePath returns the path of the provider catalog cache file.
func ProviderCachePath() string { return cachePathFor() }

// ReadProviderCache returns the cached provider catalog. It reports an error
// when no usable cache exists yet.
func ReadProviderCache() ([]Provider, error) {
	providers, _, err := newCache[[]Provider](cachePathFor()).Get()
	if err != nil {
		return nil, err
	}
	return providers, nil
}

// WriteProviderCache stores the provider catalog in the cache file.
func WriteProviderCache(providers []Provider) error {
	return newCache[[]Provider](cachePathFor()).Store(providers)
}

type cache[T any] struct {
	path string
}

func newCache[T any](path string) cache[T] {
	return cache[T]{path: path}
}

func (c cache[T]) Get() (T, string, error) {
	var v T
	data, err := os.ReadFile(c.path)
	if err != nil {
		return v, "", fmt.Errorf("failed to read provider cache file: %w", err)
	}

	if err := json.Unmarshal(data, &v); err != nil {
		return v, "", fmt.Errorf("failed to unmarshal provider data from cache: %w", err)
	}

	return v, etag.Of(data), nil
}

func (c cache[T]) Store(v T) error {
	slog.Info("Saving provider data to disk", "path", c.path)
	if err := os.MkdirAll(filepath.Dir(c.path), 0o750); err != nil {
		return fmt.Errorf("failed to create directory for provider cache: %w", err)
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal provider data: %w", err)
	}

	if err := os.WriteFile(c.path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write provider data to cache: %w", err)
	}
	return nil
}
