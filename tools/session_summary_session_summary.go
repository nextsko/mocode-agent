package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/core/agent/toolutil"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/domain/session/sessionexport"
)

// SessionSummaryToolName is the registered name of the session_summary tool.
const SessionSummaryToolName = "session_summary"

// SessionSummaryParams holds input for session_summary.
type SessionSummaryParams struct {
	Action string `json:"action,omitempty" description:"Action: latest, status, or schedule. Defaults to latest"`
}

// SessionSummaryScheduler schedules summarization for a session after the current turn.
type SessionSummaryScheduler func(ctx context.Context, sessionID string) error

// SessionSummaryMetadata is structured metadata for session_summary responses.
type SessionSummaryMetadata struct {
	SessionID        string `json:"session_id"`
	Title            string `json:"title"`
	MessageCount     int64  `json:"message_count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	SummaryMessageID string `json:"summary_message_id,omitempty"`
	SummaryPath      string `json:"summary_path,omitempty"`
	Scheduled        bool   `json:"scheduled,omitempty"`
}

// NewSessionSummaryTool inspects or schedules summarization for the current session.
func NewSessionSummaryTool(sessions session.Service, messages message.Service, workingDir string, schedule SessionSummaryScheduler) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionSummaryToolName,
		"Inspect or schedule summarization for the current session.",
		func(ctx context.Context, params SessionSummaryParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := toolutil.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for summary")
			}
			currentSession, err := sessions.Get(ctx, sessionID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("get session: %w", err)
			}

			action := strings.TrimSpace(params.Action)
			if action == "" {
				action = "latest"
			}
			metadata := SessionSummaryMetadata{
				SessionID:        currentSession.ID,
				Title:            currentSession.Title,
				MessageCount:     currentSession.MessageCount,
				PromptTokens:     currentSession.PromptTokens,
				CompletionTokens: currentSession.CompletionTokens,
				SummaryMessageID: currentSession.SummaryMessageID,
				SummaryPath:      latestSummaryPath(workingDir, currentSession.ID),
			}

			switch action {
			case "status":
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(formatSessionSummaryStatus(metadata)), metadata), nil
			case "schedule":
				if schedule == nil {
					return fantasy.NewTextErrorResponse("summary scheduling is not available"), nil
				}
				if err := schedule(ctx, sessionID); err != nil {
					return fantasy.NewTextErrorResponse(err.Error()), nil
				}
				metadata.Scheduled = true
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse("Session summarization scheduled after the current turn."), metadata), nil
			case "latest":
				if currentSession.SummaryMessageID == "" {
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse("No summary exists for this session yet."), metadata), nil
				}
				summaryMessage, err := messages.Get(ctx, currentSession.SummaryMessageID)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("get summary message: %w", err)
				}
				content := strings.TrimSpace(summaryMessage.Content().Text)
				if content == "" {
					content = "Summary message exists but has no text content yet."
				}
				if metadata.SummaryPath == "" && workingDir != "" {
					result, err := sessionexport.ExportSummary(sessionexport.SummaryOptions{
						SessionID:  currentSession.ID,
						Title:      currentSession.Title,
						Content:    content,
						WorkingDir: workingDir,
						Now:        time.Now(),
					})
					if err != nil {
						return fantasy.ToolResponse{}, err
					}
					metadata.SummaryPath = result.Path
				}
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(content), metadata), nil
			default:
				return fantasy.NewTextErrorResponse("unsupported summary action: " + action), nil
			}
		},
	)
}

func latestSummaryPath(workingDir, sessionID string) string {
	if workingDir == "" || sessionID == "" {
		return ""
	}
	pattern := filepath.Join(workingDir, sessionexport.SummaryDir, "summary-"+sessionexport.SanitizeName(sessionID)+"-*.md")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	latest := matches[0]
	latestTime := time.Time{}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latest = match
			latestTime = info.ModTime()
		}
	}
	return latest
}

func formatSessionSummaryStatus(metadata SessionSummaryMetadata) string {
	var b strings.Builder
	b.WriteString("session: ")
	b.WriteString(metadata.SessionID)
	if metadata.Title != "" {
		b.WriteString("\ntitle: ")
		b.WriteString(metadata.Title)
	}
	b.WriteString(fmt.Sprintf("\nmessages: %d", metadata.MessageCount))
	b.WriteString(fmt.Sprintf("\ntokens: prompt %d / completion %d", metadata.PromptTokens, metadata.CompletionTokens))
	if metadata.SummaryMessageID == "" {
		b.WriteString("\nsummary: none")
	} else {
		b.WriteString("\nsummary: ")
		b.WriteString(metadata.SummaryMessageID)
	}
	if metadata.SummaryPath != "" {
		b.WriteString("\npath: ")
		b.WriteString(metadata.SummaryPath)
	}
	return b.String()
}
