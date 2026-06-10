package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestErrorLearner_RecordAndPromote(t *testing.T) {
	dir := t.TempDir()
	l, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Record the same error 2 times — should not be promoted yet.
	p1 := l.Record("minimax", "MiniMax-Text-01", "bash", "head: command not found")
	if p1.IsPromoted {
		t.Fatal("should not be promoted after 1 occurrence")
	}
	if p1.Occurrences != 1 {
		t.Fatalf("expected 1 occurrence, got %d", p1.Occurrences)
	}

	p2 := l.Record("minimax", "MiniMax-Text-01", "bash", "head: command not found")
	if p2.IsPromoted {
		t.Fatal("should not be promoted after 2 occurrences")
	}

	// Third occurrence should trigger promotion.
	p3 := l.Record("minimax", "MiniMax-Text-01", "bash", "head: command not found")
	if !p3.IsPromoted {
		t.Fatal("should be promoted after 3 occurrences")
	}
	if p3.Occurrences != 3 {
		t.Fatalf("expected 3 occurrences, got %d", p3.Occurrences)
	}

	// Check file was created on disk.
	if _, statErr := os.Stat(filepath.Join(dir, p3.ID+".md")); statErr != nil {
		t.Fatalf("pattern file not created: %v", statErr)
	}
}

func TestErrorLearner_ActiveCorrections(t *testing.T) {
	dir := t.TempDir()
	l, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Promote a pattern for minimax.
	for i := 0; i < PromoteThreshold; i++ {
		l.Record("minimax", "MiniMax-Text-01", "bash", "head: command not found")
	}

	// Should get correction for minimax.
	corrections := l.ActiveCorrections("minimax", "MiniMax-Text-01")
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Tool != "bash" {
		t.Fatalf("expected tool=bash, got %s", corrections[0].Tool)
	}

	// Should NOT get correction for anthropic.
	anthropicCorrections := l.ActiveCorrections("anthropic", "claude-sonnet-4-20250514")
	if len(anthropicCorrections) != 0 {
		t.Fatal("should not get corrections for different provider")
	}
}

func TestErrorLearner_CorrectionContext(t *testing.T) {
	dir := t.TempDir()
	l, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < PromoteThreshold; i++ {
		l.Record("minimax", "MiniMax-Text-01", "bash", "head: command not found")
	}

	ctx := l.CorrectionContext("minimax", "MiniMax-Text-01")
	if ctx == "" {
		t.Fatal("expected non-empty correction context")
	}
	if !contains(ctx, "Learned Corrections") {
		t.Fatal("expected 'Learned Corrections' heading")
	}
	if !contains(ctx, "bash") {
		t.Fatal("expected bash tool reference in correction")
	}
}

func TestErrorLearner_Persistence(t *testing.T) {
	dir := t.TempDir()

	// Create and promote a pattern.
	l1, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < PromoteThreshold; i++ {
		l1.Record("minimax", "MiniMax-Text-01", "bash", "head: command not found")
	}

	// Load from the same directory — should recover the pattern.
	l2, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}
	corrections := l2.ActiveCorrections("minimax", "MiniMax-Text-01")
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction after reload, got %d", len(corrections))
	}
	if corrections[0].Occurrences != PromoteThreshold {
		t.Fatalf("expected %d occurrences, got %d", PromoteThreshold, corrections[0].Occurrences)
	}
}

func TestErrorLearner_DifferentPatternsNotMerged(t *testing.T) {
	dir := t.TempDir()
	l, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}

	l.Record("minimax", "", "bash", "head: command not found")
	l.Record("minimax", "", "bash", "cat: permission denied")

	patterns := l.AllPatterns()
	if len(patterns) != 2 {
		t.Fatalf("expected 2 distinct patterns, got %d", len(patterns))
	}
}

func TestErrorLearner_EditErrorCorrection(t *testing.T) {
	dir := t.TempDir()
	l, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}

	p := l.Record("anthropic", "claude-sonnet-4-20250514", "edit", "old_string not found in file")
	if !contains(p.Correction, "Re-read") {
		t.Fatalf("expected edit-specific correction, got: %s", p.Correction)
	}
}

func TestExtractCommandName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bash: head: command not found", "head"},
		{"command not found: vim", "vim"},
		{"no match here", ""},
	}
	for _, tt := range tests {
		got := extractCommandName(tt.input)
		if got != tt.want {
			t.Errorf("extractCommandName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPatternFileFormat(t *testing.T) {
	dir := t.TempDir()
	l, err := NewErrorLearner(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < PromoteThreshold; i++ {
		l.Record("minimax", "", "bash", "head: command not found")
	}

	patterns := l.AllPatterns()
	if len(patterns) == 0 {
		t.Fatal("expected at least one pattern")
	}

	// Read the file and verify it's valid Obsidian-flavored Markdown.
	data, err := os.ReadFile(filepath.Join(dir, patterns[0].ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !contains(content, "---\n") {
		t.Fatal("expected YAML frontmatter delimiter")
	}
	if !contains(content, "provider:") {
		t.Fatal("expected provider field in frontmatter")
	}
	if !contains(content, "tool:") {
		t.Fatal("expected tool field in frontmatter")
	}
	if !contains(content, "# Error Pattern:") {
		t.Fatal("expected error pattern heading")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
