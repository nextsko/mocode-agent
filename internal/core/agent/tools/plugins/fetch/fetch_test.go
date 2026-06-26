package fetch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/core/agent/tools/plugins/netcommon"
)

// These tests cover the input-validation paths of the fetch tool — the pure
// guards that run before any network or permission call. They do not hit the
// network (the validations return before the fetch), so no test server is
// needed.

func runFetch(t *testing.T, params netcommon.FetchParams) fantasy.ToolResponse {
	t.Helper()
	tool := NewFetchTool(nil, t.TempDir(), nil)
	input, _ := json.Marshal(params)
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test",
		Name:  FetchToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	return resp
}

func TestFetch_EmptyURLRejected(t *testing.T) {
	resp := runFetch(t, netcommon.FetchParams{URL: ""})
	require.Contains(t, resp.Content, "URL parameter is required")
}

func TestFetch_InvalidFormatRejected(t *testing.T) {
	// A valid URL with an unsupported format must be rejected before fetch.
	resp := runFetch(t, netcommon.FetchParams{URL: "https://example.com", Format: "xml"})
	require.Contains(t, resp.Content, "Format must be one of")
}

func TestFetch_NonHTTPSchemeRejected(t *testing.T) {
	// Only http/https URLs are allowed; file:// and bare hosts are rejected.
	resp := runFetch(t, netcommon.FetchParams{URL: "file:///etc/passwd", Format: "text"})
	require.Contains(t, resp.Content, "URL must start with http:// or https://")
}

func TestFetch_ValidURLFormatPassesValidation(t *testing.T) {
	// A well-formed request passes URL/format/scheme validation and reaches
	// the permission check, which fails because no session context is set.
	// This confirms the validation guards let valid input through.
	resp := runFetch(t, netcommon.FetchParams{URL: "https://example.com", Format: "markdown"})
	require.Contains(t, resp.Content, "session ID is required")
}
