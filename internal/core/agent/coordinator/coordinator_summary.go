package coordinator

import (
	"context"
	"log/slog"

	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

func (c *Coordinator) drainQueuedSummaries() {
	for _, sessionID := range c.summaryQueue.Drain() {
		go func() {
			path, err := c.SummarizeWithPath(context.Background(), sessionID)
			if err != nil {
				slog.Error("scheduled session summary failed", "session_id", sessionID, "error", err)
			}
			c.summaryDone.Publish(pubsub.UpdatedEvent, SummaryCompletedMsg{
				SessionID: sessionID,
				Path:      path,
				Err:       err,
			})
		}()
	}
}
