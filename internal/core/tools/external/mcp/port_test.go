package mcp

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

func TestListMCPResourcesPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewListMCPResourcesPortTool(nil, nil)).Info()
	require.Equal(t, ListMCPResourcesToolName, info.Name)
	require.True(t, info.Parallel, "list_mcp_resources must stay parallel-safe")
	require.Equal(t, []string{"mcp_name"}, info.Required)
}

func TestReadMCPResourcePortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewReadMCPResourcePortTool(nil, nil)).Info()
	require.Equal(t, ReadMCPResourceToolName, info.Name)
	require.True(t, info.Parallel, "read_mcp_resource must stay parallel-safe")
	require.Equal(t, []string{"mcp_name", "uri"}, info.Required)
}

func TestReadMCPResourcePortTool_MissingURI(t *testing.T) {
	t.Parallel()

	resp, err := asAgentTool(NewReadMCPResourcePortTool(nil, nil)).Run(t.Context(), fantasy.ToolCall{Input: `{"mcp_name":"docs"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "uri parameter is required", resp.Content)
}
