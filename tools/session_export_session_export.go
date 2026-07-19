package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/domain/session/sessionexport"
)

// SessionExportToolName is the registered name of the session_export tool.
const SessionExportToolName = "session_export"

// SessionExportParams holds input for session_export.
type SessionExportParams struct {
	Format string `json:"format,omitempty" description:"Export format: markdown, md, obsidian-md, or html. Defaults to markdown"`
	Scope  string `json:"scope,omitempty" description:"Export scope: all or recent10. Defaults to all"`
}

// NewSessionExportTool exports the current session transcript to a local file.
func NewSessionExportTool(messages message.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionExportToolName,
		"Export the current session transcript to a local Markdown or HTML file.",
		func(ctx context.Context, params SessionExportParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := toolutil.GetSessionFromContext(ctx)
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
