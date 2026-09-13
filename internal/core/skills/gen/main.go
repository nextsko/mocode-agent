// Command gen renders docs/skills-and-roles/README.md from the builtin skills
// and the mode files. Run from the repo root:
//
//	go run ./internal/core/skills/gen
//
// The catalog is kept honest by gen_test.go, which fails when the committed
// file is stale.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/skills"
)

// categoryOrder is the display order of skill categories.
var categoryOrder = []string{
	"元技能 · 发现与自省",
	"规范与理念",
	"计划 · 协作 · 编排",
	"评审 · 质量 · 验收",
	"调试 · 调查 · 重构",
	"架构 · 领域 · 设计系统",
	"前端 · UI 库",
	"UI 设计方法学 · 视觉产出",
	"AI 对话 UI · LLM 集成",
	"后端 · 运行时 · 构建",
	"Shell · Windows 环境",
	"写作 · 文档 · 可视化产出",
	"研究 · 调研 · 抓取 · 复盘",
	"Git · 交付 · 安全",
	"Mocode 自身",
}

// categoryOf maps every builtin skill to a category. A skill missing here lands
// in "其他" — keep this in sync (gen_test.go fails on drift only for the doc).
var categoryOf = map[string]string{
	"using-agent-skills": "元技能 · 发现与自省", "find-skills": "元技能 · 发现与自省",
	"using-superpowers": "元技能 · 发现与自省", "writing-skills": "元技能 · 发现与自省",
	"dot-skill": "元技能 · 发现与自省", "deep-roles": "元技能 · 发现与自省",

	"docs-rulebook": "规范与理念", "common-rulebook": "规范与理念",
	"ooml-rulebook": "规范与理念", "karpathy-guidelines": "规范与理念",
	"lean-build": "规范与理念",

	"planning-with-files": "计划 · 协作 · 编排", "writing-plans": "计划 · 协作 · 编排",
	"executing-plans": "计划 · 协作 · 编排", "spec-driven-development": "计划 · 协作 · 编排",
	"to-spec": "计划 · 协作 · 编排", "brainstorming": "计划 · 协作 · 编排",
	"subagent-driven-development": "计划 · 协作 · 编排", "sub-team-dev": "计划 · 协作 · 编排",
	"dispatching-parallel-agents": "计划 · 协作 · 编排", "multi-agent-orchestration": "计划 · 协作 · 编排",
	"handoff": "计划 · 协作 · 编排",

	"code-review-and-quality": "评审 · 质量 · 验收", "requesting-code-review": "评审 · 质量 · 验收",
	"receiving-code-review": "评审 · 质量 · 验收", "multi-review": "评审 · 质量 · 验收",
	"verification-before-completion": "评审 · 质量 · 验收", "verify-and-stop": "评审 · 质量 · 验收",
	"triage": "评审 · 质量 · 验收", "surgical-patch": "评审 · 质量 · 验收",
	"bug-lesson": "评审 · 质量 · 验收", "test-driven-development": "评审 · 质量 · 验收",

	"systematic-debugging": "调试 · 调查 · 重构", "investigate-first": "调试 · 调查 · 重构",
	"incremental-implementation": "调试 · 调查 · 重构", "safe-refactor": "调试 · 调查 · 重构",

	"codebase-design": "架构 · 领域 · 设计系统", "domain-modeling": "架构 · 领域 · 设计系统",
	"improve-codebase-architecture": "架构 · 领域 · 设计系统", "design-md": "架构 · 领域 · 设计系统",
	"design-taste-frontend": "架构 · 领域 · 设计系统",

	"vercel-react-best-practices": "前端 · UI 库", "vercel-composition-patterns": "前端 · UI 库",
	"web-design-guidelines": "前端 · UI 库", "tailwindcss": "前端 · UI 库",
	"shadcn": "前端 · UI 库", "zustand": "前端 · UI 库", "zod": "前端 · UI 库",
	"ui-replication": "前端 · UI 库", "playwright-cli": "前端 · UI 库",

	"screenshot-to-ui": "UI 设计方法学 · 视觉产出", "css-layout-and-box-model": "UI 设计方法学 · 视觉产出",
	"design-tokens": "UI 设计方法学 · 视觉产出", "responsive-design": "UI 设计方法学 · 视觉产出",
	"motion-design": "UI 设计方法学 · 视觉产出", "ecommerce-image-studio": "UI 设计方法学 · 视觉产出",
	"interactive-h5-app": "UI 设计方法学 · 视觉产出", "red-team-animation-verification": "UI 设计方法学 · 视觉产出",

	"assistant-ui": "AI 对话 UI · LLM 集成", "primitives": "AI 对话 UI · LLM 集成",
	"runtime": "AI 对话 UI · LLM 集成", "streaming": "AI 对话 UI · LLM 集成",
	"cloud": "AI 对话 UI · LLM 集成", "observability": "AI 对话 UI · LLM 集成",
	"rig-core-llm-integration": "AI 对话 UI · LLM 集成",

	"rust-backend": "后端 · 运行时 · 构建", "rust-android-apk": "后端 · 运行时 · 构建",
	"tauri": "后端 · 运行时 · 构建", "bun": "后端 · 运行时 · 构建",
	"rsbuild": "后端 · 运行时 · 构建", "go-tools-mcp-master": "后端 · 运行时 · 构建",

	"powershell-on-windows": "Shell · Windows 环境", "nu-shell-helper": "Shell · Windows 环境",
	"pi-windows-setup": "Shell · Windows 环境", "windows-junction-link": "Shell · Windows 环境",
	"jq": "Shell · Windows 环境",

	"report-writer": "写作 · 文档 · 可视化产出", "docx": "写作 · 文档 · 可视化产出",
	"pptx": "写作 · 文档 · 可视化产出", "kami-pdf-workflow": "写作 · 文档 · 可视化产出",
	"stunning-html-slides": "写作 · 文档 · 可视化产出", "excalidraw-diagram": "写作 · 文档 · 可视化产出",
	"file-organizer": "写作 · 文档 · 可视化产出",

	"project-teardown": "研究 · 调研 · 抓取 · 复盘", "reverse-engineering-docs": "研究 · 调研 · 抓取 · 复盘",
	"fromsko-research": "研究 · 调研 · 抓取 · 复盘", "fromsko-knowledge": "研究 · 调研 · 抓取 · 复盘",
	"public-social-research": "研究 · 调研 · 抓取 · 复盘", "firecrawl": "研究 · 调研 · 抓取 · 复盘",
	"session-viz": "研究 · 调研 · 抓取 · 复盘", "exp-recorder": "研究 · 调研 · 抓取 · 复盘",

	"shipping-gitea-prs": "Git · 交付 · 安全", "github-issue-fix-pr": "Git · 交付 · 安全",
	"resolving-merge-conflicts": "Git · 交付 · 安全", "finishing-a-development-branch": "Git · 交付 · 安全",
	"using-git-worktrees": "Git · 交付 · 安全", "security-and-hardening": "Git · 交付 · 安全",

	"mocode-config": "Mocode 自身", "mocode-hooks": "Mocode 自身",
}

const howToAdd = "## 三、新增 skill / 角色\n\n" +
	"- **新增 skill**：建 `internal/core/skills/builtin/<name>/SKILL.md`，front matter `name` 必须等于目录名（kebab-case，**禁止下划线**），`description` 以 `Use when ...` 开头且 ≤1024 字符（**多行用 `>-` 折叠块，避免含 `:` 的 plain scalar 解析失败**）。方法见 `writing-skills`。\n" +
	"- **新增角色**：建 `internal/core/config/templates/modes/<id>.md`（front matter `id/name/description` + 可选 `sub_agents/tools`），在「主导技能」列表引用真实存在的 skill。\n" +
	"- **本文档自动生成**：改完运行 `go run ./internal/core/skills/gen` 刷新；`gen_test.go` 会在陈旧时报错。\n" +
	"- 两者均通过 `//go:embed` 打包，**需重新构建并重启**生效。"

type modeInfo struct {
	id, name, desc string
	skills         []string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	out, err := render(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	dst := filepath.Join(root, "docs", "skills-and-roles", "README.md")
	if err := os.WriteFile(dst, []byte(out), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", dst)
}

// render builds the catalog markdown for the given repo root.
func render(root string) (string, error) {
	builtins := skills.DiscoverBuiltin()
	sort.Slice(builtins, func(i, j int) bool { return builtins[i].Name < builtins[j].Name })

	modes, err := readModes(filepath.Join(root, "internal", "core", "config", "templates", "modes"))
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# 助手角色 & 内置 Skill 总览\n\n")
	fmt.Fprintf(&b, "> mocode 内置 **%d 个专家角色**（模式）与 **%d 个内置 skill** 的可浏览索引。\n", len(modes), len(builtins))
	b.WriteString("> **本文档由 `internal/core/skills/gen` 自动生成**，请勿手改；运行 `go run ./internal/core/skills/gen` 刷新。\n> ")
	b.WriteString("角色 = `internal/core/config/templates/modes/*.md`；skill = `internal/core/skills/builtin/<name>/SKILL.md`（front matter `name` 必须等于目录名）。\n\n")
	b.WriteString("---\n\n")

	// Roles
	b.WriteString("## 一、专家角色（Modes）\n\n")
	b.WriteString("用模式选择器 / `Ctrl+G` 切换；每个角色声明其主导 skill、工作流与约束。\n\n")
	b.WriteString("| 角色 | id | 定位 | 主导 skill |\n|------|----|------|-----------|\n")
	for _, m := range modes {
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", m.name, m.id, m.desc, strings.Join(m.skills, ", "))
	}
	b.WriteString("\n---\n\n")

	// Skills by category
	fmt.Fprintf(&b, "## 二、内置 Skill（%d）\n\n", len(builtins))
	byCat := map[string][]string{}
	for _, s := range builtins {
		cat := categoryOf[s.Name]
		if cat == "" {
			cat = "其他"
		}
		byCat[cat] = append(byCat[cat], "- `"+s.Name+"` — "+oneLine(s.Description))
	}
	ordered := append([]string{}, categoryOrder...)
	ordered = append(ordered, "其他")
	for _, cat := range ordered {
		items := byCat[cat]
		if len(items) == 0 {
			continue
		}
		sort.Strings(items)
		fmt.Fprintf(&b, "### %s\n", cat)
		for _, it := range items {
			b.WriteString(it)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n\n")
	b.WriteString(howToAdd)
	b.WriteString("\n")
	return b.String(), nil
}

// oneLine collapses whitespace and trims a leading "Use when" from a skill
// description for the catalog line.
func oneLine(desc string) string {
	s := strings.Join(strings.Fields(desc), " ")
	s = strings.TrimPrefix(s, "Use when ")
	s = strings.TrimPrefix(s, "当")
	return s
}

func readModes(dir string) ([]modeInfo, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []modeInfo
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, parseMode(string(data)))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func parseMode(content string) modeInfo {
	var m modeInfo
	lines := strings.Split(content, "\n")
	i := 0

	// Only the FIRST '---' fenced block is front matter; bodies may contain
	// further '---' fences (e.g. an example front-matter snippet).
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i = 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "---" {
				i++
				break
			}
			for _, k := range []string{"id", "name", "description"} {
				if v, ok := strings.CutPrefix(trimmed, k+":"); ok {
					v = strings.Trim(strings.TrimSpace(v), `"`)
					switch k {
					case "id":
						m.id = v
					case "name":
						m.name = v
					case "description":
						m.desc = v
					}
				}
			}
		}
	}

	inSkills := false
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## 主导技能") {
			inSkills = true
			continue
		}
		if inSkills {
			if strings.HasPrefix(trimmed, "## ") {
				inSkills = false
				continue
			}
			if rest, ok := strings.CutPrefix(trimmed, "- `"); ok {
				if name, _, ok := strings.Cut(rest, "`"); ok {
					m.skills = append(m.skills, name)
				}
			}
		}
	}
	return m
}
