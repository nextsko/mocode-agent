package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/store"
	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

func newStoreSessionService(st *store.Store) *storeSessionService {
	return &storeSessionService{
		Broker: pubsub.NewBroker[session.Session](),
		store:  st.Sessions(),
	}
}

func newStoreMessageService(st *store.Store) *storeMessageService {
	svc := &storeMessageService{
		Broker: pubsub.NewBroker[message.Message](),
		store:  st.Messages(),
	}
	svc.debouncer = newMessageUpdateDebouncer(svc)
	return svc
}

func newStoreHistoryService(st *store.Store) *storeHistoryService {
	return &storeHistoryService{
		Broker: pubsub.NewBroker[history.File](),
		store:  st.Files(),
	}
}

func newStoreFileTrackerService(st *store.Store) *storeFileTrackerService {
	return &storeFileTrackerService{store: st.Tracker()}
}
func (s *storeSessionService) Create(ctx context.Context, title string) (session.Session, error) {
	sess, err := s.store.Create(ctx, title)
	if err != nil {
		return session.Session{}, err
	}
	s.Publish(pubsub.CreatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) Get(ctx context.Context, id string) (session.Session, error) {
	return s.store.Get(ctx, id)
}

func (s *storeSessionService) GetLast(ctx context.Context) (session.Session, error) {
	return s.store.GetLast(ctx)
}

func (s *storeSessionService) List(ctx context.Context) ([]session.Session, error) {
	return s.store.List(ctx)
}

func (s *storeSessionService) Save(ctx context.Context, sess session.Session) (session.Session, error) {
	if err := s.store.Save(ctx, sess); err != nil {
		return session.Session{}, err
	}
	s.Publish(pubsub.UpdatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) UpdateTitleAndUsage(ctx context.Context, id, title string, p, c, cacheRead, cacheCreation int64, cost float64) error {
	return s.store.UpdateTitleAndUsage(ctx, id, title, p, c, cacheRead, cacheCreation, cost)
}

func (s *storeSessionService) Rename(ctx context.Context, id, title string) error {
	return s.store.Rename(ctx, id, title)
}

func (s *storeSessionService) Delete(ctx context.Context, id string) error {
	sess, _ := s.store.Get(ctx, id)
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if sess.ID != "" {
		s.Publish(pubsub.DeletedEvent, sess)
	}
	return nil
}

func (s *storeSessionService) CreateTitleSession(ctx context.Context, pid string) (session.Session, error) {
	now := time.Now().Unix()
	sess := session.Session{ID: "title-" + pid, ParentSessionID: pid, Title: "Generate a title", CreatedAt: now, UpdatedAt: now}
	s.store.Save(ctx, sess)
	s.Publish(pubsub.CreatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) CreateTaskSession(ctx context.Context, tool, pid, title string) (session.Session, error) {
	now := time.Now().Unix()
	sess := session.Session{
		ID:              tool,
		ParentSessionID: pid,
		Title:           title,
		CreatedAt:       now,
		UpdatedAt:       now,
		AgentToolCallID: tool,
	}
	if err := s.store.Save(ctx, sess); err != nil {
		return session.Session{}, err
	}
	s.Publish(pubsub.CreatedEvent, sess)
	return sess, nil
}

func (s *storeSessionService) CreateAgentToolSessionID(mID, tcID string) string {
	return fmt.Sprintf("%s$$%s", mID, tcID)
}

func (s *storeSessionService) ParseAgentToolSessionID(id string) (string, string, bool) {
	parts := strings.Split(id, "$$")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *storeSessionService) IsAgentToolSession(id string) bool {
	_, _, ok := s.ParseAgentToolSessionID(id)
	return ok
}

func (s *storeSessionService) IncrementCost(ctx context.Context, id string, delta float64) error {
	return s.store.IncrementCost(ctx, id, delta)
}

// storeMessageService wraps *store.MessageStore.

func (s *storeMessageService) Create(ctx context.Context, sid string, params message.CreateMessageParams) (message.Message, error) {
	msg, err := s.store.Create(ctx, sid, params)
	if err != nil {
		return message.Message{}, err
	}
	s.Publish(pubsub.CreatedEvent, msg.Clone())
	return msg, nil
}

func (s *storeMessageService) Update(ctx context.Context, msg message.Message) error {
	return s.debouncer.Update(ctx, msg)
}

func (s *storeMessageService) Get(ctx context.Context, id string) (message.Message, error) {
	return s.store.Get(ctx, id)
}

func (s *storeMessageService) List(ctx context.Context, sid string) ([]message.Message, error) {
	return s.store.List(ctx, sid)
}

func (s *storeMessageService) ListUserMessages(ctx context.Context, sid string) ([]message.Message, error) {
	return s.store.ListUserMessages(ctx, sid)
}

func (s *storeMessageService) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return s.store.ListAllUserMessages(ctx)
}

func (s *storeMessageService) Delete(ctx context.Context, id string) error {
	msg, _ := s.store.Get(ctx, id)
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if msg.ID != "" {
		s.Publish(pubsub.DeletedEvent, msg.Clone())
	}
	return nil
}

func (s *storeMessageService) DeleteSessionMessages(ctx context.Context, sid string) error {
	return s.store.DeleteSessionMessages(ctx, sid)
}

// storeHistoryService wraps *store.FileHistoryStore.

func (s *storeHistoryService) Create(ctx context.Context, sid, path, content string) (history.File, error) {
	f, err := s.store.Create(ctx, sid, path, content)
	if err != nil {
		return history.File{}, err
	}
	s.Publish(pubsub.CreatedEvent, f)
	return f, nil
}

func (s *storeHistoryService) CreateVersion(ctx context.Context, sid, path, content string) (history.File, error) {
	f, err := s.store.CreateVersion(ctx, sid, path, content)
	if err != nil {
		return history.File{}, err
	}
	s.Publish(pubsub.CreatedEvent, f)
	return f, nil
}

func (s *storeHistoryService) Get(ctx context.Context, id string) (history.File, error) {
	return s.store.Get(ctx, id)
}

func (s *storeHistoryService) GetByPathAndSession(ctx context.Context, path, sid string) (history.File, error) {
	return s.store.GetByPathAndSession(ctx, path, sid)
}

func (s *storeHistoryService) ListBySession(ctx context.Context, sid string) ([]history.File, error) {
	return s.store.ListBySession(ctx, sid)
}

func (s *storeHistoryService) ListLatestSessionFiles(ctx context.Context, sid string) ([]history.File, error) {
	return s.store.ListLatestSessionFiles(ctx, sid)
}

func (s *storeHistoryService) Delete(ctx context.Context, id string) error {
	f, _ := s.store.Get(ctx, id)
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	if f.ID != "" {
		s.Publish(pubsub.DeletedEvent, f)
	}
	return nil
}

func (s *storeHistoryService) DeleteSessionFiles(ctx context.Context, sid string) error {
	return s.store.DeleteSessionFiles(ctx, sid)
}

// storeFileTrackerService wraps *store.FileTrackerStore.

func (s *storeFileTrackerService) RecordRead(ctx context.Context, sid, path string) {
	s.store.RecordRead(ctx, sid, path)
}

func (s *storeFileTrackerService) LastReadTime(ctx context.Context, sid, path string) time.Time {
	return s.store.LastReadTime(ctx, sid, path)
}

func (s *storeFileTrackerService) ListReadFiles(ctx context.Context, sid string) ([]string, error) {
	return s.store.ListReadFiles(ctx, sid)
}

func (s *storeFileTrackerService) DeleteSession(ctx context.Context, sid string) {
	s.store.DeleteSession(ctx, sid)
}
