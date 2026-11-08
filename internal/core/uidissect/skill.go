package uidissect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillDraft turns a dissection into a skill proposal: a name, a trigger
// description and the full SKILL.md body. It does not write to the skill
// library; callers hand the payload to the `learn` tool so the normal
// permission gate and provenance handling apply.
func SkillDraft(m Map, comp *Component) (name, description, body string) {
	base := firstNonEmpty(m.Title, m.URL, "ui")
	name = slugify(base + "-ui-dissection")
	description = fmt.Sprintf("Use when reviewing, replicating or restyling the UI of %s (%s)", firstNonEmpty(m.Title, "this page"), m.URL)

	var b strings.Builder
	b.WriteString("## 何时使用\n\n")
	fmt.Fprintf(&b, "当你需要审查、复刻或改造 `%s` 的界面时使用。\n\n", m.URL)
	b.WriteString("## 操作步骤\n\n")
	b.WriteString("1. 用 `browser` 打开目标页面；\n")
	b.WriteString("2. 用 `ui_dissect action=map` 取结构地图与组件切片；\n")
	b.WriteString("3. 用 `ui_dissect action=review` 取审美 findings；\n")
	b.WriteString("4. 用 `ui_dissect action=replicate component=cN target=react|vue|svelte` 生成骨架；\n")
	b.WriteString("5. 改完后再次 `map` + `diff` 验证视觉回归。\n\n")

	b.WriteString("## 组件清单\n\n")
	b.WriteString("| ID | Kind | Box | Nodes | Repeat |\n|---|---|---|---|---|\n")
	for _, c := range m.Components {
		if comp != nil && c.ID != comp.ID {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s | %sx%s | %d | %d |\n", c.ID, c.Kind, trimFloat(c.Rect.W), trimFloat(c.Rect.H), c.NodeCount, c.Repeat)
	}
	b.WriteString("\n## 设计令牌\n\n")
	fmt.Fprintf(&b, "- Font sizes: %s\n", strings.Join(floatsToStrings(m.Tokens.FontSizes), ", "))
	fmt.Fprintf(&b, "- Colors: %s\n", strings.Join(m.Tokens.Colors, ", "))
	fmt.Fprintf(&b, "- Spacing: %s\n\n", strings.Join(floatsToStrings(m.Tokens.Spacing), ", "))

	if len(m.Findings) > 0 {
		b.WriteString("## 已知问题\n\n")
		for i, f := range m.Findings {
			if i >= 12 {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s — %s\n", f.Severity, f.Message, f.Suggestion)
		}
		b.WriteString("\n")
	}

	b.WriteString("## 证据\n\n")
	fmt.Fprintf(&b, "- 页面: %s\n- 采集节点: %d · 组件: %d · findings: %d\n", m.URL, m.Stats.Nodes, m.Stats.Components, m.Stats.Findings)
	b.WriteString("- 地图与复刻产物位于 `.mocode/ui/`。\n")
	return name, description, b.String()
}

// RenderSkillMarkdown renders a complete SKILL.md.
func RenderSkillMarkdown(name, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n\n%s\n", name, description, name, body)
}

// SaveSkillDraft stages the proposal under .mocode/ui/skills so it survives the
// session; promoting it into the skill library is the `learn` tool's job.
func SaveSkillDraft(m Map, comp *Component, workingDir string) (string, error) {
	name, description, body := SkillDraft(m, comp)
	dir := filepath.Join(Dir(workingDir), "skills", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create skill draft dir: %w", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(RenderSkillMarkdown(name, description, body)), 0o600); err != nil {
		return "", fmt.Errorf("write skill draft: %w", err)
	}
	return path, nil
}

// RulesSkillDraft turns the retrospective into a reusable project aesthetic
// rulebook skill: lessons first, evidence second.
func RulesSkillDraft(t Taste) (name, description, body string) {
	name = "ui-taste-rules"
	description = "Use when reviewing, replicating or changing UI: the project's aesthetic rules, learned from past reviews and regression runs"

	var b strings.Builder
	b.WriteString("## 何时使用\n\n")
	b.WriteString("在评审、复刻或改造界面之前先读这份规则；每条规则都来自实际页面的重复问题。\n\n")

	b.WriteString("## 规则\n\n")
	for i, l := range t.Lessons {
		fmt.Fprintf(&b, "%d. %s\n", i+1, l)
	}
	b.WriteString("\n## 重复问题证据\n\n")
	if len(t.Recurring) == 0 {
		b.WriteString("_暂无累积证据。_\n")
	} else {
		b.WriteString("| Category | Count | Pages |\n|---|---|---|\n")
		for _, r := range t.Recurring {
			fmt.Fprintf(&b, "| %s | %d | %d |\n", r.Category, r.Count, r.Pages)
		}
	}
	b.WriteString("\n## 回归纪律\n\n")
	fmt.Fprintf(&b, "- 已记录回归 %d 次：改善 %d、退化 %d、平均 Δ %.1f。\n", t.EvolutionSt.Runs, t.EvolutionSt.Improved, t.EvolutionSt.Degraded, t.EvolutionSt.AvgDelta)
	b.WriteString("- 每次改版后执行 `ui_dissect action=diff`，只有 Δ ≥ 0 才算通过审美验收。\n")
	b.WriteString("- 新的重复问题要追加到本规则，而不是只修当前页面。\n\n")

	b.WriteString("## 工作流\n\n")
	b.WriteString("1. `map` 取结构与令牌；\n2. `review` 找问题；\n3. 按上面的规则修改；\n4. `diff` 记录分数变化；\n5. `retro` 复盘并把新规则沉淀回来。\n")
	return name, description, b.String()
}

// SaveRulesSkill stages the rulebook skill under .mocode/ui/skills.
func SaveRulesSkill(t Taste, workingDir string) (string, error) {
	name, description, body := RulesSkillDraft(t)
	dir := filepath.Join(Dir(workingDir), "skills", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create rules skill dir: %w", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(RenderSkillMarkdown(name, description, body)), 0o600); err != nil {
		return "", fmt.Errorf("write rules skill: %w", err)
	}
	return path, nil
}
