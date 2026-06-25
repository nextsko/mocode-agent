package session

import (
	"context"
)

// FileState, FileDiff, SessionSnapshot, and SnapshotService are
// temporarily disabled pending migration from SQLite to file-based storage.
// See .skcode/plans/sqlite-to-file-storage/ for details.

type SnapshotService struct{}

func NewSnapshotService(conn, queries interface{}) *SnapshotService {
	return &SnapshotService{}
}

func (s *SnapshotService) CreateSnapshot(_ context.Context, sessionID, messageID, description string) (*SessionSnapshot, error) {
	return nil, nil
}

func (s *SnapshotService) ListSnapshots(_ context.Context, sessionID string, limit int, beforeCursor string) ([]SessionSnapshot, error) {
	return nil, nil
}

func (s *SnapshotService) GetSnapshot(_ context.Context, snapshotID string) (*SessionSnapshot, error) {
	return nil, nil
}

func (s *SnapshotService) DiffSnapshots(_ context.Context, snapshotAID, snapshotBID string) ([]FileDiff, error) {
	return nil, nil
}

func (s *SnapshotService) RevertTo(_ context.Context, sessionID, targetSnapshotID string) ([]FileDiff, error) {
	return nil, nil
}

func (s *SnapshotService) Branch(_ context.Context, sessionService Service, parentSessionID, snapshotID, title string) (Session, error) {
	return Session{}, nil
}

type FileState struct {
	Path        string `json:"path"`
	Version     int64  `json:"version"`
	ContentHash string `json:"content_hash"`
}

type FileDiff struct {
	Path          string `json:"path"`
	BeforeVersion int64  `json:"before_version"`
	AfterVersion  int64  `json:"after_version"`
	Diff          string `json:"diff"`
	Additions     int    `json:"additions"`
	Deletions     int    `json:"deletions"`
}

type SessionSnapshot struct {
	ID               string      `json:"id"`
	SessionID        string      `json:"session_id"`
	ParentSnapshotID string      `json:"parent_snapshot_id,omitempty"`
	MessageID        string      `json:"message_id,omitempty"`
	Description      string      `json:"description"`
	FileStates       []FileState `json:"file_states"`
	TimeCreated      int64       `json:"time_created"`
}
