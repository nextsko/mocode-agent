// task_queue.go provides an async task queue for the WeChat butler system.
// Tasks are dispatched to sessions and executed in background goroutines.
package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TaskStatus represents the execution state of a task.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

// Task represents a unit of work dispatched to a session.
type Task struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	UserID    string     `json:"user_id"`
	Prompt    string     `json:"prompt"`
	Status    TaskStatus `json:"status"`
	Result    string     `json:"result,omitempty"`
	Error     string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// TaskExecutor is the function signature for executing a task in a session.
type TaskExecutor func(ctx context.Context, sessionID, prompt string) (string, error)

// TaskNotifier is called when a task completes or fails.
type TaskNotifier func(task *Task)

// TaskQueue manages background task execution across multiple sessions.
type TaskQueue struct {
	mu        sync.Mutex
	tasks     map[string]*Task
	executor  TaskExecutor
	notifier  TaskNotifier
	cancelMap map[string]context.CancelFunc
	wg        sync.WaitGroup
}

// NewTaskQueue creates a new task queue.
func NewTaskQueue(executor TaskExecutor, notifier TaskNotifier) *TaskQueue {
	return &TaskQueue{
		tasks:     make(map[string]*Task),
		executor:  executor,
		notifier:  notifier,
		cancelMap: make(map[string]context.CancelFunc),
	}
}

// Submit adds a task to the queue and starts execution immediately.
func (q *TaskQueue) Submit(sessionID, userID, prompt string) *Task {
	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
	task := &Task{
		ID:        taskID,
		SessionID: sessionID,
		UserID:    userID,
		Prompt:    prompt,
		Status:    TaskPending,
		CreatedAt: time.Now(),
	}

	q.mu.Lock()
	q.tasks[taskID] = task
	q.mu.Unlock()

	// Execute in background.
	q.wg.Add(1)
	go q.execute(task)

	return task
}

// Cancel stops a running task.
func (q *TaskQueue) Cancel(taskID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if cancel, ok := q.cancelMap[taskID]; ok {
		cancel()
		if task, ok := q.tasks[taskID]; ok {
			now := time.Now()
			task.Status = TaskCancelled
			task.EndedAt = &now
		}
		return true
	}
	return false
}

// Get returns a task by ID.
func (q *TaskQueue) Get(taskID string) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.tasks[taskID]
}

// List returns all tasks, optionally filtered by status.
func (q *TaskQueue) List(status TaskStatus) []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var result []*Task
	for _, t := range q.tasks {
		if status == "" || t.Status == status {
			result = append(result, t)
		}
	}
	return result
}

// ActiveCount returns the number of running tasks.
func (q *TaskQueue) ActiveCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	count := 0
	for _, t := range q.tasks {
		if t.Status == TaskRunning {
			count++
		}
	}
	return count
}

// WaitForAll blocks until all running tasks complete.
func (q *TaskQueue) WaitForAll() {
	q.wg.Wait()
}

func (q *TaskQueue) execute(task *Task) {
	defer q.wg.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	q.mu.Lock()
	q.cancelMap[task.ID] = cancel
	task.Status = TaskRunning
	now := time.Now()
	task.StartedAt = &now
	q.mu.Unlock()

	defer func() {
		q.mu.Lock()
		delete(q.cancelMap, task.ID)
		q.mu.Unlock()
	}()

	result, err := q.executor(ctx, task.SessionID, task.Prompt)

	q.mu.Lock()
	endNow := time.Now()
	task.EndedAt = &endNow
	if err != nil {
		task.Status = TaskFailed
		task.Error = err.Error()
		slog.Error("Butler task failed", "taskID", task.ID, "error", err)
	} else {
		task.Status = TaskCompleted
		task.Result = result
		slog.Debug("Butler task completed", "taskID", task.ID)
	}
	q.mu.Unlock()

	// Notify on completion.
	if q.notifier != nil {
		q.notifier(task)
	}
}
