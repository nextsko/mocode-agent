package gates

import (
	"context"
	"errors"
	"testing"
)

// erroringGate is a SpecGate stub that always returns an error, to verify the
// Pipeline propagates gate errors rather than swallowing them or treating an
// error as a rejection.
type erroringGate struct{}

func (erroringGate) Validate(_ context.Context, _ *Candidate) (*Report, error) {
	return nil, errors.New("synthetic gate failure")
}

// TestPipelinePropagatesGateError confirms that when a gate returns a non-nil
// error, the Pipeline returns that error (wrapped with the gate name) instead
// of silently passing the candidate or returning a zero-value verdict. The
// coordinator and Producer treat a returned error as a hard failure distinct
// from a gate rejection.
func TestPipelinePropagatesGateError(t *testing.T) {
	p := &Pipeline{Spec: erroringGate{}}
	_, err := p.Run(context.Background(), &Candidate{
		Action:      ActionCreate,
		Title:       "t",
		Description: "d",
	})
	if err == nil {
		t.Fatal("expected error from erroring gate, got nil")
	}
	if !contains(err.Error(), "synthetic gate failure") {
		t.Fatalf("expected wrapped error to contain cause, got: %v", err)
	}
	if !contains(err.Error(), "gate spec") {
		t.Fatalf("expected error to name the failing gate, got: %v", err)
	}
}

// TestPipelineHumanGateErrorPropagates confirms the human gate's error path is
// also wrapped, not swallowed.
type erroringHumanGate struct{}

func (erroringHumanGate) ShouldHold(_ context.Context, _ *Candidate) (bool, error) {
	return false, errors.New("human gate io failure")
}

func TestPipelineHumanGateErrorPropagates(t *testing.T) {
	p := &Pipeline{Human: erroringHumanGate{}}
	_, err := p.Run(context.Background(), &Candidate{
		Action:      ActionCreate,
		Title:       "t",
		Description: "d",
	})
	if err == nil {
		t.Fatal("expected error from erroring human gate, got nil")
	}
	if !contains(err.Error(), "human gate io failure") {
		t.Fatalf("expected wrapped cause, got: %v", err)
	}
	if !contains(err.Error(), "gate human") {
		t.Fatalf("expected error to name the human gate, got: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
