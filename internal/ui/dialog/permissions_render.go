package dialog

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/util/ext"
	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

// Calculate usable content width (dialog border + horizontal padding).
func (p *Permissions) calculateContentWidth(width int) int {
	t := p.com.Styles
	const dialogHorizontalPadding = 2
	return width - t.Dialog.View.GetHorizontalFrameSize() - dialogHorizontalPadding
}

// Draw implements [Dialog].
func (p *Permissions) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.com.Styles
	// Force fullscreen when window is too small.
	forceFullscreen := area.Dx() <= minWindowWidth || area.Dy() <= minWindowHeight

	// Calculate dialog dimensions based on fullscreen state and content type.
	var width, maxHeight int
	if forceFullscreen || (p.fullscreen && p.hasDiffView()) {
		// Use nearly full window for fullscreen.
		width = area.Dx()
		maxHeight = area.Dy()
	} else if p.hasDiffView() {
		// Wide for side-by-side diffs, capped for readability.
		width = min(int(float64(area.Dx())*diffSizeRatio), diffMaxWidth)
		maxHeight = int(float64(area.Dy()) * diffSizeRatio)
	} else {
		// Narrower for simple content like commands/URLs.
		width = min(int(float64(area.Dx())*simpleSizeRatio), simpleMaxWidth)
		maxHeight = int(float64(area.Dy()) * simpleHeightRatio)
	}

	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)

	contentWidth := p.calculateContentWidth(width)
	header := p.renderHeader(contentWidth)
	buttons := p.renderButtons(contentWidth)
	helpView := p.help.View(p)

	// Calculate available height for content.
	headerHeight := lipgloss.Height(header)
	buttonsHeight := lipgloss.Height(buttons)
	helpHeight := lipgloss.Height(helpView)
	frameHeight := dialogStyle.GetVerticalFrameSize() + layoutSpacingLines

	p.defaultDiffSplitMode = width >= splitModeMinWidth

	// Pre-render content to measure its actual height.
	renderedContent := p.renderContent(contentWidth)
	contentHeight := lipgloss.Height(renderedContent)

	// For non-diff views, shrink dialog to fit content if it's smaller than max.
	var availableHeight int
	if !p.hasDiffView() && !forceFullscreen {
		fixedHeight := headerHeight + buttonsHeight + helpHeight + frameHeight
		neededHeight := fixedHeight + contentHeight
		if neededHeight < maxHeight {
			availableHeight = contentHeight
		} else {
			availableHeight = maxHeight - fixedHeight
		}
		availableHeight = max(availableHeight, 3)
	} else {
		availableHeight = maxHeight - headerHeight - buttonsHeight - helpHeight - frameHeight
	}

	// Determine if scrollbar is needed.
	needsScrollbar := p.hasDiffView() || contentHeight > availableHeight
	viewportWidth := contentWidth
	if needsScrollbar {
		viewportWidth = contentWidth - 1 // Reserve space for scrollbar.
	}

	if p.viewport.Width() != viewportWidth {
		// Mark content as dirty if width has changed.
		p.viewportDirty = true
		renderedContent = p.renderContent(viewportWidth)
	}

	var content string
	var scrollbar string
	p.viewport.SetWidth(viewportWidth)
	p.viewport.SetHeight(availableHeight)
	if p.viewportDirty {
		p.viewport.SetContent(renderedContent)
		p.viewportWidth = p.viewport.Width()
		p.viewportDirty = false
	}
	content = p.viewport.View()
	if needsScrollbar {
		scrollbar = common.Scrollbar(t, availableHeight, p.viewport.TotalLineCount(), availableHeight, p.viewport.YOffset())
	}

	// Join content with scrollbar if present.
	if scrollbar != "" {
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	parts := []string{header}
	if content != "" {
		parts = append(parts, "", content)
	}
	parts = append(parts, "", buttons, "", helpView)

	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	DrawCenterCursor(scr, area, dialogStyle.Render(innerContent), nil)
	return nil
}

func (p *Permissions) renderHeader(contentWidth int) string {
	t := p.com.Styles

	title := common.DialogTitle(t, "Permission Required", contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor)
	title = t.Dialog.Title.Render(title)

	// Tool info.
	toolLine := p.renderToolName(contentWidth)
	pathLine := p.renderKeyValue("Path", fsext.PrettyPath(p.permission.Path), contentWidth)

	lines := []string{title, "", toolLine, pathLine}

	// Add tool-specific header info.
	switch p.permission.ToolName {
	case tools.BashToolName:
		if params, ok := p.permission.Params.(tools.BashPermissionsParams); ok {
			lines = append(lines, p.renderKeyValue("Desc", params.Description, contentWidth))
		}
	case tools.DownloadToolName:
		if params, ok := p.permission.Params.(tools.DownloadPermissionsParams); ok {
			lines = append(lines, p.renderKeyValue("URL", params.URL, contentWidth))
			lines = append(lines, p.renderKeyValue("File", fsext.PrettyPath(params.FilePath), contentWidth))
		}
	case tools.EditToolName, tools.WriteToolName, tools.MultiEditToolName, tools.ViewToolName:
		var filePath string
		switch params := p.permission.Params.(type) {
		case tools.EditPermissionsParams:
			filePath = params.FilePath
		case tools.WritePermissionsParams:
			filePath = params.FilePath
		case tools.MultiEditPermissionsParams:
			filePath = params.FilePath
		case tools.ViewPermissionsParams:
			filePath = params.FilePath
		}
		if filePath != "" {
			lines = append(lines, p.renderKeyValue("File", fsext.PrettyPath(filePath), contentWidth))
		}
	case tools.LSToolName:
		if params, ok := p.permission.Params.(tools.LSPermissionsParams); ok {
			lines = append(lines, p.renderKeyValue("Directory", fsext.PrettyPath(params.Path), contentWidth))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p *Permissions) renderKeyValue(key, value string, width int) string {
	t := p.com.Styles
	keyStyle := t.Dialog.Permissions.KeyText
	valueStyle := t.Dialog.Permissions.ValueText

	keyStr := keyStyle.Render(key)
	valueStr := valueStyle.Width(width - lipgloss.Width(keyStr) - 1).Render(" " + value)

	return lipgloss.JoinHorizontal(lipgloss.Left, keyStr, valueStr)
}

func (p *Permissions) renderToolName(width int) string {
	toolName := p.permission.ToolName

	// Check if this is an MCP tool (format: mcp_<mcpname>_<toolname>).
	if strings.HasPrefix(toolName, "mcp_") {
		parts := strings.SplitN(toolName, "_", 3)
		if len(parts) == 3 {
			mcpName := prettyName(parts[1])
			toolPart := prettyName(parts[2])
			toolName = fmt.Sprintf("%s %s %s", mcpName, styles.ArrowRightIcon, toolPart)
		}
	}

	return p.renderKeyValue("Tool", toolName, width)
}

// prettyName converts snake_case or kebab-case to Title Case.
func prettyName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return ext.Capitalize(name)
}

func (p *Permissions) renderContent(width int) string {
	switch p.permission.ToolName {
	case tools.BashToolName:
		return p.renderBashContent(width)
	case tools.EditToolName:
		return p.renderEditContent(width)
	case tools.WriteToolName:
		return p.renderWriteContent(width)
	case tools.MultiEditToolName:
		return p.renderMultiEditContent(width)
	case tools.DownloadToolName:
		return p.renderDownloadContent(width)
	case tools.FetchToolName:
		return p.renderFetchContent(width)
	case tools.AgenticFetchToolName:
		return p.renderAgenticFetchContent(width)
	case tools.ViewToolName:
		return p.renderViewContent(width)
	case tools.LSToolName:
		return p.renderLSContent(width)
	default:
		return p.renderDefaultContent(width)
	}
}

func (p *Permissions) renderButtons(contentWidth int) string {
	buttons := []common.ButtonOpts{
		{Text: "Allow", UnderlineIndex: 0, Selected: p.selectedOption == 0},
		{Text: "Allow for Session", UnderlineIndex: 10, Selected: p.selectedOption == 1},
		{Text: "Deny", UnderlineIndex: 0, Selected: p.selectedOption == 2},
	}

	content := common.ButtonGroup(p.com.Styles, buttons, "  ")

	// If buttons are too wide, stack them vertically.
	if lipgloss.Width(content) > contentWidth {
		content = common.ButtonGroup(p.com.Styles, buttons, "\n")
		return lipgloss.NewStyle().
			Width(contentWidth).
			Align(lipgloss.Center).
			Render(content)
	}

	return lipgloss.NewStyle().
		Width(contentWidth).
		Align(lipgloss.Right).
		Render(content)
}
