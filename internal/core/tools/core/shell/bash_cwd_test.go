package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
)

// allowAllPermissions is a minimal permission.Service stub: every request is
// granted, nothing else is exercised by these tests.
type allowAllPermissions struct {
	permission.Service
}

func (allowAllPermissions) Request(_ context.Context, _ permission.CreatePermissionRequest) (bool, error) {
	return true, nil
}

// runBashCmd executes one bash tool call in the given session via the
// production adapter and returns the result content text.
func runBashCmd(t *testing.T, sessionID string, projectDir string, input map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(input)
	require.NoError(t, err)

	ctx := context.WithValue(t.Context(), toolutil.SessionIDContextKeyVal, sessionID)

	resp, err := asAgentTool(NewBashPortTool(
		allowAllPermissions{}, projectDir, &config.Attribution{}, "test-model",
	)).Run(ctx, fantasy.ToolCall{ID: "call-cwd-1", Input: string(raw)})
	require.NoError(t, err)

	return resp.Content
}

// subInContent extracts the path inside the trailing <cwd>…</cwd> tag.
func cwdInContent(content string) string {
	i := strings.LastIndex(content, "<cwd>")
	if i < 0 {
		return ""
	}
	j := strings.LastIndex(content, "</cwd>")
	if j < i {
		return ""
	}
	return content[i+len("<cwd>") : j]
}

func TestBashSessionCwdPersistence(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	sub := filepath.ToSlash(filepath.Join(project, "sub"))
	require.NoError(t, os.MkdirAll(sub, 0o755))

	// Call 1: cd into sub (needs permission — stub grants it).
	out1 := runBashCmd(t, "sess-cwd-1", project, map[string]any{
		"description": "cd into sub",
		"command":     "cd " + sub + " && pwd",
	})
	require.Contains(t, out1, "sub", "call 1 should report the sub directory")

	// Call 2: no cd, no working_dir — must start where call 1 ended.
	out2 := runBashCmd(t, "sess-cwd-1", project, map[string]any{
		"description": "pwd",
		"command":     "pwd",
	})
	require.Contains(t, out2, "/sub", "call 2 should start in the persisted sub directory")

	// Call 3: relative working_dir resolves against the persisted cwd.
	subsub := filepath.ToSlash(filepath.Join(project, "sub", "deep"))
	require.NoError(t, os.MkdirAll(subsub, 0o755))
	out3 := runBashCmd(t, "sess-cwd-1", project, map[string]any{
		"description": "relative working_dir",
		"command":     "pwd",
		"working_dir": "deep",
	})
	require.Contains(t, out3, "sub/deep", "relative working_dir should resolve against persisted cwd")
}

func TestBashSessionCwdExplicitParamPersists(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	sub := filepath.ToSlash(filepath.Join(project, "sub"))
	require.NoError(t, os.MkdirAll(sub, 0o755))

	out1 := runBashCmd(t, "sess-cwd-2", project, map[string]any{
		"description": "pwd in sub via working_dir",
		"command":     "pwd",
		"working_dir": sub,
	})
	require.Contains(t, out1, "/sub")

	// The explicit working_dir becomes the session cwd for later calls.
	out2 := runBashCmd(t, "sess-cwd-2", project, map[string]any{
		"description": "pwd",
		"command":     "pwd",
	})
	require.Contains(t, out2, "/sub")
}

func TestBashSessionCwdFailedCdKeepsPrevious(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	sub := filepath.ToSlash(filepath.Join(project, "sub"))
	require.NoError(t, os.MkdirAll(sub, 0o755))

	_ = runBashCmd(t, "sess-cwd-3", project, map[string]any{
		"description": "cd into sub",
		"command":     "cd " + sub + " && pwd",
	})

	// cd to a nonexistent dir fails; the session cwd must stay in sub.
	out := runBashCmd(t, "sess-cwd-3", project, map[string]any{
		"description": "cd to nowhere",
		"command":     "cd /nonexistent-dir-xyz-123 && pwd",
	})
	// The command fails (exit != 0) — result carries the error text, and the
	// persisted cwd must still be sub.
	require.Contains(t, out, "nonexistent-dir-xyz-123")

	out2 := runBashCmd(t, "sess-cwd-3", project, map[string]any{
		"description": "pwd",
		"command":     "pwd",
	})
	require.Contains(t, out2, "/sub")
}

func TestBashSessionCwdSessionIsolation(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	sub := filepath.ToSlash(filepath.Join(project, "sub"))
	require.NoError(t, os.MkdirAll(sub, 0o755))

	_ = runBashCmd(t, "sess-iso-a", project, map[string]any{
		"description": "cd into sub",
		"command":     "cd " + sub + " && pwd",
	})

	// A different session must still start at the project dir.
	out := runBashCmd(t, "sess-iso-b", project, map[string]any{
		"description": "pwd",
		"command":     "pwd",
	})
	require.NotContains(t, out, "/sub")
}

func TestBashSessionCwdDeletedDirFallback(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	sub := filepath.ToSlash(filepath.Join(project, "sub"))
	require.NoError(t, os.MkdirAll(sub, 0o755))

	_ = runBashCmd(t, "sess-cwd-del", project, map[string]any{
		"description": "cd into sub",
		"command":     "cd " + sub + " && pwd",
	})

	// Delete the directory out from under the session; the next call must
	// fall back to the project dir instead of failing interpreter setup.
	require.NoError(t, os.RemoveAll(sub))

	out := runBashCmd(t, "sess-cwd-del", project, map[string]any{
		"description": "pwd after deletion",
		"command":     "pwd",
	})
	require.NotContains(t, out, "/sub")
}
