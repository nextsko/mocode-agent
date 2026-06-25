package model

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	execbuiltin "github.com/package-register/mocode/internal/core/agent/tools/builtin/bash"
	"github.com/package-register/mocode/internal/core/shellruntime/shell"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/chat"
)

func (m *UI) resumeBackgroundJobPolling(msgs []*message.Message) tea.Cmd {
	var cmds []tea.Cmd
	for _, msg := range msgs {
		if msg == nil || msg.Role != message.Tool {
			continue
		}
		for _, tr := range msg.ToolResults() {
			if cmd := m.maybeStartBackgroundJobPoll(tr); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *UI) maybeStartBackgroundJobPoll(tr message.ToolResult) tea.Cmd {
	if tr.ToolCallID == "" || tr.Metadata == "" {
		return nil
	}

	var meta execbuiltin.BashResponseMetadata
	if err := json.Unmarshal([]byte(tr.Metadata), &meta); err != nil {
		return nil
	}
	if !meta.Background || strings.TrimSpace(meta.ShellID) == "" {
		return nil
	}

	bgShell, ok := shell.GetBackgroundShellManager().Get(meta.ShellID)
	if !ok {
		return nil
	}
	if m.backgroundJobs == nil {
		m.backgroundJobs = make(map[string]string)
	}
	if existingToolCallID, ok := m.backgroundJobs[meta.ShellID]; ok && existingToolCallID == tr.ToolCallID && bgShell.IsDone() {
		return nil
	}
	m.backgroundJobs[meta.ShellID] = tr.ToolCallID
	return m.pollBackgroundJobCmd(meta.ShellID)
}

func (m *UI) pollBackgroundJobCmd(shellID string) tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		bgShell, ok := shell.GetBackgroundShellManager().Get(shellID)
		if !ok {
			return backgroundJobOutputMsg{ShellID: shellID, Done: true}
		}

		stdout, stderr, done, err := bgShell.GetOutput()
		output := formatBackgroundShellOutput(stdout, stderr, err)
		if output == "" {
			output = "no output"
		}

		status := "running"
		if done {
			status = "completed"
			if err != nil && shell.IsInterrupt(err) {
				status = "interrupted"
			}
		}

		return backgroundJobOutputMsg{
			ShellID: shellID,
			Output:  fmt.Sprintf("Status: %s\n\n%s", status, output),
			Done:    done,
			Err:     err,
		}
	})
}

func (m *UI) handleBackgroundJobOutput(msg backgroundJobOutputMsg) tea.Cmd {
	toolCallID, ok := m.backgroundJobs[msg.ShellID]
	if !ok {
		return nil
	}
	if msg.Done {
		delete(m.backgroundJobs, msg.ShellID)
	}

	toolItem := m.findToolMessageItem(toolCallID)
	if toolItem == nil {
		return nil
	}

	current := toolItem.ToolCall()
	current.Finished = msg.Done
	toolItem.SetToolCall(current)

	res := toolItemCurrentResult(toolItem)
	if res == nil {
		res = &message.ToolResult{
			ToolCallID: toolCallID,
			Name:       current.Name,
		}
	}
	res.Content = msg.Output
	toolItem.SetResult(res)

	if msg.Done {
		return nil
	}
	return m.pollBackgroundJobCmd(msg.ShellID)
}

func (m *UI) findToolMessageItem(toolCallID string) chat.ToolMessageItem {
	if toolCallID == "" {
		return nil
	}
	item := m.chat.MessageItem(toolCallID)
	if toolItem, ok := item.(chat.ToolMessageItem); ok && toolItem.ToolCall().ID == toolCallID {
		return toolItem
	}
	container, ok := item.(chat.NestedToolContainer)
	if !ok {
		return nil
	}
	for _, nested := range container.NestedTools() {
		if nested.ToolCall().ID == toolCallID {
			return nested
		}
	}
	return nil
}

func toolItemCurrentResult(item chat.ToolMessageItem) *message.ToolResult {
	if item == nil {
		return nil
	}
	if item.Result() == nil {
		return nil
	}
	result := *item.Result()
	return &result
}

func formatBackgroundShellOutput(stdout, stderr string, execErr error) string {
	stdout = truncateBackgroundShellOutput(stdout)
	stderr = truncateBackgroundShellOutput(stderr)

	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	if execErr != nil && !shell.IsInterrupt(execErr) {
		if output != "" {
			output += "\n"
		}
		output += execErr.Error()
	}
	return output
}

func truncateBackgroundShellOutput(content string) string {
	const maxOutputLength = 30000
	if len(content) <= maxOutputLength {
		return content
	}
	halfLength := maxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]
	return fmt.Sprintf("%s\n\n... output truncated ...\n\n%s", start, end)
}

func (m *UI) handleClickFocus(msg tea.MouseClickMsg) (cmd tea.Cmd) {
	switch {
	case m.state != uiChat:
		return nil
	case image.Pt(msg.X, msg.Y).In(m.layout.sidebar):
		return nil
	case m.focus != uiFocusEditor && image.Pt(msg.X, msg.Y).In(m.layout.editor):
		m.focus = uiFocusEditor
		cmd = m.textarea.Focus()
		m.chat.Blur()
	case m.focus != uiFocusMain && image.Pt(msg.X, msg.Y).In(m.layout.main):
		m.focus = uiFocusMain
		m.textarea.Blur()
		m.chat.Focus()
	}
	return cmd
}
