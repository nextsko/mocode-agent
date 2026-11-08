package fs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	mocodeconfig "github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
)

const testSessionID = "sess-test"

// fakeFiles satisfies history.Service; only the lookups the fs tools perform
// are implemented. Everything else panics through the embedded nil interface,
// which keeps the fake honest about which surface is under test.
type fakeFiles struct {
	history.Service
}

func (f *fakeFiles) GetByPathAndSession(context.Context, string, string) (history.File, error) {
	return history.File{}, errors.New("not found")
}

func (f *fakeFiles) Create(_ context.Context, sessionID, path, content string) (history.File, error) {
	return history.File{SessionID: sessionID, Path: path, Content: content}, nil
}

func (f *fakeFiles) CreateVersion(_ context.Context, sessionID, path, content string) (history.File, error) {
	return history.File{SessionID: sessionID, Path: path, Content: content}, nil
}

// fakeTracker records read times in memory; readAt is configured by the test.
type fakeTracker struct {
	filetracker.Service
	mu     sync.Mutex
	readAt time.Time
}

func (f *fakeTracker) RecordRead(context.Context, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readAt = time.Now()
}

func (f *fakeTracker) LastReadTime(context.Context, string, string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readAt
}

func portContext(t *testing.T) context.Context {
	t.Helper()
	ctx := context.WithValue(t.Context(), toolutil.SessionIDContextKeyVal, testSessionID)
	return toolutil.WithToolCallID(ctx, "call-test")
}

// runPort executes a port through the production adapter, which is what the
// agent runtime (and every test here) exercises.
func runPort(t *testing.T, tool contract.Tool, input string) fantasy.ToolResponse {
	t.Helper()
	return runPortWithContext(t, portContext(t), tool, input)
}

func runPortWithContext(t *testing.T, ctx context.Context, tool contract.Tool, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := asAgentTool(tool).Run(ctx, fantasy.ToolCall{ID: "call-test", Input: input})
	require.NoError(t, err)
	return resp
}

// resultMeta decodes the structured metadata. The adapter flattens the
// contract's {"result": ...} envelope to the legacy wire shape, so the test
// decodes the payload directly.
func resultMeta(t *testing.T, resp fantasy.ToolResponse, v any) {
	t.Helper()
	require.NotEmpty(t, resp.Metadata)
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), v))
}

func TestGlobPortTool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o600))

	resp := runPort(t, NewGlobPortTool(dir), `{"pattern":"*.go"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "a.go")
	require.NotContains(t, resp.Content, "b.txt")

	var meta GlobResponseMetadata
	resultMeta(t, resp, &meta)
	require.Equal(t, 1, meta.NumberOfFiles)
	require.False(t, meta.Truncated)
}

func TestGlobPortToolRequiresPattern(t *testing.T) {
	t.Parallel()

	resp := runPort(t, NewGlobPortTool(t.TempDir()), `{"pattern":""}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "pattern is required")
}

func TestGrepPortTool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.go"),
		[]byte("package target\n\n// needle lives here\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.go"),
		[]byte("package other\n"), 0o600))

	resp := runPort(t, NewGrepPortTool(dir, mocodeconfig.ToolGrep{}), `{"pattern":"needle"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "target.go")
	require.NotContains(t, resp.Content, "other.go")

	var meta GrepResponseMetadata
	resultMeta(t, resp, &meta)
	require.GreaterOrEqual(t, meta.NumberOfMatches, 1)
	require.False(t, meta.Truncated)
}

func TestLsPortTool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("y"), 0o600))

	resp := runPort(t, NewLsPortTool(nil, dir, mocodeconfig.ToolLs{}), `{}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "file.txt")
	require.Contains(t, resp.Content, "sub/")
	require.Contains(t, resp.Content, "nested.txt")

	var meta LSResponseMetadata
	resultMeta(t, resp, &meta)
	require.GreaterOrEqual(t, meta.NumberOfFiles, 2)
}

func TestReadFilesPortTool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta"), 0o600))

	tool := NewReadFilesPortTool(nil, &fakeTracker{}, dir)

	_, parallel := tool.(contract.ParallelTool)
	require.True(t, parallel, "read_files must advertise parallel execution")
	require.True(t, asAgentTool(tool).Info().Parallel)

	resp := runPort(t, tool, `{"paths":["a.txt","b.txt"]}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "alpha")
	require.Contains(t, resp.Content, "beta")

	var meta ReadFilesResponseMetadata
	resultMeta(t, resp, &meta)
	require.Equal(t, 2, meta.TotalFiles)
	require.Equal(t, 2, meta.SuccessCount)
	require.Equal(t, 0, meta.FailureCount)
	require.Equal(t, len("alpha")+len("beta"), meta.TotalSize)
}

func TestWritePortTool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tracker := &fakeTracker{}
	tool := NewWritePortTool(
		nil,
		permission.NewPermissionService(dir, true, nil),
		&fakeFiles{},
		tracker,
		dir,
	)

	resp := runPort(t, tool, `{"file_path":"notes/new.txt","content":"hello world\n"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "File successfully written")

	written, err := os.ReadFile(filepath.Join(dir, "notes", "new.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello world\n", string(written))
	require.False(t, tracker.LastReadTime(context.Background(), testSessionID, "").IsZero())

	var meta WriteResponseMetadata
	resultMeta(t, resp, &meta)
	require.NotEmpty(t, meta.Diff)
	require.GreaterOrEqual(t, meta.Additions, 1)
	require.Equal(t, 0, meta.Removals)
}

func TestEditPortToolReplacesContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(target, []byte("hello world\n"), 0o600))

	tracker := &fakeTracker{readAt: time.Now()}
	tool := NewEditPortTool(
		nil,
		permission.NewPermissionService(dir, true, nil),
		&fakeFiles{},
		tracker,
		dir,
	)

	resp := runPort(t, tool, `{"file_path":"edit.txt","old_string":"world","new_string":"there"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Content replaced in file:")

	updated, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "hello there\n", string(updated))

	var meta EditResponseMetadata
	resultMeta(t, resp, &meta)
	require.Equal(t, 1, meta.Additions)
	require.Equal(t, 1, meta.Removals)
}

func TestEditPortToolRequiresPriorRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "edit.txt")
	require.NoError(t, os.WriteFile(target, []byte("hello world\n"), 0o600))

	tool := NewEditPortTool(
		nil,
		permission.NewPermissionService(dir, true, nil),
		&fakeFiles{},
		&fakeTracker{},
		dir,
	)

	resp := runPort(t, tool, `{"file_path":"edit.txt","old_string":"hello","new_string":"hi"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "you must read the file before editing it")

	unchanged, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "hello world\n", string(unchanged))
}

func TestMultiEditPortToolCreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tool := NewMultiEditPortTool(
		nil,
		permission.NewPermissionService(dir, true, nil),
		&fakeFiles{},
		&fakeTracker{},
		dir,
	)

	resp := runPort(t, tool,
		`{"file_path":"sub/new.md","edits":[`+
			`{"old_string":"","new_string":"# Title\n"},`+
			`{"old_string":"# Title","new_string":"# Title\nbody\n"}]}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "File created with 2 edits")

	// The second edit replaces "# Title" inside "# Title\n", so the original
	// trailing newline is preserved after the inserted body.
	created, err := os.ReadFile(filepath.Join(dir, "sub", "new.md"))
	require.NoError(t, err)
	require.Equal(t, "# Title\nbody\n\n", string(created))

	var meta MultiEditResponseMetadata
	resultMeta(t, resp, &meta)
	require.Equal(t, 2, meta.EditsApplied)
	require.Empty(t, meta.EditsFailed)
}

func TestMultiEditPortToolEditsExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "existing.txt")
	require.NoError(t, os.WriteFile(target, []byte("alpha\nbeta\n"), 0o600))

	tool := NewMultiEditPortTool(
		nil,
		permission.NewPermissionService(dir, true, nil),
		&fakeFiles{},
		&fakeTracker{readAt: time.Now()},
		dir,
	)

	resp := runPort(t, tool,
		`{"file_path":"existing.txt","edits":[`+
			`{"old_string":"alpha","new_string":"ALPHA"},`+
			`{"old_string":"beta","new_string":"BETA"}]}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Applied 2 edits to file:")

	updated, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "ALPHA\nBETA\n", string(updated))
}

func TestViewPortToolReadsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "text.txt"), []byte("line one\nline two\n"), 0o600))

	resp := runPort(t, NewViewPortTool(nil, nil, &fakeTracker{}, nil, nil, dir), `{"file_path":"text.txt"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "<file>")
	require.Contains(t, resp.Content, "line one")
	require.Contains(t, resp.Content, "line two")

	var meta ViewResponseMetadata
	resultMeta(t, resp, &meta)
	require.Equal(t, filepath.Join(dir, "text.txt"), meta.FilePath)
	require.Contains(t, meta.Content, "line one")
}

func TestViewPortToolMissingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tool := NewViewPortTool(nil, nil, &fakeTracker{}, nil, nil, dir)

	resp := runPort(t, tool, `{"file_path":"nope.txt"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "File not found")

	resp = runPort(t, tool, `{"file_path":""}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "file_path is required")
}

func TestViewPortToolURIWithoutRegistry(t *testing.T) {
	t.Parallel()

	resp := runPort(t, NewViewPortTool(nil, nil, &fakeTracker{}, nil, nil, t.TempDir()),
		`{"file_path":"mocode://docs"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "not available in this session")
}

func TestViewPortToolImage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pic.png"), []byte("\x89PNG\r\n\x1a\n"), 0o600))

	tool := NewViewPortTool(nil, nil, &fakeTracker{}, nil, nil, dir)

	// Without image support the tool reports a tool-level error.
	resp := runPort(t, tool, `{"file_path":"pic.png"}`)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "does not support image data")

	// With image support the port returns an attachment, and the adapter maps
	// it onto a binary image response.
	ctx := context.WithValue(portContext(t), toolutil.SupportsImagesContextKeyVal, true)
	resp = runPortWithContext(t, ctx, tool, `{"file_path":"pic.png"}`)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "image", resp.Type)
	require.Equal(t, "image/png", resp.MediaType)
	require.NotEmpty(t, resp.Data)
}

func TestPortToolSchemas(t *testing.T) {
	t.Parallel()

	type property struct {
		typ    string
		needed bool
	}

	cases := []struct {
		name     string
		tool     contract.Tool
		required []string
		props    map[string]property
	}{
		{
			name:     GlobToolName,
			tool:     NewGlobPortTool("."),
			required: []string{"pattern"},
			props: map[string]property{
				"pattern": {typ: "string"},
				"path":    {typ: "string"},
			},
		},
		{
			name:     GrepToolName,
			tool:     NewGrepPortTool(".", mocodeconfig.ToolGrep{}),
			required: []string{"pattern"},
			props: map[string]property{
				"pattern":      {typ: "string"},
				"path":         {typ: "string"},
				"include":      {typ: "string"},
				"literal_text": {typ: "boolean"},
			},
		},
		{
			name:     LSToolName,
			tool:     NewLsPortTool(nil, ".", mocodeconfig.ToolLs{}),
			required: []string{},
			props: map[string]property{
				"path":   {typ: "string"},
				"ignore": {typ: "array"},
				"depth":  {typ: "integer"},
			},
		},
		{
			name:     WriteToolName,
			tool:     NewWritePortTool(nil, nil, nil, nil, "."),
			required: []string{"file_path", "content"},
			props: map[string]property{
				"file_path": {typ: "string"},
				"content":   {typ: "string"},
			},
		},
		{
			name:     EditToolName,
			tool:     NewEditPortTool(nil, nil, nil, nil, "."),
			required: []string{"file_path", "old_string", "new_string"},
			props: map[string]property{
				"file_path":   {typ: "string"},
				"old_string":  {typ: "string"},
				"new_string":  {typ: "string"},
				"replace_all": {typ: "boolean"},
			},
		},
		{
			name:     MultiEditToolName,
			tool:     NewMultiEditPortTool(nil, nil, nil, nil, "."),
			required: []string{"file_path", "edits"},
			props: map[string]property{
				"file_path": {typ: "string"},
				"edits":     {typ: "array"},
			},
		},
		{
			name:     ReadFilesToolName,
			tool:     NewReadFilesPortTool(nil, nil, "."),
			required: []string{"paths"},
			props: map[string]property{
				"paths":         {typ: "array"},
				"max_file_size": {typ: "integer"},
			},
		},
		{
			name:     ViewToolName,
			tool:     NewViewPortTool(nil, nil, nil, nil, nil, "."),
			required: []string{"file_path"},
			props: map[string]property{
				"file_path": {typ: "string"},
				"offset":    {typ: "integer"},
				"limit":     {typ: "integer"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			info := asAgentTool(tc.tool).Info()
			require.Equal(t, tc.name, info.Name)
			require.Equal(t, tc.required, info.Required)

			for name, want := range tc.props {
				spec, ok := info.Parameters[name].(map[string]any)
				require.True(t, ok, "missing parameter %q", name)
				require.Equal(t, want.typ, spec["type"], "parameter %q", name)
				require.NotEmpty(t, spec["description"], "parameter %q needs a description", name)
			}
		})
	}
}

func TestMultiEditSchemaEditsItems(t *testing.T) {
	t.Parallel()

	info := asAgentTool(NewMultiEditPortTool(nil, nil, nil, nil, ".")).Info()
	edits, ok := info.Parameters["edits"].(map[string]any)
	require.True(t, ok)
	items, ok := edits["items"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "object", items["type"])
	require.Equal(t, []string{"old_string", "new_string"}, items["required"])

	props, ok := items["properties"].(map[string]any)
	require.True(t, ok)
	for _, name := range []string{"old_string", "new_string", "replace_all"} {
		spec, ok := props[name].(map[string]any)
		require.True(t, ok, "missing edits item property %q", name)
		require.NotEmpty(t, spec["description"], "edits item %q needs a description", name)
	}
}
