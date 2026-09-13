package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

func newAgentToolItem(t *testing.T, finished bool, nested int) *AgentToolMessageItem {
	t.Helper()
	sty := styles.ThemeForProvider("")
	tc := message.ToolCall{
		ID:       "tc1",
		Name:     "agent",
		Input:    `{"prompt":"research the repo structure and report back"}`,
		Finished: finished,
	}
	var result *message.ToolResult
	if finished {
		result = &message.ToolResult{
			ToolCallID: "tc1",
			Name:       "agent",
			Content:    "# Report\n\nAll done.\n" + strings.Repeat("findings line\n", 80),
		}
	}
	item := NewAgentToolMessageItem(&sty, tc, result, false)
	for i := range nested {
		ntc := message.ToolCall{
			ID:       fmt.Sprintf("n%d", i),
			Name:     "bash",
			Input:    `{"command":"ls -la"}`,
			Finished: true,
		}
		nres := &message.ToolResult{ToolCallID: ntc.ID, Name: "bash", Content: "total 0"}
		item.AddNestedTool(NewBashToolMessageItem(&sty, ntc, nres, false))
	}
	return item
}

func countLines(s string) int { return len(strings.Split(strings.TrimRight(s, "\n"), "\n")) }

// EffectiveStatus must reflect completion derived from the tool result, while
// the raw Status stays Running because it is never updated when the result
// arrives. The sub-agent summary box relies on EffectiveStatus to tell when
// sub-agents have finished.
func TestEffectiveStatusDerivesFromResult(t *testing.T) {
	t.Parallel()

	done := newAgentToolItem(t, true, 0)
	if got := done.EffectiveStatus(); got != ToolStatusSuccess {
		t.Fatalf("finished item EffectiveStatus = %v, want ToolStatusSuccess", got)
	}
	if got := done.Status(); got != ToolStatusRunning {
		t.Fatalf("raw Status = %v, want ToolStatusRunning (this is why EffectiveStatus exists)", got)
	}

	running := newAgentToolItem(t, false, 0)
	if got := running.EffectiveStatus(); got != ToolStatusRunning {
		t.Fatalf("running item EffectiveStatus = %v, want ToolStatusRunning", got)
	}
}

// A1: while running, rendering stays a fixed handful of lines no matter how
// many nested tool calls the sub-agent already made (prime-agent summary
// line instead of a growing tree).
func TestAgentToolRunningSummaryStaysCompact(t *testing.T) {
	item := newAgentToolItem(t, false, 50)
	out := item.Render(100)
	requireLess(t, countLines(out), 10, "running view must not grow with nested tools: got %d lines", countLines(out))
	if strings.Contains(out, "ls -la") {
		t.Error("running summary view must not render nested tool details")
	}
	if !strings.Contains(out, "50 tool calls") {
		t.Error("summary line must report the nested tool count")
	}
}

// A2: the completed default view shows the result body (already
// height-capped) but never the nested tree.
func TestAgentToolCompletedHidesNestedTreeByDefault(t *testing.T) {
	item := newAgentToolItem(t, true, 30)
	out := item.Render(100)
	if strings.Contains(out, "ls -la") {
		t.Error("completed default view must not render the nested tree")
	}
	if !strings.Contains(out, "30 tool calls") {
		t.Error("summary line must survive completion")
	}
	if !strings.Contains(out, "Report") {
		t.Error("result body must still render")
	}
}

// A3: the expanded audit view keeps the full nested tree.
func TestAgentToolExpandedShowsNestedTree(t *testing.T) {
	item := newAgentToolItem(t, true, 3)
	item.ToggleExpanded()
	out := item.Render(100)
	if !strings.Contains(out, "ls -la") {
		t.Error("expanded view must render nested tools")
	}
}

func requireLess(t *testing.T, got, max int, msg string, args ...any) {
	t.Helper()
	if got >= max {
		t.Errorf(msg, args...)
	}
}
