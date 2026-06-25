package llmjudge

import (
	"math"
	"testing"
)

func TestParseScore(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want float64
		err  bool
	}{
		{"bare 100", "100", 1.0, false},
		{"score marker", "SCORE: 80", 0.8, false},
		{"score with prose", "The agent was good.\nSCORE: 75\n", 0.75, false},
		{"already normalized", "0.5", 0.5, false},
		{"clamped high", "150", 1.0, false},
		{"clamped low", "-5", 0.0, false},
		{"no number", "good job", 0, true},
		{"empty", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseScore(tt.in)
			if tt.err {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.in, err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("parseScore(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildJudgePromptIncludesRubrics(t *testing.T) {
	// Smoke test: ensure rubrics land in the prompt. Trace formatting is covered
	// indirectly by the criterion integration; here we only assert structure.
	out := buildJudgePrompt([]string{"be helpful", "be concise"}, nil)
	if !contains(out, "be helpful") || !contains(out, "be concise") {
		t.Fatalf("prompt missing rubrics: %s", out)
	}
	if !contains(out, "SCORE:") {
		t.Fatalf("prompt missing SCORE instruction: %s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
