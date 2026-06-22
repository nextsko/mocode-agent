package config

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
	"github.com/charmbracelet/x/etag"
	"github.com/package-register/mocode/internal/csync"
	"github.com/package-register/mocode/internal/infra"
)

type syncer[T any] interface {
	Get(context.Context) (T, error)
}

var (
	providerOnce sync.Once
	providerList []catwalk.Provider
	providerErr  error
)

// file to cache provider data
func cachePathFor(name string) string {
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName, name+".json")
	}

	// return the path to the main data directory
	// for windows, it should be in `%LOCALAPPDATA%/Mocode/`
	// for linux and macOS, it should be in `$HOME/.local/share/mocode/`
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(localAppData, appName, name+".json")
	}

	return filepath.Join(infra.Dir(), ".local", "share", appName, name+".json")
}

// UpdateProviders updates the Catwalk providers list from a specified source.
func UpdateProviders(pathOrURL string) error {
	var providers []catwalk.Provider
	pathOrURL = cmp.Or(pathOrURL, os.Getenv("CATWALK_URL"), defaultCatwalkURL)

	switch {
	case pathOrURL == "embedded":
		providers = embedded.GetAll()
	case strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://"):
		var err error
		providers, err = catwalk.NewWithURL(pathOrURL).GetProviders(context.Background(), "")
		if err != nil {
			return fmt.Errorf("failed to fetch providers from Catwalk: %w", err)
		}
	default:
		content, err := os.ReadFile(pathOrURL)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		if err := json.Unmarshal(content, &providers); err != nil {
			return fmt.Errorf("failed to unmarshal provider data: %w", err)
		}
		if len(providers) == 0 {
			return fmt.Errorf("no providers found in the provided source")
		}
	}

	if err := newCache[[]catwalk.Provider](cachePathFor("providers")).Store(providers); err != nil {
		return fmt.Errorf("failed to save providers to cache: %w", err)
	}

	slog.Info("Providers updated successfully", "count", len(providers), "from", pathOrURL, "to", cachePathFor("providers"))
	return nil
}

// ProvidersWithCache returns the list of providers from cache only,
// without any network requests. This is the default behavior to avoid
// blocking startup on network operations.
func ProvidersWithCache(cfg *Config) ([]catwalk.Provider, error) {
	providerOnce.Do(func() {
		var errs []error
		providers := csync.NewSlice[catwalk.Provider]()
		customProvidersOnly := cfg.Options.DisableDefaultProviders

		if !customProvidersOnly {
			// Load catwalk providers from cache
			catwalkCache := newCache[[]catwalk.Provider](cachePathFor("providers"))
			catwalkProviders, _, err := catwalkCache.Get()
			if err == nil {
				providers.Append(catwalkProviders...)
			} else {
				slog.Debug("Failed to load catwalk providers from cache, using embedded", "error", err)
				providers.Append(embedded.GetAll()...)
			}
		}

		providerList = slices.Collect(providers.Seq())
		providerErr = errors.Join(errs...)
	})
	return providerList, providerErr
}

// Providers returns the list of providers. This is an alias for ProvidersWithCache
// to maintain backward compatibility. Network updates are now handled asynchronously
// via UpdateProvidersAsync.
func Providers(cfg *Config) ([]catwalk.Provider, error) {
	return ProvidersWithCache(cfg)
}

// UpdateProvidersAsync updates providers in the background without blocking.
// This should be called from a goroutine during app startup.
func UpdateProvidersAsync(cfg *Config) {
	go func() {
		if cfg.Options.DisableProviderAutoUpdate {
			return
		}
		if cfg.Options.DisableDefaultProviders {
			return
		}

		slog.Info("Starting async provider update")
		if err := UpdateProviders(""); err != nil {
			slog.Warn("Async provider update failed", "error", err)
		}
	}()
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
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for provider cache: %w", err)
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal provider data: %w", err)
	}

	if err := os.WriteFile(c.path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write provider data to cache: %w", err)
	}
	return nil
}
