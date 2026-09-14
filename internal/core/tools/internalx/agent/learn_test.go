package agenttools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/skills"
)

func fixedNow() time.Time {
	return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
}

// fakeGate records permission requests and returns a canned verdict.
type fakeGate struct {
	allow bool
	err   error
	calls []permission.CreatePermissionRequest
}

func (f *fakeGate) Request(_ context.Context, opts permission.CreatePermissionRequest) (bool, error) {
	f.calls = append(f.calls, opts)
	return f.allow, f.err
}

func run(t *testing.T, deps LearnDeps, input string) fantasy.ToolResponse {
	t.Helper()
	resp, err := NewLearnTool(deps).Run(context.Background(), fantasy.ToolCall{
		ID: "call-1", Name: LearnToolName, Input: input,
	})
	require.NoError(t, err)
	return resp
}

func createInput(name, desc, body string) string {
	return `{"action":"create","name":"` + name + `","description":"` + desc + `","instructions":"` + body + `"}`
}

// ─── list ────────────────────────────────────────────────────────────────────

func TestLearn_List_SeparatesEditableFromBundled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{
		Root: root,
		Known: []*skills.Skill{
			{Name: "jq", Description: "Use when parsing JSON", Builtin: true, SkillFilePath: "mocode://skills/jq/SKILL.md"},
			{Name: "deploy", Description: "Use when deploying", SkillFilePath: filepath.Join(root, "deploy", "SKILL.md"),
				Metadata: map[string]string{skills.MetaOrigin: skills.OriginLearn, skills.MetaRevision: "3"}},
			{Name: "handwritten", Description: "Use when hand written", SkillFilePath: "/tmp/handwritten/SKILL.md"},
		},
	}

	resp := run(t, deps, `{"action":"list"}`)
	require.False(t, resp.IsError)

	assert.Contains(t, resp.Content, "3 (2 editable, 1 bundled)")
	assert.Contains(t, resp.Content, "deploy [learn, rev 3]")
	assert.Contains(t, resp.Content, "handwritten [hand-written, rev 1]")
	assert.Contains(t, resp.Content, "Bundled skills")
	assert.Contains(t, resp.Content, "jq")
	assert.Contains(t, resp.Content, root, "the write target must be stated so the model knows where skills land")
}

func TestLearn_List_NoSkills(t *testing.T) {
	t.Parallel()

	resp := run(t, LearnDeps{Root: t.TempDir()}, `{"action":"list"}`)
	require.False(t, resp.IsError)
	assert.Contains(t, resp.Content, "0 (0 editable, 0 bundled)")
}

// ─── dispatch ────────────────────────────────────────────────────────────────

func TestLearn_InvalidAction(t *testing.T) {
	t.Parallel()

	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"nope"}`} {
		resp := run(t, LearnDeps{Root: t.TempDir()}, input)
		assert.True(t, resp.IsError, "input %q must be rejected", input)
		assert.Contains(t, resp.Content, "create, refine, list")
	}
}

// ─── create ──────────────────────────────────────────────────────────────────

func TestLearn_Create_WritesDiscoverableSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resp := run(t, LearnDeps{Root: root, Now: fixedNow},
		createInput("deploy-runbook", "Use when deploying mocode to a new host", "# Deploy\\n\\n1. Run task dev:ship."))
	require.False(t, resp.IsError, resp.Content)

	assert.Contains(t, resp.Content, "Created skill \"deploy-runbook\" (revision 1)")
	assert.Contains(t, resp.Content, filepath.Join(root, "deploy-runbook", "SKILL.md"))

	// The whole point: the next discovery pass must pick it up.
	found := skills.Discover([]string{root})
	require.Len(t, found, 1)
	assert.Equal(t, "deploy-runbook", found[0].Name)
	assert.Equal(t, skills.OriginLearn, found[0].Metadata[skills.MetaOrigin])
	assert.Equal(t, "1", found[0].Metadata[skills.MetaRevision])
	assert.Contains(t, found[0].Instructions, "task dev:ship")
}

func TestLearn_Create_RejectsShadowingBundledSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{Root: root, Known: []*skills.Skill{
		{Name: "jq", Builtin: true, SkillFilePath: "mocode://skills/jq/SKILL.md"},
	}}

	resp := run(t, deps, createInput("jq", "Use when parsing JSON", "body"))
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "bundled")

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "a refused create must not touch the skills directory")
}

func TestLearn_Create_RejectsExistingSkillAndPointsAtRefine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{Root: root, Known: []*skills.Skill{
		{Name: "deploy", Description: "Use when deploying", SkillFilePath: filepath.Join(root, "deploy", "SKILL.md")},
	}}

	resp := run(t, deps, createInput("deploy", "Use when deploying", "body"))
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "already exists")
	assert.Contains(t, resp.Content, "refine")
}

// A skill created earlier in this session is absent from Known (which is built
// at session start) but present on disk — create must not clobber it.
func TestLearn_Create_RejectsSkillWrittenEarlierInSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "deploy"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "deploy", skills.SkillFileName), []byte("x"), 0o644))

	resp := run(t, LearnDeps{Root: root, Now: fixedNow}, createInput("deploy", "Use when deploying", "body"))
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "already exists")
}

func TestLearn_Create_ValidatesInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantSub string
	}{
		{"missing name", `{"action":"create","description":"Use when x","instructions":"b"}`, "name is required"},
		{"bad name", createInput("Bad_Name", "Use when x", "b"), "invalid skill name"},
		{"missing instructions", createInput("ok-name", "Use when x", ""), "instructions is required"},
		{"missing description", createInput("ok-name", "", "b"), "description is required"},
		{"description off-style", createInput("ok-name", "This skill explains X", "b"), "must start with"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			resp := run(t, LearnDeps{Root: root}, tt.input)
			assert.True(t, resp.IsError)
			assert.Contains(t, resp.Content, tt.wantSub)

			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			assert.Empty(t, entries, "invalid input must not write anything")
		})
	}
}

func TestLearn_NoRootConfigured(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		createInput("x", "Use when x", "b"),
		`{"action":"refine","name":"x","instructions":"b"}`,
	} {
		resp := run(t, LearnDeps{}, input)
		assert.True(t, resp.IsError)
		assert.Contains(t, resp.Content, "no writable skills directory")
	}
}

// ─── refine ──────────────────────────────────────────────────────────────────

func TestLearn_Refine_BumpsRevisionAndBacksUp(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{Root: root, Now: fixedNow}

	require.False(t, run(t, deps, createInput("deploy", "Use when deploying", "first body")).IsError)

	later := deps
	later.Now = func() time.Time { return fixedNow().Add(time.Hour) }
	resp := run(t, later, `{"action":"refine","name":"deploy","instructions":"second body"}`)
	require.False(t, resp.IsError, resp.Content)

	assert.Contains(t, resp.Content, "revision 2")
	assert.Contains(t, resp.Content, "backed up")

	found := skills.Discover([]string{root})
	require.Len(t, found, 1, "refine must not create a duplicate")
	assert.Equal(t, "2", found[0].Metadata[skills.MetaRevision])
	assert.Equal(t, "second body", found[0].Instructions)
	// The description carries over when the caller does not restate it.
	assert.Equal(t, "Use when deploying", found[0].Description)
	// Creation time is preserved across refinements.
	assert.Equal(t, fixedNow().Format(time.RFC3339), found[0].Metadata[skills.MetaCreatedAt])
	assert.Equal(t, fixedNow().Add(time.Hour).Format(time.RFC3339), found[0].Metadata[skills.MetaUpdatedAt])
}

// The trust invariant: refining a hand-written skill must not relabel it as
// agent-authored, otherwise future automatic curation would happily rewrite
// something a human wrote.
func TestLearn_Refine_DoesNotLaunderHandWrittenProvenance(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	handWritten := filepath.Join(root, "team-conventions")
	require.NoError(t, os.MkdirAll(handWritten, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(handWritten, skills.SkillFileName), []byte(`---
name: team-conventions
description: Use when touching the payment service
---

Original hand-written body.
`), 0o644))

	deps := LearnDeps{Root: root, Now: fixedNow, Known: []*skills.Skill{
		{Name: "team-conventions", Description: "Use when touching the payment service",
			SkillFilePath: filepath.Join(handWritten, skills.SkillFileName)},
	}}

	resp := run(t, deps, `{"action":"refine","name":"team-conventions","instructions":"Improved body."}`)
	require.False(t, resp.IsError, resp.Content)

	found := skills.Discover([]string{root})
	require.Len(t, found, 1)
	assert.Equal(t, "Improved body.", found[0].Instructions)
	_, hasOrigin := found[0].Metadata[skills.MetaOrigin]
	assert.False(t, hasOrigin, "provenance must not be laundered into origin=learn")
}

func TestLearn_Refine_RefusesBundledSkill(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{Root: root, Known: []*skills.Skill{
		{Name: "jq", Builtin: true, SkillFilePath: "mocode://skills/jq/SKILL.md"},
	}}

	resp := run(t, deps, `{"action":"refine","name":"jq","instructions":"body"}`)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "no editable skill")
}

func TestLearn_Refine_UnknownSkill(t *testing.T) {
	t.Parallel()

	resp := run(t, LearnDeps{Root: t.TempDir()}, `{"action":"refine","name":"ghost","instructions":"b"}`)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "no editable skill")
}

// A skill discovered outside Root (for example in a project-scoped or shared
// directory) must be rewritten where it lives, not shadowed by a copy in Root.
func TestLearn_Refine_TargetsTheSkillActualDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	other := t.TempDir()
	elsewhere := filepath.Join(other, "shared-skill")
	require.NoError(t, os.MkdirAll(elsewhere, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, skills.SkillFileName), []byte(`---
name: shared-skill
description: Use when sharing
---

Original.
`), 0o644))

	deps := LearnDeps{Root: root, Now: fixedNow, Known: []*skills.Skill{
		{Name: "shared-skill", Description: "Use when sharing",
			SkillFilePath: filepath.Join(elsewhere, skills.SkillFileName)},
	}}

	resp := run(t, deps, `{"action":"refine","name":"shared-skill","instructions":"Rewritten in place."}`)
	require.False(t, resp.IsError, resp.Content)
	assert.Contains(t, resp.Content, elsewhere)

	// Rewritten where it lives…
	updated, err := os.ReadFile(filepath.Join(elsewhere, skills.SkillFileName))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "Rewritten in place.")

	// …and no shadowing copy was created in Root.
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestLearn_Refine_CanUpdateDescription(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{Root: root, Now: fixedNow}
	require.False(t, run(t, deps, createInput("deploy", "Use when deploying", "body")).IsError)

	resp := run(t, deps, `{"action":"refine","name":"deploy","description":"Use when rolling back a deploy","instructions":"body"}`)
	require.False(t, resp.IsError, resp.Content)

	found := skills.Discover([]string{root})
	require.Len(t, found, 1)
	assert.Equal(t, "Use when rolling back a deploy", found[0].Description)
}

func TestLearn_Refine_RejectsOffStyleDescription(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	deps := LearnDeps{Root: root, Now: fixedNow}
	require.False(t, run(t, deps, createInput("deploy", "Use when deploying", "body")).IsError)

	resp := run(t, deps, `{"action":"refine","name":"deploy","description":"Nope","instructions":"body"}`)
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "must start with")
}

// ─── permissions ─────────────────────────────────────────────────────────────

func TestLearn_Create_AsksForPermission(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gate := &fakeGate{allow: true}
	resp := run(t, LearnDeps{Root: root, Permissions: gate, Now: fixedNow},
		createInput("deploy", "Use when deploying", "body"))
	require.False(t, resp.IsError, resp.Content)

	require.Len(t, gate.calls, 1)
	call := gate.calls[0]
	assert.Equal(t, LearnToolName, call.ToolName)
	assert.Equal(t, "create", call.Action)
	assert.Equal(t, "call-1", call.ToolCallID)
	assert.Equal(t, filepath.Join(root, "deploy", skills.SkillFileName), call.Path)

	// The payload must carry the content so the UI can show what will be written.
	params, ok := call.Params.(LearnPermissionParams)
	require.True(t, ok)
	assert.Equal(t, "deploy", params.Name)
	assert.Contains(t, params.NewContent, "name: deploy")
	assert.Empty(t, params.OldContent)
}

func TestLearn_DeniedPermissionWritesNothing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gate := &fakeGate{allow: false}
	resp := run(t, LearnDeps{Root: root, Permissions: gate, Now: fixedNow},
		createInput("deploy", "Use when deploying", "body"))
	assert.True(t, resp.IsError)
	assert.Contains(t, resp.Content, "denied")

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "a denied write must leave the skills directory untouched")
}

func TestLearn_PermissionErrorIsHardFailure(t *testing.T) {
	t.Parallel()

	gate := &fakeGate{allow: true, err: errors.New("boom")}
	_, err := NewLearnTool(LearnDeps{Root: t.TempDir(), Permissions: gate, Now: fixedNow}).Run(
		context.Background(),
		fantasy.ToolCall{ID: "call-1", Name: LearnToolName,
			Input: createInput("deploy", "Use when deploying", "body")},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

// ─── schema ──────────────────────────────────────────────────────────────────

// The model only knows how to call this tool from the generated schema, so the
// action enum and the field descriptions must actually survive generation.
func TestLearn_SchemaExposesActionEnum(t *testing.T) {
	t.Parallel()

	info := NewLearnTool(LearnDeps{}).Info()
	assert.Equal(t, LearnToolName, info.Name)

	// ToolInfo.Parameters is a flat map of property name → property schema
	// (fantasy's schema.ToParameters drops the top-level "required" list, so
	// per-action requiredness is enforced in the handler instead).
	params := info.Parameters
	require.NotNil(t, params)

	action, ok := params["action"].(map[string]any)
	require.True(t, ok, "action must be part of the schema")
	assert.Equal(t, []any{"create", "refine", "list"}, action["enum"])
	assert.Contains(t, action["description"], "create writes a new skill")

	// Every field the handler reads must be advertised, or the model cannot
	// discover how to set it.
	for _, field := range []string{"name", "description", "instructions", "reason"} {
		spec, ok := params[field].(map[string]any)
		require.Truef(t, ok, "field %q missing from the generated schema", field)
		assert.NotEmptyf(t, spec["description"], "field %q needs a description for the model", field)
	}
}

// The embedded description is the tool's only documentation for the model;
// guard against it being emptied or truncated to a useless first line.
func TestLearn_DescriptionIsUsable(t *testing.T) {
	t.Parallel()

	desc := NewLearnTool(LearnDeps{}).Info().Description
	assert.NotEmpty(t, desc)
	assert.Greater(t, len(desc), 40, "description should orient the model, got %q", desc)
	assert.False(t, strings.HasPrefix(desc, "//"), "the //go:embed directive must not leak into the description")
}
