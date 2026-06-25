package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/package-register/mocode/internal/core/knowledge/memory"
)

// MemoryStore provides file-based memory persistence via JSONL.
type MemoryStore struct {
	store   *Store
	entries []*memory.Entry
	mu      sync.RWMutex
}

func (ms *MemoryStore) memoryPath() string {
	return filepath.Join(ms.store.ProjectDir, "memory", "entries.jsonl")
}

func newMemoryStore(s *Store) *MemoryStore {
	ms := &MemoryStore{store: s}
	ms.loadEntries()
	return ms
}

func (ms *MemoryStore) loadEntries() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	writer := NewJSONLWriter(ms.memoryPath())
	if !writer.Exists() {
		ms.entries = nil
		return
	}

	var loaded []*memory.Entry
	err := writer.ScanLines(func(line string) error {
		var entry memory.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil // skip corrupt lines
		}
		if entry.Memory != nil {
			loaded = append(loaded, &entry)
		}
		return nil
	})
	if err != nil {
		ms.entries = nil
		return
	}
	ms.entries = loaded
}

func (ms *MemoryStore) persistLocked() error {
	writer := NewJSONLWriter(ms.memoryPath())
	values := make([]any, len(ms.entries))
	for i, e := range ms.entries {
		values[i] = e
	}
	return writer.WriteAll(values)
}

func (ms *MemoryStore) AddMemory(ctx context.Context, appName, userID, mem string,
	topics []string, kind memory.Kind, eventTime *time.Time,
	participants []string, location string,
) error {
	now := time.Now()
	entry := &memory.Entry{
		AppName: appName,
		UserID:  userID,
		Memory: &memory.Memory{
			Memory:       mem,
			Topics:       topics,
			Kind:         kind,
			EventTime:    eventTime,
			Participants: participants,
			Location:     location,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	entry.ID = memory.GenerateMemoryID(entry.Memory, appName, userID)

	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.entries = append(ms.entries, entry)
	return ms.persistLocked()
}

func (ms *MemoryStore) UpdateMemory(ctx context.Context, appName, userID, memoryID, mem string,
	topics []string, kind memory.Kind, eventTime *time.Time,
	participants []string, location string,
) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, e := range ms.entries {
		if e.ID == memoryID && e.AppName == appName && e.UserID == userID {
			e.Memory.Memory = mem
			e.Memory.Topics = topics
			e.Memory.Kind = kind
			e.Memory.EventTime = eventTime
			e.Memory.Participants = participants
			e.Memory.Location = location
			e.UpdatedAt = time.Now()
			return ms.persistLocked()
		}
	}
	return fmt.Errorf("memory not found: %s", memoryID)
}

func (ms *MemoryStore) DeleteMemory(ctx context.Context, appName, userID, memoryID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	for i, e := range ms.entries {
		if e.ID == memoryID && e.AppName == appName && e.UserID == userID {
			ms.entries = append(ms.entries[:i], ms.entries[i+1:]...)
			return ms.persistLocked()
		}
	}
	return fmt.Errorf("memory not found: %s", memoryID)
}

func (ms *MemoryStore) ClearMemories(ctx context.Context, appName, userID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var kept []*memory.Entry
	for _, e := range ms.entries {
		if e.AppName != appName || e.UserID != userID {
			kept = append(kept, e)
		}
	}
	ms.entries = kept
	return ms.persistLocked()
}

func (ms *MemoryStore) ReadMemories(ctx context.Context, appName, userID string, limit int) ([]*memory.Entry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var result []*memory.Entry
	for _, e := range ms.entries {
		if e.AppName == appName && e.UserID == userID {
			result = append(result, e)
		}
	}
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}

func (ms *MemoryStore) SearchMemories(ctx context.Context, appName, userID, query string, limit int) ([]*memory.Entry, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var filtered []*memory.Entry
	for _, e := range ms.entries {
		if e.AppName == appName && e.UserID == userID {
			filtered = append(filtered, e)
		}
	}

	var results []*memory.Entry
	ql := strings.ToLower(query)
	for _, e := range filtered {
		if e.Memory == nil {
			continue
		}
		if strings.Contains(strings.ToLower(e.Memory.Memory), ql) {
			results = append(results, e)
		}
	}
	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	return results, nil
}

func (ms *MemoryStore) Close() error {
	return nil
}
