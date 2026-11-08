// Package catwalkconv adapts the Catwalk provider catalog to the provider
// types owned by internal/core/config.
//
// The config package deliberately knows nothing about the catalog client: it
// owns the wire-compatible types, while this package is the only place in
// core that imports charm.land/catwalk and its embedded catalog. Configuration
// loading receives the catalog through a source function, so the dependency
// points inwards (adapter -> config) and never the other way around.
package catwalkconv

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"

	"github.com/nextsko/mocode-agent/internal/core/config"
)

// defaultCatalogURL is the upstream Catwalk catalog endpoint.
const defaultCatalogURL = "https://catwalk.charm.land"

// ToConfigModel converts a catalog model into the config-owned model type.
func ToConfigModel(m catwalk.Model) config.Model {
	return config.Model{
		ID:                     m.ID,
		Name:                   m.Name,
		CostPer1MIn:            m.CostPer1MIn,
		CostPer1MOut:           m.CostPer1MOut,
		CostPer1MInCached:      m.CostPer1MInCached,
		CostPer1MOutCached:     m.CostPer1MOutCached,
		ContextWindow:          m.ContextWindow,
		DefaultMaxTokens:       m.DefaultMaxTokens,
		CanReason:              m.CanReason,
		ReasoningLevels:        m.ReasoningLevels,
		DefaultReasoningEffort: m.DefaultReasoningEffort,
		SupportsImages:         m.SupportsImages,
		Options: config.ModelOptions{
			Temperature:      m.Options.Temperature,
			TopP:             m.Options.TopP,
			TopK:             m.Options.TopK,
			FrequencyPenalty: m.Options.FrequencyPenalty,
			PresencePenalty:  m.Options.PresencePenalty,
			ProviderOptions:  m.Options.ProviderOptions,
		},
	}
}

// ToConfigProvider converts a catalog provider into the config-owned provider
// type. The JSON shape is identical to the catalog's, so cache files stay
// interchangeable.
func ToConfigProvider(p catwalk.Provider) config.Provider {
	models := make([]config.Model, len(p.Models))
	for i, m := range p.Models {
		models[i] = ToConfigModel(m)
	}
	return config.Provider{
		Name:                p.Name,
		ID:                  config.InferenceProvider(p.ID),
		APIKey:              p.APIKey,
		APIEndpoint:         p.APIEndpoint,
		Type:                config.ProviderType(p.Type),
		DefaultLargeModelID: p.DefaultLargeModelID,
		DefaultSmallModelID: p.DefaultSmallModelID,
		Models:              models,
		DefaultHeaders:      p.DefaultHeaders,
	}
}

// ToConfigProviders converts a whole catalog.
func ToConfigProviders(providers []catwalk.Provider) []config.Provider {
	out := make([]config.Provider, len(providers))
	for i, p := range providers {
		out[i] = ToConfigProvider(p)
	}
	return out
}

// Embedded returns the provider catalog shipped inside the Catwalk module.
func Embedded() []config.Provider {
	return ToConfigProviders(embedded.GetAll())
}

// ProvidersWithCache returns the known-provider catalog from the local cache,
// falling back to the embedded catalog. It performs no network requests, so it
// is safe to call during startup.
func ProvidersWithCache(cfg *config.Config) ([]config.Provider, error) {
	if disablesDefaultProviders(cfg) {
		return nil, nil
	}

	providers, err := config.ReadProviderCache()
	if err == nil {
		return providers, nil
	}
	slog.Debug("Failed to load catwalk providers from cache, using embedded", "error", err)
	return Embedded(), nil
}

// Providers is an alias for ProvidersWithCache kept for historical call sites.
// Network updates are handled asynchronously via UpdateProvidersAsync.
func Providers(cfg *config.Config) ([]config.Provider, error) {
	return ProvidersWithCache(cfg)
}

// UpdateProviders refreshes the cached provider catalog from a file, a URL, or
// the embedded catalog ("embedded").
func UpdateProviders(pathOrURL string) error {
	pathOrURL = cmp.Or(pathOrURL, os.Getenv("CATWALK_URL"), defaultCatalogURL)

	providers, err := loadProviders(pathOrURL)
	if err != nil {
		return err
	}

	if err := config.WriteProviderCache(providers); err != nil {
		return fmt.Errorf("failed to save providers to cache: %w", err)
	}

	slog.Info("Providers updated successfully", "count", len(providers), "from", pathOrURL, "to", config.ProviderCachePath())
	return nil
}

// loadProviders resolves the catalog from its configured source.
func loadProviders(pathOrURL string) ([]config.Provider, error) {
	switch {
	case pathOrURL == "embedded":
		return Embedded(), nil
	case strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://"):
		fetched, err := catwalk.NewWithURL(pathOrURL).GetProviders(context.Background(), "")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch providers from Catwalk: %w", err)
		}
		return ToConfigProviders(fetched), nil
	default:
		content, err := os.ReadFile(pathOrURL) //nolint:gosec // cache path derives from the internal provider name.
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		var providers []config.Provider
		if err := json.Unmarshal(content, &providers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal provider data: %w", err)
		}
		if len(providers) == 0 {
			return nil, fmt.Errorf("no providers found in the provided source")
		}
		return providers, nil
	}
}

// UpdateProvidersAsync refreshes the provider catalog in the background
// without blocking startup.
func UpdateProvidersAsync(cfg *config.Config) {
	go func() {
		if cfg != nil && cfg.Options != nil && cfg.Options.DisableProviderAutoUpdate {
			return
		}
		if disablesDefaultProviders(cfg) {
			return
		}

		slog.Info("Starting async provider update")
		if err := UpdateProviders(""); err != nil {
			slog.Warn("Async provider update failed", "error", err)
		}
	}()
}

// disablesDefaultProviders reports whether the config opts out of the default
// provider catalog.
func disablesDefaultProviders(cfg *config.Config) bool {
	return cfg != nil && cfg.Options != nil && cfg.Options.DisableDefaultProviders
}
