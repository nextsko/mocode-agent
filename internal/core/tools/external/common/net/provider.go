package netcommon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Provider is the abstraction over a web search backend. Implementations fetch
// results for a query and return up to maxResults of them.
//
// The provider abstraction is adapted from trpc-agent-go's separation of
// google/duckduckgo into independent search tools, but unified here behind a
// single interface so the web_search plugin can run a fallback chain instead
// of being locked to one backend.
type Provider interface {
	// Name identifies the backend (e.g. "duckduckgo", "duckduckgo-ia").
	Name() string
	// Search runs the query and returns at most maxResults results. A returned
	// error causes the MultiProvider to try the next provider in the chain.
	Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error)
}

// DuckDuckGoProvider wraps the existing HTML-lite scraper as a Provider.
type DuckDuckGoProvider struct{}

// Name implements Provider.
func (DuckDuckGoProvider) Name() string { return "duckduckgo" }

// Search implements Provider by delegating to the existing SearchDuckDuckGo.
func (DuckDuckGoProvider) Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error) {
	return SearchDuckDuckGo(ctx, client, query, maxResults)
}

// DuckDuckGoInstantAnswerProvider queries the DuckDuckGo Instant Answer API
// (https://api.duckduckgo.com). Unlike the HTML scraper, it returns structured
// answers, abstracts, and definitions — better for factual/encyclopedic
// queries, and a useful fallback when the lite HTML endpoint is rate-limited.
// Adapted from trpc-agent-go's duckduckgo tool.
type DuckDuckGoInstantAnswerProvider struct {
	BaseURL string // empty = "https://api.duckduckgo.com"
}

// Name implements Provider.
func (p DuckDuckGoInstantAnswerProvider) Name() string { return "duckduckgo-ia" }

// ddgIAEndpoint is the default Instant Answer API base URL.
const ddgIAEndpoint = "https://api.duckduckgo.com"

// ddgIAResponse models the subset of the Instant Answer API payload we use.
type ddgIAResponse struct {
	Answer         string `json:"Answer"`
	AbstractText   string `json:"AbstractText"`
	AbstractURL    string `json:"AbstractURL"`
	AbstractSource string `json:"AbstractSource"`
	Definition     string `json:"Definition"`
	DefinitionURL  string `json:"DefinitionURL"`
	RelatedTopics  []struct {
		Text     string `json:"text"`
		FirstURL string `json:"FirstURL"`
	} `json:"RelatedTopics"`
}

// Search implements Provider against the Instant Answer API.
func (p DuckDuckGoInstantAnswerProvider) Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	base := p.BaseURL
	if base == "" {
		base = ddgIAEndpoint
	}
	// The API is JSON; skip_html=1 keeps the payload small. no_redirect=1
	// avoids following the bang-redirect for disambiguation pages.
	endpoint := fmt.Sprintf("%s/?q=%s&format=json&no_html=1&no_redirect=1&skip_disambig=1",
		base, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build instant-answer request: %w", err)
	}
	req.Header.Set("User-Agent", "mocode-websearch/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instant-answer request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instant-answer status %d", resp.StatusCode)
	}

	var ia ddgIAResponse
	if err := json.NewDecoder(resp.Body).Decode(&ia); err != nil {
		return nil, fmt.Errorf("decode instant-answer: %w", err)
	}

	var results []SearchResult
	// An abstract or answer is the highest-signal result.
	if ia.AbstractText != "" {
		results = append(results, SearchResult{
			Title:   firstSentence(ia.AbstractText),
			Link:    ia.AbstractURL,
			Snippet: ia.AbstractText,
		})
	} else if ia.Answer != "" {
		results = append(results, SearchResult{
			Title:   query,
			Snippet: ia.Answer,
		})
	} else if ia.Definition != "" {
		results = append(results, SearchResult{
			Title:   query,
			Link:    ia.DefinitionURL,
			Snippet: ia.Definition,
		})
	}
	// Related topics fill the remaining slots.
	for _, t := range ia.RelatedTopics {
		if len(results) >= maxResults {
			break
		}
		if t.Text == "" || t.FirstURL == "" {
			continue
		}
		results = append(results, SearchResult{
			Title:   firstSentence(t.Text),
			Link:    t.FirstURL,
			Snippet: t.Text,
		})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no instant-answer results for %q", query)
	}
	return results, nil
}

// firstSentence extracts the leading sentence of a block to use as a title.
func firstSentence(s string) string {
	for i, r := range s {
		if r == '.' || r == '\n' {
			if i > 0 {
				return s[:i]
			}
		}
		if i >= 80 {
			return s[:i] + "..."
		}
	}
	return s
}

// MultiProvider runs providers in order, returning the first non-empty,
// error-free result set. It is the fallback chain that makes web search
// resilient: if the primary HTML scraper is rate-limited or returns nothing,
// the Instant Answer API is tried next.
type MultiProvider struct {
	Providers []Provider
}

// Name implements Provider.
func (m MultiProvider) Name() string {
	if len(m.Providers) == 0 {
		return "multi(empty)"
	}
	return m.Providers[0].Name()
}

// Search implements Provider by trying each provider in order. A provider is
// skipped on error or an empty result; the first provider that returns at
// least one result wins.
func (m MultiProvider) Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error) {
	if len(m.Providers) == 0 {
		return nil, fmt.Errorf("multi-provider: no providers configured")
	}
	var lastErr error
	for _, p := range m.Providers {
		results, err := p.Search(ctx, client, query, maxResults)
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all search providers failed; last error: %w", lastErr)
	}
	return nil, nil
}

// DefaultSearchProvider returns the provider chain used when none is
// configured: DuckDuckGo HTML scraper first (richest snippets), then the
// DuckDuckGo Instant Answer API as a factual/structured fallback.
func DefaultSearchProvider() Provider {
	return MultiProvider{
		Providers: []Provider{
			DuckDuckGoProvider{},
			DuckDuckGoInstantAnswerProvider{},
		},
	}
}
