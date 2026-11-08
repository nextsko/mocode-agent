package sandbox

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

func TestPyRunPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewPyRunPortTool(nil, "")).Info()
	require.Equal(t, PyRunToolName, info.Name)
	require.Equal(t, []string{"code"}, info.Required)
	require.Contains(t, info.Parameters, "deps")
	require.Contains(t, info.Parameters, "timeout_sec")
}

func TestTsRunPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewTsRunPortTool(nil, "")).Info()
	require.Equal(t, TsRunToolName, info.Name)
	require.Equal(t, []string{"code"}, info.Required)
	require.Contains(t, info.Parameters, "working_dir")
}

func TestPyRunPortTool_MissingCode(t *testing.T) {
	t.Parallel()

	resp, err := asAgentTool(NewPyRunPortTool(nil, "")).Run(t.Context(), fantasy.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "missing code", resp.Content)
}

func TestTsRunPortTool_MissingCode(t *testing.T) {
	t.Parallel()

	resp, err := asAgentTool(NewTsRunPortTool(nil, "")).Run(t.Context(), fantasy.ToolCall{Input: `{}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "missing code", resp.Content)
}
