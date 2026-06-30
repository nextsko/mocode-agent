package tools_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/tools"
)

func TestErrorResult(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	result, err := tools.ErrorResult(want)
	require.NoError(t, err)
	require.ErrorIs(t, result.Error, want)
}

func TestErrorResultWithMetadata(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	metadata := map[string]any{"key": "value"}
	result, err := tools.ErrorResultWithMetadata(want, metadata)
	require.NoError(t, err)
	require.ErrorIs(t, result.Error, want)
	require.Equal(t, metadata, result.Metadata)
}
