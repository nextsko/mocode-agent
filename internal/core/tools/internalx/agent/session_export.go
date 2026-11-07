package agenttools

import (
	"context"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
)

// SessionExportToolName is the registered name of the session_export tool.
const SessionExportToolName = "session_export"

// SessionExportParams holds input for session_export.
type SessionExportParams struct {
	Format string `json:"format,omitempty" description:"Export format: markdown, md, obsidian-md, or html. Defaults to markdown"`
	Scope  string `json:"scope,omitempty" description:"Export scope: all or recent10. Defaults to all"`
}

// NewSessionExportTool is the legacy fantasy-typed constructor; new code should
// use NewSessionExportPortTool and wrap it at the agent boundary.
func NewSessionExportTool(messages message.Service, workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SessionExportToolName,
		"Export the current session transcript to a local Markdown or HTML file.",
		func(ctx context.Context, params SessionExportParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			text, meta, err := runSessionExport(ctx, messages, workingDir, params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(text), meta), nil
		})
}
