package skills

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// capturingHandler collects slog records for assertions.
type capturingHandler struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.WriteString(r.Message + "\n")
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

func TestDiscoverLogsParseFailureOnce(t *testing.T) {
	dir := t.TempDir()
	badDir := filepath.Join(dir, "bad-skill")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	bad := filepath.Join(badDir, "SKILL.md")
	require.NoError(t, os.WriteFile(bad, []byte("# no frontmatter\nbody\n"), 0o644))

	// Capture warnings across two discovery runs over the same tree.
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	paths := []string{dir}
	skills1, _ := DiscoverWithStates(paths)
	skills2, _ := DiscoverWithStates(paths)
	require.Empty(t, skills1)
	require.Empty(t, skills2)

	count := strings.Count(buf.String(), "Failed to parse skill file")
	require.Equal(t, 1, count,
		"a broken SKILL.md must warn once per process, not once per discovery run")
}
