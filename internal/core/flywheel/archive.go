package flywheel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RootDir is where the flywheel keeps archives, skills and reports.
const RootDir = ".mocode/flywheel"

// Dir returns the flywheel root for a project.
func Dir(workingDir string) string { return filepath.Join(workingDir, RootDir) }

// SaveTask archives one run as JSON + Markdown.
func SaveTask(t TaskRun, workingDir string) (string, error) {
	if t.ID == "" {
		t.ID = fmt.Sprintf("%s-%d", t.StartedAt.UTC().Format("20060102T150405"), time.Now().UnixNano()%1e6)
	}
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now().UTC()
	}
	if t.EndedAt.IsZero() {
		t.EndedAt = t.StartedAt
	}
	dir := filepath.Join(Dir(workingDir), "tasks", t.StartedAt.UTC().Format("2006-01"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create task dir: %w", err)
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal task: %w", err)
	}
	jsonPath := filepath.Join(dir, t.ID+".json")
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		return "", fmt.Errorf("write task json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, t.ID+".md"), []byte(RenderTask(t)), 0o600); err != nil {
		return "", fmt.Errorf("write task markdown: %w", err)
	}
	return jsonPath, nil
}

// LoadTasks reads every archived task, oldest first.
func LoadTasks(workingDir string) ([]TaskRun, error) {
	root := filepath.Join(Dir(workingDir), "tasks")
	var out []TaskRun
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil //nolint:nilerr // a vanished/unreadable entry is skipped, not fatal
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // path is produced by WalkDir under the project root
		if readErr != nil {
			return nil //nolint:nilerr // skip unreadable archive entries
		}
		var t TaskRun
		if json.Unmarshal(raw, &t) != nil {
			return nil //nolint:nilerr // skip corrupt archive entries
		}
		out = append(out, t)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("walk tasks: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// RenderTask renders one archive entry.
func RenderTask(t TaskRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 任务归档 · %s\n\n", t.Goal)
	fmt.Fprintf(&b, "- ID: %s\n- Type: %s\n- Outcome: %s\n- Ran: %s → %s\n\n", t.ID, t.Type, t.Outcome, t.StartedAt.Format(time.RFC3339), t.EndedAt.Format(time.RFC3339))
	b.WriteString("## 实际路径\n\n")
	for i, s := range t.Steps {
		mark := "✓"
		if s.Failed {
			mark = "✗"
		}
		label := s.Action
		if s.Target != "" {
			label += " " + s.Target
		}
		if s.Command != "" {
			label += " `" + s.Command + "`"
		}
		fmt.Fprintf(&b, "%d. %s %s", i+1, mark, label)
		if s.Result != "" {
			fmt.Fprintf(&b, " → %s", truncate(s.Result, 80))
		}
		b.WriteString("\n")
	}
	path := Distill(t.Steps)
	b.WriteString("\n## 最优路径\n\n")
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
	fmt.Fprintf(&b, "\n_去掉 %d 个失败/重试步骤，节省 %.0f%%._\n", path.Dropped, path.SavingsPct)
	if len(t.Lessons) > 0 {
		b.WriteString("\n## 经验\n\n")
		for _, l := range t.Lessons {
			fmt.Fprintf(&b, "- %s\n", l)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
