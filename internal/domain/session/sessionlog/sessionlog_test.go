package sessionlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLogger_HeaderWrittenOnce verifies that the per-file "# Title" header is
// written only when the file is first created, not every time a Logger is
// reopened for an existing category file. Without the size check in getFile,
// each new Logger (one per turn, one per sub-agent) would append a duplicate
// header to the existing file, which inflates the log and breaks downstream
// parsers that expect a single header at the top.
func TestLogger_HeaderWrittenOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sid := "test-session"

	lg1, err := NewLogger(dir, sid)
	require.NoError(t, err)
	lg1.LogInfo("step1", "first turn", Meta{})
	require.NoError(t, lg1.Close())

	// Simulate a second turn / sub-agent opening the same file.
	lg2, err := NewLogger(dir, sid)
	require.NoError(t, err)
	lg2.LogInfo("step2", "second turn", Meta{})
	require.NoError(t, lg2.Close())

	path := filepath.Join(dir, sid, "info.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	headerCount := strings.Count(content, "# Info & Decisions")
	require.Equal(t, 1, headerCount,
		"info.md should contain exactly one header, got %d. Content:\n%s",
		headerCount, content)

	// Both entries should still be present.
	require.Contains(t, content, "first turn")
	require.Contains(t, content, "second turn")
}

// TestLogger_HeaderWrittenForNewFile ensures the first Logger for a brand-new
// category does write the header.
func TestLogger_HeaderWrittenForNewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sid := "fresh-session"

	lg, err := NewLogger(dir, sid)
	require.NoError(t, err)
	lg.LogBug("crash", "something broke", Meta{})
	require.NoError(t, lg.Close())

	path := filepath.Join(dir, sid, "bug.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(data), "# Bug & Error Log\n\n"),
		"new bug.md must start with the header, got: %q", string(data))
}
