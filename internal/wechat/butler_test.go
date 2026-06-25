package wechat

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mockButlerWorkspace struct {
	mu       sync.Mutex
	sessions map[string]string
	titles   map[string]string
	messages map[string][]MsgInfo
	busy     map[string]bool
	created  []string
}

func newMockButlerWorkspace() *mockButlerWorkspace {
	return &mockButlerWorkspace{
		sessions: make(map[string]string),
		titles:   make(map[string]string),
		messages: make(map[string][]MsgInfo),
		busy:     make(map[string]bool),
	}
}

func (m *mockButlerWorkspace) CreateSession(_ context.Context, title string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "sess-" + title
	m.sessions[id] = title
	m.titles[id] = title
	m.created = append(m.created, id)
	return id, nil
}

func (m *mockButlerWorkspace) ListSessions(_ context.Context) ([]SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for id, title := range m.sessions {
		out = append(out, SessionInfo{ID: id, Title: title, CreatedAt: "now"})
	}
	return out, nil
}

func (m *mockButlerWorkspace) DeleteSession(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	delete(m.titles, sessionID)
	delete(m.messages, sessionID)
	return nil
}

func (m *mockButlerWorkspace) AgentRun(_ context.Context, sessionID, prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = append(m.messages[sessionID], MsgInfo{Role: "user", Content: prompt})
	if m.busy[sessionID] {
		return nil
	}
	m.messages[sessionID] = append(m.messages[sessionID], MsgInfo{Role: "assistant", Content: "reply:" + prompt})
	return nil
}

func (m *mockButlerWorkspace) ListMessages(_ context.Context, sessionID string) ([]MsgInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MsgInfo(nil), m.messages[sessionID]...), nil
}

func (m *mockButlerWorkspace) AgentIsSessionBusy(_ context.Context, sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[sessionID]
}

func (m *mockButlerWorkspace) setBusy(sessionID string, busy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busy[sessionID] = busy
}

func (m *mockButlerWorkspace) appendAssistant(sessionID, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = append(m.messages[sessionID], MsgInfo{Role: "assistant", Content: content})
}

func TestSessionKey(t *testing.T) {
	t.Parallel()
	if got := SessionKey("user1"); got != "wx:user1" {
		t.Fatalf("SessionKey() = %q, want wx:user1", got)
	}
}

func TestExtractAssistantAfter(t *testing.T) {
	t.Parallel()
	msgs := []MsgInfo{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "old"},
		{Role: "user", Content: "again"},
	}
	if got := extractAssistantAfter(msgs, 2); got != "" {
		t.Fatalf("expected empty before new assistant, got %q", got)
	}
	msgs = append(msgs, MsgInfo{Role: "assistant", Content: "new"})
	if got := extractAssistantAfter(msgs, 2); got != "new" {
		t.Fatalf("extractAssistantAfter() = %q, want new", got)
	}
}

func TestButlerPerUserSession(t *testing.T) {
	t.Parallel()
	ws := newMockButlerWorkspace()
	ch := New()
	h := newButlerHandler(&ButlerContext{Channel: ch, Workspace: ws})

	if err := h.ensureSession(context.Background(), "alice"); err != nil {
		t.Fatalf("ensureSession alice: %v", err)
	}
	if err := h.ensureSession(context.Background(), "bob"); err != nil {
		t.Fatalf("ensureSession bob: %v", err)
	}

	alice := h.butlerSessionID("alice")
	bob := h.butlerSessionID("bob")
	if alice == "" || bob == "" || alice == bob {
		t.Fatalf("expected distinct sessions, alice=%q bob=%q", alice, bob)
	}
}

func TestButlerWaitForAssistantReply(t *testing.T) {
	t.Parallel()
	ws := newMockButlerWorkspace()
	ch := New()
	h := newButlerHandler(&ButlerContext{Channel: ch, Workspace: ws})

	sid, err := ws.CreateSession(context.Background(), "Butler: u1")
	if err != nil {
		t.Fatal(err)
	}
	ws.messages[sid] = []MsgInfo{{Role: "user", Content: "init"}}
	before := len(ws.messages[sid])

	ws.setBusy(sid, true)
	go func() {
		time.Sleep(100 * time.Millisecond)
		ws.appendAssistant(sid, "delayed")
		ws.setBusy(sid, false)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reply, err := h.waitForAssistantReply(ctx, sid, before)
	if err != nil {
		t.Fatalf("waitForAssistantReply: %v", err)
	}
	if reply != "delayed" {
		t.Fatalf("reply = %q, want delayed", reply)
	}
}

func TestSwitchUpdatesBinding(t *testing.T) {
	t.Parallel()
	ws := newMockButlerWorkspace()
	ch := New()
	targetID, err := ws.CreateSession(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	h := newButlerHandler(&ButlerContext{Channel: ch, Workspace: ws})

	reply := h.handleButlerSlash(context.Background(), "user1", "/switch "+targetID)
	if reply == "" || contains(reply, "❌") {
		t.Fatalf("switch failed: %q", reply)
	}
	got, ok := ch.GetSession("user1")
	if !ok || got != targetID {
		t.Fatalf("GetSession() = %q, ok=%v, want %q", got, ok, targetID)
	}
}

func TestClearBindingsForSessionID(t *testing.T) {
	t.Parallel()
	ch := New()
	ch.SetSession("alice", "sess-a")
	ch.SetSession("bob", "sess-b")
	ch.ClearBindingsForSessionID("sess-a")
	if _, ok := ch.GetSession("alice"); ok {
		t.Fatal("alice binding should be cleared")
	}
	if got, ok := ch.GetSession("bob"); !ok || got != "sess-b" {
		t.Fatalf("bob binding = %q ok=%v", got, ok)
	}
}

func TestChannelSetSessionUsesSessionKey(t *testing.T) {
	t.Parallel()
	ch := New()
	ch.SetSession("user1", "sess-1")
	key := SessionKey("user1")
	v, ok := ch.sessions.Load(key)
	if !ok || v.(string) != "sess-1" {
		t.Fatalf("stored under %q = %v ok=%v", key, v, ok)
	}
}
