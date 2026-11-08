package chat

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
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

	headerText, body, pending := todosHeaderAndBody(sty, opts, cappedWidth)
	if pending {
		return pendingTool(sty, "To-Do", opts.Anim, opts.Compact, cappedWidth, ansi.Strip(headerText))
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

// todosHeaderAndBody derives the header line and optional body for a To-Do
// tool call from its JSON input and, when present, its result metadata. The
// pending flag reports that the call has no result yet, in which case only the
// header is meaningful.
func todosHeaderAndBody(sty *styles.Styles, opts *ToolRenderOpts, cappedWidth int) (headerText, body string, pending bool) {
	var params tools.TodosParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return "", "", false
	}
	headerText = todosParamsHeader(sty, params)
	if opts.IsPending() {
		return headerText, "", true
	}
	var meta tools.TodosResponseMetadata
	if opts.HasResult() && opts.Result.Metadata != "" && json.Unmarshal([]byte(opts.Result.Metadata), &meta) == nil {
		headerText, body = todosMetadataView(sty, &meta, cappedWidth)
	}
	return headerText, body, false
}

// todosParamsHeader renders the "<done>/<total> · <task>" header from the
// tool-call parameters alone.
func todosParamsHeader(sty *styles.Styles, params tools.TodosParams) string {
	completedCount := 0
	inProgressTask := ""
	for _, todo := range params.Todos {
		switch todo.Status {
		case "completed":
			completedCount++
		case "in_progress":
			inProgressTask = todo.ActiveForm
			if inProgressTask == "" {
				inProgressTask = todo.Content
			}
		}
	}

	ratio := sty.Tool.TodoRatio.Render(fmt.Sprintf("%d/%d", completedCount, len(params.Todos)))
	if inProgressTask == "" {
		return ratio
	}
	return fmt.Sprintf("%s · %s", ratio, inProgressTask)
}

// todosMetadataView renders the richer header and body derived from a result
// metadata payload.
func todosMetadataView(sty *styles.Styles, meta *tools.TodosResponseMetadata, cappedWidth int) (headerText, body string) {
	if !meta.IsNew {
		return todosUpdatedView(sty, meta, cappedWidth)
	}

	if meta.JustStarted != "" {
		headerText = fmt.Sprintf("created %d todos, starting first", meta.Total)
	} else {
		headerText = fmt.Sprintf("created %d todos", meta.Total)
	}
	body = FormatTodosList(sty, meta.Todos, styles.ArrowRightIcon, cappedWidth)
	return headerText, body
}

// todosUpdatedView renders the header and body for an update to an existing
// To-Do list.
func todosUpdatedView(sty *styles.Styles, meta *tools.TodosResponseMetadata, cappedWidth int) (headerText, body string) {
	hasCompleted := len(meta.JustCompleted) > 0
	hasStarted := meta.JustStarted != ""
	allCompleted := meta.Completed == meta.Total

	ratio := sty.Tool.TodoRatio.Render(fmt.Sprintf("%d/%d", meta.Completed, meta.Total))
	switch {
	case hasCompleted && hasStarted:
		text := sty.Tool.TodoStatusNote.Render(fmt.Sprintf(" · completed %d, starting next", len(meta.JustCompleted)))
		headerText = fmt.Sprintf("%s%s", ratio, text)
	case hasCompleted:
		text := sty.Tool.TodoStatusNote.Render(fmt.Sprintf(" · completed %d", len(meta.JustCompleted)))
		if allCompleted {
			text = sty.Tool.TodoStatusNote.Render(" · completed all")
		}
		headerText = fmt.Sprintf("%s%s", ratio, text)
	case hasStarted:
		headerText = fmt.Sprintf("%s%s", ratio, sty.Tool.TodoStatusNote.Render(" · starting task"))
	default:
		headerText = ratio
	}

	if allCompleted {
		// Show all todos when all are completed, like when created.
		body = FormatTodosList(sty, meta.Todos, styles.ArrowRightIcon, cappedWidth)
	} else if meta.JustStarted != "" {
		body = sty.Tool.TodoInProgressIcon.Render(styles.ArrowRightIcon+" ") +
			sty.Tool.TodoJustStarted.Render(meta.JustStarted)
	}
	return headerText, body
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
