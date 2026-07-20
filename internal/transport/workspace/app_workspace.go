package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/core/agent"
	"github.com/nextsko/mocode-agent/internal/core/agent/extension"
	"github.com/nextsko/mocode-agent/internal/core/app"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/knowledge/kngs"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/ui/slash"
	"github.com/nextsko/mocode-agent/internal/util/infra"
	"github.com/nextsko/mocode-agent/tools/lsp"
	mcptools "github.com/nextsko/mocode-agent/tools/mcp"
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

func (w *AppWorkspace) AgentCancelSubagent(subagentID string) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.CancelSubagent(subagentID)
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

// AgentEnqueueSummary schedules a session summary asynchronously via
// the coordinator's summary queue. The LLM generation runs in a
// goroutine on context.Background(); the TUI is not blocked. The
// completion notification is delivered via app.events as a
// SummaryCompletedMsg (see internal/core/agent/coordinator.go).
func (w *AppWorkspace) AgentEnqueueSummary(_ context.Context, sessionID string) error {
	if w.app.AgentCoordinator == nil {
		return errors.New("agent coordinator not initialized")
	}
	w.app.AgentCoordinator.EnqueueSummaryAndDrain(sessionID)
	return nil
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

func (w *AppWorkspace) ReloadConfig(ctx context.Context) error {
	return w.store.ReloadFromDisk(ctx)
}

func (w *AppWorkspace) ReloadMCP(ctx context.Context, name string) error {
	if err := mcptools.DisableSingle(w.store, name); err != nil {
		return err
	}
	return mcptools.InitializeSingle(ctx, name, w.store)
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
	return slash.GetMCPPrompt(w.store, clientID, promptID, args)
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
	kngDir := filepath.Join(infra.Config(), "mocode", "kngs")
	return kngs.InitTemplates([]string{kngDir})
}

func (w *AppWorkspace) CurrentAgentID() string {
	if w.app.AgentCoordinator == nil {
		return ""
	}
	return w.app.AgentCoordinator.ActiveAgentID()
}

// AgentSystemPrompt returns the proven system prompt of the active agent.
func (w *AppWorkspace) AgentSystemPrompt() string {
	if w.app.AgentCoordinator == nil {
		return ""
	}
	return w.app.AgentCoordinator.ActiveAgentSystemPrompt()
}

// AgentSmallLanguageModel returns the configured small model for cheap
// auxiliary calls (the /evo lesson distiller), or nil if unconfigured.
func (w *AppWorkspace) AgentSmallLanguageModel(ctx context.Context) fantasy.LanguageModel {
	if w.app.AgentCoordinator == nil {
		return nil
	}
	return w.app.AgentCoordinator.SmallLanguageModel(ctx)
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

// AgentRegisterExtension attaches a lifecycle extension to the coordinator.
func (w *AppWorkspace) AgentRegisterExtension(ext extension.Extension) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.RegisterExtension(ext)
	}
}

// AgentUnregisterExtension detaches a lifecycle extension by name.
func (w *AppWorkspace) AgentUnregisterExtension(name string) {
	if w.app.AgentCoordinator != nil {
		w.app.AgentCoordinator.UnregisterExtension(name)
	}
}

// AgentDir returns the agents directory the config loader scans.
func (w *AppWorkspace) AgentDir() string {
	return config.ResolveAgentsDir(w.Config())
}

// AgentReload re-reads a single agent .md and merges it into the live config.
func (w *AppWorkspace) AgentReload(id, path string) error {
	return w.app.Store().ReloadAgent(id, path)
}

// BuildCommandRegistry returns a flat list of all command descriptors from all registered
// providers. Action is nil — UI layer maps ID to Action to avoid circular deps.
func (w *AppWorkspace) BuildCommandRegistry() []slash.CommandDescriptor {
	cfg := w.Config()

	var customDescs []slash.CommandDescriptor
	if cfg != nil {
		if customCommands, err := slash.LoadCustomCommands(cfg); err == nil {
			customDescs = make([]slash.CommandDescriptor, 0, len(customCommands))
			for _, cmd := range customCommands {
				customDescs = append(customDescs, slash.CommandDescriptor{
					ID: "custom_" + cmd.ID, Title: cmd.Name,
					Category: slash.CommandCategoryUser, Arguments: cmd.Arguments,
					Risk: slash.RiskLevelRead,
				})
			}
		}
	}

	mcpDescs := make([]slash.CommandDescriptor, 0)
	if mcpPrompts, err := slash.LoadMCPPrompts(); err == nil {
		for _, prompt := range mcpPrompts {
			mcpDescs = append(mcpDescs, slash.CommandDescriptor{
				ID:    "mcp_" + prompt.ID,
				Title: prompt.Title, Description: prompt.Description,
				Category:  slash.CommandCategoryMCP,
				Arguments: prompt.Arguments,
				Risk:      slash.RiskLevelNetwork,
				Provider:  slash.ProviderInfo{ID: "mcp-" + prompt.ClientID, Name: prompt.ClientID, Kind: slash.ProviderKindMCP},
			})
		}
	}

	builtinDescs := []slash.CommandDescriptor{
		{ID: "skplan", Title: "SK Plan", Description: "/plan", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "skplan_start", Title: "Start New Plan", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead, ParentID: "skplan"},
		{ID: "skplan_code", Title: "Quick Code Mode", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead, ParentID: "skplan"},
		{ID: "code", Title: "Quick Code Mode", Shortcut: "/code", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "switch_mode", Title: "Switch Agent Mode...", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "switch_model", Title: "Switch Model...", Shortcut: "ctrl+l", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "new", Title: "New Session", Shortcut: "/new", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "history", Title: "Browse Past Sessions", Shortcut: "/history", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "init", Title: "Initialize Project", Shortcut: "/init", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "init_kng", Title: "Initialize Kng Knowledge Templates", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "context", Title: "Browse Current Context Messages", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "rollback", Title: "Rollback Files to Session Node", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "summarize", Title: "Summarize Current Session", Shortcut: "/summarize", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "export_md", Title: "Export Session as Markdown", Shortcut: "/export-md", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "export_html", Title: "Export Session as HTML", Shortcut: "/export-html", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "sidebar", Title: "Toggle Sidebar", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "tasks", Title: "Toggle To-Dos / Queue", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "mcps", Title: "MCP Servers", Shortcut: "/mcps", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "wechat", Title: "Manage WeChat Accounts", Shortcut: "/wechat", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "editor", Title: "Open External Editor", Shortcut: "/editor", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelWrite},
		{ID: "approve", Title: "Toggle Auto-Approve (Yolo)", Shortcut: "/approve", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "notifications", Title: "Toggle Notifications", Shortcut: "/notifications", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "theme", Title: "Toggle Transparent Background", Shortcut: "/theme", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "think", Title: "Toggle Thinking Mode", Shortcut: "/think", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "reasoning", Title: "Select Reasoning Effort", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "help", Title: "Show Help & Key Bindings", Shortcut: "/help", Category: slash.CommandCategorySystem, Risk: slash.RiskLevelRead},
		{ID: "admin", Title: "Open Admin Panel", Shortcut: "/admin", Category: slash.CommandCategoryAdmin, Risk: slash.RiskLevelRead},
		{ID: "admin_start", Title: "Start Admin Server", Shortcut: "/admin-start", Category: slash.CommandCategoryAdmin, Risk: slash.RiskLevelWrite},
		{ID: "admin_stop", Title: "Stop Admin Server", Shortcut: "/admin-stop", Category: slash.CommandCategoryAdmin, Risk: slash.RiskLevelWrite},
		{ID: "reload", Title: "Reload Config", Shortcut: "/reload", Category: slash.CommandCategoryAdmin, Risk: slash.RiskLevelWrite},
		{ID: "reload_mcp", Title: "Reload MCP Servers", Shortcut: "/reload-mcp", Category: slash.CommandCategoryAdmin, Risk: slash.RiskLevelWrite},
		{ID: "quit", Title: "Quit", Shortcut: "/quit", Category: slash.CommandCategoryAdmin, Risk: slash.RiskLevelDangerous},
	}

	return append(append(builtinDescs, customDescs...), mcpDescs...)
}
