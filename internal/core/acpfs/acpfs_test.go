package acpfs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClient struct{ content map[string]string }

func (f *fakeClient) ReadTextFile(_ context.Context, _ string, path string) (string, error) {
	return f.content[path], nil
}

func (f *fakeClient) WriteTextFile(_ context.Context, _ string, path, content string) error {
	f.content[path] = content
	return nil
}

func TestRegistryReadWriteAndUnregister(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ok, err := Read(ctx, "missing", "/x")
	require.NoError(t, err)
	assert.False(t, ok, "no client means caller falls back to local disk")

	c := &fakeClient{content: map[string]string{"/virtual/a.txt": "hello"}}
	Register("s1", c)
	assert.True(t, Has("s1"))

	content, ok, err := Read(ctx, "s1", "/virtual/a.txt")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "hello", content)

	ok, err = Write(ctx, "s1", "/virtual/b.txt", "world")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "world", c.content["/virtual/b.txt"])

	Unregister("s1")
	assert.False(t, Has("s1"))
}
