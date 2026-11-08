// Package pills renders the session "pills" (todo progress and prompt queue)
// shown above the prompt. It is a pure rendering package: it takes todo lists,
// style tables, and widths, and returns strings, so the UI model keeps only the
// state machine.
package pills

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/ui/chat"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

const (
	// maxTaskDisplayLength is the maximum length of a task name in the pill.
	maxTaskDisplayLength = 40
	// maxQueueDisplayLength is the maximum length of a queue item in the list.
	maxQueueDisplayLength = 60
)

// Style returns the appropriate style for a pill based on focus state.
func Style(focused, panelFocused bool, t *styles.Styles) lipgloss.Style {
	if !panelFocused || focused {
		return t.Pills.Focused
	}
	return t.Pills.Blurred
}

// HasIncompleteTodos reports whether any todo is not completed.
func HasIncompleteTodos(todos []session.Todo) bool {
	return session.HasIncompleteTodos(todos)
}

// HasInProgressTodo reports whether at least one todo is in progress.
func HasInProgressTodo(todos []session.Todo) bool {
	for _, todo := range todos {
		if todo.Status == session.TodoStatusInProgress {
			return true
		}
	}
	return false
}

// QueuePill renders the queue count pill with gradient triangles.
func QueuePill(queue int, focused, panelFocused bool, t *styles.Styles) string {
	if queue <= 0 {
		return ""
	}
	triangles := styles.ForegroundGrad(t.Pills.QueueIconBase, "▶▶▶▶▶▶▶▶▶", false, t.Pills.QueueGradFromColor, t.Pills.QueueGradToColor)
	if queue < len(triangles) {
		triangles = triangles[:queue]
	}

	text := t.Pills.QueueLabel.Render(fmt.Sprintf("%d Queued", queue))
	content := fmt.Sprintf("%s %s", strings.Join(triangles, ""), text)
	return Style(focused, panelFocused, t).Render(content)
}

// TodoPill renders the todo progress pill with optional spinner and task name.
func TodoPill(todos []session.Todo, spinnerView string, focused, panelFocused bool, t *styles.Styles) string {
	if !HasIncompleteTodos(todos) {
		return ""
	}

	completed := 0
	var currentTodo *session.Todo
	for i := range todos {
		switch todos[i].Status {
		case session.TodoStatusCompleted:
			completed++
		case session.TodoStatusInProgress:
			if currentTodo == nil {
				currentTodo = &todos[i]
			}
		case session.TodoStatusPending:
			// Pending todos only contribute to the total count.
		}
	}

	total := len(todos)

	label := t.Pills.TodoLabel.Render("To-Do")
	progress := t.Pills.TodoProgress.Render(fmt.Sprintf("%d/%d", completed, total))

	var content string
	if panelFocused {
		content = fmt.Sprintf("%s %s", label, progress)
	} else if currentTodo != nil {
		taskText := currentTodo.Content
		if currentTodo.ActiveForm != "" {
			taskText = currentTodo.ActiveForm
		}
		if len(taskText) > maxTaskDisplayLength {
			taskText = taskText[:maxTaskDisplayLength-1] + "…"
		}
		task := t.Pills.TodoCurrentTask.Render(taskText)
		content = fmt.Sprintf("%s %s %s  %s", spinnerView, label, progress, task)
	} else {
		content = fmt.Sprintf("%s %s", label, progress)
	}

	return Style(focused, panelFocused, t).Render(content)
}

// TodoList renders the expanded todo list.
func TodoList(sessionTodos []session.Todo, spinnerView string, t *styles.Styles, width int) string {
	return chat.FormatTodosList(t, sessionTodos, spinnerView, width)
}

// QueueList renders the expanded queue items list.
func QueueList(queueItems []string, t *styles.Styles) string {
	if len(queueItems) == 0 {
		return ""
	}

	var lines []string
	for _, item := range queueItems {
		text := item
		if len(text) > maxQueueDisplayLength {
			text = text[:maxQueueDisplayLength-1] + "…"
		}
		prefix := t.Pills.QueueItemPrefix.Render() + " "
		lines = append(lines, prefix+t.Pills.QueueItemText.Render(text))
	}

	return strings.Join(lines, "\n")
}
