package evo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/package-register/mocode/internal/core/agent/extension"
	"github.com/stretchr/testify/require"
)

func TestObservability_ReconstructsTheoryFromRuns(t *testing.T) {
	t.Parallel()
	var st State
	st.Enter("reviewer", "You are a careful engineer.")
	ext := NewObservabilityExtension(&st)
	mgr := extension.NewManager().Register(ext)

	// A successful run: its prompt is distilled into a lesson and folded
	// into the reconstructed theory, layered on the proven base.
	mgr.Dispatch(context.Background(), extension.EventAfterRun, &extension.Context{
		AgentName: "reviewer", Prompt: "refactor the auth module",
	})
	require.Equal(t, 1, st.Iterations)
	require.Contains(t, st.PendingPrompt, "You are a careful engineer.")
	require.Contains(t, st.PendingPrompt, "refactor the auth module")
	require.Contains(t, st.PendingPrompt, "Refined theory (1 lessons")

	// A second successful run adds a second lesson; the base is preserved.
	mgr.Dispatch(context.Background(), extension.EventAfterRun, &extension.Context{
		AgentName: "reviewer", Prompt: "add tests for the new helper",
	})
	require.Equal(t, 2, st.Iterations)
	require.Contains(t, st.PendingPrompt, "add tests for the new helper")
	require.Contains(t, st.PendingPrompt, "Refined theory (2 lessons")
	// Lessons are deduplicated: re-folding an identical prompt does not add.
	mgr.Dispatch(context.Background(), extension.EventAfterRun, &extension.Context{
		AgentName: "reviewer", Prompt: "refactor the auth module",
	})
	require.Contains(t, st.PendingPrompt, "Refined theory (2 lessons")
}

func TestObservability_ErrorDoesNotFoldButLogs(t *testing.T) {
	t.Parallel()
	var st State
	st.Enter("reviewer", "")
	ext := NewObservabilityExtension(&st)
	mgr := extension.NewManager().Register(ext)

	before := st.Iterations
	mgr.Dispatch(context.Background(), extension.EventOnError, &extension.Context{
		AgentName: "reviewer", Err: errors.New("provider 500"),
	})
	require.Equal(t, before, st.Iterations)
}

func TestObservability_IgnoresIrrelevantEvents(t *testing.T) {
	t.Parallel()
	var st State
	st.Enter("reviewer", "")
	ext := NewObservabilityExtension(&st)
	mgr := extension.NewManager().Register(ext)

	for _, ev := range []extension.Event{
		extension.EventBeforeRun, extension.EventBeforeToolCall,
		extension.EventAfterToolCall, extension.EventOnMessage,
	} {
		mgr.Dispatch(context.Background(), ev, &extension.Context{Prompt: "x"})
	}
	require.Equal(t, 0, st.Iterations)
	require.False(t, strings.Contains(st.PendingPrompt, "Refined theory"))
}
