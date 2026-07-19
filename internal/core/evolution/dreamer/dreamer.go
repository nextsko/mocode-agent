// Package dreamer consolidates episodic conversation into long-term facts
// during idle time.
//
// Modeled after MiMo-Code /dream and grok-build /flush: we wait for the
// session to be idle (DreamMinInterval since the last dream), then walk
// the most recent turns and extract durable facts.
//
// Like Reviewer, the Dreamer is fail-open and never blocks the main
// conversation. The Dreamer is more aggressive than the Reviewer in two
// ways:
//
//   - it scans a larger window (DreamBatchSize, default 50 turns)
//   - it runs only when the session has been quiet for DreamMinInterval
//     (default 30m), so the LLM cost is amortized over long sessions
package dreamer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/evolution/api"
	"github.com/nextsko/mocode-agent/internal/core/evolution/distillation"
	msgpkg "github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// Dreamer is the per-app+user idle consolidation agent. Safe for
// concurrent Tick / Shutdown calls.
type Dreamer struct {
	deps  api.Deps
	scope Scope

	mu         sync.Mutex
	lastDream  time.Time
	turnsAtLast time.Time
	closed     bool

	inflightWG sync.WaitGroup
}

// Scope pins the (appName, userID) pair — same shape as Reviewer.
type Scope struct {
	AppName string
	UserID  string
}

// New constructs a Dreamer.
func New(deps api.Deps, scope Scope) (*Dreamer, error) {
	if deps.Memory == nil || deps.SmallModel == nil {
		return nil, fmt.Errorf("dreamer: Deps.Memory and Deps.SmallModel are required")
	}
	if scope.AppName == "" {
		scope.AppName = "mocode"
	}
	filled, err := deps.WithDefaults()
	if err != nil {
		return nil, err
	}
	return &Dreamer{deps: filled, scope: scope}, nil
}

// GateResult tells the caller whether the dreamer is ready to run.
type GateResult struct {
	ShouldRun bool
	Reason    string // "disabled" | "min_interval" | "min_turns" | "first_run" | "ok"
}

// Gate is the cheap, side-effect-free predicate. Callers (the
// coordinator's idle loop) call this every tick.
func (d *Dreamer) Gate(turnCount int) GateResult {
	r := GateResult{}
	if d.deps.Config.DreamEnabled == false {
		r.Reason = "disabled"
		return r
	}
	d.mu.Lock()
	lastDream := d.lastDream
	d.mu.Unlock()
	if !lastDream.IsZero() && d.deps.Now().Sub(lastDream) < d.deps.Config.DreamMinInterval {
		r.Reason = "min_interval"
		return r
	}
	if turnCount < d.deps.Config.DreamMinTurns {
		r.Reason = "min_turns"
		return r
	}
	r.ShouldRun = true
	r.Reason = "ok"
	return r
}

// TickIdle is called from the coordinator's idle loop. When the gate
// opens, it spawns a background dream. Mirrors Reviewer.AfterTurn's
// pattern: fire-and-forget, fail-open.
func (d *Dreamer) TickIdle(turnCount int) {
	gate := d.Gate(turnCount)
	if !gate.ShouldRun {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.lastDream = d.deps.Now()
	d.inflightWG.Add(1)
	d.mu.Unlock()

	go func() {
		defer d.inflightWG.Done()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Debug("dreamer: panic recovered", "panic", rec)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), d.deps.Config.DreamTimeout)
		defer cancel()
		res, err := d.runDream(ctx)
		if err != nil {
			slog.Debug("dreamer: dream failed", "user", d.scope.UserID, "err", err)
			return
		}
		d.logResult(res)
	}()
}

// RunNow triggers a dream synchronously and returns the result. Used by
// tests and admin commands.
func (d *Dreamer) RunNow(ctx context.Context) (distillation.PipelineResult, error) {
	return d.runDream(ctx)
}

// runDream reads the most recent DreamBatchSize messages from the latest
// session and runs the distillation pipeline over them.
func (d *Dreamer) runDream(ctx context.Context) (distillation.PipelineResult, error) {
	if d.deps.Messages == nil || d.deps.Sessions == nil {
		return distillation.PipelineResult{}, fmt.Errorf("dreamer: Messages and Sessions are required")
	}

	msgs, err := d.recentMessages(ctx)
	if err != nil {
		return distillation.PipelineResult{}, fmt.Errorf("dreamer: read recent messages: %w", err)
	}
	if len(msgs) == 0 {
		return distillation.PipelineResult{}, nil
	}

	pipe, err := d.buildPipeline()
	if err != nil {
		return distillation.PipelineResult{}, err
	}
	return pipe.Run(ctx, msgs)
}

// recentMessages walks up to DreamBatchSize messages from the user's
// latest session.
func (d *Dreamer) recentMessages(ctx context.Context) ([]distillation.MessageInput, error) {
	sessions, err := d.deps.Sessions.List(ctx)
	if err != nil {
		return nil, err
	}
	var latestID string
	var latestUpdated int64
	for _, s := range sessions {
		if s.UpdatedAt > latestUpdated {
			latestUpdated = s.UpdatedAt
			latestID = s.ID
		}
	}
	if latestID == "" {
		return nil, nil
	}
	domainMsgs, err := d.deps.Messages.List(ctx, latestID)
	if err != nil {
		return nil, err
	}

	batchSize := d.deps.Config.DreamBatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	if len(domainMsgs) > batchSize {
		domainMsgs = domainMsgs[len(domainMsgs)-batchSize:]
	}

	out := make([]distillation.MessageInput, 0, len(domainMsgs))
	for _, m := range domainMsgs {
		out = append(out, distillation.MessageInput{
			ID:      m.ID,
			Role:    roleString(m.Role),
			Content: m.Content().Text,
		})
	}
	return out, nil
}

func roleString(r msgpkg.MessageRole) string {
	switch r {
	case msgpkg.User:
		return "user"
	case msgpkg.Assistant:
		return "assistant"
	default:
		return "other"
	}
}

func (d *Dreamer) buildPipeline() (*distillation.Pipeline, error) {
	ext := distillation.NewExtractor(d.deps.SmallModel, d.deps.Config.DreamMaxToks)
	ded := distillation.NewDeduper(d.deps.Memory, distillation.DeduperConfig{
		AppName:          d.scope.AppName,
		UserID:           d.scope.UserID,
		JaccardThreshold: d.deps.Config.DedupJaccardThreshold,
		CosineThreshold:  d.deps.Config.DedupCosineThreshold,
	})
	wb := distillation.NewWriteback(d.deps.Memory, distillation.WritebackConfig{
		AppName:       d.scope.AppName,
		UserID:        d.scope.UserID,
		MinConfidence: d.deps.Config.MinConfidence,
	})
	return distillation.NewPipeline(ext, ded, wb), nil
}

func (d *Dreamer) logResult(res distillation.PipelineResult) {
	fields := map[string]string{
		"extracted":   fmt.Sprintf("%d", res.Extracted),
		"surviving":   fmt.Sprintf("%d", res.Surviving),
		"written":     fmt.Sprintf("%d", res.Written),
		"skipped_low": fmt.Sprintf("%d", res.SkippedLow),
		"dropped":     fmt.Sprintf("%d", len(res.Dropped)),
		"errors":      fmt.Sprintf("%d", len(res.Errors)),
	}
	if err := d.deps.Recorder.RecordEvolutionEvent(
		d.scope.UserID, api.PhaseDream, "dream_completed", fields,
	); err != nil {
		slog.Debug("dreamer: recorder failed", "err", err)
	}
}

// Shutdown waits for in-flight dreams to drain.
func (d *Dreamer) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.inflightWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
