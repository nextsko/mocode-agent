package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedTime() time.Time {
	return time.Date(2026, 9, 14, 12, 30, 45, 0, time.UTC)
}

const testSession = "1a2b3c4d-e5f6-7890-abcd-ef1234567890"

func sampleEntry(scope Scope) *Entry {
	return &Entry{
		Title:   "Flaky Go tests caused by a shared temp dir",
		Scope:   scope,
		Kind:    KindLesson,
		Date:    fixedTime(),
		Session: testSession,
		Tags:    []string{"Go", "testing", "go"},
		Body:    "Symptom: passes alone, fails under -race.\n\nFix: t.TempDir().",
	}
}

// The filename is the user's stated requirement: time + session + markdown.
func TestFileName_IsTimeSessionSlug(t *testing.T) {
	t.Parallel()

	e := sampleEntry(ScopeSession)
	assert.Equal(t, "20260914T123045-1a2b3c4d-flaky-go-tests-caused-by-a-shared-temp-dir.md", e.FileName())
	assert.True(t, strings.HasSuffix(e.FileName(), ".md"))
}

// The session prefix must be path-safe even for the synthetic ids the app
// builds (`<messageID>$$<toolCallID>`), which contain separators.
func TestFileName_SanitisesSyntheticSessionIDs(t *testing.T) {
	t.Parallel()

	e := sampleEntry(ScopeSession)
	e.Session = "msg-123$$call/456"

	name := e.FileName()
	assert.NotContains(t, name, "/")
	assert.NotContains(t, name, "$")
	assert.True(t, strings.HasPrefix(name, "20260914T123045-msg-123-"),
		"the prefix is the first path-safe run of the id, got %q", name)
}

func TestFileName_NoSessionStillProducesAUsableName(t *testing.T) {
	t.Parallel()

	e := sampleEntry(ScopeGlobal)
	e.Session = ""

	assert.Equal(t, "20260914T123045-flaky-go-tests-caused-by-a-shared-temp-dir.md", e.FileName())
	assert.Equal(t, GlobalDir, e.RelDir())
}

func TestRelDir_TiersAndSessions(t *testing.T) {
	t.Parallel()

	assert.Equal(t, GlobalDir, sampleEntry(ScopeGlobal).RelDir())
	assert.Equal(t, "sessions/1a2b3c4d", sampleEntry(ScopeSession).RelDir())

	orphan := sampleEntry(ScopeSession)
	orphan.Session = ""
	assert.Equal(t, "sessions/unknown", orphan.RelDir(), "an orphaned session entry must still land somewhere deterministic")
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Entry)
		wantSub string
	}{
		{"ok", func(*Entry) {}, ""},
		{"no title", func(e *Entry) { e.Title = "  " }, "title is required"},
		{"title too long", func(e *Entry) { e.Title = strings.Repeat("x", 200) }, "title exceeds"},
		{"bad scope", func(e *Entry) { e.Scope = "nope" }, "scope must be one of"},
		{"bad kind", func(e *Entry) { e.Kind = "nope" }, "kind must be one of"},
		{"no body", func(e *Entry) { e.Body = " " }, "body is required"},
		{"session entry without session", func(e *Entry) { e.Session = "" }, "needs the session"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := sampleEntry(ScopeSession)
			tt.mutate(e)

			err := e.Validate()
			if tt.wantSub == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
		})
	}
}

// A kind left empty defaults rather than failing: the caller should not have to
// name a category to record a thought.
func TestValidate_DefaultsKindAndDate(t *testing.T) {
	t.Parallel()

	e := sampleEntry(ScopeSession)
	e.Kind = ""
	e.Date = time.Time{}

	require.NoError(t, e.Validate())
	assert.Equal(t, KindNote, e.Kind)
	assert.False(t, e.Date.IsZero())
}

func TestRender_ParseRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry *Entry
	}{
		{"session", sampleEntry(ScopeSession)},
		{"global", sampleEntry(ScopeGlobal)},
		{
			// Titles are model-written prose, so YAML-hostile characters must
			// survive.
			name: "tricky title and body",
			entry: &Entry{
				Title:   `Fixed "quit" on :wq, and other #gotchas`,
				Scope:   ScopeGlobal,
				Kind:    KindFact,
				Date:    fixedTime(),
				Session: testSession,
				Tags:    []string{"vim", "editing"},
				Body:    "A body with `---` inside:\n\n---\n\nand a colon: like this.",
			},
		},
		{
			name: "promoted",
			entry: func() *Entry {
				e := sampleEntry(ScopeGlobal)
				e.PromotedFrom = "sessions/1a2b3c4d/x.md"
				return e
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := tt.entry.Render()
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(data), "---\n"))

			got, err := ParseEntry(data)
			require.NoError(t, err)

			assert.Equal(t, tt.entry.Title, got.Title)
			assert.Equal(t, tt.entry.Scope, got.Scope)
			assert.Equal(t, tt.entry.Kind, got.Kind)
			assert.Equal(t, tt.entry.Session, got.Session)
			assert.Equal(t, tt.entry.PromotedFrom, got.PromotedFrom)
			assert.Equal(t, strings.TrimSpace(tt.entry.Body), got.Body)
			assert.WithinDuration(t, tt.entry.Date, got.Date, time.Second)
			// Tags are normalised (lowercased, deduped) on the way out.
			for _, tag := range got.Tags {
				assert.Equal(t, strings.ToLower(tag), tag)
			}
		})
	}
}

// The body must not be able to look like it ends the frontmatter early.
func TestParseEntry_BodyKeepshHorizontalRules(t *testing.T) {
	t.Parallel()

	data, err := sampleEntry(ScopeGlobal).Render()
	require.NoError(t, err)

	got, err := ParseEntry(data)
	require.NoError(t, err)
	assert.Contains(t, got.Body, "t.TempDir()")
	assert.NotContains(t, got.Body, "---\nscope:")
}

// A hand-written file with no frontmatter is still readable, so dropping a note
// into the tree by hand is not silently ignored.
func TestParseEntry_PlainFileIsStillAMemory(t *testing.T) {
	t.Parallel()

	got, err := ParseEntry([]byte("# A plain note\n\nsome content\n"))
	require.NoError(t, err)

	assert.Equal(t, "A plain note", got.Title)
	assert.Equal(t, KindNote, got.Kind)
	assert.Contains(t, got.Body, "some content")
}

func TestParseEntry_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := ParseEntry([]byte("   \n\n"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidEntry)
}

// A malformed date must not hide the content: the entry is still useful.
func TestParseEntry_BadDateKeepsContent(t *testing.T) {
	t.Parallel()

	got, err := ParseEntry([]byte("---\ntitle: T\ndate: not-a-date\nscope: global\n---\n\nbody\n"))
	require.NoError(t, err)
	assert.Equal(t, "T", got.Title)
	assert.Equal(t, "body", got.Body)
	assert.True(t, got.Date.IsZero())
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"Hello World":                  "hello-world",
		"  spaces  and---dashes  ":     "spaces-and-dashes",
		"CamelCaseTitle":               "camelcasetitle",
		"trailing punctuation!!":       "trailing-punctuation",
		"中文标题 with ascii":              "with-ascii",
		"!!!":                          "memory",
		"a":                            "a",
		strings.Repeat("verylong", 20): strings.Trim(strings.Repeat("verylong", 20)[:maxSlugLength], "-"),
	} {
		assert.Equal(t, want, slugify(in), "input %q", in)
	}
}

func TestNormalizeTags(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"go", "testing"}, normalizeTags([]string{"Go", " testing ", "GO", "", "testing"}))
	assert.Empty(t, normalizeTags(nil))
}

// An unbounded slug would produce a filename some filesystems reject.
func TestSlugify_Bounded(t *testing.T) {
	t.Parallel()

	slug := slugify(strings.Repeat("word ", 100))
	assert.LessOrEqual(t, len(slug), maxSlugLength)
	assert.NotEmpty(t, slug)
}
