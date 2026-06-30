package web_fetch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/netcommon"
	"github.com/package-register/mocode/tools/plugins/web_fetch"
)

type testContext struct{ workingDir string }

func (c testContext) SessionID() string { return "session-1" }

func (c testContext) WorkingDir() string { return c.workingDir }

func (c testContext) Permissions() tools.PermissionChecker { return nil }

func (c testContext) MCP() tools.MCPHandles { return nil }

func (c testContext) Callbacks() tools.ToolCallbacks { return nil }

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(netcommon.WebFetchToolName)
	require.True(t, ok)
	require.Equal(t, netcommon.WebFetchToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, netcommon.WebFetchToolName, tool.Schema().Name)
}

func TestExecuteEmptyURLReturnsError(t *testing.T) {
	t.Parallel()

	result, err := web_fetch.New(nil).Execute(context.Background(), testContext{workingDir: t.TempDir()}, json.RawMessage(`{"url":""}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "url is required")
}

func TestExecuteFetchesSmallPage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "hello page")
	}))
	defer srv.Close()

	result, err := web_fetch.New(srv.Client()).Execute(context.Background(), testContext{workingDir: t.TempDir()}, json.RawMessage(fmt.Sprintf(`{"url":%q}`, srv.URL)))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Contains(t, result.Content, "Fetched content from")
	require.Contains(t, result.Content, "hello page")
}

func TestExecuteStoresLargePage(t *testing.T) {
	t.Parallel()

	large := strings.Repeat("x", netcommon.LargeContentThreshold+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, large)
	}))
	defer srv.Close()

	result, err := web_fetch.New(srv.Client()).Execute(context.Background(), testContext{workingDir: t.TempDir()}, json.RawMessage(fmt.Sprintf(`{"url":%q}`, srv.URL)))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Contains(t, result.Content, "large page")
	require.Contains(t, result.Content, "Content saved to:")
}
