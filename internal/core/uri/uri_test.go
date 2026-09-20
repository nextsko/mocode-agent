package uri

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsURI(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"mocode://skills/x", " mocode://docs/", "mocode://"} {
		assert.True(t, IsURI(s), "%q", s)
	}
	for _, s := range []string{"", "/tmp/x", "https://example.com", "mocode:/skills/x", "Mocode://x"} {
		assert.False(t, IsURI(s), "%q", s)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        string
		authority string
		path      string
		query     map[string]string
	}{
		{"file", "mocode://skills/jq/SKILL.md", "skills", "jq/SKILL.md", nil},
		{"root listing", "mocode://skills", "skills", "", nil},
		{"trailing slash listing", "mocode://docs/", "docs", "", nil},
		{"nested", "mocode://docs/plans/a/b/01-x.md", "docs", "plans/a/b/01-x.md", nil},
		{"authority case folded", "mocode://SKILLS/x", "skills", "x", nil},
		{"leading slash trimmed", "mocode://docs//x", "docs", "x", nil},
		{"query", "mocode://github/o/r/x.go?ref=main", "github", "o/r/x.go", map[string]string{"ref": "main"}},
		{"query only", "mocode://github?ref=v1", "github", "", map[string]string{"ref": "v1"}},
		{"surrounded by space", "  mocode://docs/a.md  ", "docs", "a.md", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, err := Parse(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.authority, u.Authority)
			assert.Equal(t, tt.path, u.Path)
			for k, v := range tt.query {
				assert.Equal(t, v, u.Query.Get(k))
			}
		})
	}
}

// Parse must be strict. A malformed reference that quietly became a filesystem
// path would surface as "file not found", sending the agent after a path bug
// instead of the typo it actually made.
func TestParse_RejectsMalformedReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{"no authority", "mocode://"},
		{"empty authority with path", "mocode:///x"},
		{"authority with space", "mocode://sk ills/x"},
		{"authority with slash only", "mocode:///"},
		{"not the scheme", "https://example.com"},
		{"missing slashes", "mocode:skills/x"},
		{"parent segment", "mocode://docs/../../etc/passwd"},
		{"dot segment", "mocode://docs/./x"},
		{"backslash traversal", "mocode://docs/..\\..\\etc"},
		{"backslash separator", `mocode://docs\a.md`},
		{"interior empty segment", "mocode://docs/a//b"},
		{"control character", "mocode://docs/a\x00b"},
		{"newline", "mocode://docs/a\nb"},
		{"bad query", "mocode://docs/a?%zz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(tt.in)
			assert.Error(t, err, "Parse(%q) must fail", tt.in)
		})
	}
}

// Traversal is rejected centrally rather than trusted to each provider: the
// same path is resolved inside embedded filesystems and cloned repositories
// where a ".." cannot be validated by the OS.
func TestParse_TraversalIsRejectedNotNormalised(t *testing.T) {
	t.Parallel()

	_, err := Parse("mocode://docs/a/../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}

// Repeated leading/trailing slashes are folded away rather than rejected —
// `mocode://docs//` is the root listing, which is the obvious intent — but an
// empty segment *inside* a path is a typo and is rejected.
func TestParse_SlashLeniency(t *testing.T) {
	t.Parallel()

	u, err := Parse("mocode://docs//")
	require.NoError(t, err)
	assert.True(t, u.IsListing())

	u, err = Parse("mocode://docs///guides//")
	require.NoError(t, err)
	assert.Equal(t, "guides", u.Path)

	_, err = Parse("mocode://docs/a//b")
	require.Error(t, err)
}

func TestURI_String(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"mocode://skills/jq/SKILL.md",
		"mocode://docs/",
		"mocode://github/o/r/x.go?ref=main",
	} {
		u, err := Parse(s)
		require.NoError(t, err)

		got := u.String()
		// Listings canonicalise without the trailing slash; everything else must
		// round-trip exactly, or a listing's links would not resolve.
		if !u.IsListing() {
			assert.Equal(t, s, got)
		}

		again, err := Parse(got)
		require.NoError(t, err)
		assert.Equal(t, u.Authority, again.Authority)
		assert.Equal(t, u.Path, again.Path)
	}
}

func TestURI_IsListingAndBaseAndJoin(t *testing.T) {
	t.Parallel()

	root, err := Parse("mocode://docs")
	require.NoError(t, err)
	assert.True(t, root.IsListing())

	child := root.Join("guides")
	assert.Equal(t, "mocode://docs/guides", child.String())
	assert.False(t, child.IsListing())

	deep := child.Join("01-x.md")
	assert.Equal(t, "mocode://docs/guides/01-x.md", deep.String())

	assert.Equal(t, "mocode://docs/guides", deep.Base().String())
	assert.Equal(t, "mocode://docs", child.Base().String())
	// Going up from the root is a no-op rather than an error: listings render
	// this unconditionally.
	assert.Equal(t, "mocode://docs", root.Base().String())
}

func TestValidAuthoritiesCoversTheNamedConstants(t *testing.T) {
	t.Parallel()

	got := ValidAuthorities()
	for _, want := range []string{
		AuthoritySkills, AuthorityDocs, AuthorityExp, AuthorityGitHub, AuthorityBrowser,
	} {
		assert.Contains(t, got, want)
	}
}
