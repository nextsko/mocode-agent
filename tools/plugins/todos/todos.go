package todos

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/tools"
)

const TodosToolName = "todos"

//go:embed todos.md
var todosDescription []byte

type TodosParams struct {
	Todos []TodoItem `json:"todos" description:"The updated todo list"`
}

type TodoItem struct {
	Content    string `json:"content" description:"What needs to be done (imperative form)"`
	Status     string `json:"status" description:"Task status: pending, in_progress, or completed"`
	ActiveForm string `json:"active_form" description:"Present continuous form (e.g., 'Running tests')"`
}

type TodosResponseMetadata struct {
	IsNew         bool           `json:"is_new"`
	Todos         []session.Todo `json:"todos"`
	JustCompleted []string       `json:"just_completed,omitempty"`
	JustStarted   string         `json:"just_started,omitempty"`
	Completed     int            `json:"completed"`
	Total         int            `json:"total"`
}

type todosTool struct{}

func New() tools.Tool { return &todosTool{} }

func (t *todosTool) Name() string { return TodosToolName }

func (t *todosTool) Description() string { return firstLine(todosDescription) }

func (t *todosTool) Schema() tools.Schema {
	return tools.Schema{
		Name:        TodosToolName,
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "The updated todo list",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{"type": "string"},
							"status": map[string]any{
								"type": "string",
								"enum": []string{"pending", "in_progress", "completed"},
							},
							"active_form": map[string]any{"type": "string"},
						},
						"required": []string{"content", "status", "active_form"},
					},
				},
			},
			"required": []string{"todos"},
		},
	}
}

func (t *todosTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
	var params TodosParams
	if err := json.Unmarshal(args, &params); err != nil {
		return tools.ErrorResult(err)
	}
	if tctx == nil || tctx.SessionID() == "" {
		return tools.ToolResult{}, errors.New("session ID is required for managing todos")
	}

	sessions, ok := tools.RuntimeDependency[session.Service](tctx, tools.RuntimeDependencySessions)
	if !ok || sessions == nil {
		return tools.ToolResult{Error: errors.New("session service is required for managing todos")}, nil
	}

	currentSession, err := sessions.Get(ctx, tctx.SessionID())
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to get session: %w", err)
	}

	isNew := len(currentSession.Todos) == 0
	oldStatusByContent := make(map[string]session.TodoStatus)
	for _, todo := range currentSession.Todos {
		oldStatusByContent[todo.Content] = todo.Status
	}

	for _, item := range params.Todos {
		switch item.Status {
		case string(session.TodoStatusPending), string(session.TodoStatusInProgress), string(session.TodoStatusCompleted):
		default:
			return tools.ToolResult{Error: fmt.Errorf("invalid status %q for todo %q", item.Status, item.Content)}, nil
		}
	}

	todos := make([]session.Todo, len(params.Todos))
	var justCompleted []string
	var justStarted string
	completedCount := 0

	for i, item := range params.Todos {
		newStatus := session.TodoStatus(item.Status)
		todos[i] = session.Todo{
			Content:    item.Content,
			Status:     newStatus,
			ActiveForm: item.ActiveForm,
		}

		oldStatus, existed := oldStatusByContent[item.Content]
		if newStatus == session.TodoStatusCompleted {
			completedCount++
			if existed && oldStatus != session.TodoStatusCompleted {
				justCompleted = append(justCompleted, item.Content)
			}
		}

		if newStatus == session.TodoStatusInProgress && (!existed || oldStatus != session.TodoStatusInProgress) {
			justStarted = item.Content
			if item.ActiveForm != "" {
				justStarted = item.ActiveForm
			}
		}
	}

	currentSession.Todos = todos
	_, err = sessions.Save(ctx, currentSession)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to save todos: %w", err)
	}

	pendingCount := 0
	inProgressCount := 0
	for _, todo := range todos {
		switch todo.Status {
		case session.TodoStatusPending:
			pendingCount++
		case session.TodoStatusInProgress:
			inProgressCount++
		}
	}

	response := fmt.Sprintf(
		"Todo list updated successfully.\n\nStatus: %d pending, %d in progress, %d completed\nTodos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable.",
		pendingCount,
		inProgressCount,
		completedCount,
	)
	metadata := TodosResponseMetadata{
		IsNew:         isNew,
		Todos:         todos,
		JustCompleted: justCompleted,
		JustStarted:   justStarted,
		Completed:     completedCount,
		Total:         len(todos),
	}

	return tools.ToolResult{Content: response, Metadata: map[string]any{
		"is_new":         metadata.IsNew,
		"todos":          metadata.Todos,
		"just_completed": metadata.JustCompleted,
		"just_started":   metadata.JustStarted,
		"completed":      metadata.Completed,
		"total":          metadata.Total,
	}}, nil
}

func firstLine(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func init() { tools.Register(TodosToolName, New()) }
