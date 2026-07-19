package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/panel"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/util/anim"
	"github.com/nextsko/mocode-agent/tools"
)

// responseContextHeight limits the number of lines displayed in tool output.
const responseContextHeight = 10

// toolBodyLeftPaddingTotal represents the padding that should be applied to each tool body
const toolBodyLeftPaddingTotal = 2

// ToolStatus represents the current state of a tool call.
type ToolStatus int

const (
	ToolStatusAwaitingPermission ToolStatus = iota
	ToolStatusRunning
	ToolStatusSuccess
	ToolStatusError
	ToolStatusCanceled
)

// ToolMessageItem represents a tool call message in the chat UI.
type ToolMessageItem interface {
	MessageItem

	ToolCall() message.ToolCall
	SetToolCall(tc message.ToolCall)
	Result() *message.ToolResult
	SetResult(res *message.ToolResult)
	MessageID() string
	SetMessageID(id string)
	SetStatus(status ToolStatus)
	Status() ToolStatus
}

// toolPanelView is set by the UI model for parallel agent panel display.
var toolPanelView *panel.View

// agentPanelResolver lets the UI layer provide task-aware panel grouping for
// nested agent tool calls.
var agentPanelResolver func(parentToolCallID string, params agent.AgentParams, nestedTools []ToolMessageItem) []panel.AgentPanelData

// SetToolPanelView sets the panel view reference for agent tool rendering.
func SetToolPanelView(pv *panel.View) {
	toolPanelView = pv
}

// SetAgentPanelResolver sets the resolver used to build task-aware agent
// panels for multi-agent tool calls.
func SetAgentPanelResolver(
	resolver func(parentToolCallID string, params agent.AgentParams, nestedTools []ToolMessageItem) []panel.AgentPanelData,
) {
	agentPanelResolver = resolver
}

// Compactable is an interface for tool items that can render in a compacted mode.
// When compact mode is enabled, tools render as a compact single-line header.
type Compactable interface {
	SetCompact(compact bool)
}

// SpinningState contains the state passed to SpinningFunc for custom spinning logic.
type SpinningState struct {
	ToolCall message.ToolCall
	Result   *message.ToolResult
	Status   ToolStatus
}

// IsCanceled returns true if the tool status is canceled.
func (s *SpinningState) IsCanceled() bool {
	return s.Status == ToolStatusCanceled
}

// HasResult returns true if the result is not nil.
func (s *SpinningState) HasResult() bool {
	return s.Result != nil
}

// SpinningFunc is a function type for custom spinning logic.
// Returns true if the tool should show the spinning animation.
type SpinningFunc func(state SpinningState) bool

// DefaultToolRenderContext implements the default [ToolRenderer] interface.
type DefaultToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (d *DefaultToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return "TODO: Implement Tool Renderer For: " + opts.ToolCall.Name
}

// ToolRenderOpts contains the data needed to render a tool call.
type ToolRenderOpts struct {
	ToolCall        message.ToolCall
	Result          *message.ToolResult
	Anim            *anim.Anim
	ExpandedContent bool
	Compact         bool
	IsSpinning      bool
	Status          ToolStatus
	PanelView       *panel.View
	AgentName       string
}

// IsPending returns true if the tool call is still pending (not finished and
// not canceled).
func (o *ToolRenderOpts) IsPending() bool {
	return !o.ToolCall.Finished && !o.IsCanceled()
}

// IsCanceled returns true if the tool status is canceled.
func (o *ToolRenderOpts) IsCanceled() bool {
	return o.Status == ToolStatusCanceled
}

// HasResult returns true if the result is not nil.
func (o *ToolRenderOpts) HasResult() bool {
	return o.Result != nil
}

// HasEmptyResult returns true if the result is nil or has empty content.
func (o *ToolRenderOpts) HasEmptyResult() bool {
	return o.Result == nil || o.Result.Content == ""
}

// ToolRenderer represents an interface for rendering tool calls.
type ToolRenderer interface {
	RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string
}

// ToolRendererFunc is a function type that implements the [ToolRenderer] interface.
type ToolRendererFunc func(sty *styles.Styles, width int, opts *ToolRenderOpts) string

// RenderTool implements the ToolRenderer interface.
func (f ToolRendererFunc) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	return f(sty, width, opts)
}

// baseToolMessageItem represents a tool call message that can be displayed in the UI.
type baseToolMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	toolRenderer ToolRenderer
	toolCall     message.ToolCall
	result       *message.ToolResult
	messageID    string
	status       ToolStatus
	// we use this so we can efficiently cache
	// tools that have a capped width (e.x bash.. and others)
	hasCappedWidth bool
	// isCompact indicates this tool should render in compact mode.
	isCompact bool
	// spinningFunc allows tools to override the default spinning logic.
	// If nil, uses the default: !toolCall.Finished && !canceled.
	spinningFunc SpinningFunc

	sty             *styles.Styles
	anim            *anim.Anim
	expandedContent bool
}

var _ Expandable = (*baseToolMessageItem)(nil)

// newBaseToolMessageItem is the internal constructor for base tool message items.
func newBaseToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	toolRenderer ToolRenderer,
	canceled bool,
) *baseToolMessageItem {
	// we only do full width for diffs (as far as I know)
	hasCappedWidth := toolCall.Name != tools.EditToolName && toolCall.Name != tools.MultiEditToolName

	status := ToolStatusRunning
	if canceled {
		status = ToolStatusCanceled
	}

	t := &baseToolMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		sty:                      sty,
		toolRenderer:             toolRenderer,
		toolCall:                 toolCall,
		result:                   result,
		status:                   status,
		hasCappedWidth:           hasCappedWidth,
	}
	t.anim = anim.New(anim.Settings{
		ID:          toolCall.ID,
		Size:        15,
		GradColorA:  sty.WorkingGradFromColor,
		GradColorB:  sty.WorkingGradToColor,
		LabelColor:  sty.WorkingLabelColor,
		CycleColors: true,
	})

	return t
}

// NewToolMessageItem creates a new [ToolMessageItem] based on the tool call name.
//
// It returns a specific tool message item type if implemented, otherwise it
// returns a generic tool message item. The messageID is the ID of the assistant
// message containing this tool call.
func NewToolMessageItem(
	sty *styles.Styles,
	messageID string,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	var item ToolMessageItem
	switch toolCall.Name {
	case tools.BashToolName:
		item = NewBashToolMessageItem(sty, toolCall, result, canceled)
	case tools.JobOutputToolName:
		item = NewJobOutputToolMessageItem(sty, toolCall, result, canceled)
	case tools.JobKillToolName:
		item = NewJobKillToolMessageItem(sty, toolCall, result, canceled)
	case tools.ViewToolName:
		item = NewViewToolMessageItem(sty, toolCall, result, canceled)
	case tools.WriteToolName:
		item = NewWriteToolMessageItem(sty, toolCall, result, canceled)
	case tools.EditToolName:
		item = NewEditToolMessageItem(sty, toolCall, result, canceled)
	case tools.MultiEditToolName:
		item = NewMultiEditToolMessageItem(sty, toolCall, result, canceled)
	case tools.ReadFilesToolName:
		item = NewReadFilesToolMessageItem(sty, toolCall, result, canceled)
	case tools.GlobToolName:
		item = NewGlobToolMessageItem(sty, toolCall, result, canceled)
	case tools.GrepToolName:
		item = NewGrepToolMessageItem(sty, toolCall, result, canceled)
	case tools.LSToolName:
		item = NewLSToolMessageItem(sty, toolCall, result, canceled)
	case tools.TransferToolName:
		item = NewTransferToolMessageItem(sty, toolCall, result, canceled)
	case tools.DownloadToolName:
		item = NewDownloadToolMessageItem(sty, toolCall, result, canceled)
	case tools.FetchToolName:
		item = NewFetchToolMessageItem(sty, toolCall, result, canceled)
	case tools.SourcegraphToolName:
		item = NewSourcegraphToolMessageItem(sty, toolCall, result, canceled)
	case tools.DiagnosticsToolName:
		item = NewDiagnosticsToolMessageItem(sty, toolCall, result, canceled)
	case agent.AgentToolName:
		item = NewAgentToolMessageItem(sty, toolCall, result, canceled)
	case tools.AgenticFetchToolName:
		item = NewAgenticFetchToolMessageItem(sty, toolCall, result, canceled)
	case tools.WebFetchToolName:
		item = NewWebFetchToolMessageItem(sty, toolCall, result, canceled)
	case tools.WebSearchToolName:
		item = NewWebSearchToolMessageItem(sty, toolCall, result, canceled)
	case tools.TodosToolName:
		item = NewTodosToolMessageItem(sty, toolCall, result, canceled)
	case tools.ReferencesToolName:
		item = NewReferencesToolMessageItem(sty, toolCall, result, canceled)
	case tools.LSPRestartToolName:
		item = NewLSPRestartToolMessageItem(sty, toolCall, result, canceled)
	default:
		if IsDockerMCPTool(toolCall.Name) {
			item = NewDockerMCPToolMessageItem(sty, toolCall, result, canceled)
		} else if strings.HasPrefix(toolCall.Name, "mcp_") {
			item = NewMCPToolMessageItem(sty, toolCall, result, canceled)
		} else {
			item = NewGenericToolMessageItem(sty, toolCall, result, canceled)
		}
	}
	item.SetMessageID(messageID)
	return item
}

// SetCompact implements the Compactable interface.
func (t *baseToolMessageItem) SetCompact(compact bool) {
	t.isCompact = compact
	t.clearCache()
}

// ID returns the unique identifier for this tool message item.
func (t *baseToolMessageItem) ID() string {
	return t.toolCall.ID
}

// StartAnimation starts the assistant message animation if it should be spinning.
func (t *baseToolMessageItem) StartAnimation() tea.Cmd {
	if !t.isSpinning() {
		return nil
	}
	return t.anim.Start()
}

// Animate progresses the assistant message animation if it should be spinning.
func (t *baseToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if !t.isSpinning() {
		return nil
	}
	return t.anim.Animate(msg)
}

// RawRender implements [MessageItem].
func (t *baseToolMessageItem) RawRender(width int) string {
	toolItemWidth := width - MessageLeftPaddingTotal
	if t.hasCappedWidth {
		toolItemWidth = cappedMessageWidth(width)
	}

	content, height, ok := t.getCachedRenderWithLog(toolItemWidth, "tool")
	// For agent tools, content structure is stable during spinning;
	// only re-render animation frames, not full tool output
	useCache := !t.isSpinning() || !isAgentToolRenderer(t.toolRenderer)
	if !ok || !useCache {
		content = t.toolRenderer.RenderTool(t.sty, toolItemWidth, &ToolRenderOpts{
			ToolCall:        t.toolCall,
			Result:          t.result,
			Anim:            t.anim,
			ExpandedContent: t.expandedContent,
			Compact:         t.isCompact,
			IsSpinning:      t.isSpinning(),
			Status:          t.computeStatus(),
			PanelView:       toolPanelView,
			AgentName:       t.toolCall.AgentName,
		})

		// Prepend hook indicator if hooks ran for this tool call.
		if t.result != nil {
			if hookLine := toolOutputHookIndicator(t.sty, t.result.Metadata, toolItemWidth); hookLine != "" {
				content = hookLine + "\n\n" + content
			}
		}
		// Prepend agent badge if set.
		if t.toolCall.AgentName != "" {
			badge := styles.AgentBadgeStyleFor(t.toolCall.AgentName).Render(t.toolCall.AgentName)
			content = badge + "\n" + content
		}

		height = lipgloss.Height(content)
		// cache the rendered content
		t.setCachedRender(content, toolItemWidth, height)
	}

	return t.renderHighlighted(content, toolItemWidth, height)
}

// Render renders the tool message item at the given width.
func (t *baseToolMessageItem) Render(width int) string {
	var prefix string
	if t.isCompact {
		prefix = t.sty.Messages.ToolCallCompact.Render()
	} else if t.focused {
		prefix = t.sty.Messages.ToolCallFocused.Render()
	} else {
		prefix = t.sty.Messages.ToolCallBlurred.Render()
	}
	lines := strings.Split(t.RawRender(width), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

// ToolCall returns the tool call associated with this message item.
func (t *baseToolMessageItem) ToolCall() message.ToolCall {
	return t.toolCall
}

// Result returns the tool result associated with this message item.
func (t *baseToolMessageItem) Result() *message.ToolResult {
	return t.result
}

// SetToolCall sets the tool call associated with this message item.
func (t *baseToolMessageItem) SetToolCall(tc message.ToolCall) {
	t.toolCall = tc
	t.clearCache()
}

// SetResult sets the tool result associated with this message item.
func (t *baseToolMessageItem) SetResult(res *message.ToolResult) {
	t.result = res
	t.clearCache()
}

// MessageID returns the ID of the message containing this tool call.
func (t *baseToolMessageItem) MessageID() string {
	return t.messageID
}

// SetMessageID sets the ID of the message containing this tool call.
func (t *baseToolMessageItem) SetMessageID(id string) {
	t.messageID = id
}

// SetStatus sets the tool status.
func (t *baseToolMessageItem) SetStatus(status ToolStatus) {
	t.status = status
	t.clearCache()
}

// Status returns the current tool status.
func (t *baseToolMessageItem) Status() ToolStatus {
	return t.status
}

// computeStatus computes the effective status considering the result.
func (t *baseToolMessageItem) computeStatus() ToolStatus {
	if t.result != nil {
		if t.result.IsError {
			return ToolStatusError
		}
		return ToolStatusSuccess
	}
	return t.status
}

// isSpinning returns true if the tool should show animation.
func (t *baseToolMessageItem) isSpinning() bool {
	if t.spinningFunc != nil {
		return t.spinningFunc(SpinningState{
			ToolCall: t.toolCall,
			Result:   t.result,
			Status:   t.status,
		})
	}
	return !t.toolCall.Finished && t.status != ToolStatusCanceled
}

// SetSpinningFunc sets a custom function to determine if the tool should spin.
func (t *baseToolMessageItem) SetSpinningFunc(fn SpinningFunc) {
	t.spinningFunc = fn
}

// ToggleExpanded toggles the expanded state of the thinking box.
func (t *baseToolMessageItem) ToggleExpanded() bool {
	t.expandedContent = !t.expandedContent
	t.clearCache()
	return t.expandedContent
}

// HandleMouseClick implements MouseClickable.
func (t *baseToolMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	return btn == ansi.MouseLeft
}

// HandleKeyEvent implements KeyEventHandler.
func (t *baseToolMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := t.formatToolForCopy()
		return true, common.CopyToClipboard(text, "Tool content copied to clipboard")
	}
	return false, nil
}

// pendingTool renders a tool that is still in progress with an animation.

// toolEarlyStateContent handles error/cancelled/pending states before content rendering.
// Returns the rendered output and true if early state was handled.

// toolErrorContent formats an error message with ERROR tag.

// toolIcon returns the status icon for a tool call.
// toolIcon returns the status icon for a tool call based on its status.

// toolParamList formats parameters as "main (key=value, ...)" with truncation.
// toolParamList formats tool parameters as "main (key=value, ...)" with truncation.

// toolHeader builds the tool header line: "● ToolName params..."

// toolOutputPlainContent renders plain text with optional expansion support.

// toolOutputCodeContent renders code with syntax highlighting and line numbers.

// toolOutputImageContent renders image data with size info.

// toolOutputSkillContent renders a skill loaded indicator.

// toolOutputHookIndicator renders hook indicator lines from tool metadata.
// Returns empty string if no hook metadata is present. Hook names are
// sanitized (newlines replaced with ¶) and truncated to fit the available
// horizontal space.

// truncateHookName truncates a hook name to fit within maxWidth cells,
// using left-truncation for absolute paths (e.g. `…/format.sh`) and
// right-truncation for everything else. Left-truncation is only applied
// when the name looks unambiguously like a path: absolute, single-line,
// and contains no spaces.

// isLikelyPath reports whether s looks unambiguously like a filesystem
// path, suitable for left-truncation. We accept absolute paths and
// relative paths that contain a separator and no shell-ish characters.

// renderHookLine renders a single hook indicator line with aligned columns.

// hookDetail returns the styled detail text for a single hook result.

// getDigits returns the number of digits in a number.

// formatSize formats byte size into human readable format.

// toolOutputDiffContent renders a diff between old and new content.

// formatTimeout converts timeout seconds to a duration string (e.g., "30s").
// Returns empty string if timeout is 0.

// formatNonZero returns string representation of non-zero integers, empty string for zero.

// toolOutputMultiEditDiffContent renders a diff with optional failed edits note.

// roundedEnumerator creates a tree enumerator with rounded corners.

// toolOutputMarkdownContent renders markdown content with optional truncation.

// formatToolForCopy formats the tool call for clipboard copying.

// formatParametersForCopy formats tool parameters for clipboard copying.

// formatResultForCopy formats tool results for clipboard copying.

// formatBashResultForCopy formats bash tool results for clipboard.

// formatViewResultForCopy formats view tool results for clipboard.

// formatEditResultForCopy formats edit tool results for clipboard.

// formatMultiEditResultForCopy formats multi-edit tool results for clipboard.

// formatWriteResultForCopy formats write tool results for clipboard.

// formatFetchResultForCopy formats fetch tool results for clipboard.

// formatAgenticFetchResultForCopy formats agentic fetch tool results for clipboard.

// formatWebFetchResultForCopy formats web fetch tool results for clipboard.

// formatAgentResultForCopy formats agent tool results for clipboard.

// prettifyToolName returns a human-readable name for tool names.

// isAgentToolRenderer reports whether r is the agent tool renderer.
func isAgentToolRenderer(r ToolRenderer) bool {
	if _, ok := r.(*AgentToolRenderContext); ok {
		return true
	}
	return false
}
