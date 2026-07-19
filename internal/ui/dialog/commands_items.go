package dialog

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/package-register/mocode/internal/core/config"
	"github.com/package-register/mocode/internal/ui/list"
	"github.com/package-register/mocode/internal/ui/slash"
)

func (c *Commands) nextCommandType() CommandType {
	switch c.selected {
	case SystemCommands:
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		fallthrough
	case UserCommands:
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		fallthrough
	case MCPPrompts:
		return SystemCommands
	default:
		return SystemCommands
	}
}

func (c *Commands) previousCommandType() CommandType {
	switch c.selected {
	case SystemCommands:
		if len(c.mcpPrompts) > 0 {
			return MCPPrompts
		}
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		return SystemCommands
	case UserCommands:
		return SystemCommands
	case MCPPrompts:
		if len(c.customCommands) > 0 {
			return UserCommands
		}
		return SystemCommands
	default:
		return SystemCommands
	}
}

func (c *Commands) setCommandItems(commandType CommandType) {
	c.selected = commandType

	if commandType == SystemCommands {
		// System commands use the hierarchical defaultCommands() directly
		// to preserve parent/child grouping structure.
		items := c.defaultCommands()
		filterableItems := make([]list.FilterableItem, len(items))
		for i, item := range items {
			filterableItems[i] = item
		}
		c.list.SetItems(filterableItems...)
		c.list.SetFilter("")
		c.list.ScrollToTop()
		c.list.SetSelected(0)
		c.input.SetValue("")
		c.filterActive = false

		// Build flattened list for cross-level fuzzy search.
		c.allItems = flattenItems(items)
		return
	}

	c.refreshRegistry()

	commandItems := []list.FilterableItem{}
	category := commandCategoryForType(c.selected)
	if c.registry != nil {
		descriptors, _ := c.registry.Commands(slash.CommandContext{})
		for _, cmd := range descriptors {
			if cmd.Category != category {
				continue
			}
			commandItems = append(commandItems, NewCommandItem(c.com.Styles, cmd.ID, cmd.Title, cmd.Shortcut, cmd.Action))
		}
	}

	c.list.SetItems(commandItems...)
	c.list.SetFilter("")
	c.list.ScrollToTop()
	c.list.SetSelected(0)
	c.input.SetValue("")
	c.filterActive = false
	c.allItems = nil
}

func flattenItems(items []*CommandItem) []list.FilterableItem {
	var result []list.FilterableItem
	for _, item := range items {
		result = append(result, item)
		if item.HasChildren() {
			result = append(result, flattenItems(item.Children)...)
		}
	}
	return result
}

func commandCategoryForType(commandType CommandType) slash.CommandCategory {
	switch commandType {
	case UserCommands:
		return slash.CommandCategoryUser
	case MCPPrompts:
		return slash.CommandCategoryMCP
	default:
		return slash.CommandCategorySystem
	}
}

func (c *Commands) refreshRegistry() {
	c.registry = slash.NewCommandRegistry(
		slash.StaticCommandProvider{
			Info:  slash.ProviderInfo{ID: "builtin", Name: "Built-in Commands", Kind: slash.ProviderKindBuiltin},
			Items: c.defaultCommandDescriptors(),
		},
		slash.StaticCommandProvider{
			Info:  slash.ProviderInfo{ID: "session-actions", Name: "Session Actions", Kind: slash.ProviderKindSession},
			Items: c.sessionCommandDescriptors(),
		},
		slash.StaticCommandProvider{
			Info:  slash.ProviderInfo{ID: "custom", Name: "Custom Commands", Kind: slash.ProviderKindCustomCommand},
			Items: c.customCommandDescriptors(),
		},
		slash.StaticCommandProvider{
			Info:  slash.ProviderInfo{ID: "mcp-prompts", Name: "MCP Prompts", Kind: slash.ProviderKindMCP},
			Items: c.mcpPromptCommandDescriptors(),
		},
	)
}

func (c *Commands) defaultCommandDescriptors() []slash.CommandDescriptor {
	return descriptorsFromCommandItems(c.defaultCommands(), slash.CommandCategorySystem, slash.RiskLevelRead)
}

func (c *Commands) sessionCommandDescriptors() []slash.CommandDescriptor {
	if !c.hasSession {
		return nil
	}
	return []slash.CommandDescriptor{
		{
			ID:          "summarize",
			Title:       "Summarize Session",
			Description: "Compress the active session context.",
			Category:    slash.CommandCategorySystem,
			Risk:        slash.RiskLevelWrite,
			Action:      ActionSummarize{SessionID: c.sessionID},
		},
		{
			ID:          "export_markdown",
			Title:       "Export Session as Markdown",
			Description: "Export all active session messages under the global data dir as Markdown.",
			Category:    slash.CommandCategorySystem,
			Risk:        slash.RiskLevelWrite,
			Action:      ActionExportSession{SessionID: c.sessionID, Format: "markdown", Scope: "all"},
		},
		{
			ID:          "export_html",
			Title:       "Export Session as HTML",
			Description: "Export all active session messages under the global data dir as HTML.",
			Category:    slash.CommandCategorySystem,
			Risk:        slash.RiskLevelWrite,
			Action:      ActionExportSession{SessionID: c.sessionID, Format: "html", Scope: "all"},
		},
	}
}

func (c *Commands) customCommandDescriptors() []slash.CommandDescriptor {
	descriptors := make([]slash.CommandDescriptor, 0, len(c.customCommands))
	for _, cmd := range c.customCommands {
		action := ActionRunCustomCommand{
			Content:   cmd.Content,
			Arguments: cmd.Arguments,
		}
		descriptors = append(descriptors, slash.CommandDescriptor{
			ID:        "custom_" + cmd.ID,
			Title:     cmd.Name,
			Category:  slash.CommandCategoryUser,
			Arguments: cmd.Arguments,
			Risk:      slash.RiskLevelRead,
			Action:    action,
		})
	}
	return descriptors
}

func (c *Commands) mcpPromptCommandDescriptors() []slash.CommandDescriptor {
	descriptors := make([]slash.CommandDescriptor, 0, len(c.mcpPrompts))
	for _, cmd := range c.mcpPrompts {
		action := ActionRunMCPPrompt{
			Title:       cmd.Title,
			Description: cmd.Description,
			PromptID:    cmd.PromptID,
			ClientID:    cmd.ClientID,
			Arguments:   cmd.Arguments,
		}
		descriptors = append(descriptors, slash.CommandDescriptor{
			ID:          "mcp_" + cmd.ID,
			Title:       cmd.PromptID,
			Description: cmd.Description,
			Category:    slash.CommandCategoryMCP,
			Arguments:   cmd.Arguments,
			Risk:        slash.RiskLevelNetwork,
			Action:      action,
		})
	}
	return descriptors
}

func descriptorsFromCommandItems(items []*CommandItem, category slash.CommandCategory, risk slash.RiskLevel) []slash.CommandDescriptor {
	descriptors := make([]slash.CommandDescriptor, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		desc := slash.CommandDescriptor{
			ID:       item.id,
			Title:    item.title,
			Shortcut: item.shortcut,
			Category: category,
			Risk:     risk,
			Action:   item.action,
		}
		// If it has children, record its ID so completions can render a sub-group hint.
		if len(item.Children) > 0 {
			desc.ParentID = item.id
		}
		descriptors = append(descriptors, desc)
		// Flatten children into top-level list (no nesting in completions).
		for _, child := range item.Children {
			if child == nil {
				continue
			}
			descriptors = append(descriptors, slash.CommandDescriptor{
				ID:       child.id,
				Title:    child.title,
				Shortcut: child.shortcut,
				Category: category,
				Risk:     risk,
				Action:   child.action,
				ParentID: item.id, // non-empty = child of this parent
			})
		}
	}
	return descriptors
}

func (c *Commands) defaultCommands() []*CommandItem {
	cfg := c.com.Config()
	t := c.com.Styles
	var commands []*CommandItem

	// ── Agent ──
	agentCommands := []*CommandItem{
		NewParentCommandItem(t, "skplan", "SK Plan", "/plan",
			NewCommandItem(t, "skplan_start", "Start New Plan", "", ActionSelectMode{ModeID: "plan"}),
			NewCommandItem(t, "skplan_code", "Quick Code Mode", "", ActionSelectMode{ModeID: "coder"}),
		),
		NewCommandItem(t, "code", "Quick Code Mode", "/code", ActionSelectMode{ModeID: "coder"}),
		NewCommandItem(t, "switch_mode", "Switch Agent Mode...", "", ActionOpenDialog{ModesID}),
		NewCommandItem(t, "switch_model", "Switch Model...", "ctrl+l", ActionOpenDialog{ModelsID}),
	}
	commands = append(commands, NewParentCommandItem(t, "group_agent", "Agent", "", agentCommands...))

	// ── Session ──
	sessionCommands := []*CommandItem{
		NewCommandItem(t, "new_session", "New Session", "ctrl+n", ActionNewSession{}),
		NewCommandItem(t, "switch_session", "Open Sessions...", "ctrl+s", ActionOpenDialog{SessionsID}),
		NewCommandItem(t, "init", "Initialize Project", "", ActionInitializeProject{}),
	}
	// Summarize and export only when there is an active session.
	if c.hasSession {
		sessionCommands = append(sessionCommands,
			NewCommandItem(t, "context", "Browse Context Messages...", "", ActionOpenDialog{ContextID}),
			NewCommandItem(t, "summarize", "Summarize Session", "", ActionSummarize{SessionID: c.sessionID}),
			NewCommandItem(t, "export_markdown", "Export as Markdown", "", ActionExportSession{SessionID: c.sessionID, Format: "markdown", Scope: "all"}),
			NewCommandItem(t, "export_html", "Export as HTML", "", ActionExportSession{SessionID: c.sessionID, Format: "html", Scope: "all"}),
		)
	}
	if c.windowWidth >= sidebarCompactModeBreakpoint && c.hasSession {
		sessionCommands = append(sessionCommands, NewCommandItem(t, "toggle_sidebar", "Toggle Sidebar", "", ActionToggleCompactMode{}))
	}
	if c.hasTodos || c.hasQueue {
		var label string
		switch {
		case c.hasTodos && c.hasQueue:
			label = "Toggle To-Dos/Queue"
		case c.hasQueue:
			label = "Toggle Queue"
		default:
			label = "Toggle To-Dos"
		}
		sessionCommands = append(sessionCommands, NewCommandItem(t, "toggle_pills", label, "ctrl+t", ActionTogglePills{}))
	}
	commands = append(commands, NewParentCommandItem(t, "group_session", "Session", "", sessionCommands...))

	// ── Tools ──
	toolsCommands := []*CommandItem{
		NewCommandItem(t, "mcp_servers", "MCP Servers...", "", ActionOpenDialog{MCPID}),
	}
	if c.hasSession {
		agentCfg, cfgOk := cfg.Agents[config.AgentCoder]
		if cfgOk {
			model := cfg.GetModelByType(agentCfg.Model)
			if model != nil && model.SupportsImages {
				toolsCommands = append(toolsCommands, NewCommandItem(t, "file_picker", "Open File Picker", "ctrl+f", ActionOpenDialog{FilePickerID}))
			}
		}
	}
	if os.Getenv("EDITOR") != "" {
		toolsCommands = append(toolsCommands, NewCommandItem(t, "open_external_editor", "Open External Editor", "ctrl+o", ActionExternalEditor{}))
	}
	if !cfg.IsDockerMCPEnabled() && c.dockerMCPAvailable != nil && *c.dockerMCPAvailable {
		toolsCommands = append(toolsCommands, NewCommandItem(t, "enable_docker_mcp", "Enable Docker MCP", "", ActionEnableDockerMCP{}))
	}
	if cfg.IsDockerMCPEnabled() {
		toolsCommands = append(toolsCommands, NewCommandItem(t, "disable_docker_mcp", "Disable Docker MCP", "", ActionDisableDockerMCP{}))
	}
	commands = append(commands, NewParentCommandItem(t, "group_tools", "Tools", "", toolsCommands...))

	// ── Settings ──
	settingsCommands := []*CommandItem{
		NewCommandItem(t, "toggle_yolo", "Toggle Yolo Mode", "", ActionToggleYoloMode{}),
		NewCommandItem(t, "toggle_help", "Show Help", "", ActionOpenDialog{HelpID}),
	}
	notificationsDisabled := cfg != nil && cfg.Options != nil && cfg.Options.DisableNotifications
	notificationLabel := "Disable Notifications"
	if notificationsDisabled {
		notificationLabel = "Enable Notifications"
	}
	settingsCommands = append(settingsCommands, NewCommandItem(t, "toggle_notifications", notificationLabel, "", ActionToggleNotifications{}))

	transparentLabel := "Disable Transparency"
	if cfg != nil && cfg.Options != nil && cfg.Options.TUI.Transparent != nil && *cfg.Options.TUI.Transparent {
		transparentLabel = "Enable Background"
	}
	settingsCommands = append(settingsCommands, NewCommandItem(t, "toggle_transparent", transparentLabel, "", ActionToggleTransparentBackground{}))

	settingsCommands = append(settingsCommands,
		NewCommandItem(t, "proxy_configure", "Configure Proxy...", "", ActionSetProxyURL{Enabled: true}),
		NewCommandItem(t, "proxy_disable", "Disable Proxy", "", ActionSetProxyURL{Enabled: false}),
	)

	if agentCfg, cfgOk := cfg.Agents[config.AgentCoder]; cfgOk {
		providerCfg := cfg.GetProviderForModel(agentCfg.Model)
		model := cfg.GetModelByType(agentCfg.Model)
		if providerCfg != nil && model != nil && model.CanReason {
			selectedModel := cfg.Models[agentCfg.Model]
			if model.CanReason && len(model.ReasoningLevels) == 0 {
				status := "Enable"
				if selectedModel.Think {
					status = "Disable"
				}
				settingsCommands = append(settingsCommands, NewCommandItem(t, "toggle_thinking", status+" Thinking Mode", "", ActionToggleThinking{}))
			}
			if len(model.ReasoningLevels) > 0 {
				settingsCommands = append(settingsCommands, NewCommandItem(t, "select_reasoning_effort", "Select Reasoning Effort...", "", ActionOpenDialog{ReasoningID}))
			}
		}
	}
	commands = append(commands, NewParentCommandItem(t, "group_settings", "Settings", "", settingsCommands...))

	// ── Admin ──
	adminCommands := []*CommandItem{
		NewCommandItem(t, "admin_panel", "Open Admin Panel", "", ActionOpenAdmin{}),
		NewCommandItem(t, "admin_start", "Start Admin Server", "", ActionStartAdmin{}),
		NewCommandItem(t, "admin_stop", "Stop Admin Server", "", ActionStopAdmin{}),
		NewCommandItem(t, "wechat_login", "Connect WeChat", "", ActionOpenDialog{WeChatQRID}),
		NewCommandItem(t, "wechat_manager", "Manage WeChat accounts", "", ActionOpenDialog{WeChatManagerID}),
	}
	commands = append(commands, NewParentCommandItem(t, "group_admin", "Admin", "", adminCommands...))

	// ── Exit ──
	commands = append(commands, NewCommandItem(t, "quit", "Quit Ctrl+C", "ctrl+c", tea.QuitMsg{}))

	return commands
}
