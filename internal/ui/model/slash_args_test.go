package model

import (
	"testing"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/stretchr/testify/require"
)

func newArgTestUI(t *testing.T) *UI {
	t.Helper()
	ui := newTestUIWithConfig(t, &config.Config{
		Options: &config.Options{},
		Agents: map[string]config.Agent{
			"coder": {ID: "coder", Name: "Code", Description: "Write code"},
			"plan":  {ID: "plan", Name: "SK Plan", Description: "Plan mode"},
			"task":  {ID: "task", Name: "Task", Disabled: true},
		},
	})
	ui.session = nil
	return ui
}

func TestSlashArgCompletion_AgentsModes(t *testing.T) {
	ui := newArgTestUI(t)

	groups, q, ok := ui.slashArgCompletionGroups("/agents ")
	require.True(t, ok)
	require.Equal(t, "", q)
	require.Len(t, groups, 1)
	require.Equal(t, "Modes", groups[0].Label)
	// Disabled agents are hidden.
	require.Len(t, groups[0].Items, 2)

	groups, q, ok = ui.slashArgCompletionGroups("/agents pla")
	require.True(t, ok)
	require.Equal(t, "pla", q)
	require.Len(t, groups[0].Items, 2)

	// Alias forms route to the same completion.
	_, _, ok = ui.slashArgCompletionGroups("/mode co")
	require.True(t, ok)

	// Non-argument forms stay in command mode.
	_, _, ok = ui.slashArgCompletionGroups("/agents")
	require.False(t, ok)
	_, _, ok = ui.slashArgCompletionGroups("/agents plan extra")
	require.False(t, ok, "one argument only")
	_, _, ok = ui.slashArgCompletionGroups("/model pro")
	require.False(t, ok, "unknown command has no arg completion yet")
}
