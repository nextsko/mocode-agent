package exa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// restSummary requests per-result summaries steered by the query.
type restSummary struct {
	Query string `json:"query"`
}

// restContents is the contents block of the REST body.
type restContents struct {
	Summary restSummary `json:"summary"`
}

// restRequestBody is the POST body of the REST search path.
type restRequestBody struct {
	Query              string       `json:"query"`
	NumResults         int          `json:"numResults"`
	Type               string       `json:"type"`
	Contents           restContents `json:"contents"`
	IncludeDomains     []string     `json:"includeDomains,omitempty"`
	ExcludeDomains     []string     `json:"excludeDomains,omitempty"`
	StartPublishedDate string       `json:"startPublishedDate,omitempty"`
	EndPublishedDate   string       `json:"endPublishedDate,omitempty"`
}

// buildRESTBody assembles the REST request body. The result count
// defaults to ten; the search type is always "auto" because the provider
// accepts no caller preference for it.
func buildRESTBody(in searchInput) restRequestBody {
	num := in.numResults
	if num <= 0 {
		num = defaultNumResults
	}
	body := restRequestBody{
		Query:      in.query,
		NumResults: num,
		Type:       "auto",
		Contents:   restContents{Summary: restSummary{Query: in.query}},
	}
	if len(in.includeDomains) > 0 {
		body.IncludeDomains = in.includeDomains
	}
	if len(in.excludeDomains) > 0 {
		body.ExcludeDomains = in.excludeDomains
	}
	body.StartPublishedDate = in.startPublishedDate
	body.EndPublishedDate = in.endPublishedDate
	return body
}

// callREST executes the authenticated REST search against api.exa.ai.
// HTTP failures surface as classified *search.SearchProviderError values.
func (p *Provider) callREST(
	ctx context.Context,
	apiKey string,
	in searchInput,
) (*exaSearchResponse, error) {
	payload, err := json.Marshal(buildRESTBody(in))
	if err != nil {
		return nil, fmt.Errorf("exa: encode REST body: %w", err)
	}
	if err := waitForSearchSlot(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.restURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("exa: build REST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exa: REST request: %w", err)
	}
	defer resp.Body.Close()
	body, err := readAll(resp)
	if err != nil {
		return nil, fmt.Errorf("exa: read REST response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if spe := classifyRESTHTTP(resp.StatusCode, string(body)); spe != nil {
			return nil, spe
		}
		return nil, searchError(resp.StatusCode,
			fmt.Sprintf("Exa API error (%d): %s", resp.StatusCode, string(body)))
	}
	var out exaSearchResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("exa: decode REST response: %w", err)
	}
	return &out, nil
}
