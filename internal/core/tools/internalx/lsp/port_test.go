package lsp

import (
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/agenttooladapter"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// asAgentTool wraps a migrated port with the production adapter so tests can
// keep exercising the fantasy AgentTool surface (Run/Info).
func asAgentTool(port contract.Tool) fantasy.AgentTool {
	return agenttooladapter.New(port, nil)
}

func TestReferencesPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewReferencesPortTool(nil)).Info()
	require.Equal(t, ReferencesToolName, info.Name)
	require.Equal(t, []string{"symbol"}, info.Required)
	require.Contains(t, info.Parameters, "path")
}

func TestLSPRestartPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewLSPRestartPortTool(nil)).Info()
	require.Equal(t, LSPRestartToolName, info.Name)
	require.Contains(t, info.Parameters, "name")
	require.Empty(t, info.Required)
}

func TestReferencesPortTool_MissingSymbol(t *testing.T) {
	t.Parallel()

	resp, err := asAgentTool(NewReferencesPortTool(nil)).Run(t.Context(), fantasy.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "symbol is required", resp.Content)
}
