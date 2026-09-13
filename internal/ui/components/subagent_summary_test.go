package components

import (
	"strings"
	"testing"

	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestRenderSubagentSummary(t *testing.T) {
	sty := &styles.Styles{}

	// No sub-agents → renders nothing (editor stays clean).
	require.Empty(t, RenderSubagentSummary(sty, SubagentCounts{}, 80))

	out := RenderSubagentSummary(sty, SubagentCounts{Running: 2, Done: 5}, 60)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3, "box is exactly three rows")
	require.Contains(t, lines[0], "subagents", "top border carries the label")
	require.Contains(t, lines[1], "2 running", "running count visible")
	require.Contains(t, lines[1], "5 done", "done count visible")
	require.Contains(t, lines[1], "ctrl+g", "open hint visible")

	// Running-only variant omits the done part (0 done is not shown).
	out = RenderSubagentSummary(sty, SubagentCounts{Running: 1}, 60)
	require.Contains(t, out, "1 running")
	require.NotContains(t, out, "done", "zero-count done part stays hidden")

	// Narrow width must not panic and stays three rows.
	out = RenderSubagentSummary(sty, SubagentCounts{Running: 3, Done: 9}, 12)
	require.Len(t, strings.Split(strings.TrimSpace(out), "\n"), 3)
}
