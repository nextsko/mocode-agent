package service

import (
	"context"
	"fmt"

	"github.com/package-register/mocode/internal/core/evaluation/criterion"
	"github.com/package-register/mocode/internal/core/evaluation/evalset"
	"github.com/package-register/mocode/internal/core/evaluation/metric"
	"github.com/package-register/mocode/internal/core/evaluation/result"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
)

// LocalOptions configures the local service.
type LocalOptions struct {
	// EvalSets manages evaluation sets. Required.
	EvalSets evalset.Manager
	// Sessions creates per-case inference sessions. Required.
	Sessions session.Service
	// Messages reads the inference trace. Required.
	Messages message.Service
}

// localService is the default Service implementation.
// It drives inference through a mocode SessionAgent and scores traces with
// criteria, reusing the existing session/message stores rather than inventing
// a parallel runner.
type localService struct {
	opts LocalOptions
}

// NewLocal builds a Service backed by the mocode session and message stores.
func NewLocal(opts LocalOptions) (Service, error) {
	if opts.EvalSets == nil {
		return nil, fmt.Errorf("evaluation service: EvalSets manager is required")
	}
	return &localService{opts: opts}, nil
}

func (s *localService) Close() error { return nil }

// Inference runs the agent for every case in the set.
//
// For each case it creates a dedicated inference session, runs the agent with
// the case's user content (and optional system prompt override), then captures
// the full message trace. A case whose inference errors is still reported, with
// StatusError, so the evaluation result reflects it rather than aborting the run.
func (s *localService) Inference(ctx context.Context, req *InferenceRequest) ([]*InferenceResult, error) {
	if req.Agent == nil {
		return nil, fmt.Errorf("evaluation service: agent is required")
	}
	if s.opts.Sessions == nil {
		return nil, fmt.Errorf("evaluation service: Sessions service is required for inference")
	}
	if s.opts.Messages == nil {
		return nil, fmt.Errorf("evaluation service: Messages service is required for inference")
	}
	es, err := s.opts.EvalSets.Get(ctx, req.EvalSetID)
	if err != nil {
		return nil, fmt.Errorf("get eval set %q: %w", req.EvalSetID, err)
	}
	results := make([]*InferenceResult, 0, len(es.Cases))
	for _, c := range es.Cases {
		results = append(results, s.runCase(ctx, req, c))
	}
	return results, nil
}

func (s *localService) runCase(ctx context.Context, req *InferenceRequest, c *evalset.EvalCase) *InferenceResult {
	sess, err := s.opts.Sessions.Create(ctx, "eval: "+c.ID)
	if err != nil {
		return &InferenceResult{EvalID: c.ID, Status: result.StatusError, ErrorMessage: "create session: " + err.Error()}
	}
	if c.SystemPrompt != "" {
		req.Agent.SetSystemPrompt(c.SystemPrompt)
	}
	call := req.CallOpts
	call.SessionID = sess.ID
	call.Prompt = c.UserContent
	call.NonInteractive = true
	agentResult, err := req.Agent.Run(ctx, call)
	if err != nil {
		return &InferenceResult{EvalID: c.ID, SessionID: sess.ID, Status: result.StatusError, ErrorMessage: "agent run: " + err.Error()}
	}
	trace, err := s.opts.Messages.List(ctx, sess.ID)
	if err != nil {
		// Trace capture failure is non-fatal: score against whatever the agent
		// returned directly. FinalText/ToolCalls fall back to empty.
		trace = nil
	}
	return &InferenceResult{
		EvalID:    c.ID,
		Result:    agentResult,
		Trace:     trace,
		SessionID: sess.ID,
		Status:    result.StatusPassed,
	}
}

// Evaluate scores each inference result with every metric.
//
// A case with StatusError is reported as a failed case result without scoring.
// Otherwise each metric's criterion is evaluated against the trace; the metric's
// threshold decides pass/fail. A case passes only if every metric passes.
func (s *localService) Evaluate(ctx context.Context, req *EvaluateRequest) (*result.EvalSetResult, error) {
	caseResults := make([]*result.CaseResult, 0, len(req.InferenceResults))
	for _, inf := range req.InferenceResults {
		caseResults = append(caseResults, s.scoreCase(ctx, req, inf))
	}
	return &result.EvalSetResult{
		EvalSetID:   req.EvalSetID,
		CaseResults: caseResults,
	}, nil
}

func (s *localService) scoreCase(ctx context.Context, req *EvaluateRequest, inf *InferenceResult) *result.CaseResult {
	if inf.Status == result.StatusError {
		return &result.CaseResult{
			EvalID:       inf.EvalID,
			RunID:        req.RunID,
			Status:       result.StatusError,
			SessionID:    inf.SessionID,
			ErrorMessage: inf.ErrorMessage,
		}
	}
	// Look up the case to get ExpectedTools/Rubrics/ExpectedText for scoring.
	es, err := s.opts.EvalSets.Get(ctx, req.EvalSetID)
	if err != nil {
		return &result.CaseResult{
			EvalID:       inf.EvalID,
			RunID:        req.RunID,
			Status:       result.StatusError,
			ErrorMessage: "get eval set for case: " + err.Error(),
		}
	}
	var cs *evalset.EvalCase
	for _, c := range es.Cases {
		if c.ID == inf.EvalID {
			cs = c
			break
		}
	}
	metricResults := make([]*result.MetricResult, 0, len(req.Metrics))
	allPassed := true
	for _, m := range req.Metrics {
		mr := scoreMetric(ctx, m, cs, inf, req.Judger)
		metricResults = append(metricResults, mr)
		if !mr.Passed {
			allPassed = false
		}
	}
	status := result.StatusPassed
	if !allPassed {
		status = result.StatusFailed
	}
	return &result.CaseResult{
		EvalID:        inf.EvalID,
		RunID:         req.RunID,
		Status:        status,
		MetricResults: metricResults,
		SessionID:     inf.SessionID,
	}
}

func scoreMetric(ctx context.Context, m *metric.EvalMetric, cs *evalset.EvalCase, inf *InferenceResult, judger criterion.Judger) *result.MetricResult {
	er, err := m.Criterion.Eval(ctx, cs, inf.Trace, judger)
	if err != nil {
		return &result.MetricResult{MetricName: m.Name, Score: 0, Passed: false, Reason: "criterion error: " + err.Error()}
	}
	return &result.MetricResult{
		MetricName: m.Name,
		Score:      er.Score,
		Passed:     er.Score >= m.Threshold,
		Reason:     er.Reason,
	}
}
