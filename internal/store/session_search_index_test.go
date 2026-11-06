package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// newSearchTestStore builds an isolated store whose data directory lives in a
// temporary directory.
func newSearchTestStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("MOCODE_GLOBAL_DATA", t.TempDir())
	st, err := New(t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })
	return st
}

// createMessage writes one user message with the given text.
func createMessage(t *testing.T, st *Store, sessionID, text string) message.Message {
	t.Helper()
	msg, err := st.Messages().Create(t.Context(), sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: text}},
	})
	require.NoError(t, err)
	return msg
}

func TestSessionSearchIndexesNewMessages(t *testing.T) {
	st := newSearchTestStore(t)
	sess, err := st.Sessions().Create(t.Context(), "test")
	require.NoError(t, err)

	createMessage(t, st, sess.ID, "the quick brown fox")

	resp, err := st.Search().Search(t.Context(), "quick", 10)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
	require.Contains(t, resp.Results[0].Content, "quick brown fox")
}

func TestSessionSearchFollowsUpdates(t *testing.T) {
	st := newSearchTestStore(t)
	sess, err := st.Sessions().Create(t.Context(), "test")
	require.NoError(t, err)

	msg := createMessage(t, st, sess.ID, "original wording")
	msg.Parts = []message.ContentPart{message.TextContent{Text: "replacement wording"}}
	require.NoError(t, st.Messages().Update(t.Context(), msg))

	resp, err := st.Search().Search(t.Context(), "original", 10)
	require.NoError(t, err)
	require.Zero(t, resp.Total)

	resp, err = st.Search().Search(t.Context(), "replacement", 10)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)
}

func TestSessionSearchFollowsDeletes(t *testing.T) {
	st := newSearchTestStore(t)
	sess, err := st.Sessions().Create(t.Context(), "test")
	require.NoError(t, err)

	msg := createMessage(t, st, sess.ID, "ephemeral content")
	resp, err := st.Search().Search(t.Context(), "ephemeral", 10)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)

	require.NoError(t, st.Messages().Delete(t.Context(), msg.ID))

	resp, err = st.Search().Search(t.Context(), "ephemeral", 10)
	require.NoError(t, err)
	require.Zero(t, resp.Total)
}

func TestSessionSearchFollowsTruncation(t *testing.T) {
	st := newSearchTestStore(t)
	sess, err := st.Sessions().Create(t.Context(), "test")
	require.NoError(t, err)

	createMessage(t, st, sess.ID, "keep this one")
	createMessage(t, st, sess.ID, "drop this one")

	require.NoError(t, st.Messages().DeleteAfter(t.Context(), sess.ID, 0))

	resp, err := st.Search().Search(t.Context(), "keep", 10)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Total)

	resp, err = st.Search().Search(t.Context(), "drop", 10)
	require.NoError(t, err)
	require.Zero(t, resp.Total)
}

func TestSessionSearchDropsWholeSession(t *testing.T) {
	st := newSearchTestStore(t)
	sess, err := st.Sessions().Create(t.Context(), "test")
	require.NoError(t, err)
	createMessage(t, st, sess.ID, "session scoped content")

	require.NoError(t, st.Messages().DeleteSessionMessages(t.Context(), sess.ID))

	resp, err := st.Search().Search(t.Context(), "session scoped", 10)
	require.NoError(t, err)
	require.Zero(t, resp.Total)
}
