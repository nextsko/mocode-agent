package todos_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/util/pubsub"
	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/todos"
)

type testContext struct {
	sessionID string
	deps      tools.RuntimeDependencies
}

func (c testContext) SessionID() string { return c.sessionID }

func (c testContext) WorkingDir() string { return "C:/work" }

func (c testContext) Permissions() tools.PermissionChecker { return nil }

func (c testContext) MCP() tools.MCPHandles { return nil }

func (c testContext) Callbacks() tools.ToolCallbacks { return nil }

func (c testContext) RuntimeDependencies() tools.RuntimeDependencies { return c.deps }

type fakeSessionService struct {
	current session.Session
	saved   session.Session
}

func (f *fakeSessionService) Subscribe(context.Context) <-chan pubsub.Event[session.Session] {
	return make(chan pubsub.Event[session.Session])
}

func (f *fakeSessionService) Create(context.Context, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) CreateTitleSession(context.Context, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) CreateTaskSession(context.Context, string, string, string) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) Get(context.Context, string) (session.Session, error) {
	return f.current, nil
}

func (f *fakeSessionService) GetLast(context.Context) (session.Session, error) {
	return session.Session{}, nil
}

func (f *fakeSessionService) List(context.Context) ([]session.Session, error) { return nil, nil }

func (f *fakeSessionService) Save(_ context.Context, s session.Session) (session.Session, error) {
	f.saved = s
	return s, nil
}

func (f *fakeSessionService) UpdateTitleAndUsage(context.Context, string, string, int64, int64, int64, int64, float64) error {
	return nil
}

func (f *fakeSessionService) Rename(context.Context, string, string) error { return nil }

func (f *fakeSessionService) Delete(context.Context, string) error { return nil }

func (f *fakeSessionService) IncrementCost(context.Context, string, float64) error { return nil }

func (f *fakeSessionService) CreateAgentToolSessionID(string, string) string { return "" }

func (f *fakeSessionService) ParseAgentToolSessionID(string) (string, string, bool) {
	return "", "", false
}

func (f *fakeSessionService) IsAgentToolSession(string) bool { return false }

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(todos.TodosToolName)
	require.True(t, ok)
	require.Equal(t, todos.TodosToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, todos.TodosToolName, tool.Schema().Name)
}

func TestExecuteRequiresSessionService(t *testing.T) {
	t.Parallel()

	result, err := todos.New().Execute(context.Background(), testContext{sessionID: "session-1"}, json.RawMessage(`{"todos":[]}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "session service is required")
}

func TestExecuteUpdatesTodos(t *testing.T) {
	t.Parallel()

	sessions := &fakeSessionService{current: session.Session{
		ID: "session-1",
		Todos: []session.Todo{
			{Content: "Draft plan", Status: session.TodoStatusInProgress, ActiveForm: "Drafting plan"},
		},
	}}
	tctx := testContext{
		sessionID: "session-1",
		deps: tools.NewRuntimeDependencySet(map[tools.RuntimeDependencyKey]any{
			tools.RuntimeDependencySessions: sessions,
		}),
	}

	result, err := todos.New().Execute(context.Background(), tctx, json.RawMessage(`{"todos":[{"content":"Draft plan","status":"completed","active_form":"Drafting plan"},{"content":"Run tests","status":"in_progress","active_form":"Running tests"}]}`))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Contains(t, result.Content, "Todo list updated successfully")
	require.Len(t, sessions.saved.Todos, 2)
	require.Equal(t, session.TodoStatusCompleted, sessions.saved.Todos[0].Status)
	require.Equal(t, session.TodoStatusInProgress, sessions.saved.Todos[1].Status)
	require.Equal(t, []string{"Draft plan"}, result.Metadata["just_completed"])
	require.Equal(t, "Running tests", result.Metadata["just_started"])
	require.Equal(t, 1, result.Metadata["completed"])
	require.Equal(t, 2, result.Metadata["total"])
}

func TestExecuteRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	sessions := &fakeSessionService{current: session.Session{ID: "session-1"}}
	tctx := testContext{
		sessionID: "session-1",
		deps: tools.NewRuntimeDependencySet(map[tools.RuntimeDependencyKey]any{
			tools.RuntimeDependencySessions: sessions,
		}),
	}

	result, err := todos.New().Execute(context.Background(), tctx, json.RawMessage(`{"todos":[{"content":"Bad","status":"blocked"}]}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "invalid status")
}
