package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/domain/memory"
)

func TestMemoryStore_PersistAndSearch(t *testing.T) {
	t.Parallel()

	st, err := New(t.TempDir(), nil)
	require.NoError(t, err)

	ms := st.Memories()
	ctx := context.Background()
	err = ms.AddMemory(ctx, "app", "user1", "likes Go programming", nil, memory.KindFact, nil, nil, "")
	require.NoError(t, err)

	results, err := ms.SearchMemories(ctx, "app", "user1", "Go", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, results[0].Memory.Memory, "Go")

	st2, err := New(st.ProjectPath, nil)
	require.NoError(t, err)
	results2, err := st2.Memories().SearchMemories(ctx, "app", "user1", "Go", 10)
	require.NoError(t, err)
	require.Len(t, results2, 1)
}
