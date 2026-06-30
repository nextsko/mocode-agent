package tools_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
)

type runtimeDependencyValue struct {
	value string
}

type runtimeDependencyContext struct {
	deps tools.RuntimeDependencies
}

func (c *runtimeDependencyContext) SessionID() string { return "session-1" }

func (c *runtimeDependencyContext) WorkingDir() string { return "C:/work" }

func (c *runtimeDependencyContext) Permissions() tools.PermissionChecker { return nil }

func (c *runtimeDependencyContext) MCP() tools.MCPHandles { return nil }

func (c *runtimeDependencyContext) Callbacks() tools.ToolCallbacks { return nil }

func (c *runtimeDependencyContext) RuntimeDependencies() tools.RuntimeDependencies { return c.deps }

type runtimeDependencyBareContext struct{}

func (c runtimeDependencyBareContext) SessionID() string { return "session-1" }

func (c runtimeDependencyBareContext) WorkingDir() string { return "C:/work" }

func (c runtimeDependencyBareContext) Permissions() tools.PermissionChecker { return nil }

func (c runtimeDependencyBareContext) MCP() tools.MCPHandles { return nil }

func (c runtimeDependencyBareContext) Callbacks() tools.ToolCallbacks { return nil }

func TestRuntimeDependencySetCopiesInput(t *testing.T) {
	t.Parallel()

	want := &runtimeDependencyValue{value: "original"}
	values := map[tools.RuntimeDependencyKey]any{
		tools.RuntimeDependencyConfigStore: want,
	}
	set := tools.NewRuntimeDependencySet(values)
	values[tools.RuntimeDependencyConfigStore] = &runtimeDependencyValue{value: "mutated"}

	got, ok := set.Get(tools.RuntimeDependencyConfigStore)
	require.True(t, ok)
	require.Same(t, want, got)
}

func TestRuntimeDependencyTypedLookup(t *testing.T) {
	t.Parallel()

	want := &runtimeDependencyValue{value: "config"}
	tctx := &runtimeDependencyContext{
		deps: tools.NewRuntimeDependencySet(map[tools.RuntimeDependencyKey]any{
			tools.RuntimeDependencyConfigStore: want,
			tools.RuntimeDependencyModelName:   "gpt-test",
		}),
	}

	got, ok := tools.RuntimeDependency[*runtimeDependencyValue](tctx, tools.RuntimeDependencyConfigStore)
	require.True(t, ok)
	require.Same(t, want, got)

	model, ok := tools.RuntimeDependency[string](tctx, tools.RuntimeDependencyModelName)
	require.True(t, ok)
	require.Equal(t, "gpt-test", model)
}

func TestRuntimeDependencyTypedLookupRejectsMissingProviderOrWrongType(t *testing.T) {
	t.Parallel()

	_, ok := tools.RuntimeDependency[*runtimeDependencyValue](runtimeDependencyBareContext{}, tools.RuntimeDependencyConfigStore)
	require.False(t, ok)

	tctx := &runtimeDependencyContext{
		deps: tools.NewRuntimeDependencySet(map[tools.RuntimeDependencyKey]any{
			tools.RuntimeDependencyConfigStore: "not-a-config-store",
		}),
	}
	_, ok = tools.RuntimeDependency[*runtimeDependencyValue](tctx, tools.RuntimeDependencyConfigStore)
	require.False(t, ok)

	_, ok = tools.RuntimeDependency[string](tctx, tools.RuntimeDependencyLSPManager)
	require.False(t, ok)
}
