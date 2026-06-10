package dialog

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/styles"
	wechat "github.com/package-register/mocode/internal/wechat"
)

const (
	WeChatManagerID = "wechat_manager"
	wechatManagerMaxWidth  = 88
	wechatManagerMaxHeight = 24
	wechatManagerRefreshInterval = 2 * time.Second
)

type WeChatManagerTickMsg time.Time

type ActionWeChatReconnect struct{ AccountID string }
type ActionWeChatStart struct{ AccountID string }
type ActionWeChatStop struct{ AccountID string }
type ActionWeChatDelete struct{ AccountID string }

// WeChatManager is a two-panel dialog for managing WeChat accounts.
//
// Left panel: scrollable account list with active marker, name, and status.
// Right panel: selected account details with key-value rows and action hints.
//
// While open, the dialog schedules a [WeChatManagerTickMsg] every
// [wechatManagerRefreshInterval] so the on-screen status stays in sync with
// background connection changes.
type WeChatManager struct {
	com      *common.Common
	help     help.Model
	accounts []wechat.AccountInfo
	selected int

	pendingDelete string
	tickEnabled   bool

	keyMap struct {
		Up        key.Binding
		Down      key.Binding
		Select    key.Binding
		Reconnect key.Binding
		Toggle    key.Binding
		Delete    key.Binding
		New       key.Binding
		Close     key.Binding
		Confirm   key.Binding
		Cancel    key.Binding
	}
}

var _ Dialog = (*WeChatManager)(nil)

func NewWeChatManager(com *common.Common) (*WeChatManager, tea.Cmd) {
	d := &WeChatManager{com: com, tickEnabled: true}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Up = key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up"))
	d.keyMap.Down = key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down"))
	d.keyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "set active"))
	d.keyMap.Reconnect = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reconnect"))
	d.keyMap.Toggle = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start/stop"))
	d.keyMap.Delete = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete"))
	d.keyMap.New = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new account"))
	d.keyMap.Close = CloseKey
	d.keyMap.Confirm = key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm"))
	d.keyMap.Cancel = key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel"))
	return d, d.scheduleTick()
}

func (d *WeChatManager) scheduleTick() tea.Cmd {
	return tea.Tick(wechatManagerRefreshInterval, func(t time.Time) tea.Msg {
		return WeChatManagerTickMsg(t)
	})
}

func (d *WeChatManager) stopTick() { d.tickEnabled = false }

func (d *WeChatManager) ID() string { return WeChatManagerID }

func (d *WeChatManager) refreshAccounts() {
	d.accounts = wechat.GetManager().List()
	if d.selected >= len(d.accounts) {
		d.selected = max(0, len(d.accounts)-1)
	}
	if d.selected < 0 {
		d.selected = 0
	}
}

func (d *WeChatManager) HandleMsg(msg tea.Msg) Action {
	if _, ok := msg.(WeChatManagerTickMsg); ok {
		d.refreshAccounts()
		if !d.tickEnabled {
			return nil
		}
		return ActionCmd{Cmd: d.scheduleTick()}
	}

	d.refreshAccounts()
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if !d.tickEnabled {
			d.tickEnabled = true
			return ActionCmd{Cmd: d.scheduleTick()}
		}

		if d.pendingDelete != "" {
			switch {
			case key.Matches(msg, d.keyMap.Confirm):
				id := d.pendingDelete
				d.pendingDelete = ""
				return ActionWeChatDelete{AccountID: id}
			case key.Matches(msg, d.keyMap.Cancel):
				d.pendingDelete = ""
			}
			return nil
		}

		switch {
		case key.Matches(msg, d.keyMap.Close):
			d.stopTick()
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
		case key.Matches(msg, d.keyMap.Reconnect):
			if d.selected >= 0 && d.selected < len(d.accounts) {
				return ActionWeChatReconnect{AccountID: d.accounts[d.selected].ID}
			}
		case key.Matches(msg, d.keyMap.Toggle):
			if d.selected >= 0 && d.selected < len(d.accounts) {
				id := d.accounts[d.selected].ID
				if wechat.GetManager().IsRunning(id) {
					return ActionWeChatStop{AccountID: id}
				}
				return ActionWeChatStart{AccountID: id}
			}
		case key.Matches(msg, d.keyMap.Delete):
			if d.selected >= 0 && d.selected < len(d.accounts) {
				d.pendingDelete = d.accounts[d.selected].ID
			}
		case key.Matches(msg, d.keyMap.New):
			d.stopTick()
			return ActionOpenDialog{DialogID: WeChatQRID}
		}
	}
	return nil
}

// Draw renders the two-panel account manager.
func (d *WeChatManager) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	d.refreshAccounts()

	width := max(0, min(wechatManagerMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(wechatManagerMaxHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	heightOffset := t.Dialog.Title.GetVerticalFrameSize() +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize() +
		4 // title content + padding

	panelHeight := max(6, height-heightOffset)
	compact := innerWidth < 60
	leftWidth := min(30, max(20, innerWidth/3))
	rightWidth := max(26, innerWidth-leftWidth-1)
	if compact {
		leftWidth = innerWidth
		rightWidth = innerWidth
		panelHeight = max(4, panelHeight/2)
	}

	d.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "WeChat Accounts"

	listView := d.renderAccountList(t, leftWidth, panelHeight)
	detailView := d.renderAccountDetail(t, rightWidth, panelHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, listView, " ", detailView)
	if compact {
		body = lipgloss.JoinVertical(lipgloss.Left, listView, detailView)
	}
	rc.AddPart(body)

	if d.pendingDelete != "" {
		rc.AddPart(t.Dialog.PrimaryText.Render(
			fmt.Sprintf("Delete account %q? [y/n]", shortID(d.pendingDelete))))
	}

	rc.Help = d.help.View(d)
	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}

// renderAccountList builds the left panel with the account list.
func (d *WeChatManager) renderAccountList(t *styles.Styles, w, h int) string {
	innerW := max(0, w-2)
	header := t.Dialog.TitleText.Render(ansi.Truncate("Accounts", innerW, "…"))
	line := t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", innerW))

	var rows []string
	if len(d.accounts) == 0 {
		rows = append(rows, t.Dialog.ListItem.InfoBlurred.Render("  No accounts"))
		rows = append(rows, t.Dialog.ListItem.InfoBlurred.Render("  Press [n] to add"))
	} else {
		for i, a := range d.accounts {
			marker := "  "
			style := t.Dialog.NormalItem
			if i == d.selected {
				style = t.Dialog.SelectedItem
				marker = "▶ "
			}

			activeMark := "  "
			if a.IsActive {
				activeMark = "● "
			}

			statusIcon := wechatStatusIcon(a.Status)
			name := a.Name
			if name == "" || name == a.UserID {
				name = "wx:" + truncate(a.UserID, 10)
			}
			name = truncate(name, innerW-8)

			row := fmt.Sprintf("%s%s%s %s", marker, activeMark, statusIcon, name)
			rows = append(rows, style.Padding(0, 0).Render(row))
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	content := lipgloss.JoinVertical(lipgloss.Left, header, line, body)
	return lipgloss.NewStyle().Width(w).Height(h).Render(content)
}

// renderAccountDetail builds the right panel showing selected account info.
func (d *WeChatManager) renderAccountDetail(t *styles.Styles, w, h int) string {
	contentW := max(0, w-2)

	if len(d.accounts) == 0 || d.selected < 0 || d.selected >= len(d.accounts) {
		inner := t.Dialog.ListItem.InfoBlurred.Render("Select an account to view details")
		return lipgloss.NewStyle().Width(w).Height(h).PaddingLeft(1).Render(inner)
	}

	a := d.accounts[d.selected]

	header := t.Dialog.TitleText.Render("Details")
	if a.IsActive {
		badge := t.Dialog.TitleAccent.Render("active")
		gap := strings.Repeat(" ", max(1, contentW-lipgloss.Width(header)-lipgloss.Width(badge)))
		header += gap + badge
	}
	line := t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", contentW))

	statusStr := wechatStatusIcon(a.Status) + " " + string(a.Status)
	pollStr := "off"
	if wechat.GetManager().IsRunning(a.ID) {
		pollStr = "running"
	}

	parts := []string{
		header,
		line,
		"",
		wmSection(t, "Info", contentW),
		wmRow(t, "Name", truncate(a.Name, contentW-8), contentW),
		wmRow(t, "User ID", truncate(a.UserID, contentW-12), contentW),
		wmRow(t, "Status", statusStr, contentW),
		wmRow(t, "Polling", pollStr, contentW),
		"",
		wmSection(t, "Actions", contentW),
		t.Dialog.ListItem.InfoBlurred.Render("  [r] reconnect  [s] start/stop"),
		t.Dialog.ListItem.InfoBlurred.Render("  [d] delete      [n] new account"),
	}

	return lipgloss.NewStyle().Width(w).Height(h).PaddingLeft(1).Render(
		lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func wmSection(t *styles.Styles, title string, width int) string {
	title = t.Dialog.TitleText.Render(title)
	lineW := max(0, width-lipgloss.Width(title)-1)
	return title + " " + t.Dialog.ListItem.InfoBlurred.Render(strings.Repeat("─", lineW))
}

func wmRow(t *styles.Styles, label, value string, width int) string {
	labelStr := t.Dialog.ListItem.InfoBlurred.Render(label)
	valueW := max(0, width-lipgloss.Width(labelStr)-2)
	valueStr := t.Dialog.TitleText.Render(ansi.Truncate(value, valueW, "…"))
	return "  " + labelStr + "  " + valueStr
}

func wechatStatusIcon(status wechat.AccountStatus) string {
	switch status {
	case wechat.AccountStatusOnline:
		return "🟢"
	case wechat.AccountStatusOffline:
		return "⚫"
	case wechat.AccountStatusReconnecting:
		return "🟡"
	case wechat.AccountStatusExpired:
		return "🔴"
	case wechat.AccountStatusError:
		return "❌"
	default:
		return "⚪"
	}
}

func shortID(id string) string {
	if len(id) > 10 {
		return id[:10] + "…"
	}
	return id
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen > 1 {
		return s[:maxLen-1] + "…"
	}
	return s
}

func (d *WeChatManager) ShortHelp() []key.Binding {
	return []key.Binding{
		d.keyMap.Up,
		d.keyMap.Select,
		d.keyMap.New,
		d.keyMap.Close,
	}
}

func (d *WeChatManager) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{d.keyMap.Up, d.keyMap.Down, d.keyMap.Select, d.keyMap.Reconnect},
		{d.keyMap.Toggle, d.keyMap.Delete, d.keyMap.New, d.keyMap.Close},
	}
}
