package evo

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFixationStore_FixCreatesManifestAndRevision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewFixationStore(root)

	rev, err := s.Fix(context.Background(), "reviewer", "Code Reviewer", "tamed code reviewer",
		Revision{SystemPrompt: "You are a meticulous reviewer.", Skills: []string{"code-review"}})
	require.NoError(t, err)
	require.Equal(t, RevisionActive, rev.Status)
	require.NotEmpty(t, rev.ID)

	// Manifest points at the new revision.
	m, loaded, err := s.Load(context.Background(), "reviewer")
	require.NoError(t, err)
	require.Equal(t, "reviewer", m.Name)
	require.Equal(t, "Code Reviewer", m.Title)
	require.Equal(t, rev.ID, m.ActiveRevision)
	require.Equal(t, "You are a meticulous reviewer.", loaded.SystemPrompt)
	require.Equal(t, 1, m.Iterations)
}

func TestFixationStore_SecondFixArchivesPrior(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewFixationStore(root)

	_, err := s.Fix(context.Background(), "a", "A", "", Revision{SystemPrompt: "v1"})
	require.NoError(t, err)
	r2, err := s.Fix(context.Background(), "a", "A", "", Revision{SystemPrompt: "v2"})
	require.NoError(t, err)

	// Active is now r2; r1 is archived.
	m, active, err := s.Load(context.Background(), "a")
	require.NoError(t, err)
	require.Equal(t, r2.ID, m.ActiveRevision)
	require.Equal(t, "v2", active.SystemPrompt)

	hist, err := s.History(context.Background(), "a")
	require.NoError(t, err)
	require.Len(t, hist, 2)
	// Newest first.
	require.Equal(t, r2.ID, hist[0].ID)
	require.Equal(t, RevisionActive, hist[0].Status)
	require.Equal(t, RevisionArchived, hist[1].Status)
	require.Equal(t, 2, m.Iterations)
}

func TestFixationStore_RollbackReactivatesPrior(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewFixationStore(root)

	r1, err := s.Fix(context.Background(), "a", "A", "", Revision{SystemPrompt: "v1"})
	require.NoError(t, err)
	_, err = s.Fix(context.Background(), "a", "A", "", Revision{SystemPrompt: "v2"})
	require.NoError(t, err)

	require.NoError(t, s.Rollback(context.Background(), "a", r1.ID))
	m, active, err := s.Load(context.Background(), "a")
	require.NoError(t, err)
	require.Equal(t, r1.ID, m.ActiveRevision)
	require.Equal(t, "v1", active.SystemPrompt)
	require.Equal(t, RevisionActive, active.Status)
}

func TestFixationStore_ListAgents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewFixationStore(root)

	_, err := s.Fix(context.Background(), "beta", "B", "", Revision{SystemPrompt: "p"})
	require.NoError(t, err)
	_, err = s.Fix(context.Background(), "alpha", "A", "", Revision{SystemPrompt: "p"})
	require.NoError(t, err)

	list, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "alpha", list[0].Name)
	require.Equal(t, "beta", list[1].Name)
}

func TestFixationStore_LoadMissingAgent(t *testing.T) {
	t.Parallel()
	s := NewFixationStore(t.TempDir())
	_, _, err := s.Load(context.Background(), "ghost")
	require.Error(t, err)
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestFixationStore_RejectsEmptyPrompt(t *testing.T) {
	t.Parallel()
	s := NewFixationStore(t.TempDir())
	_, err := s.Fix(context.Background(), "a", "A", "", Revision{SystemPrompt: "   "})
	require.Error(t, err)
}

func TestState_Lifecycle(t *testing.T) {
	t.Parallel()
	var st State
	require.False(t, st.IsActive())

	st.Enter("reviewer", "You are a careful engineer.")
	require.True(t, st.IsActive())
	require.Equal(t, ModeActive, st.Mode)

	// Iterations accumulate; pending prompt and skills fold in.
	st.RecordIteration("optimal theory v1", "refactored auth", []string{"s1"})
	st.RecordIteration("optimal theory v2", "added tests", []string{"s2", "s1"}) // s1 deduped
	require.Equal(t, 2, st.Iterations)
	// The reconstructed theory keeps the proven base and layers both lessons.
	require.Contains(t, st.PendingPrompt, "You are a careful engineer.")
	require.Contains(t, st.PendingPrompt, "optimal theory v1")
	require.Contains(t, st.PendingPrompt, "optimal theory v2")
	require.ElementsMatch(t, []string{"s1", "s2"}, st.CapturedSkills)

	// First exit request -> confirm; second -> snapshot returned.
	require.True(t, st.RequestExit())
	require.Equal(t, ModeConfirmExit, st.Mode)

	// Cancel returns to active.
	st.CancelExit()
	require.Equal(t, ModeActive, st.Mode)

	// Confirm exit yields the snapshot and resets.
	st.RequestExit()
	rev := st.ConfirmExit()
	require.Equal(t, "reviewer", rev.AgentName)
	require.Contains(t, rev.SystemPrompt, "optimal theory v1")
	require.Contains(t, rev.SystemPrompt, "optimal theory v2")
	require.ElementsMatch(t, []string{"s1", "s2"}, rev.Skills)
	require.False(t, st.IsActive())
}
