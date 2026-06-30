package netcommon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type Provider interface {
	Name() string
	Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error)
}

type DuckDuckGoProvider struct{}

func (DuckDuckGoProvider) Name() string { return "duckduckgo" }

func (DuckDuckGoProvider) Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error) {
	return SearchDuckDuckGo(ctx, client, query, maxResults)
}

type DuckDuckGoInstantAnswerProvider struct {
	BaseURL string
}

func (p DuckDuckGoInstantAnswerProvider) Name() string { return "duckduckgo-ia" }

const ddgIAEndpoint = "https://api.duckduckgo.com"

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

func (p DuckDuckGoInstantAnswerProvider) Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	base := p.BaseURL
	if base == "" {
		base = ddgIAEndpoint
	}
	endpoint := fmt.Sprintf("%s/?q=%s&format=json&no_html=1&no_redirect=1&skip_disambig=1", base, url.QueryEscape(query))

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
	if ia.AbstractText != "" {
		results = append(results, SearchResult{Title: firstSentence(ia.AbstractText), Link: ia.AbstractURL, Snippet: ia.AbstractText})
	} else if ia.Answer != "" {
		results = append(results, SearchResult{Title: query, Snippet: ia.Answer})
	} else if ia.Definition != "" {
		results = append(results, SearchResult{Title: query, Link: ia.DefinitionURL, Snippet: ia.Definition})
	}
	for _, t := range ia.RelatedTopics {
		if len(results) >= maxResults {
			break
		}
		if t.Text == "" || t.FirstURL == "" {
			continue
		}
		results = append(results, SearchResult{Title: firstSentence(t.Text), Link: t.FirstURL, Snippet: t.Text})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no instant-answer results for %q", query)
	}
	return results, nil
}

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

type MultiProvider struct {
	Providers []Provider
}

func (m MultiProvider) Name() string {
	if len(m.Providers) == 0 {
		return "multi(empty)"
	}
	return m.Providers[0].Name()
}

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

func DefaultSearchProvider() Provider {
	return MultiProvider{Providers: []Provider{DuckDuckGoProvider{}, DuckDuckGoInstantAnswerProvider{}}}
}
