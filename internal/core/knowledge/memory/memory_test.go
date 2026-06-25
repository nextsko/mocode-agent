package memory

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	f, err := os.CreateTemp("", "memory-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Remove(f.Name())
	})

	db, err := sql.Open("sqlite", f.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create memories table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			app_name TEXT NOT NULL,
			user_id TEXT NOT NULL,
			memory_data TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			deleted_at INTEGER
		);
		CREATE INDEX IF NOT EXISTS idx_memories_user ON memories(app_name, user_id);
		CREATE INDEX IF NOT EXISTS idx_memories_deleted ON memories(deleted_at);
	`)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

func TestMemoryService_AddMemory(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc, err := NewService(ServiceConfig{
		DB: db,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	appName := "test-app"
	userID := "test-user"
	memory := "User prefers dark mode"

	err = svc.AddMemory(ctx, appName, userID, memory, []string{"preference"}, KindFact, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Verify memory was added
	entries, err := svc.ReadMemories(ctx, appName, userID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(entries))
	}

	if entries[0].Memory.Memory != memory {
		t.Fatalf("expected memory %q, got %q", memory, entries[0].Memory.Memory)
	}
}

func TestMemoryService_SearchMemories(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc, err := NewService(ServiceConfig{
		DB: db,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	appName := "test-app"
	userID := "test-user"

	// Add test memories
	memories := []string{
		"User likes Python programming",
		"User prefers dark mode",
		"User works at tech company",
	}

	for _, mem := range memories {
		err = svc.AddMemory(ctx, appName, userID, mem, []string{"info"}, KindFact, nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
	}

	// Search for Python
	entries, err := svc.SearchMemories(ctx, appName, userID, "Python", 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) == 0 {
		t.Fatal("expected search results, got none")
	}

	// First result should be about Python
	if entries[0].Score == 0 {
		t.Fatal("expected non-zero score")
	}
}

func TestMemoryService_UpdateMemory(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc, err := NewService(ServiceConfig{
		DB: db,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	appName := "test-app"
	userID := "test-user"
	memory := "User likes Python"

	// Add memory
	err = svc.AddMemory(ctx, appName, userID, memory, []string{"lang"}, KindFact, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Read to get ID
	entries, err := svc.ReadMemories(ctx, appName, userID, 1)
	if err != nil {
		t.Fatal(err)
	}
	memoryID := entries[0].ID

	// Update memory
	updatedMemory := "User likes Python and Go"
	err = svc.UpdateMemory(ctx, appName, userID, memoryID, updatedMemory, []string{"lang"}, KindFact, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Verify update
	entries, err = svc.ReadMemories(ctx, appName, userID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(entries))
	}

	if entries[0].Memory.Memory != updatedMemory {
		t.Fatalf("expected updated memory %q, got %q", updatedMemory, entries[0].Memory.Memory)
	}
}

func TestMemoryService_DeleteMemory(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc, err := NewService(ServiceConfig{
		DB: db,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	appName := "test-app"
	userID := "test-user"
	memory := "User likes Python"

	// Add memory
	err = svc.AddMemory(ctx, appName, userID, memory, []string{"lang"}, KindFact, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Read to get ID
	entries, err := svc.ReadMemories(ctx, appName, userID, 1)
	if err != nil {
		t.Fatal(err)
	}
	memoryID := entries[0].ID

	// Delete memory
	err = svc.DeleteMemory(ctx, appName, userID, memoryID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify deletion
	entries, err = svc.ReadMemories(ctx, appName, userID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Fatalf("expected 0 memories after delete, got %d", len(entries))
	}
}

func TestMemoryService_EpisodicMemory(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc, err := NewService(ServiceConfig{
		DB: db,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	appName := "test-app"
	userID := "test-user"

	eventTime := time.Date(2024, 5, 7, 10, 30, 0, 0, time.UTC)
	participants := []string{"Alice", "Bob"}
	location := "Mt. Fuji"

	// Add episodic memory
	err = svc.AddMemory(
		ctx, appName, userID,
		"User went hiking",
		[]string{"activity"},
		KindEpisode,
		&eventTime,
		participants,
		location,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Verify episodic memory
	entries, err := svc.ReadMemories(ctx, appName, userID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(entries))
	}

	if entries[0].Memory.Kind != KindEpisode {
		t.Fatalf("expected kind %q, got %q", KindEpisode, entries[0].Memory.Kind)
	}

	if entries[0].Memory.EventTime == nil {
		t.Fatal("expected event time to be set")
	}

	if len(entries[0].Memory.Participants) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(entries[0].Memory.Participants))
	}

	if entries[0].Memory.Location != location {
		t.Fatalf("expected location %q, got %q", location, entries[0].Memory.Location)
	}
}

func TestMemoryService_MemoryLimit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc, err := NewService(ServiceConfig{
		DB:               db,
		MaxMemories:      3,
		MaxSearchResults: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	appName := "test-app"
	userID := "test-user"

	// Add 3 memories (at limit)
	for i := 0; i < 3; i++ {
		err = svc.AddMemory(ctx, appName, userID, "Memory "+string(rune('A'+i)), nil, KindFact, nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
	}

	// Try to add 4th memory (should fail)
	err = svc.AddMemory(ctx, appName, userID, "Memory D", nil, KindFact, nil, nil, "")
	if err == nil {
		t.Fatal("expected error when exceeding memory limit")
	}
}
