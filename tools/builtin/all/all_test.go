package all_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
	_ "github.com/package-register/mocode/tools/builtin/all"
)

func TestInitialBuiltinsRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"job_input", "job_kill", "job_output"} {
		tool, ok := tools.Get(name)
		require.True(t, ok, name)
		require.Equal(t, name, tool.Name())
	}
}
