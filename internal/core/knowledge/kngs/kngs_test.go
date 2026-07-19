package kngs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/knowledge/memory"
)

// mockMemory implements memory.Service for testing.
type mockMemory struct {
	mu      sync.Mutex
	entries map[string]string // key -> content
}

func (m *mockMemory) AddMemory(_ context.Context, _, _, mem string, topics []string, _ memory.Kind, _ *time.Time, _ []string, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = make(map[string]string)
	}
	m.entries["mem"] = mem
	return nil
}

func (m *mockMemory) DeleteMemory(_ context.Context, _, _, memoryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries != nil {
		m.entries["deleted"] = memoryID
	}
	return nil
}

func (m *mockMemory) UpdateMemory(_ context.Context, _, _, _, mem string, _ []string, _ memory.Kind, _ *time.Time, _ []string, _ string) error {
	return nil
}
func (m *mockMemory) ClearMemories(_ context.Context, _, _ string) error { return nil }
func (m *mockMemory) ReadMemories(_ context.Context, _, _ string, _ int) ([]*memory.Entry, error) {
	return nil, nil
}

func (m *mockMemory) SearchMemories(_ context.Context, _, _, _ string, _ int) ([]*memory.Entry, error) {
	return nil, nil
}
func (m *mockMemory) Tools() []fantasy.AgentTool { return nil }
func (m *mockMemory) Close() error               { return nil }

func TestStore_Sync(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "go-patterns.md"), []byte("Always use interfaces"), 0o644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "naming.md"), []byte("Use camelCase"), 0o644)
	require.NoError(t, err)

	mem := &mockMemory{}
	store := NewStore(mem, "test-app", "test-user")
	err = store.Sync(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Equal(t, 2, store.EntryCount())
}

func TestStore_Sync_NonExistentPath(t *testing.T) {
	mem := &mockMemory{}
	store := NewStore(mem, "test", "user")
	err := store.Sync(context.Background(), []string{"/nonexistent"})
	require.NoError(t, err)
	require.Equal(t, 0, store.EntryCount())
}

func TestStore_AddOrUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	err := os.WriteFile(path, []byte("Knowledge content"), 0o644)
	require.NoError(t, err)

	mem := &mockMemory{}
	store := NewStore(mem, "test", "user")
	err = store.AddOrUpdate(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, 1, store.EntryCount())
}

func TestStore_AddOrUpdate_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	err := os.WriteFile(path, []byte("   "), 0o644)
	require.NoError(t, err)

	mem := &mockMemory{}
	store := NewStore(mem, "test", "user")
	err = store.AddOrUpdate(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, 0, store.EntryCount())
}

func TestStore_Remove(t *testing.T) {
	mem := &mockMemory{entries: make(map[string]string)}
	store := NewStore(mem, "test", "user")

	store.entries["/path/to/file.md"] = "hash123"

	err := store.Remove(context.Background(), "/path/to/file.md")
	require.NoError(t, err)
	require.Equal(t, 0, store.EntryCount())

	mem.mu.Lock()
	deletedID := mem.entries["deleted"]
	mem.mu.Unlock()
	require.Contains(t, deletedID, "kngs_")
}

func TestStore_EntryKey(t *testing.T) {
	store := NewStore(nil, "test", "user")
	key1 := store.entryKey("/path/to/file.md")
	key2 := store.entryKey("/path/to/file.md")
	key3 := store.entryKey("/different/path.md")

	require.Equal(t, key1, key2, "same path should produce same key")
	require.NotEqual(t, key1, key3, "different paths should produce different keys")
	require.Contains(t, key1, "kngs_")
}

func TestStore_ExtractTopics(t *testing.T) {
	store := NewStore(nil, "test", "user")

	tests := []struct {
		path     string
		contains []string
	}{
		{"/agents/rules/go-patterns.md", []string{"knowledge", "rules", "md"}},
		{"/agents/kngs/python.txt", []string{"knowledge", "txt"}},
	}

	for _, tt := range tests {
		topics := store.extractTopics(tt.path)
		for _, want := range tt.contains {
			require.Contains(t, topics, want, "path %q should have topic %q", tt.path, want)
		}
	}
}

func TestWatcher_New(t *testing.T) {
	mem := &mockMemory{}
	store := NewStore(mem, "test", "user")
	w, err := NewWatcher(store)
	require.NoError(t, err)
	require.NotNil(t, w)
	defer w.Close()
}
