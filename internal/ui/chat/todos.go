package chat

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/package-register/mocode/internal/agent/tools"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// Todos Tool
// -----------------------------------------------------------------------------

// TodosToolMessageItem is a message item that represents a todos tool call.
type TodosToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*TodosToolMessageItem)(nil)

type OpenTodoDialogMsg struct {
	Selected int
}

// NewTodosToolMessageItem creates a new [TodosToolMessageItem].
func NewTodosToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return &TodosToolMessageItem{
		baseToolMessageItem: newBaseToolMessageItem(sty, toolCall, result, &TodosToolRenderContext{}, canceled),
	}
}

func (t *TodosToolMessageItem) HandleMouseDoubleClick(btn ansi.MouseButton, x, y int) tea.Cmd {
	if btn != ansi.MouseLeft {
		return nil
	}
	index := todoDialogSelectionIndex(t.toolCall, t.result)
	return func() tea.Msg {
		return OpenTodoDialogMsg{Selected: index}
	}
}

// TodosToolRenderContext renders todos tool messages.
type TodosToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (t *TodosToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)

	var params tools.TodosParams
	var meta tools.TodosResponseMetadata
	var headerText string
	var body string

	// Parse params for pending state (before result is available).
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err == nil {
		completedCount := 0
		inProgressTask := ""
		for _, todo := range params.Todos {
			if todo.Status == "completed" {
				completedCount++
			}
			if todo.Status == "in_progress" {
				if todo.ActiveForm != "" {
					inProgressTask = todo.ActiveForm
				} else {
					inProgressTask = todo.Content
				}
			}
		}

		// Default display from params (used when pending or no metadata).
		ratio := sty.Tool.TodoRatio.Render(fmt.Sprintf("%d/%d", completedCount, len(params.Todos)))
		headerText = ratio
		if inProgressTask != "" {
			headerText = fmt.Sprintf("%s · %s", ratio, inProgressTask)
		}

		if opts.IsPending() {
			return pendingTool(sty, "To-Do", opts.Anim, opts.Compact, cappedWidth, ansi.Strip(headerText))
		}

		// If we have metadata, use it for richer display.
		if opts.HasResult() && opts.Result.Metadata != "" {
			if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err == nil {
				if meta.IsNew {
					if meta.JustStarted != "" {
						headerText = fmt.Sprintf("created %d todos, starting first", meta.Total)
					} else {
						headerText = fmt.Sprintf("created %d todos", meta.Total)
					}
					body = FormatTodosList(sty, meta.Todos, styles.ArrowRightIcon, cappedWidth)
				} else {
					// Build header based on what changed.
					hasCompleted := len(meta.JustCompleted) > 0
					hasStarted := meta.JustStarted != ""
					allCompleted := meta.Completed == meta.Total

					ratio := sty.Tool.TodoRatio.Render(fmt.Sprintf("%d/%d", meta.Completed, meta.Total))
					if hasCompleted && hasStarted {
						text := sty.Tool.TodoStatusNote.Render(fmt.Sprintf(" · completed %d, starting next", len(meta.JustCompleted)))
						headerText = fmt.Sprintf("%s%s", ratio, text)
					} else if hasCompleted {
						text := sty.Tool.TodoStatusNote.Render(fmt.Sprintf(" · completed %d", len(meta.JustCompleted)))
						if allCompleted {
							text = sty.Tool.TodoStatusNote.Render(" · completed all")
						}
						headerText = fmt.Sprintf("%s%s", ratio, text)
					} else if hasStarted {
						headerText = fmt.Sprintf("%s%s", ratio, sty.Tool.TodoStatusNote.Render(" · starting task"))
					} else {
						headerText = ratio
					}

					// Build body with details.
					if allCompleted {
						// Show all todos when all are completed, like when created.
						body = FormatTodosList(sty, meta.Todos, styles.ArrowRightIcon, cappedWidth)
					} else if meta.JustStarted != "" {
						body = sty.Tool.TodoInProgressIcon.Render(styles.ArrowRightIcon+" ") +
							sty.Tool.TodoJustStarted.Render(meta.JustStarted)
					}
				}
			}
		}
	}

	toolParams := []string{headerText}
	header := toolHeader(sty, opts.Status, "To-Do", cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if body == "" {
		return header
	}

	return joinToolParts(header, sty.Tool.Body.Render(body))
}

// FormatTodosList formats a list of todos for display.
func FormatTodosList(sty *styles.Styles, todos []session.Todo, inProgressIcon string, width int) string {
	if len(todos) == 0 {
		return ""
	}

	sorted := make([]session.Todo, len(todos))
	copy(sorted, todos)
	sortTodos(sorted)

	var lines []string
	for _, todo := range sorted {
		var prefix string
		textStyle := sty.Tool.TodoItem

		switch todo.Status {
		case session.TodoStatusCompleted:
			prefix = sty.Tool.TodoCompletedIcon.Render(styles.TodoCompletedIcon) + " "
		case session.TodoStatusInProgress:
			prefix = sty.Tool.TodoInProgressIcon.Render(inProgressIcon + " ")
		default:
			prefix = sty.Tool.TodoPendingIcon.Render(styles.TodoPendingIcon) + " "
		}

		text := todo.Content
		if todo.Status == session.TodoStatusInProgress && todo.ActiveForm != "" {
			text = todo.ActiveForm
		}
		line := prefix + textStyle.Render(text)
		line = ansi.Truncate(line, width, "…")

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// sortTodos sorts todos by status: completed, in_progress, pending.
func sortTodos(todos []session.Todo) {
	slices.SortStableFunc(todos, func(a, b session.Todo) int {
		return statusOrder(a.Status) - statusOrder(b.Status)
	})
}

// statusOrder returns the sort order for a todo status.
func statusOrder(s session.TodoStatus) int {
	switch s {
	case session.TodoStatusCompleted:
		return 0
	case session.TodoStatusInProgress:
		return 1
	default:
		return 2
	}
}

func todoDialogSelectionIndex(toolCall message.ToolCall, result *message.ToolResult) int {
	todos := todosForDialogSelection(toolCall, result)
	if len(todos) == 0 {
		return -1
	}
	sorted := make([]session.Todo, len(todos))
	copy(sorted, todos)
	sortTodos(sorted)
	selected := sorted[0]
	for _, todo := range sorted {
		if todo.Status == session.TodoStatusInProgress {
			selected = todo
			break
		}
	}
	for idx, todo := range todos {
		if todo.Content == selected.Content && todo.Status == selected.Status && todo.ActiveForm == selected.ActiveForm {
			return idx
		}
	}
	return -1
}

func todosForDialogSelection(toolCall message.ToolCall, result *message.ToolResult) []session.Todo {
	if result != nil && result.Metadata != "" {
		var meta tools.TodosResponseMetadata
		if err := json.Unmarshal([]byte(result.Metadata), &meta); err == nil && len(meta.Todos) > 0 {
			return meta.Todos
		}
	}
	var params tools.TodosParams
	if err := json.Unmarshal([]byte(toolCall.Input), &params); err != nil {
		return nil
	}
	todos := make([]session.Todo, 0, len(params.Todos))
	for _, todo := range params.Todos {
		todos = append(todos, session.Todo{
			Content:    todo.Content,
			Status:     session.TodoStatus(todo.Status),
			ActiveForm: todo.ActiveForm,
		})
	}
	return todos
}
