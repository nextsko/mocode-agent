package chat

import (
	"encoding/json"

	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/util/fsext"
	"github.com/nextsko/mocode-agent/tools"
)

// -----------------------------------------------------------------------------
// Diagnostics Tool
// -----------------------------------------------------------------------------

// DiagnosticsToolMessageItem is a message item that represents a diagnostics tool call.
type DiagnosticsToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*DiagnosticsToolMessageItem)(nil)

// NewDiagnosticsToolMessageItem creates a new [DiagnosticsToolMessageItem].
func NewDiagnosticsToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &DiagnosticsToolRenderContext{}, canceled)
}

// DiagnosticsToolRenderContext renders diagnostics tool messages.
type DiagnosticsToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (d *DiagnosticsToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)

	var params tools.DiagnosticsParams
	_ = json.Unmarshal([]byte(opts.ToolCall.Input), &params)

	// Show "project" if no file path, otherwise show the file path.
	mainParam := "project"
	if params.FilePath != "" {
		mainParam = fsext.PrettyPath(params.FilePath)
	}

	if opts.IsPending() {
		return pendingTool(sty, "Diagnostics", opts.Anim, opts.Compact, cappedWidth, mainParam)
	}

	header := toolHeader(sty, opts.Status, "Diagnostics", cappedWidth, opts.Compact, mainParam)
	if opts.Compact {
		return header
	}

	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if opts.HasEmptyResult() {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, opts.Result.Content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}
