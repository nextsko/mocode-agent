package wechat

import (
	"context"
	"testing"
	"time"

	wechatbot "github.com/package-register/mocode/internal/integration/wechat/sdk"
)

// ─── Slash Command Tests ─────────────────────────────────────────────────────

func TestSlashCommandParsing(t *testing.T) {
	tests := []struct {
		input   string
		wantCmd string
		wantOK  bool
	}{
		{"/help", "/help", true},
		{"/status", "/status", true},
		{"/list", "/list", true},
		{"/models", "/models", true},
		{"/model minimax/minimax-m2.7", "/model", true},
		{"/test model minimax/MiniMax-M2.7", "/test", true},
		{"/screenshot", "/screenshot", true},
		{"/send C:/file.txt", "/send", true},
		{"hello world", "", false},
		{"/unknown", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			found := false
			for _, cmd := range slashRegistry {
				if cmd.name == tt.wantCmd {
					found = true
					break
				}
			}
			if tt.wantOK && !found {
				t.Errorf("expected command %q to be registered", tt.wantCmd)
			}
		})
	}
}

func TestHelpCommand(t *testing.T) {
	result := cmdHelp(nil, nil, nil, "")
	if result == "" || !contains(result, "/help") || !contains(result, "/status") {
		t.Error("help output incomplete")
	}
}

func TestShortSessionID(t *testing.T) {
	if got := shortSessionID("abcdefghijklmnop"); got != "abcdefgh..." {
		t.Errorf("got %q", got)
	}
}

// ─── Session Manager Tests ───────────────────────────────────────────────────

func TestSessionManagerCreateAndList(t *testing.T) {
	mgr := NewSessionManager(nil)
	sess := mgr.Create("user1", "test", "/tmp", "mock-id")
	if sess == nil || sess.MocodeID != "mock-id" {
		t.Fatal("create failed")
	}
	if len(mgr.List()) != 1 {
		t.Error("expected 1 session")
	}
}

func TestSessionManagerDelete(t *testing.T) {
	mgr := NewSessionManager(nil)
	sess := mgr.Create("u1", "t", "/tmp", "m1")
	if !mgr.Delete(sess.ID) || mgr.Delete(sess.ID) {
		t.Error("delete logic wrong")
	}
}

func TestSessionManagerSubmitTask(t *testing.T) {
	mgr := NewSessionManager(nil)
	sess := mgr.Create("u1", "t", "/tmp", "m1")
	task, err := mgr.SubmitTask(sess.ID, "test")
	if err != nil || task == nil {
		t.Fatal("submit failed")
	}
	time.Sleep(100 * time.Millisecond)
}

// ─── Router Tests ────────────────────────────────────────────────────────────

func TestRouterShortMessageToActiveSession(t *testing.T) {
	mgr := NewSessionManager(nil)
	sess := mgr.Create("user1", "mocode", "/tmp/mocode", "m1")
	router := NewButlerRouter(mgr)
	result := router.Route("user1", "帮我看看编译错误", sess.ID)
	if result.Action != "send_to_session" || result.SessionID != sess.ID {
		t.Errorf("got action=%s session=%s", result.Action, result.SessionID)
	}
}

func TestRouterKeywordRouting(t *testing.T) {
	mgr := NewSessionManager(nil)
	sess := mgr.Create("user1", "neuron", "/tmp/neuron", "m1")
	router := NewButlerRouter(mgr)
	result := router.Route("user1", "neuron 那个项目怎么样了", "")
	if result.Action != "send_to_session" || result.SessionID != sess.ID {
		t.Errorf("got action=%s", result.Action)
	}
}

func TestRouterStatusRequest(t *testing.T) {
	mgr := NewSessionManager(nil)
	_ = mgr.Create("user1", "test", "/tmp", "m1")
	router := NewButlerRouter(mgr)
	result := router.Route("user1", "所有会话状态怎么样", "")
	if result.Action != "aggregate_status" {
		t.Errorf("got %s", result.Action)
	}
}

// ─── Media Store Tests ───────────────────────────────────────────────────────

func TestMediaStoreBasic(t *testing.T) {
	store := NewMediaStore("", 0)
	defer store.Stop()
	id := store.Store("/tmp/test.jpg", "image/jpeg", "test", "inbound")
	if id == "" {
		t.Error("mediaID should not be empty")
	}
	path, ok := store.Resolve(id)
	if !ok || path != "/tmp/test.jpg" {
		t.Error("resolve failed")
	}
	if store.Count() != 1 {
		t.Error("count wrong")
	}
}

func TestMediaStoreReleaseAll(t *testing.T) {
	store := NewMediaStore("", 0)
	defer store.Stop()
	store.Store("/tmp/a.jpg", "image/jpeg", "test", "inbound")
	store.Store("/tmp/b.jpg", "image/jpeg", "test", "outbound")
	store.ReleaseAll("inbound", false)
	if store.Count() != 1 {
		t.Error("release failed")
	}
}

// ─── Dedup Tests ─────────────────────────────────────────────────────────────

func TestDedupHashing(t *testing.T) {
	ch := &Channel{}
	ch.recentMsgs = make(map[string]time.Time)
	msg1 := &wechatbot.IncomingMessage{UserID: "u1", Text: "hello"}
	msg2 := &wechatbot.IncomingMessage{UserID: "u1", Text: "hello"}
	if ch.isDuplicate(msg1) {
		t.Error("first should not be duplicate")
	}
	if !ch.isDuplicate(msg2) {
		t.Error("second should be duplicate")
	}
}

// ─── Task Queue Tests ────────────────────────────────────────────────────────

func TestTaskQueueSubmitAndComplete(t *testing.T) {
	done := make(chan *Task, 1)
	queue := NewTaskQueue(
		func(_ context.Context, sid, prompt string) (string, error) {
			return "result: " + prompt, nil
		},
		func(task *Task) { done <- task },
	)
	task := queue.Submit("sess1", "user1", "test")
	if task == nil {
		t.Fatal("task nil")
	}
	completed := <-done
	if completed.Status != TaskCompleted || completed.Result == "" {
		t.Error("task not completed properly")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
