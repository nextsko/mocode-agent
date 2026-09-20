package zai

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// searchEngineValue names the search-prime engine on the third attempt.
const searchEngineValue = "search-prime"

// buildAttempts returns the three argument shapes the upstream endpoint
// accepts across its schema generations, in fallback order: the plain
// query form, the search_query rename, and the fully qualified form.
func buildAttempts(q string, count int) []map[string]any {
	return []map[string]any{
		{"query": q, "count": count},
		{"search_query": q, "count": count},
		{
			"search_query":  q,
			"search_engine": searchEngineValue,
			"count":         count,
		},
	}
}

// isArgumentError reports whether err looks like an argument rejection
// worth retrying with the next argument shape. Only provider errors
// qualify: an HTTP 400 or a message mentioning invalid arguments or the
// query parameter names, as in the TypeScript port. Auth, billing, and
// transport failures surface immediately instead of burning retries.
func isArgumentError(err error) bool {
	var spe *search.SearchProviderError
	if !errors.As(err, &spe) {
		return false
	}
	if spe.HTTPStatus == 400 {
		return true
	}
	message := strings.ToLower(spe.Message)
	for _, keyword := range []string{
		"invalid", "argument", "search_query", "query",
	} {
		if strings.Contains(message, keyword) {
			return true
		}
	}
	return false
}

// callWebSearchPrime drives the three-stage argument retry. Each failed
// attempt only degrades to the next shape when the failure looks like
// an argument rejection; the last attempt's error (or any non-argument
// error) is returned as-is.
func (p *Provider) callWebSearchPrime(
	ctx context.Context,
	apiKey string,
	in searchInput,
) (*mcp.CallToolResult, error) {
	attempts := buildAttempts(in.query, in.numResults)
	var lastErr error
	for i, args := range attempts {
		result, err := p.callTool(ctx, apiKey, args)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i == len(attempts)-1 {
			break
		}
		if !isArgumentError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}
