package browser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIDissectTool_ListWithoutMaps(t *testing.T) {
	t.Parallel()
	tool := NewUIDissectPortTool(nil, t.TempDir())
	require.Equal(t, UIDissectToolName, tool.Name())

	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"list"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "No UI maps saved yet")
}

func TestUIDissectTool_RejectsUnknownAction(t *testing.T) {
	t.Parallel()
	tool := NewUIDissectPortTool(nil, t.TempDir())
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"nope"}`))
	require.NoError(t, err)
	require.Error(t, res.Error)
	assert.Contains(t, res.Error.Error(), "action must be one of")
}

func TestUIDissectTool_SchemaExposesActions(t *testing.T) {
	t.Parallel()
	schema := NewUIDissectPortTool(nil, t.TempDir()).Schema()
	props, ok := schema.Parameters["properties"].(map[string]any)
	require.True(t, ok)
	action, ok := props["action"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"map", "review", "replicate", subActionList}, action["enum"])
}
