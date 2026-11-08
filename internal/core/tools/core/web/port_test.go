package web

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	netcommon "github.com/nextsko/mocode-agent/internal/core/tools/external/common/net"
)

// portCase pairs a migrated port with the schema contract callers rely on.
type portCase struct {
	name     string
	tool     contract.Tool
	required []string
	types    map[string]string
}

func portCases(t *testing.T) []portCase {
	t.Helper()
	dir := t.TempDir()
	return []portCase{
		{
			name:     FetchToolName,
			tool:     NewFetchPortTool(nil, dir, nil),
			required: []string{"url", "format"},
			types:    map[string]string{"url": "string", "format": "string", "timeout": "integer"},
		},
		{
			name:     CrawlToolName,
			tool:     NewCrawlPortTool(),
			required: []string{"url"},
			types:    map[string]string{"url": "string", "max_depth": "integer", "max_pages": "integer"},
		},
		{
			name:     DownloadToolName,
			tool:     NewDownloadPortTool(nil, dir, nil),
			required: []string{"url", "file_path"},
			types:    map[string]string{"url": "string", "file_path": "string", "timeout": "integer"},
		},
		{
			name:     DownloadDocsToolName,
			tool:     NewDownloadDocsPortTool(nil),
			required: []string{"repo_url"},
			types:    map[string]string{"repo_url": "string", "docs_path": "string", "max_files": "integer"},
		},
		{
			name:     SourcegraphToolName,
			tool:     NewSourcegraphPortTool(nil),
			required: []string{"query"},
			types: map[string]string{
				"query":          "string",
				"count":          "integer",
				"context_window": "integer",
				"timeout":        "integer",
			},
		},
		{
			name:     netcommon.WebFetchToolName,
			tool:     NewWebFetchPortTool(dir, nil),
			required: []string{"url"},
			types:    map[string]string{"url": "string"},
		},
		{
			name:     netcommon.WebSearchToolName,
			tool:     NewWebSearchPortTool(nil),
			required: []string{"query"},
			types:    map[string]string{"query": "string", "max_results": "integer"},
		},
	}
}

// TestPortTools_Info pins name, description, parallel-safety and the argument
// schema of every migrated tool.
func TestPortTools_Info(t *testing.T) {
	t.Parallel()

	for _, tc := range portCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			info := asAgentTool(tc.tool).Info()
			require.Equal(t, tc.name, info.Name)
			require.NotEmpty(t, info.Description)
			require.True(t, info.Parallel, "web tools must stay safe to run in parallel")
			require.ElementsMatch(t, tc.required, info.Required)

			for field, wantType := range tc.types {
				spec, ok := info.Parameters[field].(map[string]any)
				require.True(t, ok, "missing schema property %q", field)
				require.Equal(t, wantType, spec["type"], "wrong type for %q", field)
				require.NotEmpty(t, spec["description"], "missing description for %q", field)
			}
			for _, field := range tc.required {
				require.Contains(t, info.Parameters, field)
			}
		})
	}
}

// TestPortTools_ValidationErrors exercises each tool's local pre-flight checks.
// All of them reject bad input before any network or permission work happens,
// so this test is fully offline.
func TestPortTools_ValidationErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tests := []struct {
		name  string
		tool  contract.Tool
		input string
		want  string
	}{
		{"fetch/missing_url", NewFetchPortTool(nil, dir, nil), `{"format":"text"}`, "URL parameter is required"},
		{"fetch/bad_format", NewFetchPortTool(nil, dir, nil), `{"url":"https://example.com","format":"xml"}`, "Format must be one of: text, markdown, html"},
		{"fetch/bad_scheme", NewFetchPortTool(nil, dir, nil), `{"url":"file:///etc/passwd","format":"text"}`, "URL must start with http:// or https://"},
		{"crawl/missing_url", NewCrawlPortTool(), `{}`, "URL parameter is required"},
		{"crawl/bad_scheme", NewCrawlPortTool(), `{"url":"ftp://example.com"}`, "URL must start with http:// or https://"},
		{"download/missing_url", NewDownloadPortTool(nil, dir, nil), `{"file_path":"out.bin"}`, "URL parameter is required"},
		{"download/missing_file_path", NewDownloadPortTool(nil, dir, nil), `{"url":"https://example.com/a"}`, "file_path parameter is required"},
		{"download/bad_scheme", NewDownloadPortTool(nil, dir, nil), `{"url":"file:///etc/passwd","file_path":"out.bin"}`, "URL must start with http:// or https://"},
		{"download_docs/missing_repo_url", NewDownloadDocsPortTool(nil), `{}`, "repo_url parameter is required"},
		{"download_docs/non_github", NewDownloadDocsPortTool(nil), `{"repo_url":"https://gitlab.com/x/y"}`, "Only GitHub repositories are supported"},
		{"sourcegraph/missing_query", NewSourcegraphPortTool(nil), `{}`, "Query parameter is required"},
		{"web_fetch/missing_url", NewWebFetchPortTool(dir, nil), `{}`, "url is required"},
		{"web_search/missing_query", NewWebSearchPortTool(nil), `{}`, "query is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp, err := asAgentTool(tc.tool).Run(context.Background(), fantasy.ToolCall{
				ID:    "call-" + tc.name,
				Name:  tc.name,
				Input: tc.input,
			})
			require.NoError(t, err)
			require.True(t, resp.IsError, "expected an error response, got %q", resp.Content)
			require.Contains(t, resp.Content, tc.want)
		})
	}
}

// TestPortTools_InvalidParameters verifies malformed JSON reaches the model as
// a Go error rather than a panic or a silent success.
func TestPortTools_InvalidParameters(t *testing.T) {
	t.Parallel()

	for _, tc := range portCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := asAgentTool(tc.tool).Run(context.Background(), fantasy.ToolCall{
				ID:    "call-bad-json",
				Name:  tc.name,
				Input: `{"url":`,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid parameters")
		})
	}
}

// TestPermissionDeniedResult pins the port form of the legacy
// toolutil.NewPermissionDeniedResponse: an error result that stops the turn.
func TestPermissionDeniedResult(t *testing.T) {
	t.Parallel()

	res := permissionDeniedResult()
	require.Error(t, res.Error)
	require.Equal(t, "User denied permission", res.Error.Error())
	require.True(t, res.StopTurn)
	require.Empty(t, res.Content)
}
