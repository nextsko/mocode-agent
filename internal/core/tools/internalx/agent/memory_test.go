package agenttools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/memory"
	"github.com/nextsko/mocode-agent/internal/core/uri"
)

const (
	memSessionOne = "11111111-1111-1111-1111-111111111111"
	memSessionTwo = "22222222-2222-2222-2222-222222222222"
)

func memClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 9, 14, 12, 30, 45, 0, time.UTC) }
}

// memDeps builds a tool bound to a fresh store, with the session the tool should
// believe it is running in.
func memDeps(t *testing.T, session string) (MemoryDeps, context.Context, string) {
	t.Helper()

	root := t.TempDir()
	store := memory.NewStore(root).WithClock(memClock())
	deps := MemoryDeps{Store: store, Indexer: memory.NewFileIndexer(store), Now: memClock()}

	ctx := context.WithValue(context.Background(), toolutil.SessionIDContextKeyVal, session)
	return deps, ctx, root
}

func runMem(t *testing.T, deps MemoryDeps, ctx context.Context, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := NewMemoryTool(deps).Run(ctx, fantasy.ToolCall{
		ID: "call-1", Name: MemoryToolName, Input: input,
	})
	require.NoError(t, err)
	return resp
}

// ─── dispatch and validation ─────────────────────────────────────────────────

func TestMemory_InvalidAction(t *testing.T) {
	t.Parallel()

	deps, ctx, _ := memDeps(t, memSessionOne)
	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"nope"}`} {
		resp := runMem(t, deps, ctx, input)
		assert.True(t, resp.IsError, "input %q must be rejected", input)
		assert.Contains(t, resp.Content, "write")
	}
}

func TestMemory_UnconfiguredStore(t *testing.T) {
	t.Parallel()

	resp, err := NewMemoryTool(MemoryDeps{}).Run(context.Background(),
		fantasy.ToolCall{ID: "c", Name: MemoryToolName, Input: `{"action":"list"}`})
	require.NoError(t, err)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "not configured")
}

func TestMemory_WriteValidation(t *testing.T) {
	t.Parallel()

	deps, ctx, _ := memDeps(t, memSessionOne)

	tests := []struct {
		input   string
		wantSub string
	}{
		{`{"action":"write"}`, "title is required"},
		{`{"action":"write","title":"T"}`, "body is required"},
		{`{"action":"write","title":"T","body":"b","scope":"nope"}`, "invalid scope"},
		{`{"action":"write","title":"T","body":"b","kind":"nope"}`, "invalid kind"},
		{`{"action":"search"}`, "query is required"},
		{`{"action":"forget"}`, "path is required"},
	}

	for _, tt := range tests {
		resp := runMem(t, deps, ctx, tt.input)
		assert.True(t, resp.IsError, "input %q", tt.input)
		assert.Contains(t, resp.Content, tt.wantSub, "input %q", tt.input)
	}
}

// ─── the closed loop ─────────────────────────────────────────────────────────

// TestMemory_ClosedLoop is the end-to-end guarantee: write in one session,
// discover it from another, read it through the protocol, promote it, and have
// it survive as durable knowledge — all through the real tool, the real store
// and the real URI namespace.
func TestMemory_ClosedLoop(t *testing.T) {
	t.Parallel()

	deps, ctxOne, root := memDeps(t, memSessionOne)

	// ── 1. Session one writes a memory through the tool.
	resp := runMem(t, deps, ctxOne, `{
		"action":"write",
		"title":"Flaky Go tests caused by a shared temp dir",
		"body":"Symptom: passes alone, fails under -race. Fix: t.TempDir().",
		"kind":"lesson",
		"tags":["go","testing"]
	}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "session memory", "a write defaults to the session tier")

	// ── 2. It really is on disk, named time+session+slug.
	glob := filepath.Join(root, "sessions", "11111111", "20260914T123045-11111111-flaky-go-tests-caused-by-a-shared-temp-dir.md")
	require.FileExists(t, glob, "the filename must be time + session + slug")

	// ── 3. The index was refreshed as part of writing, and records the location.
	indexBytes, err := os.ReadFile(filepath.Join(root, memory.IndexFileName))
	require.NoError(t, err)
	index := string(indexBytes)
	assert.Contains(t, index, "sessions/11111111/", "the index must record where session memory lives")
	assert.Contains(t, index, "Flaky Go tests")

	// ── 4. A different session, sharing only the root, finds it.
	depsTwo, ctxTwo, _ := memDeps(t, memSessionTwo)
	depsTwo.Store = memory.NewStore(root).WithClock(memClock())
	depsTwo.Indexer = memory.NewFileIndexer(depsTwo.Store)

	resp = runMem(t, depsTwo, ctxTwo, `{"action":"search","query":"t.TempDir"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "Flaky Go tests", "a later session must be able to find it")
	assert.Contains(t, resp.Content, "matched body")

	resp = runMem(t, depsTwo, ctxTwo, `{"action":"list"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "sessions/11111111/", "the listing must show where it came from")

	// ── 5. It is readable through the protocol, and the namespace root is the
	//       live index rather than a stale copy.
	registry := uri.NewRegistry()
	registry.Register(memory.NewURIProvider(depsTwo.Store, depsTwo.Indexer))

	content, _, err := registry.Resolve(context.Background(), "mocode://memory/")
	require.NoError(t, err)
	assert.Contains(t, content.Text, "Session memories")
	assert.Contains(t, content.Text, "sessions/11111111/")

	entryPath := "sessions/11111111/20260914T123045-11111111-flaky-go-tests-caused-by-a-shared-temp-dir.md"
	content, _, err = registry.Resolve(context.Background(), "mocode://memory/"+entryPath)
	require.NoError(t, err)
	assert.Contains(t, content.Text, "t.TempDir()")
	assert.Contains(t, content.Text, "scope: session")

	// ── 6. Session two promotes it into the durable tier.
	resp = runMem(t, depsTwo, ctxTwo, `{"action":"extract"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "Promoted")

	// ── 7. It now reads as global, with provenance, and the session copy is intact.
	content, _, err = registry.Resolve(context.Background(), "mocode://memory/global/20260914T123045-11111111-flaky-go-tests-caused-by-a-shared-temp-dir.md")
	require.NoError(t, err)
	assert.Contains(t, content.Text, "scope: global")
	assert.Contains(t, content.Text, "promoted_from: "+entryPath)

	// The session's own record is untouched: extraction copies.
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(entryPath)))

	// ── 8. Re-running extraction does not duplicate.
	resp = runMem(t, depsTwo, ctxTwo, `{"action":"extract"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "already in global memory")

	idx, err := depsTwo.Indexer.Rebuild(context.Background())
	require.NoError(t, err)
	assert.Len(t, idx.Global, 1, "no duplicate was minted")
	assert.Equal(t, 1, idx.SessionEntryCount())

	// ── 9. A third session sees the durable entry in its prompt digest.
	depsThree, _, _ := memDeps(t, "33333333-3333-3333-3333-333333333333")
	depsThree.Store = memory.NewStore(root).WithClock(memClock())
	idxThree, err := memory.NewFileIndexer(depsThree.Store).Rebuild(context.Background())
	require.NoError(t, err)

	digest := memory.Digest(idxThree, 4000)
	assert.Contains(t, digest, "Flaky Go tests", "durable knowledge must reach a fresh session's prompt")

	// ── 10. Forgetting archives rather than deletes.
	resp = runMem(t, depsTwo, ctxTwo, `{"action":"forget","path":"global/20260914T123045-11111111-flaky-go-tests-caused-by-a-shared-temp-dir.md"}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, "Archived")

	entries, err := os.ReadDir(filepath.Join(root, memory.ArchiveDir))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the content must still exist, just archived")
}

// Step 6 of the loop needs a session that already has memory; without one the
// tool must say how to proceed rather than failing obscurely.
func TestMemory_ExtractWithNothingToPromote(t *testing.T) {
	t.Parallel()

	deps, ctx, _ := memDeps(t, memSessionOne)
	resp := runMem(t, deps, ctx, `{"action":"extract"}`)

	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "no session memory to extract")
	assert.Contains(t, resp.Content, "action=write")
}

// Extracting an explicit path, and overriding the title, is how a long session
// note is distilled rather than copied verbatim.
func TestMemory_ExtractExplicitPathWithOverrides(t *testing.T) {
	t.Parallel()

	deps, ctx, root := memDeps(t, memSessionOne)

	resp := runMem(t, deps, ctx, `{"action":"write","title":"Raw note","body":"long rambling detail"}`)
	require.False(t, resp.IsError, resp.Content)

	src := "sessions/11111111/20260914T123045-11111111-raw-note.md"
	resp = runMem(t, deps, ctx, `{"action":"extract","path":"`+src+`","title":"Distilled","body":"short version","kind":"lesson"}`)
	require.False(t, resp.IsError, resp.Content)

	content, err := memory.NewStore(root).Read("global/20260914T123045-11111111-distilled.md")
	require.NoError(t, err)
	assert.Equal(t, "Distilled", content.Title)
	assert.Equal(t, "short version", content.Body)
	assert.Equal(t, memory.KindLesson, content.Kind)
	assert.Equal(t, src, content.PromotedFrom)
}

// A write straight to global is allowed but should not be the default, so the
// tool's answer points at extraction instead.
func TestMemory_WriteDirectlyToGlobal(t *testing.T) {
	t.Parallel()

	deps, ctx, root := memDeps(t, memSessionOne)
	resp := runMem(t, deps, ctx, `{"action":"write","scope":"global","title":"Durable","body":"body"}`)
	require.False(t, resp.IsError, resp.Content)

	require.FileExists(t, filepath.Join(root, "global", "20260914T123045-11111111-durable.md"))
	assert.NotContains(t, resp.Content, "Promote it with action=extract",
		"an entry already in global has nothing to promote")
}

func TestMemory_WriteIsIdempotentForTheSameStampAndTitle(t *testing.T) {
	t.Parallel()

	deps, ctx, root := memDeps(t, memSessionOne)

	first := runMem(t, deps, ctx, `{"action":"write","title":"Same","body":"first"}`)
	require.False(t, first.IsError)

	second := runMem(t, deps, ctx, `{"action":"write","title":"Same","body":"second"}`)
	require.False(t, second.IsError)
	assert.Contains(t, second.Content, "already existed and was replaced")

	// One file, holding the newer content.
	entries, err := os.ReadDir(filepath.Join(root, "sessions", "11111111"))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	got, err := memory.NewStore(root).Read("sessions/11111111/20260914T123045-11111111-same.md")
	require.NoError(t, err)
	assert.Equal(t, "second", got.Body)
}

func TestMemory_ListEmptyThenPopulated(t *testing.T) {
	t.Parallel()

	deps, ctx, _ := memDeps(t, memSessionOne)

	resp := runMem(t, deps, ctx, `{"action":"list"}`)
	require.False(t, resp.IsError)
	assert.Contains(t, resp.Content, "No memory entries")
	assert.Contains(t, resp.Content, "action=write")

	runMem(t, deps, ctx, `{"action":"write","title":"Something","body":"b"}`)

	resp = runMem(t, deps, ctx, `{"action":"list"}`)
	assert.Contains(t, resp.Content, "Something")
}

// A scope filter that excludes everything must not look like "no memory at all".
func TestMemory_ListNarrowsByScopeAndSaysSo(t *testing.T) {
	t.Parallel()

	deps, ctx, _ := memDeps(t, memSessionOne)
	runMem(t, deps, ctx, `{"action":"write","title":"Session thing","body":"b"}`)

	resp := runMem(t, deps, ctx, `{"action":"list","scope":"global"}`)
	require.False(t, resp.IsError)
	assert.Contains(t, resp.Content, "No memory entries")
	assert.Contains(t, resp.Content, "other tier", "the tool must point at what it filtered out")

	resp = runMem(t, deps, ctx, `{"action":"list","scope":"session"}`)
	assert.Contains(t, resp.Content, "Session thing")
}

func TestMemory_SearchNoMatchSuggestsWriting(t *testing.T) {
	t.Parallel()

	deps, ctx, _ := memDeps(t, memSessionOne)
	resp := runMem(t, deps, ctx, `{"action":"search","query":"nothing like this exists"}`)

	require.False(t, resp.IsError)
	assert.Contains(t, resp.Content, "No memory matched")
	assert.Contains(t, resp.Content, "action=write")
}

// The session is taken from the tool context, which is how a write ends up in
// the right session's directory rather than in "unknown".
func TestMemory_WriteWithoutASessionContextFails(t *testing.T) {
	t.Parallel()

	deps, _, _ := memDeps(t, memSessionOne)
	resp := runMem(t, deps, context.Background(), `{"action":"write","title":"T","body":"b"}`)

	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "needs the session")
}

// ─── schema ──────────────────────────────────────────────────────────────────

func TestMemory_SchemaExposesActionEnum(t *testing.T) {
	t.Parallel()

	info := NewMemoryTool(MemoryDeps{}).Info()
	assert.Equal(t, MemoryToolName, info.Name)

	action, ok := info.Parameters["action"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"write", "list", "search", "extract", "forget"}, action["enum"])

	for _, field := range []string{"title", "body", "scope", "kind", "tags", "path", "query", "session", "limit"} {
		spec, ok := info.Parameters[field].(map[string]any)
		require.Truef(t, ok, "field %q missing from the generated schema", field)
		assert.NotEmptyf(t, spec["description"], "field %q needs a description for the model", field)
	}

	// The scope and kind enums are what stop the model inventing tiers.
	scope, _ := info.Parameters["scope"].(map[string]any)
	assert.Equal(t, []any{"session", "global"}, scope["enum"])
	kind, _ := info.Parameters["kind"].(map[string]any)
	assert.Equal(t, []any{"note", "lesson", "decision", "fact"}, kind["enum"])
}

func TestMemory_DescriptionIsUsable(t *testing.T) {
	t.Parallel()

	desc := NewMemoryTool(MemoryDeps{}).Info().Description
	assert.NotEmpty(t, desc)
	assert.Greater(t, len(desc), 40)
	assert.False(t, strings.HasPrefix(desc, "//"), "the //go:embed directive must not leak")
}
