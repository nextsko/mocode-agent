// Package evaluation is the top-level entry point for running agent evaluations.
//
// An Evaluator drives the full loop: for each run (1..numRuns), run inference
// over every case, score it with the configured metrics, and aggregate the
// results into a single EvalSetResult with a multi-run Summary.
package evaluation

import (
	"context"
	"fmt"

	"github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/internal/core/evaluation/criterion"
	"github.com/package-register/mocode/internal/core/evaluation/evalset"
	"github.com/package-register/mocode/internal/core/evaluation/metric"
	"github.com/package-register/mocode/internal/core/evaluation/result"
	"github.com/package-register/mocode/internal/core/evaluation/service"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
)

// Evaluator runs an evaluation set against an agent.
type Evaluator interface {
	// Evaluate runs numRuns inference passes over the set and returns the
	// aggregated result. The returned EvalSetResult always has a Summary when
	// numRuns > 1.
	Evaluate(ctx context.Context, evalSetID string) (*result.EvalSetResult, error)
	// Close releases owned resources.
	Close() error
}

// Option configures an Evaluator.
type Option func(*options)

type options struct {
	numRuns   int
	metrics   []*metric.EvalMetric
	metricMgr metric.Manager
	judger    criterion.Judger
	evalSets  evalset.Manager
	sessions  session.Service
	messages  message.Service
	svc       service.Service
}

// WithNumRuns sets the number of inference runs per case (default 1).
func WithNumRuns(n int) Option {
	return func(o *options) {
		if n > 0 {
			o.numRuns = n
		}
	}
}

// WithMetrics binds the metrics to apply to every case.
// When set, these take precedence over WithMetricManager.
func WithMetrics(ms ...*metric.EvalMetric) Option {
	return func(o *options) { o.metrics = ms }
}

// WithMetricManager supplies metrics by name from a manager.
func WithMetricManager(m metric.Manager) Option {
	return func(o *options) { o.metricMgr = m }
}

// WithJudger injects the LLM judge used by LLMJudge criteria.
func WithJudger(j criterion.Judger) Option {
	return func(o *options) { o.judger = j }
}

// WithEvalSetManager supplies the eval set store. Required unless WithService
// provides a fully wired service.
func WithEvalSetManager(m evalset.Manager) Option {
	return func(o *options) { o.evalSets = m }
}

// WithSessions supplies the session store for inference. Required for inference.
func WithSessions(s session.Service) Option {
	return func(o *options) { o.sessions = s }
}

// WithMessages supplies the message store for trace capture. Required for inference.
func WithMessages(m message.Service) Option {
	return func(o *options) { o.messages = m }
}

// WithService injects a pre-built evaluation service, bypassing local wiring.
func WithService(s service.Service) Option {
	return func(o *options) { o.svc = s }
}

// New builds an Evaluator that runs the agent against evaluation sets.
//
// The agent is the system under test. Inference reuses mocode's existing
// SessionAgent.Run plus the session/message stores; scoring reuses the
// criterion layer. A single numRuns > 1 produces a multi-run Summary.
func New(a agent.SessionAgent, opts ...Option) (Evaluator, error) {
	if a == nil {
		return nil, fmt.Errorf("evaluation: agent is required")
	}
	o := &options{numRuns: 1}
	for _, opt := range opts {
		opt(o)
	}
	if o.evalSets == nil {
		return nil, fmt.Errorf("evaluation: eval set manager is required")
	}
	svc := o.svc
	if svc == nil {
		if o.sessions == nil || o.messages == nil {
			return nil, fmt.Errorf("evaluation: Sessions and Messages are required for inference")
		}
		var err error
		svc, err = service.NewLocal(service.LocalOptions{
			EvalSets: o.evalSets,
			Sessions: o.sessions,
			Messages: o.messages,
		})
		if err != nil {
			return nil, err
		}
	}
	return &evaluator{agent: a, opts: o, svc: svc}, nil
}

type evaluator struct {
	agent agent.SessionAgent
	opts  *options
	svc   service.Service
}

func (e *evaluator) Close() error { return e.svc.Close() }

func (e *evaluator) Evaluate(ctx context.Context, evalSetID string) (*result.EvalSetResult, error) {
	metrics, err := e.resolveMetrics(ctx)
	if err != nil {
		return nil, err
	}
	es, err := e.opts.evalSets.Get(ctx, evalSetID)
	if err != nil {
		return nil, fmt.Errorf("get eval set %q: %w", evalSetID, err)
	}
	totalCases := len(es.Cases)

	allCaseResults := make([]*result.CaseResult, 0, totalCases*e.opts.numRuns)
	for runID := 1; runID <= e.opts.numRuns; runID++ {
		infResults, err := e.svc.Inference(ctx, &service.InferenceRequest{
			EvalSetID: evalSetID,
			Agent:     e.agent,
		})
		if err != nil {
			return nil, fmt.Errorf("run %d inference: %w", runID, err)
		}
		runResult, err := e.svc.Evaluate(ctx, &service.EvaluateRequest{
			EvalSetID:        evalSetID,
			InferenceResults: infResults,
			Metrics:          metrics,
			RunID:            runID,
			Judger:           e.opts.judger,
		})
		if err != nil {
			return nil, fmt.Errorf("run %d evaluate: %w", runID, err)
		}
		allCaseResults = append(allCaseResults, runResult.CaseResults...)
	}

	out := &result.EvalSetResult{
		EvalSetID:   evalSetID,
		CaseResults: allCaseResults,
		Summary:     result.Summarize(allCaseResults, totalCases, e.opts.numRuns),
	}
	return out, nil
}

func (e *evaluator) resolveMetrics(ctx context.Context) ([]*metric.EvalMetric, error) {
	if len(e.opts.metrics) > 0 {
		return e.opts.metrics, nil
	}
	if e.opts.metricMgr != nil {
		names, err := e.opts.metricMgr.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list metrics: %w", err)
		}
		ms := make([]*metric.EvalMetric, 0, len(names))
		for _, name := range names {
			m, err := e.opts.metricMgr.Get(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("get metric %q: %w", name, err)
			}
			ms = append(ms, m)
		}
		return ms, nil
	}
	return nil, fmt.Errorf("evaluation: no metrics configured (use WithMetrics or WithMetricManager)")
}
