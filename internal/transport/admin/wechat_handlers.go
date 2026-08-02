package admin

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/config"
	wechat "github.com/nextsko/mocode-agent/internal/integration/wechat"
	"github.com/nextsko/mocode-agent/internal/transport/workspace"
	"github.com/nextsko/mocode-agent/internal/util/infra"
)

type wechatState struct {
	mu      sync.Mutex
	status  string
	qr      string
	qrImage string
	userID  string
	err     string
	active  bool
}

func (s *Server) handleWeChatStart(w http.ResponseWriter, r *http.Request) {
	s.wechat.mu.Lock()
	if s.wechat.active {
		state := s.wechat.snapshot()
		s.wechat.mu.Unlock()
		writeJSON(w, state)
		return
	}
	s.wechat.status = "generating"
	s.wechat.qr = ""
	s.wechat.qrImage = ""
	s.wechat.userID = ""
	s.wechat.err = ""
	s.wechat.active = true
	s.wechat.mu.Unlock()
	go s.loginWeChat()
	writeJSON(w, map[string]any{"ok": true, "status": "generating"})
}

func (s *Server) handleWeChatStatus(w http.ResponseWriter, r *http.Request) {
	s.wechat.mu.Lock()
	defer s.wechat.mu.Unlock()
	writeJSON(w, s.wechat.snapshot())
}

func (s *Server) loginWeChat() {
	wc := wechat.Default()
	wc.SetSessionStore(filepath.Join(infra.WeChatDir(), "sessions.json"))
	wc.SetAgentHandler(func(ctx context.Context, userID, text string, _ *wechat.IncomingMessage) (string, error) {
		sessKey := "wx:" + userID
		stopTyping := wc.StartTyping(ctx, userID)
		defer stopTyping()

		if !s.workspace.AgentIsReady() {
			if err := s.workspace.InitCoderAgent(ctx); err != nil {
				return "", fmt.Errorf("agent init: %w", err)
			}
		}

		var sessionID string
		if v, ok := wc.GetSession(sessKey); ok {
			sessionID = v
		}
		if sessionID == "" {
			sess, err := s.workspace.CreateSession(ctx, "WeChat: "+userID)
			if err != nil {
				return "", fmt.Errorf("create session: %w", err)
			}
			sessionID = sess.ID
			wc.SetSession(sessKey, sessionID)
		}

		if err := s.workspace.AgentRun(ctx, sessionID, text); err != nil {
			return "", err
		}
		msgs, err := s.workspace.ListMessages(ctx, sessionID)
		if err != nil || len(msgs) == 0 {
			return "处理完成。", nil
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "assistant" {
				return msgs[i].Content().Text, nil
			}
		}
		return "处理完成。", nil
	})
	err := wc.LoginWithCallbacks(context.Background(), true, wechat.LoginCallbacks{
		OnQRURL: func(qrURL string) {
			qr, genErr := wechat.GenerateQR(qrURL)
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			if genErr != nil {
				s.wechat.status = "error"
				s.wechat.err = genErr.Error()
				s.wechat.active = false
				return
			}
			s.wechat.status = "scan"
			s.wechat.qr = qr.ASCII
			s.wechat.qrImage = qr.PNGDataURL
		},
		OnScanned: func() {
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			s.wechat.status = "scanned"
		},
		OnExpired: func() {
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			s.wechat.status = "expired"
		},
		OnLoggedIn: func(userID string) {
			s.wechat.mu.Lock()
			defer s.wechat.mu.Unlock()
			s.wechat.status = "connected"
			s.wechat.userID = userID
		},
	}, s.workspace.Config().HTTPClient(s.workspace.Resolver(), 45*time.Second))
	s.wechat.mu.Lock()
	defer s.wechat.mu.Unlock()
	if err != nil {
		s.wechat.status = "error"
		s.wechat.err = err.Error()
		s.wechat.active = false
		return
	}
	s.wechat.status = "connected"
	if wc.Credentials != nil {
		s.wechat.userID = wc.Credentials.UserID
	}
	s.wechat.active = false
	// Initialize butler routing on login.
	wc.InitButler(&adminButlerWorkspace{s.workspace})
	go func() {
		_ = wc.Run(context.Background())
	}()
}

func (w *wechatState) snapshot() map[string]any {
	return map[string]any{
		"status":   w.status,
		"qr":       w.qr,
		"qr_image": w.qrImage,
		"user_id":  w.userID,
		"error":    w.err,
		"active":   w.active,
	}
}

// adminButlerWorkspace adapts workspace.Workspace to wechat.ButlerWorkspace.
type adminButlerWorkspace struct {
	ws workspace.Workspace
}

func (w *adminButlerWorkspace) CreateSession(ctx context.Context, title string) (string, error) {
	sess, err := w.ws.CreateSession(ctx, title)
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (w *adminButlerWorkspace) GetSession(ctx context.Context, id string) (string, error) {
	sess, err := w.ws.GetSession(ctx, id)
	if err != nil {
		return "", err
	}
	return sess.Title, nil
}

func (w *adminButlerWorkspace) ListSessions(ctx context.Context) ([]wechat.SessionInfo, error) {
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

func (w *adminButlerWorkspace) DeleteSession(ctx context.Context, id string) error {
	return w.ws.DeleteSession(ctx, id)
}

func (w *adminButlerWorkspace) AgentRun(ctx context.Context, id, prompt string) error {
	return w.ws.AgentRun(ctx, id, prompt)
}

func (w *adminButlerWorkspace) ListMessages(ctx context.Context, id string) ([]wechat.MsgInfo, error) {
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

func (w *adminButlerWorkspace) AgentIsSessionBusy(_ context.Context, sessionID string) bool {
	return w.ws.AgentIsSessionBusy(sessionID)
}

func (w *adminButlerWorkspace) CurrentModel() string {
	cfg := w.ws.Config()
	if large, ok := cfg.Models[config.SelectedModelTypeLarge]; ok {
		return large.Provider + "/" + large.Model
	}
	return ""
}

func (w *adminButlerWorkspace) UpdateModel(provider, model string) error {
	return w.ws.UpdatePreferredModel(config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{
		Provider: provider,
		Model:    model,
	})
}
