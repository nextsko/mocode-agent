package dialog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKnowledgeCommandsWiring verifies the palette entries and the prompt each
// one sends; it exercises the same construction path the TUI uses.
func TestKnowledgeCommandsWiring(t *testing.T) {
	t.Parallel()
	items := knowledgeCommands(nil)
	require.Len(t, items, 4)
	want := map[string]string{
		"flywheel_report":    "flywheel",
		"flywheel_context":   "flywheel",
		"memgraph_persona":   "memgraph",
		"memgraph_conflicts": "memgraph",
	}
	for _, it := range items {
		tool, ok := want[it.id]
		require.True(t, ok, "unexpected command id %q", it.id)
		action, ok := it.action.(ActionRunCustomCommand)
		require.True(t, ok, "%s must send a prompt", it.id)
		assert.Contains(t, action.Content, tool)
		assert.NotEmpty(t, it.Label(), "label for %s", it.id)
		assert.True(t, strings.HasPrefix(it.Label(), "/"), "slash label for %s", it.id)
	}
}
