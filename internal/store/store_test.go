package store

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zeebo/xxh3"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "mocode-store-test-"+t.Name())
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestProjectDirName(t *testing.T) {
	n1 := projectDirName("/home/user/projects/my-app")
	n2 := projectDirName("/home/user/other/my-app") // same base, different path
	n3 := projectDirName("C:\\Users\\user\\my-app")
	n4 := projectDirName("C:\\Users\\user\\my-app") // same path

	if n1 == n2 {
		t.Error("different paths should produce different names")
	}
	if n3 != n4 {
		t.Error("same path should produce same name")
	}
	// Verify format: name_hash
	if len(n1) < 5 {
		t.Errorf("name too short: %s", n1)
	}
}

func TestSessionDirName(t *testing.T) {
	sess := session.Session{
		ID:        "abc12345-0000-0000-0000-000000000000",
		CreatedAt: 1700000000,
	}
	name := sessionDirName(sess)
	// New format uses xxh3 hash, not ID prefix
	hash := fmt.Sprintf("%x", xxh3.HashString(sess.ID))
	expected := fmt.Sprintf("1700000000_%s", hash)
	if name != expected {
		t.Errorf("expected %s, got %s", expected, name)
	}

	// Verify uniqueness: two sessions with different IDs should have different dirs,
	// even when they share a prefix.
	sess1 := session.Session{ID: "msg-123$$call-456-uuid-1", CreatedAt: 1700000000}
	sess2 := session.Session{ID: "msg-123$$call-456-uuid-2", CreatedAt: 1700000000}
	if sessionDirName(sess1) == sessionDirName(sess2) {
		t.Error("sessions with same prefix should have different directory names")
	}
}

func TestJSONLWriter_AppendAndRead(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "test.jsonl")
	w := NewJSONLWriter(path)

	type entry struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}

	// Append
	if err := w.Append(entry{ID: "1", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(entry{ID: "2", Text: "world"}); err != nil {
		t.Fatal(err)
	}

	// Read
	results, err := w.ReadAll(func() any { return &entry{} })
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(results))
	}
	e1 := results[0].(*entry)
	if e1.ID != "1" || e1.Text != "hello" {
		t.Errorf("wrong first entry: %+v", e1)
	}
}

func TestJSONLWriter_WriteAll(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "test.jsonl")
	w := NewJSONLWriter(path)

	type entry struct {
		ID string `json:"id"`
	}

	// Write all
	if err := w.WriteAll([]any{entry{ID: "a"}, entry{ID: "b"}, entry{ID: "c"}}); err != nil {
		t.Fatal(err)
	}

	// Read back
	results, err := w.ReadAll(func() any { return &entry{} })
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
}

func TestJSONLWriter_ConcurrentAppend(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "concurrent.jsonl")
	w := NewJSONLWriter(path)

	type entry struct {
		Seq int `json:"seq"`
	}

	const goroutines = 10
	const writes = 100
	var wg sync.WaitGroup

	for g := range goroutines {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < writes; i++ {
				seq := base*writes + i
				if err := w.Append(entry{Seq: seq}); err != nil {
					t.Errorf("append seq=%d failed: %v", seq, err)
				}
			}
		}(g)
	}
	wg.Wait()

	results, err := w.ReadAll(func() any { return &entry{} })
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != goroutines*writes {
		t.Fatalf("expected %d entries, got %d", goroutines*writes, len(results))
	}

	// Verify no duplicates and all present
	seen := make(map[int]bool)
	for _, r := range results {
		e := r.(*entry)
		if seen[e.Seq] {
			t.Errorf("duplicate seq=%d (data corruption)", e.Seq)
		}
		seen[e.Seq] = true
	}
	for i := 0; i < goroutines*writes; i++ {
		if !seen[i] {
			t.Errorf("missing seq=%d", i)
		}
	}
}

func TestJSONLWriter_LargeMessage(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "large.jsonl")
	w := NewJSONLWriter(path)

	// Create payload > 4KB (POSIX atomic write threshold)
	largeText := make([]byte, 10000)
	for i := range largeText {
		largeText[i] = 'A' + byte(i%26)
	}

	type entry struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := w.Append(entry{ID: "large", Text: string(largeText)}); err != nil {
		t.Fatal(err)
	}

	results, err := w.ReadAll(func() any { return &entry{} })
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	e := results[0].(*entry)
	if len(e.Text) != 10000 {
		t.Errorf("expected 10000 chars, got %d", len(e.Text))
	}
}

func TestSessionStore_CRUD(t *testing.T) {
	dir := tempDir(t)
	s := &Store{DataDir: dir, ProjectDir: filepath.Join(dir, "projects", "test_00000000")}
	s.sessions = newSessionStore(s)

	// Create
	sess, err := s.sessions.Create(t.Context(), "My Test Session")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" {
		t.Error("missing session ID")
	}

	// Get
	got, err := s.sessions.Get(t.Context(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "My Test Session" {
		t.Errorf("expected 'My Test Session', got '%s'", got.Title)
	}

	// List
	list, err := s.sessions.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list))
	}

	// Save
	sess.Title = "Updated Title"
	if err := s.sessions.Save(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	got, _ = s.sessions.Get(t.Context(), sess.ID)
	if got.Title != "Updated Title" {
		t.Errorf("expected 'Updated Title', got '%s'", got.Title)
	}

	// Delete
	if err := s.sessions.Delete(t.Context(), sess.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.sessions.Get(t.Context(), sess.ID)
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMessageStore_AppendAndList(t *testing.T) {
	dir := tempDir(t)
	s := &Store{
		DataDir:    dir,
		ProjectDir: filepath.Join(dir, "projects", "test_00000000"),
	}
	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	os.MkdirAll(filepath.Join(s.ProjectDir, "sessions", "sid1"), 0o700)

	msg1, err := s.messages.Create(t.Context(), "sid1", message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg1.Role != message.User {
		t.Errorf("expected user, got %s", msg1.Role)
	}

	_, err = s.messages.Create(t.Context(), "sid1", message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "world"},
			message.ToolCall{ID: "tc1", Name: "test", Input: `{}`},
		},
		Model: "gpt-4",
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.messages.List(t.Context(), "sid1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(list))
	}
	if len(list[1].ToolCalls()) != 1 {
		t.Error("expected 1 tool call in assistant message")
	}
	if list[1].Model != "gpt-4" {
		t.Errorf("expected gpt-4 model, got %s", list[1].Model)
	}
}

func TestWriteAndReadJSON(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "meta.json")

	type meta struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	if err := WriteJSON(path, meta{Name: "test", Count: 42}); err != nil {
		t.Fatal(err)
	}

	var got meta
	if err := ReadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "test" || got.Count != 42 {
		t.Errorf("wrong data: %+v", got)
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"my-project", "my-project"},
		{"My Project!", "My-Project-"},
		{"中文项目", "----"},
		{"hello_world.v2", "hello_world.v2"},
		{"", "project"},
	}
	for _, tt := range tests {
		got := sanitize(tt.in)
		if got != tt.want {
			t.Errorf("sanitize(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSessionStore_IncrementCost(t *testing.T) {
	dir := tempDir(t)
	s := &Store{DataDir: dir, ProjectDir: filepath.Join(dir, "projects", "test_00000000")}
	s.sessions = newSessionStore(s)

	// Create a session
	sess, err := s.sessions.Create(t.Context(), "Test")
	if err != nil {
		t.Fatal(err)
	}

	// Increment cost multiple times
	for i := 0; i < 5; i++ {
		if err := s.sessions.IncrementCost(t.Context(), sess.ID, 1.5); err != nil {
			t.Fatal(err)
		}
	}

	// Verify final cost
	got, err := s.sessions.Get(t.Context(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	expected := 7.5 // 5 * 1.5
	if got.Cost != expected {
		t.Errorf("expected cost %.2f, got %.2f", expected, got.Cost)
	}
}

func TestSessionStore_IncrementCost_Concurrent(t *testing.T) {
	dir := tempDir(t)
	s := &Store{DataDir: dir, ProjectDir: filepath.Join(dir, "projects", "test_00000000")}
	s.sessions = newSessionStore(s)

	// Create a session
	sess, err := s.sessions.Create(t.Context(), "Concurrent Test")
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	const increments = 100
	const delta = 0.01
	var wg sync.WaitGroup

	// Simulate concurrent sub-agents incrementing parent cost
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < increments; i++ {
				if err := s.sessions.IncrementCost(t.Context(), sess.ID, delta); err != nil {
					t.Errorf("increment failed: %v", err)
				}
			}
		}()
	}
	wg.Wait()

	// Verify no lost updates
	got, err := s.sessions.Get(t.Context(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	expected := float64(goroutines) * float64(increments) * delta
	// Use small epsilon for float comparison
	if math.Abs(got.Cost-expected) > 0.0001 {
		t.Errorf("cost mismatch: expected %.4f, got %.4f (lost %.4f due to race)",
			expected, got.Cost, expected-got.Cost)
	}
}

func TestSessionStore_ConcurrentCreateSameToolID(t *testing.T) {
	dir := tempDir(t)
	s := &Store{DataDir: dir, ProjectDir: filepath.Join(dir, "projects", "test_00000000")}
	s.sessions = newSessionStore(s)

	// Create parent session
	parent, err := s.sessions.Create(t.Context(), "Parent")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate multiple sub-agents trying to create sessions with the same toolCallID
	// (which can happen with retries or concurrent batch execution).
	const goroutines = 20
	const toolCallID = "msg-123$$call-456"
	ids := make([]string, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Simulate the new behavior: use UUID suffix to ensure uniqueness
			now := time.Now().Unix()
			sess := session.Session{
				ID:              fmt.Sprintf("%s-%s", toolCallID, uuid.New().String()),
				ParentSessionID: parent.ID,
				Title:           fmt.Sprintf("Task %d", idx),
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if err := s.sessions.Save(t.Context(), sess); err != nil {
				errs[idx] = err
				return
			}
			ids[idx] = sess.ID
		}(i)
	}
	wg.Wait()

	// Verify all IDs are unique (UUID suffix prevents collisions)
	seen := make(map[string]bool)
	for i, id := range ids {
		if errs[i] != nil {
			t.Errorf("goroutine %d failed: %v", i, errs[i])
			continue
		}
		if seen[id] {
			t.Errorf("duplicate session ID: %s", id)
		}
		seen[id] = true
	}
	if len(seen) != goroutines {
		t.Errorf("expected %d unique IDs, got %d", goroutines, len(seen))
	}
}
