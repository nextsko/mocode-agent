package evolution

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBugLog writes a sessionlog-style bug.md with the given JSON entries.
func writeBugLog(t *testing.T, sessionsDir, sessionID string, entries []string) {
	t.Helper()
	dir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var content string
	for _, e := range entries {
		content += "```json\n" + e + "\n```\n\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "bug.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func bugEntry(toolName, data string) string {
	return `{"event":"tool_error","data":` + jsonString(data) + `,"meta":{"tool_name":` + jsonString(toolName) + `,"error_type":"tool_execution"}}`
}

// jsonString quotes a string for JSON. Minimal; test data has no special chars.
func jsonString(s string) string {
	return `"` + s + `"`
}

func TestProducerCreatesRulePatchForRecurringError(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, err := NewPatchStore(tmp)
	if err != nil {
		t.Fatalf("NewPatchStore: %v", err)
	}
	prod, err := NewProducer(store, sessionsDir, WithMinRepeats(3))
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}

	// Three sessions, each with the same bash error.
	shared := bugEntry("bash", "command not found: foo")
	writeBugLog(t, sessionsDir, "s1", []string{shared})
	writeBugLog(t, sessionsDir, "s2", []string{shared})
	writeBugLog(t, sessionsDir, "s3", []string{shared})

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 patch, got %d: %v", len(created), created)
	}

	patches, err := store.ListUnapplied()
	if err != nil {
		t.Fatalf("ListUnapplied: %v", err)
	}
	if len(patches) != 1 || patches[0].Kind != KindRule {
		t.Fatalf("expected 1 unapplied rule patch, got %+v", patches)
	}
	if patches[0].Title == "" {
		t.Fatal("patch has empty title")
	}

	// Verify the rule content was written.
	rulePath := filepath.Join(store.Dir(), patches[0].ID, "rules", "rule.md")
	rule, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatalf("read rule.md: %v", err)
	}
	if string(rule) == "" {
		t.Fatal("rule.md is empty")
	}
}

func TestProducerDedupsOnRerun(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir, WithMinRepeats(2))

	shared := bugEntry("grep", "no matches")
	writeBugLog(t, sessionsDir, "s1", []string{shared})
	writeBugLog(t, sessionsDir, "s2", []string{shared})

	if _, err := prod.Produce(); err != nil {
		t.Fatalf("first Produce: %v", err)
	}
	// Second run on identical data must not duplicate.
	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("second Produce: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("expected 0 new patches on rerun, got %d", len(created))
	}
}

func TestProducerBelowThresholdNoPatch(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir, WithMinRepeats(5))

	writeBugLog(t, sessionsDir, "s1", []string{bugEntry("bash", "rare error")})
	writeBugLog(t, sessionsDir, "s2", []string{bugEntry("bash", "rare error")})

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("expected 0 patches below threshold, got %d", len(created))
	}
}

func TestProducerEmptySessionsDir(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, filepath.Join(tmp, "nonexistent"))
	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce on missing dir: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("expected 0 patches, got %d", len(created))
	}
}

func TestSplitJSONBlocks(t *testing.T) {
	md := "```json\n{\"a\":1}\n```\n\n```json\n{\"b\":2}\n```\n"
	blocks := splitJSONBlocks(md)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestBuildContextAutoConverges(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewPatchStore(tmp)
	if err != nil {
		t.Fatalf("NewPatchStore: %v", err)
	}
	// Create a patch with MaxInjects=2 so it converges after two injections.
	dir, err := store.CreatePatch(Patch{Kind: KindRule, Title: "converge-test", MaxInjects: 2})
	if err != nil {
		t.Fatalf("CreatePatch: %v", err)
	}
	if err := store.WriteFile(dir, "rules", "rule.md", "do the thing"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loader := NewPatchLoader(store)

	// First build: patch is unapplied, gets injected, InjectCount -> 1.
	ctx1, err := loader.BuildContext()
	if err != nil {
		t.Fatalf("BuildContext 1: %v", err)
	}
	if ctx1 == "" {
		t.Fatal("expected non-empty context on first build")
	}
	unapplied, _ := store.ListUnapplied()
	if len(unapplied) != 1 {
		t.Fatalf("expected 1 unapplied after 1 inject, got %d", len(unapplied))
	}

	// Second build: InjectCount -> 2 == MaxInjects, auto-applied.
	if _, err := loader.BuildContext(); err != nil {
		t.Fatalf("BuildContext 2: %v", err)
	}
	unapplied, _ = store.ListUnapplied()
	if len(unapplied) != 0 {
		t.Fatalf("expected 0 unapplied after convergence, got %d", len(unapplied))
	}

	// Third build: converged patch no longer injected.
	ctx3, err := loader.BuildContext()
	if err != nil {
		t.Fatalf("BuildContext 3: %v", err)
	}
	if ctx3 != "" {
		t.Fatalf("expected empty context after convergence, got %q", ctx3)
	}
}

func TestBuildContextNoMaxInjectsNeverConverges(t *testing.T) {
	tmp := t.TempDir()
	store, _ := NewPatchStore(tmp)
	store.CreatePatch(Patch{Kind: KindRule, Title: "no-converge", MaxInjects: 0})
	dir, _ := store.ListUnapplied()
	store.WriteFile(filepath.Join(store.Dir(), dir[0].ID), "rules", "rule.md", "x")
	loader := NewPatchLoader(store)
	for i := 0; i < 5; i++ {
		loader.BuildContext()
	}
	unapplied, _ := store.ListUnapplied()
	if len(unapplied) != 1 {
		t.Fatalf("MaxInjects=0 should never converge, got %d unapplied", len(unapplied))
	}
}

func TestProducerConsumesErrcollJSONL(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	errorsDir := filepath.Join(tmp, "errors")
	os.MkdirAll(errorsDir, 0o755)
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir, WithMinRepeats(2))

	// Write errcoll JSONL with the same bash error twice.
	rec := `{"tool_name":"bash","error":"command not found: bar","session_id":"e1"}` + "\n"
	os.WriteFile(filepath.Join(errorsDir, "errors-20260101.jsonl"), []byte(rec+rec), 0o644)

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 patch from errcoll, got %d", len(created))
	}
	patches, _ := store.ListUnapplied()
	if len(patches) != 1 {
		t.Fatalf("expected 1 unapplied patch, got %d", len(patches))
	}
}

func TestProducerMergesSessionAndErrcollSources(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	errorsDir := filepath.Join(tmp, "errors")
	os.MkdirAll(errorsDir, 0o755)
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir, WithMinRepeats(3))

	// Same error once in sessionlog, twice in errcoll -> merged count 3.
	writeBugLog(t, sessionsDir, "s1", []string{bugEntry("bash", "command not found: baz")})
	rec := `{"tool_name":"bash","error":"command not found: baz","session_id":"e1"}` + "\n"
	os.WriteFile(filepath.Join(errorsDir, "errors-20260101.jsonl"), []byte(rec+rec), 0o644)

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("expected 1 merged patch (count 3), got %d", len(created))
	}
}

func writeInfoLog(t *testing.T, sessionsDir, sessionID string, entries []string) {
	t.Helper()
	dir := filepath.Join(sessionsDir, sessionID)
	os.MkdirAll(dir, 0o755)
	var content string
	for _, e := range entries {
		content += "```json\n" + e + "\n```\n\n"
	}
	os.WriteFile(filepath.Join(dir, "info.md"), []byte(content), 0o644)
}

func turnScoreEntry(score float64, reason string) string {
	return `{"event":"turn_score","data":"score=` + fmtScore(score) + ` reason=` + reason + `"}`
}

func fmtScore(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

func TestProducerQualityPatchOnLowScores(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir)

	// Three low-scoring turns across sessions.
	writeInfoLog(t, sessionsDir, "s1", []string{turnScoreEntry(0.2, "incomplete")})
	writeInfoLog(t, sessionsDir, "s2", []string{turnScoreEntry(0.3, "vague")})
	writeInfoLog(t, sessionsDir, "s3", []string{turnScoreEntry(0.25, "wrong")})

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	// Expect exactly the quality patch (no error patches since no bug logs).
	found := false
	for _, id := range created {
		patches, _ := store.List()
		for _, p := range patches {
			if p.ID == id && strings.Contains(p.Title, "response quality") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected a quality patch, created=%v", created)
	}
}

func TestProducerNoQualityPatchOnHighScores(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir)

	writeInfoLog(t, sessionsDir, "s1", []string{turnScoreEntry(0.9, "good")})
	writeInfoLog(t, sessionsDir, "s2", []string{turnScoreEntry(0.85, "great")})
	writeInfoLog(t, sessionsDir, "s3", []string{turnScoreEntry(0.8, "solid")})

	created, err := prod.Produce()
	if err != nil {
		t.Fatalf("Produce: %v", err)
	}
	if len(created) != 0 {
		t.Fatalf("expected no patch for high scores, got %v", created)
	}
}

func TestProducerNoQualityPatchTooFewSamples(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir)

	// Only two low scores — below qualityMinSamples.
	writeInfoLog(t, sessionsDir, "s1", []string{turnScoreEntry(0.1, "bad")})
	writeInfoLog(t, sessionsDir, "s2", []string{turnScoreEntry(0.2, "bad")})

	created, _ := prod.Produce()
	if len(created) != 0 {
		t.Fatalf("expected no patch with too few samples, got %v", created)
	}
}

func TestParseScoreField(t *testing.T) {
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"score=0.42 reason=vague", 0.42, true},
		{"score=1.00 reason=", 1.0, true},
		{"no score here", 0, false},
		{"score=0.5", 0.5, true},
	}
	for _, tt := range tests {
		got, ok := parseScoreField(tt.in)
		if ok != tt.ok {
			t.Errorf("parseScoreField(%q) ok=%v want %v", tt.in, ok, tt.ok)
			continue
		}
		if ok && (got < tt.want-0.001 || got > tt.want+0.001) {
			t.Errorf("parseScoreField(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestScanQualityReadsScores(t *testing.T) {
	tmp := t.TempDir()
	sessionsDir := filepath.Join(tmp, "sessions")
	store, _ := NewPatchStore(tmp)
	prod, _ := NewProducer(store, sessionsDir)

	writeInfoLog(t, sessionsDir, "s1", []string{turnScoreEntry(0.2, "x")})
	writeInfoLog(t, sessionsDir, "s2", []string{turnScoreEntry(0.3, "y")})
	writeInfoLog(t, sessionsDir, "s3", []string{turnScoreEntry(0.25, "z")})

	scores := prod.scanQuality()
	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d: %v", len(scores), scores)
	}
	// Also exercise the patch emission directly to isolate Produce's wiring.
	id, ok, err := prod.produceQualityPatch(map[string]bool{})
	if err != nil {
		t.Fatalf("produceQualityPatch: %v", err)
	}
	if !ok || id == "" {
		t.Fatalf("expected quality patch emitted, got id=%q ok=%v", id, ok)
	}
	want := []float64{0.2, 0.3, 0.25}
	for i, s := range scores {
		if s < want[i]-0.01 || s > want[i]+0.01 {
			t.Errorf("score[%d] = %v, want ~%v", i, s, want[i])
		}
	}
}
