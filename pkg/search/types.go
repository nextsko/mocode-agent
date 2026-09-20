// Package search defines the web search provider chain: unified request and
// response DTOs, the ProviderV2 contract, a lazy provider Registry, a
// fallback Chain executor, HTTP error classification, and the
// CredentialBroker credential contract.
package search

// SearchParams carries one web search request through every provider in the
// chain. The HTTP client is intentionally absent: providers receive their
// transport via constructor injection, keeping params pure data.
type SearchParams struct {
	// Query is the raw user query.
	Query string `json:"query"`
	// Limit caps the number of returned sources.
	Limit int `json:"limit,omitempty"`
	// Recency is a pure time filter: "day", "week", "month", "year", or "".
	// Providers without temporal filtering must ignore it silently.
	Recency string `json:"recency,omitempty"`
	// SystemPrompt steers LLM-mediated answer synthesis.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// MaxOutputTokens bounds LLM-mediated answer synthesis.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	// NumSearchResults requests a result count from native search APIs.
	NumSearchResults int `json:"num_search_results,omitempty"`
	// Temperature controls LLM-mediated answer generation.
	Temperature float64 `json:"temperature,omitempty"`
	// AuthStorage is the only credential source providers may consult.
	AuthStorage CredentialBroker `json:"-"`
	// SessionID scopes credential round-robin and sticky selection.
	SessionID string `json:"session_id,omitempty"`
}

// SearchResponse is the unified provider response consumed by the chain and
// its callers.
type SearchResponse struct {
	// Provider is the responding provider ID, or "none".
	Provider string `json:"provider"`
	// Answer is the synthesized answer text (LLM-mediated providers).
	Answer string `json:"answer,omitempty"`
	// Sources lists the search result entries.
	Sources []SearchSource `json:"sources,omitempty"`
	// Citations lists text references with context.
	Citations []SearchCitation `json:"citations,omitempty"`
	// SearchQueries lists intermediate queries (e.g. Anthropic).
	SearchQueries []string `json:"search_queries,omitempty"`
	// RelatedQuestions lists provider-suggested follow-ups.
	RelatedQuestions []string `json:"related_questions,omitempty"`
	// Usage reports token and search request metrics.
	Usage *SearchUsage `json:"usage,omitempty"`
	// Model names the model that produced the answer.
	Model string `json:"model,omitempty"`
	// RequestID identifies the upstream request for debugging.
	RequestID string `json:"request_id,omitempty"`
	// AuthMode reports the authentication mode used (e.g. "oauth",
	// "api_key"), or "" when unauthenticated.
	AuthMode string `json:"auth_mode,omitempty"`
}

// SearchSource is one search result entry.
type SearchSource struct {
	// Title is the result headline.
	Title string `json:"title"`
	// URL is the result location.
	URL string `json:"url"`
	// Snippet is the excerpt shown to the user.
	Snippet string `json:"snippet,omitempty"`
	// Author is the content author when known.
	Author string `json:"author,omitempty"`
	// PublishedDate is an ISO date or relative label (e.g. "2d ago").
	PublishedDate string `json:"published_date,omitempty"`
	// AgeSeconds is the result age in seconds for formatting.
	AgeSeconds int64 `json:"age_seconds,omitempty"`
}

// SearchCitation is a text reference with surrounding context.
type SearchCitation struct {
	// URL is the cited location.
	URL string `json:"url"`
	// Title is the cited page headline.
	Title string `json:"title"`
	// CitedText is the referenced passage.
	CitedText string `json:"cited_text,omitempty"`
}

// SearchUsage reports provider-side consumption metrics.
type SearchUsage struct {
	// InputTokens counts prompt tokens.
	InputTokens int64 `json:"input_tokens,omitempty"`
	// OutputTokens counts completion tokens.
	OutputTokens int64 `json:"output_tokens,omitempty"`
	// SearchRequests counts upstream web search requests.
	SearchRequests int64 `json:"search_requests,omitempty"`
	// TotalTokens counts combined tokens when the provider reports one.
	TotalTokens int64 `json:"total_tokens,omitempty"`
}
