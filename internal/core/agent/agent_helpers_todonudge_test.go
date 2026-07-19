package agent

import (
	"testing"

	"github.com/nextsko/mocode-agent/internal/domain/session"
)

func TestBuildTodoNudge_EmptyWhenNoTodos(t *testing.T) {
	if got := buildTodoNudge(nil); got != "" {
		t.Fatalf("expected empty nudge for no todos, got %q", got)
	}
}

func TestBuildTodoNudge_EmptyWhenAllCompleted(t *testing.T) {
	todos := []session.Todo{
		{Content: "done thing", Status: session.TodoStatusCompleted},
		{Content: "also done", Status: session.TodoStatusCompleted},
	}
	if got := buildTodoNudge(todos); got != "" {
		t.Fatalf("expected empty nudge when all completed, got %q", got)
	}
}

func TestBuildTodoNudge_ListsPendingAndInProgress(t *testing.T) {
	todos := []session.Todo{
		{Content: "finished", Status: session.TodoStatusCompleted},
		{Content: "still pending", Status: session.TodoStatusPending, ActiveForm: "Working on pending"},
		{Content: "in progress one", Status: session.TodoStatusInProgress},
	}
	got := buildTodoNudge(todos)
	if got == "" {
		t.Fatal("expected non-empty nudge with open todos")
	}
	// Completed items must NOT appear in the nudge.
	if contains(got, "finished") {
		t.Fatalf("completed item should not appear in nudge: %q", got)
	}
	// Both open items should appear.
	if !contains(got, "still pending") && !contains(got, "Working on pending") {
		t.Fatalf("pending item missing from nudge: %q", got)
	}
	if !contains(got, "in progress one") {
		t.Fatalf("in-progress item missing from nudge: %q", got)
	}
	// Status markers should distinguish them.
	if !contains(got, "[pending]") {
		t.Fatalf("expected [pending] marker: %q", got)
	}
	if !contains(got, "[in_progress]") {
		t.Fatalf("expected [in_progress] marker: %q", got)
	}
}

func TestBuildTodoNudge_PrefersActiveForm(t *testing.T) {
	todos := []session.Todo{
		{Content: "raw content", Status: session.TodoStatusPending, ActiveForm: "active form text"},
	}
	got := buildTodoNudge(todos)
	if !contains(got, "active form text") {
		t.Fatalf("nudge should prefer active_form over content: %q", got)
	}
	if contains(got, "raw content") {
		t.Fatalf("nudge should not include raw content when active_form is set: %q", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (indexOf(s, substr) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
