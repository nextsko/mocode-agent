// Package gateway runs the long-lived WeChat bot entrypoint without import cycles.
package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/core/knowledge/memory"
	"github.com/package-register/mocode/internal/integration/wechat"
	"github.com/package-register/mocode/internal/transport/workspace"
	"github.com/package-register/mocode/internal/util/infra"
	"github.com/package-register/mocode/internal/util/log"
)

// Options configures the long-running gateway runtime.
type Options struct {
	Workspace workspace.Workspace
	Stdout    io.Writer
	Stderr    io.Writer
}

// WeChatGateway runs the WeChat bot as a persistent mocode gateway.
type WeChatGateway struct {
	ws     workspace.Workspace
	stdout io.Writer
	stderr io.Writer
	wc     *wechat.Channel
}

// NewWeChatGateway creates a WeChat gateway.
func NewWeChatGateway(opts Options) *WeChatGateway {
	return &WeChatGateway{
		ws:     opts.Workspace,
		stdout: opts.Stdout,
		stderr: opts.Stderr,
		wc:     wechat.Default(),
	}
}

// Start authenticates if needed, prints startup details, and blocks until cancelled.
func (g *WeChatGateway) Start(ctx context.Context) error {
	if g.ws == nil {
		return fmt.Errorf("workspace is required")
	}
	g.ws.PermissionSetSkipRequests(true)
	g.wc.SetSessionStore(filepath.Join(g.ws.WorkingDir(), ".mocode", "wechat", "sessions.json"))
	g.installAgentHandler()

	if !g.wc.IsLoggedIn() {
		fmt.Fprintln(g.stdout, "WeChat is not authenticated. Starting QR login...")
		if err := g.wc.LoginWithCallbacks(ctx, false, wechat.LoginCallbacks{
			OnQRURL: func(qrURL string) {
				qr, err := wechat.GenerateQR(qrURL)
				if err != nil {
					fmt.Fprintf(g.stderr, "failed to render QR: %v\n", err)
					fmt.Fprintf(g.stdout, "QR URL: %s\n", qrURL)
					return
				}
				fmt.Fprintln(g.stdout)
				fmt.Fprintln(g.stdout, "Scan this QR code with WeChat and confirm on your phone:")
				fmt.Fprintln(g.stdout)
				fmt.Fprintln(g.stdout, qr.ASCII)
				fmt.Fprintln(g.stdout)
			},
			OnScanned: func() { fmt.Fprintln(g.stdout, "QR scanned. Confirm login in WeChat...") },
		}); err != nil {
			return err
		}
	}

	g.PrintStartupSummary()
	return g.wc.Run(ctx)
}

func (g *WeChatGateway) installAgentHandler() {
	g.wc.SetSlashConfig(wechat.SlashConfig{
		CurrentModel: func() string {
			cfg := g.ws.Config()
			if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
				return large.Provider + "/" + large.Model
			}
			return ""
		},
		SmallModel: func() string {
			cfg := g.ws.Config()
			if small, ok := cfg.Models[config.SelectedModelTypeSmall]; ok {
				return small.Provider + "/" + small.Model
			}
			return ""
		},
		ListModels: func() []string {
			cfg := g.ws.Config()
			var result []string
			for id, item := range cfg.Providers.Seq2() {
				for _, m := range item.Models {
					result = append(result, id+"/"+m.ID)
				}
			}
			return result
		},
		SwitchModel: func(provider, model string) error {
			return g.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
				Provider: provider,
				Model:    model,
			})
		},
		TestModel: func(provider, model string) error { return nil },
	})

	g.wc.InitButler(&gatewayButlerWorkspace{g.ws})

	g.wc.SetAgentHandler(func(ctx context.Context, userID, text string, _ *wechat.IncomingMessage) (string, error) {
		ctx = memory.WithAppUserInContext(ctx, "mocode", wechat.SessionKey(userID))
		stopTyping := g.wc.StartTyping(ctx, userID)
		defer stopTyping()

		if !g.ws.AgentIsReady() {
			if err := g.ws.InitCoderAgent(ctx); err != nil {
				return "", fmt.Errorf("agent init: %w", err)
			}
		}

		var sessionID string
		if v, ok := g.wc.GetSession(userID); ok {
			sessionID = v
			if _, err := g.ws.GetSession(ctx, sessionID); err != nil {
				slog.Warn("WeChat cached session not found, creating new session", "sessionID", sessionID, "userID", userID)
				sessionID = ""
				g.wc.DelSession(userID)
			}
		}
		if sessionID == "" {
			sess, err := g.ws.CreateSession(ctx, "WeChat: "+userID)
			if err != nil {
				return "", fmt.Errorf("create session: %w", err)
			}
			sessionID = sess.ID
			g.wc.SetSession(userID, sessionID)
		}

		if err := g.ws.AgentRun(ctx, sessionID, text); err != nil {
			return "", err
		}
		return lastAssistantReply(ctx, g.ws, sessionID)
	})
}

type gatewayButlerWorkspace struct {
	ws workspace.Workspace
}

func (w *gatewayButlerWorkspace) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := w.ws.CreateSession(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (w *gatewayButlerWorkspace) GetSession(ctx context.Context, sessionID string) (string, error) {
	sess, err := w.ws.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return sess.Title, nil
}

func (w *gatewayButlerWorkspace) ListSessions(ctx context.Context) ([]wechat.SessionInfo, error) {
	sessions, err := w.ws.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = wechat.SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: time.Unix(s.CreatedAt, 0).Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

func (w *gatewayButlerWorkspace) DeleteSession(ctx context.Context, sessionID string) error {
	return w.ws.DeleteSession(ctx, sessionID)
}

func (w *gatewayButlerWorkspace) AgentRun(ctx context.Context, sessionID, prompt string) error {
	return w.ws.AgentRun(ctx, sessionID, prompt)
}

func (w *gatewayButlerWorkspace) ListMessages(ctx context.Context, sessionID string) ([]wechat.MsgInfo, error) {
	msgs, err := w.ws.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.MsgInfo, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, wechat.MsgInfo{
			Role:    string(m.Role),
			Content: m.Content().Text,
		})
	}
	return result, nil
}

func (w *gatewayButlerWorkspace) AgentIsSessionBusy(_ context.Context, sessionID string) bool {
	return w.ws.AgentIsSessionBusy(sessionID)
}

func (w *gatewayButlerWorkspace) CurrentModel() string {
	cfg := w.ws.Config()
	if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
		return large.Provider + "/" + large.Model
	}
	return ""
}

func (w *gatewayButlerWorkspace) UpdateModel(provider, model string) error {
	return w.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider: provider,
		Model:    model,
	})
}

func lastAssistantReply(ctx context.Context, ws workspace.Workspace, sessionID string) (string, error) {
	msgs, err := ws.ListMessages(ctx, sessionID)
	if err != nil || len(msgs) == 0 {
		return "处理完成。", nil
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Content().Text, nil
		}
	}
	return "处理完成。", nil
}

// PrintStartupSummary writes the gateway runtime summary.
func (g *WeChatGateway) PrintStartupSummary() {
	cfg := g.ws.Config()
	fmt.Fprintln(g.stdout)
	fmt.Fprintln(g.stdout, "Mocode Gateway starting...")
	fmt.Fprintln(g.stdout)
	fmt.Fprintln(g.stdout, "Workspace")
	fmt.Fprintf(g.stdout, "  Path:              %s\n", g.ws.WorkingDir())
	fmt.Fprintf(g.stdout, "  Data directory:    %s\n", infra.DataDir())
	fmt.Fprintf(g.stdout, "  Log path:          %s\n", log.MainLogPath(infra.DataDir()))
	fmt.Fprintln(g.stdout)

	selected := cfg.Models[config.SelectedModelTypeLarge]
	model := cfg.GetModelByType(config.SelectedModelTypeLarge)
	contextWindow := int64(0)
	if model != nil {
		contextWindow = model.ContextWindow
	}
	fmt.Fprintln(g.stdout, "Model")
	fmt.Fprintf(g.stdout, "  Current model:     %s/%s\n", selected.Provider, selected.Model)
	if small, ok := cfg.Models[config.SelectedModelTypeSmall]; ok {
		fmt.Fprintf(g.stdout, "  Small model:       %s/%s\n", small.Provider, small.Model)
	}
	fmt.Fprintf(g.stdout, "  Context window:    %d\n", contextWindow)
	fmt.Fprintln(g.stdout)

	fmt.Fprintln(g.stdout, "Permissions")
	fmt.Fprintln(g.stdout, "  Mode:              yolo")
	fmt.Fprintln(g.stdout, "  Auto approve:      enabled")
	fmt.Fprintln(g.stdout)

	fmt.Fprintln(g.stdout, "MCP")
	fmt.Fprintf(g.stdout, "  Enabled servers:   %s\n", strings.Join(enabledMCPNames(g.ws), ", "))
	fmt.Fprintln(g.stdout)

	fmt.Fprintln(g.stdout, "Tools")
	fmt.Fprintf(g.stdout, "  Enabled tools:     %s\n", enabledGatewayTools(cfg))
	fmt.Fprintln(g.stdout)

	account := "unknown"
	if g.wc.Credentials != nil && strings.TrimSpace(g.wc.Credentials.UserID) != "" {
		account = g.wc.Credentials.UserID
	}
	fmt.Fprintln(g.stdout, "WeChat")
	fmt.Fprintln(g.stdout, "  Status:            authenticated")
	fmt.Fprintf(g.stdout, "  Bot account:       %s\n", account)
	fmt.Fprintln(g.stdout)
	fmt.Fprintln(g.stdout, "Gateway is running. Press Ctrl+C to stop.")
	fmt.Fprintln(g.stdout)
}

func enabledMCPNames(ws workspace.Workspace) []string {
	states := ws.MCPGetStates()
	if len(states) == 0 {
		return []string{"none"}
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func enabledGatewayTools(cfg *config.Config) string {
	if agent, ok := cfg.Agents[config.AgentCoder]; ok && len(agent.AllowedTools) > 0 {
		tools := append([]string(nil), agent.AllowedTools...)
		sort.Strings(tools)
		return strings.Join(tools, ", ")
	}
	return "all configured tools"
}
