package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"

	"github.com/nextsko/mocode-agent/internal/core/agent/loopdetect"
	"github.com/nextsko/mocode-agent/internal/core/agent/messages"
	"github.com/nextsko/mocode-agent/internal/core/agent/notify"
	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

// turnState carries the mutable per-turn state shared between runTurn's
// orchestration and its stream callbacks. It exists so every callback body
// can live in a focused method instead of a 475-line god function.
type turnState struct {
	call       SessionAgentCall
	largeModel Model
	parentCtx  context.Context // survives mid-stream cancellation
	genCtx     context.Context // bound to the generation

	promptPrefix string
	// assistant is assigned by prepareStep and read by every later callback.
	assistant **message.Message
	// currentSession is updated by onStepFinish under sessionLock.
	currentSession  *session.Session
	sessionLock     *sync.Mutex
	shouldSummarize *bool
}

// prepareStep builds the per-step preparation callback: cache-control
// placement, prompt prefix, todo nudge, and the assistant message that later
// callbacks stream into.
func (a *sessionAgent) prepareStep(ts *turnState) func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
	return func(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
		prepared.Messages = options.Messages
		for i := range prepared.Messages {
			prepared.Messages[i].ProviderOptions = nil
		}

		// Use latest tools (updated by SetTools when MCP tools change).
		prepared.Tools = a.tools.Copy()

		// NOTE: queued prompts are intentionally NOT drained into the
		// in-flight turn — they only start at turn boundaries (see Run).

		prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, ts.largeModel)

		// Final safety: drop messages with empty content arrays to
		// prevent "messages.content.type is invalid" API errors.
		prepared.Messages = messages.FilterEmptyContentMessages(prepared.Messages)

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

		if ts.promptPrefix != "" {
			prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(ts.promptPrefix)}, prepared.Messages...)
		}

		// Mid-turn user guidance (Inject): drain the session buffer and
		// surface each text as a <user_guidance> system message so the model
		// steers immediately. The texts were already persisted as user
		// messages, so history stays complete for later turns.
		for _, guidance := range a.drainInjected(ts.call.SessionID) {
			gm := fantasy.NewSystemMessage("<user_guidance>\n" + guidance + "\n</user_guidance>")
			prepared.Messages = append([]fantasy.Message{gm}, prepared.Messages...)
		}

		// Todo nudge: surface open todos at each model step so the agent is
		// reminded to finish pending/in-progress items before declaring done.
		// Re-fetch the session for fresh todo state (the todos tool may have
		// updated it mid-run). Best-effort: a fetch failure just skips the nudge.
		if nudgedSession, nudgeErr := a.sessions.Get(callContext, ts.call.SessionID); nudgeErr == nil {
			if nudge := messages.BuildTodoNudge(nudgedSession.Todos); nudge != "" {
				prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(nudge)}, prepared.Messages...)
			}
		}

		var assistantMsg message.Message
		assistantMsg, err = a.messages.Create(callContext, ts.call.SessionID, message.CreateMessageParams{
			Role:     message.Assistant,
			Parts:    []message.ContentPart{},
			Model:    ts.largeModel.ModelCfg.Model,
			Provider: ts.largeModel.ModelCfg.Provider,
		})
		if err != nil {
			return callContext, prepared, err
		}
		callContext = context.WithValue(callContext, tools.MessageIDContextKey, assistantMsg.ID)
		callContext = context.WithValue(callContext, tools.SupportsImagesContextKey, ts.largeModel.CatwalkCfg.SupportsImages)
		callContext = context.WithValue(callContext, tools.ModelNameContextKey, ts.largeModel.CatwalkCfg.Name)
		*ts.assistant = &assistantMsg
		return callContext, prepared, err
	}
}

// ─── reasoning callbacks ─────────────────────────────────────────────────────

func (a *sessionAgent) onReasoningStart(ts *turnState) func(id string, reasoning fantasy.ReasoningContent) error {
	return func(_ string, reasoning fantasy.ReasoningContent) error {
		(*ts.assistant).AppendReasoningContent(reasoning.Text)
		return a.messages.Update(ts.genCtx, *(*ts.assistant))
	}
}

func (a *sessionAgent) onReasoningDelta(ts *turnState) func(id string, text string) error {
	return func(_ string, text string) error {
		(*ts.assistant).AppendReasoningContent(text)
		return a.messages.Update(ts.genCtx, *(*ts.assistant))
	}
}

func (a *sessionAgent) onReasoningEnd(ts *turnState) func(id string, reasoning fantasy.ReasoningContent) error {
	return func(_ string, reasoning fantasy.ReasoningContent) error {
		// Record the reasoning trace for the evolution/observability layer.
		if lg := toolutil.GetSessionLogger(ts.parentCtx); lg != nil && reasoning.Text != "" {
			lg.LogThink("reasoning", truncateForLog(reasoning.Text), toolutil.SessionLogMeta{})
		}
		// handle anthropic signature
		if anthropicData, ok := reasoning.ProviderMetadata[anthropic.Name]; ok {
			if reasoning, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok {
				(*ts.assistant).AppendReasoningSignature(reasoning.Signature)
			}
		}
		if googleData, ok := reasoning.ProviderMetadata[google.Name]; ok {
			if reasoning, ok := googleData.(*google.ReasoningMetadata); ok {
				(*ts.assistant).AppendThoughtSignature(reasoning.Signature, reasoning.ToolID)
			}
		}
		if openaiData, ok := reasoning.ProviderMetadata[openai.Name]; ok {
			if reasoning, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
				(*ts.assistant).SetReasoningResponsesData(reasoning)
			}
		}
		(*ts.assistant).FinishThinking()
		return a.messages.Update(ts.genCtx, *(*ts.assistant))
	}
}

// ─── text / tool-call callbacks ──────────────────────────────────────────────

func (a *sessionAgent) onTextDelta(ts *turnState) func(id string, text string) error {
	return func(_ string, text string) error {
		// Strip leading newline from initial text content. This is is
		// particularly important in non-interactive mode where leading
		// newlines are very visible.
		if len((*ts.assistant).Parts) == 0 {
			text = strings.TrimPrefix(text, "\n")
		}
		(*ts.assistant).AppendContent(text)
		return a.messages.Update(ts.genCtx, *(*ts.assistant))
	}
}

func (a *sessionAgent) onToolInputStart(ts *turnState) func(id string, toolName string) error {
	return func(id string, toolName string) error {
		// Send tool executing notification
		if !ts.call.NonInteractive && a.notify != nil {
			a.notify.Publish(pubsub.CreatedEvent, notify.Notification{
				SessionID: ts.call.SessionID,
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
		(*ts.assistant).AddToolCall(toolCall)
		// Use parent ctx instead of genCtx to ensure the update succeeds
		// even if the request is canceled mid-stream
		return a.messages.Update(ts.parentCtx, *(*ts.assistant))
	}
}

func (a *sessionAgent) onRetry() func(err *fantasy.ProviderError, delay time.Duration) {
	return func(err *fantasy.ProviderError, delay time.Duration) {
		slog.Warn("Provider request failed, retrying", messages.ProviderRetryLogFields(err, delay)...)
	}
}

func (a *sessionAgent) onToolCall(ts *turnState) func(tc fantasy.ToolCallContent) error {
	return func(tc fantasy.ToolCallContent) error {
		toolCall := message.ToolCall{
			ID:               tc.ToolCallID,
			Name:             tc.ToolName,
			Input:            tc.Input,
			ProviderExecuted: false,
			Finished:         true,
		}
		(*ts.assistant).AddToolCall(toolCall)
		// Record the tool invocation for the evolution/observability layer.
		if lg := toolutil.GetSessionLogger(ts.parentCtx); lg != nil {
			lg.LogToolCall("tool_call", tc.ToolName+": "+truncateForLog(tc.Input), toolutil.SessionLogMeta{
				ToolName: tc.ToolName, ToolCallID: tc.ToolCallID,
			})
		}
		// Use parent ctx instead of genCtx to ensure the update succeeds
		// even if the request is canceled mid-stream
		return a.messages.Update(ts.parentCtx, *(*ts.assistant))
	}
}

func (a *sessionAgent) onToolResult(ts *turnState) func(result fantasy.ToolResultContent) error {
	return func(result fantasy.ToolResultContent) error {
		toolResult := a.convertToToolResult(result)

		// Record tool results (and errors) for the evolution/observability layer.
		if lg := toolutil.GetSessionLogger(ts.parentCtx); lg != nil {
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
		_, createMsgErr := a.messages.Create(ts.parentCtx, (*ts.assistant).SessionID, message.CreateMessageParams{
			Role: message.Tool,
			Parts: []message.ContentPart{
				toolResult,
			},
		})
		return createMsgErr
	}
}

func (a *sessionAgent) onStepFinish(ts *turnState) func(stepResult fantasy.StepResult) error {
	return func(stepResult fantasy.StepResult) error {
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
		(*ts.assistant).AddFinish(finishReason, "", "")
		ts.sessionLock.Lock()
		defer ts.sessionLock.Unlock()

		updatedSession, getSessionErr := a.sessions.Get(ts.parentCtx, ts.call.SessionID)
		if getSessionErr != nil {
			return getSessionErr
		}
		a.updateSessionUsage(ts.largeModel, &updatedSession, stepResult.Usage, a.openrouterCost(stepResult.ProviderMetadata))
		_, sessionErr := a.sessions.Save(ts.parentCtx, updatedSession)
		if sessionErr != nil {
			return sessionErr
		}
		*ts.currentSession = updatedSession
		// Record per-step token usage for the evolution/observability layer.
		if lg := toolutil.GetSessionLogger(ts.parentCtx); lg != nil {
			lg.LogInfo("step_usage", fmt.Sprintf("finish=%s in=%d out=%d cache_read=%d cache_create=%d",
				finishReason, stepResult.Usage.InputTokens, stepResult.Usage.OutputTokens,
				stepResult.Usage.CacheReadTokens, stepResult.Usage.CacheCreationTokens),
				toolutil.SessionLogMeta{AgentID: ts.largeModel.ModelCfg.Model})
		}
		return a.messages.Update(ts.genCtx, *(*ts.assistant))
	}
}

// ─── stop conditions ─────────────────────────────────────────────────────────

func (a *sessionAgent) contextWindowStop(ts *turnState) func([]fantasy.StepResult) bool {
	return func(_ []fantasy.StepResult) bool {
		cw := int64(ts.largeModel.CatwalkCfg.ContextWindow)
		// If context window is unknown (0), skip auto-summarize
		// to avoid immediately truncating custom/local models.
		if cw == 0 {
			return false
		}
		tokens := ts.currentSession.CompletionTokens + ts.currentSession.PromptTokens
		remaining := cw - tokens
		var threshold int64
		if cw > largeContextWindowThreshold {
			threshold = largeContextWindowBuffer
		} else {
			threshold = int64(float64(cw) * smallContextWindowRatio)
		}
		if (remaining <= threshold) && !a.disableAutoSummarize {
			*ts.shouldSummarize = true
			return true
		}
		return false
	}
}

func loopStopCondition(steps []fantasy.StepResult) bool {
	return loopdetect.HasRepeatedToolCalls(steps, loopdetect.LoopDetectionWindowSize, loopdetect.LoopDetectionMaxRepeats)
}
