// Package metric binds a named scoring threshold to a criterion.
package metric

import (
	"context"

	"github.com/nextsko/mocode-agent/internal/core/evaluation/criterion"
)

// EvalMetric binds a name and pass threshold to a criterion.
// One metric produces one MetricResult per case run.
type EvalMetric struct {
	// Name identifies this metric instance.
	Name string
	// Threshold is the minimum score for the metric to pass.
	// Score >= Threshold marks the case metric as passed.
	Threshold float64
	// Criterion is the scoring criterion. Required.
	Criterion *criterion.Criterion
}

// Manager stores and retrieves metrics.
type Manager interface {
	// List returns the names of all registered metrics.
	List(ctx context.Context) ([]string, error)
	// Get returns the metric identified by name.
	Get(ctx context.Context, name string) (*EvalMetric, error)
	// Add registers a metric. Re-adding the same name replaces it.
	Add(ctx context.Context, m *EvalMetric) error
	// Close releases resources held by the manager.
	Close() error
}

// ErrNotFound is returned by Manager.Get when no metric exists for the given name.
var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "metric: not found" }

func (errNotFound) Is(target error) bool {
	_, ok := target.(errNotFound)
	return ok
}
