package store

import (
	"context"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
)

// FileTrackerStore provides file-based read-tracking persistence.
type FileTrackerStore struct {
	store *Store

	mu    sync.RWMutex
	reads map[string]map[string]time.Time // reads[sessionID][filePath] = lastReadTime
}

func newFileTrackerStore(s *Store) *FileTrackerStore {
	return &FileTrackerStore{
		store: s,
		reads: make(map[string]map[string]time.Time),
	}
}

// RecordRead records that a file was read in a session.
func (fts *FileTrackerStore) RecordRead(ctx context.Context, sessionID, path string) {
	fts.mu.Lock()
	defer fts.mu.Unlock()

	sessionReads, ok := fts.reads[sessionID]
	if !ok {
		sessionReads = make(map[string]time.Time)
		fts.reads[sessionID] = sessionReads
	}
	sessionReads[path] = time.Now()
}

// LastReadTime returns the last time a file was read in a session.
func (fts *FileTrackerStore) LastReadTime(ctx context.Context, sessionID, path string) time.Time {
	fts.mu.RLock()
	defer fts.mu.RUnlock()

	sessionReads, ok := fts.reads[sessionID]
	if !ok {
		return time.Time{}
	}
	return sessionReads[path]
}

// ListReadFiles lists all files read in a session.
func (fts *FileTrackerStore) ListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	fts.mu.RLock()
	defer fts.mu.RUnlock()

	sessionReads, ok := fts.reads[sessionID]
	if !ok {
		return nil, nil
	}
	files := make([]string, 0, len(sessionReads))
	for path := range sessionReads {
		files = append(files, path)
	}
	return files, nil
}

// DeleteSession removes all read tracking data for a session.
func (fts *FileTrackerStore) DeleteSession(ctx context.Context, sessionID string) {
	fts.mu.Lock()
	defer fts.mu.Unlock()

	delete(fts.reads, sessionID)
}

// Ensure FileTrackerStore satisfies the interface (verified at compile time).
var _ filetracker.Service = (*FileTrackerStore)(nil)
