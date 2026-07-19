package distillation

import (
	"context"
	"fmt"
	"time"

	memcore "github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
	"github.com/nextsko/mocode-agent/internal/core/evolution/fact"
)

// Writeback persists surviving facts to the existing memory.Service.
//
// Inspired by hermes-agent's MemoryStore "frozen snapshot" pattern: we
// accept that the snapshot of MEMORY.md won't change mid-session, and we
// therefore prefer to *append* durable facts that take effect on the
// next session reload (goclaw consolidation-style promotion from L0 to
// L1 — episodic events become long-term facts here).
//
// Failure mode: AddMemory errors are returned to the caller. The caller
// (Reviewer/Dreamer) is responsible for fail-open semantics at its
// boundary. We do NOT retry — retry risks quota exhaustion.
type Writeback struct {
	mem              memcore.Service
	app              string
	user             string
	minConfidence    float64
	nowFn            func() time.Time
}

// WritebackConfig holds the small set of knobs.
type WritebackConfig struct {
	AppName        string
	UserID         string
	MinConfidence  float64
}

// NewWriteback constructs a Writeback.
func NewWriteback(mem memcore.Service, cfg WritebackConfig) *Writeback {
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.5
	}
	return &Writeback{
		mem:           mem,
		app:           cfg.AppName,
		user:          cfg.UserID,
		minConfidence: cfg.MinConfidence,
		nowFn:         time.Now,
	}
}

// WriteResult is the structured output. Stats in-line (goclaw PruneStats
// pattern).
type WriteResult struct {
	Written      int               `json:"written"`
	SkippedLow   int               `json:"skipped_low_confidence"`
	Errors       []WriteError      `json:"errors,omitempty"`
	WrittenItems []WrittenFactInfo `json:"written_items,omitempty"`
}

// WriteError captures per-fact write failures so the caller can log them.
type WriteError struct {
	Content string `json:"content"`
	Reason  string `json:"reason"`
}

// WrittenFactInfo records what got persisted, so the caller can later
// echo provenance into sessionlog.
type WrittenFactInfo struct {
	Content string `json:"content"`
	Kind    string `json:"kind"`
	Topics  []string `json:"topics,omitempty"`
}

// Write persists the surviving facts.
//
// The function continues on per-fact errors and returns them in
// WriteResult.Errors — this matches the goclaw consolidation pattern
// where worker failures must not abort other workers.
func (w *Writeback) Write(ctx context.Context, facts []fact.Fact) (WriteResult, error) {
	out := WriteResult{}
	if len(facts) == 0 {
		return out, nil
	}

	for _, f := range facts {
		if f.Confidence < w.minConfidence {
			out.SkippedLow++
			continue
		}
		kind := f.Kind.ToMemoryKind()

		// Episode facts get an explicit event time; facts do not.
		var eventTime *time.Time
		if kind == memcore.KindEpisode {
			t := f.At
			eventTime = &t
		}

		if err := w.mem.AddMemory(ctx, w.app, w.user, f.Content, f.Topics, kind, eventTime, nil, ""); err != nil {
			out.Errors = append(out.Errors, WriteError{
				Content: f.Content,
				Reason:  err.Error(),
			})
			continue
		}
		out.Written++
		out.WrittenItems = append(out.WrittenItems, WrittenFactInfo{
			Content: f.Content,
			Kind:    string(f.Kind),
			Topics:  f.Topics,
		})
	}
	return out, nil
}

// Pipeline is the thin convenience wrapper that chains Extractor →
// Deduper → Writeback. Used by Reviewer and Dreamer.
type Pipeline struct {
	ext  *Extractor
	ded  *Deduper
	wb   *Writeback
}

// NewPipeline builds a Pipeline with the same (app, user) scope for both
// dedup and writeback.
func NewPipeline(ext *Extractor, ded *Deduper, wb *Writeback) *Pipeline {
	return &Pipeline{ext: ext, ded: ded, wb: wb}
}

// PipelineResult bundles the full stats for one distillation run. Returned
// to the caller for sessionlog emission.
type PipelineResult struct {
	Extracted int                 `json:"extracted"`
	Surviving int                 `json:"surviving"`
	Dropped   []DedupeDropReason  `json:"dropped,omitempty"`
	Written   int                 `json:"written"`
	SkippedLow int                `json:"skipped_low_confidence"`
	Errors    []WriteError        `json:"errors,omitempty"`
}

// Run executes the full pipeline.
func (p *Pipeline) Run(ctx context.Context, messages []MessageInput) (PipelineResult, error) {
	var result PipelineResult

	facts, err := p.ext.Extract(ctx, messages)
	if err != nil {
		return result, fmt.Errorf("pipeline: extract: %w", err)
	}
	result.Extracted = len(facts)

	dedup, err := p.ded.Dedupe(ctx, facts)
	if err != nil {
		// Dedup fail-open: continue with surviving slice as-is.
		result.Surviving = len(dedup.Surviving)
	} else {
		result.Surviving = len(dedup.Surviving)
		result.Dropped = dedup.Dropped
	}

	// If dedup failed and surviving is empty, we still try to write facts
	// (fail-open). Otherwise we use the surviving slice.
	toWrite := dedup.Surviving
	if len(toWrite) == 0 && err != nil && len(facts) > 0 {
		toWrite = facts
	}

	wr, werr := p.wb.Write(ctx, toWrite)
	if werr != nil {
		return result, fmt.Errorf("pipeline: write: %w", werr)
	}
	result.Written = wr.Written
	result.SkippedLow = wr.SkippedLow
	result.Errors = wr.Errors
	return result, nil
}