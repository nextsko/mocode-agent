package service

import (
	"context"
	"testing"

	"github.com/package-register/mocode/internal/evaluation/criterion"
	"github.com/package-register/mocode/internal/evaluation/evalset"
	"github.com/package-register/mocode/internal/evaluation/evalset/inmemory"
	"github.com/package-register/mocode/internal/evaluation/metric"
	"github.com/package-register/mocode/internal/evaluation/result"
	"github.com/package-register/mocode/internal/session/message"
)

func TestEvaluate(t *testing.T) {
	es := &evalset.EvalSet{
		ID: "set-1",
		Cases: []*evalset.EvalCase{
			{ID: "pass-case", UserContent: "say hi", ExpectedText: "Hello World", ExpectedTools: []evalset.ExpectedToolCall{{Name: "grep"}}},
			{ID: "fail-case", UserContent: "say hi", ExpectedText: "Goodbye"},
			{ID: "err-case", UserContent: "boom"},
		},
	}
	sets := inmemory.New()
	sets.Put(es)

	svc, err := NewLocal(LocalOptions{EvalSets: sets, Sessions: nil, Messages: nil})
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	metrics := []*metric.EvalMetric{
		{
			Name:      "final-response",
			Threshold: 1.0,
			Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact}},
		},
		{
			Name:      "tool-trajectory",
			Threshold: 1.0,
			Criterion: &criterion.Criterion{ToolTrajectory: &criterion.ToolTrajectory{Exact: true}},
		},
	}

	// Inference results are built directly (they are plain structs) to exercise
	// the Evaluate path without spinning up a full SessionAgent.
	infResults := []*InferenceResult{
		buildInference("pass-case", "Hello World", message.ToolCall{Name: "grep"}),
		buildInference("fail-case", "wrong text", message.ToolCall{Name: "grep"}),
		{EvalID: "err-case", Status: result.StatusError, ErrorMessage: "agent exploded"},
	}

	got, err := svc.Evaluate(context.Background(), &EvaluateRequest{
		EvalSetID:        "set-1",
		InferenceResults: infResults,
		Metrics:          metrics,
		RunID:            1,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	byID := map[string]*result.CaseResult{}
	for _, cr := range got.CaseResults {
		byID[cr.EvalID] = cr
	}

	if c := byID["pass-case"]; c == nil || c.Status != result.StatusPassed {
		t.Fatalf("pass-case: want StatusPassed, got %+v", c)
	}
	if c := byID["fail-case"]; c == nil || c.Status != result.StatusFailed {
		t.Fatalf("fail-case: want StatusFailed, got %+v", c)
	}
	if c := byID["err-case"]; c == nil || c.Status != result.StatusError || c.ErrorMessage == "" {
		t.Fatalf("err-case: want StatusError with message, got %+v", c)
	}

	// Verify metric-level detail on the passing case.
	pass := byID["pass-case"]
	if len(pass.MetricResults) != 2 {
		t.Fatalf("pass-case: want 2 metric results, got %d", len(pass.MetricResults))
	}
	for _, mr := range pass.MetricResults {
		if !mr.Passed {
			t.Fatalf("pass-case metric %s should have passed: %s", mr.MetricName, mr.Reason)
		}
	}
}

func buildInference(evalID, finalText string, calls ...message.ToolCall) *InferenceResult {
	parts := []message.ContentPart{}
	if finalText != "" {
		parts = append(parts, message.TextContent{Text: finalText})
	}
	for _, c := range calls {
		parts = append(parts, c)
	}
	trace := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "input"}}},
		{Role: message.Assistant, Parts: parts},
	}
	return &InferenceResult{EvalID: evalID, Trace: trace, Status: result.StatusPassed}
}
