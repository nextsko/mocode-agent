package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// makeTree creates dirs subdirectories each containing one f.txt.
func makeTree(t *testing.T, dirs int) string {
	t.Helper()
	root := t.TempDir()
	for i := range dirs {
		d := filepath.Join(root, fmt.Sprintf("d%03d", i))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGlobScanTimeoutReturnsHint(t *testing.T) {
	// NOT parallel: mutates the package-level globScanTimeout var; parallel
	// tests in this package (e.g. TestGlobPortTool) read it concurrently.
	// Sequential tests complete before paused parallel tests resume.

	// A 1ns deadline fires before rg can produce anything — a deterministic
	// way to exercise the deadline path without a huge tree.
	orig := globScanTimeout
	globScanTimeout = time.Nanosecond
	t.Cleanup(func() { globScanTimeout = orig })

	root := makeTree(t, 20)

	res, err := runGlob(t.Context(), t.TempDir(), GlobParams{
		Pattern: "**/*.txt",
		Path:    root,
	})
	require.NoError(t, err)

	require.Contains(t, res.Content, "No files found")
	require.Contains(t, res.Content, "hit the")
}

func TestGlobNormalScanNotFlagged(t *testing.T) {
	t.Parallel()

	root := makeTree(t, 5)

	res, err := runGlob(t.Context(), t.TempDir(), GlobParams{
		Pattern: "**/*.txt",
		Path:    root,
	})
	require.NoError(t, err)
	require.NotContains(t, res.Content, "hit the")
}

func TestGlobNarrowHintContextError(t *testing.T) {
	t.Parallel()

	root := makeTree(t, 3)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	hint := globNarrowHint(root, ctx)
	require.Contains(t, hint, "hit the")
	require.Contains(t, hint, root)
}
