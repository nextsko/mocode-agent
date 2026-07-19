package chat

import (
	"encoding/json"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
)

// NewTransferToolMessageItem creates a new tool message item for transfer_to_agent.
func NewTransferToolMessageItem(sty *styles.Styles, toolCall message.ToolCall, result *message.ToolResult, canceled bool) ToolMessageItem {
	t := &transferToolMessageItem{}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &TransferToolRenderer{}, canceled)
	t.spinningFunc = func(state SpinningState) bool {
		return !state.HasResult() && !state.IsCanceled()
	}
	return t
}

// transferToolMessageItem wraps baseToolMessageItem for transfer_to_agent tool calls.
type transferToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*transferToolMessageItem)(nil)

// TransferToolRenderer implements tool rendering for transfer_to_agent calls.
type TransferToolRenderer struct{}

// RenderTool renders a transfer_to_agent tool call showing source → target agent
// with coloured badges.
func (t *TransferToolRenderer) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	// Parse the transfer parameters from the tool call input.
	var params struct {
		AgentName string `json:"agent_name"`
		Message   string `json:"message,omitempty"`
	}
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return defaultTransferRender(sty, opts.ToolCall, opts.Status)
	}

	sourceAgent := opts.AgentName
	if sourceAgent == "" {
		sourceAgent = opts.ToolCall.Name
	}
	targetAgent := params.AgentName

	if sourceAgent == "" && targetAgent == "" {
		return defaultTransferRender(sty, opts.ToolCall, opts.Status)
	}

	// Render source → target with coloured badges.
	sourceBadge := styles.AgentBadgeStyleFor(sourceAgent).Render(sourceAgent)
	targetBadge := styles.AgentBadgeStyleFor(targetAgent).Render(targetAgent)
	arrow := sty.ToolCallSuccess.Render(" ─► ")

	header := sourceBadge + arrow + targetBadge

	// Add status icon.
	var icon string
	switch opts.Status {
	case ToolStatusRunning:
		icon = sty.Tool.IconPending.String()
	case ToolStatusSuccess:
		icon = sty.Tool.IconSuccess.String()
	case ToolStatusError:
		icon = sty.Tool.IconError.String()
	case ToolStatusCanceled:
		icon = sty.Tool.IconCancelled.String()
	}
	if icon != "" {
		header = icon + " " + header
	}

	// Show task description if present.
	if params.Message != "" {
		taskStyle := sty.Messages.NoContent.Width(max(0, width-4))
		taskContent := taskStyle.Render(params.Message)
		return lipgloss.JoinVertical(lipgloss.Left,
			sty.Tool.Body.Render(header),
			"",
			taskContent,
		)
	}

	return sty.Tool.Body.Render(header)
}

// defaultTransferRender renders a fallback view when transfer parameters can't be parsed.
func defaultTransferRender(sty *styles.Styles, tc message.ToolCall, status ToolStatus) string {
	icon := sty.Tool.IconPending.String()
	switch status {
	case ToolStatusSuccess:
		icon = sty.Tool.IconSuccess.String()
	case ToolStatusError:
		icon = sty.Tool.IconError.String()
	case ToolStatusCanceled:
		icon = sty.Tool.IconCancelled.String()
	}
	text := sty.Tool.NameNormal.Render("transfer_to_agent")
	detail := sty.Messages.NoContent.Render(tc.Input)
	return strings.TrimSpace(icon + " " + text + "\n" + detail)
}
