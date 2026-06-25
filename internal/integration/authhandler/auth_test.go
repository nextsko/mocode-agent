package authhandler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthHandlersExcludeMiniMax(t *testing.T) {
	t.Parallel()

	for _, id := range IDs() {
		require.NotEqual(t, "minimax", id, "MiniMax auth handler was removed")
	}
}
