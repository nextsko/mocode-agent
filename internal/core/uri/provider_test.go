package uri

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRegistry builds a registry with a docs tree and a second, lower
// priority tree, mirroring how skills combine an embedded and a user source.
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()

	builtin := fstest.MapFS{
		"alpha/SKILL.md":          &fstest.MapFile{Data: []byte("---\nname: alpha\ndescription: Use when alpha\n---\n\n# Alpha\n\nbody\n")},
		"beta/SKILL.md":           &fstest.MapFile{Data: []byte("---\nname: beta\ndescription: Use when beta\n---\n\n# Beta\n")},
		"alpha/reference/info.md": &fstest.MapFile{Data: []byte("# Info\n\ntext\n")},
		"notes/plain.md":          &fstest.MapFile{Data: []byte("just a line\nsecond\n")},
		".hidden/secret.md":       &fstest.MapFile{Data: []byte("nope\n")},
	}
	user := fstest.MapFS{
		"alpha/SKILL.md":  &fstest.MapFile{Data: []byte("---\nname: alpha\ndescription: USER OVERRIDE\n---\n\nuser body\n")},
		"custom/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: custom\ndescription: Use when custom\n---\n\nx\n")},
		"alpha/extra.md":  &fstest.MapFile{Data: []byte("extra\n")},
	}

	r := NewRegistry()
	r.Register(NewTreeProvider(AuthoritySkills, "skills summary",
		Tree{Label: "builtin", FS: builtin},
		Tree{Label: "user", FS: user},
	))
	return r
}

func TestRegistry_ResolveFile(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	content, u, err := r.Resolve(context.Background(), "mocode://skills/alpha/SKILL.md")
	require.NoError(t, err)

	assert.Equal(t, AuthoritySkills, u.Authority)
	assert.Equal(t, KindText, content.Kind)
	assert.Contains(t, content.Text, "# Alpha")
	// The first tree wins, so the builtin copy is what a read returns even
	// though a user tree also has the same path.
	assert.Contains(t, content.Source, "builtin")
	assert.Equal(t, "SKILL.md", content.Title)
	assert.Contains(t, content.Description, "Alpha")
}

func TestRegistry_ReadsFromLowerPriorityTreeWhenAbsentAbove(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	content, _, err := r.Resolve(context.Background(), "mocode://skills/custom/SKILL.md")
	require.NoError(t, err)
	assert.Contains(t, content.Text, "Use when custom")
	assert.Contains(t, content.Source, "user")
}

func TestRegistry_ListsRootAcrossTrees(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	content, u, err := r.Resolve(context.Background(), "mocode://skills")
	require.NoError(t, err)

	assert.Equal(t, KindListing, content.Kind)
	assert.True(t, u.IsListing())

	names := entryNames(content)
	// Root entries are the skill directories, merged and deduplicated across
	// trees: `alpha` appears in both but is listed once.
	assert.Equal(t, []string{"alpha", "beta", "custom", "notes"}, names)
	assert.NotContains(t, names, ".hidden", "hidden entries are noise in a listing")
	assert.NotContains(t, names, "plain.md", "the root lists directories, not every nested file")
}

func TestRegistry_ListsSubdirectory(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	content, _, err := r.Resolve(context.Background(), "mocode://skills/alpha")
	require.NoError(t, err)

	assert.Equal(t, KindListing, content.Kind)
	assert.Contains(t, entryNames(content), "extra.md")
	assert.Contains(t, entryNames(content), "reference")
}

// A URI that names a directory must list it: the caller exploring the namespace
// wants to see inside, not be told it is a directory.
func TestRegistry_DirectoryPathListsInsteadOfFailing(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	content, _, err := r.Resolve(context.Background(), "mocode://skills/alpha/reference")
	require.NoError(t, err)
	assert.Equal(t, KindListing, content.Kind)
	assert.Equal(t, []string{"info.md"}, entryNames(content))
}

func TestRegistry_UnknownAuthorityListsTheKnownOnes(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	_, _, err := r.Resolve(context.Background(), "mocode://nope/x")
	require.Error(t, err)

	assert.True(t, errors.Is(err, ErrUnknownAuthority))
	assert.Contains(t, err.Error(), "skills", "the error must name what is available")
}

func TestRegistry_NotFoundNamesEveryTreeSearched(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	_, _, err := r.Resolve(context.Background(), "mocode://skills/ghost/SKILL.md")
	require.Error(t, err)

	assert.True(t, errors.Is(err, ErrNotFound))
	assert.Contains(t, err.Error(), "builtin")
	assert.Contains(t, err.Error(), "user")
}

func TestRegistry_MissingSubdirectory(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	_, _, err := r.Resolve(context.Background(), "mocode://skills/ghost")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

// A non-UTF-8 payload must be refused with a clear message rather than returned
// as mojibake for the model to reason about.
func TestTreeProvider_RejectsNonUTF8(t *testing.T) {
	t.Parallel()

	bad := fstest.MapFS{
		"bin.dat": &fstest.MapFile{Data: []byte{0xff, 0xfe, 0x00, 0x01}},
	}
	p := NewTreeProvider(AuthorityDocs, "docs", Tree{Label: "t", FS: bad})

	_, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs, Path: "bin.dat"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UTF-8")
}

func TestTreeProvider_SummaryAndDescribe(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"a.md": &fstest.MapFile{Data: []byte("# Heading\n\nbody\n")},
	}
	p := NewTreeProvider(AuthorityDocs, "the docs", Tree{Label: "t", FS: fsys})
	assert.Equal(t, AuthorityDocs, p.Authority())
	assert.Equal(t, "the docs", p.Summary())

	// With no custom Describe, a listing falls back to the first heading.
	content, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs})
	require.NoError(t, err)
	require.Len(t, content.Entries, 1)
	assert.Equal(t, "Heading", content.Entries[0].Description)
}

func TestTreeProvider_DescribeHookWins(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte("# Heading\n")}}
	p := NewTreeProvider(AuthorityDocs, "s", Tree{Label: "t", FS: fsys})
	p.Describe = func(string, []byte) string { return "custom gist" }

	content, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs})
	require.NoError(t, err)
	require.Len(t, content.Entries, 1)
	assert.Equal(t, "custom gist", content.Entries[0].Description)
}

// A nil FS means "this root is not configured" and must be skipped silently, so
// callers can pass optional directories without checking first.
func TestNewTreeProvider_SkipsNilTrees(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"a.md": &fstest.MapFile{Data: []byte("x\n")}}
	p := NewTreeProvider(AuthorityDocs, "s", Tree{Label: "missing", FS: nil}, Tree{Label: "real", FS: fsys})

	content, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs, Path: "a.md"})
	require.NoError(t, err)
	assert.Equal(t, "x\n", content.Text)
}

func TestRegistry_DescribeListsNamespaces(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	r.Register(NewTreeProvider(AuthorityDocs, "the docs", Tree{Label: "d", FS: fstest.MapFS{}}))

	out := r.Describe()
	assert.Contains(t, out, "mocode://docs/")
	assert.Contains(t, out, "the docs")
	assert.Contains(t, out, "mocode://skills/")
	assert.Contains(t, out, "skills summary")
}

func TestRegistry_RootListingIncludesEveryAuthority(t *testing.T) {
	t.Parallel()

	r := newTestRegistry(t)
	r.Register(NewTreeProvider(AuthorityDocs, "the docs", Tree{Label: "d", FS: fstest.MapFS{}}))

	content := r.RootListing(context.Background())
	require.Equal(t, KindListing, content.Kind)

	names := entryNames(content)
	assert.Contains(t, names, "docs")
	assert.Contains(t, names, "skills")

	for _, e := range content.Entries {
		assert.True(t, e.Dir, "%s is a namespace, not a file", e.Name)
	}
}

// A provider that fails must not break the whole root listing: the other
// namespaces are still worth showing.
func TestRegistry_RootListingSurvivesAFailingProvider(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(failingProvider{})
	r.Register(NewTreeProvider(AuthorityDocs, "the docs", Tree{Label: "d", FS: fstest.MapFS{}}))

	content := r.RootListing(context.Background())
	require.Equal(t, KindListing, content.Kind)
	require.Len(t, content.Entries, 2)

	for _, e := range content.Entries {
		if e.Name == "broken" {
			assert.Contains(t, e.Description, "unavailable")
		}
	}
}

func TestRegistry_RegisterIgnoresEmptyProvider(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	r.Register(nil)
	assert.Empty(t, r.Authorities())

	_, ok := r.Lookup("anything")
	assert.False(t, ok)
}

func TestRenderListing(t *testing.T) {
	t.Parallel()

	u, err := Parse("mocode://docs")
	require.NoError(t, err)

	out := RenderListing(u, &Content{
		Kind:        KindListing,
		Description: "the docs",
		Entries: []Entry{
			{Name: "guides", URI: "mocode://docs/guides", Dir: true, Description: "how to"},
			{Name: "a.md", URI: "mocode://docs/a.md", Size: 12},
		},
	})

	assert.Contains(t, out, "mocode://docs")
	assert.Contains(t, out, "guides/", "directories are marked with a trailing slash")
	assert.Contains(t, out, "how to")
	assert.Contains(t, out, "12 bytes")

	empty := RenderListing(u, &Content{Kind: KindListing, Entries: nil})
	assert.Contains(t, empty, "empty")
}

func TestFirstLineOf(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"# Title\nbody":     "Title",
		"## Sub\ntext":      "Sub",
		"\n\n\nplain\nmore": "plain",
		// A leading YAML block is skipped, so a skill is described by its
		// heading rather than by "name: x", which says nothing about when to
		// read it.
		"---\nname: x\n---\n# Real title\n":                "Real title",
		"---\nname: x\n---\n":                              "",
		"---\nname: x\ndescription: d\n---\n\nbody line\n": "body line",
		// A fence that is not frontmatter must not swallow the document.
		"# Title\n\n---\n\nbody": "Title",
		"":                       "",
	} {
		assert.Equal(t, want, firstLineOf([]byte(in)), "input %q", in)
	}

	long := firstLineOf([]byte(fmt.Sprintf("# %s\n", string(make([]rune, 300)))))
	assert.LessOrEqual(t, len([]rune(long)), 101)
}

type failingProvider struct{}

func (failingProvider) Authority() string { return AuthorityBrowser }
func (failingProvider) Summary() string   { return "always fails" }
func (failingProvider) Resolve(context.Context, URI) (*Content, error) {
	return nil, errors.New("boom")
}

func entryNames(c *Content) []string {
	out := make([]string, 0, len(c.Entries))
	for _, e := range c.Entries {
		out = append(out, e.Name)
	}
	return out
}

// A missing file must be reported with the provider's own guidance, not the
// raw OS message. os.DirFS wraps the failure in *fs.PathError, so a sentinel
// comparison misses it and the platform-specific text ("The system cannot find
// the file specified" on Windows) escapes to the model instead.
func TestTreeProvider_MissingFileIsOurErrorNotTheOSes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "present.md"), []byte("x\n"), 0o644))

	p := NewTreeProvider(AuthorityDocs, "docs", Tree{Label: "docs", FS: os.DirFS(dir)})
	_, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs, Path: "absent.md"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound, "the sentinel must survive so callers can branch on it")
	assert.Contains(t, err.Error(), "absent.md")
	assert.Contains(t, err.Error(), "looked in docs")
	assert.NotContains(t, err.Error(), "The system cannot find", "the OS message must not leak")
}

// The same wrapping problem would abort a multi-tree search on the first miss
// instead of continuing to the next tree.
func TestTreeProvider_MissInOneTreeStillFindsTheNext(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(second, "only-here.md"), []byte("found\n"), 0o644))

	p := NewTreeProvider(AuthorityDocs, "docs",
		Tree{Label: "first", FS: os.DirFS(first)},
		Tree{Label: "second", FS: os.DirFS(second)},
	)

	content, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs, Path: "only-here.md"})
	require.NoError(t, err)
	assert.Equal(t, "found\n", content.Text)
	assert.Contains(t, content.Source, "second", "the source must name the tree it came from")
}

// A missing subdirectory must fall through to the next tree rather than error.
func TestTreeProvider_MissingSubdirectoryFallsThrough(t *testing.T) {
	t.Parallel()

	first, second := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(second, "guides"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(second, "guides", "a.md"), []byte("x\n"), 0o644))

	p := NewTreeProvider(AuthorityDocs, "docs",
		Tree{Label: "first", FS: os.DirFS(first)},
		Tree{Label: "second", FS: os.DirFS(second)},
	)

	content, err := p.Resolve(context.Background(), URI{Authority: AuthorityDocs, Path: "guides"})
	require.NoError(t, err)
	assert.Equal(t, KindListing, content.Kind)
	assert.Equal(t, []string{"a.md"}, entryNames(content))
}
