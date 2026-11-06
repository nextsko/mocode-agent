package agenttooladapter_test

import (
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/agenttooladapter"
	agenttools "github.com/nextsko/mocode-agent/internal/core/tools/internalx/agent"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/store"
)

// TestSessionSearchPortEndToEnd proves the migrated tool works through the
// adapter against a real store: create → index → search → adapter response.
func TestSessionSearchPortEndToEnd(t *testing.T) {
	t.Setenv("MOCODE_GLOBAL_DATA", t.TempDir())

	st, err := store.New(t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	ctx := t.Context()
	sess, err := st.Sessions().Create(ctx, "adapter e2e")
	require.NoError(t, err)
	_, err = st.Messages().Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "needle in the haystack"}},
	})
	require.NoError(t, err)

	tool := agenttools.NewSessionSearchPortTool(st.Search())
	adapter := agenttooladapter.New(tool, nil)

	resp, err := adapter.Run(ctx, fantasy.ToolCall{Input: `{"query":"needle"}`})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Found 1 session")
}
