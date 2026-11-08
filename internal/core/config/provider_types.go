package config

// Provider types and provider catalog entries used by the configuration.
//
// These are local mirrors of the wire format served by the Catwalk catalog.
// Keeping them in this package lets configuration types stay independent of
// the catalog client; the conversion from Catwalk's own types lives in
// internal/core/catwalkconv. Field names and JSON tags must stay aligned with
// that wire format (the config file and the provider cache are both persisted
// as JSON).

// ProviderType is the API flavour of a provider, e.g. "openai" or
// "anthropic".
type ProviderType string

// Supported provider API flavours. Values match the wire format.
const (
	ProviderTypeOpenAI       ProviderType = "openai"
	ProviderTypeOpenAICompat ProviderType = "openai-compat"
	ProviderTypeOpenRouter   ProviderType = "openrouter"
	ProviderTypeVercel       ProviderType = "vercel"
	ProviderTypeAnthropic    ProviderType = "anthropic"
	ProviderTypeGoogle       ProviderType = "google"
	ProviderTypeAzure        ProviderType = "azure"
	ProviderTypeBedrock      ProviderType = "bedrock"
	ProviderTypeVertexAI     ProviderType = "google-vertex"
)

// KnownProviderTypes lists every supported provider API flavour.
func KnownProviderTypes() []ProviderType {
	return []ProviderType{
		ProviderTypeOpenAI,
		ProviderTypeOpenAICompat,
		ProviderTypeOpenRouter,
		ProviderTypeVercel,
		ProviderTypeAnthropic,
		ProviderTypeGoogle,
		ProviderTypeAzure,
		ProviderTypeBedrock,
		ProviderTypeVertexAI,
	}
}

// InferenceProvider identifies a provider in the catalog, e.g. "anthropic".
// Values match the wire format.
type InferenceProvider string

// Provider identifiers referenced by the configuration logic. The catalog
// itself carries many more; these are the ones whose handling is special.
const (
	InferenceProviderAnthropic    InferenceProvider = "anthropic"
	InferenceProviderAzure        InferenceProvider = "azure"
	InferenceProviderBedrock      InferenceProvider = "bedrock"
	InferenceProviderVertexAI     InferenceProvider = "vertexai"
	InferenceProviderMiniMax      InferenceProvider = "minimax"
	InferenceProviderMiniMaxChina InferenceProvider = "minimax-china"
	InferenceProviderOpenRouter   InferenceProvider = "openrouter"
	InferenceProviderOpenCodeGo   InferenceProvider = "opencode-go"
	InferenceProviderIoNet        InferenceProvider = "ionet"
	InferenceProviderKimiCoding   InferenceProvider = "kimi-coding"
	InferenceProviderZAI          InferenceProvider = "zai"
)

// ModelOptions holds provider-specific sampling options for a model.
type ModelOptions struct {
	Temperature      *float64       `json:"temperature,omitempty"`
	TopP             *float64       `json:"top_p,omitempty"`
	TopK             *int64         `json:"top_k,omitempty"`
	FrequencyPenalty *float64       `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64       `json:"presence_penalty,omitempty"`
	ProviderOptions  map[string]any `json:"provider_options,omitempty"`
}

// Model describes a model entry in the provider catalog. It mirrors the
// catalog wire format, so JSON round-trips stay compatible.
type Model struct {
	ID                     string       `json:"id"`
	Name                   string       `json:"name"`
	CostPer1MIn            float64      `json:"cost_per_1m_in"`
	CostPer1MOut           float64      `json:"cost_per_1m_out"`
	CostPer1MInCached      float64      `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64      `json:"cost_per_1m_out_cached"`
	ContextWindow          int64        `json:"context_window"`
	DefaultMaxTokens       int64        `json:"default_max_tokens"`
	CanReason              bool         `json:"can_reason"`
	ReasoningLevels        []string     `json:"reasoning_levels,omitempty"`
	DefaultReasoningEffort string       `json:"default_reasoning_effort,omitempty"`
	SupportsImages         bool         `json:"supports_attachments"`
	Options                ModelOptions `json:"options,omitzero"`
}

// Provider is a provider catalog entry. It mirrors the catalog wire format so
// cache files written by earlier versions keep loading.
type Provider struct {
	Name                string            `json:"name"`
	ID                  InferenceProvider `json:"id"`
	APIKey              string            `json:"api_key,omitempty"`
	APIEndpoint         string            `json:"api_endpoint,omitempty"`
	Type                ProviderType      `json:"type,omitempty"`
	DefaultLargeModelID string            `json:"default_large_model_id,omitempty"`
	DefaultSmallModelID string            `json:"default_small_model_id,omitempty"`
	Models              []Model           `json:"models,omitempty"`
	DefaultHeaders      map[string]string `json:"default_headers,omitempty"`
}
