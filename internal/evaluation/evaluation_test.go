package evaluation

import (
	"context"
	"testing"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/agent"
	"github.com/package-register/mocode/internal/evaluation/criterion"
	"github.com/package-register/mocode/internal/evaluation/evalset"
	"github.com/package-register/mocode/internal/evaluation/evalset/inmemory"
	"github.com/package-register/mocode/internal/evaluation/metric"
	"github.com/package-register/mocode/internal/evaluation/result"
	"github.com/package-register/mocode/internal/evaluation/service"
	"github.com/package-register/mocode/internal/session/message"
)

// fakeService returns a deterministic inference + scoring result so the
// Evaluator's run-loop and summarization can be tested in isolation.
type fakeService struct {
	metrics []*metric.EvalMetric
}

func (f *fakeService) Inference(_ context.Context, req *service.InferenceRequest) ([]*service.InferenceResult, error) {
	// Return one inference result per case: a single passing trace.
	out := []*service.InferenceResult{
		{
			EvalID: "c1",
			Trace: []message.Message{
				{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
				{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "Hello"}}},
			},
			Status: result.StatusPassed,
		},
	}
	return out, nil
}

func (f *fakeService) Evaluate(_ context.Context, req *service.EvaluateRequest) (*result.EvalSetResult, error) {
	caseResults := make([]*result.CaseResult, 0, len(req.InferenceResults))
	for _, inf := range req.InferenceResults {
		mrs := make([]*result.MetricResult, 0, len(req.Metrics))
		for _, m := range req.Metrics {
			er, err := m.Criterion.Eval(context.Background(), &evalset.EvalCase{ID: inf.EvalID, ExpectedText: "Hello"}, inf.Trace, nil)
			if err != nil {
				mrs = append(mrs, &result.MetricResult{MetricName: m.Name, Passed: false})
				continue
			}
			mrs = append(mrs, &result.MetricResult{MetricName: m.Name, Score: er.Score, Passed: er.Score >= m.Threshold, Reason: er.Reason})
		}
		caseResults = append(caseResults, &result.CaseResult{EvalID: inf.EvalID, RunID: req.RunID, Status: result.StatusPassed, MetricResults: mrs})
	}
	return &result.EvalSetResult{EvalSetID: req.EvalSetID, CaseResults: caseResults}, nil
}

func (f *fakeService) Close() error { return nil }

func TestEvaluatorMultiRunSummary(t *testing.T) {
	sets := inmemory.New()
	sets.Put(&evalset.EvalSet{ID: "s1", Cases: []*evalset.EvalCase{{ID: "c1", UserContent: "hi", ExpectedText: "Hello"}}})

	svc := &fakeService{}
	// fakeService bypasses real inference, so the agent is never exercised; it
	// only needs to satisfy New()'s type requirement.
	ev, err := New(nilAgent{}, WithService(svc), WithEvalSetManager(sets),
		WithMetrics(&metric.EvalMetric{Name: "fr", Threshold: 1.0, Criterion: &criterion.Criterion{FinalResponse: &criterion.FinalResponse{Mode: criterion.MatchExact}}}),
		WithNumRuns(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := ev.Evaluate(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.Summary == nil {
		t.Fatal("expected Summary")
	}
	if got.Summary.NumRuns != 3 {
		t.Fatalf("NumRuns = %d, want 3", got.Summary.NumRuns)
	}
	if len(got.CaseResults) != 3 {
		t.Fatalf("CaseResults len = %d, want 3 (one per run)", len(got.CaseResults))
	}
	if got.Summary.PassRate != 1.0 {
		t.Fatalf("PassRate = %v, want 1.0", got.Summary.PassRate)
	}
}

// nilAgent satisfies agent.SessionAgent without exercising real inference;
// the fakeService bypasses the agent entirely.
type nilAgent struct{}

func (nilAgent) Run(context.Context, agent.SessionAgentCall) (*fantasy.AgentResult, error) {
	return nil, nil
}
func (nilAgent) SetModels(agent.Model, agent.Model)                        {}
func (nilAgent) SetTools([]fantasy.AgentTool)                              {}
func (nilAgent) SetSystemPrompt(string)                                    {}
func (nilAgent) SystemPrompt() string                                      { return "" }
func (nilAgent) Cancel(string)                                             {}
func (nilAgent) CancelAll()                                                {}
func (nilAgent) IsSessionBusy(string) bool                                 { return false }
func (nilAgent) IsBusy() bool                                              { return false }
func (nilAgent) QueuedPrompts(string) int                                  { return 0 }
func (nilAgent) QueuedPromptsList(string) []string                         { return nil }
func (nilAgent) ClearQueue(string)                                         {}
func (nilAgent) Summarize(context.Context, string, fantasy.ProviderOptions) (string, error) {
	return "", nil
}
func (nilAgent) Model() agent.Model { return agent.Model{} }
