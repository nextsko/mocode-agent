// Package orchestration provides a lightweight workflow runtime for multi-agent
// orchestration. It tracks task state, handoff history, and checkpoints
// within a root session, enabling observable and recoverable swarm execution.
package orchestration

import (
	"encoding/json"
	"time"
)

// ── Status types ────────────────────────────────────────────────────────────

// TaskStatus is the execution state of a single task.
type TaskStatus string

const (
	TaskPlanned   TaskStatus = "planned"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
	TaskBlocked   TaskStatus = "blocked"
	TaskCancelled TaskStatus = "cancelled"
)

// WorkflowStatus is the overall state of a workflow.
type WorkflowStatus string

const (
	WorkflowActive    WorkflowStatus = "active"
	WorkflowCompleted WorkflowStatus = "completed"
	WorkflowFailed    WorkflowStatus = "failed"
	WorkflowPaused    WorkflowStatus = "paused"
)

// ── Core types ──────────────────────────────────────────────────────────────

// WorkflowRuntime is the top-level runtime container for a swarm execution.
type WorkflowRuntime struct {
	// WorkflowID uniquely identifies this workflow execution.
	WorkflowID string `json:"workflow_id"`
	// RootSessionID is the parent session that initiated the swarm.
	RootSessionID string `json:"root_session_id"`
	// ActiveTaskID is the currently executing task (empty if idle).
	ActiveTaskID string `json:"active_task_id,omitempty"`
	// ActiveAgentID is the currently executing agent mode.
	ActiveAgentID string `json:"active_agent_id,omitempty"`
	// Status is the overall workflow state.
	Status WorkflowStatus `json:"status"`
	// Tasks maps task ID to runtime state.
	Tasks map[string]*TaskRuntime `json:"tasks"`
	// Handoffs is the audit trail of agent transfers.
	Handoffs []HandoffRecord `json:"handoffs,omitempty"`
	// Checkpoints records snapshots taken during execution.
	Checkpoints []Checkpoint `json:"checkpoints,omitempty"`
	// PendingAttention tracks items needing user review.
	PendingAttention []AttentionItem `json:"pending_attention,omitempty"`

	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TaskRuntime tracks the execution state of a single delegated task.
type TaskRuntime struct {
	// TaskID is the unique identifier (matches plan task ID).
	TaskID string `json:"task_id"`
	// ParentTaskID is the upstream task that spawned this one.
	ParentTaskID string `json:"parent_task_id,omitempty"`
	// SessionID is the sub-agent session for this task.
	SessionID string `json:"session_id,omitempty"`
	// AgentID is the agent mode used (e.g. "coder", "task").
	AgentID string `json:"agent_id"`
	// Status is the current execution state.
	Status TaskStatus `json:"status"`
	// DependsOn lists task IDs that must complete first.
	DependsOn []string `json:"depends_on,omitempty"`
	// RetryCount is the number of times this task has been retried.
	RetryCount int `json:"retry_count"`
	// OutputSummary is a one-line summary of the task result.
	OutputSummary string `json:"output_summary,omitempty"`
	// EvidenceRefs collects references to evidence (file:line).
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	// LastError holds the most recent error message.
	LastError string `json:"last_error,omitempty"`

	PlannedAt  time.Time `json:"planned_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// HandoffRecord captures a single transfer_to_agent event.
type HandoffRecord struct {
	// FromAgent is the agent that initiated the transfer.
	FromAgent string `json:"from_agent"`
	// ToAgent is the target agent.
	ToAgent string `json:"to_agent"`
	// TaskID is the task context during handoff.
	TaskID string `json:"task_id,omitempty"`
	// Reason is the handoff justification if provided.
	Reason string `json:"reason,omitempty"`
	// Depth is the transfer chain depth.
	Depth int `json:"depth"`

	Timestamp time.Time `json:"timestamp"`
}

// Checkpoint is a snapshot marker for workflow recovery.
type Checkpoint struct {
	// ID is a unique checkpoint identifier.
	ID string `json:"id"`
	// WorkflowID is the owning workflow.
	WorkflowID string `json:"workflow_id"`
	// Label describes the checkpoint (e.g. "pre-batch-spawn").
	Label string `json:"label,omitempty"`
	// TaskHead is the task that was active at checkpoint time.
	TaskHead string `json:"task_head,omitempty"`
	// MessageNodeID is the session message node at checkpoint time.
	MessageNodeID string `json:"message_node_id,omitempty"`
	// Snapshot is optional serialized state for recovery.
	Snapshot json.RawMessage `json:"snapshot,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// AttentionItem flags something needing user review.
type AttentionItem struct {
	// Type describes the category (e.g. "review_fail", "handoff_approval").
	Type string `json:"type"`
	// TaskID is the related task.
	TaskID string `json:"task_id,omitempty"`
	// Message is a human-readable description.
	Message string `json:"message"`

	CreatedAt time.Time `json:"created_at"`
}

// ── New / helpers ───────────────────────────────────────────────────────────

// NewWorkflow creates a new WorkflowRuntime for the given session.
func NewWorkflow(workflowID, rootSessionID, activeAgentID string) *WorkflowRuntime {
	now := time.Now()
	return &WorkflowRuntime{
		WorkflowID:    workflowID,
		RootSessionID: rootSessionID,
		ActiveAgentID: activeAgentID,
		Status:        WorkflowActive,
		Tasks:         make(map[string]*TaskRuntime),
		StartedAt:     now,
		UpdatedAt:     now,
	}
}

// AddTask registers a new task in the workflow.
func (w *WorkflowRuntime) AddTask(taskID, agentID string, dependsOn []string) *TaskRuntime {
	t := &TaskRuntime{
		TaskID:    taskID,
		AgentID:   agentID,
		Status:    TaskPlanned,
		DependsOn: dependsOn,
		PlannedAt: time.Now(),
	}
	w.Tasks[taskID] = t
	w.UpdatedAt = time.Now()
	return t
}

// RecordHandoff appends a handoff record to the audit trail.
func (w *WorkflowRuntime) RecordHandoff(from, to, taskID, reason string) {
	w.Handoffs = append(w.Handoffs, HandoffRecord{
		FromAgent: from,
		ToAgent:   to,
		TaskID:    taskID,
		Reason:    reason,
		Depth:     len(w.Handoffs) + 1,
		Timestamp: time.Now(),
	})
	w.UpdatedAt = time.Now()
}

// RecordCheckpoint creates and appends a new checkpoint.
func (w *WorkflowRuntime) RecordCheckpoint(label, taskHead, msgNodeID string) *Checkpoint {
	cp := &Checkpoint{
		ID:            checkpointID(len(w.Checkpoints) + 1),
		WorkflowID:    w.WorkflowID,
		Label:         label,
		TaskHead:      taskHead,
		MessageNodeID: msgNodeID,
		CreatedAt:     time.Now(),
	}
	w.Checkpoints = append(w.Checkpoints, *cp)
	w.UpdatedAt = time.Now()
	return cp
}

// AddAttention adds an item requiring user review.
func (w *WorkflowRuntime) AddAttention(typ, taskID, message string) {
	w.PendingAttention = append(w.PendingAttention, AttentionItem{
		Type:      typ,
		TaskID:    taskID,
		Message:   message,
		CreatedAt: time.Now(),
	})
	w.UpdatedAt = time.Now()
}

// SetActive updates the active task/agent.
func (w *WorkflowRuntime) SetActive(taskID, agentID string) {
	w.ActiveTaskID = taskID
	w.ActiveAgentID = agentID
	w.UpdatedAt = time.Now()
}

func checkpointID(n int) string {
	return "ckpt-" + time.Now().Format("20060102-150405") + "-" + itoa(n)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
