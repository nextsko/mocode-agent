// Package wechat provides a scope-aware media store for downloaded/uploaded files.
package wechat

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MediaStore manages media files with scoped lifecycle and automatic cleanup.
type MediaStore struct {
	mu       sync.Mutex
	baseDir  string
	entries  map[string]*MediaEntry // mediaID → entry
	maxAge   time.Duration
	interval time.Duration
	stopCh   chan struct{}
}

// MediaEntry represents a managed media file.
type MediaEntry struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	ContentType string    `json:"content_type"`
	Source      string    `json:"source"` // e.g. "weixin", "tool:screenshot"
	Scope       string    `json:"scope"`  // e.g. "inbound", "outbound"
	CreatedAt   time.Time `json:"created_at"`
}

// NewMediaStore creates a new media store with automatic cleanup.
func NewMediaStore(baseDir string, maxAge time.Duration) *MediaStore {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".mocode", "wechat", "media")
	}
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	os.MkdirAll(baseDir, 0o755)
	return &MediaStore{
		baseDir:  baseDir,
		entries:  make(map[string]*MediaEntry),
		maxAge:   maxAge,
		interval: 1 * time.Hour,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background cleanup goroutine.
func (s *MediaStore) Start() {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.CleanExpired()
			}
		}
	}()
}

// Stop stops the background cleanup goroutine.
func (s *MediaStore) Stop() {
	close(s.stopCh)
}

// Store registers a file and returns a mediaID.
func (s *MediaStore) Store(path, contentType, source, scope string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := generateMediaID()
	s.entries[id] = &MediaEntry{
		ID:          id,
		Path:        path,
		ContentType: contentType,
		Source:      source,
		Scope:       scope,
		CreatedAt:   time.Now(),
	}
	return id
}

// Resolve returns the local path for a mediaID.
func (s *MediaStore) Resolve(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return "", false
	}
	return entry.Path, true
}

// ResolveWithMeta returns the full entry for a mediaID.
func (s *MediaStore) ResolveWithMeta(id string) (*MediaEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	return entry, ok
}

// ReleaseAll removes all entries in a scope and optionally deletes files.
func (s *MediaStore) ReleaseAll(scope string, deleteFiles bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.entries {
		if entry.Scope == scope {
			if deleteFiles {
				os.Remove(entry.Path)
			}
			delete(s.entries, id)
		}
	}
}

// CleanExpired removes entries older than maxAge.
func (s *MediaStore) CleanExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, entry := range s.entries {
		if now.Sub(entry.CreatedAt) > s.maxAge {
			os.Remove(entry.Path)
			delete(s.entries, id)
		}
	}
}

// Count returns the number of managed entries.
func (s *MediaStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func generateMediaID() string {
	var buf [16]byte
	for i := range buf {
		buf[i] = byte(time.Now().UnixNano() >> (8 * uint(i%8)))
	}
	return filepath.Base(string(buf[:]))
}
