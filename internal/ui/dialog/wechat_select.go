package dialog

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	wechat "github.com/package-register/mocode/internal/integration/wechat"
	"github.com/package-register/mocode/internal/ui/common"
)

const (
	WeChatSelectID = "wechat_select"
)

// ActionSelectWeChat is a message indicating a WeChat account has been selected.
type ActionSelectWeChat struct {
	AccountID string
}

// WeChatSelect represents a dialog for selecting the active WeChat account.
type WeChatSelect struct {
	com      *common.Common
	accounts []wechat.AccountInfo
	selected int

	keyMap struct {
		Close  key.Binding
		Select key.Binding
		Up     key.Binding
		Down   key.Binding
	}
}

var _ Dialog = (*WeChatSelect)(nil)

// NewWeChatSelect creates a new WeChat account selection dialog.
func NewWeChatSelect(com *common.Common) *WeChatSelect {
	d := &WeChatSelect{com: com}
	d.keyMap.Close = CloseKey
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up"))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down"))
	return d
}

// ID implements Dialog.
func (d *WeChatSelect) ID() string { return WeChatSelectID }

// HandleMsg implements Dialog.
func (d *WeChatSelect) HandleMsg(msg tea.Msg) Action {
	d.accounts = wechat.GetManager().List()
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, d.keyMap.Up):
			if d.selected > 0 {
				d.selected--
			}
		case key.Matches(msg, d.keyMap.Down):
			if d.selected < len(d.accounts)-1 {
				d.selected++
			}
		case key.Matches(msg, d.keyMap.Select):
			if d.selected >= 0 && d.selected < len(d.accounts) {
				return ActionSelectWeChat{AccountID: d.accounts[d.selected].ID}
			}
		}
	}
	return nil
}

// Draw implements Dialog.
func (d *WeChatSelect) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	title := common.DialogTitle(t, "WeChat Accounts", min(area.Dx()-4, 60),
		t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor)

	d.accounts = wechat.GetManager().List()

	var lines []string
	if len(d.accounts) == 0 {
		lines = append(lines, t.Dialog.ListItem.InfoBlurred.Render("No accounts — use /wechat to login"))
	} else {
		for i, a := range d.accounts {
			style := t.Dialog.NormalItem
			marker := "  "
			if i == d.selected {
				style = t.Dialog.SelectedItem
				marker = "▶ "
			}
			activeMark := " "
			if a.IsActive {
				activeMark = "●"
			}
			shortID := a.ID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			line := fmt.Sprintf("%s%s  %s  [%s]  %s",
				marker, activeMark, a.Name, shortID, a.Status)
			lines = append(lines, style.Render(line))
		}
	}
	lines = append(lines,
		"",
		t.Dialog.ListItem.InfoBlurred.Render("[↑/k ↓/j move]  [enter select]  [esc close]"),
	)

	view := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
	DrawCenter(scr, area, t.Dialog.View.Width(min(area.Dx()-4, 60)).Render(view))
	return nil
}
