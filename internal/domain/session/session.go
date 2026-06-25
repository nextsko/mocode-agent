package session

import (
	"context"
	"fmt"

	"github.com/zeebo/xxh3"

	"github.com/package-register/mocode/internal/util/pubsub"
)

type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
)

// HashID returns the XXH3 hash of a session ID (UUID) as a hex string.
func HashID(id string) string {
	h := xxh3.New()
	h.WriteString(id)
	return fmt.Sprintf("%x", h.Sum(nil))
}

type Todo struct {
	Content    string     `json:"content"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"active_form"`
}

// HasIncompleteTodos returns true if there are any non-completed todos.
func HasIncompleteTodos(todos []Todo) bool {
	for _, todo := range todos {
		if todo.Status != TodoStatusCompleted {
			return true
		}
	}
	return false
}

type Session struct {
	ID                  string
	ParentSessionID     string
	RevertSessionID     string
	ActiveSnapshotID    string
	Title               string
	MessageCount        int64
	PromptTokens        int64
	CompletionTokens    int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	SummaryMessageID    string
	Cost                float64
	Todos               []Todo
	CreatedAt           int64
	UpdatedAt           int64
	// AgentToolCallID tracks which tool call created this sub-session (for traceability).
	// Not persisted as part of the session ID, just metadata.
	AgentToolCallID string `json:"-"`
	AgentMessageID  string `json:"-"`
}

type Service interface {
	pubsub.Subscriber[Session]
	Create(ctx context.Context, title string) (Session, error)
	CreateTitleSession(ctx context.Context, parentSessionID string) (Session, error)
	CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	GetLast(ctx context.Context) (Session, error)
	List(ctx context.Context) ([]Session, error)
	Save(ctx context.Context, session Session) (Session, error)
	UpdateTitleAndUsage(ctx context.Context, sessionID, title string, promptTokens, completionTokens, cacheReadTokens, cacheCreationTokens int64, cost float64) error
	Rename(ctx context.Context, id string, title string) error
	Delete(ctx context.Context, id string) error
	// IncrementCost atomically adds delta to the session's cost.
	// This is the safe way to accumulate costs from multiple concurrent sub-agents.
	IncrementCost(ctx context.Context, id string, delta float64) error

	// Agent tool session management
	CreateAgentToolSessionID(messageID, toolCallID string) string
	ParseAgentToolSessionID(sessionID string) (messageID string, toolCallID string, ok bool)
	IsAgentToolSession(sessionID string) bool
}
