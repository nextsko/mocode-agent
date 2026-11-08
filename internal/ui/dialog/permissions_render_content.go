package dialog

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/tools"
	"github.com/nextsko/mocode-agent/internal/ui/common"
	"github.com/nextsko/mocode-agent/internal/ui/styles"
	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

func (p *Permissions) renderBashContent(width int) string {
	params, ok := p.permission.Params.(tools.BashPermissionsParams)
	if !ok {
		return ""
	}

	return p.renderContentPanel(params.Command, width)
}

func (p *Permissions) renderEditContent(contentWidth int) string {
	params, ok := p.permission.Params.(tools.EditPermissionsParams)
	if !ok {
		return ""
	}
	return p.renderDiff(params.FilePath, params.OldContent, params.NewContent, contentWidth)
}

func (p *Permissions) renderWriteContent(contentWidth int) string {
	params, ok := p.permission.Params.(tools.WritePermissionsParams)
	if !ok {
		return ""
	}
	return p.renderDiff(params.FilePath, params.OldContent, params.NewContent, contentWidth)
}

func (p *Permissions) renderMultiEditContent(contentWidth int) string {
	params, ok := p.permission.Params.(tools.MultiEditPermissionsParams)
	if !ok {
		return ""
	}
	return p.renderDiff(params.FilePath, params.OldContent, params.NewContent, contentWidth)
}

func (p *Permissions) renderDiff(filePath, oldContent, newContent string, contentWidth int) string {
	if !p.viewportDirty {
		if p.isSplitMode() {
			return p.splitDiffContent
		}
		return p.unifiedDiffContent
	}

	isSplitMode := p.isSplitMode()
	formatter := common.DiffFormatter(p.com.Styles).
		Before(fsext.PrettyPath(filePath), oldContent).
		After(fsext.PrettyPath(filePath), newContent).
		XOffset(p.diffXOffset).
		Width(contentWidth)

	var result string
	if isSplitMode {
		formatter = formatter.Split()
		p.splitDiffContent = formatter.String()
		result = p.splitDiffContent
	} else {
		formatter = formatter.Unified()
		p.unifiedDiffContent = formatter.String()
		result = p.unifiedDiffContent
	}

	return result
}

func (p *Permissions) renderDownloadContent(width int) string {
	params, ok := p.permission.Params.(tools.DownloadPermissionsParams)
	if !ok {
		return ""
	}

	content := fmt.Sprintf("URL: %s\nFile: %s", params.URL, fsext.PrettyPath(params.FilePath))
	if params.Timeout > 0 {
		content += fmt.Sprintf("\nTimeout: %ds", params.Timeout)
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderFetchContent(width int) string {
	params, ok := p.permission.Params.(tools.FetchPermissionsParams)
	if !ok {
		return ""
	}

	return p.renderContentPanel(params.URL, width)
}

func (p *Permissions) renderAgenticFetchContent(width int) string {
	params, ok := p.permission.Params.(tools.AgenticFetchPermissionsParams)
	if !ok {
		return ""
	}

	var content string
	if params.URL != "" {
		content = fmt.Sprintf("URL: %s\n\nPrompt: %s", params.URL, params.Prompt)
	} else {
		content = fmt.Sprintf("Prompt: %s", params.Prompt)
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderViewContent(width int) string {
	params, ok := p.permission.Params.(tools.ViewPermissionsParams)
	if !ok {
		return ""
	}

	content := fmt.Sprintf("File: %s", fsext.PrettyPath(params.FilePath))
	if params.Offset > 0 {
		content += fmt.Sprintf("\nStarting from line: %d", params.Offset+1)
	}
	if params.Limit > 0 && params.Limit != 2000 {
		content += fmt.Sprintf("\nLines to read: %d", params.Limit)
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderLSContent(width int) string {
	params, ok := p.permission.Params.(tools.LSPermissionsParams)
	if !ok {
		return ""
	}

	content := fmt.Sprintf("Directory: %s", fsext.PrettyPath(params.Path))
	if len(params.Ignore) > 0 {
		content += fmt.Sprintf("\nIgnore patterns: %s", strings.Join(params.Ignore, ", "))
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderDefaultContent(width int) string {
	t := p.com.Styles
	var content string
	// do not add the description for mcp tools
	if !strings.HasPrefix(p.permission.ToolName, "mcp_") {
		content = p.permission.Description
	}

	// Pretty-print JSON params if available.
	if rendered := renderPermissionParams(t, p.permission.Params); rendered != "" {
		if content != "" {
			content += "\n\n"
		}
		content += rendered
	}

	if content == "" {
		return ""
	}

	return p.renderContentPanel(strings.TrimSpace(content), width)
}

// renderPermissionParams renders tool-call parameters, pretty-printing and
// highlighting them when they are JSON.
func renderPermissionParams(t *styles.Styles, params any) string {
	if params == nil {
		return ""
	}
	paramStr := fmt.Sprintf("%v", params)
	if str, ok := params.(string); ok {
		paramStr = str
	}

	var parsed any
	if err := json.Unmarshal([]byte(paramStr), &parsed); err != nil {
		return paramStr
	}
	b, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return paramStr
	}
	jsonContent := string(b)
	if highlighted, err := common.SyntaxHighlight(t, jsonContent, "params.json", t.Dialog.Permissions.ParamsBg); err == nil {
		jsonContent = highlighted
	}
	return jsonContent
}

// renderContentPanel renders content in a panel with the full width.
func (p *Permissions) renderContentPanel(content string, width int) string {
	panelStyle := p.com.Styles.Dialog.ContentPanel
	return panelStyle.Width(width).Render(content)
}
