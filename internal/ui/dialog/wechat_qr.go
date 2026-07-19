package dialog

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	wechat "github.com/nextsko/mocode-agent/internal/integration/wechat"
	"github.com/nextsko/mocode-agent/internal/ui/common"
)

const (
	WeChatQRID = "wechat_qr"
)

type WeChatQRState int

const (
	WeChatQRStateGenerating WeChatQRState = iota
	WeChatQRStateDisplay
	WeChatQRStateScanned
	WeChatQRStateLoggedIn
	WeChatQRStateError
)

type WeChatQRMsg struct {
	State   WeChatQRState
	QRASCII string
	Error   string
	UserID  string
}

type WeChatQRPollMsg struct{}

// WeChatQR represents a dialog for WeChat QR login via AccountManager.
type WeChatQR struct {
	com       *common.Common
	help      help.Model
	state     WeChatQRState
	qrASCII   string
	errMsg    string
	userID    string
	loginDone chan WeChatQRMsg
	cancelFn  context.CancelFunc
	started   bool
	client    *http.Client

	keyMap struct {
		Close   key.Binding
		Refresh key.Binding
	}
}

var _ Dialog = (*WeChatQR)(nil)

func NewWeChatQR(com *common.Common) (*WeChatQR, error) {
	d := &WeChatQR{
		com:       com,
		state:     WeChatQRStateGenerating,
		loginDone: make(chan WeChatQRMsg, 5),
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	d.help = h

	d.keyMap.Close = CloseKey
	d.keyMap.Refresh = key.NewBinding(
		key.WithKeys("r", "enter"),
		key.WithHelp("r/enter", "retry/confirm"),
	)

	return d, nil
}

// StartLogin begins the login flow using AccountManager so the new account
// is automatically registered, credentials persisted, and the poll loop can
// be started from the manager dialog afterwards.
func (d *WeChatQR) StartLogin() {
	if d.started {
		return
	}
	d.started = true
	d.state = WeChatQRStateGenerating

	ctx, cancel := context.WithCancel(context.Background())
	d.cancelFn = cancel
	mgr := wechat.GetManager()

	go func() {
		defer cancel()

		info, err := mgr.Login(ctx, true, wechat.LoginCallbacks{
			OnQRURL: func(qrURL string) {
				qr, genErr := wechat.GenerateQR(qrURL)
				if genErr != nil {
					d.sendMsg(ctx, WeChatQRMsg{State: WeChatQRStateError, Error: genErr.Error()})
					return
				}
				d.sendMsg(ctx, WeChatQRMsg{State: WeChatQRStateDisplay, QRASCII: qr.ASCII})
			},
			OnScanned: func() {
				d.sendMsg(ctx, WeChatQRMsg{State: WeChatQRStateScanned})
			},
			OnExpired: func() {
				d.sendMsg(ctx, WeChatQRMsg{State: WeChatQRStateGenerating})
			},
			OnLoggedIn: func(userID string) {
				d.sendMsg(ctx, WeChatQRMsg{State: WeChatQRStateLoggedIn, UserID: userID})
			},
		}, d.client)
		if err != nil {
			select {
			case d.loginDone <- WeChatQRMsg{State: WeChatQRStateError, Error: err.Error()}:
			case <-ctx.Done():
			}
			return
		}

		if info != nil {
			d.sendMsg(ctx, WeChatQRMsg{State: WeChatQRStateLoggedIn, UserID: info.UserID})
		}
	}()
}

func (d *WeChatQR) sendMsg(ctx context.Context, msg WeChatQRMsg) {
	select {
	case d.loginDone <- msg:
	case <-ctx.Done():
	}
}

func (d *WeChatQR) PollLoginCmd() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-d.loginDone:
			if !ok {
				return nil
			}
			return msg
		case <-time.After(250 * time.Millisecond):
			return WeChatQRPollMsg{}
		}
	}
}

func (d *WeChatQR) SetHTTPClient(client *http.Client) {
	d.client = client
}

func (d *WeChatQR) ID() string { return WeChatQRID }

func (d *WeChatQR) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case WeChatQRMsg:
		d.state = msg.State
		if msg.QRASCII != "" {
			d.qrASCII = msg.QRASCII
		}
		d.errMsg = msg.Error
		d.userID = msg.UserID
		return ActionCmd{Cmd: d.PollLoginCmd()}

	case WeChatQRPollMsg:
		if d.state == WeChatQRStateLoggedIn || d.state == WeChatQRStateError {
			return nil
		}
		return ActionCmd{Cmd: d.PollLoginCmd()}

	case tea.KeyPressMsg:
		if d.state == WeChatQRStateLoggedIn {
			if key.Matches(msg, d.keyMap.Close, d.keyMap.Refresh) {
				return ActionClose{}
			}
		}

		if key.Matches(msg, d.keyMap.Close) {
			d.cleanup()
			return ActionClose{}
		}
		if key.Matches(msg, d.keyMap.Refresh) {
			if d.state == WeChatQRStateError {
				d.started = false
				d.StartLogin()
				return ActionCmd{Cmd: d.PollLoginCmd()}
			}
		}
	}
	return nil
}

func (d *WeChatQR) cleanup() {
	if d.cancelFn != nil && d.state != WeChatQRStateLoggedIn {
		d.cancelFn()
		d.cancelFn = nil
	}
}

func (d *WeChatQR) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles

	maxW := area.Dx() - t.Dialog.View.GetHorizontalFrameSize() - 4
	width := max(30, min(60, maxW))

	rc := NewRenderContext(t, width)

	switch d.state {
	case WeChatQRStateGenerating:
		rc.Title = "WeChat Login"
		rc.AddPart(t.Dialog.PrimaryText.Render("Generating QR code..."))

	case WeChatQRStateDisplay:
		rc.Title = "Scan with WeChat"
		rc.AddPart(t.Dialog.PrimaryText.Render("Scan this code in WeChat, then confirm on your phone."))
		rc.AddPart(renderQRPanel(d.qrASCII))

	case WeChatQRStateScanned:
		rc.Title = "Confirm in WeChat"
		rc.AddPart(t.Dialog.PrimaryText.Render("QR scanned. Confirm login in WeChat."))
		rc.AddPart(renderQRPanel(d.qrASCII))

	case WeChatQRStateLoggedIn:
		rc.Title = "WeChat Connected"
		rc.AddPart(t.Dialog.PrimaryText.Render(
			fmt.Sprintf("Logged in: %s\n\nAccount registered. Press Enter to close.\nOpen /wechat to manage accounts.", d.userID),
		))

	case WeChatQRStateError:
		rc.Title = "WeChat Error"
		rc.AddPart(t.Dialog.PrimaryText.Render(fmt.Sprintf("Error: %s\nPress r to retry.", d.errMsg)))
	}

	rc.Help = d.help.View(d)
	view := rc.Render()
	DrawCenter(scr, area, view)
	return nil
}

func renderQRPanel(qrASCII string) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#FFFFFF")).
		Foreground(lipgloss.Color("#000000")).
		Padding(1, 2).
		Render(qrASCII)
}

func (d *WeChatQR) ShortHelp() []key.Binding {
	switch d.state {
	case WeChatQRStateLoggedIn:
		return []key.Binding{d.keyMap.Refresh}
	case WeChatQRStateError:
		return []key.Binding{d.keyMap.Refresh, d.keyMap.Close}
	default:
		return []key.Binding{d.keyMap.Close}
	}
}

func (d *WeChatQR) FullHelp() [][]key.Binding {
	return [][]key.Binding{{d.keyMap.Close, d.keyMap.Refresh}}
}
