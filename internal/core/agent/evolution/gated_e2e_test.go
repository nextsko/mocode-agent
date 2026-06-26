package evolution

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/package-register/mocode/internal/core/agent/evolution/gates"
)

// TestEvolutionLoopGatedBlocksDangerousPatch proves that when gates are wired
// (as cmd/evolve.go now does), a recurring error whose body contains a secret
// or dangerous shell pattern is BLOCKED before persistence — it never reaches
// the system-prompt injection path. This closes the verification loop on the
// production wiring: the safety gate works end-to-end, not just in isolation.
func TestEvolutionLoopGatedBlocksDangerousPatch(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, err := NewPatchStore(tmp)
	if err != nil {
		t.Fatalf("NewPatchStore: %v", err)
	}
	prod, err := NewProducer(
		store, sessionsDir,
		WithMinRepeats(2),
		WithGates(&gates.Pipeline{
			Spec:   gates.NewDefaultSpecGate(),
			Safety: gates.NewDefaultSafetyGate(),
		}),
	)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	// Seed sessions with a recurring error that LEAKS a secret in its body.
	// Without the SafetyGate, this would be persisted as a patch and injected
	// into every future system prompt — a real secret-exfiltration path.
	shared := "```json\n" + `{"event":"tool_error","data":"curl failed: token ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa rejected","meta":{"tool_name":"fetch","error_type":"auth"}}` + "\n```\n\n"
	for _, s := range []string{"s1", "s2", "s3"} {
		os.MkdirAll(filepath.Join(sessionsDir, s), 0o755)
		os.WriteFile(filepath.Join(sessionsDir, s, "bug.md"), []byte(shared), 0o644)
	}

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("SafetyGate should have blocked the secret-leaking patch, but %d were created: %v", len(created), created)
	}

	// Confirm nothing was persisted: the read side must show no context.
	loader := NewPatchLoader(store)
	ctx, err := loader.BuildContext()
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if ctx != "" {
		t.Fatalf("expected empty context (patch was gated), got non-empty: %q", ctx)
	}
}
