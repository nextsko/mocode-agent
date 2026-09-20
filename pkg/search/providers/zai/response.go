package zai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// zaiResult is one entry of the Z.AI search payload. Field names cover
// both spellings the endpoint emits.
type zaiResult struct {
	Title         string `json:"title"`
	Content       string `json:"content"`
	Link          string `json:"link"`
	URL           string `json:"url"`
	Media         string `json:"media"`
	PublishDate   string `json:"publish_date"`
	PublishedDate string `json:"publishedDate"`
}

// zaiPayload is the outcome of the layered extraction over one tool
// result.
type zaiPayload struct {
	Results   []zaiResult
	Answer    string
	RequestID string
}

// contentTexts returns the trimmed text of the result's non-empty text
// content blocks.
func contentTexts(result *mcp.CallToolResult) []string {
	texts := make([]string, 0, len(result.Content))
	for _, item := range result.Content {
		tc, ok := item.(*mcp.TextContent)
		if !ok {
			continue
		}
		if text := strings.TrimSpace(tc.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// parseSearchPayload recovers search results from whichever layer of
// the tool result carries them, mirroring the TypeScript port's
// candidate walk: StructuredContent first (after the Z.AI direct-error
// check), then its nested data/result keys, then the JSON documents
// embedded in content text blocks (with double-encoded strings decoded
// one level further). Text parts that yield no results are joined with
// blank lines into the answer; a candidate object contributes its
// request id. An empty payload is a valid outcome, never an error.
func parseSearchPayload(result *mcp.CallToolResult) (*zaiPayload, error) {
	var candidates []any
	var textParts []string

	if structured := result.StructuredContent; structured != nil {
		if obj, ok := structured.(map[string]any); ok {
			if err := zaiDirectError(obj); err != nil {
				return nil, err
			}
			candidates = append(candidates, obj)
			for _, key := range []string{"data", "result"} {
				if nested, ok := obj[key]; ok {
					candidates = append(candidates, nested)
				}
			}
		} else {
			candidates = append(candidates, structured)
		}
	}

	for _, text := range contentTexts(result) {
		parsed, ok := parseTextJSON(text)
		if !ok {
			textParts = append(textParts, text)
			continue
		}
		candidates = append(candidates, parsed)
		if len(searchResultsOf(parsed)) == 0 {
			textParts = append(textParts, text)
		}
	}

	payload := &zaiPayload{Answer: strings.Join(textParts, "\n\n")}
	for _, candidate := range candidates {
		results := searchResultsOf(candidate)
		if len(results) == 0 {
			continue
		}
		payload.Results = results
		payload.RequestID = candidateRequestID(candidate)
		return payload, nil
	}
	return payload, nil
}

// zaiDirectError inspects a structured payload for the non-JSON-RPC
// error envelope Z.AI sometimes returns ({code, success: false, msg})
// and turns it into a provider error, mirroring the TypeScript port's
// readJsonRpcPayload check.
func zaiDirectError(obj map[string]any) error {
	if success, ok := obj["success"].(bool); !ok || success {
		return nil
	}
	message := ""
	for _, key := range []string{"msg", "message", "error_message"} {
		if s, _ := obj[key].(string); asNonEmpty(s) != "" {
			message = asNonEmpty(s)
			break
		}
	}
	if message == "" {
		return nil
	}
	code, _ := obj["code"].(float64)
	text := "Z.AI API error: " + message
	if code != 0 {
		text = fmt.Sprintf("Z.AI API error (%s): %s",
			strconv.FormatFloat(code, 'f', -1, 64), message)
	}
	return &search.SearchProviderError{
		Provider:   "zai",
		Message:    text,
		HTTPStatus: int(code),
	}
}

// parseTextJSON decodes a content text block as JSON, decoding one more
// level when the result is itself a JSON string (the endpoint's double
// encoding). It reports false when the text is not JSON at all.
func parseTextJSON(text string) (any, bool) {
	if !json.Valid([]byte(text)) {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, false
	}
	if s, ok := parsed.(string); ok {
		if json.Valid([]byte(s)) {
			var inner any
			if err := json.Unmarshal([]byte(s), &inner); err == nil {
				return inner, true
			}
		}
	}
	return parsed, true
}

// searchResultsOf reads the results array off a candidate value: a bare
// array, or an object exposing search_result (the endpoint's primary
// field) or results. It returns nil when nothing matches.
func searchResultsOf(value any) []zaiResult {
	switch v := value.(type) {
	case []any:
		return decodeResults(v)
	case map[string]any:
		for _, key := range []string{"search_result", "results"} {
			if arr, ok := v[key].([]any); ok {
				return decodeResults(arr)
			}
		}
	}
	return nil
}

// decodeResults converts a raw JSON array onto zaiResult entries,
// skipping elements that do not decode, mirroring the TypeScript port's
// per-field tolerance.
func decodeResults(arr []any) []zaiResult {
	results := make([]zaiResult, 0, len(arr))
	for _, item := range arr {
		data, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var out zaiResult
		if err := json.Unmarshal(data, &out); err != nil {
			continue
		}
		results = append(results, out)
	}
	if len(results) == 0 {
		return nil
	}
	return results
}

// candidateRequestID reads the request identifier off the winning
// candidate, tolerating every spelling the endpoint emits.
func candidateRequestID(value any) string {
	obj, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"request_id", "requestId", "id"} {
		if s, _ := obj[key].(string); asNonEmpty(s) != "" {
			return asNonEmpty(s)
		}
	}
	return ""
}

// toSources maps payload results onto unified sources: link wins over
// url and entries without either are dropped, media surfaces as the
// author, and publish_date/publishedDate feed the date fields.
func toSources(results []zaiResult) []search.SearchSource {
	sources := make([]search.SearchSource, 0, len(results))
	for _, r := range results {
		link := asNonEmpty(r.Link)
		if link == "" {
			link = asNonEmpty(r.URL)
		}
		if link == "" {
			continue
		}
		title := asNonEmpty(r.Title)
		if title == "" {
			title = link
		}
		published := asNonEmpty(r.PublishDate)
		if published == "" {
			published = asNonEmpty(r.PublishedDate)
		}
		sources = append(sources, search.SearchSource{
			Title:         title,
			URL:           link,
			Snippet:       asNonEmpty(r.Content),
			Author:        asNonEmpty(r.Media),
			PublishedDate: published,
			AgeSeconds:    dateToAgeSeconds(published),
		})
	}
	return sources
}

// dateLayouts lists the publish-date formats the endpoint emits.
var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// dateToAgeSeconds converts a publish date to its age in seconds,
// returning 0 for absent or unparseable dates.
func dateToAgeSeconds(date string) int64 {
	if date == "" {
		return 0
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, date); err == nil {
			return int64(time.Since(t) / time.Second)
		}
	}
	return 0
}
