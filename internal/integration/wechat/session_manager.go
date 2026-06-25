// session_manager.go manages multiple parallel WeChat sessions with real workspace integration.
package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// WeChatSession represents a managed session with its own workdir and agent.
type WeChatSession struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Name       string    `json:"name"`
	WorkDir    string    `json:"work_dir"`
	MocodeID   string    `json:"mocode_id"` // the real mocode session ID
	Status     string    `json:"status"`    // idle, running, error
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
	ErrorMsg   string    `json:"error_msg,omitempty"`
}

// SessionManager manages multiple parallel sessions for the butler system.
type SessionManager struct {
	mu        sync.RWMutex
	sessions  map[string]*WeChatSession // keyed by userID
	butler    *ButlerContext
	taskQueue *TaskQueue
}

// NewSessionManager creates a real session manager with workspace integration.
func NewSessionManager(butler *ButlerContext) *SessionManager {
	m := &SessionManager{
		sessions: make(map[string]*WeChatSession),
		butler:   butler,
	}
	m.taskQueue = NewTaskQueue(m.executeTask, m.onTaskComplete)
	return m
}

// Create creates a new session with the given workdir and real mocode sessionID.
func (m *SessionManager) Create(userID, name, workDir, mocodeSessionID string) *WeChatSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess := &WeChatSession{
		ID:         userID,
		UserID:     userID,
		Name:       name,
		WorkDir:    workDir,
		MocodeID:   mocodeSessionID,
		Status:     "idle",
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}
	m.sessions[userID] = sess
	slog.Info("Session created", "userID", userID, "mocodeID", mocodeSessionID, "workdir", workDir)
	return sess
}

// Get returns a session by userID.
func (m *SessionManager) Get(userID string) (*WeChatSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[userID]
	return s, ok
}

// GetByMocodeID returns a session by mocode session ID.
func (m *SessionManager) GetByMocodeID(mocodeID string) (*WeChatSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.MocodeID == mocodeID {
			return s, true
		}
	}
	return nil, false
}

// List returns all sessions.
func (m *SessionManager) List() []*WeChatSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*WeChatSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// Delete removes a session by userID.
func (m *SessionManager) Delete(userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[userID]; ok {
		delete(m.sessions, userID)
		return true
	}
	return false
}

// SubmitTask sends a task to a session for async execution.
func (m *SessionManager) SubmitTask(userID, prompt string) (*Task, error) {
	m.mu.RLock()
	sess, ok := m.sessions[userID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session for user %s not found", userID)
	}

	m.mu.Lock()
	sess.Status = "running"
	sess.LastActive = time.Now()
	m.mu.Unlock()

	task := m.taskQueue.Submit(sess.MocodeID, userID, prompt)
	return task, nil
}

// ActiveTasks returns the number of currently running tasks.
func (m *SessionManager) ActiveTasks() int {
	return m.taskQueue.ActiveCount()
}

// executeTask is the real TaskExecutor that calls workspace.AgentRun.
func (m *SessionManager) executeTask(ctx context.Context, mocodeSessionID, prompt string) (string, error) {
	if m.butler == nil || m.butler.Workspace == nil {
		return "", fmt.Errorf("workspace not available")
	}

	sess, ok := m.GetByMocodeID(mocodeSessionID)
	userID := mocodeSessionID
	if ok {
		userID = sess.UserID
	}

	slog.Info("Executing task", "session", mocodeSessionID, "user", userID, "promptLen", len(prompt))

	// Call real workspace.AgentRun - synchronous, blocks until agent finishes.
	if err := m.butler.Workspace.AgentRun(ctx, mocodeSessionID, prompt); err != nil {
		return "", fmt.Errorf("agent run: %w", err)
	}

	// After agent finishes, get the last assistant message.
	msgs, err := m.butler.Workspace.ListMessages(ctx, mocodeSessionID)
	if err != nil {
		return "任务已完成", nil
	}

	// Find the last assistant reply.
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Content, nil
		}
	}

	return "任务已完成", nil
}

// onTaskComplete is the TaskNotifier called when a task finishes.
func (m *SessionManager) onTaskComplete(task *Task) {
	m.mu.Lock()
	for _, sess := range m.sessions {
		if sess.MocodeID == task.SessionID {
			sess.LastActive = time.Now()
			if task.Status == TaskFailed {
				sess.Status = "error"
				sess.ErrorMsg = task.Error
			} else {
				sess.Status = "idle"
				sess.ErrorMsg = ""
			}
			break
		}
	}
	m.mu.Unlock()

	// Send WeChat notification about task completion.
	if m.butler != nil && m.butler.Channel != nil {
		userID := task.UserID
		notifyMsg := formatTaskComplete(task)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := m.butler.Channel.SendText(ctx, userID, notifyMsg); err != nil {
			slog.Warn("Failed to send task completion notification", "taskID", task.ID, "error", err)
		}
	}

	slog.Info("Task completed", "taskID", task.ID, "status", task.Status)
}

func formatTaskComplete(task *Task) string {
	status := "✅ 完成"
	if task.Status == TaskFailed {
		status = "❌ 失败"
	}
	msg := fmt.Sprintf("%s 任务 %s:\n", status, shortSessionID(task.ID))
	if task.Result != "" {
		// Truncate long results for WeChat.
		result := task.Result
		if len(result) > 500 {
			result = result[:500] + "..."
		}
		msg += result
	}
	if task.Error != "" {
		msg += fmt.Sprintf("\n错误: %s", task.Error)
	}
	return msg
}
