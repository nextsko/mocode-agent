package extension

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterOrError_AcceptsNew(t *testing.T) {
	t.Parallel()
	m := NewManager()
	ext := &FuncExtension{ExtensionName: "obs"}
	require.NoError(t, m.RegisterOrError(ext))
	require.True(t, m.Has("obs"))
}

func TestRegisterOrError_IdempotentSameInstance(t *testing.T) {
	t.Parallel()
	m := NewManager()
	ext := &FuncExtension{ExtensionName: "obs"}
	require.NoError(t, m.RegisterOrError(ext))
	// Re-registering the exact same extension is idempotent, not an error.
	require.NoError(t, m.RegisterOrError(ext))
	require.Len(t, m.Names(), 1)
}

func TestRegisterOrError_RejectsShadowingDuplicate(t *testing.T) {
	t.Parallel()
	m := NewManager()
	first := &FuncExtension{ExtensionName: "obs"}
	second := &FuncExtension{ExtensionName: "obs"}
	require.NoError(t, m.RegisterOrError(first))
	// A different extension under the same name is a shadowing bug.
	err := m.RegisterOrError(second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
	// The first extension stays registered, unshadowed.
	require.True(t, m.Has("obs"))
}

func TestRegisterOrError_RejectsNil(t *testing.T) {
	t.Parallel()
	m := NewManager()
	require.Error(t, m.RegisterOrError(nil))
}

func TestRegisterOrError_RejectsEmptyName(t *testing.T) {
	t.Parallel()
	m := NewManager()
	require.Error(t, m.RegisterOrError(&FuncExtension{ExtensionName: ""}))
}

func TestMustRegister_PanicsOnDuplicate(t *testing.T) {
	t.Parallel()
	m := NewManager()
	first := &FuncExtension{ExtensionName: "obs", Fn: func(context.Context, Event, *Context) (*Decision, error) { return nil, nil }}
	second := &FuncExtension{ExtensionName: "obs"}
	m.MustRegister(first)
	require.Panics(t, func() { m.MustRegister(second) })
	// MustRegister on the same instance does not panic.
	require.NotPanics(t, func() { m.MustRegister(first) })
}
