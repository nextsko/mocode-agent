package agenttools

import (
	"context"
	_ "embed"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/domain/session"
)

//go:embed todos.md
var todosDescription []byte

const TodosToolName = "todos"

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

// NewTodosTool is the legacy fantasy-typed constructor; new code should use
// NewTodosPortTool and wrap it at the agent boundary.
func NewTodosTool(sessions session.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TodosToolName,
		toolutil.FirstLineDescription(todosDescription),
		func(ctx context.Context, params TodosParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			text, meta, err := runTodos(ctx, sessions, params)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(text), meta), nil
		})
}
