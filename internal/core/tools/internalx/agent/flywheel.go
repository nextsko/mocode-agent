package agenttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/flywheel"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
)

//go:embed flywheel.md
var flywheelDescription []byte

// FlywheelToolName is the registered name of the flywheel tool.
const FlywheelToolName = "flywheel"

// FlywheelParams controls the capability flywheel.
type FlywheelParams struct {
	Action   string          `json:"action" enum:"archive,distill,skill,use,report,export,context" description:"archive stores a task; distill shows its shortest path; skill proposes/updates a skill; use records reuse; report/export show the flywheel; context returns a compact digest."`
	TaskID   string          `json:"task_id,omitempty" description:"Task id for distill/skill; defaults to the latest archived task."`
	Type     string          `json:"type,omitempty" description:"Task type, e.g. ui-release, refactor, debugging."`
	Goal     string          `json:"goal,omitempty" description:"One-line goal of the archived task."`
	Outcome  string          `json:"outcome,omitempty" enum:"success,partial,failed" description:"Task outcome."`
	Steps    []flywheel.Step `json:"steps,omitempty" description:"Actions taken; mark failed attempts so they become pitfalls."`
	Lessons  []string        `json:"lessons,omitempty" description:"What should be remembered."`
	Name     string          `json:"name,omitempty" description:"Skill name for action=use."`
	Success  bool            `json:"success,omitempty" description:"Whether the reused skill worked."`
	MaxChars int             `json:"max_chars,omitempty" description:"For action=context: digest budget (default 600)."`
}

// NewFlywheelPortTool builds the flywheel tool for one project working dir.
func NewFlywheelPortTool(workingDir string) contract.Tool {
	return contract.NewFunc(
		FlywheelToolName,
		"Archive completed work, distil the shortest successful path and evolve reusable skills.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []string{"archive", "distill", "skill", "use", "report", "export", "context"},
					"description": "archive stores a task; distill shows its shortest path; skill proposes/updates a skill; use records reuse; report/export show the flywheel; context returns a compact digest.",
				},
				"task_id": map[string]any{"type": "string", "description": "Task id for distill/skill; defaults to the latest archived task."},
				"type":    map[string]any{"type": "string", "description": "Task type, e.g. ui-release, refactor, debugging."},
				"goal":    map[string]any{"type": "string", "description": "One-line goal of the archived task."},
				"outcome": map[string]any{"type": "string", "enum": []string{"success", "partial", "failed"}, "description": "Task outcome."},
				"steps": map[string]any{
					"type":        "array",
					"description": "Actions taken; mark failed attempts so they become pitfalls.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"action":      map[string]any{"type": "string"},
							"target":      map[string]any{"type": "string"},
							"command":     map[string]any{"type": "string"},
							"result":      map[string]any{"type": "string"},
							"failed":      map[string]any{"type": "boolean"},
							"retries":     map[string]any{"type": "integer"},
							"duration_ms": map[string]any{"type": "integer"},
						},
						"required": []string{"action"},
					},
				},
				"lessons":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "What should be remembered."},
				"name":      map[string]any{"type": "string", "description": "Skill name for action=use."},
				"success":   map[string]any{"type": "boolean", "description": "Whether the reused skill worked."},
				"max_chars": map[string]any{"type": "integer", "description": "For action=context: digest budget (default 600)."},
			},
			"required": []string{"action"},
		},
		func(_ context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
			var params FlywheelParams
			if len(args) > 0 {
				if err := json.Unmarshal(args, &params); err != nil {
					return contract.ToolResult{}, fmt.Errorf("invalid parameters: %w", err)
				}
			}
			return runFlywheel(workingDir, params)
		},
	)
}

func runFlywheel(workingDir string, p FlywheelParams) (contract.ToolResult, error) {
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "archive":
		task := flywheel.TaskRun{
			Type: p.Type, Goal: p.Goal, Outcome: p.Outcome,
			Steps: p.Steps, Lessons: p.Lessons,
		}
		task.StartedAt, task.EndedAt = nowUTC(), nowUTC()
		path, err := flywheel.SaveTask(task, workingDir)
		if err != nil {
			return contract.Error("failed to archive task: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		distilled := flywheel.Distill(task.Steps)
		return contract.Text(fmt.Sprintf("Archived task %s (%d steps → %d-step optimal path).\nSaved: `%s`", task.ID, len(task.Steps), len(distilled.Steps), path)), nil

	case "distill":
		task, err := pickTask(workingDir, p.TaskID)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
		path := flywheel.Distill(task.Steps)
		var b strings.Builder
		fmt.Fprintf(&b, "Optimal path for %q (%d → %d steps, −%.0f%%):\n", task.Goal, len(task.Steps), len(path.Steps), path.SavingsPct)
		for i, s := range path.Steps {
			label := s.Action
			if s.Target != "" {
				label += " " + s.Target
			}
			if s.Command != "" {
				label += " `" + s.Command + "`"
			}
			fmt.Fprintf(&b, "%d. %s\n", i+1, label)
		}
		return contract.Text(b.String()), nil

	case "skill":
		task, err := pickTask(workingDir, p.TaskID)
		if err != nil {
			return contract.Error(err.Error()), nil
		}
		decision, err := flywheel.UpsertSkill(flywheel.DraftFromTask(task), workingDir)
		if err != nil {
			return contract.Error("failed to upsert skill: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		name := ""
		if decision.Skill != nil {
			name = decision.Skill.Name
		}
		return contract.Text(fmt.Sprintf("Skill %s: %s (v%d) — %s", name, decision.Kind, decision.Version, decision.Reason)), nil

	case "use":
		if p.Name == "" {
			return contract.Error("name is required for action=use"), nil
		}
		skill, ok, err := flywheel.RecordUse(p.Name, p.Success, workingDir)
		if err != nil {
			return contract.Error("failed to record use: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		if !ok {
			return contract.Error("skill not found: " + p.Name), nil
		}
		return contract.Text(fmt.Sprintf("Recorded use of %s: uses=%d successes=%d", skill.Name, skill.Uses, skill.Successes)), nil

	case "report":
		tasks, _ := flywheel.LoadTasks(workingDir)
		skills, _ := flywheel.LoadSkills(workingDir)
		report := flywheel.BuildReport(tasks, skills)
		text := flywheel.RenderReport(report) + "\n```mermaid\n" + flywheel.RenderMermaid(tasks, skills) + "```\n"
		return contract.Text(text), nil

	case "export":
		dir, err := flywheel.ExportBundle(workingDir)
		if err != nil {
			return contract.Error("failed to export: " + err.Error()), nil //nolint:nilerr // tool-level error is reported as text to the model.
		}
		return contract.Text("Flywheel exported: `" + dir + "` (report.md, report.json, graph.mmd)"), nil

	case "context":
		budget := p.MaxChars
		if budget <= 0 {
			budget = 600
		}
		return contract.Text(flywheel.Digest(workingDir, budget)), nil

	default:
		return contract.Error("action must be one of: archive, distill, skill, use, report, export, context"), nil
	}
}

func nowUTC() time.Time { return time.Now().UTC() }

func pickTask(workingDir, id string) (flywheel.TaskRun, error) {
	tasks, err := flywheel.LoadTasks(workingDir)
	if err != nil {
		return flywheel.TaskRun{}, fmt.Errorf("load tasks: %w", err)
	}
	if len(tasks) == 0 {
		return flywheel.TaskRun{}, fmt.Errorf("no archived tasks yet; run action=archive first")
	}
	if id == "" {
		return tasks[len(tasks)-1], nil
	}
	for _, t := range tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return flywheel.TaskRun{}, fmt.Errorf("task %q not found", id)
}
