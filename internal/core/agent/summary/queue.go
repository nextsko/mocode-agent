// Package summary holds the coordinator's asynchronous session-summary
// queue: sessions needing a summary are added as work is discovered and
// drained in batches on the agent loop.
package summary

import "sync"

// Queue tracks session IDs pending an asynchronous summary.
type Queue struct {
	mu       sync.Mutex
	sessions map[string]struct{}
}

// NewQueue creates an empty summary queue.
func NewQueue() *Queue {
	return &Queue{sessions: make(map[string]struct{})}
}

// Add enqueues a session (empty IDs are ignored).
func (q *Queue) Add(sessionID string) {
	if sessionID == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sessions[sessionID] = struct{}{}
}

// Drain returns every queued session ID and resets the queue.
func (q *Queue) Drain() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.sessions) == 0 {
		return nil
	}
	sessions := make([]string, 0, len(q.sessions))
	for sessionID := range q.sessions {
		sessions = append(sessions, sessionID)
	}
	q.sessions = make(map[string]struct{})
	return sessions
}
