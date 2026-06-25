package agent

import (
	"context"
	"log/slog"
	"sync"
)

type sessionSummaryQueue struct {
	mu       sync.Mutex
	sessions map[string]struct{}
}

func sessionSummaryQueueNew() *sessionSummaryQueue {
	return &sessionSummaryQueue{sessions: make(map[string]struct{})}
}

func (q *sessionSummaryQueue) Add(sessionID string) {
	if sessionID == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sessions[sessionID] = struct{}{}
}

func (q *sessionSummaryQueue) Drain() []string {
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

func (c *coordinator) drainQueuedSummaries() {
	for _, sessionID := range c.summaryQueue.Drain() {
		sessionID := sessionID
		go func() {
			if err := c.Summarize(context.Background(), sessionID); err != nil {
				slog.Error("scheduled session summary failed", "session_id", sessionID, "error", err)
			}
		}()
	}
}
