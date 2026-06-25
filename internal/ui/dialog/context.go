package dialog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/ui/util"
)

const ContextID = "context"

type contextMode int

const (
	contextModeNormal contextMode = iota
	contextModeEdit
)

// ContextMessages displays and edits messages that make up the current session
// context. The left pane is a message directory; the right pane shows message
// content plus a secondary tools section for tool calls/results.
type ContextMessages struct {
	com       *common.Common
	help      help.Model
	messages  []message.Message
	selected  int
	scroll    int
	mode      contextMode
	editor    textarea.Model
	sessionID string

	keyMap struct {
		Close    key.Binding
		Next     key.Binding
		Previous key.Binding
		Top      key.Binding
		Bottom   key.Binding
		Delete   key.Binding
		Copy     key.Binding
		Edit     key.Binding
		Save     key.Binding
		Newline  key.Binding
	}
}

var _ Dialog = (*ContextMessages)(nil)

func NewContextMessages(com *common.Common, sessionID string) (*ContextMessages, error) {
	msgs, err := com.Workspace.ListMessages(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	d := &ContextMessages{
		com:       com,
		messages:  msgs,
		sessionID: sessionID,
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	ed := textarea.New()
	ed.SetStyles(com.Styles.Editor.Textarea)
	ed.ShowLineNumbers = false
	ed.CharLimit = -1
	ed.Prompt = ""
	d.editor = ed

	d.keyMap.Close = CloseKey
	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next"))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "prev"))
	d.keyMap.Top = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "top"))
	d.keyMap.Bottom = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bottom"))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	d.keyMap.Copy = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy"))
	d.keyMap.Edit = key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "edit"))
	d.keyMap.Save = key.NewBinding(key.WithKeys("enter", "ctrl+s"), key.WithHelp("enter", "save"))
	d.keyMap.Newline = key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "newline"))
	return d, nil
}

func (d *ContextMessages) ID() string { return ContextID }

func (d *ContextMessages) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if d.mode == contextModeEdit {
		switch {
		case key.Matches(keyMsg, d.keyMap.Close):
			d.mode = contextModeNormal
			return nil
		case key.Matches(keyMsg, d.keyMap.Save):
			return d.saveEdit()
		case key.Matches(keyMsg, d.keyMap.Newline):
			d.editor.InsertRune('\n')
			return nil
		default:
			var cmd tea.Cmd
			d.editor, cmd = d.editor.Update(msg)
			return ActionCmd{Cmd: cmd}
		}
	}

	switch {
	case key.Matches(keyMsg, d.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, d.keyMap.Previous):
		if d.selected > 0 {
			d.selected--
		}
	case key.Matches(keyMsg, d.keyMap.Next):
		if d.selected < len(d.messages)-1 {
			d.selected++
		}
	case key.Matches(keyMsg, d.keyMap.Top):
		d.selected = 0
		d.scroll = 0
	case key.Matches(keyMsg, d.keyMap.Bottom):
		if len(d.messages) > 0 {
			d.selected = len(d.messages) - 1
		}
	case key.Matches(keyMsg, d.keyMap.Delete):
		return d.deleteSelected()
	case key.Matches(keyMsg, d.keyMap.Copy):
		return d.copySelected()
	case key.Matches(keyMsg, d.keyMap.Edit):
		d.startEdit()
	}
	return nil
}

func (d *ContextMessages) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	maxW := area.Dx() - t.Dialog.View.GetHorizontalFrameSize() - 4
	width := max(64, min(120, maxW))
	maxH := area.Dy() - t.Dialog.View.GetVerticalFrameSize() - 6
	height := max(10, min(28, maxH))

	rc := NewRenderContext(t, width)
	rc.Title = "Context"
	if len(d.messages) > 0 {
		rc.TitleInfo = t.Dialog.ListItem.InfoFocused.Render(fmt.Sprintf(" %d/%d", d.selected+1, len(d.messages)))
	}

	if d.mode == contextModeEdit {
		d.editor.SetWidth(max(20, width-t.Dialog.View.GetHorizontalFrameSize()-4))
		d.editor.SetHeight(max(5, min(16, height-5)))
		rc.AddPart(t.Dialog.PrimaryText.Render("Editing selected message. Enter saves, Ctrl+J inserts newline, Esc cancels."))
		rc.AddPart(d.editor.View())
		rc.Help = d.help.View(d)
		DrawCenterCursor(scr, area, rc.Render(), nil)
		return nil
	}

	bodyH := height - 4
	leftW := max(18, min(32, width/3))
	rightW := max(24, width-leftW-8)
	left := d.renderMessageDirectory(t, leftW, bodyH)
	right := d.renderMessageDetail(t, rightW, bodyH)
	rc.AddPart(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
	rc.Help = d.help.View(d)
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

func (d *ContextMessages) renderMessageDirectory(t *styles.Styles, width, height int) string {
	if len(d.messages) == 0 {
		return t.Dialog.PrimaryText.Width(width).Render("No messages")
	}
	d.ensureVisible(height - 2)
	lines := []string{t.Dialog.TitleText.Render("Messages")}
	for i := 0; i < height-2 && d.scroll+i < len(d.messages); i++ {
		idx := d.scroll + i
		msg := d.messages[idx]
		preview := oneLine(messageText(msg))
		if preview == "" {
			preview = messagePartsSummary(msg)
		}
		row := ansi.Truncate(fmt.Sprintf("%2d %-9s %s", idx+1, msg.Role, preview), width-2, "…")
		if idx == d.selected {
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(row))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(row))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (d *ContextMessages) renderMessageDetail(t *styles.Styles, width, height int) string {
	msg := d.selectedMessage()
	if msg == nil {
		return t.Dialog.PrimaryText.Width(width).Render("No message selected")
	}

	created := ""
	if msg.CreatedAt > 0 {
		created = time.Unix(msg.CreatedAt, 0).Format("2006-01-02 15:04:05")
	}
	lines := []string{
		t.Dialog.TitleText.Render("Message"),
		fmt.Sprintf("Role: %s", msg.Role),
		fmt.Sprintf("ID: %s", msg.ID),
		fmt.Sprintf("Model: %s/%s", msg.Provider, msg.Model),
		fmt.Sprintf("Created: %s", created),
		"",
	}

	text := messageText(*msg)
	if strings.TrimSpace(text) != "" {
		lines = append(lines, t.Dialog.TitleText.Render("Content"))
		lines = append(lines, clampLines(text, width, max(3, height/2))...)
	}

	toolLines := renderToolLines(*msg, width, max(3, height/3))
	if len(toolLines) > 0 {
		lines = append(lines, "", t.Dialog.TitleText.Render("Tools"))
		lines = append(lines, toolLines...)
	}

	if len(lines) > height {
		lines = append(lines[:height-1], "…")
	}
	return t.Dialog.PrimaryText.Width(width).Render(strings.Join(lines, "\n"))
}

func (d *ContextMessages) ShortHelp() []key.Binding {
	if d.mode == contextModeEdit {
		return []key.Binding{d.keyMap.Save, d.keyMap.Newline, d.keyMap.Close}
	}
	upDown := key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select"))
	return []key.Binding{upDown, d.keyMap.Copy, d.keyMap.Edit, d.keyMap.Delete, d.keyMap.Close}
}

func (d *ContextMessages) FullHelp() [][]key.Binding {
	if d.mode == contextModeEdit {
		return [][]key.Binding{{d.keyMap.Save, d.keyMap.Newline, d.keyMap.Close}}
	}
	return [][]key.Binding{{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Top, d.keyMap.Bottom}, {d.keyMap.Copy, d.keyMap.Edit, d.keyMap.Delete, d.keyMap.Close}}
}

func (d *ContextMessages) selectedMessage() *message.Message {
	if d.selected < 0 || d.selected >= len(d.messages) {
		return nil
	}
	return &d.messages[d.selected]
}

func (d *ContextMessages) ensureVisible(visible int) {
	if visible <= 0 {
		return
	}
	if d.selected < d.scroll {
		d.scroll = d.selected
	}
	if d.selected >= d.scroll+visible {
		d.scroll = d.selected - visible + 1
	}
	if d.scroll < 0 {
		d.scroll = 0
	}
}

func (d *ContextMessages) deleteSelected() Action {
	msg := d.selectedMessage()
	if msg == nil {
		return nil
	}
	id := msg.ID
	d.messages = append(d.messages[:d.selected], d.messages[d.selected+1:]...)
	if d.selected >= len(d.messages) && d.selected > 0 {
		d.selected--
	}
	return ActionCmd{Cmd: func() tea.Msg {
		if err := d.com.Workspace.DeleteMessage(context.Background(), id); err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Context message deleted")
	}}
}

func (d *ContextMessages) copySelected() Action {
	msg := d.selectedMessage()
	if msg == nil {
		return nil
	}
	return ActionCmd{Cmd: common.CopyToClipboard(formatMessageForCopy(*msg), "Context message copied to clipboard")}
}

func (d *ContextMessages) startEdit() {
	msg := d.selectedMessage()
	if msg == nil {
		return
	}
	d.mode = contextModeEdit
	d.editor.SetValue(messageText(*msg))
	d.editor.MoveToEnd()
	d.editor.Focus()
}

func (d *ContextMessages) saveEdit() Action {
	msg := d.selectedMessage()
	if msg == nil {
		d.mode = contextModeNormal
		return nil
	}
	updated := *msg
	setMessageText(&updated, d.editor.Value())
	d.messages[d.selected] = updated
	d.mode = contextModeNormal
	return ActionCmd{Cmd: func() tea.Msg {
		if err := d.com.Workspace.UpdateMessage(context.Background(), updated); err != nil {
			return util.NewErrorMsg(err)
		}
		return util.NewInfoMsg("Context message updated")
	}}
}

func messageText(msg message.Message) string {
	return msg.Content().Text
}

func setMessageText(msg *message.Message, text string) {
	for i, part := range msg.Parts {
		if _, ok := part.(message.TextContent); ok {
			msg.Parts[i] = message.TextContent{Text: text}
			return
		}
	}
	msg.Parts = append([]message.ContentPart{message.TextContent{Text: text}}, msg.Parts...)
}

func messagePartsSummary(msg message.Message) string {
	var parts []string
	if len(msg.ToolCalls()) > 0 {
		parts = append(parts, fmt.Sprintf("%d tool calls", len(msg.ToolCalls())))
	}
	if len(msg.ToolResults()) > 0 {
		parts = append(parts, fmt.Sprintf("%d tool results", len(msg.ToolResults())))
	}
	if msg.ReasoningContent().Thinking != "" {
		parts = append(parts, "reasoning")
	}
	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, ", ")
}

func formatMessageForCopy(msg message.Message) string {
	var out []string
	out = append(out, fmt.Sprintf("Role: %s", msg.Role), fmt.Sprintf("ID: %s", msg.ID), "")
	if text := messageText(msg); text != "" {
		out = append(out, text)
	}
	for _, tc := range msg.ToolCalls() {
		out = append(out, "", fmt.Sprintf("Tool call: %s (%s)", tc.Name, tc.ID), tc.Input)
	}
	for _, tr := range msg.ToolResults() {
		out = append(out, "", fmt.Sprintf("Tool result: %s (%s)", tr.Name, tr.ToolCallID), tr.Content)
	}
	return strings.Join(out, "\n")
}

func renderToolLines(msg message.Message, width, maxLines int) []string {
	var lines []string
	for _, tc := range msg.ToolCalls() {
		lines = append(lines, ansi.Truncate(fmt.Sprintf("call  %s %s", tc.Name, tc.ID), width, "…"))
		lines = append(lines, clampLines(tc.Input, width, 2)...)
	}
	for _, tr := range msg.ToolResults() {
		lines = append(lines, ansi.Truncate(fmt.Sprintf("result %s %s", tr.Name, tr.ToolCallID), width, "…"))
		content := tr.Content
		if content == "" {
			content = tr.Data
		}
		lines = append(lines, clampLines(content, width, 2)...)
	}
	if len(lines) > maxLines {
		return append(lines[:maxLines-1], "…")
	}
	return lines
}

func clampLines(text string, width, maxLines int) []string {
	if strings.TrimSpace(text) == "" || maxLines <= 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, ansi.Truncate(line, width, "…"))
		if len(out) >= maxLines {
			break
		}
	}
	if len(strings.Split(text, "\n")) > maxLines {
		out = append(out, "…")
	}
	return out
}

func oneLine(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return text
}
