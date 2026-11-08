package plan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreLifecycleAndSubscription(t *testing.T) {
	t.Parallel()
	s := New()
	ch, cancel := s.Subscribe("s1")
	defer cancel()

	select {
	case items := <-ch:
		assert.Empty(t, items)
	case <-time.After(time.Second):
		t.Fatal("initial snapshot not delivered")
	}

	item := s.Add("s1", Item{Content: "first"})
	require.NotEmpty(t, item.ID)
	assert.Equal(t, StatusPending, item.Status)

	select {
	case items := <-ch:
		require.Len(t, items, 1)
		assert.Equal(t, "first", items[0].Content)
	case <-time.After(time.Second):
		t.Fatal("add not published")
	}

	require.True(t, s.Update("s1", item.ID, func(it *Item) { it.Status = StatusCompleted }))
	assert.Equal(t, StatusCompleted, s.Get("s1")[0].Status)
	require.True(t, s.Remove("s1", item.ID))
	assert.Empty(t, s.Get("s1"))
	s.Clear("s1")
}
