package roundtable

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStore_SaveAndLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)

	rt, err := New(Config{
		ID:    "rt-123",
		Topic: "design retry backoff",
		Participants: []Participant{
			{Name: "mod", IsModerator: true},
			{Name: "architect"},
		},
		Budget: Budget{MaxTurns: 10},
	})
	require.NoError(t, err)

	rt.AddStatement(Statement{Speaker: "mod", Kind: StatementChat, Content: "hello"})
	rt.AddStatement(Statement{Speaker: "architect", Kind: StatementChat, Content: "hi"})
	rt.applyUsage("mod", Usage{TotalTokens: 42})

	ctx := context.Background()
	require.NoError(t, store.Save(ctx, rt))

	loaded, err := store.Load(ctx, rt.Config.ID)
	require.NoError(t, err)
	require.Equal(t, rt.Config.ID, loaded.Config.ID)
	require.Equal(t, rt.Config.Topic, loaded.Config.Topic)
	require.Equal(t, rt.Phase, loaded.Phase)
	require.Len(t, loaded.Transcript, 2)
	require.Equal(t, 42, loaded.Usage.TotalTokens)
	require.Equal(t, 42, loaded.Usage.ByRole["mod"].TotalTokens)
}

func TestStore_ListAndDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	ctx := context.Background()

	rt1, _ := New(Config{ID: "a", Topic: "x", Participants: []Participant{{Name: "m", IsModerator: true}}})
	rt2, _ := New(Config{ID: "b", Topic: "y", Participants: []Participant{{Name: "m", IsModerator: true}}})
	require.NoError(t, store.Save(ctx, rt1))
	require.NoError(t, store.Save(ctx, rt2))

	ids, err := store.ListSessions(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "b"}, ids)

	require.NoError(t, store.Delete(ctx, "a"))
	ids, err = store.ListSessions(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"b"}, ids)
}

func TestStore_LoadMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	_, err := store.Load(context.Background(), "missing")
	require.Error(t, err)
}
