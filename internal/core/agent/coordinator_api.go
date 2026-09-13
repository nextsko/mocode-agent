package agent

import (
	"context"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/domain/messenger"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

type Coordinator interface {
	// SetMainAgent switches the active agent to the given mode/agent ID.
	// This supports transfer_to_agent and mode switching.
	SetMainAgent(agentID string) error
	// SetMessenger wires the external-account send port (e.g. messaging
	// integration) that the coordinator-owned messaging tools use. Safe to
	// call once at startup.
	SetMessenger(m messenger.Messenger)
	Run(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error)
	Cancel(sessionID string)
	// InjectGuidance adds mid-turn user guidance to the RUNNING turn of the
	// session: persisted as a user message and surfaced to the model at its
	// next step (steering without a new queued turn).
	InjectGuidance(ctx context.Context, sessionID, text string) error
	// ForceRun preempts the session: the prompt jumps the queue head and the
	// running turn is interrupted so dispatch continues into it immediately.
	ForceRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error
	// CancelSubagent stops a single sub-agent dispatched by the Agent
	// tool without cancelling the parent session. subagentID is the
	// user-visible identifier (params.AgentID), e.g. "<parentToolCallID>-1".
	// No-op when the sub-agent is unknown or already finished.
	CancelSubagent(subagentID string)
	CancelAll()
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	QueuedPrompts(sessionID string) int
	QueuedPromptsList(sessionID string) []string
	ClearQueue(sessionID string)
	Summarize(context.Context, string) error
	// EnqueueSummaryAndDrain enqueues a session for asynchronous summary
	// generation and immediately drains the queue. Unlike Summarize, it
	// never blocks the caller; the LLM-driven generation runs in a
	// goroutine on context.Background(). Used by the /summary slash
	// command path to keep the TUI interactive while the summary runs.
	EnqueueSummaryAndDrain(sessionID string)
	// SummarySubscribe exposes the channel of SummaryCompletedMsg events
	// emitted by the asynchronous summary goroutine. The composition root
	// (internal/core/app/app.go) forwards these into app.events so the
	// TUI can render a completion InfoMsg without polling.
	SummarySubscribe(ctx context.Context) <-chan pubsub.Event[SummaryCompletedMsg]
	Model() Model
	ActiveAgentID() string
	// ActiveAgentSystemPrompt returns the proven system prompt of the active
	// agent. The /evo mode captures it at enter time so the reconstructed
	// optimal theory preserves what already worked as a stable base.
	ActiveAgentSystemPrompt() string
	// SmallLanguageModel returns the configured small model, used for cheap
	// auxiliary calls (the /evo lesson distiller). Returns nil when no small
	// model is configured, in which case the distiller falls back to its
	// zero-overhead default.
	SmallLanguageModel(ctx context.Context) fantasy.LanguageModel
	UpdateModels(ctx context.Context) error
	// Close releases coordinator-owned resources (the tool registry's
	// connection pools, e.g. SSH). Called from the composition root's
	// Shutdown after all agents are cancelled.
	Close(ctx context.Context) error
}
