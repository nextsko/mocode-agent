package job_kill_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/builtin/job_kill"
)

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(job_kill.JobKillToolName)
	require.True(t, ok)
	require.Equal(t, job_kill.JobKillToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, job_kill.JobKillToolName, tool.Schema().Name)
}

func TestExecuteMissingShellID(t *testing.T) {
	t.Parallel()

	result, err := job_kill.New().Execute(context.Background(), nil, json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Error(t, result.Error)
	require.Contains(t, result.Error.Error(), "missing shell_id")
}
