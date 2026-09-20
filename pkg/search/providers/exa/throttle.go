package exa

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultSearchDelayMs paces consecutive Exa requests. The public MCP
// endpoint throttles keyless callers per IP, so searches are spaced even
// when the chain fans out.
const DefaultSearchDelayMs = 100

// searchDelayMs holds the configured pacing in milliseconds; tests lower
// it to keep suites fast.
var searchDelayMs atomic.Int64

func init() { searchDelayMs.Store(DefaultSearchDelayMs) }

// SetSearchDelayMs configures request pacing; values <= 0 disable it.
func SetSearchDelayMs(ms int64) { searchDelayMs.Store(ms) }

// SearchDelayMs reports the configured pacing.
func SearchDelayMs() int64 { return searchDelayMs.Load() }

// throttleMu serializes slot reservations so waiting callers queue in
// arrival order (FIFO).
var throttleMu sync.Mutex

// nextRequestAt is the earliest instant the next search may start.
var nextRequestAt time.Time

// waitForSearchSlot reserves the next pacing slot for this caller and
// sleeps until it opens. The reservation happens under the throttle
// mutex before sleeping, which serializes concurrent searches: each
// caller queues behind the reserved slots of earlier arrivals. It
// returns early on context cancellation.
func waitForSearchSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delay := searchDelayMs.Load()
	if delay <= 0 {
		return nil
	}
	throttleMu.Lock()
	now := time.Now()
	var wait time.Duration
	if nextRequestAt.After(now) {
		wait = nextRequestAt.Sub(now)
	}
	nextRequestAt = now.Add(wait + time.Duration(delay)*time.Millisecond)
	throttleMu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// resetSearchThrottle clears pacing state for isolated tests.
func resetSearchThrottle() {
	throttleMu.Lock()
	nextRequestAt = time.Time{}
	throttleMu.Unlock()
}
