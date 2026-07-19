package chat

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

func TestTodoDialogSelectionIndexPrefersInProgress(t *testing.T) {
	t.Parallel()

	toolCall := message.ToolCall{
		Input: `{"todos":[{"content":"done","status":"completed","active_form":""},{"content":"working","status":"in_progress","active_form":"Working now"},{"content":"next","status":"pending","active_form":""}]}`,
	}

	require.Equal(t, 1, todoDialogSelectionIndex(toolCall, nil))
}

func TestTodoDialogSelectionIndexFallsBackToFirstSortedTodo(t *testing.T) {
	t.Parallel()

	toolCall := message.ToolCall{
		Input: `{"todos":[{"content":"later","status":"pending","active_form":""},{"content":"done","status":"completed","active_form":""}]}`,
	}

	require.Equal(t, 1, todoDialogSelectionIndex(toolCall, nil))
}

func TestTodosToolMessageItemHandleMouseDoubleClick(t *testing.T) {
	t.Parallel()

	sty := styles.ThemeForProvider("")
	item := NewTodosToolMessageItem(
		&sty,
		message.ToolCall{
			ID:    "todo-call",
			Name:  "todos",
			Input: `{"todos":[{"content":"done","status":"completed","active_form":""},{"content":"working","status":"in_progress","active_form":"Working now"}]}`,
		},
		nil,
		false,
	)

	todoItem, ok := item.(*TodosToolMessageItem)
	require.True(t, ok)

	require.Nil(t, todoItem.HandleMouseDoubleClick(ansi.MouseRight, 0, 0))

	cmd := todoItem.HandleMouseDoubleClick(ansi.MouseLeft, 0, 0)
	require.NotNil(t, cmd)

	msg := cmd()
	openMsg, ok := msg.(OpenTodoDialogMsg)
	require.True(t, ok)
	require.Equal(t, 1, openMsg.Selected)
}
