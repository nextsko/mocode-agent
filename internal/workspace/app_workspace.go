package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/package-register/mocode/internal/agent"
	mcptools "github.com/package-register/mocode/internal/agent/tools/mcp"
	"github.com/package-register/mocode/internal/app"
	"github.com/package-register/mocode/internal/commands"
	"github.com/package-register/mocode/internal/capability"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/history"
	"github.com/package-register/mocode/internal/knowledge/kngs"
	"github.com/package-register/mocode/internal/lsp"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
)

// AppWorkspace implements the Workspace interface by delegating
// directly to an in-process [app.App] instance. This is the default
// mode when the client/server architecture is not enabled.
type AppWorkspace struct {
	app   *app.App
	store *config.ConfigStore
}

// NewAppWorkspace creates a new AppWorkspace wrapping the given app
// and config store.
func NewAppWorkspace(a *app.App, store *config.ConfigStore) *AppWorkspace {
	return &AppWorkspace{
		app:   a,
		store: store,
	}
}

// -- Sessions --

func (w *AppWorkspace) CreateSession(ctx context.Context, title string) (session.Session, error) {
	return w.app.CreateSession(ctx, title)
}

func (w *AppWorkspace) GetSession(ctx context.Context, sessionID string) (session.Session, error) {
	return w.app.GetSession(ctx, sessionID)
}

func (w *AppWorkspace) ListSessions(ctx context.Context) ([]session.Session, error) {
	return w.app.ListSessions(ctx)
}

func (w *AppWorkspace) SaveSession(ctx context.Context, sess session.Session) (session.Session, error) {
	return w.app.SaveSession(ctx, sess)
}

func (w *AppWorkspace) DeleteSession(ctx context.Context, sessionID string) error {
	return w.app.DeleteSession(ctx, sessionID)
}

func (w *AppWorkspace) CreateAgentToolSessionID(messageID, toolCallID string) string {
	return w.app.Sessions.CreateAgentToolSessionID(messageID, toolCallID)
}

func (w *AppWorkspace) ParseAgentToolSessionID(sessionID string) (string, string, bool) {
	return w.app.Sessions.ParseAgentToolSessionID(sessionID)
}

// -- Messages --

func (w *AppWorkspace) ListMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return w.app.Messages.List(ctx, sessionID)
}

func (w *AppWorkspace) ListUserMessages(ctx context.Context, sessionID string) ([]message.Message, error) {
	return w.app.Messages.ListUserMessages(ctx, sessionID)
}

func (w *AppWorkspace) ListAllUserMessages(ctx context.Context) ([]message.Message, error) {
	return w.app.Messages.ListAllUserMessages(ctx)
}

func (w *AppWorkspace) UpdateMessage(ctx context.Context, msg message.Message) error {
	return w.app.Messages.Update(ctx, msg)
}

func (w *AppWorkspace) DeleteMessage(ctx context.Context, messageID string) error {
	return w.app.Messages.Delete(ctx, messageID)
}

// TruncateMessagesAfter deletes all messages after the specified message
// in the session. Returns the number of messages deleted.
func (w *AppWorkspace) TruncateMessagesAfter(ctx context.Context, sessionID string, messageID string) (int, error) {
	msgs, err := w.app.Messages.List(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("list messages: %w", err)
	}

	// Find the target message index
	targetIdx := -1
	for i, msg := range msgs {
		if msg.ID == messageID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return 0, fmt.Errorf("target message not found: %s", messageID)
	}

	deleted := len(msgs) - targetIdx - 1
	if deleted <= 0 {
		return 0, nil
	}

	// Delete messages by ID, working backwards
	for i := len(msgs) - 1; i > targetIdx; i-- {
		if err := w.app.Messages.Delete(ctx, msgs[i].ID); err != nil {
			return 0, fmt.Errorf("delete message %s: %w", msgs[i].ID, err)
		}
	}

	return deleted, nil
}

// -- Agent --

func (w *AppWorkspace) AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error {
	if w.app.AgentCoordinator == nil {
		return errors.New("agent coordinator not initialized")
	}
	_, err := w.app.AgentCoordinator.Run(ctx, sessionID, prompt, attachments...)
	return err
}

func (w *AppWorkspace) AgentCancel(sessionID string) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.Cancel(sessionID)
	}
}

func (w *AppWorkspace) AgentIsBusy() bool {
	if w.app.AgentCoordinator == nil {
		return false
	}
	return w.app.AgentCoordinator.IsBusy()
}

func (w *AppWorkspace) AgentIsSessionBusy(sessionID string) bool {
	if w.app.AgentCoordinator == nil {
		return false
	}
	return w.app.AgentCoordinator.IsSessionBusy(sessionID)
}

func (w *AppWorkspace) AgentModel() AgentModel {
	if w.app.AgentCoordinator == nil {
		return AgentModel{}
	}
	m := w.app.AgentCoordinator.Model()
	return AgentModel{
		CatwalkCfg: m.CatwalkCfg,
		ModelCfg:   m.ModelCfg,
	}
}

func (w *AppWorkspace) AgentIsReady() bool {
	return w.app.AgentCoordinator != nil
}

func (w *AppWorkspace) AgentQueuedPrompts(sessionID string) int {
	if w.app.AgentCoordinator == nil {
		return 0
	}
	return w.app.AgentCoordinator.QueuedPrompts(sessionID)
}

func (w *AppWorkspace) AgentQueuedPromptsList(sessionID string) []string {
	if w.app.AgentCoordinator == nil {
		return nil
	}
	return w.app.AgentCoordinator.QueuedPromptsList(sessionID)
}

func (w *AppWorkspace) AgentClearQueue(sessionID string) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.ClearQueue(sessionID)
	}
}

func (w *AppWorkspace) AgentSummarize(ctx context.Context, sessionID string) error {
	if w.app.AgentCoordinator == nil {
		return errors.New("agent coordinator not initialized")
	}
	return w.app.AgentCoordinator.Summarize(ctx, sessionID)
}

func (w *AppWorkspace) UpdateAgentModel(ctx context.Context) error {
	return w.app.UpdateAgentModel(ctx)
}

func (w *AppWorkspace) InitCoderAgent(ctx context.Context) error {
	return w.app.InitCoderAgent(ctx)
}

func (w *AppWorkspace) GetDefaultSmallModel(providerID string) config.SelectedModel {
	return w.app.GetDefaultSmallModel(providerID)
}

// -- Permissions --

func (w *AppWorkspace) PermissionGrant(perm permission.PermissionRequest) {
	w.app.Permissions.Grant(perm)
}

func (w *AppWorkspace) PermissionGrantPersistent(perm permission.PermissionRequest) {
	w.app.Permissions.GrantPersistent(perm)
}

func (w *AppWorkspace) PermissionDeny(perm permission.PermissionRequest) {
	w.app.Permissions.Deny(perm)
}

func (w *AppWorkspace) PermissionSkipRequests() bool {
	return w.app.Permissions.SkipRequests()
}

func (w *AppWorkspace) PermissionSetSkipRequests(skip bool) {
	w.app.Permissions.SetSkipRequests(skip)
}

// -- FileTracker --

func (w *AppWorkspace) FileTrackerRecordRead(ctx context.Context, sessionID, path string) {
	w.app.FileTracker.RecordRead(ctx, sessionID, path)
}

func (w *AppWorkspace) FileTrackerLastReadTime(ctx context.Context, sessionID, path string) time.Time {
	return w.app.FileTracker.LastReadTime(ctx, sessionID, path)
}

func (w *AppWorkspace) FileTrackerListReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	return w.app.FileTracker.ListReadFiles(ctx, sessionID)
}

// -- History --

func (w *AppWorkspace) ListSessionHistory(ctx context.Context, sessionID string) ([]history.File, error) {
	return w.app.History.ListBySession(ctx, sessionID)
}

// -- LSP --

func (w *AppWorkspace) LSPStart(ctx context.Context, path string) {
	w.app.LSPManager.Start(ctx, path)
}

func (w *AppWorkspace) LSPStopAll(ctx context.Context) {
	w.app.LSPManager.StopAll(ctx)
}

func (w *AppWorkspace) LSPGetStates() map[string]LSPClientInfo {
	states := app.GetLSPStates()
	result := make(map[string]LSPClientInfo, len(states))
	for k, v := range states {
		result[k] = LSPClientInfo{
			Name:            v.Name,
			State:           v.State,
			Error:           v.Error,
			DiagnosticCount: v.DiagnosticCount,
			ConnectedAt:     v.ConnectedAt,
		}
	}
	return result
}

func (w *AppWorkspace) LSPGetDiagnosticCounts(name string) lsp.DiagnosticCounts {
	state, ok := app.GetLSPState(name)
	if !ok || state.Client == nil {
		return lsp.DiagnosticCounts{}
	}
	return state.Client.GetDiagnosticCounts()
}

// -- Config (read-only) --

func (w *AppWorkspace) Config() *config.Config {
	return w.store.Config()
}

func (w *AppWorkspace) WorkingDir() string {
	return w.store.WorkingDir()
}

func (w *AppWorkspace) Resolver() config.VariableResolver {
	return w.store.Resolver()
}

// -- Config mutations --

func (w *AppWorkspace) UpdatePreferredModel(scope config.Scope, modelType config.SelectedModelType, model config.SelectedModel) error {
	return w.store.UpdatePreferredModel(scope, modelType, model)
}

func (w *AppWorkspace) SetCompactMode(scope config.Scope, enabled bool) error {
	return w.store.SetCompactMode(scope, enabled)
}

func (w *AppWorkspace) SetProviderAPIKey(scope config.Scope, providerID string, apiKey any) error {
	return w.store.SetProviderAPIKey(scope, providerID, apiKey)
}

func (w *AppWorkspace) SetConfigField(scope config.Scope, key string, value any) error {
	return w.store.SetConfigField(scope, key, value)
}

func (w *AppWorkspace) RemoveConfigField(scope config.Scope, key string) error {
	return w.store.RemoveConfigField(scope, key)
}

func (w *AppWorkspace) RefreshOAuthToken(ctx context.Context, scope config.Scope, providerID string) error {
	return w.store.RefreshOAuthToken(ctx, scope, providerID)
}

// -- Project lifecycle --

func (w *AppWorkspace) ProjectNeedsInitialization() (bool, error) {
	return config.ProjectNeedsInitialization(w.store)
}

func (w *AppWorkspace) MarkProjectInitialized() error {
	return config.MarkProjectInitialized(w.store)
}

func (w *AppWorkspace) InitializePrompt() (string, error) {
	return agent.InitializePrompt(w.store)
}

// -- MCP operations --

func (w *AppWorkspace) MCPGetStates() map[string]mcptools.ClientInfo {
	return mcptools.GetStates()
}

func (w *AppWorkspace) MCPRefreshPrompts(ctx context.Context, name string) {
	mcptools.RefreshPrompts(ctx, name)
}

func (w *AppWorkspace) MCPRefreshResources(ctx context.Context, name string) {
	mcptools.RefreshResources(ctx, name)
}

func (w *AppWorkspace) RefreshMCPTools(ctx context.Context, name string) {
	mcptools.RefreshTools(ctx, w.store, name)
}

func (w *AppWorkspace) ReadMCPResource(ctx context.Context, name, uri string) ([]MCPResourceContents, error) {
	contents, err := mcptools.ReadResource(ctx, w.store, name, uri)
	if err != nil {
		return nil, err
	}
	result := make([]MCPResourceContents, len(contents))
	for i, c := range contents {
		result[i] = MCPResourceContents{
			URI:      c.URI,
			MIMEType: c.MIMEType,
			Text:     c.Text,
			Blob:     c.Blob,
		}
	}
	return result, nil
}

func (w *AppWorkspace) GetMCPPrompt(clientID, promptID string, args map[string]string) (string, error) {
	return commands.GetMCPPrompt(w.store, clientID, promptID, args)
}

func (w *AppWorkspace) EnableMCP(ctx context.Context, name string) error {
	if name == config.DockerMCPName {
		return w.EnableDockerMCP(ctx)
	}
	if _, ok := w.store.Config().MCP[name]; !ok {
		return fmt.Errorf("mcp '%s' not found in configuration", name)
	}
	if err := w.store.SetConfigField(config.ScopeGlobal, "mcp."+name+".disabled", false); err != nil {
		return err
	}
	return mcptools.InitializeSingle(ctx, name, w.store)
}

func (w *AppWorkspace) DisableMCP(name string) error {
	if name == config.DockerMCPName {
		return w.DisableDockerMCP()
	}
	if _, ok := w.store.Config().MCP[name]; !ok {
		return fmt.Errorf("mcp '%s' not found in configuration", name)
	}
	if err := mcptools.DisableSingle(w.store, name); err != nil {
		return err
	}
	return w.store.SetConfigField(config.ScopeGlobal, "mcp."+name+".disabled", true)
}

func (w *AppWorkspace) EnableDockerMCP(ctx context.Context) error {
	mcpConfig, err := w.store.PrepareDockerMCPConfig()
	if err != nil {
		return err
	}

	if err := mcptools.InitializeSingle(ctx, config.DockerMCPName, w.store); err != nil {
		disableErr := mcptools.DisableSingle(w.store, config.DockerMCPName)
		delete(w.store.Config().MCP, config.DockerMCPName)
		return fmt.Errorf("failed to start docker MCP: %w", errors.Join(err, disableErr))
	}

	if err := w.store.PersistDockerMCPConfig(mcpConfig); err != nil {
		disableErr := mcptools.DisableSingle(w.store, config.DockerMCPName)
		delete(w.store.Config().MCP, config.DockerMCPName)
		return fmt.Errorf("docker MCP started but failed to persist configuration: %w", errors.Join(err, disableErr))
	}

	return nil
}

func (w *AppWorkspace) DisableDockerMCP() error {
	if err := mcptools.DisableSingle(w.store, config.DockerMCPName); err != nil {
		return fmt.Errorf("failed to disable docker MCP: %w", err)
	}
	return w.store.DisableDockerMCP()
}

// -- Lifecycle --

func (w *AppWorkspace) Subscribe(program *tea.Program) {
	w.app.Subscribe(program)
}

func (w *AppWorkspace) Shutdown() {
	w.app.Shutdown()
}

// App returns the underlying app.App instance.
func (w *AppWorkspace) App() *app.App {
	return w.app
}

// Store returns the underlying config store.
func (w *AppWorkspace) Store() *config.ConfigStore {
	return w.store
}

// Compile-time check that AppWorkspace implements Workspace.
var _ Workspace = (*AppWorkspace)(nil)

func (w *AppWorkspace) AvailableAgents() []AgentInfo {
	cfg := w.Config()
	if cfg == nil || cfg.Agents == nil {
		return nil
	}
	agents := make([]AgentInfo, 0, len(cfg.Agents))
	for id, a := range cfg.Agents {
		if a.Disabled {
			continue
		}
		info := AgentInfo{
			ID:          id,
			Name:        a.Name,
			Description: a.Description,
			SubAgents:   a.SubAgents,
		}
		if model, ok := cfg.Models[a.Model]; ok {
			info.Model = model.Model
			if p, ok := cfg.Providers.Get(model.Provider); ok {
				info.Provider = p.Name
			}
		}
		agents = append(agents, info)
	}
	return agents
}

func (w *AppWorkspace) InitKnowledge(ctx context.Context) ([]string, error) {
	_ = ctx
	kngDir := filepath.Join(w.WorkingDir(), ".mocode", "kngs")
	return kngs.InitTemplates([]string{kngDir})
}

func (w *AppWorkspace) CurrentAgentID() string {
	if w.app.AgentCoordinator == nil {
		return ""
	}
	return w.app.AgentCoordinator.ActiveAgentID()
}

func (w *AppWorkspace) SwitchAgent(ctx context.Context, agentID string) error {
	if w.app.AgentCoordinator == nil {
		return fmt.Errorf("agent coordinator not initialized")
	}
	cfg := w.Config()
	if cfg != nil && cfg.Options != nil {
		cfg.Options.ActiveMode = agentID
	}
	return w.app.AgentCoordinator.SetMainAgent(agentID)
}

// BuildCommandRegistry returns a flat list of all command descriptors from all registered
// providers. Action is nil — UI layer maps ID to Action to avoid circular deps.
func (w *AppWorkspace) BuildCommandRegistry() []capability.CommandDescriptor {
	cfg := w.Config()

	var customDescs []capability.CommandDescriptor
	if cfg != nil {
		if customCommands, err := commands.LoadCustomCommands(cfg); err == nil {
			customDescs = make([]capability.CommandDescriptor, 0, len(customCommands))
			for _, cmd := range customCommands {
				customDescs = append(customDescs, capability.CommandDescriptor{
					ID: "custom_" + cmd.ID, Title: cmd.Name,
					Category: capability.CommandCategoryUser, Arguments: cmd.Arguments,
					Risk: capability.RiskLevelRead,
				})
			}
		}
	}

	mcpDescs := make([]capability.CommandDescriptor, 0)
	if mcpPrompts, err := commands.LoadMCPPrompts(); err == nil {
		for _, prompt := range mcpPrompts {
			mcpDescs = append(mcpDescs, capability.CommandDescriptor{
				ID: "mcp_" + prompt.ID,
				Title: prompt.Title, Description: prompt.Description,
				Category: capability.CommandCategoryMCP,
				Arguments: prompt.Arguments,
				Risk: capability.RiskLevelNetwork,
				Provider: capability.ProviderInfo{ID: "mcp-" + prompt.ClientID, Name: prompt.ClientID, Kind: capability.ProviderKindMCP},
			})
		}
	}

	builtinDescs := []capability.CommandDescriptor{
		{ID: "skplan", Title: "SK Plan", Description: "/plan", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "skplan_start", Title: "Start New Plan", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead, ParentID: "skplan"},
		{ID: "skplan_code", Title: "Quick Code Mode", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead, ParentID: "skplan"},
		{ID: "code", Title: "Quick Code Mode", Shortcut: "/code", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "switch_mode", Title: "Switch Agent Mode...", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "switch_model", Title: "Switch Model...", Shortcut: "ctrl+l", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "new", Title: "New Session", Shortcut: "/new", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "history", Title: "Browse Past Sessions", Shortcut: "/history", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "init", Title: "Initialize Project", Shortcut: "/init", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "init_kng", Title: "Initialize Kng Knowledge Templates", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "context", Title: "Browse Current Context Messages", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "rollback", Title: "Rollback Files to Session Node", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "summarize", Title: "Summarize Current Session", Shortcut: "/summarize", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "export_md", Title: "Export Session as Markdown", Shortcut: "/export-md", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "export_html", Title: "Export Session as HTML", Shortcut: "/export-html", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "sidebar", Title: "Toggle Sidebar", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "tasks", Title: "Toggle To-Dos / Queue", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "mcps", Title: "MCP Servers", Shortcut: "/mcps", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "wechat", Title: "Manage WeChat Accounts", Shortcut: "/wechat", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "editor", Title: "Open External Editor", Shortcut: "/editor", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelWrite},
		{ID: "approve", Title: "Toggle Auto-Approve (Yolo)", Shortcut: "/approve", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "notifications", Title: "Toggle Notifications", Shortcut: "/notifications", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "theme", Title: "Toggle Transparent Background", Shortcut: "/theme", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "think", Title: "Toggle Thinking Mode", Shortcut: "/think", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "reasoning", Title: "Select Reasoning Effort", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "help", Title: "Show Help & Key Bindings", Shortcut: "/help", Category: capability.CommandCategorySystem, Risk: capability.RiskLevelRead},
		{ID: "admin", Title: "Open Admin Panel", Shortcut: "/admin", Category: capability.CommandCategoryAdmin, Risk: capability.RiskLevelRead},
		{ID: "admin_start", Title: "Start Admin Server", Shortcut: "/admin-start", Category: capability.CommandCategoryAdmin, Risk: capability.RiskLevelWrite},
		{ID: "admin_stop", Title: "Stop Admin Server", Shortcut: "/admin-stop", Category: capability.CommandCategoryAdmin, Risk: capability.RiskLevelWrite},
		{ID: "minimax", Title: "MiniMax Quota", Shortcut: "/minimax", Category: capability.CommandCategoryAdmin, Risk: capability.RiskLevelRead},
		{ID: "quit", Title: "Quit", Shortcut: "/quit", Category: capability.CommandCategoryAdmin, Risk: capability.RiskLevelDangerous},
	}

	return append(append(builtinDescs, customDescs...), mcpDescs...)
}
