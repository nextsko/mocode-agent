package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

func TestURIProvider_AuthorityAndSummary(t *testing.T) {
	t.Parallel()

	p := NewURIProvider()
	assert.Equal(t, uri.AuthoritySkills, p.Authority())
	assert.Contains(t, p.Summary(), "builtin skills ship with mocode")
}

// The builtin mapping must be unchanged from the prefix check this replaced:
// mocode://skills/<rel> still resolves to the embedded builtin/<rel>.
func TestURIProvider_ReadsBuiltinSkill(t *testing.T) {
	t.Parallel()

	p := NewURIProvider()
	content, err := p.Resolve(context.Background(), uri.URI{
		Authority: uri.AuthoritySkills, Path: "mocode-config/SKILL.md",
	})
	require.NoError(t, err)

	assert.Equal(t, uri.KindText, content.Kind)
	assert.Contains(t, content.Text, "name: mocode-config")
	assert.Equal(t, "SKILL.md", content.Title)
	// The gist comes from the skill's own description, which is what decides
	// whether the model should open the file at all.
	assert.NotEmpty(t, content.Description)
	assert.NotContains(t, content.Description, "name:")
}

func TestURIProvider_ListsBuiltinSkills(t *testing.T) {
	t.Parallel()

	p := NewURIProvider()
	content, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthoritySkills})
	require.NoError(t, err)

	require.Equal(t, uri.KindListing, content.Kind)
	require.NotEmpty(t, content.Entries)

	names := map[string]bool{}
	for _, e := range content.Entries {
		names[e.Name] = true
		assert.True(t, e.Dir, "%s should list as a directory", e.Name)
	}
	for _, want := range []string{"mocode-config", "jq", "docs-rulebook", "writing-skills"} {
		assert.True(t, names[want], "builtin skill %q missing from the listing", want)
	}
}

func TestURIProvider_ReadsNestedBuiltinReference(t *testing.T) {
	t.Parallel()

	p := NewURIProvider()
	content, err := p.Resolve(context.Background(), uri.URI{
		Authority: uri.AuthoritySkills, Path: "playwright-cli/references/tracing.md",
	})
	require.NoError(t, err)
	assert.Contains(t, content.Text, "trac")
}

// A user skills directory is searched after the embedded tree, so a user skill
// is reachable without shadowing the shipped one.
func TestURIProvider_ReadsUserSkill(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "my-skill"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "my-skill", SkillFileName),
		[]byte("---\nname: my-skill\ndescription: Use when testing\n---\n\nbody\n"), 0o644))

	p := NewURIProvider(dir)
	content, err := p.Resolve(context.Background(), uri.URI{
		Authority: uri.AuthoritySkills, Path: "my-skill/SKILL.md",
	})
	require.NoError(t, err)
	assert.Contains(t, content.Text, "Use when testing")

	// Both trees still list.
	root, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthoritySkills})
	require.NoError(t, err)
	var found bool
	for _, e := range root.Entries {
		if e.Name == "my-skill" {
			found = true
		}
	}
	assert.True(t, found, "a user skill must appear in the listing")
}

// A directory that does not exist is normal (no user skills installed) and must
// not break the namespace.
func TestURIProvider_IgnoresMissingUserDir(t *testing.T) {
	t.Parallel()

	p := NewURIProvider(filepath.Join(t.TempDir(), "nope"), "")
	_, err := p.Resolve(context.Background(), uri.URI{Authority: uri.AuthoritySkills})
	require.NoError(t, err)
}

func TestResolveURI_Convenience(t *testing.T) {
	t.Parallel()

	content, err := ResolveURI(context.Background(), "mocode://skills/jq/SKILL.md")
	require.NoError(t, err)
	assert.Contains(t, content.Text, "jq")
}

// ─── containment ─────────────────────────────────────────────────────────────

// SkillsDirIn is what decides that a path may be read without a permission
// prompt. The previous implementation lived in the View tool and tested the
// configured directories unexpanded, so a `~/.config/...` entry never matched
// and the exemption silently did not apply.
func TestSkillsDirIn(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillFile := filepath.Join(root, "jq", SkillFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(skillFile), 0o755))
	require.NoError(t, os.WriteFile(skillFile, []byte("x"), 0o644))

	outside := filepath.Join(t.TempDir(), "other.md")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o644))

	t.Run("inside", func(t *testing.T) {
		t.Parallel()
		assert.True(t, SkillsDirIn(skillFile, []string{root}))
	})

	t.Run("the directory itself", func(t *testing.T) {
		t.Parallel()
		assert.True(t, SkillsDirIn(root, []string{root}))
	})

	t.Run("outside", func(t *testing.T) {
		t.Parallel()
		assert.False(t, SkillsDirIn(outside, []string{root}))
	})

	t.Run("no directories configured", func(t *testing.T) {
		t.Parallel()
		assert.False(t, SkillsDirIn(skillFile, nil))
	})

	t.Run("empty entries are skipped", func(t *testing.T) {
		t.Parallel()
		assert.True(t, SkillsDirIn(skillFile, []string{"", "  ", root}))
	})

	t.Run("a sibling with a shared prefix is not inside", func(t *testing.T) {
		t.Parallel()
		sibling := root + "-extra"
		require.NoError(t, os.MkdirAll(sibling, 0o755))
		f := filepath.Join(sibling, "x.md")
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

		// The old implementation compared with a string prefix check on the
		// relative path, which "/root-extra" would have satisfied for "/root".
		assert.False(t, SkillsDirIn(f, []string{root}))
	})
}

// A non-existent file still has to be placed correctly: the check runs before
// the read, so a missing file inside a skills directory must still count.
func TestSkillsDirIn_NonExistentFileInsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	assert.True(t, SkillsDirIn(filepath.Join(root, "not-yet-created.md"), []string{root}))
}

func TestExpandDirs(t *testing.T) {
	t.Parallel()

	got := ExpandDirs([]string{"~/skills", "  ", "$HOME/x", "/abs"}, func(p string) string {
		return strings.ReplaceAll(p, "~", "/home/dev")
	})
	assert.Equal(t, []string{"/home/dev/skills", "$HOME/x", "/abs"}, got)

	assert.Empty(t, ExpandDirs(nil, nil))
}

func TestURIRegistryIncludesSkillsNamespace(t *testing.T) {
	t.Parallel()

	r := URIRegistry()
	_, ok := r.Lookup(uri.AuthoritySkills)
	assert.True(t, ok)
}
