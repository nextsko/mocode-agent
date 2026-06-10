package web

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentexec "github.com/package-register/mocode/internal/agent/tools/builtin/exec"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/tools/shell"
)

func TestStatusTokenUsageSplitsCachedTokens(t *testing.T) {
	got := statusTokenUsage(session.Session{
		PromptTokens:        135,
		CompletionTokens:    50,
		CacheReadTokens:     25,
		CacheCreationTokens: 10,
	})

	if got["input_other"] != int64(100) {
		t.Fatalf("expected regular input tokens 100, got %#v", got["input_other"])
	}
	if got["input_cache_read"] != int64(25) {
		t.Fatalf("expected cache read tokens 25, got %#v", got["input_cache_read"])
	}
	if got["input_cache_creation"] != int64(10) {
		t.Fatalf("expected cache creation tokens 10, got %#v", got["input_cache_creation"])
	}
	if got["output"] != int64(50) {
		t.Fatalf("expected output tokens 50, got %#v", got["output"])
	}
}

func TestStatusTokenUsageClampsNegativeRegularInput(t *testing.T) {
	got := statusTokenUsage(session.Session{
		PromptTokens:        10,
		CacheReadTokens:     25,
		CacheCreationTokens: 10,
	})

	if got["input_other"] != int64(0) {
		t.Fatalf("expected regular input tokens to clamp at 0, got %#v", got["input_other"])
	}
}

func TestWSBridgeStateAssistantDeltaEventsOnlyEmitNewContent(t *testing.T) {
	state := newWSBridgeState(nil, "")

	initial := message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "hello"},
			message.TextContent{Text: "world"},
			message.ToolCall{ID: "tool-1", Name: "bash", Input: `{"cmd":"echo"`},
		},
	}

	got := state.AssistantDeltaEvents(initial)
	if len(got) != 3 {
		t.Fatalf("expected 3 initial delta events, got %d", len(got))
	}

	updated := message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "hello there"},
			message.TextContent{Text: "world!"},
			message.ToolCall{ID: "tool-1", Name: "bash", Input: `{"cmd":"echo","tty":true}`},
		},
	}

	got = state.AssistantDeltaEvents(updated)
	if len(got) != 3 {
		t.Fatalf("expected 3 delta events after append, got %d", len(got))
	}

	thinkPayload := got[0]["params"].(map[string]any)["payload"].(map[string]any)
	if thinkPayload["think"] != " there" {
		t.Fatalf("expected thinking delta %q, got %#v", " there", thinkPayload["think"])
	}

	textPayload := got[1]["params"].(map[string]any)["payload"].(map[string]any)
	if textPayload["text"] != "!" {
		t.Fatalf("expected text delta %q, got %#v", "!", textPayload["text"])
	}

	toolPayload := got[2]["params"].(map[string]any)["payload"].(map[string]any)
	if toolPayload["arguments_part"] != `,"tty":true}` {
		t.Fatalf("expected tool args delta, got %#v", toolPayload["arguments_part"])
	}

	if len(state.AssistantDeltaEvents(updated)) != 0 {
		t.Fatal("expected no duplicate delta events when content is unchanged")
	}
}

func TestWSBridgeStateDeduplicatesToolResults(t *testing.T) {
	state := newWSBridgeState(nil, "")

	if !state.MarkToolResultSent("tool-1") {
		t.Fatal("expected first tool result emission to be allowed")
	}
	if state.MarkToolResultSent("tool-1") {
		t.Fatal("expected duplicate tool result emission to be suppressed")
	}
}

func TestBuildToolReturnValueIncludesBashExtras(t *testing.T) {
	metaBytes, err := json.Marshal(agentexec.BashResponseMetadata{
		Background:       true,
		ShellID:          "001",
		TTY:              true,
		Description:      "Long task",
		WorkingDirectory: "/tmp/work",
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	got := buildToolReturnValue(message.ToolResult{
		ToolCallID: "tool-1",
		Content:    "Background shell started",
		Metadata:   string(metaBytes),
	})

	extras, ok := got["extras"].(map[string]any)
	if !ok {
		t.Fatalf("expected extras map, got %#v", got["extras"])
	}
	if extras["job_status"] != "running" {
		t.Fatalf("expected running job status, got %#v", extras["job_status"])
	}
	if extras["shell_id"] != "001" {
		t.Fatalf("expected shell_id 001, got %#v", extras["shell_id"])
	}
	if extras["tty"] != true {
		t.Fatalf("expected tty=true, got %#v", extras["tty"])
	}
}

func TestBuildBackgroundToolResultPayloadTracksRunningState(t *testing.T) {
	bgShell, err := shell.GetBackgroundShellManager().Start(
		context.Background(),
		t.TempDir(),
		nil,
		"printf 'hello\\n'; sleep 1",
		"test",
	)
	if err != nil {
		t.Fatalf("start background shell: %v", err)
	}
	t.Cleanup(func() {
		_ = shell.GetBackgroundShellManager().Kill(bgShell.ID)
	})
	time.Sleep(100 * time.Millisecond)

	payload, _, finished := buildBackgroundToolResultPayload("tool-1", agentexec.BashResponseMetadata{
		Background: true,
		ShellID:    bgShell.ID,
		TTY:        true,
	}, bgShell)
	if finished {
		t.Fatal("expected running background job")
	}

	returnValue := payload["return_value"].(map[string]any)
	if returnValue["is_error"] != false {
		t.Fatalf("expected non-error running payload, got %#v", returnValue["is_error"])
	}
	extras := returnValue["extras"].(map[string]any)
	if extras["job_status"] != "running" {
		t.Fatalf("expected running extras, got %#v", extras["job_status"])
	}
	if returnValue["output"] != "hello\n" {
		t.Fatalf("expected streamed stdout, got %#v", returnValue["output"])
	}
}

func TestBackgroundShellStatusClassifiesInterrupts(t *testing.T) {
	if got := backgroundShellStatus(true, true, context.Canceled); got != "interrupted" {
		t.Fatalf("expected interrupted status, got %q", got)
	}
}
