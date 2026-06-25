package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManager_RegisterAndDispatch(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var calls []string
	m.Register(&FuncExtension{
		ExtensionName: "a",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			calls = append(calls, "a:"+string(ev))
			return nil, nil
		},
	})
	m.Register(&FuncExtension{
		ExtensionName: "b",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			calls = append(calls, "b:"+string(ev))
			return nil, nil
		},
	})

	dec := m.Dispatch(context.Background(), EventBeforeRun, &Context{SessionID: "s1"})
	require.Nil(t, dec)
	require.Equal(t, []string{"a:before_run", "b:before_run"}, calls)
}

func TestManager_DisableSkips(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var called bool
	m.Register(&FuncExtension{
		ExtensionName: "obs",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			called = true
			return nil, nil
		},
	})
	m.Disable("obs")
	require.Nil(t, m.Dispatch(context.Background(), EventBeforeRun, &Context{}))
	require.False(t, called)
	require.True(t, m.Has("obs"))

	m.Enable("obs")
	require.Nil(t, m.Dispatch(context.Background(), EventBeforeRun, &Context{}))
	require.True(t, called)
}

func TestManager_DecisionShortCircuits(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var secondCalled bool
	m.Register(&FuncExtension{
		ExtensionName: "guard",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			return &Decision{SkipTool: true, AbortReason: "blocked"}, nil
		},
	})
	m.Register(&FuncExtension{
		ExtensionName: "after",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			secondCalled = true
			return nil, nil
		},
	})

	dec := m.Dispatch(context.Background(), EventBeforeToolCall, &Context{})
	require.NotNil(t, dec)
	require.True(t, dec.SkipTool)
	// The first decision short-circuits dispatch, so later callbacks never run.
	require.False(t, secondCalled)
}

func TestManager_CallbackErrorDoesNotAbort(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var recovered bool
	m.Register(&FuncExtension{
		ExtensionName: "bad",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			return nil, errors.New("boom")
		},
	})
	m.Register(&FuncExtension{
		ExtensionName: "good",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			recovered = true
			return nil, nil
		},
	})

	require.Nil(t, m.Dispatch(context.Background(), EventOnError, &Context{}))
	require.True(t, recovered, "dispatch must continue past an erroring callback")
}

func TestManager_PanicRecovered(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var afterCalled bool
	m.Register(&FuncExtension{
		ExtensionName: "panicker",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			panic("kaboom")
		},
	})
	m.Register(&FuncExtension{
		ExtensionName: "after",
		Fn: func(ctx context.Context, ev Event, ec *Context) (*Decision, error) {
			afterCalled = true
			return nil, nil
		},
	})

	// Must not panic out of Dispatch.
	require.NotPanics(t, func() {
		m.Dispatch(context.Background(), EventBeforeRun, &Context{})
	})
	require.True(t, afterCalled)
}

func TestManager_Names(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.Register(&FuncExtension{ExtensionName: "x"})
	m.Register(&FuncExtension{ExtensionName: "y"})
	require.ElementsMatch(t, []string{"x", "y"}, m.Names())

	m.Unregister("x")
	require.False(t, m.Has("x"))
	require.True(t, m.Has("y"))
}
