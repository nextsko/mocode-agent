package authhandler

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/integration/wechat"
	"github.com/nextsko/mocode-agent/internal/util/infra"
)

type wechatHandler struct{}

func init() {
	Register(wechatHandler{})
}

func (wechatHandler) ID() string { return "wechat" }

func (wechatHandler) Description() string { return "Authenticate WeChat bot by QR code" }

func (wechatHandler) Login(ctx context.Context, env Env) error {
	fmt.Fprintln(env.Stdout, "Authenticating WeChat...")
	wc := wechat.Default()
	if env.Workspace != nil {
		wc.SetSessionStore(infra.WeChatDir() + stringPathSeparator() + "sessions.json")
	}

	return wc.LoginWithCallbacks(ctx, true, wechat.LoginCallbacks{
		OnQRURL: func(qrURL string) {
			qr, err := wechat.GenerateQR(qrURL)
			if err != nil {
				fmt.Fprintf(env.Stderr, "failed to render QR: %v\n", err)
				fmt.Fprintf(env.Stdout, "QR URL: %s\n", qrURL)
				return
			}
			fmt.Fprintln(env.Stdout)
			fmt.Fprintln(env.Stdout, "Scan this QR code with WeChat and confirm on your phone:")
			fmt.Fprintln(env.Stdout)
			fmt.Fprintln(env.Stdout, qr.ASCII)
			fmt.Fprintln(env.Stdout)
		},
		OnScanned: func() {
			fmt.Fprintln(env.Stdout, "QR scanned. Confirm login in WeChat...")
		},
		OnLoggedIn: func(userID string) {
			fmt.Fprintln(env.Stdout)
			fmt.Fprintln(env.Stdout, "WeChat authenticated.")
			fmt.Fprintf(env.Stdout, "Account: %s\n", strings.TrimSpace(userID))
		},
	})
}

func stringPathSeparator() string { return string([]rune{filepathSeparator()}) }

func filepathSeparator() rune { return '/' }
