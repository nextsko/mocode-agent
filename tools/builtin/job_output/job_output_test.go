package job_output_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/builtin/job_output"
)

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(job_output.JobOutputToolName)
	require.True(t, ok)
	require.Equal(t, job_output.JobOutputToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, job_output.JobOutputToolName, tool.Schema().Name)
}

func TestExecuteMissingShellID(t *testing.T) {
	t.Parallel()

	result, err := job_output.New().Execute(context.Background(), nil, json.RawMessage(`{"wait":false}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "missing shell_id")
}
