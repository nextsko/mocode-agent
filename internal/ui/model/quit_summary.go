package model

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/styles"
	"github.com/package-register/mocode/internal/util/infra"
)

type quitSummary struct {
	Status       string
	Duration     time.Duration
	Agent        string
	Model        string
	CWD          string
	UserMessages int
	AIMessages   int
	ToolMessages int
	PromptTokens int64
	OutputTokens int64
	FilesChanged int
	Additions    int
	Deletions    int
	SessionID    string
	SessionPath  string
	AppName      string
}

func (m *UI) quitWithSummary() tea.Cmd {
	return tea.Sequence(m.buildAndStoreQuitSummary(), tea.Quit)
}

func (m *UI) buildAndStoreQuitSummary() tea.Cmd {
	return func() tea.Msg {
		summary, err := m.buildQuitSummary(context.Background())
		if err != nil {
			slog.Error("Failed to build quit summary", "error", err)
			return nil
		}
		m.pendingQuitSummary = &summary
		return nil
	}
}

func (m *UI) QuitSummary() string {
	if m.pendingQuitSummary == nil {
		return ""
	}
	return renderQuitSummary(*m.pendingQuitSummary, m.com.Styles.Dialog.QuitSummary)
}

func (m *UI) buildQuitSummary(ctx context.Context) (quitSummary, error) {
	if m.session == nil {
		return quitSummary{}, fmt.Errorf("no active session")
	}
	msgs, err := m.com.Workspace.ListMessages(ctx, m.session.ID)
	if err != nil {
		return quitSummary{}, err
	}
	model := m.com.Workspace.AgentModel()
	cfg := m.com.Config()
	agentID := ""
	if cfg != nil && cfg.Options != nil {
		agentID = cfg.Options.ActiveMode
	}
	if agentID == "" {
		agentID = config.AgentDefault
	}
	cwd := m.com.Workspace.WorkingDir()
	filesChanged, additions, deletions := quitFileStats(m.sessionFiles)
	userMessages, aiMessages, toolMessages := quitMessageStats(msgs)
	status := "completed"
	if m.isAgentBusy() {
		status = "interrupted"
	}
	return quitSummary{
		Status:       status,
		Duration:     time.Since(m.startedAt).Truncate(time.Second),
		Agent:        agentID,
		Model:        strings.TrimSpace(model.ModelCfg.Provider + " / " + model.ModelCfg.Model),
		CWD:          cwd,
		UserMessages: userMessages,
		AIMessages:   aiMessages,
		ToolMessages: toolMessages,
		PromptTokens: m.session.PromptTokens,
		OutputTokens: m.session.CompletionTokens,
		FilesChanged: filesChanged,
		Additions:    additions,
		Deletions:    deletions,
		SessionID:    m.session.ID,
		SessionPath:  quitSessionPath(*m.session),
		AppName:      config.GetAppName(m.com.Config()),
	}, nil
}

func quitMessageStats(messages []message.Message) (users, assistants, tools int) {
	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			users++
		case message.Assistant:
			assistants++
		case message.Tool:
			tools++
		}
	}
	return users, assistants, tools
}

func quitFileStats(files []SessionFile) (changed, additions, deletions int) {
	for _, file := range files {
		changed++
		additions += file.Additions
		deletions += file.Deletions
	}
	return changed, additions, deletions
}

func quitSessionPath(s session.Session) string {
	return filepath.Join(infra.DataDir(), "sessions", session.HashID(s.ID), "history")
}

func renderQuitSummary(summary quitSummary, qs styles.QuitSummary) string {
	sessionHash := session.HashID(summary.SessionID)
	if len(sessionHash) > 7 {
		sessionHash = sessionHash[:7]
	}
	labelWidth := 10

	// Helper to render a key-value row
	makeRow := func(key, value string) string {
		return qs.Label.Render(fmt.Sprintf("%-*s", labelWidth, key)) + qs.Value.Render(value)
	}

	// Status with color-coded dot
	var statusStr string
	if summary.Status == "completed" {
		statusStr = qs.StatusCompleted.Render("● " + summary.Status)
	} else {
		statusStr = qs.StatusInterrupted.Render("● " + summary.Status)
	}

	// Build all rows
	var rows []string

	// ── Title ──
	rows = append(rows, qs.Title.Render(fmt.Sprintf(" %s session summary ", strings.ToLower(summary.AppName))), "")

	// ── Section 1: Session ──
	rows = append(rows, qs.SectionHeader.Render("── Session ──"))
	rows = append(rows, qs.Label.Render(fmt.Sprintf("%-*s", labelWidth, "status"))+statusStr)
	rows = append(rows, makeRow("duration", summary.Duration.String()))
	rows = append(rows, makeRow("agent", summary.Agent))
	rows = append(rows, makeRow("model", summary.Model))
	rows = append(rows, "")

	// ── Section 2: Statistics ──
	rows = append(rows, qs.SectionHeader.Render("── Statistics ──"))
	rows = append(rows, makeRow("messages", fmt.Sprintf("user %d / assistant %d / tool %d", summary.UserMessages, summary.AIMessages, summary.ToolMessages)))
	rows = append(rows, makeRow("tokens", fmt.Sprintf("prompt %d / completion %d / total %d", summary.PromptTokens, summary.OutputTokens, summary.PromptTokens+summary.OutputTokens)))
	rows = append(rows, makeRow("files", fmt.Sprintf("%d changed / +%d -%d", summary.FilesChanged, summary.Additions, summary.Deletions)))
	rows = append(rows, "")

	// ── Section 3: Info ──
	rows = append(rows, qs.SectionHeader.Render("── Info ──"))
	rows = append(rows, makeRow("cwd", summary.CWD))
	rows = append(rows, makeRow("session", summary.SessionID))
	rows = append(rows, makeRow("history", summary.SessionPath))
	rows = append(rows, "")

	// ── Resume ──
	rows = append(rows, qs.Resume.Render(fmt.Sprintf("%s --session %s", config.AppName(), sessionHash)))

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return qs.Frame.Render(content)
}
