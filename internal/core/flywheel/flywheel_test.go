package flywheel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleTask() TaskRun {
	now := time.Now().UTC()
	return TaskRun{
		ID: "t1", Type: "ui-release", Goal: "publish a UI release with tests", Outcome: "success",
		Steps: []Step{
			{Action: "run", Target: "tests", Failed: true, Result: "flaky", Retries: 1},
			{Action: "run", Target: "tests", Command: "go test ./..."},
			{Action: "run", Target: "tests", Command: "go test ./..."},
			{Action: "build", Target: "binary"},
			{Action: "tag", Target: "v1"},
		},
		Lessons:   []string{"run tests twice before tagging"},
		StartedAt: now.Add(-time.Minute), EndedAt: now,
	}
}

func TestDistillKeepsShortestSuccessfulPath(t *testing.T) {
	t.Parallel()
	task := sampleTask()
	path := Distill(task.Steps)
	require.Len(t, path.Steps, 3)
	assert.Equal(t, 2, path.Dropped)
	assert.Greater(t, path.SavingsPct, 0.0)
}

func TestUpsertSkillLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	task := sampleTask()
	_, err := SaveTask(task, dir)
	require.NoError(t, err)
	draft := DraftFromTask(task)

	d, err := UpsertSkill(draft, dir)
	require.NoError(t, err)
	assert.Equal(t, DecisionCreated, d.Kind)
	assert.Equal(t, 1, d.Version)

	// Same draft again: nothing new -> skipped.
	d2, err := UpsertSkill(draft, dir)
	require.NoError(t, err)
	assert.Equal(t, DecisionSkipped, d2.Kind)

	// A shorter path -> upgraded.
	short := task
	short.Steps = []Step{{Action: "build", Target: "binary"}}
	up, err := UpsertSkill(DraftFromTask(short), dir)
	require.NoError(t, err)
	assert.Equal(t, DecisionUpgraded, up.Kind)
	assert.Equal(t, 2, up.Version)
}

func TestRecordUseAndReportAndDigest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	task := sampleTask()
	_, err := SaveTask(task, dir)
	require.NoError(t, err)
	d, err := UpsertSkill(DraftFromTask(task), dir)
	require.NoError(t, err)

	s, ok, err := RecordUse(d.Skill.Name, true, dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 1, s.Uses)
	assert.Equal(t, 1, s.Successes)

	tasks, err := LoadTasks(dir)
	require.NoError(t, err)
	skills, err := LoadSkills(dir)
	require.NoError(t, err)
	report := BuildReport(tasks, skills)
	assert.Equal(t, 1, report.Tasks)
	assert.Equal(t, 1, report.Skills)
	assert.Contains(t, RenderReport(report), "能力飞轮")
	assert.Contains(t, RenderMermaid(tasks, skills), "graph LR")
	assert.Contains(t, Digest(dir, 400), "Flywheel:")

	exportDir, err := ExportBundle(dir)
	require.NoError(t, err)
	for _, name := range []string{"report.md", "report.json", "graph.mmd"} {
		_, statErr := os.Stat(filepath.Join(exportDir, name))
		require.NoError(t, statErr, name)
	}
}

func TestDraftDescriptionStartsWithUseWhen(t *testing.T) {
	t.Parallel()
	d := DraftFromTask(sampleTask())
	assert.True(t, strings.HasPrefix(d.Description, "Use when"))
	assert.NotEmpty(t, d.Instructions)
}
