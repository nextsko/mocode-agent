package evolution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvolutionLoopEndToEnd drives the full loop: write sessionlog bug logs,
// produce patches, then confirm BuildContext (the read side that
// injectEvolutionContext uses) actually consumes them. This proves the
// negative loop is structurally closed end-to-end, not just in isolation.
func TestEvolutionLoopEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, err := NewPatchStore(tmp)
	if err != nil {
		t.Fatalf("NewPatchStore: %v", err)
	}
	prod, err := NewProducer(store, sessionsDir, WithMinRepeats(2))
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	// Seed three sessions with the same recurring error.
	shared := "```json\n" + `{"event":"tool_error","data":"grep: no such file","meta":{"tool_name":"grep","error_type":"tool_execution"}}` + "\n```\n\n"
	for _, s := range []string{"s1", "s2", "s3"} {
		os.MkdirAll(filepath.Join(sessionsDir, s), 0o755)
		os.WriteFile(filepath.Join(sessionsDir, s, "bug.md"), []byte(shared), 0o644)
	}

	// Produce.
	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 patch, got %d", len(created))
	}

	// Read side: BuildContext must surface the produced patch (this is what
	// injectEvolutionContext calls every agent run).
	loader := NewPatchLoader(store)
	ctx, err := loader.BuildContext()
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if ctx == "" {
		t.Fatal("expected non-empty evolution context")
	}
	if !strings.Contains(ctx, "grep") {
		t.Fatalf("context should mention the recurring grep error: %s", ctx)
	}
}
