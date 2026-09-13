package model

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/editor"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/components"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

func (m *UI) openEditor(value string) tea.Cmd {
	tmpfile, err := os.CreateTemp("", "msg_*.md")
	if err != nil {
		return util.ReportError(err)
	}
	tmpPath := tmpfile.Name()
	defer tmpfile.Close() //nolint:errcheck
	if _, err := tmpfile.WriteString(value); err != nil {
		return util.ReportError(err)
	}
	cmd, err := editor.Command(
		"mocode",
		tmpPath,
		editor.AtPosition(
			m.textarea.Line()+1,
			m.textarea.Column()+1,
		),
	)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer func() {
			_ = os.Remove(tmpPath)
		}()

		if err != nil {
			return util.ReportError(err)
		}
		content, err := os.ReadFile(tmpPath)
		if err != nil {
			return util.ReportError(err)
		}
		if len(content) == 0 {
			return util.ReportWarn("Message is empty")
		}
		return openEditorMsg{
			Text: strings.TrimSpace(string(content)),
		}
	})
}

func (m *UI) setEditorPrompt(yolo bool) {
	if yolo {
		m.textarea.SetPromptFunc(4, m.yoloPromptFunc)
		return
	}
	m.textarea.SetPromptFunc(4, m.normalPromptFunc)
}

func (m *UI) normalPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return "  > "
		}
		return "::: "
	}
	if info.Focused {
		return t.Editor.PromptNormalFocused.Render()
	}
	// When the agent is busy, alternate between bright and dim (breathing).
	if m.breathOn {
		return t.Editor.PromptNormalFocused.Render()
	}
	return t.Editor.PromptNormalBlurred.Render()
}

func (m *UI) yoloPromptFunc(info textarea.PromptInfo) string {
	t := m.com.Styles
	if info.LineNumber == 0 {
		if info.Focused {
			return t.Editor.PromptYoloIconFocused.Render()
		} else {
			return t.Editor.PromptYoloIconBlurred.Render()
		}
	}
	if info.Focused {
		return t.Editor.PromptYoloDotsFocused.Render()
	}
	return t.Editor.PromptYoloDotsBlurred.Render()
}

// subagentSummaryHeight returns the number of rows the sub-agent summary box
// occupies above the editor, or 0 when no sub-agent is running. It must stay in
// sync with renderEditorView so generateLayout reserves enough space and the
// input line is never clipped.
func (m *UI) subagentSummaryHeight() int {
	if m.chat == nil {
		return 0
	}
	running, _ := m.chat.SubagentCounts()
	if running == 0 {
		return 0
	}
	return components.SummaryHeight
}

func (m *UI) renderEditorView(width int) string {
	var attachmentsView string
	if len(m.attachments.List()) > 0 {
		attachmentsView = m.attachments.Render(width)
	}
	// Prime-agent style sub-agent summary box, shown only while sub-agents are
	// still running; once they all finish the box hides and the chat reclaims
	// the rows (see subagentSummaryHeight).
	running, done := m.chat.SubagentCounts()
	var summaryBox string
	if running > 0 {
		summaryBox = components.RenderSubagentSummary(m.com.Styles, components.SubagentCounts{
			Running: running,
			Done:    done,
		}, width)
	}
	separator := m.com.Styles.Header.Separator.Render(strings.Repeat("─", max(0, width)))
	parts := make([]string, 0, 5)
	if summaryBox != "" {
		parts = append(parts, summaryBox)
	}
	parts = append(
		parts,
		separator,
		attachmentsView,
		m.textarea.View(),
		"", // margin at bottom of editor
	)
	return strings.Join(parts, "\n")
}

func (m *UI) applyTheme(s styles.Styles) {
	*m.com.Styles = s
	common.InvalidateMarkdownRendererCache()
	m.chat.InvalidateRenderCaches()
	m.refreshStyles()
}

func (m *UI) refreshStyles() {
	t := m.com.Styles
	m.header.refresh()
	m.textarea.SetStyles(t.Editor.Textarea)
	m.completions.SetStyles(t.Completions.Normal, t.Completions.Focused, t.Completions.Match)
	m.attachments.Renderer().SetStyles(
		t.Attachments.Normal,
		t.Attachments.Deleting,
		t.Attachments.Image,
		t.Attachments.Text,
	)
	m.todoSpinner.Style = t.Pills.TodoSpinner
	m.chat.InvalidateRenderCaches()
}

func (m *UI) toggleCompactMode() tea.Cmd {
	m.forceCompactMode = !m.forceCompactMode

	err := m.com.Workspace.SetCompactMode(config.ScopeGlobal, m.forceCompactMode)
	if err != nil {
		return util.ReportError(err)
	}

	m.updateLayoutAndSize()

	return nil
}
