package store

import (
	"context"
	"testing"

	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/knowledge/memory"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_PersistAndSearch(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)

	st, err := New(t.TempDir(), cfg)
	require.NoError(t, err)

	ms := st.Memories()
	ctx := context.Background()
	err = ms.AddMemory(ctx, "app", "user1", "likes Go programming", nil, memory.KindFact, nil, nil, "")
	require.NoError(t, err)

	results, err := ms.SearchMemories(ctx, "app", "user1", "Go", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, results[0].Memory.Memory, "Go")

	st2, err := New(st.ProjectPath, cfg)
	require.NoError(t, err)
	results2, err := st2.Memories().SearchMemories(ctx, "app", "user1", "Go", 10)
	require.NoError(t, err)
	require.Len(t, results2, 1)
}
