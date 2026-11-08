package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/contract"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionexport"
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

// NewSessionSummaryPortTool is the framework-agnostic form of session_summary.
func NewSessionSummaryPortTool(sessions session.Service, messages message.Service, workingDir string, schedule SessionSummaryScheduler) contract.Tool {
	return contract.NewFunc(SessionSummaryToolName, "Inspect or schedule summarization for the current session.", map[string]any{
		"type": "object",
		"properties": map[string]any{
			schemaActionKey: map[string]any{
				"type":        "string",
				"description": "Action: latest, status, or schedule. Defaults to latest",
				schemaEnumKey: []string{"latest", "status", "schedule"},
			},
		},
	}, func(ctx context.Context, _ contract.ToolContext, args json.RawMessage) (contract.ToolResult, error) {
		var params SessionSummaryParams
		if len(args) > 0 {
			if err := json.Unmarshal(args, &params); err != nil {
				return contract.ToolResult{}, err
			}
		}
		text, metadata, err := runSessionSummary(ctx, sessions, messages, workingDir, schedule, params)
		if err != nil {
			return contract.ToolResult{Error: err}, nil //nolint:nilerr // Tool-level failures are reported in the result, not as a Go error.
		}
		return contract.ToolResult{Content: text, Metadata: map[string]any{metaResultKey: metadata}}, nil
	})
}

// runSessionSummary implements latest/status/schedule for the current session.
func runSessionSummary(
	ctx context.Context,
	sessions session.Service,
	messages message.Service,
	workingDir string,
	schedule SessionSummaryScheduler,
	params SessionSummaryParams,
) (string, SessionSummaryMetadata, error) {
	sessionID := toolutil.GetSessionFromContext(ctx)
	if sessionID == "" {
		return "", SessionSummaryMetadata{}, fmt.Errorf("session ID is required for summary")
	}
	currentSession, err := sessions.Get(ctx, sessionID)
	if err != nil {
		return "", SessionSummaryMetadata{}, fmt.Errorf("get session: %w", err)
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
		return formatSessionSummaryStatus(metadata), metadata, nil
	case "schedule":
		if schedule == nil {
			return "", metadata, fmt.Errorf("summary scheduling is not available")
		}
		if err := schedule(ctx, sessionID); err != nil {
			return "", metadata, err
		}
		metadata.Scheduled = true
		return "Session summarization scheduled after the current turn.", metadata, nil
	case "latest":
		if currentSession.SummaryMessageID == "" {
			return "No summary exists for this session yet.", metadata, nil
		}
		summaryMessage, err := messages.Get(ctx, currentSession.SummaryMessageID)
		if err != nil {
			return "", metadata, fmt.Errorf("get summary message: %w", err)
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
				return "", metadata, err
			}
			metadata.SummaryPath = result.Path
		}
		return content, metadata, nil
	default:
		return "", metadata, fmt.Errorf("unsupported summary action: %s", action)
	}
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
	fmt.Fprintf(&b, "\nmessages: %d", metadata.MessageCount)
	fmt.Fprintf(&b, "\ntokens: prompt %d / completion %d", metadata.PromptTokens, metadata.CompletionTokens)
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
