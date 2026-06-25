package evo

import (
	"context"
	"errors"
	"testing"

	"github.com/package-register/mocode/internal/core/agent/extension"
	"github.com/stretchr/testify/require"
)

func TestObservability_FoldsSuccessfulRunIntoState(t *testing.T) {
	t.Parallel()
	var st State
	st.Enter("reviewer")
	ext := NewObservabilityExtension(&st)
	mgr := extension.NewManager().Register(ext)

	// A successful run: AfterRun folds the prompt into the working theory.
	mgr.Dispatch(context.Background(), extension.EventAfterRun, &extension.Context{
		AgentName: "reviewer", Prompt: "refactor the auth module",
	})
	require.Equal(t, 1, st.Iterations)
	require.Equal(t, "refactor the auth module", st.PendingPrompt)

	// A second successful run advances the theory.
	mgr.Dispatch(context.Background(), extension.EventAfterRun, &extension.Context{
		AgentName: "reviewer", Prompt: "add tests for the new helper",
	})
	require.Equal(t, 2, st.Iterations)
	require.Equal(t, "add tests for the new helper", st.PendingPrompt)
}

func TestObservability_ErrorDoesNotFoldButLogs(t *testing.T) {
	t.Parallel()
	var st State
	st.Enter("reviewer")
	ext := NewObservabilityExtension(&st)
	mgr := extension.NewManager().Register(ext)

	before := st.Iterations
	mgr.Dispatch(context.Background(), extension.EventOnError, &extension.Context{
		AgentName: "reviewer", Err: errors.New("provider 500"),
	})
	// Errors are observed (evolution data) but do not advance the theory.
	require.Equal(t, before, st.Iterations)
}

func TestObservability_IgnoresIrrelevantEvents(t *testing.T) {
	t.Parallel()
	var st State
	st.Enter("reviewer")
	ext := NewObservabilityExtension(&st)
	mgr := extension.NewManager().Register(ext)

	for _, ev := range []extension.Event{
		extension.EventBeforeRun, extension.EventBeforeToolCall,
		extension.EventAfterToolCall, extension.EventOnMessage,
	} {
		mgr.Dispatch(context.Background(), ev, &extension.Context{Prompt: "x"})
	}
	require.Equal(t, 0, st.Iterations)
}
