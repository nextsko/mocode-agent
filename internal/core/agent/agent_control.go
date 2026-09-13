package agent

import (
	"log/slog"
	"time"
)

func (a *sessionAgent) Cancel(sessionID string) {
	// Cancel active requests (regular turns and Summarize both register
	// under the bare sessionID). Don't use Take() here - we need the entry
	// to remain in activeRequests so IsBusy() returns true until the
	// goroutine fully completes (including error handling that may access
	// the DB). The dispatcher loop in Run releases the entry at the end of
	// each turn, on error returns, and via its panic safety net.
	if cancel, ok := a.activeRequests.Get(sessionID); ok && cancel != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		cancel()
	}

	// Cancel also clears the queue: the running dispatcher pops an empty
	// queue at its turn boundary and exits without starting further turns.
	// Hold runMu so a concurrent enqueue cannot race this clear (its call
	// would otherwise be deleted before any dispatcher sees it).
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.runMu.Lock()
		a.messageQueue.Del(sessionID)
		a.runMu.Unlock()
	}
}

func (a *sessionAgent) CancelAll() {
	if !a.IsBusy() {
		return
	}
	for key := range a.activeRequests.Seq2() {
		a.Cancel(key) // key is sessionID
	}

	timeout := time.After(5 * time.Second)
	for a.IsBusy() {
		select {
		case <-timeout:
			return
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (a *sessionAgent) IsBusy() bool {
	var busy bool
	for cancelFunc := range a.activeRequests.Seq() {
		if cancelFunc != nil {
			busy = true
			break
		}
	}
	return busy
}

func (a *sessionAgent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Get(sessionID)
	return busy
}
