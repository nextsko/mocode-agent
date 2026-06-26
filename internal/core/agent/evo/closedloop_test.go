package evo

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/stretchr/testify/require"
)

// TestClosedLoop_FixateMaterializeLoadRun demonstrates the full /evo closed
// loop: a session fixes an agent, materializes it as a config .md, and the
// config loader picks it up as a runnable agent. This is the end-to-end proof
// that "exit /evo → agent becomes usable like a mode" actually works.
func TestClosedLoop_FixateMaterializeLoadRun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := NewFixationStore(root)
	agentsDir := filepath.Join(t.TempDir(), "agents")

	// 1. Simulate a /evo exit: fix the converged context.
	rev, err := store.Fix(context.Background(), "architect", "Architect",
		"tamed to design systems", Revision{
			SystemPrompt: "You are a senior architect.\nDecompose before coding.",
			Skills:       []string{"bash", "view", "grep", "glob"},
		})
	require.NoError(t, err)
	require.Equal(t, RevisionActive, rev.Status)

	// 2. Load the fixed agent and materialize it into the config agents dir.
	entry, err := LoadAgent(context.Background(), root, "architect")
	require.NoError(t, err)
	require.NoError(t, MaterializeAgent(entry, agentsDir))

	// 3. The config loader must read it back as a runnable, named agent.
	loaded := config.LoadAgentsFromDir(agentsDir, nil)
	agent, ok := loaded["evo-architect"]
	require.True(t, ok, "materialized agent must be selectable; got: %v", mapKeys(loaded))
	require.Equal(t, "Architect", agent.Name)
	require.Equal(t, "tamed to design systems", agent.Description)
	require.Equal(t, "evo-architect", agent.ID)
	// The固化 prompt survived round-trip verbatim.
	require.Contains(t, agent.SystemPrompt, "senior architect")
	require.Contains(t, agent.SystemPrompt, "Decompose before coding")
	// Captured skills became the allowed tool set.
	require.ElementsMatch(t, []string{"bash", "view", "grep", "glob"}, agent.AllowedTools)
	require.False(t, agent.Disabled, "must be enabled to run")

	// 4. A second /evo pass produces a new active revision; re-materializing
	// overwrites the .md with the latest theory (idempotent projection).
	_, err = store.Fix(context.Background(), "architect", "Architect", "", Revision{
		SystemPrompt: "You are a senior architect (v2).\nAlways document trade-offs.",
	})
	require.NoError(t, err)
	entry2, err := LoadAgent(context.Background(), root, "architect")
	require.NoError(t, err)
	require.NoError(t, MaterializeAgent(entry2, agentsDir))

	loaded2 := config.LoadAgentsFromDir(agentsDir, nil)
	require.Len(t, loaded2, 1, "re-materialization overwrites, does not duplicate")
	a2 := loaded2["evo-architect"]
	require.Contains(t, a2.SystemPrompt, "document trade-offs")
}
