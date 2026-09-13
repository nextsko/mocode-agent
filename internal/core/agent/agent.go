// Package agent is the core orchestration layer for Mocode AI agents.
//
// The session-agent implementation lives in the lifecycle subpackage; this
// root keeps the public API surface (type and constructor aliases) so every
// existing caller keeps working unchanged.
package agent

import (
	"github.com/nextsko/mocode-agent/internal/core/agent/lifecycle"
)

const DefaultSessionName = lifecycle.DefaultSessionName

// SessionAgent is a single session's agent runtime (alias).
type SessionAgent = lifecycle.SessionAgent

// SessionAgentCall carries one turn's parameters (alias).
type SessionAgentCall = lifecycle.SessionAgentCall

// SessionAgentOptions configures a new session agent (alias).
type SessionAgentOptions = lifecycle.SessionAgentOptions

// Model bundles the language model with its configuration (alias).
type Model = lifecycle.Model

// AgentCallbacks hosts optional per-turn hooks (alias).
type AgentCallbacks = lifecycle.AgentCallbacks

// NewSessionAgent builds a session agent (forwarded constructor).
func NewSessionAgent(opts SessionAgentOptions) SessionAgent {
	return lifecycle.NewSessionAgent(opts)
}

var (
	// ErrEmptyPrompt is returned when a turn is requested with an empty prompt.
	ErrEmptyPrompt = lifecycle.ErrEmptyPrompt
	// ErrSessionMissing is returned when the referenced session does not exist.
	ErrSessionMissing = lifecycle.ErrSessionMissing
	// ErrSessionBusy is returned when the session already has a run in flight.
	ErrSessionBusy = lifecycle.ErrSessionBusy
	// ErrRequestCancelled is returned when the request was cancelled mid-run.
	ErrRequestCancelled = lifecycle.ErrRequestCancelled
)
