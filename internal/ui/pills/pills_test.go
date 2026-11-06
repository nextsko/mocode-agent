package pills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

func TestHasIncompleteTodos(t *testing.T) {
	t.Parallel()

	require.False(t, HasIncompleteTodos(nil))
	require.False(t, HasIncompleteTodos([]session.Todo{
		{Status: session.TodoStatusCompleted},
	}))
	require.True(t, HasIncompleteTodos([]session.Todo{
		{Status: session.TodoStatusCompleted},
		{Status: session.TodoStatusPending},
	}))
	require.True(t, HasIncompleteTodos([]session.Todo{
		{Status: session.TodoStatusInProgress},
	}))
}

func TestHasInProgressTodo(t *testing.T) {
	t.Parallel()

	require.False(t, HasInProgressTodo(nil))
	require.False(t, HasInProgressTodo([]session.Todo{
		{Status: session.TodoStatusPending},
		{Status: session.TodoStatusCompleted},
	}))
	require.True(t, HasInProgressTodo([]session.Todo{
		{Status: session.TodoStatusCompleted},
		{Status: session.TodoStatusInProgress},
	}))
}

func TestTodoPillEmptyWithoutOpenTodos(t *testing.T) {
	t.Parallel()

	sty := &styles.Styles{}
	require.Empty(t, TodoPill(nil, "", false, false, sty))
	require.Empty(t, TodoPill([]session.Todo{{Status: session.TodoStatusCompleted}}, "", false, false, sty))
}

func TestQueueListTruncatesLongItems(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 200)
	out := QueueList([]string{long}, &styles.Styles{})
	require.Contains(t, out, "…")
	require.Less(t, len(out), len(long))
}

func TestQueueListEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, QueueList(nil, &styles.Styles{}))
}
