package think_test

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/core/agent/tools/plugins/think"
)

func TestThinkTool_BasicThought(t *testing.T) {
	t.Parallel()

	tool := think.NewThinkTool()

	params := think.ThinkParams{
		Thought: "I need to check the file exists before editing it.",
	}
	input, err := json.Marshal(params)
	require.NoError(t, err)

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test-1",
		Name:  think.ThinkToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.Equal(t, params.Thought, resp.Content)
}

func TestThinkTool_MetadataContainsThought(t *testing.T) {
	t.Parallel()

	tool := think.NewThinkTool()
	thought := "Breaking the problem into: (1) list files, (2) read, (3) edit."
	params := think.ThinkParams{Thought: thought}
	input, err := json.Marshal(params)
	require.NoError(t, err)

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test-2",
		Name:  think.ThinkToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.NotEmpty(t, resp.Metadata)

	var meta think.ThinkResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.Equal(t, thought, meta.Thought)
}

func TestThinkTool_EmptyThought(t *testing.T) {
	t.Parallel()

	tool := think.NewThinkTool()
	params := think.ThinkParams{Thought: ""}
	input, err := json.Marshal(params)
	require.NoError(t, err)

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "test-3",
		Name:  think.ThinkToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
}

func TestThinkTool_IsReadOnly(t *testing.T) {
	t.Parallel()

	// Calling think multiple times must be idempotent — no side effects.
	tool := think.NewThinkTool()
	call := func(thought string) fantasy.ToolResponse {
		params := think.ThinkParams{Thought: thought}
		input, _ := json.Marshal(params)
		resp, err := tool.Run(context.Background(), fantasy.ToolCall{
			ID:    "rw-" + thought,
			Name:  think.ThinkToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		return resp
	}

	r1 := call("thought A")
	r2 := call("thought B")
	require.Equal(t, "thought A", r1.Content)
	require.Equal(t, "thought B", r2.Content)
}
