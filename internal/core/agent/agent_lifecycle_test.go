package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/ctxcompress"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/csync"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

func TestMain(m *testing.M) {
	slog.SetLogLoggerLevel(slog.LevelError)
	os.Exit(m.Run())
}

// --- fakes -----------------------------------------------------------------

type fakeSessionService struct {
	mu       sync.Mutex
	sessions map[string]session.Session
	nextID   int
}

func newFakeSessionService() *fakeSessionService {
	return &fakeSessionService{sessions: map[string]session.Session{}}
}

func (f *fakeSessionService) Subscribe(context.Context) <-chan pubsub.Event[session.Session] {
	return make(chan pubsub.Event[session.Session])
}

func (f *fakeSessionService) Create(_ context.Context, title string) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	s := session.Session{ID: fmt.Sprintf("sess-%d", f.nextID), Title: title}
	f.sessions[s.ID] = s
	return s, nil
}

func (f *fakeSessionService) CreateTitleSession(context.Context, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) CreateTaskSession(context.Context, string, string, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) Get(_ context.Context, id string) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		return s, nil
	}
	return session.Session{}, fmt.Errorf("session not found: %s", id)
}

func (f *fakeSessionService) GetLast(context.Context) (session.Session, error) {
	return session.Session{}, errors.New("not implemented")
}

func (f *fakeSessionService) List(context.Context) ([]session.Session, error) { return nil, nil }

func (f *fakeSessionService) Save(_ context.Context, s session.Session) (session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	return s, nil
}

func (f *fakeSessionService) UpdateTitleAndUsage(context.Context, string, string, int64, int64, int64, int64, float64) error {
	return nil
}

func (f *fakeSessionService) Rename(_ context.Context, id, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		s.Title = title
		f.sessions[id] = s
	}
	return nil
}

func (f *fakeSessionService) Delete(context.Context, string) error { return nil }

func (f *fakeSessionService) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return messageID + "$$" + toolCallID
}

func (f *fakeSessionService) ParseAgentToolSessionID(id string) (string, string, bool) {
	if m, t, ok := strings.Cut(id, "$$"); ok {
		return m, t, true
	}
	return "", "", false
}

func (f *fakeSessionService) IsAgentToolSession(id string) bool {
	_, _, ok := f.ParseAgentToolSessionID(id)
	return ok
}

func (f *fakeSessionService) IncrementCost(context.Context, string, float64) error { return nil }

type fakeMessageService struct {
	mu  sync.Mutex
	msg []message.Message
}

func newFakeMessageService() *fakeMessageService { return &fakeMessageService{} }

func (f *fakeMessageService) Subscribe(context.Context) <-chan pubsub.Event[message.Message] {
	return make(chan pubsub.Event[message.Message])
}

func (f *fakeMessageService) Create(_ context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msg = append(f.msg, message.Message{
		ID:               fmt.Sprintf("msg-%d", len(f.msg)+1),
		Role:             params.Role,
		SessionID:        sessionID,
		Parts:            params.Parts,
		Model:            params.Model,
		Provider:         params.Provider,
		IsSummaryMessage: params.IsSummaryMessage,
	})
	return f.msg[len(f.msg)-1], nil
}

func (f *fakeMessageService) Update(_ context.Context, m message.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.msg {
		if f.msg[i].ID == m.ID {
			f.msg[i] = m
			return nil
		}
	}
	return fmt.Errorf("message not found: %s", m.ID)
}

func (f *fakeMessageService) Get(_ context.Context, id string) (message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msg {
		if m.ID == id {
			return m, nil
		}
	}
	return message.Message{}, fmt.Errorf("message not found: %s", id)
}

func (f *fakeMessageService) List(_ context.Context, sessionID string) ([]message.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []message.Message
	for _, m := range f.msg {
		if m.SessionID == sessionID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMessageService) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	all, err := f.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var out []message.Message
	for _, m := range all {
		if m.Role == message.User {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeMessageService) ListAllUserMessages(context.Context) ([]message.Message, error) {
	return nil, nil
}

func (f *fakeMessageService) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.msg {
		if f.msg[i].ID == id {
			f.msg = append(f.msg[:i], f.msg[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakeMessageService) DeleteSessionMessages(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []message.Message
	for _, m := range f.msg {
		if m.SessionID != sessionID {
			out = append(out, m)
		}
	}
	f.msg = out
	return nil
}

// scriptedResponse defines one scripted model Stream() result.
type scriptedResponse struct {
	text     string          // text content to stream
	toolName string          // if set, emit a tool call (finish reason tool_calls)
	until    <-chan struct{} // if set, block until closed (or ctx done)
	err      error           // if set, Stream returns this error immediately
}

// scriptedModel is a fantasy.LanguageModel that replays scripted responses
// (repeating the last entry once exhausted) and records every call.
type scriptedModel struct {
	mu     sync.Mutex
	script []scriptedResponse
	next   int
	calls  []fantasy.Call
}

func (m *scriptedModel) Provider() string { return "scripted" }
func (m *scriptedModel) Model() string    { return "scripted-model" }
func (m *scriptedModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not implemented")
}
func (m *scriptedModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *scriptedModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *scriptedModel) callsSnapshot() []fantasy.Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]fantasy.Call, len(m.calls))
	copy(out, m.calls)
	return out
}

func (m *scriptedModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, call)
	idx := m.next
	if idx >= len(m.script) {
		idx = len(m.script) - 1
	} else {
		m.next++
	}
	resp := m.script[idx]
	m.mu.Unlock()

	if resp.err != nil {
		return nil, resp.err
	}
	finishReason := fantasy.FinishReasonStop
	if resp.toolName != "" {
		finishReason = fantasy.FinishReasonToolCalls
	}
	return func(yield func(fantasy.StreamPart) bool) {
		if resp.until != nil {
			select {
			case <-resp.until:
			case <-ctx.Done():
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: ctx.Err()})
				return
			}
		}
		if resp.toolName != "" {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: "tc_1", ToolCallName: resp.toolName}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: "tc_1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: "tc_1", ToolCallName: resp.toolName, ToolCallInput: "{}"}) {
				return
			}
		}
		if resp.text != "" {
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "txt_1"}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "txt_1", Delta: resp.text}) {
				return
			}
			if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "txt_1"}) {
				return
			}
		}
		yield(fantasy.StreamPart{
			Type:         fantasy.StreamPartTypeFinish,
			FinishReason: finishReason,
			Usage:        fantasy.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		})
	}, nil
}

// promptText flattens a fantasy.Prompt for substring assertions.
func promptText(p fantasy.Prompt) string {
	var sb strings.Builder
	for _, msg := range p {
		for _, part := range msg.Content {
			if t, ok := fantasy.AsMessagePart[fantasy.TextPart](part); ok {
				sb.WriteString(t.Text)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func newLifecycleTestAgent(t *testing.T, large, small fantasy.LanguageModel, agentTools []fantasy.AgentTool, callbacks *AgentCallbacks) (*sessionAgent, *fakeSessionService, *fakeMessageService) {
	t.Helper()
	sessions := newFakeSessionService()
	messages := newFakeMessageService()
	sa := &sessionAgent{
		largeModel:         csync.NewValue(Model{Model: large, CatwalkCfg: catwalk.Model{ContextWindow: 0}}),
		smallModel:         csync.NewValue(Model{Model: small, CatwalkCfg: catwalk.Model{ContextWindow: 0}}),
		systemPromptPrefix: csync.NewValue(""),
		systemPrompt:       csync.NewValue("test system prompt"),
		tools:              csync.NewSliceFrom(agentTools),
		sessions:           sessions,
		messages:           messages,
		compressor:         ctxcompress.NewPipeline(ctxcompress.DefaultPolicy()),
		messageQueue:       csync.NewMap[string, []SessionAgentCall](),
		activeRequests:     csync.NewMap[string, context.CancelFunc](),
		callbacks:          callbacks,
	}
	return sa, sessions, messages
}

// --- tests -----------------------------------------------------------------

// 1. Serial progression: while turn A runs, Run(B) returns (nil, nil); after
// A finishes, B runs as an independent turn and never appears in A's stream.
func TestRunSerialProgression_QueuedPromptWaitsForTurnBoundary(t *testing.T) {
	releaseA := make(chan struct{})
	large := &scriptedModel{script: []scriptedResponse{
		{text: "result A", until: releaseA},
		{text: "result B"},
	}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, messages := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "test")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
		assert.NoError(t, err)
		assert.NotNil(t, res)
	}()

	// Wait until turn A is in-flight, then submit B.
	require.Eventually(t, func() bool { return len(large.callsSnapshot()) >= 1 },
		5*time.Second, 10*time.Millisecond)
	bRes, bErr := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task B"})
	require.NoError(t, bErr)
	require.Nil(t, bRes)
	require.Equal(t, 1, agent.QueuedPrompts(sess.ID))

	// B's prompt never appears in any provider call made during turn A.
	for _, c := range large.callsSnapshot() {
		require.NotContains(t, promptText(c.Prompt), "task B")
	}

	close(releaseA)
	<-done

	// B ran afterwards as an independent turn.
	calls := large.callsSnapshot()
	require.Len(t, calls, 2)
	require.Contains(t, promptText(calls[1].Prompt), "task B")
	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))
	require.False(t, agent.IsSessionBusy(sess.ID))

	msgs, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	var userOrder []string
	for _, m := range msgs {
		if m.Role == message.User {
			userOrder = append(userOrder, m.Content().Text)
		}
	}
	require.Equal(t, []string{"task A", "task B"}, userOrder)
}

// 2. Drain removal: a prompt enqueued mid-turn never enters the next model
// step's prepared messages.
func TestRunNoDrain_QueuedPromptNotInjectedIntoInFlightTurn(t *testing.T) {
	releaseTool := make(chan struct{})
	toolEntered := make(chan struct{})
	blockTool := fantasy.NewAgentTool("block_tool", "blocks until released",
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			close(toolEntered)
			<-releaseTool
			return fantasy.NewTextResponse("tool done"), nil
		})
	large := &scriptedModel{script: []scriptedResponse{
		{toolName: "block_tool"}, // step 1: tool call
		{text: "A finished"},     // step 2: turn ends
		{text: "B finished"},     // turn B
	}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, _ := newLifecycleTestAgent(t, large, small, []fantasy.AgentTool{blockTool}, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "test")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
	}()

	<-toolEntered // tool executes between step 1 and step 2
	res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task B"})
	require.NoError(t, err)
	require.Nil(t, res)
	require.Equal(t, 1, agent.QueuedPrompts(sess.ID))

	callsDuringTurn := large.callsSnapshot()
	require.Len(t, callsDuringTurn, 1) // only step 1 so far

	close(releaseTool)
	<-done

	calls := large.callsSnapshot()
	require.Len(t, calls, 3)
	// Step 2 (index 1) ran with B already queued — must NOT contain B.
	require.NotContains(t, promptText(calls[1].Prompt), "task B")
	// B ran as its own turn afterwards.
	require.Contains(t, promptText(calls[2].Prompt), "task B")
}

// 3. Queue drain order: A, B, C execute sequentially; RunAfterAgent fires once.
func TestRunQueueDrainOrder_ABCWithSingleAfterAgent(t *testing.T) {
	var afterCount atomic.Int32
	callbacks := &AgentCallbacks{
		AfterAgent: func(_ context.Context, _ *SessionAgentCall, result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error) {
			afterCount.Add(1)
			return result, err
		},
	}
	releaseA := make(chan struct{})
	large := &scriptedModel{script: []scriptedResponse{
		{until: releaseA}, // turn A blocks until B and C are queued
		{text: "B ok"},   // turn B
		{text: "C ok"},   // turn C
	}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, _ := newLifecycleTestAgent(t, large, small, nil, callbacks)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "test")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
	}()
	// Wait until turn A is in-flight (its model call is recorded forever,
	// so this is a deterministic sync point).
	require.Eventually(t, func() bool { return len(large.callsSnapshot()) >= 1 },
		5*time.Second, 5*time.Millisecond)

	for _, p := range []string{"task B", "task C"} {
		res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: p})
		require.NoError(t, err)
		require.Nil(t, res)
	}
	close(releaseA)
	<-done

	calls := large.callsSnapshot()
	require.Len(t, calls, 3)
	require.Contains(t, promptText(calls[0].Prompt), "task A")
	require.NotContains(t, promptText(calls[0].Prompt), "task B")
	require.Contains(t, promptText(calls[1].Prompt), "task B")
	require.NotContains(t, promptText(calls[1].Prompt), "task C")
	require.Contains(t, promptText(calls[2].Prompt), "task C")
	require.Equal(t, int32(1), afterCount.Load())
	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))
	require.False(t, agent.IsSessionBusy(sess.ID))
}

// 4. Cancel mid-turn: queue cleared, no further turn starts, session recovers.
func TestRunCancelMidTurn_StopsFurtherTurns(t *testing.T) {
	release := make(chan struct{})
	large := &scriptedModel{script: []scriptedResponse{
		{until: release},
		{text: "should not run"},
		{text: "after cancel"},
	}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, _ := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "test")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
		assert.Nil(t, res)
		assert.ErrorIs(t, err, context.Canceled)
	}()

	require.Eventually(t, func() bool { return len(large.callsSnapshot()) >= 1 },
		5*time.Second, 10*time.Millisecond)
	res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task B"})
	require.NoError(t, err)
	require.Nil(t, res)

	agent.Cancel(sess.ID) // cancels in-flight turn + clears queue
	<-done

	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))
	require.False(t, agent.IsSessionBusy(sess.ID))
	require.Len(t, large.callsSnapshot(), 1) // B never started

	// Session is not stuck: a fresh prompt runs normally.
	res2, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "after cancel"})
	require.NoError(t, err)
	require.NotNil(t, res2)
	require.Len(t, large.callsSnapshot(), 2)
}

// 5. ctx-too-large: provider errors once → Summarize runs → retry turn runs
// with no user intervention.
func TestRunContextTooLarge_AutoSummarizeAndRetry(t *testing.T) {
	large := &scriptedModel{script: []scriptedResponse{
		{err: &fantasy.ProviderError{Title: "prompt too long", Message: "context length exceeded", ContextTooLargeErr: true}},
		{text: "summary text"}, // Summarize call
		{text: "retry done"},   // retried turn
	}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, messages := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "test")
	require.NoError(t, err)

	res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "original request"})
	require.NoError(t, err)
	require.NotNil(t, res)

	calls := large.callsSnapshot()
	require.Len(t, calls, 3) // failed turn + summarize + retry
	require.Contains(t, promptText(calls[2].Prompt), "previous turn was interrupted")

	msgs, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	hasSummary := false
	for _, m := range msgs {
		if m.IsSummaryMessage {
			hasSummary = true
		}
	}
	require.True(t, hasSummary, "expected a summary message to be persisted")
	require.False(t, agent.IsSessionBusy(sess.ID))
	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))
}

// 6. Concurrent enqueue: N goroutines Run() while busy → no loss.
func TestRunConcurrentEnqueue_NoLoss(t *testing.T) {
	const n = 8
	release := make(chan struct{})
	large := &scriptedModel{script: []scriptedResponse{{until: release}, {text: "ok"}}}
	small := &scriptedModel{script: []scriptedResponse{{text: "Title"}}}
	agent, sessions, messages := newLifecycleTestAgent(t, large, small, nil, nil)

	ctx := t.Context()
	sess, err := sessions.Create(ctx, "test")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: "task A"})
		assert.NoError(t, err)
		assert.NotNil(t, res)
	}()
	require.Eventually(t, func() bool { return len(large.callsSnapshot()) >= 1 },
		5*time.Second, 10*time.Millisecond)

	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			res, err := agent.Run(ctx, SessionAgentCall{SessionID: sess.ID, Prompt: fmt.Sprintf("task %d", i)})
			assert.NoError(t, err)
			assert.Nil(t, res)
		})
	}
	wg.Wait()
	require.Equal(t, n, agent.QueuedPrompts(sess.ID))

	close(release)
	<-done

	require.Len(t, large.callsSnapshot(), n+1)
	require.Equal(t, 0, agent.QueuedPrompts(sess.ID))
	require.False(t, agent.IsSessionBusy(sess.ID))

	msgs, err := messages.List(ctx, sess.ID)
	require.NoError(t, err)
	count := 0
	for _, m := range msgs {
		if m.Role == message.User && strings.HasPrefix(m.Content().Text, "task ") {
			count++
		}
	}
	require.Equal(t, n+1, count) // "task A" + the n enqueued prompts
}
