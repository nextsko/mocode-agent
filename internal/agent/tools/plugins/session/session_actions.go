package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/agent/toolutil/shared"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/session/sessionexport"
)

const (
	SessionExportToolName  = "session_export"
	SessionSummaryToolName = "session_summary"
)

type SessionExportParams struct {
	Format string `json:"format,omitempty" description:"Export format: markdown, md, obsidian-md, or html. Defaults to markdown"`
	Scope  string `json:"scope,omitempty" description:"Export scope: all or recent10. Defaults to all"`
}

type SessionSummaryParams struct {
	Action string `json:"action,omitempty" description:"Action: latest, status, or schedule. Defaults to latest"`
}

type SessionSummaryScheduler func(ctx context.Context, sessionID string) error

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

func NewSessionExportTool(messages message.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionExportToolName,
		"Export the current session transcript to a local Markdown or HTML file.",
		func(ctx context.Context, params SessionExportParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := shared.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for export")
			}
			format := strings.TrimSpace(params.Format)
			if format == "" {
				format = "markdown"
			}
			scope := strings.TrimSpace(params.Scope)
			if scope == "" {
				scope = "all"
			}

			msgs, err := messages.List(ctx, sessionID)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("list messages: %w", err)
			}
			result, err := sessionexport.Export(msgs, sessionexport.Options{
				SessionID:  sessionID,
				Format:     format,
				Scope:      scope,
				WorkingDir: workingDir,
				Now:        time.Now(),
			})
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Session exported to %s", result.Path)),
				result,
			), nil
		},
	)
}

func NewSessionSummaryTool(sessions session.Service, messages message.Service, workingDir string, schedule SessionSummaryScheduler) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionSummaryToolName,
		"Inspect or schedule summarization for the current session.",
		func(ctx context.Context, params SessionSummaryParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := shared.GetSessionFromContext(ctx)
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

const MessageExportToolName = "message_export"

type MessageExportParams struct {
	MessageIDs []string `json:"message_ids,omitempty" description:"Specific message IDs to export. If empty, exports the last assistant reply."`
	Format     string   `json:"format,omitempty" description:"Export format: markdown, md, or obsidian-md. Defaults to markdown"`
}

func NewMessageExportTool(messages message.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		MessageExportToolName,
		"Export selected messages to .mocode/export/recents/<dir>_<time>.md. If no message_ids given, exports the last assistant reply.",
		func(ctx context.Context, params MessageExportParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := shared.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for export")
			}

			format := strings.TrimSpace(params.Format)
			if format == "" {
				format = "markdown"
			}

			var msgs []message.Message
			if len(params.MessageIDs) > 0 {
				for _, id := range params.MessageIDs {
					msg, err := messages.Get(ctx, id)
					if err != nil {
						return fantasy.ToolResponse{}, fmt.Errorf("get message %s: %w", id, err)
					}
					msgs = append(msgs, msg)
				}
			} else {
				all, err := messages.List(ctx, sessionID)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("list messages: %w", err)
				}
				for i := len(all) - 1; i >= 0; i-- {
					if all[i].Role == message.Assistant && all[i].Content().Text != "" {
						msgs = append(msgs, all[i])
						break
					}
				}
				if len(msgs) == 0 {
					return fantasy.NewTextErrorResponse("no assistant reply found to export"), nil
				}
			}

			result, err := sessionexport.ExportRecents(sessionexport.RecentExportOptions{
				Messages:   msgs,
				WorkingDir: workingDir,
				Format:     format,
				Now:        time.Now(),
			})
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(fmt.Sprintf("Exported %d message(s) to %s", len(msgs), result.Path)),
				result,
			), nil
		},
	)
}
