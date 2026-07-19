// Package candidate implements best-of-n candidate selection: generate N
// candidate agent runs in parallel, score each with an evaluation selector,
// and return the winner.
//
// This is the mocode port of trpc-agent-go's bestofn pattern, where the
// evaluation subsystem is reused as a candidate selector: the same scoring
// pipeline that judges an eval set offline can pick the best response online.
// It composes directly with internal/evaluation — a candidate is scored with
// criterion.Criterion.Eval, exactly like an eval case.
package candidate

import (
	"context"
	"fmt"
	"sync"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// Result is the outcome of one candidate attempt: the raw agent result, the
// structured trace used for scoring, and any generation error.
type Result struct {
	// RunID is the 1-based index of this candidate.
	RunID int
	// Result is the agent's raw result. Nil if generation errored.
	Result *fantasy.AgentResult
	// Trace is the full message trace used for scoring (the same ground truth
	// the evaluation system consumes).
	Trace []message.Message
	// SessionID is the session that produced this candidate.
	SessionID string
	// Err is non-nil when candidate generation failed. Such candidates are
	// skipped during selection and never selected as the winner.
	Err error
}

// Selector picks the best candidate from a set. Implementations score each
// candidate and return the index of the winner (0-based into the slice).
// Returning an error aborts selection; the caller surfaces it.
type Selector interface {
	Select(ctx context.Context, candidates []Result) (int, error)
}

// Generate produces one candidate for the given runID. It is called N times
// by Run, potentially concurrently. Returning an error marks that candidate
// as failed (stored in Result.Err) but does not abort the whole batch.
type Generate func(ctx context.Context, runID int) (Result, error)

// Run executes n candidate attempts and selects the winner with sel.
//
// Candidates are generated concurrently (bounded by n). Failed candidates
// carry their error but do not stop the batch. Selection considers the full
// candidate set (failed entries have Err != nil); selectors that only score
// viable candidates must skip Err entries themselves. If every candidate
// fails, the last error is returned. If n < 1, Run panics (programming error).
func Run(ctx context.Context, n int, gen Generate, sel Selector) (Result, error) {
	if n < 1 {
		panic(fmt.Sprintf("candidate: n must be >= 1, got %d", n))
	}
	if gen == nil {
		return Result{}, fmt.Errorf("candidate: generate function is required")
	}
	if sel == nil {
		return Result{}, fmt.Errorf("candidate: selector is required")
	}

	candidates := make([]Result, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			res, err := gen(ctx, i+1)
			res.RunID = i + 1
			if err != nil {
				res.Err = err
			}
			candidates[i] = res
		}()
	}
	wg.Wait()

	var viableCount int
	var lastErr error
	for _, c := range candidates {
		if c.Err != nil {
			lastErr = c.Err
			continue
		}
		viableCount++
	}
	if viableCount == 0 {
		return Result{}, fmt.Errorf("candidate: all %d candidates failed; last error: %w", n, lastErr)
	}

	winnerIdx, err := sel.Select(ctx, candidates)
	if err != nil {
		return Result{}, fmt.Errorf("candidate: select: %w", err)
	}
	if winnerIdx < 0 || winnerIdx >= len(candidates) {
		return Result{}, fmt.Errorf("candidate: selector returned out-of-range index %d for %d candidates", winnerIdx, len(candidates))
	}
	if candidates[winnerIdx].Err != nil {
		return Result{}, fmt.Errorf("candidate: selector chose a failed candidate: %w", candidates[winnerIdx].Err)
	}
	return candidates[winnerIdx], nil
}
