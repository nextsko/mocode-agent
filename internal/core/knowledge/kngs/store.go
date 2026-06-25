// Package kngs provides a knowledge base that watches directories for file
// changes and automatically syncs content into the memory service.
//
// Files in watched directories are treated as knowledge entries. When a file
// is created, modified, or deleted, the corresponding memory entry is updated
// or removed, enabling hot-patching of the agent's knowledge base.
package kngs

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/package-register/mocode/internal/core/knowledge/memory"
)

// Store manages knowledge entries backed by the memory service.
type Store struct {
	mu      sync.RWMutex
	mem     memory.Service
	appName string
	userID  string
	entries map[string]string // path -> content hash
}

// NewStore creates a new knowledge store.
func NewStore(mem memory.Service, appName, userID string) *Store {
	return &Store{
		mem:     mem,
		appName: appName,
		userID:  userID,
		entries: make(map[string]string),
	}
}

// Sync loads all files from the given paths into memory.
func (s *Store) Sync(ctx context.Context, paths []string) error {
	for _, base := range paths {
		if err := s.syncDir(ctx, base); err != nil {
			slog.Warn("Failed to sync kngs directory", "path", base, "error", err)
		}
	}
	return nil
}

func (s *Store) syncDir(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := s.AddOrUpdate(ctx, path); err != nil {
			slog.Warn("Failed to load kngs entry", "path", path, "error", err)
		}
	}
	return nil
}

// AddOrUpdate reads a file and stores its content as a memory entry.
func (s *Store) AddOrUpdate(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read kngs file %s: %w", path, err)
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	topics := s.extractTopics(path)

	s.mu.Lock()
	s.entries[path] = hash
	s.mu.Unlock()

	now := time.Now()
	return s.mem.AddMemory(ctx, s.appName, s.userID, content, topics, memory.KindFact, &now, nil, "")
}

// Remove deletes the memory entry for a given file path.
func (s *Store) Remove(ctx context.Context, path string) error {
	s.mu.Lock()
	delete(s.entries, path)
	s.mu.Unlock()

	return s.mem.DeleteMemory(ctx, s.appName, s.userID, s.entryKey(path))
}

// entryKey generates a deterministic memory ID from a file path.
func (s *Store) entryKey(path string) string {
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("kngs_%x", hash[:8])
}

// extractTopics derives memory topics from the file path.
func (s *Store) extractTopics(path string) []string {
	var topics []string
	topics = append(topics, "knowledge")

	dir := filepath.Dir(path)
	// Use directory name as a topic if it's not "." or "kngs"
	if base := filepath.Base(dir); base != "." && strings.ToLower(base) != "kngs" {
		topics = append(topics, strings.ToLower(base))
	}

	// Add file extension as a topic hint
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	if ext != "" {
		topics = append(topics, ext)
	}

	return topics
}

// EntryCount returns the number of tracked knowledge entries.
func (s *Store) EntryCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
