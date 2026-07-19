package model

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/nextsko/mocode-agent/internal/ui/common"
)

func (m *UI) selectAllEditorText() {
	if m.textarea.Value() == "" {
		m.editorAllSelected = false
		return
	}
	m.editorAllSelected = true
	m.textarea.CursorStart()
}

func (m *UI) cutEditorText() tea.Cmd {
	text := m.textarea.Value()
	if text == "" {
		m.editorAllSelected = false
		return nil
	}
	prevHeight := m.textarea.Height()
	m.textarea.Reset()
	m.editorAllSelected = false
	m.closeCompletions()
	return tea.Batch(
		common.CopyToClipboard(text, "Input cut to clipboard"),
		m.handleTextareaHeightChange(prevHeight),
	)
}

func editorKeyReplacesSelection(msg tea.KeyPressMsg) bool {
	key := msg.String()
	if key == "space" || key == "backspace" || key == "delete" {
		return true
	}
	if strings.Contains(key, "+") {
		return false
	}
	return len([]rune(key)) == 1
}
