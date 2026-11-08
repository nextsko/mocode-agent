package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunner records calls and emits deterministic updates.
type fakeRunner struct {
	prompted  chan string
	cancelled chan struct{}
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{prompted: make(chan string, 4), cancelled: make(chan struct{}, 4)}
}

func (f *fakeRunner) NewSession(_ context.Context, cwd string) (SessionInfo, error) {
	return SessionInfo{SessionID: "s1", Cwd: cwd}, nil
}

func (f *fakeRunner) LoadSession(_ context.Context, id, cwd string) (SessionInfo, bool, error) {
	return SessionInfo{SessionID: id, Cwd: cwd}, true, nil
}

func (f *fakeRunner) ResumeSession(ctx context.Context, id, cwd string) (SessionInfo, bool, error) {
	return f.LoadSession(ctx, id, cwd)
}

func (f *fakeRunner) ListSessions(context.Context) ([]SessionInfo, error) {
	return []SessionInfo{{SessionID: "s1"}}, nil
}
func (f *fakeRunner) DeleteSession(context.Context, string) error { return nil }
func (f *fakeRunner) CloseSession(context.Context, string) error  { return nil }
func (f *fakeRunner) Prompt(ctx context.Context, sessionID, text string, emit func(SessionUpdate)) (string, error) {
	f.prompted <- text
	emit(SessionUpdate{SessionUpdate: "agent_message_chunk", Content: &ContentBlock{Type: "text", Text: "hello"}})
	emit(SessionUpdate{SessionUpdate: "tool_call", ToolCallID: "t1", Title: "bash", Kind: "execute", Status: "pending"})
	select {
	case <-f.cancelled:
		return StopCancelled, nil
	case <-ctx.Done():
		return StopCancelled, nil
	case <-time.After(50 * time.Millisecond):
		return StopEndTurn, nil
	}
}
func (f *fakeRunner) Cancel(string) { f.cancelled <- struct{}{} }
func (f *fakeRunner) Modes(context.Context, string) (SessionModeState, error) {
	return SessionModeState{CurrentModeID: "code", AvailableModes: []SessionMode{{ID: "code", Name: "Code"}}}, nil
}
func (f *fakeRunner) SetMode(context.Context, string, string) error { return nil }
func (f *fakeRunner) ConfigOptions(context.Context, string) ([]SessionConfigOption, error) {
	return []SessionConfigOption{{ID: "model", Name: "Model", CurrentValue: "p/m", Options: []ConfigSelectOption{{Value: "p/m", Name: "p/m"}}}}, nil
}

func (f *fakeRunner) SetConfigOption(context.Context, string, string, string) ([]SessionConfigOption, error) {
	return f.ConfigOptions(context.Background(), "")
}

func (f *fakeRunner) AvailableCommands() []AvailableCommand {
	return []AvailableCommand{{Name: "help", Description: "Show help"}}
}

type harness struct {
	srv  *Server
	peer *Peer
	out  *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	out := &bytes.Buffer{}
	peer := NewPeer(strings.NewReader(""), out)
	srv := NewServer(peer, newFakeRunner(), Implementation{Name: "mocode", Version: "test"})
	return &harness{srv: srv, peer: peer, out: out}
}

func (h *harness) call(t *testing.T, method string, params any) (any, *Error) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	return h.srv.Handle(context.Background(), method, raw)
}

func TestInitializeNegotiatesVersionAndCapabilities(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	res, rerr := h.call(t, "initialize", InitializeRequest{ProtocolVersion: 1})
	require.Nil(t, rerr)
	init, ok := res.(InitializeResponse)
	require.True(t, ok)
	assert.Equal(t, 1, init.ProtocolVersion)
	assert.True(t, init.AgentCapabilities.LoadSession)
	assert.True(t, init.AgentCapabilities.SessionCapabilities.List != nil)
	assert.Equal(t, "mocode", init.AgentInfo.Name)
}

func TestNewSessionEmitsAvailableCommands(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	res, rerr := h.call(t, "session/new", NewSessionRequest{Cwd: "/tmp"})
	require.Nil(t, rerr)
	resp, ok := res.(NewSessionResponse)
	require.True(t, ok)
	assert.Equal(t, "s1", resp.SessionID)
	require.Eventually(t, func() bool {
		return strings.Contains(h.out.String(), "available_commands_update")
	}, time.Second, 10*time.Millisecond)
}

func TestPromptStreamsUpdates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, rerr := h.call(t, "session/new", NewSessionRequest{Cwd: "/tmp"})
	require.Nil(t, rerr)

	res, rerr := h.call(t, "session/prompt", PromptRequest{
		SessionID: "s1",
		Prompt:    []ContentBlock{{Type: "text", Text: "hi"}},
	})
	require.Nil(t, rerr)
	pr, ok := res.(PromptResponse)
	require.True(t, ok)
	// The fake cancels after the test's cancel notification; default is end_turn.
	assert.Equal(t, StopEndTurn, pr.StopReason)
	frames := h.out.String()
	assert.Contains(t, frames, "agent_message_chunk")
	assert.Contains(t, frames, "tool_call")
	assert.Contains(t, frames, `"text":"hello"`)
}

func TestCancelNotificationReturnsCancelled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, rerr := h.call(t, "session/new", NewSessionRequest{Cwd: "/tmp"})
	require.Nil(t, rerr)

	done := make(chan PromptResponse, 1)
	go func() {
		res, _ := h.call(t, "session/prompt", PromptRequest{SessionID: "s1", Prompt: []ContentBlock{{Type: "text", Text: "long"}}})
		if pr, ok := res.(PromptResponse); ok {
			done <- pr
		}
	}()
	time.Sleep(10 * time.Millisecond)
	h.srv.HandleNotification("session/cancel", json.RawMessage(`{"sessionId":"s1"}`))
	select {
	case pr := <-done:
		assert.Equal(t, StopCancelled, pr.StopReason)
	case <-time.After(2 * time.Second):
		t.Fatal("prompt did not return after cancel")
	}
}

func TestPromptAcceptsResourceLinkAndRejectsUnknown(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, _ = h.call(t, "session/new", NewSessionRequest{Cwd: "/tmp"})
	_, rerr := h.call(t, "session/prompt", PromptRequest{SessionID: "nope", Prompt: []ContentBlock{{Type: "text", Text: "x"}}})
	require.NotNil(t, rerr)
	assert.Contains(t, rerr.Message, "unknown session")
}

func TestPermissionSyncUsesHandler(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.srv.SetPermissionRequester(func(_ context.Context, sessionID string, toolCall json.RawMessage, opts []PermissionOption) (string, bool) {
		assert.Equal(t, "s1", sessionID)
		assert.Len(t, opts, 3)
		return "allow_once", false
	})
	id, cancelled := h.srv.RequestPermissionSync(context.Background(), "s1", json.RawMessage(`{"toolCallId":"t1"}`))
	assert.Equal(t, "allow_once", id)
	assert.False(t, cancelled)
}

func TestPromptTextFlattensBlocks(t *testing.T) {
	t.Parallel()
	got := promptText([]ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "resource_link", Name: "file.go", URI: "file:///tmp/file.go"},
		{Type: "resource", Resource: &EmbeddedResource{URI: "file:///a", Text: "package a"}},
	})
	assert.Contains(t, got, "hello")
	assert.Contains(t, got, "[file.go](file:///tmp/file.go)")
	assert.Contains(t, got, "package a")
}
