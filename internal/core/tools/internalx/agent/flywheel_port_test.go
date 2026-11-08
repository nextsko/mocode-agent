package agenttools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextsko/mocode-agent/internal/core/flywheel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runFlywheelTool(t *testing.T, dir string, params FlywheelParams) string {
	t.Helper()
	tool := NewFlywheelPortTool(dir)
	require.Equal(t, FlywheelToolName, tool.Name())
	body, err := json.Marshal(params)
	require.NoError(t, err)
	res, err := tool.Execute(context.Background(), nil, body)
	require.NoError(t, err)
	require.NoError(t, res.Error)
	return res.Content
}

func TestFlywheelPortToolLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out := runFlywheelTool(t, dir, FlywheelParams{
		Action: "archive", Type: "ui-release", Goal: "publish a UI release", Outcome: "success",
		Steps: []flywheel.Step{
			{Action: "run", Target: "tests", Failed: true, Result: "flaky"},
			{Action: "run", Target: "tests", Command: "go test ./..."},
			{Action: "build", Target: "binary"},
		},
		Lessons: []string{"run tests twice"},
	})
	assert.Contains(t, out, "Archived task")

	out = runFlywheelTool(t, dir, FlywheelParams{Action: "distill"})
	assert.Contains(t, out, "Optimal path")
	assert.Contains(t, out, "go test ./...")

	out = runFlywheelTool(t, dir, FlywheelParams{Action: "skill"})
	assert.Contains(t, out, "created")

	out = runFlywheelTool(t, dir, FlywheelParams{Action: "skill"})
	assert.Contains(t, out, "skipped")

	out = runFlywheelTool(t, dir, FlywheelParams{Action: "report"})
	assert.Contains(t, out, "能力飞轮")
	assert.Contains(t, out, "graph LR")

	out = runFlywheelTool(t, dir, FlywheelParams{Action: "context"})
	assert.Contains(t, out, "Flywheel:")

	out = runFlywheelTool(t, dir, FlywheelParams{Action: "export"})
	assert.Contains(t, out, "Flywheel exported")
}
