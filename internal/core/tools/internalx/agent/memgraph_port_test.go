package agenttools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runMemgraphTool(t *testing.T, dir string, p MemgraphParams) string {
	t.Helper()
	tool := NewMemgraphPortTool(dir)
	require.Equal(t, MemgraphToolName, tool.Name())
	body, err := json.Marshal(p)
	require.NoError(t, err)
	res, err := tool.Execute(context.Background(), nil, body)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	return res.Content
}

func TestMemgraphPortToolLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out := runMemgraphTool(t, dir, MemgraphParams{Action: "remember", Kind: "user", Name: "nextsko", Props: map[string]any{"tone": "direct"}})
	assert.Contains(t, out, "Remembered user")

	runMemgraphTool(t, dir, MemgraphParams{Action: "remember", Kind: "persona", Name: "pragmatic engineer", Props: map[string]any{"traits": []any{"直接", "先验证"}}})
	pref := runMemgraphTool(t, dir, MemgraphParams{Action: "remember", Kind: "preference", Name: "结构化归档", SourceKind: "message", SourceRef: "m1", SourceQuote: "记忆要以文档结构化存储"})
	assert.Contains(t, pref, "Remembered preference")

	// Resolve ids via recall.
	recall := runMemgraphTool(t, dir, MemgraphParams{Action: "recall", Kind: "user"})
	require.Contains(t, recall, "user")
	card := runMemgraphTool(t, dir, MemgraphParams{Action: "persona"})
	assert.Contains(t, card, "AI 人格")
	assert.Contains(t, card, "pragmatic engineer")
	assert.Contains(t, card, "用户画像")

	assert.Contains(t, runMemgraphTool(t, dir, MemgraphParams{Action: "graph"}), "graph LR")
	assert.Contains(t, runMemgraphTool(t, dir, MemgraphParams{Action: "export"}), "Memory graph exported")
}
