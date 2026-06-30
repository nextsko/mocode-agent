package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
)

type fakeToolContextPermissionChecker struct {
	allowed map[string]bool
	seen    []string
}

func (f *fakeToolContextPermissionChecker) Allow(name string) bool {
	f.seen = append(f.seen, name)
	return f.allowed[name]
}

type fakeToolContextMCPStates struct {
	servers []mcpState
}

func (f fakeToolContextMCPStates) Servers() []mcpState {
	return f.servers
}

type fakeToolContextTool struct {
	name string
}

func (f *fakeToolContextTool) Name() string { return f.name }

func (f *fakeToolContextTool) Description() string { return f.name }

func (f *fakeToolContextTool) Schema() tools.Schema { return tools.Schema{Name: f.name} }

func (f *fakeToolContextTool) Execute(context.Context, tools.ToolContext, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{Content: f.name}, nil
}

func TestAgentToolContextExposesExpectedValues(t *testing.T) {
	t.Parallel()

	perms := &fakeToolContextPermissionChecker{allowed: map[string]bool{"view": true}}
	mcpTool := &fakeToolContextTool{name: "mcp_tool"}
	mcp := fakeToolContextMCPStates{servers: []mcpState{{Name: "server-a", State: "connected", Tools: []tools.Tool{mcpTool}}}}

	beforeArgs := json.RawMessage(`{"file_path":"README.md"}`)
	afterResult := tools.ToolResult{Content: "ok"}
	afterErr := errors.New("boom")

	var gotBeforeName string
	var gotBeforeArgs json.RawMessage
	var gotAfterName string
	var gotAfterResult tools.ToolResult
	var gotAfterErr error
	callbacks := &Callbacks{
		Before: func(name string, args json.RawMessage) {
			gotBeforeName = name
			gotBeforeArgs = append(json.RawMessage(nil), args...)
		},
		After: func(name string, result tools.ToolResult, err error) {
			gotAfterName = name
			gotAfterResult = result
			gotAfterErr = err
		},
	}

	tctx, cleanup := NewToolContext("session-1", "C:/work", perms, mcp, callbacks)
	defer cleanup()

	require.Equal(t, "session-1", tctx.SessionID())
	require.Equal(t, "C:/work", tctx.WorkingDir())
	require.True(t, tctx.Permissions().Allow("view"))
	require.False(t, tctx.Permissions().Allow("edit"))
	require.Equal(t, []string{"view", "edit"}, perms.seen)

	servers := tctx.MCP().Servers()
	require.Len(t, servers, 1)
	require.Equal(t, "server-a", servers[0].Name)
	require.Equal(t, "connected", servers[0].State)
	require.Len(t, servers[0].Tools, 1)
	require.Same(t, mcpTool, servers[0].Tools[0])

	tctx.Callbacks().Before("view", beforeArgs)
	tctx.Callbacks().After("view", afterResult, afterErr)
	require.Equal(t, "view", gotBeforeName)
	require.JSONEq(t, string(beforeArgs), string(gotBeforeArgs))
	require.Equal(t, "view", gotAfterName)
	require.Equal(t, afterResult, gotAfterResult)
	require.ErrorIs(t, gotAfterErr, afterErr)
}

func TestAgentToolContextDefaultsAreSafe(t *testing.T) {
	t.Parallel()

	tctx, cleanup := NewToolContext("session-2", "C:/work", nil, nil, nil)
	defer cleanup()

	require.Equal(t, "session-2", tctx.SessionID())
	require.Equal(t, "C:/work", tctx.WorkingDir())
	require.False(t, tctx.Permissions().Allow("view"))
	require.Nil(t, tctx.MCP().Servers())
	require.NotPanics(t, func() {
		tctx.Callbacks().Before("view", json.RawMessage(`{}`))
		tctx.Callbacks().After("view", tools.ToolResult{}, nil)
	})
}

func TestAgentToolContextCarriesRuntimeDependencies(t *testing.T) {
	t.Parallel()

	type dependency struct{ value string }
	want := &dependency{value: "cfg"}
	tctx, cleanup := NewToolContext(
		"session-3",
		"C:/work",
		nil,
		nil,
		nil,
		tools.NewRuntimeDependencySet(map[tools.RuntimeDependencyKey]any{
			tools.RuntimeDependencyConfigStore: want,
		}),
	)
	defer cleanup()

	got, ok := tools.RuntimeDependency[*dependency](tctx, tools.RuntimeDependencyConfigStore)
	require.True(t, ok)
	require.Same(t, want, got)
}
