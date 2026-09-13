package lifecycle

import "errors"

var (
	ErrRequestCancelled = errors.New("request canceled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
	ErrEmptyPrompt      = errors.New("prompt is empty")
	ErrSessionMissing   = errors.New("session id is missing")
	// ErrForceKick is the cancel CAUSE used by ForceRun: the interrupted
	// turn hands the dispatch over to the force-queued call instead of
	// stopping the session (plain Esc cancel keeps the stop semantics).
	ErrForceKick = errors.New("force kick: interrupted by a force-send")
)
