package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/package-register/mocode/internal/agent/tools/mcp"
	"github.com/package-register/mocode/internal/commands"
	"github.com/package-register/mocode/internal/fsext"
	"github.com/package-register/mocode/internal/infra"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/skills"
	"github.com/package-register/mocode/internal/ui/common"
	"github.com/package-register/mocode/internal/ui/completions"
)

func (m *UI) openAtCompletions(depth, limit int) tea.Cmd {
	return func() tea.Msg {
		return completions.AtCompletionItemsLoadedMsg{Items: m.atCompletionItems(depth, limit)}
	}
}

func (m *UI) atCompletionItems(depth, limit int) []completions.AtCompletionValue {
	items := []completions.AtCompletionValue{
		atCategory("@mcp:", "MCP servers, prompts, and resources", "mcp"),
		atCategory("@skill:", "Agent Skills loaded from builtin and configured paths", "skill"),
		atCategory("@file:", "Files from the current project", "file"),
		atCategory("@dir:", "Directories from the current project", "dir"),
		atCategory("@workflow:", "Windsurf workflows and custom command markdown", "workflow"),
	}

	items = append(items, m.atMCPItems()...)
	items = append(items, m.atSkillItems()...)
	items = append(items, atFileAndDirItems(depth, limit)...)
	items = append(items, m.atWorkflowItems()...)
	return items
}

func atCategory(token, desc, category string) completions.AtCompletionValue {
	return completions.AtCompletionValue{
		Kind:       completions.AtCompletionCategory,
		Token:      token,
		Desc:       desc,
		Category:   category,
		IsCategory: true,
	}
}

func (m *UI) atMCPItems() []completions.AtCompletionValue {
	var items []completions.AtCompletionValue
	for name, cfg := range m.com.Config().MCP {
		if cfg.Disabled {
			continue
		}
		items = append(items, completions.AtCompletionValue{
			Kind:    completions.AtCompletionMCP,
			Token:   "@mcp:" + name,
			Desc:    "Use MCP server " + name,
			Content: "Use the MCP server " + name + " when it is relevant to this task.",
		})
	}
	for mcpName, resources := range mcp.Resources() {
		for _, r := range resources {
			display := firstNonEmpty(r.Name, r.URI)
			items = append(items, completions.AtCompletionValue{
				Kind:  completions.AtCompletionMCP,
				Token: "@mcp:" + mcpName + "/" + display,
				Desc:  "Resource " + r.URI,
				Resource: completions.ResourceCompletionValue{
					MCPName:  mcpName,
					URI:      r.URI,
					Title:    r.Name,
					MIMEType: r.MIMEType,
				},
			})
		}
	}
	for _, prompt := range m.mcpPromptItems() {
		items = append(items, prompt)
	}
	return items
}

func (m *UI) mcpPromptItems() []completions.AtCompletionValue {
	prompts, err := commands.LoadMCPPrompts()
	if err != nil {
		return nil
	}
	items := make([]completions.AtCompletionValue, 0, len(prompts))
	for _, p := range prompts {
		desc := firstNonEmpty(p.Description, p.Title, "MCP prompt")
		items = append(items, completions.AtCompletionValue{
			Kind:    completions.AtCompletionMCP,
			Token:   "@mcp:" + p.ID,
			Desc:    desc,
			Content: fmt.Sprintf("MCP prompt %s from server %s. %s", p.PromptID, p.ClientID, desc),
		})
	}
	return items
}

func (m *UI) atSkillItems() []completions.AtCompletionValue {
	cfg := m.com.Config()
	all := append([]*skills.Skill(nil), skills.DiscoverBuiltin()...)
	if cfg.Options != nil {
		paths := make([]string, 0, len(cfg.Options.SkillsPaths))
		for _, p := range cfg.Options.SkillsPaths {
			paths = append(paths, infra.Long(p))
		}
		all = append(all, skills.Discover(paths)...)
	}
	all = skills.Deduplicate(all)
	if cfg.Options != nil {
		all = skills.Filter(all, cfg.Options.DisabledSkills)
	}

	items := make([]completions.AtCompletionValue, 0, len(all))
	for _, s := range all {
		content := s.Instructions
		if !s.Builtin && s.SkillFilePath != "" {
			if b, err := os.ReadFile(s.SkillFilePath); err == nil {
				content = string(b)
			}
		}
		items = append(items, completions.AtCompletionValue{
			Kind:    completions.AtCompletionSkill,
			Token:   "@skill:" + s.Name,
			Desc:    s.Description,
			Path:    s.SkillFilePath,
			Content: content,
		})
	}
	return items
}

func atFileAndDirItems(depth, limit int) []completions.AtCompletionValue {
	paths, _, _ := fsext.ListDirectory(".", nil, depth, limit)
	slices.Sort(paths)
	items := make([]completions.AtCompletionValue, 0, len(paths))
	for _, p := range paths {
		path := normalizeContextPath(p)
		if path == "" {
			continue
		}
		if strings.HasSuffix(path, "/") {
			items = append(items, completions.AtCompletionValue{
				Kind:  completions.AtCompletionDir,
				Token: "@dir:" + strings.TrimSuffix(path, "/"),
				Desc:  "Directory listing",
				Path:  strings.TrimSuffix(path, "/"),
			})
			continue
		}
		items = append(items, completions.AtCompletionValue{
			Kind:  completions.AtCompletionFile,
			Token: "@file:" + path,
			Desc:  "Attach file",
			Path:  path,
		})
	}
	return items
}

func (m *UI) atWorkflowItems() []completions.AtCompletionValue {
	var items []completions.AtCompletionValue
	items = append(items, windsurfWorkflowItems()...)

	if cmds, err := commands.LoadCustomCommands(m.com.Config()); err == nil {
		for _, cmd := range cmds {
			items = append(items, completions.AtCompletionValue{
				Kind:    completions.AtCompletionWorkflow,
				Token:   "@workflow:" + cmd.ID,
				Desc:    firstNonEmpty(slashDescFromContent(cmd.Content), "Custom command"),
				Content: cmd.Content,
			})
		}
	}
	return items
}

func windsurfWorkflowItems() []completions.AtCompletionValue {
	dirs := []string{
		filepath.Join(".windsurf", "workflows"),
		filepath.Join(infra.Dir(), ".windsurf", "workflows"),
		filepath.Join(infra.Config(), "windsurf", "workflows"),
		filepath.Join(infra.Config(), "Windsurf", "workflows"),
	}
	seenPaths := make(map[string]bool)
	seenTokens := make(map[string]bool)
	var items []completions.AtCompletionValue
	for _, dir := range dirs {
		base := dir
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !isWorkflowFile(d.Name()) {
				return nil
			}
			rel, _ := filepath.Rel(base, path)
			name := strings.TrimSuffix(normalizeContextPath(rel), filepath.Ext(rel))
			token := "@workflow:windsurf/" + name
			if name == "" || seenPaths[path] || seenTokens[token] {
				return nil
			}
			seenPaths[path] = true
			seenTokens[token] = true
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			items = append(items, completions.AtCompletionValue{
				Kind:    completions.AtCompletionWorkflow,
				Token:   token,
				Desc:    "Windsurf workflow",
				Path:    path,
				Content: string(content),
			})
			return nil
		})
	}
	return items
}

func isWorkflowFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".txt", ".yaml", ".yml", ".json", ".toml":
		return true
	default:
		return false
	}
}

func (m *UI) insertAtCompletion(item completions.AtCompletionValue) tea.Cmd {
	if item.IsCategory {
		prevHeight := m.textarea.Height()
		if !m.insertCompletionTextWithGap(item.Token, false) {
			return nil
		}
		m.completionsOpen = true
		depth, limit := m.com.Config().Options.TUI.Completions.Limits()
		m.completions.SetAtItems(m.atCompletionItems(depth, limit), m.com.Styles, m.layout.editor.Dx())
		m.completions.Filter(strings.TrimPrefix(item.Token, "@"))
		return m.handleTextareaHeightChange(prevHeight)
	}

	prevHeight := m.textarea.Height()
	if !m.insertCompletionTextWithGap(item.Token, true) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)
	attachCmd := m.atAttachmentCmd(item)
	return tea.Batch(heightCmd, attachCmd)
}

func (m *UI) atAttachmentCmd(item completions.AtCompletionValue) tea.Cmd {
	return func() tea.Msg {
		switch item.Kind {
		case completions.AtCompletionFile:
			content, err := os.ReadFile(item.Path)
			if err != nil || int64(len(content)) > common.MaxAttachmentSize {
				return nil
			}
			return message.Attachment{FilePath: item.Path, FileName: filepath.Base(item.Path), MimeType: mimeOf(content), Content: content}

		case completions.AtCompletionDir:
			paths, truncated, err := fsext.ListDirectory(item.Path, nil, 1, 200)
			if err != nil {
				return nil
			}
			body := "Directory: " + item.Path + "\n" + strings.Join(paths, "\n")
			if truncated {
				body += "\n...truncated"
			}
			return textAttachment(item.Path, filepath.Base(item.Path), body)

		case completions.AtCompletionMCP:
			if item.Resource.URI != "" {
				contents, err := m.com.Workspace.ReadMCPResource(context.Background(), item.Resource.MCPName, item.Resource.URI)
				if err != nil || len(contents) == 0 {
					return nil
				}
				content := contents[0]
				data := []byte(content.Text)
				if len(data) == 0 && len(content.Blob) > 0 {
					data = content.Blob
				}
				if len(data) == 0 {
					return nil
				}
				mimeType := firstNonEmpty(item.Resource.MIMEType, content.MIMEType, "text/plain")
				return message.Attachment{FilePath: item.Resource.URI, FileName: firstNonEmpty(item.Resource.Title, item.Resource.URI), MimeType: mimeType, Content: data}
			}
			return textAttachment(item.Token, item.Token, item.Content)

		case completions.AtCompletionSkill, completions.AtCompletionWorkflow:
			if item.Content == "" {
				return nil
			}
			return textAttachment(firstNonEmpty(item.Path, item.Token), item.Token, item.Content)
		}
		return nil
	}
}

func textAttachment(path, name, content string) message.Attachment {
	return message.Attachment{FilePath: path, FileName: name, MimeType: "text/plain; charset=utf-8", Content: []byte(content)}
}

func normalizeContextPath(path string) string {
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	return strings.TrimSpace(path)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
