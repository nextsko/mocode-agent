package model

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/integration/wechat"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
	"github.com/nextsko/mocode-agent/internal/ui/util"
)

func injectWeChatButler(ch *wechat.Channel, ws workspace.Workspace) {
	ch.SetSlashConfig(wechat.SlashConfig{
		CurrentModel: func() string {
			cfg := ws.Config()
			if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
				return large.Provider + "/" + large.Model
			}
			return ""
		},
		SmallModel: func() string {
			cfg := ws.Config()
			if small, ok := cfg.Models[config.SelectedModelTypeSmall]; ok {
				return small.Provider + "/" + small.Model
			}
			return ""
		},
		ListModels: func() []string {
			cfg := ws.Config()
			var result []string
			for id, item := range cfg.Providers.Seq2() {
				for _, m := range item.Models {
					result = append(result, id+"/"+m.ID)
				}
			}
			return result
		},
		SwitchModel: func(provider, model string) error {
			return ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
				Provider: provider,
				Model:    model,
			})
		},
		TestModel: func(provider, model string) error { return nil },
	})
	ch.InitButler(&tuiButlerWorkspace{ws})
}

type tuiButlerWorkspace struct {
	ws workspace.Workspace
}

func (w *tuiButlerWorkspace) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := w.ws.CreateSession(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (w *tuiButlerWorkspace) GetSession(ctx context.Context, id string) (string, error) {
	sess, err := w.ws.GetSession(ctx, id)
	if err != nil {
		return "", err
	}
	return sess.Title, nil
}

func (w *tuiButlerWorkspace) ListSessions(ctx context.Context) ([]wechat.SessionInfo, error) {
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

func (w *tuiButlerWorkspace) DeleteSession(ctx context.Context, id string) error {
	return w.ws.DeleteSession(ctx, id)
}

func (w *tuiButlerWorkspace) AgentRun(ctx context.Context, id, prompt string) error {
	return w.ws.AgentRun(ctx, id, prompt)
}

func (w *tuiButlerWorkspace) ListMessages(ctx context.Context, id string) ([]wechat.MsgInfo, error) {
	msgs, err := w.ws.ListMessages(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]wechat.MsgInfo, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, wechat.MsgInfo{Role: string(m.Role), Content: m.Content().Text})
	}
	return result, nil
}

func (w *tuiButlerWorkspace) AgentIsSessionBusy(_ context.Context, sessionID string) bool {
	return w.ws.AgentIsSessionBusy(sessionID)
}

func (w *tuiButlerWorkspace) CurrentModel() string {
	cfg := w.ws.Config()
	if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
		return large.Provider + "/" + large.Model
	}
	return ""
}

func (w *tuiButlerWorkspace) UpdateModel(provider, model string) error {
	return w.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider: provider,
		Model:    model,
	})
}

func (m *UI) handleWeChatLogin() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		mgr := wechat.GetManager()
		qrCh := make(chan string, 1)

		go func() {
			info, err := mgr.Login(ctx, true, wechat.LoginCallbacks{
				OnQRURL: func(qrURL string) {
					select {
					case qrCh <- qrURL:
					default:
					}
				},
			}, m.com.Config().HTTPClient(m.com.Workspace.Resolver(), 45*time.Second))
			if err != nil {
				slog.Error("WeChat login failed", "error", err)
				return
			}
			if info != nil {
				slog.Info("WeChat account registered", "userID", info.UserID, "accountID", info.ID)
				if ch := mgr.GetActive(); ch != nil {
					injectWeChatButler(ch, m.com.Workspace)
				}
			}
		}()

		select {
		case qrURL := <-qrCh:
			return util.NewInfoMsg("WeChat QR: " + qrURL)
		case <-time.After(5 * time.Second):
			return util.NewInfoMsg("WeChat login: waiting for QR code...")
		}
	}
}

func (m *UI) handleWeChatLogout() tea.Cmd {
	return func() tea.Msg {
		wechat.GetManager().StopAll()
		return util.NewInfoMsg("WeChat disconnected")
	}
}
