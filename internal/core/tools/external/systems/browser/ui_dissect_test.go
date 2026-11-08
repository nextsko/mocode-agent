package browser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUIDissectTool_ListWithoutMaps(t *testing.T) {
	t.Parallel()
	tool := NewUIDissectPortTool(nil, t.TempDir())
	require.Equal(t, UIDissectToolName, tool.Name())

	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"list"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "No UI maps saved yet")
}

func TestUIDissectTool_RejectsUnknownAction(t *testing.T) {
	t.Parallel()
	tool := NewUIDissectPortTool(nil, t.TempDir())
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"nope"}`))
	require.NoError(t, err)
	require.Error(t, res.Error)
	assert.Contains(t, res.Error.Error(), "action must be one of")
}

func TestUIDissectTool_SchemaExposesActions(t *testing.T) {
	t.Parallel()
	schema := NewUIDissectPortTool(nil, t.TempDir()).Schema()
	props, ok := schema.Parameters["properties"].(map[string]any)
	require.True(t, ok)
	action, ok := props["action"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"map", "review", "replicate", "diff", "system", "brand", "tokens", "skill", "dynamic", "retro", "evolve", subActionList}, action["enum"])
}

// TestUIDissectTool_RealURL is an opt-in integration check: set
// UIDISSECT_REAL_URL to a page and the tool is exercised end to end (static
// HTML fallback when no browser is available).
func TestUIDissectTool_RealURL(t *testing.T) {
	url := os.Getenv("UIDISSECT_REAL_URL")
	if url == "" {
		t.Skip("set UIDISSECT_REAL_URL to run the real-page check")
	}
	tool := NewUIDissectPortTool(nil, t.TempDir())
	body := []byte(`{"action":"map","url":"` + url + `","save":false}`)
	res, err := tool.Execute(context.Background(), nil, body)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "庖丁解牛")
	assert.Contains(t, res.Content, "组件切片")
}

// TestUIDissectSystem_RealPages maps two real URLs into one artifact dir and
// mines the site design system across them.
func TestUIDissectSystem_RealPages(t *testing.T) {
	u1 := os.Getenv("UIDISSECT_REAL_URL")
	u2 := os.Getenv("UIDISSECT_REAL_URL2")
	if u1 == "" || u2 == "" {
		t.Skip("set UIDISSECT_REAL_URL and UIDISSECT_REAL_URL2 to run the cross-page check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)
	for _, u := range []string{u1, u2} {
		res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"map","url":"`+u+`"}`))
		require.NoError(t, err)
		require.NoError(t, res.Error)
	}
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"system"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "站点设计系统")
	assert.Contains(t, res.Content, "Pages: 2")
}

// TestUIDissectDiff_RealPages maps two real pages and diffs the latest two.
func TestUIDissectDiff_RealPages(t *testing.T) {
	u1 := os.Getenv("UIDISSECT_REAL_URL")
	u2 := os.Getenv("UIDISSECT_REAL_URL2")
	if u1 == "" || u2 == "" {
		t.Skip("set UIDISSECT_REAL_URL and UIDISSECT_REAL_URL2 to run the diff check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)
	for _, u := range []string{u1, u2} {
		res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"map","url":"`+u+`"}`))
		require.NoError(t, err)
		require.NoError(t, res.Error)
	}
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"diff"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "UI 视觉回归")
	assert.NotContains(t, res.Content, "Need at least two saved maps")
}

// TestUIDissectTokens_RealPages captures a brand from one real page and
// compares a second real page against it.
func TestUIDissectTokens_RealPages(t *testing.T) {
	u1 := os.Getenv("UIDISSECT_REAL_URL")
	u2 := os.Getenv("UIDISSECT_REAL_URL2")
	if u1 == "" || u2 == "" {
		t.Skip("set UIDISSECT_REAL_URL and UIDISSECT_REAL_URL2 to run the token check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"brand","url":"`+u1+`"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "Brand captured")

	res, err = tool.Execute(context.Background(), nil, []byte(`{"action":"tokens","url":"`+u2+`"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "品牌令牌对比")
}

// TestUIDissectReplicate_RealPage replicates a component from a real page in
// every framework target.
func TestUIDissectReplicate_RealPage(t *testing.T) {
	url := os.Getenv("UIDISSECT_REAL_URL")
	if url == "" {
		t.Skip("set UIDISSECT_REAL_URL to run the replicate check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)

	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"map","url":"`+url+`","save":false}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	require.Contains(t, res.Content, "| c1 |", "map must expose component ids")

	for _, target := range []string{"html", "react", "vue", "svelte"} {
		body := []byte(`{"action":"replicate","component":"c1","target":"` + target + `","url":"` + url + `"}`)
		res, err := tool.Execute(context.Background(), nil, body)
		require.NoError(t, err)
		require.NoError(t, res.Error)
		assert.Contains(t, res.Content, "skeleton")
		assert.Contains(t, res.Content, target)
	}
}

// TestUIDissectSkill_RealPage stages a skill draft from a real page.
func TestUIDissectSkill_RealPage(t *testing.T) {
	url := os.Getenv("UIDISSECT_REAL_URL")
	if url == "" {
		t.Skip("set UIDISSECT_REAL_URL to run the skill check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)
	body := []byte(`{"action":"skill","url":"` + url + `"}`)
	res, err := tool.Execute(context.Background(), nil, body)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "Skill draft staged")
	assert.Contains(t, res.Content, "Use when")
}

func TestUIDissectTool_DynamicNeedsBrowser(t *testing.T) {
	t.Parallel()
	tool := NewUIDissectPortTool(nil, t.TempDir())
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"dynamic","steps":[{"action":"click","target":"#x"}]}`))
	require.NoError(t, err)
	require.Error(t, res.Error)
	assert.Contains(t, res.Error.Error(), "requires a live browser")
}

func TestUIDissectTool_RetroWithoutMaps(t *testing.T) {
	t.Parallel()
	tool := NewUIDissectPortTool(nil, t.TempDir())
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"retro"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "No saved maps yet")
}

// TestUIDissectRetro_RealPages maps two real pages and reviews the taste.
func TestUIDissectRetro_RealPages(t *testing.T) {
	u1 := os.Getenv("UIDISSECT_REAL_URL")
	u2 := os.Getenv("UIDISSECT_REAL_URL2")
	if u1 == "" || u2 == "" {
		t.Skip("set UIDISSECT_REAL_URL and UIDISSECT_REAL_URL2 to run the retrospective check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)
	for _, u := range []string{u1, u2} {
		res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"map","url":"`+u+`"}`))
		require.NoError(t, err)
		require.NoError(t, res.Error)
	}
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"retro"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "审美复盘")
	assert.Contains(t, res.Content, "Pages: 2")
}

// TestUIDissectEvolve_RealPages runs map → diff → retro → evolve on real pages.
func TestUIDissectEvolve_RealPages(t *testing.T) {
	u1 := os.Getenv("UIDISSECT_REAL_URL")
	u2 := os.Getenv("UIDISSECT_REAL_URL2")
	if u1 == "" || u2 == "" {
		t.Skip("set UIDISSECT_REAL_URL and UIDISSECT_REAL_URL2 to run the evolve check")
	}
	dir := t.TempDir()
	tool := NewUIDissectPortTool(nil, dir)
	for _, u := range []string{u1, u2} {
		res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"map","url":"`+u+`"}`))
		require.NoError(t, err)
		require.NoError(t, res.Error)
	}
	res, err := tool.Execute(context.Background(), nil, []byte(`{"action":"diff"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "UI 视觉回归")

	evo, err := os.ReadFile(filepath.Join(dir, ".mocode", "ui", "evolution.jsonl"))
	require.NoError(t, err, "diff must append to the improvement log")
	assert.Contains(t, string(evo), "delta")

	res, err = tool.Execute(context.Background(), nil, []byte(`{"action":"retro"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "进化记录")

	res, err = tool.Execute(context.Background(), nil, []byte(`{"action":"evolve"}`))
	require.NoError(t, err)
	require.NoError(t, res.Error)
	assert.Contains(t, res.Content, "Rulebook skill staged")
	assert.Contains(t, res.Content, "ui-taste-rules")
}
