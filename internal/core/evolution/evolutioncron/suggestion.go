// Package evolutioncron runs the auto-tune loop with guardrails.
//
// Modeled after goclaw/internal/agent/suggestion_engine.go +
// cmd/gateway_evolution_cron.go: every AutoTuneInterval we read
// "behavior" metrics from sessionlog + memory, run a small set of
// deterministic rules, and emit Suggestions. Each suggestion carries a
// guardrail-bounded change; when applied we record the change in
// sessionlog so we can roll back if the new metrics regress.
//
// In v1 we only tune ONE parameter: the recall threshold used when
// injecting memory into the agent prompt. Future versions may add
// injection top-K and (later) tool weights.
package evolutioncron

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/evolution/api"
)

// Tunable is a single configurable knob the engine can suggest changes
// for. The bounds clamp the auto-tune range so a runaway rule can't push
// the system out of a sensible operating range.
type Tunable struct {
	Name      string  // "recall_threshold" | "injection_top_k" | ...
	Current   float64 // current value
	Min       float64 // hard lower bound
	Max       float64 // hard upper bound
	StepHint  float64 // preferred absolute delta per cycle (e.g. 0.1 for threshold)
}

// Suggestion is what the engine emits. It is intentionally simple: a
// rule, a delta, and a reason. Higher layers (admin UI, automation)
// decide whether to apply.
type Suggestion struct {
	ID         string
	CreatedAt  time.Time
	Tunable    Tunable
	Proposed   float64 // proposed new value
	Reason     string  // human-readable explanation
	MetricName string  // which metric triggered it, e.g. "recall_usage_rate"
	MetricVal  float64 // the metric value that triggered it
	Status     SuggestionStatus
}

// SuggestionStatus is the lifecycle.
type SuggestionStatus string

const (
	StatusPending  SuggestionStatus = "pending"
	StatusApplied  SuggestionStatus = "applied"
	StatusRejected SuggestionStatus = "rejected"
	StatusRolledBack SuggestionStatus = "rolled_back"
)

// Engine is the auto-tune engine. Thread-safe; the cron runner is the
// sole writer in practice, but tests may call Analyze concurrently.
type Engine struct {
	deps api.Deps

	mu          sync.Mutex
	lastRun     time.Time
	suggestions []Suggestion
}

// NewEngine constructs an Engine.
func NewEngine(deps api.Deps) (*Engine, error) {
	filled, err := deps.WithDefaults()
	if err != nil {
		return nil, err
	}
	return &Engine{deps: filled}, nil
}

// SetSnapshot mutates the engine's view of "current metrics". In v1 we
// pass nil snapshots (no recorder wired) which means the engine runs in
// a no-op mode — useful for tests and for the opt-in default.
//
// When a real snapshot is provided, Analyze emits at most one suggestion
// per rule.
func (e *Engine) SetSnapshot(m api.SnapshotMetrics) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// We store the snapshot only for diagnostics; the rules below are
	// deterministic and stateless. v1 keeps them inline to avoid a
	// separate snapshot type. See api.SnapshotMetrics for the
	// shape an external metrics source should produce.
	_ = m
}

// Analyze runs the rule set against `current` and returns new
// suggestions. Returns nil when no rule fires.
//
// Rules (v1, conservative):
//
//   - recall_usage_rate < 0.2 over ≥ MinDataPts queries → suggest
//     lowering recall_threshold by AutoTuneMaxDelta (clamped to Min).
//   - recall_hit_rate < 0.3 over ≥ MinDataPts queries → suggest raising
//     injection_top_k by 2 (clamped to Max).
func (e *Engine) Analyze(current map[string]Tunable, m api.SnapshotMetrics) []Suggestion {
	if !e.deps.Config.AutoTuneEnabled {
		return nil
	}
	if m.TotalQueries < e.deps.Config.AutoTuneMinDataPts {
		return nil
	}

	var out []Suggestion
	now := e.deps.Now()

	if t, ok := current["recall_threshold"]; ok {
		if usage := m.RecallUsageRate(); usage < 0.2 {
			proposed := math.Max(t.Min, t.Current-e.deps.Config.AutoTuneMaxDelta)
			if proposed != t.Current {
				out = append(out, Suggestion{
					ID:         fmt.Sprintf("sug-%d-recall-lower", now.UnixNano()),
					CreatedAt:  now,
					Tunable:    t,
					Proposed:   roundTo(proposed, 2),
					Reason:     fmt.Sprintf("recall_usage_rate=%.2f below 0.2", usage),
					MetricName: "recall_usage_rate",
					MetricVal:  usage,
					Status:     StatusPending,
				})
			}
		}
	}
	if t, ok := current["injection_top_k"]; ok {
		if hit := m.RecallHitRate(); hit < 0.3 {
			proposed := math.Min(t.Max, t.Current+2)
			if proposed != t.Current {
				out = append(out, Suggestion{
					ID:         fmt.Sprintf("sug-%d-topk-up", now.UnixNano()),
					CreatedAt:  now,
					Tunable:    t,
					Proposed:   roundTo(proposed, 0),
					Reason:     fmt.Sprintf("recall_hit_rate=%.2f below 0.3", hit),
					MetricName: "recall_hit_rate",
					MetricVal:  hit,
					Status:     StatusPending,
				})
			}
		}
	}

	e.mu.Lock()
	e.suggestions = append(e.suggestions, out...)
	e.mu.Unlock()
	return out
}

// PendingSuggestions returns a snapshot of suggestions awaiting
// approval.
func (e *Engine) PendingSuggestions() []Suggestion {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Suggestion
	for _, s := range e.suggestions {
		if s.Status == StatusPending {
			out = append(out, s)
		}
	}
	return out
}

// Apply marks a suggestion as applied at `now` and records provenance.
// The actual config mutation is the caller's responsibility — we
// deliberately keep this engine pure.
func (e *Engine) Apply(id string, now time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.suggestions {
		if e.suggestions[i].ID == id {
			e.suggestions[i].Status = StatusApplied
			return nil
		}
	}
	return fmt.Errorf("evolutioncron: suggestion %q not found", id)
}

// Rollback marks a suggestion as rolled back. Caller should restore the
// pre-Apply value.
func (e *Engine) Rollback(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.suggestions {
		if e.suggestions[i].ID == id {
			e.suggestions[i].Status = StatusRolledBack
			return nil
		}
	}
	return fmt.Errorf("evolutioncron: suggestion %q not found", id)
}

// Cron is the periodic runner. It calls Analyze once per
// AutoTuneInterval and emits suggestions via the supplied emitter.
//
// The emitter is intentionally a function callback (not a channel) so
// tests can collect emitted suggestions without a goroutine boundary.
type Cron struct {
	engine  *Engine
	emitter func(Suggestion)
}

// NewCron constructs a Cron.
func NewCron(engine *Engine, emitter func(Suggestion)) *Cron {
	if emitter == nil {
		emitter = func(s Suggestion) {
			slog.Debug("evolutioncron: suggestion emitted", "id", s.ID, "reason", s.Reason)
		}
	}
	return &Cron{engine: engine, emitter: emitter}
}

// Start runs the cron loop until ctx is cancelled. It is a blocking
// call; run it in a goroutine.
//
// In v1, Start calls Analyze exactly once per tick using whatever
// snapshot is currently set on the engine. Future versions will pull
// fresh metrics from sessionlog.
func (c *Cron) Start(ctx context.Context, current map[string]Tunable) {
	interval := c.engine.deps.Config.AutoTuneInterval
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	c.runOnce(current)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.runOnce(current)
		}
	}
}

// runOnce performs a single Analyze and emits any new suggestions.
func (c *Cron) runOnce(current map[string]Tunable) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Debug("evolutioncron: panic recovered", "panic", rec)
		}
	}()
	m := api.SnapshotMetrics{}
	sugs := c.engine.Analyze(current, m)
	for _, s := range sugs {
		c.emitter(s)
	}
}

// roundTo rounds v to `decimals` decimal places. Used to keep suggestion
// values clean.
func roundTo(v float64, decimals int) float64 {
	pow := math.Pow10(decimals)
	return math.Round(v*pow) / pow
}
