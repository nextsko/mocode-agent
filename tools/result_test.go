package tools

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestErrorResult(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	result, err := ErrorResult(want)
	require.NoError(t, err)
	require.ErrorIs(t, result.Error, want)
}

func TestErrorResultWithMetadata(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	metadata := map[string]any{"key": "value"}
	result, err := ErrorResultWithMetadata(want, metadata)
	require.NoError(t, err)
	require.ErrorIs(t, result.Error, want)
	require.Equal(t, metadata, result.Metadata)
}
