package toolutil

import (
	"context"
	"strings"
	"testing"
)

// recordingSink is a test SessionLoggerSink that captures calls.
type recordingSink struct {
	toolCalls []string
	thinks    []string
	bugs      []string
	infos     []string
}

func (r *recordingSink) LogToolCall(event, data string, _ SessionLogMeta) {
	r.toolCalls = append(r.toolCalls, event+": "+data)
}
func (r *recordingSink) LogThink(event, data string, _ SessionLogMeta) {
	r.thinks = append(r.thinks, event+": "+data)
}
func (r *recordingSink) LogBug(event, data string, _ SessionLogMeta) {
	r.bugs = append(r.bugs, event+": "+data)
}
func (r *recordingSink) LogInfo(event, data string, _ SessionLogMeta) {
	r.infos = append(r.infos, event+": "+data)
}

var _ SessionLoggerSink = (*recordingSink)(nil)

func TestGetSessionLoggerAbsent(t *testing.T) {
	// No logger in ctx returns nil; callers nil-check.
	if lg := GetSessionLogger(context.Background()); lg != nil {
		t.Fatalf("expected nil logger, got %T", lg)
	}
}

func TestGetSessionLoggerRoundTrip(t *testing.T) {
	sink := &recordingSink{}
	ctx := context.WithValue(context.Background(), SessionLoggerContextKeyVal, sink)
	got := GetSessionLogger(ctx)
	if got == nil {
		t.Fatal("expected non-nil logger from ctx")
	}
	// Exercise all four methods to confirm the sink is usable through the
	// retrieved interface.
	got.LogToolCall("tool_call", "bash ran", SessionLogMeta{ToolName: "bash"})
	got.LogThink("reasoning", "thinking...", SessionLogMeta{})
	got.LogBug("tool_error", "boom", SessionLogMeta{ToolName: "bash", ErrorType: "tool_execution"})
	got.LogInfo("step_usage", "in=10 out=5", SessionLogMeta{})

	if len(sink.toolCalls) != 1 || !strings.Contains(sink.toolCalls[0], "bash ran") {
		t.Fatalf("toolCalls wrong: %v", sink.toolCalls)
	}
	if len(sink.thinks) != 1 {
		t.Fatalf("thinks wrong: %v", sink.thinks)
	}
	if len(sink.bugs) != 1 || !strings.Contains(sink.bugs[0], "boom") {
		t.Fatalf("bugs wrong: %v", sink.bugs)
	}
	if len(sink.infos) != 1 {
		t.Fatalf("infos wrong: %v", sink.infos)
	}
}

func TestGetSessionLoggerWrongType(t *testing.T) {
	// A value of the wrong type under the key yields nil, not a panic.
	ctx := context.WithValue(context.Background(), SessionLoggerContextKeyVal, "not-a-sink")
	if lg := GetSessionLogger(ctx); lg != nil {
		t.Fatalf("expected nil for wrong-type value, got %T", lg)
	}
}
