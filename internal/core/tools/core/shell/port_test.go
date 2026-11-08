package shell

import (
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/agenttooladapter"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

// asAgentTool wraps a migrated port with the production adapter so tests can
// keep exercising the fantasy AgentTool surface (Run/Info) they historically
// asserted against.
func asAgentTool(port contract.Tool) fantasy.AgentTool {
	return agenttooladapter.New(port, nil)
}

func TestBashPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewBashPortTool(nil, t.TempDir(), &config.Attribution{}, "test-model")).Info()
	require.Equal(t, BashToolName, info.Name)
	require.NotEmpty(t, info.Description)
	require.Equal(t, []string{"description", "command"}, info.Required)
	require.Contains(t, info.Parameters, "run_in_background")
	require.Contains(t, info.Parameters, "auto_background_after")
}

func TestJobOutputPortTool_Info(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewJobOutputPortTool()).Info()
	require.Equal(t, JobOutputToolName, info.Name)
	require.Equal(t, []string{"shell_id", "wait"}, info.Required)
}

func TestJobKillPortTool_MissingShellID(t *testing.T) {
	t.Parallel()

	resp, err := asAgentTool(NewJobKillPortTool()).Run(t.Context(), fantasy.ToolCall{ID: "call-1", Input: `{}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "missing shell_id", resp.Content)
}

func TestJobInputPortTool_MissingInput(t *testing.T) {
	t.Parallel()

	resp, err := asAgentTool(NewJobInputPortTool()).Run(t.Context(), fantasy.ToolCall{Input: `{"shell_id":"job-1"}`})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, "missing input", resp.Content)
}
