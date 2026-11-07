package agenttools

import (
	"context"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// MessageExportToolName is the registered name of the message_export tool.
const MessageExportToolName = "message_export"

// MessageExportParams holds input for message_export.
type MessageExportParams struct {
	MessageIDs []string `json:"message_ids,omitempty" description:"Specific message IDs to export. If empty, exports the last assistant reply."`
	Format     string   `json:"format,omitempty" description:"Export format: markdown, md, or obsidian-md. Defaults to markdown"`
}

// NewMessageExportTool is the legacy fantasy-typed constructor; new code should
// use NewMessageExportPortTool and wrap it at the agent boundary.
func NewMessageExportTool(messages message.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		MessageExportToolName,
		"Export selected messages to .mocode/export/recents/<dir>_<time>.md. If no message_ids given, exports the last assistant reply.",
		func(ctx context.Context, params MessageExportParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			text, meta, err := runMessageExport(ctx, messages, workingDir, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(text), meta), nil
		})
}
