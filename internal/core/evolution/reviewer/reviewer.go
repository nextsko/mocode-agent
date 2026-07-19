// Package reviewer implements the Background Review Agent.
//
// Modeled after hermes-agent/agent/background_review.py — every N turns,
// a detached "review agent" examines the recent conversation and writes
// durable user preferences / project rules to memory. The review never
// blocks the main conversation: it runs in its own goroutine and any
// failure is logged at debug level only.
package reviewer

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

// Reviewer is the per-app+user background review agent. It is safe for
// concurrent calls to AfterTurn — the inner counter and goroutine launch
// are guarded by a mutex.
//
// Lifecycle:
//
//	r := reviewer.New(deps)
//	coordinator.AfterTurn = r.AfterTurn       // every turn
//	coordinator.RunLoop      = r.TickIdle     // periodic idle sweep
//	r.Shutdown(ctx)                           // graceful drain
type Reviewer struct {
	deps      api.Deps
	scope     Scope

	mu         sync.Mutex
	turnCount  int
	turnsSince int
	lastReview time.Time

	// inflight tracks active review goroutines so Shutdown can drain.
	inflightWG sync.WaitGroup

	// closed is set by Shutdown so AfterTurn / TickIdle become no-ops.
	closed bool
}

// Scope pins the (appName, userID) pair the reviewer operates on. Each
// distinct user gets its own Reviewer instance — keeps state local and
// avoids cross-tenant leakage (goclaw scopeClause pattern).
type Scope struct {
	AppName string
	UserID  string
}

// New constructs a Reviewer.
func New(deps api.Deps, scope Scope) (*Reviewer, error) {
	if deps.Memory == nil || deps.SmallModel == nil {
		return nil, fmt.Errorf("reviewer: Deps.Memory and Deps.SmallModel are required")
	}
	if scope.AppName == "" {
		scope.AppName = "mocode"
	}
	filled, err := deps.WithDefaults()
	if err != nil {
		return nil, err
	}
	r := &Reviewer{
		deps:  filled,
		scope: scope,
	}
	return r, nil
}

// AfterTurn is the cheap path called from the coordinator's turn end.
// It bumps a counter and, when the counter crosses ReviewInterval,
// fires off a background review. Failures are swallowed; this method
// must never affect the main turn result.
//
// We do NOT take a long-lived context from the coordinator (the main
// turn's context is often cancelled the moment the response is sent
// back). Instead we run with context.Background, capped by ReviewTimeout.
func (r *Reviewer) AfterTurn(turnNum int) {
	r.mu.Lock()
	if r.closed || !r.deps.Config.ReviewEnabled {
		r.mu.Unlock()
		return
	}
	r.turnsSince++
	interval := r.deps.Config.ReviewInterval
	if interval <= 0 {
		interval = 10
	}
	if r.turnsSince < interval {
		r.mu.Unlock()
		return
	}
	// Gate by time too: even with enough turns, never re-review more
	// often than 1/minute. Avoids LLM-quota storms.
	if !r.lastReview.IsZero() && r.deps.Now().Sub(r.lastReview) < time.Minute {
		r.mu.Unlock()
		return
	}
	r.turnsSince = 0
	r.lastReview = r.deps.Now()
	r.inflightWG.Add(1)
	r.mu.Unlock()

	go func() {
		defer r.inflightWG.Done()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Debug("reviewer: panic recovered", "panic", rec)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), r.deps.Config.ReviewTimeout)
		defer cancel()
		res, err := r.runReview(ctx)
		if err != nil {
			slog.Debug("reviewer: review failed", "user", r.scope.UserID, "err", err)
			return
		}
		r.logResult("reviewer", res)
	}()
}

// TickIdle is a no-op placeholder kept for symmetry with Dreamer —
// currently the per-turn cadence handles all reviewer work.
func (r *Reviewer) TickIdle() {}

// RunNow triggers a review synchronously and returns the result. Used by
// tests and by future admin-triggered commands.
func (r *Reviewer) RunNow(ctx context.Context) (distillation.PipelineResult, error) {
	return r.runReview(ctx)
}

// runReview is the actual work: read recent messages, build pipeline,
// run it, log result. Pure function with respect to the reviewer state
// (mutates only counters via the deps.Recorder side effect).
func (r *Reviewer) runReview(ctx context.Context) (distillation.PipelineResult, error) {
	if r.deps.Messages == nil || r.deps.Sessions == nil {
		return distillation.PipelineResult{}, fmt.Errorf("reviewer: Messages and Sessions are required")
	}

	msgs, err := r.recentMessages(ctx)
	if err != nil {
		return distillation.PipelineResult{}, fmt.Errorf("reviewer: read recent messages: %w", err)
	}
	if len(msgs) == 0 {
		return distillation.PipelineResult{}, nil
	}

	pipe, err := r.buildPipeline()
	if err != nil {
		return distillation.PipelineResult{}, err
	}

	return pipe.Run(ctx, msgs)
}

// recentMessages walks the most-recent message list from the user's last
// session. We read up to MaxMessagesForExtraction messages and project
// them into the distillation.MessageInput shape.
func (r *Reviewer) recentMessages(ctx context.Context) ([]distillation.MessageInput, error) {
	sessions, err := r.deps.Sessions.List(ctx)
	if err != nil {
		return nil, err
	}
	// Pick the most recent session. The session.Service does not expose a
	// per-user filter; we filter in-memory. v1: only ever run review on
	// the most recent session overall.
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

	domainMsgs, err := r.deps.Messages.List(ctx, latestID)
	if err != nil {
		return nil, err
	}
	if len(domainMsgs) == 0 {
		return nil, nil
	}
	if len(domainMsgs) > distillation.MaxMessagesForExtraction {
		domainMsgs = domainMsgs[len(domainMsgs)-distillation.MaxMessagesForExtraction:]
	}

	out := make([]distillation.MessageInput, 0, len(domainMsgs))
	for _, m := range domainMsgs {
		out = append(out, distillation.MessageInput{
			ID:      m.ID,
			Role:    roleString(m.Role),
			Content: extractText(m),
		})
	}
	return out, nil
}

// roleString maps the message.Role enum to the lowercase string the LLM
// extractor expects.
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

// extractText pulls plain text out of a message.Message. We mirror the
// finalText convention used by llmselector.go so the LLM sees the same
// shape it would for a candidate ranking prompt.
func extractText(m msgpkg.Message) string {
	return m.Content().Text
}

// buildPipeline wires an Extractor + Deduper + Writeback against the
// reviewer's scope. We always use the small model — review is a cheap
// classification task.
func (r *Reviewer) buildPipeline() (*distillation.Pipeline, error) {
	ext := distillation.NewExtractor(r.deps.SmallModel, r.deps.Config.ReviewMaxToks)
	ded := distillation.NewDeduper(r.deps.Memory, distillation.DeduperConfig{
		AppName:          r.scope.AppName,
		UserID:           r.scope.UserID,
		JaccardThreshold: r.deps.Config.DedupJaccardThreshold,
		CosineThreshold:  r.deps.Config.DedupCosineThreshold,
	})
	wb := distillation.NewWriteback(r.deps.Memory, distillation.WritebackConfig{
		AppName:       r.scope.AppName,
		UserID:        r.scope.UserID,
		MinConfidence: r.deps.Config.MinConfidence,
	})
	return distillation.NewPipeline(ext, ded, wb), nil
}

// logResult writes a structured log entry tagged with phase=review.
// Failures here are silent — sessionlog is best-effort.
func (r *Reviewer) logResult(phase string, res distillation.PipelineResult) {
	fields := map[string]string{
		"extracted":         fmt.Sprintf("%d", res.Extracted),
		"surviving":         fmt.Sprintf("%d", res.Surviving),
		"written":           fmt.Sprintf("%d", res.Written),
		"skipped_low":       fmt.Sprintf("%d", res.SkippedLow),
		"dropped":           fmt.Sprintf("%d", len(res.Dropped)),
		"errors":            fmt.Sprintf("%d", len(res.Errors)),
	}
	if err := r.deps.Recorder.RecordEvolutionEvent(
		r.scope.UserID, api.PhaseReview, "review_completed", fields,
	); err != nil {
		slog.Debug("reviewer: recorder failed", "err", err)
	}
	_ = phase // reserved for future multi-phase tagging
}

// Shutdown waits for any in-flight reviews to finish, capped at
// ReviewTimeout + 5s grace. Idempotent.
func (r *Reviewer) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.inflightWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
