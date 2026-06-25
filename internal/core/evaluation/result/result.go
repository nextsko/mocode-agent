// Package result defines evaluation result structures.
package result

import "context"

// Status is the outcome of evaluating a case or metric.
type Status string

const (
	// StatusPassed means all evaluated metrics met their thresholds.
	StatusPassed Status = "passed"
	// StatusFailed means at least one evaluated metric did not meet its threshold.
	StatusFailed Status = "failed"
	// StatusError means inference or evaluation crashed before scoring completed.
	StatusError Status = "error"
)

// MetricResult is the score of a single metric on a single case run.
type MetricResult struct {
	// MetricName identifies the metric.
	MetricName string
	// Score is the normalized score in [0, 1].
	Score float64
	// Passed reports whether Score met the metric threshold.
	Passed bool
	// Reason explains the score.
	Reason string
}

// CaseResult aggregates metric results for one case in one run.
type CaseResult struct {
	// EvalID identifies the evaluation case.
	EvalID string
	// RunID identifies the run within a multi-run evaluation (1-based).
	RunID int
	// Status is the aggregated case status across all metrics.
	Status Status
	// MetricResults holds the per-metric scores.
	MetricResults []*MetricResult
	// SessionID is the inference session that produced the trace.
	SessionID string
	// ErrorMessage records an inference or evaluation failure when Status is Error.
	ErrorMessage string
}

// Summary aggregates results across multiple runs of an EvalSet.
type Summary struct {
	// NumRuns is the number of runs executed.
	NumRuns int
	// TotalCases is the number of cases in the EvalSet.
	TotalCases int
	// PassRate is the fraction of case-runs that passed (0..1).
	PassRate float64
	// AvgScore is the mean metric score across all case-runs and metrics.
	AvgScore float64
}

// EvalSetResult is the full result of evaluating an EvalSet.
type EvalSetResult struct {
	// EvalSetID identifies the EvalSet that was evaluated.
	EvalSetID string
	// CaseResults holds one result per case-run pair.
	CaseResults []*CaseResult
	// Summary aggregates CaseResults across runs. Nil for a single-run eval.
	Summary *Summary
}

// Manager persists evaluation results.
type Manager interface {
	// Save stores a result and returns the assigned result ID.
	Save(ctx context.Context, r *EvalSetResult) (string, error)
	// Get retrieves a result by ID.
	Get(ctx context.Context, id string) (*EvalSetResult, error)
	// List returns the IDs of all stored results.
	List(ctx context.Context) ([]string, error)
	// Close releases resources held by the manager.
	Close() error
}
