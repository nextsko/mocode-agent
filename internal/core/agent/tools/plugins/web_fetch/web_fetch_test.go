package web_fetch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/core/agent/tools/plugins/netcommon"
)

// TestNewWebFetchTool_EmptyURLReturnsError verifies the tool validates its
// input before attempting a network fetch — an empty URL is a client error
// returned as a tool error response, not a network attempt.
func TestNewWebFetchTool_EmptyURLReturnsError(t *testing.T) {
	tool := NewWebFetchTool(t.TempDir(), nil)
	require.Equal(t, netcommon.WebFetchToolName, tool.Info().Name)

	input, _ := json.Marshal(netcommon.WebFetchParams{URL: ""})
	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test",
		Name:  netcommon.WebFetchToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.Contains(t, resp.Content, "url is required")
}

// TestNewWebFetchTool_NilClientDoesNotPanic confirms a nil injected client
// falls back to the default HTTP client without panicking at construction.
func TestNewWebFetchTool_NilClientDoesNotPanic(t *testing.T) {
	tool := NewWebFetchTool(t.TempDir(), nil)
	require.NotNil(t, tool)
}
