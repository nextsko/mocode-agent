package job_input_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/builtin/job_input"
)

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(job_input.JobInputToolName)
	require.True(t, ok)
	require.Equal(t, job_input.JobInputToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, job_input.JobInputToolName, tool.Schema().Name)
}

func TestExecuteMissingShellID(t *testing.T) {
	t.Parallel()

	result, err := job_input.New().Execute(context.Background(), nil, json.RawMessage(`{"input":"hello"}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "missing shell_id")
}

func TestExecuteMissingInput(t *testing.T) {
	t.Parallel()

	result, err := job_input.New().Execute(context.Background(), nil, json.RawMessage(`{"shell_id":"shell-1"}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "missing input")
}
