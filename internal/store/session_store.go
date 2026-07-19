package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextsko/mocode-agent/internal/domain/session"
)

// SessionStore provides file-based session persistence.
type SessionStore struct {
	store *Store
	index *sessionIndex // in-memory index, loaded at startup
	mu    sync.RWMutex  // Protects concurrent access to index
}

// sessionMeta is the on-disk per-session metadata.
type sessionMeta struct {
	ID                  string         `json:"id"`
	ParentSessionID     string         `json:"parent_session_id,omitempty"`
	RevertSessionID     string         `json:"revert_session_id,omitempty"`
	ActiveSnapshotID    string         `json:"active_snapshot_id,omitempty"`
	Title               string         `json:"title"`
	MessageCount        int64          `json:"message_count"`
	PromptTokens        int64          `json:"prompt_tokens"`
	CompletionTokens    int64          `json:"completion_tokens"`
	CacheReadTokens     int64          `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64          `json:"cache_creation_tokens,omitempty"`
	SummaryMessageID    string         `json:"summary_message_id,omitempty"`
	Cost                float64        `json:"cost"`
	Todos               []session.Todo `json:"todos,omitempty"`
	CreatedAt           int64          `json:"created_at"`
	UpdatedAt           int64          `json:"updated_at"`
}

// sessionIndex is the project-level sessions index.
type sessionIndex struct {
	Sessions  map[string]*sessionMeta `json:"sessions"`
	UpdatedAt int64                   `json:"updated_at"`
}

func newSessionStore(s *Store) *SessionStore {
	ss := &SessionStore{store: s}
	// Load the index from disk (lazy — no error if missing)
	ss.index = ss.loadIndex()
	return ss
}

func (ss *SessionStore) loadIndex() *sessionIndex {
	idx := &sessionIndex{Sessions: make(map[string]*sessionMeta)}
	path := ss.store.sessionsIndexPath()
	data, err := os.ReadFile(path)
	if err != nil {
		// No index yet — scan directories
		return ss.scanSessions()
	}
	if err := json.Unmarshal(data, idx); err != nil {
		return ss.scanSessions()
	}
	return idx
}

// scanSessions rebuilds the index by scanning the sessions directory.
func (ss *SessionStore) scanSessions() *sessionIndex {
	idx := &sessionIndex{Sessions: make(map[string]*sessionMeta)}
	dir := ss.store.sessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return idx
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPath := filepath.Join(dir, entry.Name(), ".meta.json")
		var meta sessionMeta
		if err := ReadJSON(metaPath, &meta); err != nil {
			continue
		}
		idx.Sessions[meta.ID] = &meta
	}
	return idx
}

func (ss *SessionStore) saveIndex() error {
	path := ss.store.sessionsIndexPath()
	ss.index.UpdatedAt = time.Now().Unix()
	return WriteJSON(path, ss.index)
}

func (ss *SessionStore) metaPath(sess session.Session) string {
	return filepath.Join(ss.store.sessionDir(sess), ".meta.json")
}

func (ss *SessionStore) writeMeta(sess session.Session) error {
	dir := ss.store.sessionDir(sess)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir session dir: %w", err)
	}
	meta := sessionMeta{
		ID:                  sess.ID,
		ParentSessionID:     sess.ParentSessionID,
		RevertSessionID:     sess.RevertSessionID,
		ActiveSnapshotID:    sess.ActiveSnapshotID,
		Title:               sess.Title,
		MessageCount:        sess.MessageCount,
		PromptTokens:        sess.PromptTokens,
		CompletionTokens:    sess.CompletionTokens,
		CacheReadTokens:     sess.CacheReadTokens,
		CacheCreationTokens: sess.CacheCreationTokens,
		SummaryMessageID:    sess.SummaryMessageID,
		Cost:                sess.Cost,
		Todos:               sess.Todos,
		CreatedAt:           sess.CreatedAt,
		UpdatedAt:           sess.UpdatedAt,
	}
	return WriteJSON(ss.metaPath(sess), meta)
}

func (ss *SessionStore) readMeta(sess session.Session) (*sessionMeta, error) {
	var meta sessionMeta
	if err := ReadJSON(ss.metaPath(sess), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (ss *SessionStore) toSession(meta *sessionMeta) session.Session {
	return session.Session{
		ID:                  meta.ID,
		ParentSessionID:     meta.ParentSessionID,
		RevertSessionID:     meta.RevertSessionID,
		ActiveSnapshotID:    meta.ActiveSnapshotID,
		Title:               meta.Title,
		MessageCount:        meta.MessageCount,
		PromptTokens:        meta.PromptTokens,
		CompletionTokens:    meta.CompletionTokens,
		CacheReadTokens:     meta.CacheReadTokens,
		CacheCreationTokens: meta.CacheCreationTokens,
		SummaryMessageID:    meta.SummaryMessageID,
		Cost:                meta.Cost,
		Todos:               meta.Todos,
		CreatedAt:           meta.CreatedAt,
		UpdatedAt:           meta.UpdatedAt,
	}
}

// Create creates a new session.
func (ss *SessionStore) Create(ctx context.Context, title string) (session.Session, error) {
	now := time.Now().Unix()
	sess := session.Session{
		ID:        uuid.New().String(),
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := ss.writeMeta(sess); err != nil {
		return session.Session{}, err
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	meta, _ := ss.readMeta(sess)
	ss.index.Sessions[sess.ID] = meta
	_ = ss.saveIndex()

	return sess, nil
}

// Get retrieves a session by ID.
func (ss *SessionStore) Get(ctx context.Context, id string) (session.Session, error) {
	ss.mu.RLock()
	if meta, ok := ss.index.Sessions[id]; ok {
		ss.mu.RUnlock()
		return ss.toSession(meta), nil
	}
	ss.mu.RUnlock()

	// Fallback: scan for it (with write lock to update index)
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.index = ss.scanSessions()
	if meta, ok := ss.index.Sessions[id]; ok {
		return ss.toSession(meta), nil
	}
	return session.Session{}, fmt.Errorf("session not found: %s", id)
}

// GetLast returns the most recently updated top-level session.
func (ss *SessionStore) GetLast(ctx context.Context) (session.Session, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	var latest *sessionMeta
	for _, meta := range ss.index.Sessions {
		if meta.ParentSessionID != "" {
			continue
		}
		if latest == nil || meta.UpdatedAt > latest.UpdatedAt {
			latest = meta
		}
	}
	if latest == nil {
		return session.Session{}, fmt.Errorf("no sessions found")
	}
	return ss.toSession(latest), nil
}

// List returns all top-level sessions, ordered by updated_at desc.
func (ss *SessionStore) List(ctx context.Context) ([]session.Session, error) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	var topLevel []*sessionMeta
	for _, meta := range ss.index.Sessions {
		if meta.ParentSessionID == "" {
			topLevel = append(topLevel, meta)
		}
	}
	sort.Slice(topLevel, func(i, j int) bool {
		return topLevel[i].UpdatedAt > topLevel[j].UpdatedAt
	})
	result := make([]session.Session, len(topLevel))
	for i, meta := range topLevel {
		result[i] = ss.toSession(meta)
	}
	return result, nil
}

// Save updates a session's metadata.
func (ss *SessionStore) Save(ctx context.Context, sess session.Session) error {
	sess.UpdatedAt = time.Now().Unix()
	if err := ss.writeMeta(sess); err != nil {
		return err
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	meta, _ := ss.readMeta(sess)
	if meta != nil {
		ss.index.Sessions[sess.ID] = meta
	}
	_ = ss.saveIndex()
	return nil
}

// UpdateTitleAndUsage atomically updates title and usage counters.
func (ss *SessionStore) UpdateTitleAndUsage(ctx context.Context, id, title string, promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens int64, cost float64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	meta, ok := ss.index.Sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	meta.Title = title
	meta.PromptTokens = promptTokens
	meta.CompletionTokens = completionTokens
	meta.CacheReadTokens = cacheReadTokens
	meta.CacheCreationTokens = cacheCreationTokens
	meta.Cost = cost
	meta.UpdatedAt = time.Now().Unix()
	if err := WriteJSON(ss.metaPath(ss.toSession(meta)), meta); err != nil {
		return err
	}
	_ = ss.saveIndex()
	return nil
}

// Rename updates only the session title.
func (ss *SessionStore) Rename(ctx context.Context, id, title string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	meta, ok := ss.index.Sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	meta.Title = title
	meta.UpdatedAt = time.Now().Unix()
	if err := WriteJSON(ss.metaPath(ss.toSession(meta)), meta); err != nil {
		return err
	}
	_ = ss.saveIndex()
	return nil
}

// Delete removes a session and its directory.
func (ss *SessionStore) Delete(ctx context.Context, id string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	meta, ok := ss.index.Sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	dir := ss.store.sessionDir(sessionMetaToSession(*meta))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove session dir: %w", err)
	}
	delete(ss.index.Sessions, id)
	_ = ss.saveIndex()
	return nil
}

// IncrementCost atomically adds delta to the session's cost.
// This prevents lost updates when multiple concurrent operations try to update cost.
// Uses the session lock to ensure read-modify-write atomicity.
func (ss *SessionStore) IncrementCost(ctx context.Context, id string, delta float64) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	meta, ok := ss.index.Sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	meta.Cost += delta
	meta.UpdatedAt = time.Now().Unix()

	if err := WriteJSON(ss.metaPath(ss.toSession(meta)), meta); err != nil {
		return fmt.Errorf("write session meta: %w", err)
	}

	_ = ss.saveIndex()
	return nil
}

// sessionMetaToSession converts a sessionMeta to a session.Session (package-level helper).
func sessionMetaToSession(meta sessionMeta) session.Session {
	return session.Session{
		ID:               meta.ID,
		ParentSessionID:  meta.ParentSessionID,
		RevertSessionID:  meta.RevertSessionID,
		ActiveSnapshotID: meta.ActiveSnapshotID,
		Title:            meta.Title,
		MessageCount:     meta.MessageCount,
		PromptTokens:     meta.PromptTokens,
		CompletionTokens: meta.CompletionTokens,
		SummaryMessageID: meta.SummaryMessageID,
		Cost:             meta.Cost,
		Todos:            meta.Todos,
		CreatedAt:        meta.CreatedAt,
		UpdatedAt:        meta.UpdatedAt,
	}
}
