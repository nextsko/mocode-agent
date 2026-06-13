// Package web tests for the WebSocket bridge helpers. The bridge sends
// JSON-RPC envelopes with literal string keys, so we assert on literal
// strings instead of pulling them into named constants: that way a failure
// points at the wire contract, not a test-local constant.
//
//nolint:goconst,nestif
package web

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/package-register/mocode/internal/agent/notify"
	agentexec "github.com/package-register/mocode/internal/agent/tools/builtin/exec"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/tools/shell"
)

// recorder captures every payload routed through wsSender.Send so tests can
// assert on the produced JSON-RPC envelopes without standing up a real
// websocket.Conn.
type recorder struct {
	mu      sync.Mutex
	payload []map[string]any
}

func (r *recorder) Send(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payload = append(r.payload, m)
}

func (r *recorder) last() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.payload) == 0 {
		return nil
	}
	return r.payload[len(r.payload)-1]
}

func (r *recorder) all() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, len(r.payload))
	copy(out, r.payload)
	return out
}

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

func TestBridgeSubagentEventWrapsInnerPayload(t *testing.T) {
	rec := &recorder{}
	srv := &Server{}

	srv.bridgeSubagentEvent(rec, "tool-1", "agent-7", "coder", "ToolCall", map[string]any{"name": "bash"})

	if got := rec.last(); got == nil {
		t.Fatal("expected a payload to be sent")
	} else {
		if got["method"] != "event" {
			t.Fatalf("expected method=event, got %#v", got["method"])
		}
		params := got["params"].(map[string]any)
		if params["type"] != "SubagentEvent" {
			t.Fatalf("expected SubagentEvent, got %#v", params["type"])
		}
		payload := params["payload"].(map[string]any)
		if payload["parent_tool_call_id"] != "tool-1" {
			t.Fatalf("expected parent_tool_call_id=tool-1, got %#v", payload["parent_tool_call_id"])
		}
		if payload["agent_id"] != "agent-7" {
			t.Fatalf("expected agent_id=agent-7, got %#v", payload["agent_id"])
		}
		if payload["subagent_type"] != "coder" {
			t.Fatalf("expected subagent_type=coder, got %#v", payload["subagent_type"])
		}
		inner := payload["event"].(map[string]any)
		if inner["type"] != "ToolCall" {
			t.Fatalf("expected inner type ToolCall, got %#v", inner["type"])
		}
		innerPayload := inner["payload"].(map[string]any)
		if innerPayload["name"] != "bash" {
			t.Fatalf("expected inner payload name=bash, got %#v", innerPayload["name"])
		}
	}
}

func TestBridgeSubagentCompletedEventEmitsStatusAndUsage(t *testing.T) {
	rec := &recorder{}
	srv := &Server{}

	srv.bridgeSubagentCompletedEvent(rec, notify.SubagentCompletedEvent{
		ParentSessionID:  "parent-1",
		ParentToolCallID: "tool-1",
		AgentID:          "agent-42",
		SubagentType:     "explore",
		Status:           notify.SubagentStatusSuccess,
		DurationMs:       1234,
		Usage: notify.SubagentTokenUsage{
			Input:         100,
			Output:        50,
			CacheRead:     25,
			CacheCreation: 10,
			Total:         185,
		},
		Summary: "explored 3 modules",
	})

	got := rec.last()
	if got == nil {
		t.Fatal("expected a payload to be sent")
	}
	params := got["params"].(map[string]any)
	if params["type"] != "SubagentEvent" {
		t.Fatalf("expected SubagentEvent, got %#v", params["type"])
	}
	payload := params["payload"].(map[string]any)
	if payload["parent_tool_call_id"] != "tool-1" {
		t.Fatalf("expected parent_tool_call_id=tool-1, got %#v", payload["parent_tool_call_id"])
	}
	inner := payload["event"].(map[string]any)
	if inner["type"] != "SubagentCompleted" {
		t.Fatalf("expected SubagentCompleted inner type, got %#v", inner["type"])
	}
	innerPayload := inner["payload"].(map[string]any)
	if innerPayload["status"] != "success" {
		t.Fatalf("expected status=success, got %#v", innerPayload["status"])
	}
	if innerPayload["duration_ms"] != int64(1234) {
		t.Fatalf("expected duration_ms=1234, got %#v", innerPayload["duration_ms"])
	}
	usage := innerPayload["usage"].(map[string]any)
	if usage["total"] != int64(185) {
		t.Fatalf("expected total=185, got %#v", usage["total"])
	}
	if innerPayload["summary"] != "explored 3 modules" {
		t.Fatalf("expected summary to round-trip, got %#v", innerPayload["summary"])
	}
	if _, hasErr := innerPayload["error"]; hasErr {
		t.Fatalf("did not expect error key on success event, got %#v", innerPayload["error"])
	}
}

func TestBridgeSubagentCompletedEventIncludesErrorOnFailure(t *testing.T) {
	rec := &recorder{}
	srv := &Server{}

	srv.bridgeSubagentCompletedEvent(rec, notify.SubagentCompletedEvent{
		ParentToolCallID: "tool-1",
		Status:           notify.SubagentStatusError,
		DurationMs:       500,
		Usage:            notify.SubagentTokenUsage{Input: 1, Output: 0, Total: 1},
		Error:            "boom",
	})

	got := rec.last()
	if got == nil {
		t.Fatal("expected a payload to be sent")
	}
	inner := got["params"].(map[string]any)["payload"].(map[string]any)["event"].(map[string]any)
	innerPayload := inner["payload"].(map[string]any)
	if innerPayload["status"] != "error" {
		t.Fatalf("expected status=error, got %#v", innerPayload["status"])
	}
	if innerPayload["error"] != "boom" {
		t.Fatalf("expected error=boom, got %#v", innerPayload["error"])
	}
	if _, hasSummary := innerPayload["summary"]; hasSummary {
		t.Fatalf("did not expect summary key on error event, got %#v", innerPayload["summary"])
	}
}

func TestBridgeSubagentCompletedEventBlockedStatus(t *testing.T) {
	rec := &recorder{}
	srv := &Server{}

	srv.bridgeSubagentCompletedEvent(rec, notify.SubagentCompletedEvent{
		ParentToolCallID: "tool-1",
		Status:           notify.SubagentStatusBlocked,
		DurationMs:       0,
		Usage:            notify.SubagentTokenUsage{},
	})

	got := rec.last()
	if got == nil {
		t.Fatal("expected a payload to be sent")
	}
	innerPayload := got["params"].(map[string]any)["payload"].(map[string]any)["event"].(map[string]any)["payload"].(map[string]any)
	if innerPayload["status"] != "blocked" {
		t.Fatalf("expected status=blocked, got %#v", innerPayload["status"])
	}
}

func TestRecorderAccumulatesAllPayloads(t *testing.T) {
	rec := &recorder{}
	srv := &Server{}

	srv.bridgeSubagentEvent(rec, "tool-1", "", "", "ContentPart", map[string]any{"type": "text"})
	srv.bridgeSubagentEvent(rec, "tool-1", "", "", "ToolCall", map[string]any{"name": "view"})
	srv.bridgeSubagentEvent(rec, "tool-1", "", "", "SubagentCompleted", map[string]any{"status": "success"})

	all := rec.all()
	if len(all) != 3 {
		t.Fatalf("expected 3 payloads, got %d", len(all))
	}
}
