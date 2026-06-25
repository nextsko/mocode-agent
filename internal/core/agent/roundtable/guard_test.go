package roundtable

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolGuard_CanUse(t *testing.T) {
	t.Parallel()
	g := NewToolGuard()

	require.True(t, g.CanUse(PhaseDiscussion, "view"))
	require.True(t, g.CanUse(PhaseDiscussion, "think"))
	require.False(t, g.CanUse(PhaseDiscussion, "edit"))
	require.False(t, g.CanUse(PhaseDiscussion, "bash"))

	require.True(t, g.CanUse(PhaseExecution, "edit"))
	require.True(t, g.CanUse(PhaseExecution, "bash"))
}

func TestToolGuard_Deny(t *testing.T) {
	t.Parallel()
	g := NewToolGuard()
	err := g.Deny(PhaseDiscussion, "edit")
	require.Error(t, err)
	require.Contains(t, err.Error(), "edit")
}
