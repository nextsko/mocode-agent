package grep_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/grep"
)

type testContext struct {
	workingDir string
}

func (c testContext) SessionID() string { return "session-1" }

func (c testContext) WorkingDir() string { return c.workingDir }

func (c testContext) Permissions() tools.PermissionChecker { return nil }

func (c testContext) MCP() tools.MCPHandles { return nil }

func (c testContext) Callbacks() tools.ToolCallbacks { return nil }

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(grep.GrepToolName)
	require.True(t, ok)
	require.Equal(t, grep.GrepToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, grep.GrepToolName, tool.Schema().Name)
}

func TestExecuteMissingPattern(t *testing.T) {
	t.Parallel()

	result, err := grep.New().Execute(context.Background(), testContext{}, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "pattern is required")
}

func TestExecuteSearchesFilesFromContextWorkingDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "one.go"), []byte("package main\nfunc target() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "two.txt"), []byte("target text\n"), 0o644))

	result, err := grep.New().Execute(context.Background(), testContext{workingDir: dir}, json.RawMessage(`{"pattern":"target","include":"*.go"}`))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Contains(t, result.Content, "Found 1 matches")
	require.Contains(t, result.Content, "src/one.go")
	require.NotContains(t, result.Content, "src/two.txt")
	require.Equal(t, 1, result.Metadata["number_of_matches"])
	require.Equal(t, false, result.Metadata["truncated"])
}

func TestExecuteSupportsLiteralText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pattern.txt"), []byte("a.b\naxb\n"), 0o644))

	result, err := grep.New().Execute(context.Background(), testContext{workingDir: dir}, json.RawMessage(`{"pattern":"a.b","literal_text":true}`))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Contains(t, result.Content, "Found 1 matches")
	require.Contains(t, result.Content, "a.b")
	require.NotContains(t, result.Content, "axb")
}
