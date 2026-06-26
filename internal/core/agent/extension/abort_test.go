package extension

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDispatch_AbortRunPropagated verifies that when an extension returns a
// Decision with AbortRun=true, Dispatch returns that Decision to the caller.
// The coordinator relies on this to honor BeforeRun aborts (stop the run
// before any work begins). Without propagation the guardrail is silently lost.
func TestDispatch_AbortRunPropagated(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var secondCalled bool
	m.Register(&FuncExtension{
		ExtensionName: "guardrail",
		Fn: func(_ context.Context, _ Event, _ *Context) (*Decision, error) {
			return &Decision{AbortRun: true, AbortReason: "budget exceeded"}, nil
		},
	})
	m.Register(&FuncExtension{
		ExtensionName: "observer",
		Fn: func(_ context.Context, _ Event, _ *Context) (*Decision, error) {
			secondCalled = true
			return nil, nil
		},
	})

	dec := m.Dispatch(context.Background(), EventBeforeRun, &Context{SessionID: "s"})
	require.NotNil(t, dec, "AbortRun decision must propagate from Dispatch")
	require.True(t, dec.AbortRun)
	require.Equal(t, "budget exceeded", dec.AbortReason)

	// Per the Dispatch contract, ANY non-nil Decision short-circuits — so
	// AbortRun stops later extensions just like SkipTool. This is correct:
	// once a guardrail decides to abort, later extensions need not observe.
	require.False(t, secondCalled, "AbortRun short-circuits dispatch; later callback must not run")
}

// TestDispatch_AbortRunNilWhenNoneAborts confirms that a run with no aborting
// extension returns a nil Decision (the normal case the coordinator sees).
func TestDispatch_AbortRunNilWhenNoneAborts(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.Register(&FuncExtension{
		ExtensionName: "harmless",
		Fn: func(_ context.Context, _ Event, _ *Context) (*Decision, error) {
			return nil, nil
		},
	})
	dec := m.Dispatch(context.Background(), EventBeforeRun, &Context{})
	require.Nil(t, dec, "no abort -> nil Decision")
}

// TestDispatch_SkipToolStillShortCircuits confirms the SkipTool behavior is
// distinct from AbortRun: SkipTool stops later callbacks, AbortRun does not.
func TestDispatch_SkipToolStillShortCircuits(t *testing.T) {
	t.Parallel()
	m := NewManager()
	var laterCalled bool
	m.Register(&FuncExtension{
		ExtensionName: "blocker",
		Fn: func(_ context.Context, _ Event, _ *Context) (*Decision, error) {
			return &Decision{SkipTool: true, AbortReason: "policy"}, nil
		},
	})
	m.Register(&FuncExtension{
		ExtensionName: "later",
		Fn: func(_ context.Context, _ Event, _ *Context) (*Decision, error) {
			laterCalled = true
			return nil, nil
		},
	})
	dec := m.Dispatch(context.Background(), EventBeforeToolCall, &Context{})
	require.NotNil(t, dec)
	require.True(t, dec.SkipTool)
	require.False(t, laterCalled, "SkipTool short-circuits; later callback must not run")
}
