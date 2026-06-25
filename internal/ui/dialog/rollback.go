package dialog

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/package-register/mocode/internal/domain/history"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
)

const RollbackID = "rollback"

// RollbackDialog allows users to browse session nodes and rollback files to a previous state.
type RollbackDialog struct {
	com       *common.Common
	help      help.Model
	messages  []message.Message
	files     []history.File
	selected  int
	scroll    int
	sessionID string

	keyMap struct {
		Close    key.Binding
		Next     key.Binding
		Previous key.Binding
		Top      key.Binding
		Bottom   key.Binding
		Rollback key.Binding
	}
}

var _ Dialog = (*RollbackDialog)(nil)

// ActionRollback is the action sent when a rollback is confirmed.
type ActionRollback struct {
	Target message.Message
}

func NewRollback(com *common.Common, sessionID string) (*RollbackDialog, error) {
	ctx := context.Background()
	msgs, err := com.Workspace.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	// Filter out summary messages
	filtered := make([]message.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg.IsSummaryMessage {
			continue
		}
		filtered = append(filtered, msg)
	}

	files, err := com.Workspace.ListSessionHistory(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session history: %w", err)
	}

	d := &RollbackDialog{
		com:       com,
		messages:  filtered,
		files:     files,
		sessionID: sessionID,
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Close = CloseKey
	d.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next"))
	d.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "prev"))
	d.keyMap.Top = key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "top"))
	d.keyMap.Bottom = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bottom"))
	d.keyMap.Rollback = key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("enter", "rollback"))

	return d, nil
}

func (d *RollbackDialog) ID() string { return RollbackID }

func (d *RollbackDialog) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	switch {
	case key.Matches(keyMsg, d.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, d.keyMap.Previous):
		if d.selected > 0 {
			d.selected--
			if d.selected < d.scroll {
				d.scroll = d.selected
			}
		}
	case key.Matches(keyMsg, d.keyMap.Next):
		if d.selected < len(d.messages)-1 {
			d.selected++
			if d.selected >= d.scroll+20 {
				d.scroll = d.selected - 19
			}
		}
	case key.Matches(keyMsg, d.keyMap.Top):
		d.selected = 0
		d.scroll = 0
	case key.Matches(keyMsg, d.keyMap.Bottom):
		if len(d.messages) > 0 {
			d.selected = len(d.messages) - 1
			d.scroll = max(0, d.selected-19)
		}
	case key.Matches(keyMsg, d.keyMap.Rollback):
		return d.confirmRollback()
	}
	return nil
}

func (d *RollbackDialog) confirmRollback() Action {
	if len(d.messages) == 0 || d.selected < 0 || d.selected >= len(d.messages) {
		return nil
	}

	target := d.messages[d.selected]
	return ActionRollback{Target: target}
}

func (d *RollbackDialog) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	maxW := area.Dx() - t.Dialog.View.GetHorizontalFrameSize() - 4
	width := max(64, min(120, maxW))
	maxH := area.Dy() - t.Dialog.View.GetVerticalFrameSize() - 6
	height := max(10, min(28, maxH))

	rc := NewRenderContext(t, width)
	rc.Title = "Rollback"
	if len(d.messages) > 0 {
		rc.TitleInfo = t.Dialog.ListItem.InfoFocused.Render(fmt.Sprintf(" %d/%d", d.selected+1, len(d.messages)))
	}

	bodyH := height - 4
	leftW := max(18, min(32, width/3))
	rightW := max(24, width-leftW-8)

	left := d.renderNodeList(t, leftW, bodyH)
	right := d.renderFilePreview(t, rightW, bodyH)

	rc.AddPart(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
	rc.Help = d.help.View(d)
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

func (d *RollbackDialog) renderNodeList(t *styles.Styles, width, height int) string {
	if len(d.messages) == 0 {
		return t.Dialog.PrimaryText.Width(width).Render("No nodes")
	}

	lines := []string{t.Dialog.TitleText.Render("Nodes (user messages)")}
	startIdx := max(0, min(d.scroll, len(d.messages)-1))
	endIdx := min(startIdx+height-2, len(d.messages))

	for i := startIdx; i < endIdx; i++ {
		msg := d.messages[i]
		idx := i + 1
		role := msg.Role
		timeStr := time.Unix(msg.CreatedAt, 0).Format("15:04")
		preview := d.getNodePreview(msg)
		row := fmt.Sprintf("%2d %-5s %s %s", idx, role, timeStr, preview)
		truncated := ansi.Truncate(row, width-2, "…")
		if i == d.selected {
			lines = append(lines, t.Dialog.SelectedItem.Width(width).Render(truncated))
		} else {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(truncated))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (d *RollbackDialog) getNodePreview(msg message.Message) string {
	text := messageText(msg)
	if text == "" {
		parts := messagePartsSummary(msg)
		if parts != "" {
			return ansi.Truncate(parts, 30, "…")
		}
		return "(no text)"
	}
	return ansi.Truncate(oneLine(text), 30, "…")
}

func (d *RollbackDialog) renderFilePreview(t *styles.Styles, width, height int) string {
	lines := []string{t.Dialog.TitleText.Render("Files")}

	if len(d.files) == 0 {
		lines = append(lines, t.Dialog.PrimaryText.Render("No file history"))
	} else {
		// Count files that will be affected
		targetTime := int64(0)
		if d.selected >= 0 && d.selected < len(d.messages) {
			targetTime = d.messages[d.selected].UpdatedAt
		}

		restored, removed, unchanged := d.countAffectedFiles(targetTime)
		summary := fmt.Sprintf("Total: %d files | Restored: %d | Removed: %d | Unchanged: %d",
			len(d.files), restored, removed, unchanged)
		lines = append(lines, t.Dialog.ListItem.InfoBlurred.Render(ansi.Truncate(summary, width-2, "…")))

		// Show some file paths
		maxRows := height - 4
		maxRows = max(1, maxRows)
		paths := make([]string, 0, len(d.files))
		pathSet := make(map[string]bool)
		for _, f := range d.files {
			if !pathSet[f.Path] {
				paths = append(paths, f.Path)
				pathSet[f.Path] = true
			}
		}

		for i := 0; i < maxRows && i < len(paths); i++ {
			lines = append(lines, t.Dialog.NormalItem.Width(width).Render(
				ansi.Truncate(paths[i], width-2, "…")))
		}
		if len(paths) > maxRows {
			lines = append(lines, t.Dialog.NormalItem.Render(
				fmt.Sprintf("... and %d more", len(paths)-maxRows)))
		}
	}

	// Add rollback hint
	if len(d.messages) > 0 {
		lines = append(lines, "")
		lines = append(lines, t.Dialog.PrimaryText.Render("Press Enter to rollback to selected node"))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (d *RollbackDialog) countAffectedFiles(targetTime int64) (restored, removed, unchanged int) {
	pathSet := make(map[string]bool)
	for _, f := range d.files {
		pathSet[f.Path] = true
	}

	for path := range pathSet {
		var latestBeforeTarget *history.File
		for i := range d.files {
			if d.files[i].Path != path {
				continue
			}
			if d.files[i].CreatedAt <= targetTime {
				if latestBeforeTarget == nil || d.files[i].CreatedAt > latestBeforeTarget.CreatedAt {
					latestBeforeTarget = &d.files[i]
				}
			}
		}

		if latestBeforeTarget == nil {
			removed++
		} else {
			restored++
		}
	}
	return restored, removed, unchanged
}

func (d *RollbackDialog) ShortHelp() []key.Binding {
	upDown := key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "select"))
	return []key.Binding{upDown, d.keyMap.Rollback, d.keyMap.Close}
}

func (d *RollbackDialog) FullHelp() [][]key.Binding {
	return [][]key.Binding{{d.keyMap.Previous, d.keyMap.Next, d.keyMap.Top, d.keyMap.Bottom}, {d.keyMap.Rollback, d.keyMap.Close}}
}
