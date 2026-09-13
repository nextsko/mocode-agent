// Package agent is the core orchestration layer for Mocode AI agents.
//
// The session-agent implementation lives in the lifecycle subpackage; this
// root keeps the public API surface (type and constructor aliases) so every
// existing caller keeps working unchanged.
package agent

import (
	"context"

	"github.com/nextsko/mocode-agent/internal/core/agent/coordinator"
	"github.com/nextsko/mocode-agent/internal/core/agent/lifecycle"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/store"
	"github.com/nextsko/mocode-agent/internal/util/errcoll"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"

	agentnotify "github.com/nextsko/mocode-agent/internal/core/agent/notify"
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

// SummaryCompletedMsg is the payload of the async summary completion event (alias).
type SummaryCompletedMsg = coordinator.SummaryCompletedMsg

// Coordinator-owned tool symbols (aliases).
type (
	AgentParams     = coordinator.AgentParams
	AgentTaskParams = coordinator.AgentTaskParams
	TaskResult      = coordinator.TaskResult
	TaskUsage       = coordinator.TaskUsage
)

const AgentToolName = coordinator.AgentToolName

// NewCoordinator builds the default coordinator (forwarded constructor).
func NewCoordinator(
	ctx context.Context,
	cfg *config.ConfigStore,
	sessions session.Service,
	messages message.Service,
	permissions permission.Service,
	questions question.Service,
	history history.Service,
	filetracker filetracker.Service,
	lspManager *lsp.Manager,
	notify pubsub.Publisher[agentnotify.Notification],
	errorCollector *errcoll.Collector,
	sessionSearch *store.SessionSearch,
) (Coordinator, error) {
	return coordinator.New(ctx, cfg, sessions, messages, permissions, questions, history, filetracker, lspManager, notify, errorCollector, sessionSearch)
}
