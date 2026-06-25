package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/package-register/mocode/internal/domain/session/message"
)

func TestMessageStore_DeleteAfter(t *testing.T) {
	dir := tempDir(t)
	s := &Store{
		DataDir:    dir,
		ProjectDir: filepath.Join(dir, "projects", "test_00000000"),
	}
	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	os.MkdirAll(filepath.Join(s.ProjectDir, "sessions", "sid1"), 0o700)

	// Create 5 messages
	for i := 0; i < 5; i++ {
		_, err := s.messages.Create(t.Context(), "sid1", message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "msg"}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Delete after index 2 (keep 0,1,2)
	if err := s.messages.DeleteAfter(t.Context(), "sid1", 2); err != nil {
		t.Fatal(err)
	}

	list, err := s.messages.List(t.Context(), "sid1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(list))
	}
}

func TestMessageStore_DeleteAfter_ZeroIndex(t *testing.T) {
	dir := tempDir(t)
	s := &Store{
		DataDir:    dir,
		ProjectDir: filepath.Join(dir, "projects", "test_00000000"),
	}
	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	os.MkdirAll(filepath.Join(s.ProjectDir, "sessions", "sid1"), 0o700)

	for i := 0; i < 3; i++ {
		_, err := s.messages.Create(t.Context(), "sid1", message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "msg"}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Delete after index 0 (keep only first)
	if err := s.messages.DeleteAfter(t.Context(), "sid1", 0); err != nil {
		t.Fatal(err)
	}

	list, err := s.messages.List(t.Context(), "sid1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 message, got %d", len(list))
	}
}

func TestMessageStore_DeleteAfter_InvalidIndex(t *testing.T) {
	dir := tempDir(t)
	s := &Store{
		DataDir:    dir,
		ProjectDir: filepath.Join(dir, "projects", "test_00000000"),
	}
	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	os.MkdirAll(filepath.Join(s.ProjectDir, "sessions", "sid1"), 0o700)

	err := s.messages.DeleteAfter(t.Context(), "sid1", -1)
	if err == nil {
		t.Fatal("expected error for negative index")
	}
}

func TestMessageStore_DeleteAfterMessage(t *testing.T) {
	dir := tempDir(t)
	s := &Store{
		DataDir:    dir,
		ProjectDir: filepath.Join(dir, "projects", "test_00000000"),
	}
	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	os.MkdirAll(filepath.Join(s.ProjectDir, "sessions", "sid1"), 0o700)

	// Create messages and track IDs
	var ids []string
	for i := 0; i < 5; i++ {
		msg, err := s.messages.Create(t.Context(), "sid1", message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "msg"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, msg.ID)
	}

	// Delete after message[2] (keep 0,1,2)
	if err := s.messages.DeleteAfterMessage(t.Context(), "sid1", ids[2]); err != nil {
		t.Fatal(err)
	}

	list, err := s.messages.List(t.Context(), "sid1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(list))
	}
	// Verify the kept messages are correct
	for i, msg := range list {
		if msg.ID != ids[i] {
			t.Errorf("message %d: expected ID %s, got %s", i, ids[i], msg.ID)
		}
	}
}

func TestMessageStore_DeleteAfterMessage_NotFound(t *testing.T) {
	dir := tempDir(t)
	s := &Store{
		DataDir:    dir,
		ProjectDir: filepath.Join(dir, "projects", "test_00000000"),
	}
	s.sessions = newSessionStore(s)
	s.messages = newMessageStore(s)
	os.MkdirAll(filepath.Join(s.ProjectDir, "sessions", "sid1"), 0o700)

	err := s.messages.DeleteAfterMessage(t.Context(), "sid1", "non-existent-id")
	if err == nil {
		t.Fatal("expected error for non-existent message")
	}
}
