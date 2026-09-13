package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCatalogUpToDate fails when the committed catalog is stale. Regenerate
// with: go run ./internal/core/skills/gen
func TestCatalogUpToDate(t *testing.T) {
	t.Parallel()

	const root = "../../../.."
	want, err := render(root)
	require.NoError(t, err)

	got, err := os.ReadFile(root + "/docs/skills-and-roles/README.md")
	require.NoError(t, err)

	require.Equal(t, want, string(got),
		"docs/skills-and-roles/README.md is stale — run: go run ./internal/core/skills/gen")
}
