package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

func extractToolResultText(tr fantasy.ToolResultPart) string {
	if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](tr.Output); ok {
		return r.Text
	}
	if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](tr.Output); ok {
		return r.Error.Error()
	}
	return ""
}

func isToolResultError(tr fantasy.ToolResultPart) bool {
	_, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](tr.Output)
	return ok
}

func setToolResultText(tr *fantasy.ToolResultPart, text string) {
	tr.Output = fantasy.ToolResultOutputContentText{Text: text}
}

func filterImagePartsFromMessages(msgs []fantasy.Message) []fantasy.Message {
	var result []fantasy.Message
	for _, msg := range msgs {
		var filteredParts []fantasy.MessagePart
		for _, part := range msg.Content {
			if _, isFile := fantasy.AsMessagePart[fantasy.FilePart](part); isFile {
				continue
			}
			filteredParts = append(filteredParts, part)
		}
		if len(filteredParts) > 0 {
			msg.Content = filteredParts
			result = append(result, msg)
		}
	}
	return result
}

// filterEmptyContentMessages drops any messages with empty content arrays.
// Sending {"content": []} to LLM APIs causes "messages.content.type is
// invalid" errors. System messages are preserved (they use a string content
// field, not an array).
func filterEmptyContentMessages(msgs []fantasy.Message) []fantasy.Message {
	filtered := msgs[:0]
	for _, msg := range msgs {
		if msg.Role != fantasy.MessageRoleSystem && len(msg.Content) == 0 {
			slog.Warn(
				"Dropping message with empty content in PrepareStep",
				"role", msg.Role,
			)
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func filterOrphanedToolResults(m message.Message, knownToolCallIDs map[string]struct{}) (fantasy.Message, bool) {
	aiMsgs := m.ToAIMessage()
	if len(aiMsgs) == 0 {
		return fantasy.Message{}, false
	}
	var validParts []fantasy.MessagePart
	for _, part := range aiMsgs[0].Content {
		tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
		if !ok {
			validParts = append(validParts, part)
			continue
		}
		if _, known := knownToolCallIDs[tr.ToolCallID]; known {
			validParts = append(validParts, part)
		} else {
			slog.Warn(
				"Dropping orphaned tool result with no matching tool call",
				"tool_call_id", tr.ToolCallID,
			)
		}
	}
	if len(validParts) == 0 {
		return fantasy.Message{}, false
	}
	msg := aiMsgs[0]
	msg.Content = validParts
	return msg, true
}

func syntheticToolResultsForOrphanedCalls(m message.Message, knownToolResultIDs map[string]struct{}) (fantasy.Message, bool) {
	var syntheticParts []fantasy.MessagePart
	for _, tc := range m.ToolCalls() {
		if _, hasResult := knownToolResultIDs[tc.ID]; hasResult {
			continue
		}
		slog.Warn(
			"Injecting synthetic tool result for orphaned tool call",
			"tool_call_id", tc.ID,
			"tool_name", tc.Name,
		)
		syntheticParts = append(syntheticParts, fantasy.ToolResultPart{
			ToolCallID: tc.ID,
			Output: fantasy.ToolResultOutputContentError{
				Error: errors.New("tool call was interrupted and did not produce a result, you may retry this call if the result is still needed"),
			},
		})
	}
	if len(syntheticParts) == 0 {
		return fantasy.Message{}, false
	}
	return fantasy.Message{
		Role:    fantasy.MessageRoleTool,
		Content: syntheticParts,
	}, true
}

func buildSummaryPrompt(todos []session.Todo) string {
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
			}
		}
		sb.WriteString(fmt.Sprintf("\n\n## 当前 Todo 状态\n\n- pending: %d\n- in_progress: %d\n", pending, inProgress))
		sb.WriteString("\n### 详情\n\n")
		for _, t := range todos {
			fmt.Fprintf(&sb, "- [%s] %s\n", t.Status, t.Content)
		}
		sb.WriteString("\n请把这些 todo 的状态整合进 YAML frontmatter 的 todos_pending/todos_in_progress 字段，并在摘要正文的「下一步」章节中包含未完成项。")
	}
	return sb.String()
}

func providerRetryLogFields(err *fantasy.ProviderError, delay time.Duration) []any {
	fields := []any{
		"retry_delay", delay.String(),
	}
	if err == nil {
		return fields
	}
	fields = append(fields, "status_code", err.StatusCode)
	if err.Title != "" {
		fields = append(fields, "title", err.Title)
	}
	if err.Message != "" {
		fields = append(fields, "message", err.Message)
	}
	return fields
}

// buildTodoNudge produces a system-message reminder of open todos, injected at
// each model step (PrepareStep) to nudge the agent toward finishing pending or
// in-progress items before declaring itself done. This is the soft variant of
// trpc-agent-go's todoenforcer: it cannot hard-block "Done" (that needs
// model-step Done-flipping inside the fantasy loop), but surfacing open work at
// every step empirically reduces early-exit failures. Returns "" when there is
// nothing open, so the caller injects nothing.
func buildTodoNudge(todos []session.Todo) string {
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
