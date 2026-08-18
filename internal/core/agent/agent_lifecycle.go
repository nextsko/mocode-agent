package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"

	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionexport"
	"github.com/nextsko/mocode-agent/internal/util/errcoll"
	"github.com/nextsko/mocode-agent/internal/util/ext"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

// turnOutcome describes how a single turn ended so that Run's dispatcher
// loop can decide what happens next. runTurn never mutates the busy entry
// or the prompt queue; every dispatch-state transition happens in Run under
// runMu.
type turnOutcome struct {
	result *fantasy.AgentResult
	err    error

	// ctxTooLarge is set when the provider rejected the request because the
	// context window was exceeded. The dispatcher releases the busy entry,
	// summarizes the session, and re-dispatches retryCall as the next turn.
	ctxTooLarge bool
	retryCall   *SessionAgentCall

	// summarize is set when the turn hit the context-window stop condition.
	// The dispatcher releases the busy entry and runs Summarize before
	// continuing. When hadToolCalls is set, a continuation call is appended
	// to the queue tail (user-queued prompts still run first).
	summarize    bool
	hadToolCalls bool

	// shortCircuit is set when the BeforeAgent callback returned a result or
	// error; Run must return immediately without dispatching further turns.
	shortCircuit bool
}

// Run executes a prompt as one or more strictly serialized turns: the
// in-flight turn is never hijacked and queued prompts only start at turn
// boundaries. All busy-entry and queue mutations happen in short critical
// sections under runMu (never across model/IO calls).
func (a *sessionAgent) Run(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
	if call.Prompt == "" && !message.ContainsTextAttachment(call.Attachments) {
		return nil, ErrEmptyPrompt
	}
	if call.SessionID == "" {
		return nil, ErrSessionMissing
	}

	sessionID := call.SessionID
	runCtx := context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	runCtx = errcoll.WithContext(runCtx, a.errorCollector)

	var (
		genCtx      context.Context
		cancelTurn  context.CancelFunc
		busyOwned   bool
		current     = call
		haveCurrent = true
		lastCall    = call
		lastResult  *fantasy.AgentResult
	)
	defer func() {
		// Panic safety net: never leak a busy entry. Normal paths clear
		// busyOwned inside the loop's critical sections, so a concurrent Run
		// that legitimately re-marked busy is never clobbered here.
		if busyOwned {
			a.runMu.Lock()
			a.activeRequests.Del(sessionID)
			if cancelTurn != nil {
				cancelTurn()
			}
			a.runMu.Unlock()
		}
	}()

	releaseBusy := func() {
		a.runMu.Lock()
		a.activeRequests.Del(sessionID)
		if cancelTurn != nil {
			cancelTurn()
		}
		a.runMu.Unlock()
		busyOwned = false
	}

	for {
		if !busyOwned {
			// Acquire-or-enqueue critical section: either this loop already
			// owns the next turn (busyOwned handed over by the end-of-turn
			// section below) or we atomically decide busy-vs-queue.
			a.runMu.Lock()
			var ok bool
			genCtx, cancelTurn, ok = a.beginTurnLocked(sessionID, runCtx)
			if !ok {
				if haveCurrent {
					// Busy: queue the call and return silently. The running
					// dispatcher picks it up at its next turn boundary.
					a.appendQueueLocked(sessionID, current)
					a.runMu.Unlock()
					return nil, nil
				}
				// Continuation lost the race to another Run: that dispatcher
				// owns the queue now; nothing more for us to do here.
				a.runMu.Unlock()
				break
			}
			if !haveCurrent {
				// Continuation mode (post-summarize): the next call comes
				// from the queue head; user-queued prompts run first.
				next, hasNext := a.popQueueLocked(sessionID)
				if !hasNext {
					a.activeRequests.Del(sessionID)
					cancelTurn()
					a.runMu.Unlock()
					break
				}
				current = next
			}
			busyOwned = true
			a.runMu.Unlock()
		}

		outcome := a.runTurn(runCtx, genCtx, current)
		lastCall, lastResult = current, outcome.result

		switch {
		case outcome.ctxTooLarge:
			// Context window exceeded: release busy so Summarize can run,
			// then re-dispatch the retry prompt as the next turn. The lock
			// is taken fresh at the top of the loop, so re-entry cannot
			// deadlock; if another Run won the race the retry is queued.
			releaseBusy()
			if _, summarizeErr := a.Summarize(runCtx, sessionID, current.ProviderOptions); summarizeErr != nil {
				slog.Error("Failed to summarize after context-too-large error", "error", summarizeErr)
				return nil, outcome.err
			}
			current, haveCurrent = *outcome.retryCall, true
			continue

		case outcome.shortCircuit:
			releaseBusy()
			return outcome.result, outcome.err

		case outcome.err != nil:
			// Preserve existing semantics: a failed turn (including user
			// cancel) returns immediately; queued prompts survive for the
			// next dispatch and RunAfterAgent does not fire.
			releaseBusy()
			return outcome.result, outcome.err

		case outcome.summarize:
			// Context threshold reached mid-work: release busy, summarize,
			// then continue from the queue head.
			releaseBusy()
			if _, summarizeErr := a.Summarize(runCtx, sessionID, current.ProviderOptions); summarizeErr != nil {
				return nil, summarizeErr
			}
			if outcome.hadToolCalls {
				continuation := current
				continuation.Prompt = fmt.Sprintf("The previous session was interrupted because it got too long, the initial user request was: `%s`", current.Prompt)
				a.runMu.Lock()
				a.appendQueueLocked(sessionID, continuation)
				a.runMu.Unlock()
			}
			current, haveCurrent = SessionAgentCall{}, false
			continue
		}

		// Plain success: atomic end-of-turn — release, pop the next queued
		// call and re-mark busy in one critical section so a concurrent Run
		// can neither lose its enqueue nor steal the dispatch.
		a.runMu.Lock()
		a.activeRequests.Del(sessionID)
		cancelTurn()
		next, hasNext := a.popQueueLocked(sessionID)
		if !hasNext {
			a.runMu.Unlock()
			busyOwned = false
			break
		}
		var ok bool
		genCtx, cancelTurn, ok = a.beginTurnLocked(sessionID, runCtx)
		if !ok {
			// Defensive: unreachable while holding runMu (we just deleted
			// the busy entry). Keep FIFO order by pushing back to the head.
			a.pushFrontQueueLocked(sessionID, next)
			a.runMu.Unlock()
			busyOwned = false
			return nil, nil
		}
		current = next
		a.runMu.Unlock()
	}

	// The queue is exhausted: fire the AfterAgent callback exactly once with
	// the final turn's call and result.
	return a.callbacks.RunAfterAgent(runCtx, &lastCall, lastResult, nil)
}

// beginTurnLocked marks the session busy by registering a fresh cancel
// function in activeRequests. It must be called with a.runMu held and
// returns ok=false when another turn already owns the session.
func (a *sessionAgent) beginTurnLocked(sessionID string, ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if _, busy := a.activeRequests.Get(sessionID); busy {
		return nil, nil, false
	}
	genCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(sessionID, cancel)
	return genCtx, cancel, true
}

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

// runTurn executes a single user turn end-to-end: BeforeAgent callback,
// user-message persistence, model streaming, finish handling and the
// TypeAgentFinished notification. It must be called with the session marked
// busy (genCtx/cancel registered by the dispatcher in Run).
func (a *sessionAgent) runTurn(ctx context.Context, genCtx context.Context, call SessionAgentCall) turnOutcome {
	// BeforeAgent callback — may skip execution entirely.
	if result, err := a.callbacks.RunBeforeAgent(ctx, &call); result != nil || err != nil {
		return turnOutcome{result: result, err: err, shortCircuit: true}
	}

	// Send thinking notification
	if !call.NonInteractive && a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID: call.SessionID,
			Type:      notify.TypeAgentThinking,
		})
	}

	// Copy mutable fields under lock to avoid races with SetTools/SetModels.
	agentTools := a.tools.Copy()
	largeModel := a.largeModel.Get()
	systemPrompt := a.systemPrompt.Get()
	promptPrefix := a.systemPromptPrefix.Get()
	var instructions strings.Builder

	for _, server := range mcp.GetStates() {
		if server.State != mcp.StateConnected {
			continue
		}
		if s := server.Client.InitializeResult().Instructions; s != "" {
			instructions.WriteString(s)
			instructions.WriteString("\n\n")
		}
	}

	if s := instructions.String(); s != "" {
		systemPrompt += "\n\n<mcp-instructions>\n" + s + "\n</mcp-instructions>"
	}

	// BeforeModel callback — may modify system prompt.
	if err := a.callbacks.RunBeforeModel(ctx, &call, &systemPrompt); err != nil {
		return turnOutcome{err: err}
	}

	if len(agentTools) > 0 {
		// Add Anthropic caching to the last tool.
		agentTools[len(agentTools)-1].SetProviderOptions(a.getCacheControlOptions())
	}

	agent := fantasy.NewAgent(
		largeModel.Model,
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(agentTools...),
		fantasy.WithUserAgent(userAgent),
	)

	sessionLock := sync.Mutex{}
	currentSession, err := a.sessions.Get(ctx, call.SessionID)
	if err != nil {
		return turnOutcome{err: fmt.Errorf("failed to get session: %w", err)}
	}

	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return turnOutcome{err: fmt.Errorf("failed to get session messages: %w", err)}
	}

	var wg sync.WaitGroup
	// Generate title if first message.
	if len(msgs) == 0 {
		titleCtx := ctx // Copy to avoid race with ctx reassignment below.
		wg.Go(func() {
			a.generateTitle(titleCtx, call.SessionID, call.Prompt)
		})
	}
	defer wg.Wait()

	// Add the user message to the session.
	_, err = a.createUserMessage(ctx, call)
	if err != nil {
		return turnOutcome{err: err}
	}

	history, files := a.preparePrompt(msgs, largeModel.CatwalkCfg.SupportsImages, call.Attachments...)

	startTime := time.Now()
	a.eventPromptSent(call.SessionID)

	var currentAssistant *message.Message
	var shouldSummarize bool
	// Don't send MaxOutputTokens if 0 — some providers (e.g. LM Studio) reject it
	var maxOutputTokens *int64
	if call.MaxOutputTokens > 0 {
		maxOutputTokens = &call.MaxOutputTokens
	}
	result, err := agent.Stream(genCtx, fantasy.AgentStreamCall{
		Prompt:           message.PromptWithTextAttachments(call.Prompt, call.Attachments),
		Files:            files,
		Messages:         history,
		ProviderOptions:  call.ProviderOptions,
		MaxOutputTokens:  maxOutputTokens,
		TopP:             call.TopP,
		Temperature:      call.Temperature,
		PresencePenalty:  call.PresencePenalty,
		TopK:             call.TopK,
		FrequencyPenalty: call.FrequencyPenalty,
		PrepareStep: func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = options.Messages
			for i := range prepared.Messages {
				prepared.Messages[i].ProviderOptions = nil
			}

			// Use latest tools (updated by SetTools when MCP tools change).
			prepared.Tools = a.tools.Copy()

			// NOTE: queued prompts are intentionally NOT drained into the
			// in-flight turn — they only start at turn boundaries (see Run).

			prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, largeModel)

			// Final safety: drop messages with empty content arrays to
			// prevent "messages.content.type is invalid" API errors.
			prepared.Messages = filterEmptyContentMessages(prepared.Messages)

			lastSystemRoleInx := 0
			systemMessageUpdated := false
			for i, msg := range prepared.Messages {
				// Only add cache control to the last message.
				if msg.Role == fantasy.MessageRoleSystem {
					lastSystemRoleInx = i
				} else if !systemMessageUpdated {
					prepared.Messages[lastSystemRoleInx].ProviderOptions = a.getCacheControlOptions()
					systemMessageUpdated = true
				}
				// Than add cache control to the last 2 messages.
				if i > len(prepared.Messages)-3 {
					prepared.Messages[i].ProviderOptions = a.getCacheControlOptions()
				}
			}

			if promptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(promptPrefix)}, prepared.Messages...)
			}

			// Todo nudge: surface open todos at each model step so the agent is
			// reminded to finish pending/in-progress items before declaring done.
			// Re-fetch the session for fresh todo state (the todos tool may have
			// updated it mid-run). Best-effort: a fetch failure just skips the nudge.
			if nudgedSession, nudgeErr := a.sessions.Get(callContext, call.SessionID); nudgeErr == nil {
				if nudge := buildTodoNudge(nudgedSession.Todos); nudge != "" {
					prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(nudge)}, prepared.Messages...)
				}
			}

			var assistantMsg message.Message
			assistantMsg, err = a.messages.Create(callContext, call.SessionID, message.CreateMessageParams{
				Role:     message.Assistant,
				Parts:    []message.ContentPart{},
				Model:    largeModel.ModelCfg.Model,
				Provider: largeModel.ModelCfg.Provider,
			})
			if err != nil {
				return callContext, prepared, err
			}
			callContext = context.WithValue(callContext, tools.MessageIDContextKey, assistantMsg.ID)
			callContext = context.WithValue(callContext, tools.SupportsImagesContextKey, largeModel.CatwalkCfg.SupportsImages)
			callContext = context.WithValue(callContext, tools.ModelNameContextKey, largeModel.CatwalkCfg.Name)
			currentAssistant = &assistantMsg
			return callContext, prepared, err
		},
		OnReasoningStart: func(id string, reasoning fantasy.ReasoningContent) error {
			currentAssistant.AppendReasoningContent(reasoning.Text)
			return a.messages.Update(genCtx, *currentAssistant)
		},
		OnReasoningDelta: func(id string, text string) error {
			currentAssistant.AppendReasoningContent(text)
			return a.messages.Update(genCtx, *currentAssistant)
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			// Record the reasoning trace for the evolution/observability layer.
			if lg := toolutil.GetSessionLogger(ctx); lg != nil && reasoning.Text != "" {
				lg.LogThink("reasoning", truncateForLog(reasoning.Text), toolutil.SessionLogMeta{})
			}
			// handle anthropic signature
			if anthropicData, ok := reasoning.ProviderMetadata[anthropic.Name]; ok {
				if reasoning, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok {
					currentAssistant.AppendReasoningSignature(reasoning.Signature)
				}
			}
			if googleData, ok := reasoning.ProviderMetadata[google.Name]; ok {
				if reasoning, ok := googleData.(*google.ReasoningMetadata); ok {
					currentAssistant.AppendThoughtSignature(reasoning.Signature, reasoning.ToolID)
				}
			}
			if openaiData, ok := reasoning.ProviderMetadata[openai.Name]; ok {
				if reasoning, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
					currentAssistant.SetReasoningResponsesData(reasoning)
				}
			}
			currentAssistant.FinishThinking()
			return a.messages.Update(genCtx, *currentAssistant)
		},
		OnTextDelta: func(id string, text string) error {
			// Strip leading newline from initial text content. This is is
			// particularly important in non-interactive mode where leading
			// newlines are very visible.
			if len(currentAssistant.Parts) == 0 {
				text = strings.TrimPrefix(text, "\n")
			}

			currentAssistant.AppendContent(text)
			return a.messages.Update(genCtx, *currentAssistant)
		},
		OnToolInputStart: func(id string, toolName string) error {
			// Send tool executing notification
			if !call.NonInteractive && a.notify != nil {
				a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
					SessionID: call.SessionID,
					Type:      notify.TypeAgentToolExecuting,
					ToolName:  toolName,
				})
			}

			toolCall := message.ToolCall{
				ID:               id,
				Name:             toolName,
				ProviderExecuted: false,
				Finished:         false,
			}
			currentAssistant.AddToolCall(toolCall)
			// Use parent ctx instead of genCtx to ensure the update succeeds
			// even if the request is canceled mid-stream
			return a.messages.Update(ctx, *currentAssistant)
		},
		OnRetry: func(err *fantasy.ProviderError, delay time.Duration) {
			slog.Warn("Provider request failed, retrying", providerRetryLogFields(err, delay)...)
		},
		OnToolCall: func(tc fantasy.ToolCallContent) error {
			toolCall := message.ToolCall{
				ID:               tc.ToolCallID,
				Name:             tc.ToolName,
				Input:            tc.Input,
				ProviderExecuted: false,
				Finished:         true,
			}
			currentAssistant.AddToolCall(toolCall)
			// Record the tool invocation for the evolution/observability layer.
			if lg := toolutil.GetSessionLogger(ctx); lg != nil {
				lg.LogToolCall("tool_call", tc.ToolName+": "+truncateForLog(tc.Input), toolutil.SessionLogMeta{
					ToolName: tc.ToolName, ToolCallID: tc.ToolCallID,
				})
			}
			// Use parent ctx instead of genCtx to ensure the update succeeds
			// even if the request is canceled mid-stream
			return a.messages.Update(ctx, *currentAssistant)
		},
		OnToolResult: func(result fantasy.ToolResultContent) error {
			toolResult := a.convertToToolResult(result)

			// Record tool results (and errors) for the evolution/observability layer.
			if lg := toolutil.GetSessionLogger(ctx); lg != nil {
				if toolResult.IsError {
					lg.LogBug("tool_error", result.ToolName+": "+truncateForLog(toolResult.Content), toolutil.SessionLogMeta{
						ToolName: result.ToolName, ToolCallID: toolResult.ToolCallID, ErrorType: "tool_execution",
					})
				} else {
					lg.LogToolCall("tool_result", result.ToolName+": "+truncateForLog(toolResult.Content), toolutil.SessionLogMeta{
						ToolName: result.ToolName, ToolCallID: toolResult.ToolCallID,
					})
				}
			}

			// Use parent ctx instead of genCtx to ensure the message is created
			// even if the request is canceled mid-stream
			_, createMsgErr := a.messages.Create(ctx, currentAssistant.SessionID, message.CreateMessageParams{
				Role: message.Tool,
				Parts: []message.ContentPart{
					toolResult,
				},
			})
			return createMsgErr
		},
		OnStepFinish: func(stepResult fantasy.StepResult) error {
			finishReason := message.FinishReasonUnknown
			switch stepResult.FinishReason {
			case fantasy.FinishReasonLength:
				finishReason = message.FinishReasonMaxTokens
			case fantasy.FinishReasonStop:
				finishReason = message.FinishReasonEndTurn
			case fantasy.FinishReasonToolCalls:
				finishReason = message.FinishReasonToolUse
			}
			// If a tool result halted the turn (e.g. a hook halt or a
			// permission denial), the step ends on FinishReasonToolCalls but
			// the model will not be called again. Treat it as the end of the
			// turn so the UI can render the assistant footer.
			if finishReason == message.FinishReasonToolUse {
				for _, tr := range stepResult.Content.ToolResults() {
					if tr.StopTurn {
						finishReason = message.FinishReasonEndTurn
						break
					}
				}
			}
			currentAssistant.AddFinish(finishReason, "", "")
			sessionLock.Lock()
			defer sessionLock.Unlock()

			updatedSession, getSessionErr := a.sessions.Get(ctx, call.SessionID)
			if getSessionErr != nil {
				return getSessionErr
			}
			a.updateSessionUsage(largeModel, &updatedSession, stepResult.Usage, a.openrouterCost(stepResult.ProviderMetadata))
			_, sessionErr := a.sessions.Save(ctx, updatedSession)
			if sessionErr != nil {
				return sessionErr
			}
			currentSession = updatedSession
			// Record per-step token usage for the evolution/observability layer.
			if lg := toolutil.GetSessionLogger(ctx); lg != nil {
				lg.LogInfo("step_usage", fmt.Sprintf("finish=%s in=%d out=%d cache_read=%d cache_create=%d",
					finishReason, stepResult.Usage.InputTokens, stepResult.Usage.OutputTokens,
					stepResult.Usage.CacheReadTokens, stepResult.Usage.CacheCreationTokens),
					toolutil.SessionLogMeta{AgentID: largeModel.ModelCfg.Model})
			}
			return a.messages.Update(genCtx, *currentAssistant)
		},
		StopWhen: []fantasy.StopCondition{
			func(_ []fantasy.StepResult) bool {
				cw := int64(largeModel.CatwalkCfg.ContextWindow)
				// If context window is unknown (0), skip auto-summarize
				// to avoid immediately truncating custom/local models.
				if cw == 0 {
					return false
				}
				tokens := currentSession.CompletionTokens + currentSession.PromptTokens
				remaining := cw - tokens
				var threshold int64
				if cw > largeContextWindowThreshold {
					threshold = largeContextWindowBuffer
				} else {
					threshold = int64(float64(cw) * smallContextWindowRatio)
				}
				if (remaining <= threshold) && !a.disableAutoSummarize {
					shouldSummarize = true
					return true
				}
				return false
			},
			func(steps []fantasy.StepResult) bool {
				return hasRepeatedToolCalls(steps, loopDetectionWindowSize, loopDetectionMaxRepeats)
			},
		},
	})

	a.eventPromptResponded(call.SessionID, time.Since(startTime).Truncate(time.Second))

	if err != nil {
		isCancelErr := errors.Is(err, context.Canceled)
		if currentAssistant == nil {
			return turnOutcome{result: result, err: err}
		}
		// Ensure we finish thinking on error to close the reasoning state.
		currentAssistant.FinishThinking()
		toolCalls := currentAssistant.ToolCalls()
		// INFO: we use the parent context here because the genCtx has been cancelled.
		msgs, createErr := a.messages.List(ctx, currentAssistant.SessionID)
		if createErr != nil {
			return turnOutcome{err: createErr}
		}
		for _, tc := range toolCalls {
			if !tc.Finished {
				tc.Finished = true
				tc.Input = "{}"
				currentAssistant.AddToolCall(tc)
				updateErr := a.messages.Update(ctx, *currentAssistant)
				if updateErr != nil {
					return turnOutcome{err: updateErr}
				}
			}

			found := false
			for _, msg := range msgs {
				if msg.Role == message.Tool {
					for _, tr := range msg.ToolResults() {
						if tr.ToolCallID == tc.ID {
							found = true
							break
						}
					}
				}
				if found {
					break
				}
			}
			if found {
				continue
			}
			content := "There was an error while executing the tool"
			if isCancelErr {
				content = "Error: user cancelled assistant tool calling"
			}
			toolResult := message.ToolResult{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    content,
				IsError:    true,
			}
			_, createErr = a.messages.Create(ctx, currentAssistant.SessionID, message.CreateMessageParams{
				Role: message.Tool,
				Parts: []message.ContentPart{
					toolResult,
				},
			})
			if createErr != nil {
				return turnOutcome{err: createErr}
			}
		}
		var fantasyErr *fantasy.Error
		var providerErr *fantasy.ProviderError
		const defaultTitle = "Provider Error"
		// Detect context-window-exceeded errors so we can recover by
		// summarizing and retrying instead of bubbling the error to
		// the user.
		ctxTooLarge := false
		if !isCancelErr && errors.As(err, &providerErr) && providerErr.IsContextTooLarge() && !a.disableAutoSummarize {
			ctxTooLarge = true
		}
		if isCancelErr {
			currentAssistant.AddFinish(message.FinishReasonCanceled, "User canceled request", "")
		} else if ctxTooLarge {
			currentAssistant.AddFinish(message.FinishReasonError, "Context Too Large", "Context window exceeded; summarizing and retrying.")
		} else if errors.As(err, &providerErr) {
			currentAssistant.AddFinish(message.FinishReasonError, cmp.Or(ext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message)
		} else if errors.As(err, &fantasyErr) {
			currentAssistant.AddFinish(message.FinishReasonError, cmp.Or(ext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message)
		} else {
			currentAssistant.AddFinish(message.FinishReasonError, defaultTitle, err.Error())
		}
		// Note: we use the parent context here because the genCtx has been
		// cancelled.
		updateErr := a.messages.Update(ctx, *currentAssistant)
		if updateErr != nil {
			return turnOutcome{err: updateErr}
		}
		if ctxTooLarge {
			slog.Warn(
				"Context window exceeded; summarizing and retrying",
				"session_id", call.SessionID,
				"max_tokens", providerErr.ContextMaxTokens,
				"used_tokens", providerErr.ContextUsedTokens,
			)
			// Busy release + Summarize + retry re-dispatch happen in the
			// dispatcher loop (Run) so they stay atomic with queue dispatch.
			retryCall := call
			retryCall.Prompt = fmt.Sprintf("The previous turn was interrupted because the context window was exceeded. Please continue with the original request: `%s`", call.Prompt)
			return turnOutcome{ctxTooLarge: true, retryCall: &retryCall, err: err}
		}
		return turnOutcome{err: err}
	}

	// Send notification that agent has finished its turn (skip for
	// nested/non-interactive sessions).
	if !call.NonInteractive && a.notify != nil {
		a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			SessionID:    call.SessionID,
			SessionTitle: currentSession.Title,
			Type:         notify.TypeAgentFinished,
		})
	}

	if shouldSummarize {
		// Busy release + Summarize + continuation enqueue happen in the
		// dispatcher loop (Run) so they stay atomic with queue dispatch.
		return turnOutcome{result: result, summarize: true, hadToolCalls: len(currentAssistant.ToolCalls()) > 0}
	}

	return turnOutcome{result: result}
}

func (a *sessionAgent) Summarize(ctx context.Context, sessionID string, opts fantasy.ProviderOptions) (string, error) {
	if a.IsSessionBusy(sessionID) {
		return "", ErrSessionBusy
	}

	// Copy mutable fields under lock to avoid races with SetModels.
	largeModel := a.largeModel.Get()
	systemPromptPrefix := a.systemPromptPrefix.Get()

	currentSession, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get session: %w", err)
	}
	msgs, err := a.getSessionMessages(ctx, currentSession)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		// Nothing to summarize.
		return "", nil
	}

	aiMsgs, _ := a.preparePrompt(msgs, true)

	genCtx, cancel := context.WithCancel(ctx)
	a.activeRequests.Set(sessionID, cancel)
	defer a.activeRequests.Del(sessionID)
	defer cancel()

	agent := fantasy.NewAgent(
		largeModel.Model,
		fantasy.WithSystemPrompt(string(summaryPrompt)),
		fantasy.WithUserAgent(userAgent),
	)
	summaryMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:             message.Assistant,
		Model:            largeModel.Model.Model(),
		Provider:         largeModel.Model.Provider(),
		IsSummaryMessage: true,
	})
	if err != nil {
		return "", err
	}

	summaryPromptText := buildSummaryPrompt(currentSession.Todos)

	resp, err := agent.Stream(genCtx, fantasy.AgentStreamCall{
		Prompt:          summaryPromptText,
		Messages:        aiMsgs,
		ProviderOptions: opts,
		PrepareStep: func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
			prepared.Messages = options.Messages
			if systemPromptPrefix != "" {
				prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(systemPromptPrefix)}, prepared.Messages...)
			}
			return callContext, prepared, nil
		},
		OnReasoningDelta: func(id string, text string) error {
			summaryMessage.AppendReasoningContent(text)
			return a.messages.Update(genCtx, summaryMessage)
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			// Handle anthropic signature.
			if anthropicData, ok := reasoning.ProviderMetadata["anthropic"]; ok {
				if signature, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok && signature.Signature != "" {
					summaryMessage.AppendReasoningSignature(signature.Signature)
				}
			}
			summaryMessage.FinishThinking()
			return a.messages.Update(genCtx, summaryMessage)
		},
		OnTextDelta: func(id, text string) error {
			summaryMessage.AppendContent(text)
			return a.messages.Update(genCtx, summaryMessage)
		},
	})
	if err != nil {
		isCancelErr := errors.Is(err, context.Canceled)
		if isCancelErr {
			// User cancelled summarize we need to remove the summary message.
			deleteErr := a.messages.Delete(ctx, summaryMessage.ID)
			return "", deleteErr
		}
		return "", err
	}

	summaryMessage.AddFinish(message.FinishReasonEndTurn, "", "")
	err = a.messages.Update(genCtx, summaryMessage)
	if err != nil {
		return "", err
	}

	var openrouterCost *float64
	for _, step := range resp.Steps {
		stepCost := a.openrouterCost(step.ProviderMetadata)
		if stepCost != nil {
			newCost := *stepCost
			if openrouterCost != nil {
				newCost += *openrouterCost
			}
			openrouterCost = &newCost
		}
	}

	a.updateSessionUsage(largeModel, &currentSession, resp.TotalUsage, openrouterCost)

	// Just in case, get just the last usage info.
	usage := resp.Response.Usage
	currentSession.SummaryMessageID = summaryMessage.ID
	currentSession.CompletionTokens = usage.OutputTokens
	currentSession.PromptTokens = 0
	_, err = a.sessions.Save(genCtx, currentSession)
	if err != nil {
		return "", err
	}

	summaryPath := ""
	if a.workingDir != "" {
		result, err := sessionexport.ExportSummary(sessionexport.SummaryOptions{
			SessionID:  currentSession.ID,
			Title:      currentSession.Title,
			Content:    summaryMessage.Content().Text,
			WorkingDir: a.workingDir,
			Now:        time.Now(),
		})
		if err != nil {
			return "", err
		}
		summaryPath = result.Path
	}
	return summaryPath, nil
}

func (a *sessionAgent) Cancel(sessionID string) {
	// Cancel active requests (regular turns and Summarize both register
	// under the bare sessionID). Don't use Take() here - we need the entry
	// to remain in activeRequests so IsBusy() returns true until the
	// goroutine fully completes (including error handling that may access
	// the DB). The dispatcher loop in Run releases the entry at the end of
	// each turn, on error returns, and via its panic safety net.
	if cancel, ok := a.activeRequests.Get(sessionID); ok && cancel != nil {
		slog.Debug("Request cancellation initiated", "session_id", sessionID)
		cancel()
	}

	// Cancel also clears the queue: the running dispatcher pops an empty
	// queue at its turn boundary and exits without starting further turns.
	// Hold runMu so a concurrent enqueue cannot race this clear (its call
	// would otherwise be deleted before any dispatcher sees it).
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.runMu.Lock()
		a.messageQueue.Del(sessionID)
		a.runMu.Unlock()
	}
}

func (a *sessionAgent) CancelAll() {
	if !a.IsBusy() {
		return
	}
	for key := range a.activeRequests.Seq2() {
		a.Cancel(key) // key is sessionID
	}

	timeout := time.After(5 * time.Second)
	for a.IsBusy() {
		select {
		case <-timeout:
			return
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (a *sessionAgent) ClearQueue(sessionID string) {
	if a.QueuedPrompts(sessionID) > 0 {
		slog.Debug("Clearing queued prompts", "session_id", sessionID)
		a.runMu.Lock()
		a.messageQueue.Del(sessionID)
		a.runMu.Unlock()
	}
}

func (a *sessionAgent) IsBusy() bool {
	var busy bool
	for cancelFunc := range a.activeRequests.Seq() {
		if cancelFunc != nil {
			busy = true
			break
		}
	}
	return busy
}

func (a *sessionAgent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Get(sessionID)
	return busy
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

func (a *sessionAgent) SetModels(large Model, small Model) {
	a.largeModel.Set(large)
	a.smallModel.Set(small)
}

func (a *sessionAgent) SetTools(tools []fantasy.AgentTool) {
	a.tools.SetSlice(tools)
}

func (a *sessionAgent) SetSystemPrompt(systemPrompt string) {
	a.systemPrompt.Set(systemPrompt)
}

func (a *sessionAgent) SystemPrompt() string {
	return a.systemPrompt.Get()
}

func (a *sessionAgent) Model() Model {
	return a.largeModel.Get()
}

// SmallModel returns the configured small model (used for cheap auxiliary
// calls like the /evo lesson distiller). It is a zero-value Model when the
// small model was never built, whose Model field is nil.
func (a *sessionAgent) SmallModel() Model {
	if a.smallModel == nil {
		return Model{}
	}
	return a.smallModel.Get()
}
