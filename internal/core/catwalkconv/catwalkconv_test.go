package catwalkconv

import (
	"encoding/json"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/config"
)

func TestToConfigProviderRoundTripsWireShape(t *testing.T) {
	t.Parallel()

	src := catwalk.Provider{
		Name:                "OpenAI",
		ID:                  catwalk.InferenceProvider("openai"),
		APIKey:              "sk-test",
		APIEndpoint:         "https://api.openai.com/v1",
		Type:                catwalk.TypeOpenAI,
		DefaultLargeModelID: "gpt-4",
		DefaultSmallModelID: "gpt-4o-mini",
		DefaultHeaders:      map[string]string{"X-Test": "1"},
		Models: []catwalk.Model{{
			ID:                     "gpt-4",
			Name:                   "GPT-4",
			CostPer1MIn:            10,
			CostPer1MOut:           30,
			CostPer1MInCached:      2.5,
			CostPer1MOutCached:     7.5,
			ContextWindow:          128000,
			DefaultMaxTokens:       4096,
			CanReason:              true,
			ReasoningLevels:        []string{"low", "high"},
			DefaultReasoningEffort: "high",
			SupportsImages:         true,
			Options: catwalk.ModelOptions{
				Temperature:     float64Ptr(0.2),
				ProviderOptions: map[string]any{"reasoning": true},
			},
		}},
	}

	got := ToConfigProvider(src)

	require.Equal(t, config.InferenceProvider("openai"), got.ID)
	require.Equal(t, config.ProviderType("openai"), got.Type)
	require.Equal(t, "sk-test", got.APIKey)
	require.Equal(t, "https://api.openai.com/v1", got.APIEndpoint)
	require.Equal(t, "gpt-4", got.DefaultLargeModelID)
	require.Equal(t, map[string]string{"X-Test": "1"}, got.DefaultHeaders)
	require.Len(t, got.Models, 1)

	model := got.Models[0]
	require.Equal(t, "gpt-4", model.ID)
	require.Equal(t, int64(128000), model.ContextWindow)
	require.Equal(t, int64(4096), model.DefaultMaxTokens)
	require.True(t, model.CanReason)
	require.True(t, model.SupportsImages)
	require.Equal(t, []string{"low", "high"}, model.ReasoningLevels)
	require.Equal(t, "high", model.DefaultReasoningEffort)
	require.NotNil(t, model.Options.Temperature)
	require.Equal(t, 0.2, *model.Options.Temperature)
	require.Equal(t, map[string]any{"reasoning": true}, model.Options.ProviderOptions)
}

// TestProviderJSONMatchesCatalogWireFormat guards provider-cache compatibility:
// a catalog entry and its config-owned mirror must marshal to the same JSON, so
// cache files written by either side keep loading.
func TestProviderJSONMatchesCatalogWireFormat(t *testing.T) {
	t.Parallel()

	src := catwalk.Provider{
		Name:                "OpenAI",
		ID:                  catwalk.InferenceProvider("openai"),
		APIKey:              "sk-test",
		APIEndpoint:         "https://api.openai.com/v1",
		Type:                catwalk.TypeOpenAI,
		DefaultLargeModelID: "gpt-4",
		DefaultSmallModelID: "gpt-4o-mini",
		DefaultHeaders:      map[string]string{"X-Test": "1"},
		Models: []catwalk.Model{{
			ID:                     "gpt-4",
			Name:                   "GPT-4",
			CostPer1MIn:            10,
			CostPer1MOut:           30,
			CostPer1MInCached:      2.5,
			CostPer1MOutCached:     7.5,
			ContextWindow:          128000,
			DefaultMaxTokens:       4096,
			CanReason:              true,
			ReasoningLevels:        []string{"low", "high"},
			DefaultReasoningEffort: "high",
			SupportsImages:         true,
			Options: catwalk.ModelOptions{
				Temperature:     float64Ptr(0.2),
				TopP:            float64Ptr(0.9),
				ProviderOptions: map[string]any{"reasoning": true},
			},
		}},
	}

	want, err := json.Marshal([]catwalk.Provider{src})
	require.NoError(t, err)
	got, err := json.Marshal([]config.Provider{ToConfigProvider(src)})
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(got))
}

func TestProviders_FallsBackToEmbedded(t *testing.T) {
	// The cache path derives from XDG_DATA_HOME, so point it at an empty dir.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	providers, err := Providers(&config.Config{Options: &config.Options{}})
	require.NoError(t, err)
	require.Greater(t, len(providers), 5, "expected the embedded catalog")

	// Every entry must carry the wire-compatible ID and at least one model.
	for _, p := range providers {
		require.NotEmpty(t, p.ID)
		require.NotEmpty(t, p.Models)
	}
}

func TestProviders_DisabledDefaultProviders(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	providers, err := Providers(&config.Config{
		Options: &config.Options{DisableDefaultProviders: true},
	})
	require.NoError(t, err)
	require.Empty(t, providers)
}

func TestUpdateProviders_EmbeddedWritesCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	require.NoError(t, UpdateProviders("embedded"))

	cached, err := config.ReadProviderCache()
	require.NoError(t, err)
	require.Greater(t, len(cached), 5)

	// The cache round-trips through the config-owned type: the same IDs come
	// back when the cache is used as the source.
	fromCache, err := Providers(&config.Config{Options: &config.Options{}})
	require.NoError(t, err)
	require.Equal(t, len(cached), len(fromCache))
}

func float64Ptr(v float64) *float64 { return &v }
