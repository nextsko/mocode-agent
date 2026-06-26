package model

import (
	"cmp"
	"context"
	"image"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/completions"
	"github.com/package-register/mocode/internal/ui/dialog"
)

func (m *UI) openSlashCompletions() tea.Cmd {
	noop := func() tea.Msg { return nil }
	prevHeight := m.textarea.Height()
	value := m.textarea.Value()
	word := m.textareaWord()
	needsSlash := value == "" || !strings.HasPrefix(word, "/")
	if needsSlash {
		m.textarea.InsertRune('/')
		if cmd := m.handleTextareaHeightChange(prevHeight); cmd != nil {
			m.completionsOpen = true
			m.completionsSlashMode = true
			m.completionsAtMode = false
			m.completionsQuery = ""
			m.completionsStartIndex = max(0, len(m.textarea.Value())-1)
			m.completionsPositionStart = m.completionsPosition()
			m.completions.SetSlashGroups(
				m.slashCompletionGroups(),
				m.com.Styles,
				max(m.layout.editor.Dx(), 10),
			)
			return cmd
		}
	}

	m.completionsOpen = true
	m.completionsSlashMode = true
	m.completionsAtMode = false
	if strings.HasPrefix(m.textareaWord(), "/") {
		m.completionsQuery = strings.TrimPrefix(m.textareaWord(), "/")
	} else {
		m.completionsQuery = ""
	}
	m.completionsStartIndex = max(0, len(m.textarea.Value())-len(m.textareaWord()))
	m.completionsPositionStart = m.completionsPosition()
	m.completions.SetSlashGroups(
		m.slashCompletionGroups(),
		m.com.Styles,
		max(m.layout.editor.Dx(), 10),
	)
	if m.completionsQuery != "" {
		m.completions.Filter(m.completionsQuery)
	}
	return noop
}

func (m *UI) closeCompletions() {
	m.completionsOpen = false
	m.completionsSlashMode = false
	m.completionsAtMode = false
	m.completionsQuery = ""
	m.completionsStartIndex = 0
	m.completions.Close()
}

func (m *UI) slashCompletionGroups() []completions.SlashGroup {
	hasSession := m.hasSession()
	hasTodos := hasSession && hasIncompleteTodos(m.session.Todos)
	hasQueue := m.promptQueue > 0
	cfg := m.com.Config()

	v := func(cmd, desc string, msg any) completions.SlashCompletionValue {
		return completions.SlashCompletionValue{Command: cmd, Desc: desc, Msg: msg}
	}

	// ── Agent ──────────────────────────────────────────────────────────────
	agentItems := []completions.SlashCompletionValue{
		v("/plan", "Switch to SK Plan mode", dialog.ActionSelectMode{ModeID: "plan"}),
		v("/code", "Switch to Code mode", dialog.ActionSelectMode{ModeID: "coder"}),
		v("/evo", "Enter self-evolution mode (red/purple)", dialog.ActionEvo{Enter: true}),
		v("/agents", "Switch agent mode", dialog.ActionOpenDialog{DialogID: dialog.ModesID}),
		v("/models", "Switch model", dialog.ActionOpenDialog{DialogID: dialog.ModelsID}),
	}

	// ── Session ────────────────────────────────────────────────────────────
	sessionItems := []completions.SlashCompletionValue{
		v("/new", "New session", dialog.ActionNewSession{}),
		v("/history", "Browse past sessions", dialog.ActionOpenDialog{DialogID: dialog.SessionsID}),
		v("/init", "Initialize project", dialog.ActionInitializeProject{}),
		v("/init_kng", "Initialize kng knowledge templates", dialog.ActionInitKnowledge{}),
	}
	if hasSession {
		sessionItems = append(
			sessionItems,
			v("/context", "Browse current context messages",
				dialog.ActionOpenDialog{DialogID: dialog.ContextID}),
			v("/rollback", "Rollback files to a session node",
				dialog.ActionOpenDialog{DialogID: dialog.RollbackID}),
			v("/summarize", "Summarize current session",
				dialog.ActionSummarize{SessionID: m.session.ID}),
			v("/export-md", "Export session as Markdown",
				dialog.ActionExportSession{SessionID: m.session.ID, Format: "markdown", Scope: "all"}),
			v("/export-html", "Export session as HTML",
				dialog.ActionExportSession{SessionID: m.session.ID, Format: "html", Scope: "all"}),
		)
	}
	if hasSession && m.width >= compactModeWidthBreakpoint {
		sessionItems = append(sessionItems,
			v("/sidebar", "Toggle sidebar", dialog.ActionToggleCompactMode{}))
	}
	if hasTodos || hasQueue {
		tasksLabel := "Toggle To-Dos"
		switch {
		case hasTodos && hasQueue:
			tasksLabel = "Toggle To-Dos / Queue"
		case hasQueue:
			tasksLabel = "Toggle Queue"
		}
		sessionItems = append(sessionItems,
			v("/tasks", tasksLabel, dialog.ActionTogglePills{}))
	}

	// ── Tools ─────────────────────────────────────────────────────────────
	toolItems := []completions.SlashCompletionValue{
		v("/mcps", "MCP servers", dialog.ActionOpenDialog{DialogID: dialog.MCPID}),
		v("/wechat", "Manage WeChat accounts", dialog.ActionOpenDialog{DialogID: dialog.WeChatManagerID}),
	}
	if os.Getenv("EDITOR") != "" {
		toolItems = append(toolItems,
			v("/editor", "Open external editor", dialog.ActionExternalEditor{}))
	}
	if hasSession && cfg != nil {
		if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
			if model := cfg.GetModelByType(agentCfg.Model); model != nil && model.SupportsImages {
				toolItems = append(toolItems,
					v("/file", "Open file picker", dialog.ActionOpenDialog{DialogID: dialog.FilePickerID}))
			}
		}
	}

	// ── Settings ──────────────────────────────────────────────────────────
	settingsItems := []completions.SlashCompletionValue{
		v("/approve", "Toggle auto-approve (Yolo)", dialog.ActionToggleYoloMode{}),
		v("/notifications", "Toggle notifications", dialog.ActionToggleNotifications{}),
		v("/theme", "Toggle transparent background", dialog.ActionToggleTransparentBackground{}),
	}
	if cfg != nil {
		if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
			if model := cfg.GetModelByType(agentCfg.Model); model != nil && model.CanReason {
				sel := cfg.Models[agentCfg.Model]
				if len(model.ReasoningLevels) == 0 {
					status := "Enable"
					if sel.Think {
						status = "Disable"
					}
					settingsItems = append(settingsItems,
						v("/think", status+" thinking mode", dialog.ActionToggleThinking{}))
				} else {
					settingsItems = append(settingsItems,
						v("/reasoning", "Select reasoning effort",
							dialog.ActionOpenDialog{DialogID: dialog.ReasoningID}))
				}
			}
		}
	}

	// ── Help ──────────────────────────────────────────────────────────────
	helpItems := []completions.SlashCompletionValue{
		v("/help", "Show help & key bindings", dialog.ActionOpenDialog{DialogID: dialog.HelpID}),
		v("/copy", "Copy the last assistant reply to clipboard", nil),
	}

	// ── Admin ─────────────────────────────────────────────────────────────
	adminItems := []completions.SlashCompletionValue{
		v("/admin", "Open Admin Panel", dialog.ActionOpenAdmin{}),
		v("/admin-start", "Start Admin Server", dialog.ActionStartAdmin{}),
		v("/admin-stop", "Stop Admin Server", dialog.ActionStopAdmin{}),
		v("/quit", "Quit", dialog.ActionQuit{}),
	}

	groups := []completions.SlashGroup{
		{Label: "Agent", Items: agentItems},
		{Label: "Session", Items: sessionItems},
		{Label: "Tools", Items: toolItems},
		{Label: "Settings", Items: settingsItems},
		{Label: "Help", Items: helpItems},
		{Label: "Admin", Items: adminItems},
	}

	// ── Custom commands ────────────────────────────────────────────────────
	if len(m.customCommands) > 0 {
		customItems := make([]completions.SlashCompletionValue, 0, len(m.customCommands))
		for _, cmd := range m.customCommands {
			customItems = append(customItems, completions.SlashCompletionValue{
				Command: slashLabelFromCommandID(cmd.ID),
				Desc:    slashDescFromContent(cmd.Content),
				Msg: dialog.ActionRunCustomCommand{
					Content:   cmd.Content,
					Arguments: cmd.Arguments,
				},
			})
		}
		groups = append(groups, completions.SlashGroup{Label: "Custom", Items: customItems})
	}

	return groups
}

func slashLabelFromCommandID(id string) string {
	parts := strings.Split(id, ":")
	name := parts[len(parts)-1]
	name = strings.ReplaceAll(name, "_", "-")
	if name == "" {
		name = id
	}
	return "/" + name
}

func slashDescFromContent(content string) string {
	for _, line := range strings.SplitN(content, "\n", 5) {
		line = strings.TrimSpace(strings.TrimLeft(line, "#> "))
		if line != "" {
			if len(line) > 60 {
				return line[:60] + "…"
			}
			return line
		}
	}
	return ""
}

func (m *UI) insertCompletionText(text string) bool {
	return m.insertCompletionTextWithGap(text, true)
}

func (m *UI) insertCompletionTextWithGap(text string, trailingGap bool) bool {
	value := m.textarea.Value()
	if m.completionsStartIndex > len(value) {
		return false
	}

	word := m.textareaWord()
	endIdx := min(m.completionsStartIndex+len(word), len(value))
	newValue := value[:m.completionsStartIndex] + text + value[endIdx:]
	m.textarea.SetValue(newValue)
	m.textarea.MoveToEnd()
	if trailingGap {
		m.textarea.InsertRune(' ')
	}
	return true
}

func (m *UI) insertFileCompletion(path string) tea.Cmd {
	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(path) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	fileCmd := func() tea.Msg {
		absPath, _ := filepath.Abs(path)

		if m.hasSession() {
			// Skip attachment if file was already read and hasn't been modified.
			lastRead := m.com.Workspace.FileTrackerLastReadTime(context.Background(), m.session.ID, absPath)
			if !lastRead.IsZero() {
				if info, err := os.Stat(path); err == nil && !info.ModTime().After(lastRead) {
					return nil
				}
			}
		} else if slices.Contains(m.sessionFileReads, absPath) {
			return nil
		}

		m.sessionFileReads = append(m.sessionFileReads, absPath)

		// Add file as attachment.
		content, err := os.ReadFile(path)
		if err != nil {
			// If it fails, let the LLM handle it later.
			return nil
		}

		return message.Attachment{
			FilePath: path,
			FileName: filepath.Base(path),
			MimeType: mimeOf(content),
			Content:  content,
		}
	}
	return tea.Batch(heightCmd, fileCmd)
}

func (m *UI) insertMCPResourceCompletion(item completions.ResourceCompletionValue) tea.Cmd {
	displayText := cmp.Or(item.Title, item.URI)

	prevHeight := m.textarea.Height()
	if !m.insertCompletionText(displayText) {
		return nil
	}
	heightCmd := m.handleTextareaHeightChange(prevHeight)

	resourceCmd := func() tea.Msg {
		contents, err := m.com.Workspace.ReadMCPResource(
			context.Background(),
			item.MCPName,
			item.URI,
		)
		if err != nil {
			slog.Warn("Failed to read MCP resource", "uri", item.URI, "error", err)
			return nil
		}
		if len(contents) == 0 {
			return nil
		}

		content := contents[0]
		var data []byte
		if content.Text != "" {
			data = []byte(content.Text)
		} else if len(content.Blob) > 0 {
			data = content.Blob
		}
		if len(data) == 0 {
			return nil
		}

		mimeType := item.MIMEType
		if mimeType == "" && content.MIMEType != "" {
			mimeType = content.MIMEType
		}
		if mimeType == "" {
			mimeType = "text/plain"
		}

		return message.Attachment{
			FilePath: item.URI,
			FileName: displayText,
			MimeType: mimeType,
			Content:  data,
		}
	}
	return tea.Batch(heightCmd, resourceCmd)
}

func (m *UI) completionsPosition() image.Point {
	cur := m.textarea.Cursor()
	if cur == nil {
		return image.Point{
			X: m.layout.editor.Min.X,
			Y: m.layout.editor.Min.Y,
		}
	}
	return image.Point{
		X: cur.X + m.layout.editor.Min.X,
		Y: m.layout.editor.Min.Y + cur.Y,
	}
}

func (m *UI) textareaWord() string {
	return m.textarea.Word()
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func mimeOf(content []byte) string {
	mimeBufferSize := min(512, len(content))
	return http.DetectContentType(content[:mimeBufferSize])
}

func (m *UI) randomizePlaceholders() {
	m.workingPlaceholder = workingPlaceholders[randomIntn(len(workingPlaceholders))]
	m.readyPlaceholder = readyPlaceholders[randomIntn(len(readyPlaceholders))]
}

func (m *UI) isAgentBusy() bool {
	return m.com.Workspace.AgentIsReady() &&
		m.com.Workspace.AgentIsBusy()
}

func (m *UI) breathingCmd() tea.Cmd {
	return tea.Tick(800*time.Millisecond, func(time.Time) tea.Msg {
		return breathTickMsg{}
	})
}

func (m *UI) hasSession() bool {
	return m.session != nil && m.session.ID != ""
}
