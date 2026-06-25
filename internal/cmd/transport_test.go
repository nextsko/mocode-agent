package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandHasNoServerSubcommand(t *testing.T) {
	t.Parallel()

	for _, c := range rootCmd.Commands() {
		require.NotEqual(t, "server", c.Name(), "backend RPC server was removed; mocode server subcommand must not exist")
	}
}

func TestRootCommandHasNoWebSubcommand(t *testing.T) {
	t.Parallel()

	for _, c := range rootCmd.Commands() {
		require.NotEqual(t, "web", c.Name(), "browser web chat was removed; mocode web subcommand must not exist")
	}
}

func TestRootCommandHasNoQuotaSubcommand(t *testing.T) {
	t.Parallel()

	for _, c := range rootCmd.Commands() {
		require.NotEqual(t, "quota", c.Name(), "MiniMax quota CLI was removed; mocode quota subcommand must not exist")
	}
}
