package orchestration

import (
	"encoding/json"
	"testing"
)

func TestNewWorkflow_DefaultsActive(t *testing.T) {
	w := NewWorkflow("wf-1", "sess-1", "coder")
	if w.Status != WorkflowActive {
		t.Errorf("expected active status, got %s", w.Status)
	}
	if w.WorkflowID != "wf-1" || w.RootSessionID != "sess-1" || w.ActiveAgentID != "coder" {
		t.Error("workflow fields not set correctly")
	}
	if len(w.Tasks) != 0 {
		t.Error("new workflow should have no tasks")
	}
}

func TestAddTask(t *testing.T) {
	w := NewWorkflow("wf-1", "sess-1", "coder")
	t1 := w.AddTask("p1-research", "task", nil)
	if t1.Status != TaskPlanned {
		t.Errorf("expected planned, got %s", t1.Status)
	}
	if w.Tasks["p1-research"] != t1 {
		t.Error("AddTask should store task by ID")
	}
}

func TestRecordHandoff_DepthIncrements(t *testing.T) {
	w := NewWorkflow("wf-1", "sess-1", "skplan")
	w.RecordHandoff("skplan", "coder", "p2-impl", "need implementation")
	w.RecordHandoff("coder", "reviewer", "p2-impl", "need review")
	if len(w.Handoffs) != 2 {
		t.Fatalf("expected 2 handoffs, got %d", len(w.Handoffs))
	}
	if w.Handoffs[0].Depth != 1 {
		t.Errorf("first handoff depth should be 1, got %d", w.Handoffs[0].Depth)
	}
	if w.Handoffs[1].Depth != 2 {
		t.Errorf("second handoff depth should be 2, got %d", w.Handoffs[1].Depth)
	}
}

func TestRecordCheckpoint_NonEmptyID(t *testing.T) {
	w := NewWorkflow("wf-1", "sess-1", "coder")
	cp := w.RecordCheckpoint("pre-transfer", "p2-impl", "msg-123")
	if cp.ID == "" {
		t.Error("checkpoint ID should not be empty")
	}
	if len(w.Checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(w.Checkpoints))
	}
}

func TestCheckpointIDFormat(t *testing.T) {
	id := checkpointID(3)
	if id == "" {
		t.Error("checkpoint ID should not be empty")
	}
	if !json.Valid([]byte(`"` + id + `"`)) {
		t.Errorf("checkpoint ID should be valid JSON string: %s", id)
	}
}

func TestAddAttention(t *testing.T) {
	w := NewWorkflow("wf-1", "sess-1", "coder")
	w.AddAttention("review_fail", "p3-review", "3 critical findings remain")
	if len(w.PendingAttention) != 1 {
		t.Fatalf("expected 1 attention item, got %d", len(w.PendingAttention))
	}
	if w.PendingAttention[0].Type != "review_fail" {
		t.Errorf("wrong type: %s", w.PendingAttention[0].Type)
	}
}

func TestSetActive(t *testing.T) {
	w := NewWorkflow("wf-1", "sess-1", "skplan")
	w.SetActive("p2-impl", "coder")
	if w.ActiveTaskID != "p2-impl" || w.ActiveAgentID != "coder" {
		t.Error("SetActive did not update fields correctly")
	}
}
