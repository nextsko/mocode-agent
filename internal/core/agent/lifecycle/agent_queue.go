package lifecycle

import "log/slog"

// appendQueueLocked appends a call to the session's prompt queue. It must be
// called with a.runMu held so enqueue-vs-pop is atomic.
func (a *sessionAgent) appendQueueLocked(sessionID string, call SessionAgentCall) {
	queue, _ := a.messageQueue.Get(sessionID)
	queue = append(queue, call)
	a.messageQueue.Set(sessionID, queue)
}

// pushFrontQueueLocked prepends a call to the session's prompt queue,
// preserving FIFO order. It must be called with a.runMu held.
func (a *sessionAgent) pushFrontQueueLocked(sessionID string, call SessionAgentCall) {
	queue, _ := a.messageQueue.Get(sessionID)
	queue = append([]SessionAgentCall{call}, queue...)
	a.messageQueue.Set(sessionID, queue)
}

// popQueueLocked removes and returns the head of the session's prompt queue.
// It must be called with a.runMu held.
func (a *sessionAgent) popQueueLocked(sessionID string) (SessionAgentCall, bool) {
	queue, ok := a.messageQueue.Get(sessionID)
	if !ok || len(queue) == 0 {
		a.messageQueue.Del(sessionID)
		return SessionAgentCall{}, false
	}
	next := queue[0]
	queue = queue[1:]
	if len(queue) == 0 {
		a.messageQueue.Del(sessionID)
	} else {
		a.messageQueue.Set(sessionID, queue)
	}
	return next, true
}

func (a *sessionAgent) ClearQueue(sessionID string) {
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.runMu.Lock()
		a.messageQueue.Del(sessionID)
		a.runMu.Unlock()
	}
}

func (a *sessionAgent) QueuedPrompts(sessionID string) int {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return 0
	}
	return len(l)
}

func (a *sessionAgent) QueuedPromptsList(sessionID string) []string {
	l, ok := a.messageQueue.Get(sessionID)
	if !ok {
		return nil
	}
	prompts := make([]string, len(l))
	for i, call := range l {
		prompts[i] = call.Prompt
	}
	return prompts
}
