package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// Inject adds mid-turn user guidance to the RUNNING session: the text is
// persisted as a user message (so the transcript stays complete) and buffered
// for the model — prepareStep drains the buffer and prepends a
// <user_guidance> system message, so the very next model step sees it without
// waiting for the current turn to end. Idle sessions simply get the message
// stored (the next Run picks it up from history).
func (a *sessionAgent) Inject(ctx context.Context, sessionID, text string) error {
	if sessionID == "" {
		return ErrSessionMissing
	}
	if text == "" {
		return ErrEmptyPrompt
	}
	if _, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	}); err != nil {
		return fmt.Errorf("persist guidance message: %w", err)
	}
	a.injected.Update(sessionID, func(prev []string, ok bool) ([]string, bool) {
		return append(prev, text), true
	})
	return nil
}

// drainInjectedLocked swaps out the session's pending guidance buffer.
func (a *sessionAgent) drainInjected(sessionID string) []string {
	var drained []string
	a.injected.Update(sessionID, func(prev []string, ok bool) ([]string, bool) {
		drained = prev
		return nil, true
	})
	return drained
}

// ForceRun preempts the session: the call jumps the queue head and the
// running turn (if any) is interrupted with an ErrForceKick cause, so the
// dispatcher continues straight into the forced call instead of stopping.
func (a *sessionAgent) ForceRun(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	if call.SessionID == "" {
		return nil, ErrSessionMissing
	}
	a.runMu.Lock()
	a.pushFrontQueueLocked(call.SessionID, call)
	cancelFn, busy := a.activeRequests.Get(call.SessionID)
	a.runMu.Unlock()
	if busy && cancelFn != nil {
		cancelFn(ErrForceKick)
		return nil, nil
	}
	// Idle: no one to interrupt — run directly.
	return a.Run(ctx, call)
}

// forceKicked reports whether the turn ended because of a ForceRun kick.
func forceKicked(genCtx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(context.Cause(genCtx), ErrForceKick)
}
