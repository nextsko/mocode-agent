package all_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	_ "github.com/package-register/mocode/tools/plugins/all"
)

func TestInitialPluginsRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"glob", "grep", "think", "todos", "web_fetch", "web_search"} {
		tool, ok := tools.Get(name)
		require.True(t, ok, name)
		require.Equal(t, name, tool.Name())
	}
}
