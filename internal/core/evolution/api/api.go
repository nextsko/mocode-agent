// Package api holds the cross-package types that other evolution
// subpackages (reviewer, dreamer, evolutioncron) need. Split out to
// avoid an import cycle between the top-level evolution package and
// those subpackages.
package api

import (
	"fmt"
	"time"

	"charm.land/fantasy"

	memcore "github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
	memdom "github.com/nextsko/mocode-agent/internal/domain/memory"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// Phase is the lifecycle phase tag used in the SessionRecorder.
type Phase string

const (
	PhaseReview   Phase = "review"
	PhaseDream    Phase = "dream"
	PhaseEvolution Phase = "evolution"
)

// Deps bundles all external collaborators an evolution component may
// need. Lives in api (not the top-level evolution package) so the
// reviewer / dreamer / evolutioncron subpackages can import it without
// pulling in the System handle (which would create a cycle).
type Deps struct {
	Memory    memcore.Service
	Sessions  session.Service
	Messages  message.Service
	SmallModel fantasy.LanguageModel
	Recorder  SessionRecorder
	Config    Config
	Now       func() time.Time
}

// SessionRecorder persists provenance for evolution-side writes so an
// operator can audit "why did the agent remember this?".
type SessionRecorder interface {
	RecordEvolutionEvent(sessionID string, phase Phase, event string, fields map[string]string) error
}

// WithDefaults is exported so it can be called from any subpackage
// without circular-import worries.
func (d Deps) WithDefaults() (Deps, error) {
	if d.Memory == nil {
		return Deps{}, fmt.Errorf("evolution: Deps.Memory is required")
	}
	if d.SmallModel == nil {
		return Deps{}, fmt.Errorf("evolution: Deps.SmallModel is required")
	}
	if d.Recorder == nil {
		d.Recorder = nopRecorder{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	d.Config = d.Config.WithDefaults()
	return d, nil
}

// nopRecorder is the default when no recorder is wired.
type nopRecorder struct{}

func (nopRecorder) RecordEvolutionEvent(string, Phase, string, map[string]string) error {
	return nil
}

// Config is the typed view; lives here to keep one canonical place.
type Config struct {
	Enabled bool `yaml:"enabled"`

	ReviewEnabled  bool          `yaml:"review_enabled"`
	ReviewInterval int           `yaml:"review_interval"`
	ReviewTimeout  time.Duration `yaml:"review_timeout"`
	ReviewMaxToks  int64         `yaml:"review_max_tokens"`

	DreamEnabled     bool          `yaml:"dream_enabled"`
	DreamMinInterval time.Duration `yaml:"dream_min_interval"`
	DreamMinTurns    int           `yaml:"dream_min_turns"`
	DreamBatchSize   int           `yaml:"dream_batch_size"`
	DreamTimeout     time.Duration `yaml:"dream_timeout"`
	DreamMaxToks     int64         `yaml:"dream_max_tokens"`

	AutoTuneEnabled     bool          `yaml:"autotune_enabled"`
	AutoTuneInterval    time.Duration `yaml:"autotune_interval"`
	AutoTuneMinDataPts  int           `yaml:"autotune_min_data"`
	AutoTuneMaxDelta    float64       `yaml:"autotune_max_delta"`
	AutoTuneRollbackHrs time.Duration `yaml:"autotune_rollback_hours"`

	DedupJaccardThreshold float64 `yaml:"dedup_jaccard"`
	DedupCosineThreshold  float64 `yaml:"dedup_cosine"`
	MinConfidence         float64 `yaml:"min_confidence"`
}

// WithDefaults is the canonical defaulting entry point.
func (c Config) WithDefaults() Config {
	if c.ReviewInterval <= 0 {
		c.ReviewInterval = 10
	}
	if c.ReviewTimeout <= 0 {
		c.ReviewTimeout = 60 * time.Second
	}
	if c.ReviewMaxToks <= 0 {
		c.ReviewMaxToks = 1024
	}
	if c.DreamMinInterval <= 0 {
		c.DreamMinInterval = 30 * time.Minute
	}
	if c.DreamMinTurns <= 0 {
		c.DreamMinTurns = 20
	}
	if c.DreamBatchSize <= 0 {
		c.DreamBatchSize = 50
	}
	if c.DreamTimeout <= 0 {
		c.DreamTimeout = 5 * time.Minute
	}
	if c.DreamMaxToks <= 0 {
		c.DreamMaxToks = 2048
	}
	if c.AutoTuneInterval <= 0 {
		c.AutoTuneInterval = 6 * time.Hour
	}
	if c.AutoTuneMinDataPts <= 0 {
		c.AutoTuneMinDataPts = 100
	}
	if c.AutoTuneMaxDelta <= 0 {
		c.AutoTuneMaxDelta = 0.1
	}
	if c.AutoTuneRollbackHrs <= 0 {
		c.AutoTuneRollbackHrs = 24 * time.Hour
	}
	if c.DedupJaccardThreshold <= 0 {
		c.DedupJaccardThreshold = 0.85
	}
	if c.DedupCosineThreshold <= 0 {
		c.DedupCosineThreshold = 0.92
	}
	if c.MinConfidence <= 0 {
		c.MinConfidence = 0.5
	}
	return c
}

// SnapshotMetrics is the public shape of behavior metrics the evolution
// cron reads from sessionlog + memory stats.
type SnapshotMetrics struct {
	TotalQueries    int
	RecallUsage     int
	RecallHits      int
	AvgRecallScore  float64
	ToolFailureRate float64
	ToolUsageCount  int
	AvgTurnScore    float64
	WindowStart     time.Time
	WindowEnd       time.Time
}

func (m SnapshotMetrics) RecallUsageRate() float64 {
	if m.TotalQueries == 0 {
		return 0
	}
	return float64(m.RecallUsage) / float64(m.TotalQueries)
}

func (m SnapshotMetrics) RecallHitRate() float64 {
	if m.RecallUsage == 0 {
		return 0
	}
	return float64(m.RecallHits) / float64(m.RecallUsage)
}

// Avoid unused imports if memdom becomes optional in the future.
var _ = memdom.KindFact