package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/package-register/mocode/internal/agent"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/ui/anim"
	"github.com/package-register/mocode/internal/ui/panel"
	"github.com/package-register/mocode/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// Agent Tool
// -----------------------------------------------------------------------------

// NestedToolContainer is an interface for tool items that can contain nested tool calls.
type NestedToolContainer interface {
	NestedTools() []ToolMessageItem
	SetNestedTools(tools []ToolMessageItem)
	AddNestedTool(tool ToolMessageItem)
}

// AgentToolMessageItem is a message item that represents an agent tool call.
type AgentToolMessageItem struct {
	*baseToolMessageItem

	nestedTools []ToolMessageItem
}

var (
	_ ToolMessageItem     = (*AgentToolMessageItem)(nil)
	_ NestedToolContainer = (*AgentToolMessageItem)(nil)
)

// NewAgentToolMessageItem creates a new [AgentToolMessageItem].
func NewAgentToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) *AgentToolMessageItem {
	t := &AgentToolMessageItem{}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &AgentToolRenderContext{agent: t}, canceled)
	// For the agent tool we keep spinning until the tool call is finished.
	t.spinningFunc = func(state SpinningState) bool {
		return !state.HasResult() && !state.IsCanceled()
	}
	return t
}

// Animate progresses the message animation if it should be spinning.
func (a *AgentToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if a.result != nil || a.Status() == ToolStatusCanceled {
		return nil
	}
	if msg.ID == a.ID() {
		return a.anim.Animate(msg)
	}
	for _, nestedTool := range a.nestedTools {
		if msg.ID != nestedTool.ID() {
			continue
		}
		if s, ok := nestedTool.(Animatable); ok {
			return s.Animate(msg)
		}
	}
	return nil
}

// NestedTools returns the nested tools.
func (a *AgentToolMessageItem) NestedTools() []ToolMessageItem {
	return a.nestedTools
}

// SetNestedTools sets the nested tools.
func (a *AgentToolMessageItem) SetNestedTools(tools []ToolMessageItem) {
	a.nestedTools = tools
	a.clearCache()
}

// AddNestedTool adds a nested tool.
func (a *AgentToolMessageItem) AddNestedTool(tool ToolMessageItem) {
	// Mark nested tools as simple (compact) rendering.
	if s, ok := tool.(Compactable); ok {
		s.SetCompact(true)
	}
	a.nestedTools = append(a.nestedTools, tool)
	a.clearCache()
}

// AgentToolRenderContext renders agent tool messages.
type AgentToolRenderContext struct {
	agent *AgentToolMessageItem
}

// RenderTool implements the [ToolRenderer] interface.
func (r *AgentToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if !opts.ToolCall.Finished && !opts.IsCanceled() && len(r.agent.nestedTools) == 0 {
		return pendingTool(sty, "Agent", opts.Anim, opts.Compact)
	}

	var params agent.AgentParams
	_ = json.Unmarshal([]byte(opts.ToolCall.Input), &params)

	prompt := params.Prompt
	prompt = strings.ReplaceAll(prompt, "\n", " ")

	header := toolHeader(sty, opts.Status, "Agent", cappedWidth, opts.Compact)
	if opts.Compact {
		return header
	}

	// Build the task tag and prompt.
	taskTag := sty.Tool.AgentTaskTag.Render("Task")
	taskTagWidth := lipgloss.Width(taskTag)

	// Calculate remaining width for prompt.
	remainingWidth := min(cappedWidth-taskTagWidth-3, maxTextWidth-taskTagWidth-3) // -3 for spacing

	promptText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(prompt)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			taskTag,
			" ",
			promptText,
		),
	)

	// Build tree with nested tool calls.
	childTools := tree.Root(header)

	for _, nestedTool := range r.agent.nestedTools {
		childView := nestedTool.Render(remainingWidth)
		childTools.Child(childView)
	}

	// Build panel view for parallel agent display
	if opts.PanelView != nil && len(r.agent.nestedTools) > 0 && params.Tasks != nil && len(params.Tasks) > 0 {
		panels := BuildLegacyAgentPanels(r.agent.toolCall.ID, params, r.agent.nestedTools)
		if agentPanelResolver != nil {
			if resolved := agentPanelResolver(r.agent.toolCall.ID, params, r.agent.nestedTools); len(resolved) > 0 {
				panels = resolved
			}
		}
		opts.PanelView.BuildAgentPanels(r.agent.toolCall.ID, panels)
	}

	// Build parts.
	var parts []string
	parts = append(parts, childTools.Enumerator(roundedEnumerator(2, taskTagWidth-5)).String())

	// Show animation if still running.
	if !opts.HasResult() && !opts.IsCanceled() {
		parts = append(parts, "", opts.Anim.Render())
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Add body content when completed.
	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, opts.ExpandedContent)
		return joinToolParts(result, body)
	}

	return result
}

// BuildAgentTaskPanels groups nested tool calls by task ID using UI-provided
// lookup callbacks. It is used by the TUI layer to build stable multi-agent
// panels even when a task emits multiple nested tools.
func BuildAgentTaskPanels(
	parentToolCallID string,
	params agent.AgentParams,
	nestedTools []ToolMessageItem,
	taskIDForToolCall func(toolCallID string) string,
	summaryForTask func(taskID string) string,
) []panel.AgentPanelData {
	if len(params.Tasks) == 0 {
		return nil
	}

	panels := make([]panel.AgentPanelData, 0, len(params.Tasks))
	grouped := make(map[string][]ToolMessageItem, len(nestedTools))
	for _, nestedTool := range nestedTools {
		if taskIDForToolCall == nil {
			continue
		}
		taskID := strings.TrimSpace(taskIDForToolCall(nestedTool.ToolCall().ID))
		if taskID == "" {
			continue
		}
		grouped[taskID] = append(grouped[taskID], nestedTool)
	}

	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}

		content := renderAgentTaskPanelContent(grouped[taskID])
		if content == "" && summaryForTask != nil {
			content = strings.TrimSpace(summaryForTask(taskID))
		}

		panels = append(panels, panel.AgentPanelData{
			ID:      taskID,
			Title:   taskID,
			Content: content,
		})
	}

	return panels
}

// BuildLegacyAgentPanels preserves the original order-based fallback for agent
// panels when no task-aware mapping is available.
func BuildLegacyAgentPanels(parentToolCallID string, params agent.AgentParams, nestedTools []ToolMessageItem) []panel.AgentPanelData {
	if len(params.Tasks) == 0 {
		return nil
	}

	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}

	nestedByID := make(map[string][]ToolMessageItem, len(nestedTools))
	unmapped := make([]ToolMessageItem, 0, len(nestedTools))
	for _, nestedTool := range nestedTools {
		expectedTaskID, ok := expectedAgentTaskIDForNestedTool(parentToolCallID, params, nestedTool.ToolCall().ID)
		if !ok {
			unmapped = append(unmapped, nestedTool)
			continue
		}
		nestedByID[expectedTaskID] = append(nestedByID[expectedTaskID], nestedTool)
	}

	panels := make([]panel.AgentPanelData, 0, len(params.Tasks))
	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}

		content := renderAgentTaskPanelContent(nestedByID[taskID])
		if content == "" && !hasDeps && len(unmapped) > 0 {
			content = unmapped[0].Render(80)
			unmapped = unmapped[1:]
		}

		panels = append(panels, panel.AgentPanelData{
			ID:      taskID,
			Title:   taskID,
			Content: content,
		})
	}

	return panels
}

func expectedAgentTaskIDForNestedTool(parentToolCallID string, params agent.AgentParams, nestedToolCallID string) (string, bool) {
	if parentToolCallID == "" || nestedToolCallID == "" || len(params.Tasks) == 0 {
		return "", false
	}

	hasDeps := false
	for _, task := range params.Tasks {
		if len(task.DependsOn) > 0 {
			hasDeps = true
			break
		}
	}

	for i, task := range params.Tasks {
		taskID := strings.TrimSpace(task.ID)
		if taskID == "" {
			taskID = fmt.Sprintf("task-%d", i+1)
		}

		expectedChildToolCallID := fmt.Sprintf("%s-%d", parentToolCallID, i+1)
		if hasDeps {
			expectedChildToolCallID = fmt.Sprintf("%s-%s", parentToolCallID, taskID)
		}
		if nestedToolCallID == expectedChildToolCallID {
			return taskID, true
		}
	}

	return "", false
}

func renderAgentTaskPanelContent(nestedTools []ToolMessageItem) string {
	if len(nestedTools) == 0 {
		return ""
	}

	parts := make([]string, 0, len(nestedTools))
	for _, nestedTool := range nestedTools {
		parts = append(parts, nestedTool.Render(80))
	}
	return strings.Join(parts, "\n\n")
}

// -----------------------------------------------------------------------------
// Agentic Fetch Tool
// -----------------------------------------------------------------------------

// AgenticFetchToolMessageItem is a message item that represents an agentic fetch tool call.
type AgenticFetchToolMessageItem struct {
	*baseToolMessageItem

	nestedTools []ToolMessageItem
}

var (
	_ ToolMessageItem     = (*AgenticFetchToolMessageItem)(nil)
	_ NestedToolContainer = (*AgenticFetchToolMessageItem)(nil)
)

// NewAgenticFetchToolMessageItem creates a new [AgenticFetchToolMessageItem].
func NewAgenticFetchToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) *AgenticFetchToolMessageItem {
	t := &AgenticFetchToolMessageItem{}
	t.baseToolMessageItem = newBaseToolMessageItem(sty, toolCall, result, &AgenticFetchToolRenderContext{fetch: t}, canceled)
	// For the agentic fetch tool we keep spinning until the tool call is finished.
	t.spinningFunc = func(state SpinningState) bool {
		return !state.HasResult() && !state.IsCanceled()
	}
	return t
}

// NestedTools returns the nested tools.
func (a *AgenticFetchToolMessageItem) NestedTools() []ToolMessageItem {
	return a.nestedTools
}

// SetNestedTools sets the nested tools.
func (a *AgenticFetchToolMessageItem) SetNestedTools(tools []ToolMessageItem) {
	a.nestedTools = tools
	a.clearCache()
}

// AddNestedTool adds a nested tool.
func (a *AgenticFetchToolMessageItem) AddNestedTool(tool ToolMessageItem) {
	// Mark nested tools as simple (compact) rendering.
	if s, ok := tool.(Compactable); ok {
		s.SetCompact(true)
	}
	a.nestedTools = append(a.nestedTools, tool)
	a.clearCache()
}

// AgenticFetchToolRenderContext renders agentic fetch tool messages.
type AgenticFetchToolRenderContext struct {
	fetch *AgenticFetchToolMessageItem
}

// agenticFetchParams matches tools.AgenticFetchParams.
type agenticFetchParams struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt"`
}

// RenderTool implements the [ToolRenderer] interface.
func (r *AgenticFetchToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if !opts.ToolCall.Finished && !opts.IsCanceled() && len(r.fetch.nestedTools) == 0 {
		return pendingTool(sty, "Agentic Fetch", opts.Anim, opts.Compact)
	}

	var params agenticFetchParams
	_ = json.Unmarshal([]byte(opts.ToolCall.Input), &params)

	prompt := params.Prompt
	prompt = strings.ReplaceAll(prompt, "\n", " ")

	// Build header with optional URL param.
	var toolParams []string
	if params.URL != "" {
		toolParams = append(toolParams, params.URL)
	}

	header := toolHeader(sty, opts.Status, "Agentic Fetch", cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}

	// Build the prompt tag.
	promptTag := sty.Tool.AgenticFetchPromptTag.Render("Prompt")
	promptTagWidth := lipgloss.Width(promptTag)

	// Calculate remaining width for prompt text.
	remainingWidth := min(cappedWidth-promptTagWidth-3, maxTextWidth-promptTagWidth-3) // -3 for spacing

	promptText := sty.Tool.AgentPrompt.Width(remainingWidth).Render(prompt)

	header = lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		lipgloss.JoinHorizontal(
			lipgloss.Left,
			promptTag,
			" ",
			promptText,
		),
	)

	// Build tree with nested tool calls.
	childTools := tree.Root(header)

	for _, nestedTool := range r.fetch.nestedTools {
		childView := nestedTool.Render(remainingWidth)
		childTools.Child(childView)
	}

	// Build parts.
	var parts []string
	parts = append(parts, childTools.Enumerator(roundedEnumerator(2, promptTagWidth-5)).String())

	// Show animation if still running.
	if !opts.HasResult() && !opts.IsCanceled() {
		parts = append(parts, "", opts.Anim.Render())
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Add body content when completed.
	if opts.HasResult() && opts.Result.Content != "" {
		body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, opts.ExpandedContent)
		return joinToolParts(result, body)
	}

	return result
}
