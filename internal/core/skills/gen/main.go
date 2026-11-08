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

// Skill categories, in display order.
const (
	categoryMeta         = "元技能 · 发现与自省"
	categoryPrinciples   = "规范与理念"
	categoryPlanning     = "计划 · 协作 · 编排"
	categoryReview       = "评审 · 质量 · 验收"
	categoryDebug        = "调试 · 调查 · 重构"
	categoryArchitecture = "架构 · 领域 · 设计系统"
	categoryFrontend     = "前端 · UI 库"
	categoryDesign       = "UI 设计方法学 · 视觉产出"
	categoryAIChat       = "AI 对话 UI · LLM 集成"
	categoryBackend      = "后端 · 运行时 · 构建"
	categoryShell        = "Shell · Windows 环境"
	categoryWriting      = "写作 · 文档 · 可视化产出"
	categoryResearch     = "研究 · 调研 · 抓取 · 复盘"
	categoryGit          = "Git · 交付 · 安全"
	categoryMocode       = "Mocode 自身"
)

// categoryOrder is the display order of skill categories.
var categoryOrder = []string{
	categoryMeta,
	categoryPrinciples,
	categoryPlanning,
	categoryReview,
	categoryDebug,
	categoryArchitecture,
	categoryFrontend,
	categoryDesign,
	categoryAIChat,
	categoryBackend,
	categoryShell,
	categoryWriting,
	categoryResearch,
	categoryGit,
	categoryMocode,
}

// categoryOf maps every builtin skill to a category. A skill missing here lands
// in "其他" — keep this in sync (gen_test.go fails on drift only for the doc).
var categoryOf = map[string]string{
	"using-agent-skills": categoryMeta, "find-skills": categoryMeta,
	"using-superpowers": categoryMeta, "writing-skills": categoryMeta,
	"dot-skill": categoryMeta, "deep-roles": categoryMeta,

	"docs-rulebook": categoryPrinciples, "common-rulebook": categoryPrinciples,
	"ooml-rulebook": categoryPrinciples, "karpathy-guidelines": categoryPrinciples,
	"lean-build": categoryPrinciples,

	"planning-with-files": categoryPlanning, "writing-plans": categoryPlanning,
	"executing-plans": categoryPlanning, "spec-driven-development": categoryPlanning,
	"to-spec": categoryPlanning, "brainstorming": categoryPlanning,
	"subagent-driven-development": categoryPlanning, "sub-team-dev": categoryPlanning,
	"dispatching-parallel-agents": categoryPlanning, "multi-agent-orchestration": categoryPlanning,
	"handoff": categoryPlanning,

	"code-review-and-quality": categoryReview, "requesting-code-review": categoryReview,
	"receiving-code-review": categoryReview, "multi-review": categoryReview,
	"verification-before-completion": categoryReview, "verify-and-stop": categoryReview,
	"triage": categoryReview, "surgical-patch": categoryReview,
	"bug-lesson": categoryReview, "test-driven-development": categoryReview,

	"systematic-debugging": categoryDebug, "investigate-first": categoryDebug,
	"incremental-implementation": categoryDebug, "safe-refactor": categoryDebug,

	"codebase-design": categoryArchitecture, "domain-modeling": categoryArchitecture,
	"improve-codebase-architecture": categoryArchitecture, "design-md": categoryArchitecture,
	"design-taste-frontend": categoryArchitecture,

	"vercel-react-best-practices": categoryFrontend, "vercel-composition-patterns": categoryFrontend,
	"web-design-guidelines": categoryFrontend, "tailwindcss": categoryFrontend,
	"shadcn": categoryFrontend, "zustand": categoryFrontend, "zod": categoryFrontend,
	"ui-replication": categoryFrontend, "playwright-cli": categoryFrontend,

	"screenshot-to-ui": categoryDesign, "css-layout-and-box-model": categoryDesign,
	"design-tokens": categoryDesign, "responsive-design": categoryDesign,
	"motion-design": categoryDesign, "ecommerce-image-studio": categoryDesign,
	"interactive-h5-app": categoryDesign, "red-team-animation-verification": categoryDesign,

	"assistant-ui": categoryAIChat, "primitives": categoryAIChat,
	"runtime": categoryAIChat, "streaming": categoryAIChat,
	"cloud": categoryAIChat, "observability": categoryAIChat,
	"rig-core-llm-integration": categoryAIChat,

	"rust-backend": categoryBackend, "rust-android-apk": categoryBackend,
	"tauri": categoryBackend, "bun": categoryBackend,
	"rsbuild": categoryBackend, "go-tools-mcp-master": categoryBackend,

	"powershell-on-windows": categoryShell, "nu-shell-helper": categoryShell,
	"pi-windows-setup": categoryShell, "windows-junction-link": categoryShell,
	"jq": categoryShell,

	"report-writer": categoryWriting, "docx": categoryWriting,
	"pptx": categoryWriting, "kami-pdf-workflow": categoryWriting,
	"stunning-html-slides": categoryWriting, "excalidraw-diagram": categoryWriting,
	"file-organizer": categoryWriting,

	"project-teardown": categoryResearch, "reverse-engineering-docs": categoryResearch,
	"fromsko-research": categoryResearch, "fromsko-knowledge": categoryResearch,
	"public-social-research": categoryResearch, "firecrawl": categoryResearch,
	"session-viz": categoryResearch, "exp-recorder": categoryResearch,

	"shipping-gitea-prs": categoryGit, "github-issue-fix-pr": categoryGit,
	"resolving-merge-conflicts": categoryGit, "finishing-a-development-branch": categoryGit,
	"using-git-worktrees": categoryGit, "security-and-hardening": categoryGit,

	"mocode-config": categoryMocode, "mocode-hooks": categoryMocode,
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
	if err := os.WriteFile(dst, []byte(out), 0o600); err != nil { //nolint:gosec // dst is the generated docs path derived from the repo root.
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
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // walks the embedded template directory.
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
