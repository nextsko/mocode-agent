package reviewer

import (
	"context"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/evolution/api"
	"github.com/nextsko/mocode-agent/internal/core/evolution/fact"
	memcore "github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
	memdom "github.com/nextsko/mocode-agent/internal/domain/memory"
	sessdom "github.com/nextsko/mocode-agent/internal/domain/session"
	msgdom "github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

// pubSub is a no-op Subscriber stub used to satisfy the Session.Service
// and Message.Service interfaces. The reviewer tests never subscribe.
type pubSub struct{}

func (pubSub) Subscribe(context.Context) <-chan pubsub.Event[sessdom.Session] {
	return nil
}

// msgPubSub is the message-typed sibling.
type msgPubSub struct{}

func (msgPubSub) Subscribe(context.Context) <-chan pubsub.Event[msgdom.Message] {
	return nil
}

// --- stubs -----------------------------------------------------------------

type stubModel struct{}

func (stubModel) Provider() string { return "stub" }
func (stubModel) Model() string    { return "stub-1" }
func (stubModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	// Return an empty facts list so the extractor succeeds.
	return &fantasy.Response{Content: fantasy.ResponseContent{fantasy.TextContent{Text: `{"facts":[]}`}}}, nil
}
func (stubModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	return nil, nil
}
func (stubModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}
func (stubModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

type stubMem struct{}

func (stubMem) AddMemory(context.Context, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	return nil
}
func (stubMem) UpdateMemory(context.Context, string, string, string, string, []string, memdom.Kind, *time.Time, []string, string) error {
	return nil
}
func (stubMem) DeleteMemory(context.Context, string, string, string) error { return nil }
func (stubMem) ClearMemories(context.Context, string, string) error       { return nil }
func (stubMem) ReadMemories(context.Context, string, string, int) ([]*memdom.Entry, error) {
	return nil, nil
}
func (stubMem) SearchMemories(context.Context, string, string, string, int) ([]*memdom.Entry, error) {
	return nil, nil
}
func (stubMem) Tools() []fantasy.AgentTool { return nil }
func (stubMem) Close() error               { return nil }

type stubSess struct {
	pubSub
	sessions []sessdom.Session
}

func (s *stubSess) List(_ context.Context) ([]sessdom.Session, error) {
	return s.sessions, nil
}
// Other methods are not exercised.
func (s *stubSess) Create(context.Context, string) (sessdom.Session, error) {
	return sessdom.Session{}, nil
}
func (s *stubSess) CreateTitleSession(context.Context, string) (sessdom.Session, error) {
	return sessdom.Session{}, nil
}
func (s *stubSess) CreateTaskSession(context.Context, string, string, string) (sessdom.Session, error) {
	return sessdom.Session{}, nil
}
func (s *stubSess) Get(context.Context, string) (sessdom.Session, error) { return sessdom.Session{}, nil }
func (s *stubSess) GetLast(context.Context) (sessdom.Session, error)      { return sessdom.Session{}, nil }
func (s *stubSess) Save(context.Context, sessdom.Session) (sessdom.Session, error) {
	return sessdom.Session{}, nil
}
func (s *stubSess) UpdateTitleAndUsage(context.Context, string, string, int64, int64, int64, int64, float64) error {
	return nil
}
func (s *stubSess) Rename(context.Context, string, string) error { return nil }
func (s *stubSess) Delete(context.Context, string) error         { return nil }
func (s *stubSess) IncrementCost(context.Context, string, float64) error {
	return nil
}
func (s *stubSess) CreateAgentToolSessionID(string, string) string { return "" }
func (s *stubSess) ParseAgentToolSessionID(string) (string, string, bool) {
	return "", "", false
}
func (s *stubSess) IsAgentToolSession(string) bool { return false }

var _ sessdom.Service = (*stubSess)(nil)

type stubMsg struct {
	msgPubSub
	msgs []msgdom.Message
}

func (s *stubMsg) List(_ context.Context, _ string) ([]msgdom.Message, error) {
	return s.msgs, nil
}
// other methods not exercised.
func (s *stubMsg) Create(context.Context, string, msgdom.CreateMessageParams) (msgdom.Message, error) {
	return msgdom.Message{}, nil
}
func (s *stubMsg) Update(context.Context, msgdom.Message) error { return nil }
func (s *stubMsg) Get(context.Context, string) (msgdom.Message, error) {
	return msgdom.Message{}, nil
}
func (s *stubMsg) ListUserMessages(context.Context, string) ([]msgdom.Message, error) {
	return nil, nil
}
func (s *stubMsg) ListAllUserMessages(context.Context) ([]msgdom.Message, error) {
	return nil, nil
}
func (s *stubMsg) Delete(context.Context, string) error              { return nil }
func (s *stubMsg) DeleteSessionMessages(context.Context, string) error {
	return nil
}

var _ msgdom.Service = (*stubMsg)(nil)

type recorder struct {
	mu     sync.Mutex
	events []api.Phase
}

func (r *recorder) RecordEvolutionEvent(_ string, p api.Phase, _ string, _ map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, p)
	return nil
}

// --- tests -----------------------------------------------------------------

func newReviewer(t *testing.T, sess sessdom.Service, msgs msgdom.Service, rec api.SessionRecorder) *Reviewer {
	t.Helper()
	r, err := New(api.Deps{
		Memory:     stubMem{},
		Sessions:   sess,
		Messages:   msgs,
		SmallModel: stubModel{},
		Recorder:   rec,
		Config: api.Config{
			Enabled:        true,
			ReviewEnabled:  true,
			ReviewInterval: 2, // tiny for tests
			ReviewTimeout:  time.Second,
			MinConfidence:  0.5,
		},
	}, Scope{AppName: "app", UserID: "user"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestAfterTurnFiresOnInterval(t *testing.T) {
	sess := &stubSess{sessions: []sessdom.Session{
		{ID: "s1", UpdatedAt: time.Now().Unix()},
	}}
	msgs := &stubMsg{msgs: []msgdom.Message{
		{ID: "m1", Role: msgdom.User},
		{ID: "m2", Role: msgdom.Assistant},
	}}
	rec := &recorder{}
	r := newReviewer(t, sess, msgs, rec)
	// ReviewInterval=2 → second AfterTurn should kick off the review.
	r.AfterTurn(1)
	r.AfterTurn(2)

	// Wait for the goroutine. We don't actually run an LLM, but the
	// goroutine calls runReview which reads recent messages; with our
	// stub sessions/msgs it should fail to find messages (UpdatedAt
	// matches the most recent session so List returns it; but List
	// returns []Session slice copy — fine).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	rec.mu.Lock()
	count := len(rec.events)
	rec.mu.Unlock()
	if count == 0 {
		t.Fatalf("expected at least one recorder event; got 0")
	}
}

func TestAfterTurnNoFireBelowInterval(t *testing.T) {
	rec := &recorder{}
	r := newReviewer(t, &stubSess{}, &stubMsg{}, rec)
	r.AfterTurn(1)
	// Don't wait for goroutines — interval is 2 and we only called once.
	r.mu.Lock()
	count := r.turnsSince
	r.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected turnsSince=1, got %d", count)
	}
}

func TestGateNoSessions(t *testing.T) {
	r := newReviewer(t, &stubSess{}, &stubMsg{}, &recorder{})
	// Reviewer doesn't have a Gate() in this version — but runReview should
	// return early when there are no sessions/messages.
	ctx := context.Background()
	res, err := r.RunNow(ctx)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if res.Extracted != 0 || res.Written != 0 {
		t.Fatalf("expected noop RunNow with no sessions, got %+v", res)
	}
}

func TestShutdownIdempotent(t *testing.T) {
	r := newReviewer(t, &stubSess{}, &stubMsg{}, &recorder{})
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

// Suppress unused import warnings during refactors.
var _ = fact.KindUserPref
var _ = memcore.Service(stubMem{})