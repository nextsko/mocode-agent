package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionSearch_New(t *testing.T) {
	t.Parallel()

	st, err := New(t.TempDir(), nil)
	require.NoError(t, err)
	require.NotNil(t, st.Search())
}
