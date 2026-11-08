package messages

import (
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/domain/session"
)

func BuildSummaryPrompt(todos []session.Todo) string {
	var sb strings.Builder
	sb.WriteString("请基于上面的完整会话生成一份中文标准会话摘要。")
	sb.WriteString("\n\n要求：")
	sb.WriteString("\n1. 输出必须以 YAML frontmatter 开头（--- 包裹），包含 type, tags, status, files_modified, commands_run 等元数据。")
	sb.WriteString("\n2. 使用中文撰写，术语、文件路径、命令、函数名、错误信息保持原文。")
	sb.WriteString("\n3. 摘要用于下一次恢复上下文，必须足够具体，不能泛泛而谈。")
	sb.WriteString("\n4. 必须覆盖以下 Markdown 章节：用户目标、当前进展、文件与代码变更、技术决策与约束、验证结果、风险与待确认、下一步。")
	sb.WriteString("\n5. 文件引用格式：`path/to/file.go:行号` — 变更描述")
	sb.WriteString("\n6. 命令结果格式：`command` → exit_code: N, 关键输出")
	sb.WriteString("\n7. 下一步必须是可执行编号清单，包含具体文件、函数或命令。")
	if len(todos) > 0 {
		pending := 0
		inProgress := 0
		for _, t := range todos {
			switch t.Status {
			case "pending":
				pending++
			case "in_progress":
				inProgress++
			default:
				// Completed todos are not counted; the detail list below
				// still reports their final status.
			}
		}
		fmt.Fprintf(&sb, "\n\n## 当前 Todo 状态\n\n- pending: %d\n- in_progress: %d\n", pending, inProgress)
		sb.WriteString("\n### 详情\n\n")
		for _, t := range todos {
			fmt.Fprintf(&sb, "- [%s] %s\n", t.Status, t.Content)
		}
		sb.WriteString("\n请把这些 todo 的状态整合进 YAML frontmatter 的 todos_pending/todos_in_progress 字段，并在摘要正文的「下一步」章节中包含未完成项。")
	}
	return sb.String()
}

// BuildTodoNudge produces a system-message reminder of open todos, injected at
// each model step (PrepareStep) to nudge the agent toward finishing pending or
// in-progress items before declaring itself done. This is the soft variant of
// trpc-agent-go's todoenforcer: it cannot hard-block "Done" (that needs
// model-step Done-flipping inside the fantasy loop), but surfacing open work at
// every step empirically reduces early-exit failures. Returns "" when there is
// nothing open, so the caller injects nothing.
func BuildTodoNudge(todos []session.Todo) string {
	var open []session.Todo
	for _, t := range todos {
		if t.Status != session.TodoStatusCompleted {
			open = append(open, t)
		}
	}
	if len(open) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Reminder: the following todos are still open. Complete them (or use the todos tool to mark progress) before producing a final answer.")
	for _, t := range open {
		marker := "[pending]"
		if t.Status == session.TodoStatusInProgress {
			marker = "[in_progress]"
		}
		text := t.Content
		if t.ActiveForm != "" {
			text = t.ActiveForm
		}
		sb.WriteString("\n- ")
		sb.WriteString(marker)
		sb.WriteString(" ")
		sb.WriteString(text)
	}
	return sb.String()
}
