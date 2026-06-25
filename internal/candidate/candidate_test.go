package candidate

import (
	"context"
	"errors"
	"testing"

	"github.com/package-register/mocode/internal/evaluation/criterion"
	"github.com/package-register/mocode/internal/evaluation/evalset"
	"github.com/package-register/mocode/internal/session/message"
)

// traceWith returns a trace whose final assistant text is s.
func traceWith(s string) []message.Message {
	return []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "q"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: s}}},
	}
}

func TestRunPicksHighestScoring(t *testing.T) {
	// Three candidates: scores 0.5, 1.0, 0.0 against an exact-match criterion.
	traces := [][]message.Message{traceWith("a"), traceWith("best"), traceWith("x")}
	gen := func(_ context.Context, runID int) (Result, error) {
		return Result{Trace: traces[runID-1]}, nil
	}
	sel := &EvalSelector{
		Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact, Expected: "best"}},
	}
	winner, err := Run(context.Background(), 3, gen, sel)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if winner.RunID != 2 {
		t.Fatalf("winner RunID = %d, want 2 (the 'best' candidate)", winner.RunID)
	}
}

func TestRunAllFail(t *testing.T) {
	gen := func(_ context.Context, runID int) (Result, error) {
		return Result{}, errors.New("boom")
	}
	sel := &EvalSelector{Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact}}}
	_, err := Run(context.Background(), 2, gen, sel)
	if err == nil {
		t.Fatal("expected error when all candidates fail")
	}
}

func TestRunToleratesSomeFailures(t *testing.T) {
	// Candidate 1 fails; candidates 2 and 3 succeed, 3 scores highest.
	gen := func(_ context.Context, runID int) (Result, error) {
		if runID == 1 {
			return Result{}, errors.New("network error")
		}
		if runID == 2 {
			return Result{Trace: traceWith("no")}, nil
		}
		return Result{Trace: traceWith("yes")}, nil
	}
	sel := &EvalSelector{
		Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact, Expected: "yes"}},
	}
	winner, err := Run(context.Background(), 3, gen, sel)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if winner.RunID != 3 {
		t.Fatalf("winner RunID = %d, want 3", winner.RunID)
	}
}

func TestRunConcurrentGeneration(t *testing.T) {
	// Verify gen is invoked concurrently: each candidate blocks until all N
	// have reported readiness, then all proceed together. This is deterministic
	// rather than timing-dependent.
	const n = 5
	ready := make(chan struct{}, n)
	release := make(chan struct{})
	gen := func(_ context.Context, runID int) (Result, error) {
		ready <- struct{}{}
		<-release
		return Result{Trace: traceWith("ok")}, nil
	}
	sel := &EvalSelector{Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact, Expected: "ok"}}}

	type runResult struct {
		res Result
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		res, err := Run(context.Background(), n, gen, sel)
		done <- runResult{res, err}
	}()

	// Wait for all N candidates to signal readiness (proving concurrency).
	for i := 0; i < n; i++ {
		select {
		case <-ready:
		case <-done:
			t.Fatal("Run returned before all candidates became ready; not concurrent")
		}
	}
	close(release)

	r := <-done
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
}

func TestEvalSelectorRequiresCriterion(t *testing.T) {
	sel := &EvalSelector{}
	if _, err := sel.Select(context.Background(), []Result{{}}); err == nil {
		t.Fatal("expected error for nil criterion")
	}
}

func TestEvalSelectorUsesCaseExpectations(t *testing.T) {
	// Criterion has no Expected; the case supplies ExpectedText.
	sel := &EvalSelector{
		Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact}},
		Case:      &evalset.EvalCase{ExpectedText: "target"},
	}
	candidates := []Result{
		{Trace: traceWith("target")},
		{Trace: traceWith("miss")},
	}
	idx, err := sel.Select(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if idx != 0 {
		t.Fatalf("winner idx = %d, want 0", idx)
	}
}

func TestParsePick(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		count int
		want  int
		err   bool
	}{
		{"bare number", "2", 3, 2, false},
		{"with prose", "The best is 3", 3, 3, false},
		{"out of range", "5", 3, 0, true},
		{"no number", "all good", 3, 0, true},
		{"empty", "", 3, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePick(tt.in, tt.count)
			if tt.err {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parsePick(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestLLMSelectorFallsBackGracefully(t *testing.T) {
	// With a nil model, Select errors; this guards the required-model check.
	sel := &LLMSelector{Prompt: "hi"}
	if _, err := sel.Select(context.Background(), []Result{{}}); err == nil {
		t.Fatal("expected error for nil judge model")
	}
}
