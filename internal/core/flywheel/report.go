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

// BuildReport summarises the flywheel's current state.
func BuildReport(tasks []TaskRun, skills []Skill) Report {
	r := Report{GeneratedAt: time.Now().UTC(), Tasks: len(tasks), Skills: len(skills)}
	steps, ok := 0, 0
	for _, t := range tasks {
		if t.Outcome == "success" {
			ok++
		}
		steps += len(Distill(t.Steps).Steps)
		if len(t.Lessons) > 0 {
			r.Reused += 0
		}
	}
	if ok > 0 && len(tasks) > 0 {
		r.AvgSteps = float64(steps) / float64(ok)
	}
	for _, s := range skills {
		r.Reused += s.Uses
	}
	r.BestSkills = append([]Skill{}, skills...)
	sort.SliceStable(r.BestSkills, func(i, j int) bool {
		si := scoreSkill(r.BestSkills[i])
		sj := scoreSkill(r.BestSkills[j])
		return si > sj
	})
	if len(r.BestSkills) > 5 {
		r.BestSkills = r.BestSkills[:5]
	}
	r.RecentTasks = append([]TaskRun{}, tasks...)
	if len(r.RecentTasks) > 5 {
		r.RecentTasks = r.RecentTasks[len(r.RecentTasks)-5:]
	}
	return r
}

func scoreSkill(s Skill) float64 {
	rate := 0.0
	if s.Uses > 0 {
		rate = float64(s.Successes) / float64(s.Uses)
	}
	return float64(s.Uses)*(1+rate) + float64(s.Version)*0.5
}

// RenderReport renders the flywheel report.
func RenderReport(r Report) string {
	var b strings.Builder
	b.WriteString("# 能力飞轮\n\n")
	fmt.Fprintf(&b, "- Tasks archived: %d\n- Skills: %d\n- Skill reuses: %d\n- Avg optimal path: %.1f steps\n- Generated: %s\n\n", r.Tasks, r.Skills, r.Reused, r.AvgSteps, r.GeneratedAt.Format(time.RFC3339))

	b.WriteString("## 技能排行\n\n")
	if len(r.BestSkills) == 0 {
		b.WriteString("_No skills yet. Archive a task, then distill it._\n")
	} else {
		b.WriteString("| Skill | Type | v | Uses | Success | Path |\n|---|---|---|---|---|---|\n")
		for _, s := range r.BestSkills {
			rate := "-"
			if s.Uses > 0 {
				rate = fmt.Sprintf("%.0f%%", float64(s.Successes)/float64(s.Uses)*100)
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %d |\n", s.Name, s.Type, s.Version, s.Uses, rate, len(s.Path))
		}
	}

	b.WriteString("\n## 最近任务\n\n")
	if len(r.RecentTasks) == 0 {
		b.WriteString("_None._\n")
	} else {
		b.WriteString("| Task | Type | Outcome | Optimal |\n|---|---|---|---|\n")
		for _, t := range r.RecentTasks {
			fmt.Fprintf(&b, "| %s | %s | %s | %d steps |\n", truncate(t.Goal, 50), t.Type, t.Outcome, len(Distill(t.Steps).Steps))
		}
	}
	b.WriteString("\n## 飞轮含义\n\n")
	b.WriteString("- 任务完成后归档 → 抽最优路径 → 技能判定（新建/更新/升级/跳过）→ 复用记分 → 下一轮更快。\n")
	b.WriteString("- 技能只在“路径更短”或“有新经验”时升级；无新增则跳过，避免版本通胀。\n")
	return b.String()
}

// RenderMermaid renders the skill/task graph for visualization.
func RenderMermaid(tasks []TaskRun, skills []Skill) string {
	var b strings.Builder
	b.WriteString("graph LR\n")
	byType := map[string][]TaskRun{}
	for _, t := range tasks {
		byType[t.Type] = append(byType[t.Type], t)
	}
	for _, t := range tasks {
		fmt.Fprintf(&b, "  T_%s[\"%s\"]\n", safeID(t.ID), truncate(t.Goal, 32))
	}
	for _, s := range skills {
		fmt.Fprintf(&b, "  S_%s[\"%s v%d\"]\n", safeID(s.Name), s.Name, s.Version)
	}
	for _, s := range skills {
		for _, t := range byType[s.Type] {
			fmt.Fprintf(&b, "  T_%s --> S_%s\n", safeID(t.ID), safeID(s.Name))
		}
	}
	return b.String()
}

func safeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// ExportBundle writes the report, JSON and Mermaid graph for external
// consumption/visualization.
func ExportBundle(workingDir string) (string, error) {
	tasks, err := LoadTasks(workingDir)
	if err != nil {
		return "", err
	}
	skills, err := LoadSkills(workingDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(Dir(workingDir), "exports", time.Now().UTC().Format("20060102T150405"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create export dir: %w", err)
	}
	report := BuildReport(tasks, skills)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(RenderReport(report)), 0o600); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), raw, 0o600); err != nil {
		return "", fmt.Errorf("write report json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph.mmd"), []byte(RenderMermaid(tasks, skills)), 0o600); err != nil {
		return "", fmt.Errorf("write mermaid graph: %w", err)
	}
	return dir, nil
}

// Digest returns a compact, context-friendly summary the model can pull in
// when it wants to regulate its own context instead of carrying full history.
func Digest(workingDir string, maxChars int) string {
	tasks, _ := LoadTasks(workingDir)
	skills, _ := LoadSkills(workingDir)
	var b strings.Builder
	fmt.Fprintf(&b, "Flywheel: %d tasks, %d skills. ", len(tasks), len(skills))
	for i := len(skills) - 1; i >= 0 && i >= len(skills)-5; i-- {
		s := skills[i]
		fmt.Fprintf(&b, "[%s v%d uses=%d] ", s.Name, s.Version, s.Uses)
	}
	if len(tasks) > 0 {
		last := tasks[len(tasks)-1]
		path := Distill(last.Steps)
		fmt.Fprintf(&b, "Last task %q (%s): %d-step path.", truncate(last.Goal, 40), last.Outcome, len(path.Steps))
	}
	out := b.String()
	if maxChars > 0 && len(out) > maxChars {
		out = out[:maxChars] + "…"
	}
	return out
}
