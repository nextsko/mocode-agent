package evolutioncron

import (
	"context"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/evolution/api"
	memcore "github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
	memdom "github.com/nextsko/mocode-agent/internal/domain/memory"
)

// stubModel satisfies fantasy.LanguageModel. Tests for SuggestionEngine
// never invoke the model; the engine is fully deterministic on inputs.
type stubModel struct{}

func (stubModel) Provider() string { return "stub" }
func (stubModel) Model() string    { return "stub-1" }
func (stubModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return &fantasy.Response{}, nil
}
func (stubModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, nil
}
func (stubModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}
func (stubModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

var _ fantasy.LanguageModel = stubModel{}

// stubMem satisfies memcore.Service. The engine never calls into it.
type stubMem struct{}

func (stubMem) AddMemory(context.Context, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	return nil
}
func (stubMem) UpdateMemory(context.Context, string, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	return nil
}
func (stubMem) DeleteMemory(context.Context, string, string, string) error { return nil }
func (stubMem) ClearMemories(context.Context, string, string) error       { return nil }
func (stubMem) ReadMemories(context.Context, string, string, int) ([]*memdom.Entry, error) {
	return nil, nil
}
func (stubMem) SearchMemories(context.Context, string, string, string, int) ([]*memdom.Entry, error) {
	return nil, nil
}
func (stubMem) Tools() []fantasy.AgentTool { return nil }
func (stubMem) Close() error               { return nil }

var _ memcore.Service = stubMem{}

func newEnabledEngine(t *testing.T) *Engine {
	t.Helper()
	eng, err := NewEngine(api.Deps{
		Memory:     stubMem{},
		SmallModel: stubModel{},
		Config: api.Config{
			Enabled:            true,
			AutoTuneEnabled:    true,
			AutoTuneMinDataPts: 50,
			AutoTuneMaxDelta:   0.1,
			AutoTuneInterval:   time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return eng
}

func TestAnalyzeDisabled(t *testing.T) {
	eng, _ := NewEngine(api.Deps{
		Memory:     stubMem{},
		SmallModel: stubModel{},
		Config:     api.Config{Enabled: true, AutoTuneEnabled: false},
	})
	got := eng.Analyze(map[string]Tunable{
		"recall_threshold": {Name: "recall_threshold", Current: 0.5, Min: 0.1, Max: 0.9, StepHint: 0.1},
	}, api.SnapshotMetrics{TotalQueries: 1000})
	if got != nil {
		t.Fatalf("expected no suggestions when AutoTune disabled, got %+v", got)
	}
}

func TestAnalyzeInsufficientData(t *testing.T) {
	eng := newEnabledEngine(t)
	got := eng.Analyze(map[string]Tunable{
		"recall_threshold": {Name: "recall_threshold", Current: 0.5, Min: 0.1, Max: 0.9, StepHint: 0.1},
	}, api.SnapshotMetrics{TotalQueries: 5})
	if got != nil {
		t.Fatalf("expected no suggestions under MinDataPts, got %+v", got)
	}
}

func TestAnalyzeLowersRecallThresholdOnLowUsage(t *testing.T) {
	eng := newEnabledEngine(t)
	current := map[string]Tunable{
		"recall_threshold": {Name: "recall_threshold", Current: 0.5, Min: 0.1, Max: 0.9, StepHint: 0.1},
	}
	got := eng.Analyze(current, api.SnapshotMetrics{
		TotalQueries: 200,
		RecallUsage:  10, // 10/200 = 0.05
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(got), got)
	}
	s := got[0]
	if s.Proposed >= s.Tunable.Current {
		t.Fatalf("proposed %v should be < current %v", s.Proposed, s.Tunable.Current)
	}
	if s.Reason == "" || s.MetricName != "recall_usage_rate" {
		t.Fatalf("unexpected reason/metric: %+v", s)
	}
}

func TestAnalyzeRaisesTopKOnLowHitRate(t *testing.T) {
	eng := newEnabledEngine(t)
	current := map[string]Tunable{
		"injection_top_k": {Name: "injection_top_k", Current: 5, Min: 3, Max: 15, StepHint: 2},
	}
	got := eng.Analyze(current, api.SnapshotMetrics{
		TotalQueries: 200,
		RecallUsage:  100,
		RecallHits:   10, // hit rate = 0.1 < 0.3
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(got), got)
	}
	s := got[0]
	if s.Proposed <= s.Tunable.Current {
		t.Fatalf("proposed %v should be > current %v", s.Proposed, s.Tunable.Current)
	}
	if s.Proposed != 7 {
		t.Fatalf("expected proposed=7 (current+2), got %v", s.Proposed)
	}
}

func TestAnalyzeRespectsBounds(t *testing.T) {
	eng := newEnabledEngine(t)
	current := map[string]Tunable{
		"recall_threshold": {Name: "recall_threshold", Current: 0.15, Min: 0.1, Max: 0.9, StepHint: 0.1},
	}
	got := eng.Analyze(current, api.SnapshotMetrics{TotalQueries: 200, RecallUsage: 5})
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(got))
	}
	if got[0].Proposed != 0.1 {
		t.Fatalf("expected clamp to 0.1, got %v", got[0].Proposed)
	}
}

func TestApplyAndRollback(t *testing.T) {
	eng := newEnabledEngine(t)
	got := eng.Analyze(map[string]Tunable{
		"recall_threshold": {Name: "recall_threshold", Current: 0.5, Min: 0.1, Max: 0.9},
	}, api.SnapshotMetrics{TotalQueries: 200, RecallUsage: 5})
	if len(got) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(got))
	}
	id := got[0].ID
	if err := eng.Apply(id, time.Now()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if p := eng.PendingSuggestions(); len(p) != 0 {
		t.Fatalf("expected 0 pending after apply, got %d", len(p))
	}
	if err := eng.Rollback(id); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestCronEmitsOnTick(t *testing.T) {
	eng := newEnabledEngine(t)
	var emitted []Suggestion
	cron := NewCron(eng, func(s Suggestion) { emitted = append(emitted, s) })
	cron.runOnce(map[string]Tunable{
		"recall_threshold": {Name: "recall_threshold", Current: 0.5, Min: 0.1, Max: 0.9},
	})
	if len(emitted) != 0 {
		t.Fatalf("expected 0 suggestions with empty snapshot, got %d", len(emitted))
	}
}