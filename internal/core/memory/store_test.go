package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clock returns a controllable clock.
func clock(t time.Time) func() time.Time {
	cur := t
	return func() time.Time { return cur }
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir()).WithClock(clock(fixedTime()))
}

func write(t *testing.T, s *Store, scope Scope, session, title, body string, tags ...string) *Entry {
	t.Helper()
	e := &Entry{
		Title:   title,
		Body:    body,
		Scope:   scope,
		Kind:    KindNote,
		Session: session,
		Tags:    tags,
	}
	res, err := s.Write(context.Background(), e)
	require.NoError(t, err)
	assert.NotEmpty(t, res.RelPath)
	return e
}

// ─── store ───────────────────────────────────────────────────────────────────

func TestStore_WriteLaysOutTiers(t *testing.T) {
	t.Parallel()

	s := newStore(t)

	se := write(t, s, ScopeSession, testSession, "A session note", "body", "go")
	ge := write(t, s, ScopeGlobal, testSession, "A durable note", "body")

	assert.True(t, strings.HasPrefix(se.RelPath, "sessions/1a2b3c4d/"))
	assert.True(t, strings.HasPrefix(ge.RelPath, "global/"))

	// The file must actually exist at the reported path.
	assert.FileExists(t, se.Path)
	assert.FileExists(t, ge.Path)

	// And be readable back through the store.
	got, err := s.Read(se.RelPath)
	require.NoError(t, err)
	assert.Equal(t, "A session note", got.Title)
}

func TestStore_WriteRequiresSessionForSessionScope(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	_, err := s.Write(context.Background(), &Entry{
		Title: "no session", Body: "x", Scope: ScopeSession, Kind: KindNote,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntry)
}

func TestStore_WriteRefusesToEscapeTheRoot(t *testing.T) {
	t.Parallel()

	s := newStore(t)

	for _, bad := range []string{
		"../../etc/passwd",
		"/etc/passwd",
		"global/../../outside.md",
		`global\windows-style.md`,
		"global/./x.md",
		"",
	} {
		_, err := s.Read(bad)
		require.Error(t, err, "Read(%q) must be refused", bad)
	}
}

func TestStore_ReadMissing(t *testing.T) {
	t.Parallel()

	_, err := newStore(t).Read("global/nope.md")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

// The tree is user-editable, so one bad file must not make all memory
// unreadable.
func TestStore_ListSkipsUnreadableFiles(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	write(t, s, ScopeGlobal, "", "Good entry", "body")

	// A hand-written file with nothing usable in it.
	require.NoError(t, os.WriteFile(filepath.Join(s.Root(), GlobalDir, "broken.md"), []byte("   \n"), 0o644))
	// A non-markdown file that should simply be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(s.Root(), GlobalDir, "notes.txt"), []byte("ignore me"), 0o644))

	entries, err := s.List(context.Background(), ListOptions{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "Good entry", entries[0].Title)
}

func TestStore_ListFiltersAndOrders(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	// Three entries at distinct times so ordering is unambiguous.
	times := []time.Time{fixedTime(), fixedTime().Add(-2 * time.Hour), fixedTime().Add(-time.Hour)}
	for i, ts := range times {
		e := &Entry{
			Title: []string{"newest", "oldest", "middle"}[i],
			Body:  "b", Scope: ScopeGlobal, Kind: KindNote, Date: ts,
		}
		_, err := s.Write(ctx, e)
		require.NoError(t, err)
	}
	write(t, s, ScopeSession, testSession, "session one", "b")
	write(t, s, ScopeSession, "99998888-0000-0000-0000-000000000000", "session two", "b")

	all, err := s.List(ctx, ListOptions{})
	require.NoError(t, err)
	require.Len(t, all, 5)
	assert.Equal(t, "newest", all[0].Title, "listings are newest first")

	globals, err := s.List(ctx, ListOptions{Scope: ScopeGlobal})
	require.NoError(t, err)
	require.Len(t, globals, 3)

	mine, err := s.List(ctx, ListOptions{Scope: ScopeSession, Session: testSession})
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "session one", mine[0].Title)

	limited, err := s.List(ctx, ListOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, limited, 2)
}

func TestStore_ListRejectsBadScope(t *testing.T) {
	t.Parallel()

	_, err := newStore(t).List(context.Background(), ListOptions{Scope: "nope"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntry)
}

func TestStore_SearchRanksByField(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	write(t, s, ScopeGlobal, "", "Widget calibration", "unrelated body")
	write(t, s, ScopeGlobal, "", "Unrelated title", "the body mentions widget once")
	write(t, s, ScopeGlobal, "", "Another unrelated", "body", "widget")

	matches, err := s.Search(ctx, "widget", ListOptions{})
	require.NoError(t, err)
	require.Len(t, matches, 3)

	assert.Equal(t, "title", matches[0].Where)
	assert.Equal(t, "tag", matches[1].Where)
	assert.Equal(t, "body", matches[2].Where)
	assert.Contains(t, matches[2].Snippet, "widget")
}

func TestStore_SearchIsCaseInsensitiveAndScoped(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	write(t, s, ScopeGlobal, "", "Postgres tuning", "shared_buffers")
	write(t, s, ScopeSession, testSession, "Postgres tuning", "in a session")

	all, err := s.Search(ctx, "POSTGRES", ListOptions{})
	require.NoError(t, err)
	assert.Len(t, all, 2)

	globals, err := s.Search(ctx, "postgres", ListOptions{Scope: ScopeGlobal})
	require.NoError(t, err)
	assert.Len(t, globals, 1)
}

func TestStore_SearchRequiresAQuery(t *testing.T) {
	t.Parallel()

	_, err := newStore(t).Search(context.Background(), "  ", ListOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntry)
}

// Forgetting must never destroy: memory an agent is afraid to lose is memory it
// will not write.
func TestStore_ForgetArchivesRatherThanDeletes(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	e := write(t, s, ScopeGlobal, "", "Temporary note", "body")

	archived, err := s.Forget(context.Background(), e.RelPath)
	require.NoError(t, err)

	assert.False(t, fileExists(e.Path), "the original must be gone")
	archivedPath := filepath.Join(s.Root(), filepath.FromSlash(archived))
	assert.FileExists(t, archivedPath, "the content must still exist in the archive")
	assert.True(t, strings.HasPrefix(archived, ArchiveDir+"/"))
	assert.NotContains(t, archived, GlobalDir, "the archive sits outside the live tiers")

	entries, err := s.List(context.Background(), ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, entries, "archived entries must not appear in listings")
}

func TestStore_ForgetMissing(t *testing.T) {
	t.Parallel()

	_, err := newStore(t).Forget(context.Background(), "global/nope.md")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ─── index ───────────────────────────────────────────────────────────────────

// The explicit requirement: the global index must record where session memory
// lives, so knowledge is findable from a different session.
func TestIndex_RecordsSessionMemoryLocations(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	write(t, s, ScopeSession, testSession, "From session one", "body")
	write(t, s, ScopeSession, "99998888-0000-0000-0000-000000000000", "From session two", "body")
	write(t, s, ScopeGlobal, testSession, "Durable lesson", "body")

	idx, err := NewFileIndexer(s).Rebuild(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, idx.Total())
	assert.Equal(t, 1, len(idx.Global))
	assert.Equal(t, 2, idx.SessionEntryCount())
	require.Len(t, idx.Sessions, 2)

	bySession := map[string]SessionSummary{}
	for _, sum := range idx.Sessions {
		bySession[sum.Session] = sum
	}
	require.Contains(t, bySession, "1a2b3c4d")
	assert.Equal(t, "sessions/1a2b3c4d", bySession["1a2b3c4d"].Dir)
	assert.Len(t, bySession["1a2b3c4d"].Entries, 1)

	document := string(NewFileIndexer(s).Render(idx))
	assert.Contains(t, document, "sessions/1a2b3c4d/", "the index must state where session memory lives")
	assert.Contains(t, document, "sessions/99998888/")
	assert.Contains(t, document, "From session one")
	assert.Contains(t, document, "Durable lesson")
}

// A rebuild over unchanged content must produce identical bytes, or the index
// is un-diffable and any test of it proves nothing.
func TestIndex_RebuildIsDeterministic(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	for _, title := range []string{"zeta", "alpha", "mu"} {
		write(t, s, ScopeGlobal, "", title, "body")
	}
	write(t, s, ScopeSession, testSession, "s1", "body")

	indexer := NewFileIndexer(s)

	first, err := indexer.Rebuild(ctx)
	require.NoError(t, err)
	a := indexer.Render(first)

	second, err := indexer.Rebuild(ctx)
	require.NoError(t, err)
	b := indexer.Render(second)

	assert.Equal(t, string(a), string(b))
}

func TestIndex_EmptyTreeRendersUsably(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	indexer := NewFileIndexer(s)
	ctx := context.Background()

	idx, err := indexer.Rebuild(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, idx.Total())
	assert.Empty(t, idx.Sessions)
	require.NotNil(t, idx.Sessions)

	document := string(indexer.Render(idx))
	assert.Contains(t, document, "Memory Index")
	assert.Contains(t, document, "None yet")
	assert.Contains(t, document, "No session memory yet")
}

func TestIndex_Find(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	e := write(t, s, ScopeGlobal, "", "Findable", "body")

	idx, err := NewFileIndexer(s).Rebuild(context.Background())
	require.NoError(t, err)

	got, ok := idx.Find(e.RelPath)
	require.True(t, ok)
	assert.Equal(t, "Findable", got.Title)

	_, ok = idx.Find("global/nope.md")
	assert.False(t, ok)
}

// UpdateIndex is what makes the index self-maintaining rather than something a
// caller has to remember to run.
func TestUpdateIndex_WritesAndReflectsTheTree(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	write(t, s, ScopeSession, testSession, "Captured during the session", "body")

	idx, err := UpdateIndex(ctx, s, NewFileIndexer(s))
	require.NoError(t, err)
	assert.Equal(t, 1, idx.Total())

	onDisk, ok, err := LoadIndex(s.Root())
	require.NoError(t, err)
	require.True(t, ok, "the index document must be written")
	assert.Contains(t, string(onDisk), "Captured during the session")
	assert.Contains(t, string(onDisk), "sessions/1a2b3c4d/")
}

func TestLoadIndex_AbsentIsNotAnError(t *testing.T) {
	t.Parallel()

	data, ok, err := LoadIndex(t.TempDir())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, data)
}

func TestDigest_IsBoundedAndActionable(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	write(t, s, ScopeGlobal, testSession, "Global one", "body")
	write(t, s, ScopeSession, testSession, "Session one", "body")

	idx, err := NewFileIndexer(s).Rebuild(ctx)
	require.NoError(t, err)

	digest := Digest(idx, 4000)
	assert.Contains(t, digest, "2 memory entr(ies)")
	assert.Contains(t, digest, "Global one")
	assert.Contains(t, digest, "sessions/1a2b3c4d/")
	assert.Contains(t, digest, "mocode://memory/")

	// The bound must hold with many entries, and the truncation must be stated.
	for i := range 200 {
		write(t, s, ScopeGlobal, "", "Entry number "+string(rune('a'+i%26))+strings.Repeat("x", 40), "body")
	}
	big, err := NewFileIndexer(s).Rebuild(ctx)
	require.NoError(t, err)

	clamped := Digest(big, 1000)
	assert.LessOrEqual(t, len(clamped), 1100)
	assert.Contains(t, clamped, "truncated")
}

func TestDigest_EmptyIsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, Digest(&Index{}, 4000))
	assert.Empty(t, Digest(nil, 4000))
}

func TestResolveRoot(t *testing.T) {
	t.Parallel()

	expand := func(s string) string { return strings.ReplaceAll(s, "~", "/home/dev") }

	assert.Equal(t, "/custom/memory", ResolveRoot("/custom/memory", "/data", expand))
	assert.Equal(t, "/home/dev/memory", ResolveRoot("~/memory", "/data", expand))
	assert.Equal(t, "/data/memories", ResolveRoot("", "/data", expand))
	// A whitespace-only setting is treated as unset rather than as a directory
	// named "   ".
	assert.Equal(t, "/data/memories", ResolveRoot("   ", "/data", expand))
}

// ─── extraction ──────────────────────────────────────────────────────────────

// The closed loop the whole design turns on: knowledge written in one session
// becomes durable and visible from anywhere.
func TestExtract_PromotesSessionMemoryToGlobal(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	src := write(t, s, ScopeSession, testSession, "Worth keeping", "the detail", "go")

	res, err := Extract(ctx, s, src.RelPath, ExtractOptions{})
	require.NoError(t, err)
	require.True(t, res.Created)

	assert.True(t, strings.HasPrefix(res.RelPath, GlobalDir+"/"))
	assert.Equal(t, src.RelPath, res.Entry.PromotedFrom, "provenance must record where it came from")

	// The session keeps its own record: extraction copies.
	assert.FileExists(t, src.Path)
	stillThere, err := s.Read(src.RelPath)
	require.NoError(t, err)
	assert.Equal(t, "Worth keeping", stillThere.Title)

	// And the global copy is readable.
	promoted, err := s.Read(res.RelPath)
	require.NoError(t, err)
	assert.Equal(t, ScopeGlobal, promoted.Scope)
	assert.Equal(t, "the detail", promoted.Body)
}

// Re-running extraction must not mint duplicates, or the durable tier fills with
// near-identical entries every time the agent reconsiders.
func TestExtract_IsIdempotent(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	src := write(t, s, ScopeSession, testSession, "Stable", "body")

	first, err := Extract(ctx, s, src.RelPath, ExtractOptions{})
	require.NoError(t, err)
	require.True(t, first.Created)

	second, err := Extract(ctx, s, src.RelPath, ExtractOptions{})
	require.NoError(t, err)
	assert.False(t, second.Created, "a repeat extraction must be a no-op")
	assert.Equal(t, first.RelPath, second.RelPath)

	globals, err := s.List(ctx, ListOptions{Scope: ScopeGlobal})
	require.NoError(t, err)
	assert.Len(t, globals, 1, "exactly one global entry, not two")
}

func TestExtract_OverridesTitleBodyKindAndTags(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	src := write(t, s, ScopeSession, testSession, "Raw session note", "a long rambling body", "raw")

	res, err := Extract(ctx, s, src.RelPath, ExtractOptions{
		Title: "Distilled lesson",
		Body:  "the short version",
		Kind:  KindLesson,
		Tags:  []string{"distilled"},
	})
	require.NoError(t, err)

	assert.Equal(t, "Distilled lesson", res.Entry.Title)
	assert.Equal(t, "the short version", res.Entry.Body)
	assert.Equal(t, KindLesson, res.Entry.Kind)
	assert.ElementsMatch(t, []string{"raw", "distilled"}, res.Entry.Tags)
}

func TestExtract_RefusesAnEntryAlreadyGlobal(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	e := write(t, s, ScopeGlobal, "", "Already durable", "body")

	_, err := Extract(context.Background(), s, e.RelPath, ExtractOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntry)
}

func TestExtract_MissingSource(t *testing.T) {
	t.Parallel()

	_, err := Extract(context.Background(), newStore(t), "sessions/x/nope.md", ExtractOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestExtractAll_PromotesAWholeSession(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	write(t, s, ScopeSession, testSession, "One", "b")
	write(t, s, ScopeSession, testSession, "Two", "b")
	write(t, s, ScopeSession, "99998888-0000-0000-0000-000000000000", "Other session", "b")

	results, err := ExtractAll(ctx, s, testSession, ExtractOptions{})
	require.NoError(t, err)
	assert.Len(t, results, 2, "only this session's entries")

	globals, err := s.List(ctx, ListOptions{Scope: ScopeGlobal})
	require.NoError(t, err)
	assert.Len(t, globals, 2)
}

func TestExtractAll_EmptySession(t *testing.T) {
	t.Parallel()

	_, err := ExtractAll(context.Background(), newStore(t), testSession, ExtractOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

// ─── the cross-session closed loop ───────────────────────────────────────────

// This is the end-to-end guarantee: a second session, with its own store
// instance over the same root, sees what the first one wrote — through the
// index and through search — and can promote it.
func TestClosedLoop_SecondSessionSeesFirstSessionsMemory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ctx := context.Background()

	sessionOne := "11111111-1111-1111-1111-111111111111"
	sessionTwo := "22222222-2222-2222-2222-222222222222"
	_ = sessionTwo

	// ── Session one writes, and the index is refreshed as part of writing.
	storeOne := NewStore(root).WithClock(clock(fixedTime()))
	write(t, storeOne, ScopeSession, sessionOne, "Session one learned something", "the detail", "go")
	_, err := UpdateIndex(ctx, storeOne, NewFileIndexer(storeOne))
	require.NoError(t, err)

	// ── Session two: a *separate* store over the same root.
	storeTwo := NewStore(root).WithClock(clock(fixedTime().Add(time.Hour)))

	// It can see the other session's entries.
	all, err := storeTwo.List(ctx, ListOptions{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "Session one learned something", all[0].Title)

	// It can find them by search.
	matches, err := storeTwo.Search(ctx, "learned", ListOptions{})
	require.NoError(t, err)
	require.Len(t, matches, 1)

	// The index on disk names where that memory lives.
	idx, err := NewFileIndexer(storeTwo).Rebuild(ctx)
	require.NoError(t, err)
	require.Len(t, idx.Sessions, 1)
	assert.Equal(t, "sessions/11111111", idx.Sessions[0].Dir)

	document := string(NewFileIndexer(storeTwo).Render(idx))
	assert.Contains(t, document, "sessions/11111111/", "session two must be told where session one's memory is")

	// ── Session two promotes what it found into the durable tier.
	res, err := Extract(ctx, storeTwo, all[0].RelPath, ExtractOptions{Kind: KindLesson})
	require.NoError(t, err)
	require.True(t, res.Created)
	_, err = UpdateIndex(ctx, storeTwo, NewFileIndexer(storeTwo))
	require.NoError(t, err)

	// ── A third session sees it as durable knowledge, with provenance intact.
	storeThree := NewStore(root).WithClock(clock(fixedTime().Add(2 * time.Hour)))
	idxThree, err := NewFileIndexer(storeThree).Rebuild(ctx)
	require.NoError(t, err)
	require.Len(t, idxThree.Global, 1)
	// session is the session the knowledge *came from*, not the one that did the
	// promoting: a memory is evidence of an experience, and the session that
	// later decided to keep it is not what it is evidence of.
	assert.Equal(t, sessionOne, idxThree.Global[0].Session)
	assert.Equal(t, all[0].RelPath, idxThree.Global[0].PromotedFrom)
	// The source path carries the origin session, so the two agree.
	assert.Contains(t, idxThree.Global[0].PromotedFrom, "sessions/11111111/")

	// The digest a fresh session's prompt receives mentions it.
	digest := Digest(idxThree, 4000)
	assert.Contains(t, digest, "Session one learned something")
	assert.Contains(t, digest, "1 global")
}

// A hand-dropped file must join the loop without any tool call, so the tree
// stays the source of truth rather than the tool that wrote it.
func TestClosedLoop_HandDroppedFileIsIndexed(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	ctx := context.Background()

	dir := filepath.Join(s.Root(), GlobalDir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hand-written.md"),
		[]byte("---\ntitle: Written by hand\nscope: global\nkind: note\n---\n\nbody\n"), 0o644))

	idx, err := NewFileIndexer(s).Rebuild(ctx)
	require.NoError(t, err)
	require.Len(t, idx.Global, 1)
	assert.Equal(t, "Written by hand", idx.Global[0].Title)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
