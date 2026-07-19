// Package evolution is the self-evolution subsystem for mocode.
//
// It exposes the System handle (the wiring entry point) and re-exports
// the cross-package types from internal/core/evolution/api so callers
// only need to import this single package.
//
// See api.go (in the api subpackage) for the cross-package Deps /
// Phase / SnapshotMetrics types, and the subpackages for the actual
// implementations:
//
//   - distillation: Extractor / Deduper / Writeback / Pipeline
//   - fact:         ExtractedFact value type
//   - reviewer:     per-turn background review (every N turns)
//   - dreamer:      idle consolidation (every DreamMinInterval)
//   - evolutioncron: auto-tune cron with guardrails
//
// For the upstream survey that motivates this design, see
// docs/plans/self-evolution/00-upstream-survey.md.
package evolution

import "github.com/nextsko/mocode-agent/internal/core/evolution/api"

// Public re-exports so callers can write `evolution.Deps`, `evolution.Phase`,
// `evolution.SnapshotMetrics`, `evolution.Config` without importing the
// api subpackage directly.
type (
	// Phase is the lifecycle phase tag used in the SessionRecorder.
	Phase = api.Phase
	// Deps bundles all external collaborators an evolution component may
	// need. See api.Deps for field documentation.
	Deps = api.Deps
	// Config is the canonical configuration struct.
	Config = api.Config
	// SnapshotMetrics is the shape of behavior metrics the evolution cron
	// reads from sessionlog + memory stats.
	SnapshotMetrics = api.SnapshotMetrics
	// SessionRecorder persists provenance for evolution-side writes.
	SessionRecorder = api.SessionRecorder
)

const (
	PhaseReview    = api.PhaseReview
	PhaseDream     = api.PhaseDream
	PhaseEvolution = api.PhaseEvolution
)