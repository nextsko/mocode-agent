// butler.go - LLM-powered butler agent with persistent session and context.
package wechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ButlerContext carries the dependencies for the LLM butler.
type ButlerContext struct {
	Channel   *Channel
	Workspace ButlerWorkspace
}

// ButlerWorkspace is the subset of workspace.Workspace the butler needs.
type ButlerWorkspace interface {
	CreateSession(ctx context.Context, title string) (string, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	DeleteSession(ctx context.Context, sessionID string) error
	AgentRun(ctx context.Context, sessionID, prompt string) error
	ListMessages(ctx context.Context, sessionID string) ([]MsgInfo, error)
	AgentIsSessionBusy(ctx context.Context, sessionID string) bool
}

// SessionInfo is a lightweight session summary.
type SessionInfo struct {
	ID        string
	Title     string
	CreatedAt string
}

// MsgInfo is a lightweight message summary.
type MsgInfo struct {
	Role    string
	Content string
}

const (
	butlerAgentTimeout  = 5 * time.Minute
	butlerErrorReply    = "抱歉，处理出错了，请稍后重试。"
	butlerInitFailReply = "抱歉，系统正在初始化，请稍后再试。"
	butlerTimeoutReply  = "抱歉，处理超时，请稍后再试。"
	busyPollInterval    = 250 * time.Millisecond
)

type userButlerState struct {
	sessID      string
	initialized bool
}

// llmButlerHandler is the LLM-powered butler.
type llmButlerHandler struct {
	ctx    *ButlerContext
	mu     sync.Mutex
	states map[string]*userButlerState
	userMu sync.Map
}

// newButlerHandler creates the LLM butler handler.
func newButlerHandler(butlerCtx *ButlerContext) *llmButlerHandler {
	return &llmButlerHandler{ctx: butlerCtx}
}

func (h *llmButlerHandler) userState(userID string) *userButlerState {
	key := SessionKey(userID)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.states == nil {
		h.states = make(map[string]*userButlerState)
	}
	st, ok := h.states[key]
	if !ok {
		st = &userButlerState{}
		h.states[key] = st
	}
	return st
}

func (h *llmButlerHandler) userLock(userID string) *sync.Mutex {
	key := SessionKey(userID)
	v, _ := h.userMu.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (h *llmButlerHandler) butlerSessionID(userID string) string {
	st := h.userState(userID)
	h.mu.Lock()
	defer h.mu.Unlock()
	return st.sessID
}

// Handle processes a user message through the LLM butler.
func (h *llmButlerHandler) Handle(pollCtx context.Context, userID, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	if strings.HasPrefix(text, "/") {
		return h.handleButlerSlash(pollCtx, userID, text)
	}

	mu := h.userLock(userID)
	mu.Lock()
	defer mu.Unlock()

	if err := h.ensureSession(pollCtx, userID); err != nil {
		slog.Error("Butler session init failed", "userID", userID, "error", err)
		return butlerInitFailReply
	}

	sessID := h.butlerSessionID(userID)
	if sessID == "" {
		slog.Error("Butler session missing after init", "userID", userID)
		return butlerInitFailReply
	}

	stopTyping := h.ctx.Channel.StartTyping(pollCtx, userID)
	defer stopTyping()

	ctx, cancel := context.WithTimeout(pollCtx, butlerAgentTimeout)
	defer cancel()

	beforeCount, err := h.messageCount(ctx, sessID)
	if err != nil {
		slog.Error("Butler ListMessages failed", "userID", userID, "error", err)
		return butlerErrorReply
	}

	userPrompt := h.buildUserPrompt(ctx, userID, text, sessID)
	if err := h.ctx.Workspace.AgentRun(ctx, sessID, userPrompt); err != nil {
		slog.Error("Butler AgentRun failed", "userID", userID, "sessionID", sessID, "error", err)
		return butlerErrorReply
	}

	reply, err := h.waitForAssistantReply(ctx, sessID, beforeCount)
	if err != nil {
		slog.Error("Butler wait for reply failed", "userID", userID, "sessionID", sessID, "error", err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return butlerTimeoutReply
		}
		return butlerErrorReply
	}
	if reply == "" {
		return "处理完成。"
	}
	return reply
}

func (h *llmButlerHandler) messageCount(ctx context.Context, sessID string) (int, error) {
	msgs, err := h.ctx.Workspace.ListMessages(ctx, sessID)
	if err != nil {
		return 0, err
	}
	return len(msgs), nil
}

func (h *llmButlerHandler) waitForAssistantReply(ctx context.Context, sessID string, beforeCount int) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		busy := h.ctx.Workspace.AgentIsSessionBusy(ctx, sessID)
		msgs, err := h.ctx.Workspace.ListMessages(ctx, sessID)
		if err != nil {
			return "", err
		}

		reply := extractAssistantAfter(msgs, beforeCount)
		if reply != "" && !busy {
			return reply, nil
		}
		if !busy && len(msgs) > beforeCount {
			return reply, nil
		}

		time.Sleep(busyPollInterval)
	}
}

// extractAssistantAfter returns the latest non-empty assistant message after beforeCount.
func extractAssistantAfter(msgs []MsgInfo, beforeCount int) string {
	for i := len(msgs) - 1; i >= beforeCount; i-- {
		if msgs[i].Role == "assistant" && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func (h *llmButlerHandler) ensureSession(ctx context.Context, userID string) error {
	if h.ctx.Workspace == nil {
		return fmt.Errorf("workspace not available")
	}

	st := h.userState(userID)

	h.mu.Lock()
	if st.initialized {
		h.mu.Unlock()
		return nil
	}
	needsCreate := st.sessID == ""
	h.mu.Unlock()

	if needsCreate {
		sid, err := h.ctx.Workspace.CreateSession(ctx, "Butler: "+userID)
		if err != nil {
			return err
		}

		h.mu.Lock()
		if st.sessID == "" {
			st.sessID = sid
			slog.Info("Butler session created", "userID", userID, "sessionID", sid)
		} else {
			orphan := sid
			h.mu.Unlock()
			if delErr := h.ctx.Workspace.DeleteSession(ctx, orphan); delErr != nil {
				slog.Warn("Failed to delete orphan butler session", "sessionID", orphan, "error", delErr)
			}
			h.mu.Lock()
		}
		h.mu.Unlock()
	}

	h.mu.Lock()
	sessID := st.sessID
	initialized := st.initialized
	h.mu.Unlock()

	if initialized || sessID == "" {
		return nil
	}

	beforeCount, err := h.messageCount(ctx, sessID)
	if err != nil {
		return fmt.Errorf("list messages before init: %w", err)
	}

	prompt := "系统指令:\n\n" + ButlerSystemPrompt + "\n\n请确认你已理解以上指令，回复OK。"
	if err := h.ctx.Workspace.AgentRun(ctx, sessID, prompt); err != nil {
		return fmt.Errorf("system prompt injection: %w", err)
	}
	if _, err := h.waitForAssistantReply(ctx, sessID, beforeCount); err != nil {
		return fmt.Errorf("system prompt injection wait: %w", err)
	}

	h.mu.Lock()
	st.initialized = true
	h.mu.Unlock()
	slog.Info("Butler system prompt injected", "userID", userID, "sessionID", sessID)
	return nil
}

func (h *llmButlerHandler) buildUserPrompt(ctx context.Context, userID, text, butlerSessID string) string {
	var b strings.Builder

	sessions, err := h.ctx.Workspace.ListSessions(ctx)
	if err == nil && len(sessions) > 0 {
		b.WriteString("当前可用会话:\n")
		for _, s := range sessions {
			if s.ID == butlerSessID {
				continue
			}
			if strings.HasPrefix(s.Title, "Butler:") {
				continue
			}
			b.WriteString(fmt.Sprintf("- ID: %s  标题: %s  (%s)\n", s.ID, s.Title, s.CreatedAt))
		}
		b.WriteString("\n")
	}

	sessID, ok := h.ctx.Channel.GetSession(userID)
	if ok && sessID != "" {
		b.WriteString(fmt.Sprintf("用户 %s 的绑定会话 ID 是: %s\n\n", userID, sessID))
	}

	b.WriteString(fmt.Sprintf("用户 %s 说: %s", userID, text))
	return b.String()
}

func (h *llmButlerHandler) handleButlerSlash(ctx context.Context, userID, text string) string {
	parts := strings.SplitN(text, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	switch cmd {
	case "/new":
		return h.createSession(ctx, userID, args)
	case "/switch":
		if args == "" {
			return "用法: /switch <会话ID>"
		}
		return h.switchSession(ctx, userID, args)
	case "/delete":
		if args == "" {
			return "用法: /delete <会话ID>"
		}
		return h.deleteSession(ctx, args)
	default:
		return fmt.Sprintf("未知命令: %s", cmd)
	}
}

func (h *llmButlerHandler) switchSession(ctx context.Context, userID, sessionID string) string {
	if h.ctx.Workspace == nil {
		return "❌ 系统未就绪"
	}
	sessions, err := h.ctx.Workspace.ListSessions(ctx)
	if err != nil {
		return "❌ 无法列出会话"
	}
	found := false
	for _, s := range sessions {
		if s.ID == sessionID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Sprintf("❌ 会话不存在: %s", sessionID)
	}
	h.ctx.Channel.SetSession(userID, sessionID)
	return fmt.Sprintf("✅ 已切换到: %s", sessionID)
}

func (h *llmButlerHandler) createSession(ctx context.Context, userID, name string) string {
	if h.ctx.Workspace == nil {
		return "❌ 系统未就绪"
	}
	title := name
	if title == "" {
		title = fmt.Sprintf("WeChat: %s", userID)
	}
	sessionID, err := h.ctx.Workspace.CreateSession(ctx, title)
	if err != nil {
		return fmt.Sprintf("❌ 创建失败: %v", err)
	}
	h.ctx.Channel.SetSession(userID, sessionID)
	return fmt.Sprintf("✅ 会话已创建\n  ID: %s\n  标题: %s", sessionID, title)
}

func (h *llmButlerHandler) deleteSession(ctx context.Context, sessionID string) string {
	if h.ctx.Workspace == nil {
		return "❌ 系统未就绪"
	}
	if err := h.ctx.Workspace.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Sprintf("❌ 删除失败: %v", err)
	}
	h.ctx.Channel.ClearBindingsForSessionID(sessionID)
	return "✅ 会话已删除"
}
