package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/package-register/mocode/internal/core/agent"
	"github.com/package-register/mocode/internal/core/agent/tools"
	"github.com/package-register/mocode/internal/util/diff"
	"github.com/package-register/mocode/internal/util/fsext"
)

func (t *baseToolMessageItem) formatToolForCopy() string {
	var parts []string

	toolName := prettifyToolName(t.toolCall.Name)
	parts = append(parts, fmt.Sprintf("## %s Tool Call", toolName))

	if t.toolCall.Input != "" {
		params := t.formatParametersForCopy()
		if params != "" {
			parts = append(parts, "### Parameters:")
			parts = append(parts, params)
		}
	}

	if t.result != nil && t.result.ToolCallID != "" {
		if t.result.IsError {
			parts = append(parts, "### Error:")
			parts = append(parts, t.result.Content)
		} else {
			parts = append(parts, "### Result:")
			content := t.formatResultForCopy()
			if content != "" {
				parts = append(parts, content)
			}
		}
	} else if t.status == ToolStatusCanceled {
		parts = append(parts, "### Status:")
		parts = append(parts, "Cancelled")
	} else {
		parts = append(parts, "### Status:")
		parts = append(parts, "Pending...")
	}

	return strings.Join(parts, "\n\n")
}

func (t *baseToolMessageItem) formatParametersForCopy() string {
	switch t.toolCall.Name {
	case tools.BashToolName:
		var params tools.BashParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			cmd := strings.ReplaceAll(params.Command, "\n", " ")
			cmd = strings.ReplaceAll(cmd, "\t", "    ")
			return fmt.Sprintf("**Command:** %s", cmd)
		}
	case tools.ViewToolName:
		var params tools.ViewParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath)))
			if params.Limit > 0 {
				parts = append(parts, fmt.Sprintf("**Limit:** %d", params.Limit))
			}
			if params.Offset > 0 {
				parts = append(parts, fmt.Sprintf("**Offset:** %d", params.Offset))
			}
			return strings.Join(parts, "\n")
		}
	case tools.EditToolName:
		var params tools.EditParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			return fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath))
		}
	case tools.MultiEditToolName:
		var params tools.MultiEditParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath)))
			parts = append(parts, fmt.Sprintf("**Edits:** %d", len(params.Edits)))
			return strings.Join(parts, "\n")
		}
	case tools.WriteToolName:
		var params tools.WriteParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			return fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath))
		}
	case tools.FetchToolName:
		var params tools.FetchParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**URL:** %s", params.URL))
			if params.Format != "" {
				parts = append(parts, fmt.Sprintf("**Format:** %s", params.Format))
			}
			if params.Timeout > 0 {
				parts = append(parts, fmt.Sprintf("**Timeout:** %ds", params.Timeout))
			}
			return strings.Join(parts, "\n")
		}
	case tools.AgenticFetchToolName:
		var params tools.AgenticFetchParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			if params.URL != "" {
				parts = append(parts, fmt.Sprintf("**URL:** %s", params.URL))
			}
			if params.Prompt != "" {
				parts = append(parts, fmt.Sprintf("**Prompt:** %s", params.Prompt))
			}
			return strings.Join(parts, "\n")
		}
	case tools.WebFetchToolName:
		var params tools.WebFetchParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			return fmt.Sprintf("**URL:** %s", params.URL)
		}
	case tools.GrepToolName:
		var params tools.GrepParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**Pattern:** %s", params.Pattern))
			if params.Path != "" {
				parts = append(parts, fmt.Sprintf("**Path:** %s", params.Path))
			}
			if params.Include != "" {
				parts = append(parts, fmt.Sprintf("**Include:** %s", params.Include))
			}
			if params.LiteralText {
				parts = append(parts, "**Literal:** true")
			}
			return strings.Join(parts, "\n")
		}
	case tools.GlobToolName:
		var params tools.GlobParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**Pattern:** %s", params.Pattern))
			if params.Path != "" {
				parts = append(parts, fmt.Sprintf("**Path:** %s", params.Path))
			}
			return strings.Join(parts, "\n")
		}
	case tools.LSToolName:
		var params tools.LSParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			path := params.Path
			if path == "" {
				path = "."
			}
			return fmt.Sprintf("**Path:** %s", fsext.PrettyPath(path))
		}
	case tools.DownloadToolName:
		var params tools.DownloadParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**URL:** %s", params.URL))
			parts = append(parts, fmt.Sprintf("**File Path:** %s", fsext.PrettyPath(params.FilePath)))
			if params.Timeout > 0 {
				parts = append(parts, fmt.Sprintf("**Timeout:** %s", (time.Duration(params.Timeout)*time.Second).String()))
			}
			return strings.Join(parts, "\n")
		}
	case tools.SourcegraphToolName:
		var params tools.SourcegraphParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			var parts []string
			parts = append(parts, fmt.Sprintf("**Query:** %s", params.Query))
			if params.Count > 0 {
				parts = append(parts, fmt.Sprintf("**Count:** %d", params.Count))
			}
			if params.ContextWindow > 0 {
				parts = append(parts, fmt.Sprintf("**Context:** %d", params.ContextWindow))
			}
			return strings.Join(parts, "\n")
		}
	case tools.DiagnosticsToolName:
		return "**Project:** diagnostics"
	case agent.AgentToolName:
		var params agent.AgentParams
		if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
			return fmt.Sprintf("**Task:**\n%s", params.Prompt)
		}
	}

	var params map[string]any
	if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
		var parts []string
		for key, value := range params {
			displayKey := strings.ReplaceAll(key, "_", " ")
			if len(displayKey) > 0 {
				displayKey = strings.ToUpper(displayKey[:1]) + displayKey[1:]
			}
			parts = append(parts, fmt.Sprintf("**%s:** %v", displayKey, value))
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

func (t *baseToolMessageItem) formatResultForCopy() string {
	if t.result == nil {
		return ""
	}

	if t.result.Data != "" {
		if strings.HasPrefix(t.result.MIMEType, "image/") {
			return fmt.Sprintf("[Image: %s]", t.result.MIMEType)
		}
		return fmt.Sprintf("[Media: %s]", t.result.MIMEType)
	}

	switch t.toolCall.Name {
	case tools.BashToolName:
		return t.formatBashResultForCopy()
	case tools.ViewToolName:
		return t.formatViewResultForCopy()
	case tools.EditToolName:
		return t.formatEditResultForCopy()
	case tools.ReadFilesToolName:
		if t.result != nil {
			return t.result.Content
		}
		return ""
	case tools.MultiEditToolName:
		return t.formatMultiEditResultForCopy()
	case tools.WriteToolName:
		return t.formatWriteResultForCopy()
	case tools.FetchToolName:
		return t.formatFetchResultForCopy()
	case tools.AgenticFetchToolName:
		return t.formatAgenticFetchResultForCopy()
	case tools.WebFetchToolName:
		return t.formatWebFetchResultForCopy()
	case agent.AgentToolName:
		return t.formatAgentResultForCopy()
	case tools.DownloadToolName, tools.GrepToolName, tools.GlobToolName, tools.LSToolName, tools.SourcegraphToolName, tools.DiagnosticsToolName, tools.TodosToolName:
		return fmt.Sprintf("```\n%s\n```", t.result.Content)
	default:
		return t.result.Content
	}
}

func (t *baseToolMessageItem) formatBashResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var meta tools.BashResponseMetadata
	if t.result.Metadata != "" {
		if err := json.Unmarshal([]byte(t.result.Metadata), &meta); err != nil {
			return fmt.Sprintf("```bash\n%s\n```", t.result.Content)
		}
	}

	output := meta.Output
	if output == "" && t.result.Content != tools.BashNoOutput {
		output = t.result.Content
	}

	if output == "" {
		return ""
	}

	return fmt.Sprintf("```bash\n%s\n```", output)
}

func (t *baseToolMessageItem) formatViewResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var meta tools.ViewResponseMetadata
	if t.result.Metadata != "" {
		if err := json.Unmarshal([]byte(t.result.Metadata), &meta); err != nil {
			return t.result.Content
		}
	}

	if meta.Content == "" {
		return t.result.Content
	}

	lang := ""
	if meta.FilePath != "" {
		ext := strings.ToLower(filepath.Ext(meta.FilePath))
		switch ext {
		case ".go":
			lang = "go"
		case ".js", ".mjs":
			lang = "javascript"
		case ".ts":
			lang = "typescript"
		case ".py":
			lang = "python"
		case ".rs":
			lang = "rust"
		case ".java":
			lang = "java"
		case ".c":
			lang = "c"
		case ".cpp", ".cc", ".cxx":
			lang = "cpp"
		case ".sh", ".bash":
			lang = "bash"
		case ".json":
			lang = "json"
		case ".yaml", ".yml":
			lang = "yaml"
		case ".xml":
			lang = "xml"
		case ".html":
			lang = "html"
		case ".css":
			lang = "css"
		case ".md":
			lang = "markdown"
		}
	}

	var result strings.Builder
	if lang != "" {
		fmt.Fprintf(&result, "```%s\n", lang)
	} else {
		result.WriteString("```\n")
	}
	result.WriteString(meta.Content)
	result.WriteString("\n```")

	return result.String()
}

func (t *baseToolMessageItem) formatEditResultForCopy() string {
	if t.result == nil || t.result.Metadata == "" {
		if t.result != nil {
			return t.result.Content
		}
		return ""
	}

	var meta tools.EditResponseMetadata
	if json.Unmarshal([]byte(t.result.Metadata), &meta) != nil {
		return t.result.Content
	}

	var params tools.EditParams
	if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
		return t.result.Content
	}

	var result strings.Builder

	if meta.OldContent != "" || meta.NewContent != "" {
		fileName := params.FilePath
		if fileName != "" {
			fileName = fsext.PrettyPath(fileName)
		}
		diffContent, additions, removals := diff.GenerateDiff(meta.OldContent, meta.NewContent, fileName)

		fmt.Fprintf(&result, "Changes: +%d -%d\n", additions, removals)
		result.WriteString("```diff\n")
		result.WriteString(diffContent)
		result.WriteString("\n```")
	}

	return result.String()
}

func (t *baseToolMessageItem) formatMultiEditResultForCopy() string {
	if t.result == nil || t.result.Metadata == "" {
		if t.result != nil {
			return t.result.Content
		}
		return ""
	}

	var meta tools.MultiEditResponseMetadata
	if json.Unmarshal([]byte(t.result.Metadata), &meta) != nil {
		return t.result.Content
	}

	var params tools.MultiEditParams
	if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
		return t.result.Content
	}

	var result strings.Builder
	if meta.OldContent != "" || meta.NewContent != "" {
		fileName := params.FilePath
		if fileName != "" {
			fileName = fsext.PrettyPath(fileName)
		}
		diffContent, additions, removals := diff.GenerateDiff(meta.OldContent, meta.NewContent, fileName)

		fmt.Fprintf(&result, "Changes: +%d -%d\n", additions, removals)
		result.WriteString("```diff\n")
		result.WriteString(diffContent)
		result.WriteString("\n```")
	}

	return result.String()
}

func (t *baseToolMessageItem) formatWriteResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.WriteParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	lang := ""
	if params.FilePath != "" {
		ext := strings.ToLower(filepath.Ext(params.FilePath))
		switch ext {
		case ".go":
			lang = "go"
		case ".js", ".mjs":
			lang = "javascript"
		case ".ts":
			lang = "typescript"
		case ".py":
			lang = "python"
		case ".rs":
			lang = "rust"
		case ".java":
			lang = "java"
		case ".c":
			lang = "c"
		case ".cpp", ".cc", ".cxx":
			lang = "cpp"
		case ".sh", ".bash":
			lang = "bash"
		case ".json":
			lang = "json"
		case ".yaml", ".yml":
			lang = "yaml"
		case ".xml":
			lang = "xml"
		case ".html":
			lang = "html"
		case ".css":
			lang = "css"
		case ".md":
			lang = "markdown"
		}
	}

	var result strings.Builder
	fmt.Fprintf(&result, "File: %s\n", fsext.PrettyPath(params.FilePath))
	if lang != "" {
		fmt.Fprintf(&result, "```%s\n", lang)
	} else {
		result.WriteString("```\n")
	}
	result.WriteString(params.Content)
	result.WriteString("\n```")

	return result.String()
}

func (t *baseToolMessageItem) formatFetchResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.FetchParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	var result strings.Builder
	if params.URL != "" {
		fmt.Fprintf(&result, "URL: %s\n", params.URL)
	}
	if params.Format != "" {
		fmt.Fprintf(&result, "Format: %s\n", params.Format)
	}
	if params.Timeout > 0 {
		fmt.Fprintf(&result, "Timeout: %ds\n", params.Timeout)
	}
	result.WriteString("\n")

	result.WriteString(t.result.Content)

	return result.String()
}

func (t *baseToolMessageItem) formatAgenticFetchResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.AgenticFetchParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	var result strings.Builder
	if params.URL != "" {
		fmt.Fprintf(&result, "URL: %s\n", params.URL)
	}
	if params.Prompt != "" {
		fmt.Fprintf(&result, "Prompt: %s\n\n", params.Prompt)
	}

	result.WriteString("```markdown\n")
	result.WriteString(t.result.Content)
	result.WriteString("\n```")

	return result.String()
}

func (t *baseToolMessageItem) formatWebFetchResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.WebFetchParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	var result strings.Builder
	fmt.Fprintf(&result, "URL: %s\n\n", params.URL)
	result.WriteString("```markdown\n")
	result.WriteString(t.result.Content)
	result.WriteString("\n```")

	return result.String()
}

func (t *baseToolMessageItem) formatAgentResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var result strings.Builder

	if t.result.Content != "" {
		fmt.Fprintf(&result, "```markdown\n%s\n```", t.result.Content)
	}

	return result.String()
}
