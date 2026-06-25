package message_export

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/agent/toolutil"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/session/sessionexport"
)

// MessageExportToolName is the registered name of the message_export tool.
const MessageExportToolName = "message_export"

// MessageExportParams holds input for message_export.
type MessageExportParams struct {
	MessageIDs []string `json:"message_ids,omitempty" description:"Specific message IDs to export. If empty, exports the last assistant reply."`
	Format     string   `json:"format,omitempty" description:"Export format: markdown, md, or obsidian-md. Defaults to markdown"`
}

// NewMessageExportTool exports selected messages to a recents export file.
func NewMessageExportTool(messages message.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		MessageExportToolName,
		"Export selected messages to .mocode/export/recents/<dir>_<time>.md. If no message_ids given, exports the last assistant reply.",
		func(ctx context.Context, params MessageExportParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			sessionID := toolutil.GetSessionFromContext(ctx)
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
