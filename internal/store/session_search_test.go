package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/core/config"
)

func TestSessionSearch_New(t *testing.T) {
	t.Parallel()

	cfg := testConfigStore(t)
	st, err := New(t.TempDir(), cfg)
	require.NoError(t, err)
	require.NotNil(t, st.Search())
}

func testConfigStore(t *testing.T) *config.ConfigStore {
	t.Helper()
	cfg, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	return cfg
}
