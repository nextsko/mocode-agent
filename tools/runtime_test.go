package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type runtimeDependencyValue struct {
	value string
}

type runtimeDependencyContext struct {
	deps RuntimeDependencies
}

func (c *runtimeDependencyContext) SessionID() string { return "session-1" }

func (c *runtimeDependencyContext) WorkingDir() string { return "C:/work" }

func (c *runtimeDependencyContext) Permissions() PermissionChecker { return nil }

func (c *runtimeDependencyContext) MCP() MCPHandles { return nil }

func (c *runtimeDependencyContext) Callbacks() ToolCallbacks { return nil }

func (c *runtimeDependencyContext) RuntimeDependencies() RuntimeDependencies { return c.deps }

type runtimeDependencyBareContext struct{}

func (c runtimeDependencyBareContext) SessionID() string { return "session-1" }

func (c runtimeDependencyBareContext) WorkingDir() string { return "C:/work" }

func (c runtimeDependencyBareContext) Permissions() PermissionChecker { return nil }

func (c runtimeDependencyBareContext) MCP() MCPHandles { return nil }

func (c runtimeDependencyBareContext) Callbacks() ToolCallbacks { return nil }

func TestRuntimeDependencySetCopiesInput(t *testing.T) {
	t.Parallel()

	want := &runtimeDependencyValue{value: "original"}
	values := map[RuntimeDependencyKey]any{
		RuntimeDependencyConfigStore: want,
	}
	set := NewRuntimeDependencySet(values)
	values[RuntimeDependencyConfigStore] = &runtimeDependencyValue{value: "mutated"}

	got, ok := set.Get(RuntimeDependencyConfigStore)
	require.True(t, ok)
	require.Same(t, want, got)
}

func TestRuntimeDependencyTypedLookup(t *testing.T) {
	t.Parallel()

	want := &runtimeDependencyValue{value: "config"}
	tctx := &runtimeDependencyContext{
		deps: NewRuntimeDependencySet(map[RuntimeDependencyKey]any{
			RuntimeDependencyConfigStore: want,
			RuntimeDependencyModelName:   "gpt-test",
		}),
	}

	got, ok := RuntimeDependency[*runtimeDependencyValue](tctx, RuntimeDependencyConfigStore)
	require.True(t, ok)
	require.Same(t, want, got)

	model, ok := RuntimeDependency[string](tctx, RuntimeDependencyModelName)
	require.True(t, ok)
	require.Equal(t, "gpt-test", model)
}

func TestRuntimeDependencyTypedLookupRejectsMissingProviderOrWrongType(t *testing.T) {
	t.Parallel()

	_, ok := RuntimeDependency[*runtimeDependencyValue](runtimeDependencyBareContext{}, RuntimeDependencyConfigStore)
	require.False(t, ok)

	tctx := &runtimeDependencyContext{
		deps: NewRuntimeDependencySet(map[RuntimeDependencyKey]any{
			RuntimeDependencyConfigStore: "not-a-config-store",
		}),
	}
	_, ok = RuntimeDependency[*runtimeDependencyValue](tctx, RuntimeDependencyConfigStore)
	require.False(t, ok)

	_, ok = RuntimeDependency[string](tctx, RuntimeDependencyLSPManager)
	require.False(t, ok)
}
