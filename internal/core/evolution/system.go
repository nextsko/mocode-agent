package evolution

import (
	"context"
	"fmt"
	"sync"

	"github.com/nextsko/mocode-agent/internal/core/evolution/distillation"
	"github.com/nextsko/mocode-agent/internal/core/evolution/dreamer"
	"github.com/nextsko/mocode-agent/internal/core/evolution/evolutioncron"
	"github.com/nextsko/mocode-agent/internal/core/evolution/fact"
	"github.com/nextsko/mocode-agent/internal/core/evolution/reviewer"
)

// System is the top-level handle the application code wires into the
// coordinator. It owns the per-app+user Reviewer and Dreamer and the
// shared EvolutionCron engine.
//
// One System per (app, user) pair. Construct via New.
type System struct {
	deps     Deps
	scope    Scope
	reviewer *reviewer.Reviewer
	dreamer  *dreamer.Dreamer
	engine   *evolutioncron.Engine
	cron     *evolutioncron.Cron

	mu      sync.Mutex
	cronCtx context.Context
	cancel  context.CancelFunc
}

// Scope pins the (appName, userID) pair the system operates on.
type Scope struct {
	AppName string
	UserID  string
}

// NewSystem builds the full evolution stack for one (app, user). Returns
// nil + nil when evolution is disabled in config (the opt-in default).
//
// On success the caller MUST call System.Shutdown during application
// teardown to drain in-flight goroutines.
func NewSystem(deps Deps, scope Scope) (*System, error) {
	if !deps.Config.Enabled {
		return nil, nil
	}
	filled, err := deps.WithDefaults()
	if err != nil {
		return nil, err
	}
	if scope.AppName == "" {
		scope.AppName = "mocode"
	}

	rev, err := reviewer.New(filled, reviewer.Scope(scope))
	if err != nil {
		return nil, fmt.Errorf("evolution: build reviewer: %w", err)
	}
	drm, err := dreamer.New(filled, dreamer.Scope(scope))
	if err != nil {
		_ = rev.Shutdown(context.Background())
		return nil, fmt.Errorf("evolution: build dreamer: %w", err)
	}
	engine, err := evolutioncron.NewEngine(filled)
	if err != nil {
		_ = rev.Shutdown(context.Background())
		_ = drm.Shutdown(context.Background())
		return nil, fmt.Errorf("evolution: build engine: %w", err)
	}
	cron := evolutioncron.NewCron(engine, nil)

	return &System{
		deps:     filled,
		scope:    scope,
		reviewer: rev,
		dreamer:  drm,
		engine:   engine,
		cron:     cron,
	}, nil
}

// AfterTurn is the cheap hook called from the coordinator's turn-end.
// Triggers the per-turn background review cadence.
func (s *System) AfterTurn(turnNum int) {
	if s == nil {
		return
	}
	s.reviewer.AfterTurn(turnNum)
}

// TickIdle is the periodic hook from the coordinator's idle loop.
// Triggers the idle dream cadence.
func (s *System) TickIdle(turnCount int) {
	if s == nil {
		return
	}
	s.dreamer.TickIdle(turnCount)
}

// StartCron starts the auto-tune cron loop. Returns immediately; the
// loop runs in a goroutine. Call Shutdown to stop.
func (s *System) StartCron(parent context.Context, current map[string]evolutioncron.Tunable) {
	if s == nil || !s.deps.Config.AutoTuneEnabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cronCtx != nil {
		return // already started
	}
	s.cronCtx, s.cancel = context.WithCancel(parent)
	go s.cron.Start(s.cronCtx, current)
}

// Engine exposes the underlying suggestion engine so admin tooling can
// read pending suggestions, apply, or roll back.
func (s *System) Engine() *evolutioncron.Engine {
	if s == nil {
		return nil
	}
	return s.engine
}

// Reviewer exposes the reviewer for tests and admin-triggered runs.
func (s *System) Reviewer() *reviewer.Reviewer {
	if s == nil {
		return nil
	}
	return s.reviewer
}

// Dreamer exposes the dreamer for tests and admin-triggered runs.
func (s *System) Dreamer() *dreamer.Dreamer {
	if s == nil {
		return nil
	}
	return s.dreamer
}

// Pipeline exposes the underlying distillation pipeline so tests can
// drive it directly without going through reviewer/dreamer.
func (s *System) Pipeline() (*distillation.Pipeline, error) {
	if s == nil {
		return nil, fmt.Errorf("evolution: nil system")
	}
	ext := distillation.NewExtractor(s.deps.SmallModel, s.deps.Config.ReviewMaxToks)
	ded := distillation.NewDeduper(s.deps.Memory, distillation.DeduperConfig{
		AppName:          s.scope.AppName,
		UserID:           s.scope.UserID,
		JaccardThreshold: s.deps.Config.DedupJaccardThreshold,
		CosineThreshold:  s.deps.Config.DedupCosineThreshold,
	})
	wb := distillation.NewWriteback(s.deps.Memory, distillation.WritebackConfig{
		AppName:       s.scope.AppName,
		UserID:        s.scope.UserID,
		MinConfidence: s.deps.Config.MinConfidence,
	})
	return distillation.NewPipeline(ext, ded, wb), nil
}

// Shutdown drains in-flight reviews/dreams and stops the cron. Safe to
// call multiple times.
func (s *System) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.cronCtx = nil
	}
	s.mu.Unlock()

	if err := s.reviewer.Shutdown(ctx); err != nil {
		return err
	}
	if err := s.dreamer.Shutdown(ctx); err != nil {
		return err
	}
	return nil
}

// Suppress "unused import" warnings during refactors. fact is re-exported
// for downstream callers that want the canonical import path.
var _ = fact.KindUserPref