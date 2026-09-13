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

	"github.com/nextsko/mocode-agent/internal/core/agent/messages"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
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

		// Plain success: atomic end-of-turn 鈥?release, pop the next queued
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

// runTurn executes a single user turn end-to-end: BeforeAgent callback,
// user-message persistence, model streaming, finish handling and the
// TypeAgentFinished notification. It must be called with the session marked
// busy (genCtx/cancel registered by the dispatcher in Run).
func (a *sessionAgent) runTurn(ctx context.Context, genCtx context.Context, call SessionAgentCall) turnOutcome {
	// BeforeAgent callback 鈥?may skip execution entirely.
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

	// BeforeModel callback 鈥?may modify system prompt.
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
	// Shared mutable state for the stream callbacks (see agent_turn_callbacks.go).
	ts := &turnState{
		call:            call,
		largeModel:      largeModel,
		parentCtx:       ctx,
		genCtx:          genCtx,
		promptPrefix:    promptPrefix,
		assistant:       &currentAssistant,
		currentSession:  &currentSession,
		sessionLock:     &sessionLock,
		shouldSummarize: &shouldSummarize,
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
		PrepareStep:      a.prepareStep(ts),
		OnReasoningStart: a.onReasoningStart(ts),
		OnReasoningDelta: a.onReasoningDelta(ts),
		OnReasoningEnd:   a.onReasoningEnd(ts),
		OnTextDelta:      a.onTextDelta(ts),
		OnToolInputStart: a.onToolInputStart(ts),
		OnRetry:          a.onRetry(),
		OnToolCall:       a.onToolCall(ts),
		OnToolResult:     a.onToolResult(ts),
		OnStepFinish:     a.onStepFinish(ts),
		StopWhen: []fantasy.StopCondition{
			a.contextWindowStop(ts),
			loopStopCondition,
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

	summaryPromptText := messages.BuildSummaryPrompt(currentSession.Todos)

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
