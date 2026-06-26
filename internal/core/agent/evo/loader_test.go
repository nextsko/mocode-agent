package evo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAgents_ReturnsFixedAgents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := NewFixationStore(root)

	_, err := s.Fix(context.Background(), "reviewer", "Reviewer", "tamed", Revision{
		SystemPrompt: "you are a reviewer", Skills: []string{"code-review"},
	})
	require.NoError(t, err)
	_, err = s.Fix(context.Background(), "planner", "Planner", "tamed", Revision{
		SystemPrompt: "you are a planner",
	})
	require.NoError(t, err)

	agents, err := LoadAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, agents, 2)
	require.Equal(t, "planner", agents[0].Manifest.Name)
	require.Equal(t, "reviewer", agents[1].Manifest.Name)
	require.Equal(t, "you are a reviewer", agents[1].Revision.SystemPrompt)

	require.Equal(t, "evo:reviewer", ModeID("reviewer"))
	require.Equal(t, "code-review", agents[1].SkillList())
}

func TestLoadAgent_MissingIsNotExist(t *testing.T) {
	t.Parallel()
	_, err := LoadAgent(context.Background(), t.TempDir(), "ghost")
	require.Error(t, err)
}
