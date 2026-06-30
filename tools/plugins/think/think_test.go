package think_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	"github.com/package-register/mocode/tools/plugins/think"
)

func TestRegistered(t *testing.T) {
	t.Parallel()

	tool, ok := tools.Get(think.ThinkToolName)
	require.True(t, ok)
	require.Equal(t, think.ThinkToolName, tool.Name())
	require.NotEmpty(t, tool.Description())
	require.Equal(t, think.ThinkToolName, tool.Schema().Name)
}

func TestExecuteEchoesThought(t *testing.T) {
	t.Parallel()

	result, err := think.New().Execute(context.Background(), nil, json.RawMessage(`{"thought":"check plan"}`))
	require.NoError(t, err)
	require.NoError(t, result.Error)
	require.Equal(t, "check plan", result.Content)
	require.Equal(t, "check plan", result.Metadata["thought"])
}
