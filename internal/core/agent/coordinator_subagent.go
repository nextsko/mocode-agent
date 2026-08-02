package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

func (c *coordinator) QueuedPrompts(sessionID string) int {
	return c.currentAgent.QueuedPrompts(sessionID)
}

func (c *coordinator) QueuedPromptsList(sessionID string) []string {
	return c.currentAgent.QueuedPromptsList(sessionID)
}

// SummarizeWithPath returns the export path of the saved summary alongside
// any error. The path is the on-disk location written by sessionexport,
// which is what the TUI renders in the completion InfoMsg.
func (c *coordinator) SummarizeWithPath(ctx context.Context, sessionID string) (string, error) {
	providerCfg, ok := c.cfg.Config().Providers.Get(c.currentAgent.Model().ModelCfg.Provider)
	if !ok {
		return "", errModelProviderNotConfigured
	}
	return c.currentAgent.Summarize(ctx, sessionID, getProviderOptions(c.currentAgent.Model(), providerCfg))
}

func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
	_, err := c.SummarizeWithPath(ctx, sessionID)
	return err
}

// EnqueueSummaryAndDrain implements Coordinator. It pushes the
// sessionID onto the summary queue and immediately runs the drain,
// rather than waiting for the next Run() defer (which only fires for
// tool-driven scheduling). Used by the /summary slash command path.
func (c *coordinator) EnqueueSummaryAndDrain(sessionID string) {
	if sessionID == "" {
		return
	}
	c.summaryQueue.Add(sessionID)
	c.drainQueuedSummaries()
}

// SummarySubscribe implements Coordinator. It exposes the channel of
// SummaryCompletedMsg events published by the asynchronous summary
// goroutine so the composition root can forward them into app.events.
func (c *coordinator) SummarySubscribe(ctx context.Context) <-chan pubsub.Event[SummaryCompletedMsg] {
	return c.summaryDone.Subscribe(ctx)
}

// runSubAgent runs a sub-agent and handles session management and cost accumulation.
// It creates a sub-session, runs the agent with the given prompt, and propagates
// the cost to the parent session.
func (c *coordinator) runSubAgent(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, error) {
	resp, _, err := c.runSubAgentWithMeta(ctx, params)
	return resp, err
}

// runSubAgentWithMeta is the metadata-returning variant of runSubAgent. It is
// used by the parallel and DAG batch runners so they can populate
// TaskResult.DurationMs and TaskResult.Usage on each envelope.
func (c *coordinator) runSubAgentWithMeta(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, subAgentResult, error) {
	startTime := time.Now()

	// Create sub-session
	agentToolSessionID := c.sessions.CreateAgentToolSessionID(params.AgentMessageID, params.ToolCallID)
	session, err := c.sessions.CreateTaskSession(ctx, agentToolSessionID, params.SessionID, params.SessionTitle)
	if err != nil {
		duration := time.Since(startTime)
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, notify.SubagentTokenUsage{},
			"", fmt.Sprintf("create session: %s", err))
		return fantasy.ToolResponse{}, subAgentResult{DurationMs: duration.Milliseconds()}, fmt.Errorf("create session: %w", err)
	}

	// Call session setup function if provided
	if params.SessionSetup != nil {
		params.SessionSetup(session.ID)
	}

	// Get model configuration
	model := params.Agent.Model()
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		duration := time.Since(startTime)
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, notify.SubagentTokenUsage{},
			"", errModelProviderNotConfigured.Error())
		return fantasy.ToolResponse{}, subAgentResult{DurationMs: duration.Milliseconds()}, errModelProviderNotConfigured
	}

	// Register the sub-agent in the index so an external caller can stop
	// it via CancelSubagent without cancelling the parent session. The
	// entry is removed when the agent run returns — whether the run
	// finishes, errors out, or the sub-agent is cancelled.
	if params.AgentID != "" && c.subagentIndex != nil {
		c.subagentIndex.Set(params.AgentID, agentToolSessionID)
		defer c.subagentIndex.Del(params.AgentID)
	}

	// Run the agent
	result, err := params.Agent.Run(ctx, SessionAgentCall{
		SessionID:        session.ID,
		Prompt:           params.Prompt,
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  getProviderOptions(model, providerCfg),
		Temperature:      model.ModelCfg.Temperature,
		TopP:             model.ModelCfg.TopP,
		TopK:             model.ModelCfg.TopK,
		FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
		PresencePenalty:  model.ModelCfg.PresencePenalty,
		NonInteractive:   true,
	})
	duration := time.Since(startTime)

	if err != nil {
		// Distinguish user-initiated cancellation from a real error so
		// the frontend can show the right terminal state. ctx.Err() is
		// non-nil whenever the sub-agent's own ctx was cancelled
		// (CancelSubagent) or its parent ctx was cancelled (parent
		// session cancelled). In either case the LLM client returns a
		// wrapped error and we want to label it cancelled, not error.
		status := notify.SubagentStatusError
		if ctxErr := ctx.Err(); ctxErr != nil {
			status = notify.SubagentStatusCancelled
		}
		slog.Warn(
			"Sub-agent failed",
			"sub_session", session.ID,
			"tool_call_id", params.ToolCallID,
			"duration_ms", duration.Milliseconds(),
			"status", string(status),
			"error", err,
		)
		c.publishSubagentCompleted(ctx, params, status, duration, notify.SubagentTokenUsage{},
			"", err.Error())
		return fantasy.NewTextErrorResponse(fmt.Sprintf("error generating response: %s", err)),
			subAgentResult{DurationMs: duration.Milliseconds()},
			nil
	}
	if result == nil {
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, notify.SubagentTokenUsage{},
			"", "sub-agent returned no result (session busy)")
		return fantasy.NewTextErrorResponse("sub-agent returned no result (session busy)"),
			subAgentResult{DurationMs: duration.Milliseconds()},
			nil
	}

	meta := subAgentResult{
		DurationMs: duration.Milliseconds(),
		Usage:      result.TotalUsage,
	}

	// Update parent session cost
	if err := c.updateParentSessionCost(ctx, session.ID, params.SessionID); err != nil {
		// Best-effort: still publish a completed event so the UI can move
		// out of the running state, but include the cost update error in
		// the payload for observability.
		c.publishSubagentCompleted(ctx, params, notify.SubagentStatusError, duration, subagentUsageFromTotal(result.TotalUsage),
			firstLine(result.Response.Content.Text()), err.Error())
		return fantasy.ToolResponse{}, meta, err
	}

	c.publishSubagentCompleted(ctx, params, notify.SubagentStatusSuccess, duration, subagentUsageFromTotal(result.TotalUsage),
		firstLine(result.Response.Content.Text()), "")

	return fantasy.NewTextResponse(result.Response.Content.Text()), meta, nil
}

// publishSubagentCompleted emits a SubagentCompleted notification when the
// coordinator has a notify publisher configured. The call is a no-op when no
// publisher is wired (e.g. inside tests) so callers do not need to nil-check.
//
// The context argument is intentionally unused today: pubsub.Publish is
// non-blocking and discards events when no subscribers are attached, so
// cancellation should not gate emission. It is part of the signature so
// future refinements (e.g. honouring a parent ctx when bridging to a
// downstream that may block) can adopt it without churning call sites.
func (c *coordinator) publishSubagentCompleted(
	_ context.Context, // reserved for future cancellation propagation; see doc comment
	params subAgentParams,
	status notify.SubagentStatus,
	duration time.Duration,
	usage notify.SubagentTokenUsage,
	summary string,
	errMsg string,
) {
	if c.notify == nil {
		return
	}
	c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
		SessionID: params.SessionID,
		Type:      notify.TypeSubagentCompleted,
		SubagentCompleted: &notify.SubagentCompletedEvent{
			ParentSessionID:  params.SessionID,
			ParentToolCallID: params.ToolCallID,
			AgentID:          params.AgentID,
			SubagentType:     params.SubagentType,
			Status:           status,
			DurationMs:       duration.Milliseconds(),
			Usage:            usage,
			Summary:          summary,
			Error:            errMsg,
		},
	})
}

// updateParentSessionCost accumulates the cost from a child session to its parent session.
// Uses atomic increment to prevent lost updates when multiple sub-agents run concurrently.
func (c *coordinator) updateParentSessionCost(ctx context.Context, childSessionID, parentSessionID string) error {
	childSession, err := c.sessions.Get(ctx, childSessionID)
	if err != nil {
		return fmt.Errorf("get child session: %w", err)
	}

	if childSession.Cost == 0 {
		return nil // Nothing to add
	}

	// Use atomic increment to safely accumulate cost from concurrent sub-agents.
	// This prevents lost updates that would occur with read-modify-write.
	if err := c.sessions.IncrementCost(ctx, parentSessionID, childSession.Cost); err != nil {
		return fmt.Errorf("increment parent cost: %w", err)
	}

	return nil
}
