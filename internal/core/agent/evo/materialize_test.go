package evo

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/stretchr/testify/require"
)

func TestMaterializeAgent_RoundTripsThroughConfigLoader(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewFixationStore(root)

	_, err := s.Fix(context.Background(), "reviewer", "Reviewer", "tamed reviewer", Revision{
		SystemPrompt: "You are a meticulous reviewer.\nAlways cite the file.",
		Skills:       []string{"bash", "view", "read_files"},
	})
	require.NoError(t, err)

	entry, err := LoadAgent(context.Background(), root, "reviewer")
	require.NoError(t, err)

	agentsDir := filepath.Join(t.TempDir(), "agents")
	require.NoError(t, MaterializeAgent(entry, agentsDir))

	// The config loader must read it back as a runnable agent.
	loaded := config.LoadAgentsFromDir(agentsDir, nil)
	agent, ok := loaded["evo-reviewer"]
	require.True(t, ok, "materialized agent must be loadable by config; got keys: %v", mapKeys(loaded))
	require.Equal(t, "Reviewer", agent.Name)
	require.Contains(t, agent.SystemPrompt, "meticulous reviewer")
	require.Contains(t, agent.SystemPrompt, "Always cite the file.")
	require.ElementsMatch(t, []string{"bash", "view", "read_files"}, agent.AllowedTools)
}

func mapKeys(m map[string]config.Agent) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}