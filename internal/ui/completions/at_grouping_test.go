package completions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

func TestSetAtItems_GroupsByCategoryHeader(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()

	c := New(sty.Dialog.NormalItem, sty.Dialog.SelectedItem, sty.Dialog.SelectedItem)
	c.SetAtItems([]AtCompletionValue{
		{Kind: AtCompletionCategory, Token: "@mcp:", Category: "mcp", IsCategory: true},
		{Kind: AtCompletionMCP, Token: "@mcp:fetch", Desc: "fetch server"},
		{Kind: AtCompletionCategory, Token: "@file:", Category: "file", IsCategory: true},
		{Kind: AtCompletionFile, Token: "@file:main.go", Desc: "main.go"},
	}, &sty, 60)

	// With a bare query (no filter), the list must contain group headers
	// ("MCP", "Files") preceding their items, not the raw category markers.
	require.NotEmpty(t, c.allItems)

	// First item is the MCP header, not the old @mcp: marker.
	first, ok := c.allItems[0].(*SlashGroupItem)
	require.True(t, ok, "first @ item should be a group header, got %T", c.allItems[0])
	require.Contains(t, first.Text(), "MCP")

	// A Files header must appear before the file item.
	var sawFilesHeader bool
	for _, it := range c.allItems {
		if g, ok := it.(*SlashGroupItem); ok && contains(g.Text(), "FILES") {
			sawFilesHeader = true
		}
	}
	require.True(t, sawFilesHeader, "expected a Files section header")
}

func TestSectionLabel(t *testing.T) {
	t.Parallel()
	require.Equal(t, "MCP", sectionLabel("mcp", "@mcp:"))
	require.Equal(t, "Skills", sectionLabel("skill", "@skill:"))
	require.Equal(t, "Files", sectionLabel("file", "@file:"))
	require.Equal(t, "Directories", sectionLabel("dir", "@dir:"))
	require.Equal(t, "Workflows", sectionLabel("workflow", "@workflow:"))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
