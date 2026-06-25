// Package service runs agent inference and scores it with configured metrics.
package service

import (
	"context"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/internal/core/evaluation/criterion"
	"github.com/package-register/mocode/internal/core/evaluation/metric"
	"github.com/package-register/mocode/internal/core/evaluation/result"
	"github.com/package-register/mocode/internal/domain/session/message"
)

// Service covers the two evaluation phases:
//   - Inference runs the agent for each case and captures the full message trace.
//   - Evaluate scores the captured traces with the configured metrics.
type Service interface {
	// Inference runs the agent for every case in the set and returns one
	// InferenceResult per case. Each result carries both the agent's raw
	// *fantasy.AgentResult and the full structured message trace.
	Inference(ctx context.Context, req *InferenceRequest) ([]*InferenceResult, error)
	// Evaluate scores the inference results with the given metrics and returns
	// the aggregated EvalSetResult.
	Evaluate(ctx context.Context, req *EvaluateRequest) (*result.EvalSetResult, error)
	// Close releases resources owned by the service.
	Close() error
}

// InferenceRequest drives one inference pass over an evaluation set.
type InferenceRequest struct {
	// EvalSetID identifies the evaluation set to run.
	EvalSetID string
	// Agent is the agent under test. Required.
	Agent agent.SessionAgent
	// CallOpts are the base call options applied to every case (ProviderOptions,
	// temperature, etc.). Per-case fields (Prompt, SessionID) are set by Inference.
	CallOpts agent.SessionAgentCall
}

// InferenceResult is the captured outcome of running one case.
type InferenceResult struct {
	// EvalID identifies the evaluation case.
	EvalID string
	// Result is the raw agent result. May be nil if inference errored.
	Result *fantasy.AgentResult
	// Trace is the full message.Service.List result for the inference session.
	// This is the ground truth the scoring layer consumes: it contains every
	// tool call, tool result, reasoning step and the final response.
	Trace []message.Message
	// SessionID is the inference session that produced the trace.
	SessionID string
	// Status records whether inference succeeded.
	Status result.Status
	// ErrorMessage records the failure when Status is Error.
	ErrorMessage string
}

// EvaluateRequest drives one scoring pass over inference results.
type EvaluateRequest struct {
	// EvalSetID identifies the evaluation set.
	EvalSetID string
	// InferenceResults are the cases to score.
	InferenceResults []*InferenceResult
	// Metrics are the metrics to apply to each case.
	Metrics []*metric.EvalMetric
	// RunID labels the results with the run number within a multi-run eval.
	RunID int
	// Judger scores traces for LLMJudge criteria. May be nil.
	Judger criterion.Judger
}
