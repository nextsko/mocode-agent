package tools

import (
	"context"
	"fmt"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/store"
	"github.com/nextsko/mocode-agent/internal/util/infra"
	"github.com/nextsko/mocode-agent/internal/util/log"
	"github.com/nextsko/mocode-agent/tools/lsp"
	"github.com/nextsko/mocode-agent/tools/plugins/sshcommon"
)

// ToolKind distinguishes builtin from plugin tools.
type ToolKind string

const (
	// ToolKindBuiltin marks tools that every agent core relies on.
	ToolKindBuiltin ToolKind = "builtin"
	// ToolKindPlugin marks optional or role-specific tools.
	ToolKindPlugin ToolKind = "plugin"
)

// ToolCategory groups tools by functional area.
type ToolCategory string

const (
	CategoryFile      ToolCategory = "file"
	CategoryExec      ToolCategory = "exec"
	CategorySearch    ToolCategory = "search"
	CategoryNetwork   ToolCategory = "network"
	CategoryLSP       ToolCategory = "lsp"
	CategoryMocode    ToolCategory = "mocode"
	CategorySession   ToolCategory = "session"
	CategoryMCPMeta   ToolCategory = "mcp_meta"
	CategoryMemory    ToolCategory = "memory"
	CategoryReasoning ToolCategory = "reasoning"
	CategoryGitea     ToolCategory = "gitea"
	CategoryGitOps    ToolCategory = "gitops"
	CategorySSH       ToolCategory = "ssh"
)

// ToolDescriptor holds static metadata for a single tool.
type ToolDescriptor struct {
	Name     string
	Kind     ToolKind
	Category ToolCategory
}

// ToolPlugin builds a category of tools from dependencies.
type ToolPlugin interface {
	Descriptors() []ToolDescriptor
	Build(ctx context.Context, deps ToolDeps) []fantasy.AgentTool
}

// ToolDeps holds all runtime dependencies needed to construct tools.
type ToolDeps struct {
	Cfg             *config.ConfigStore
	Permissions     permission.Service
	LSPManager      *lsp.Manager
	History         history.Service
	FileTracker     filetracker.Service
	Sessions        session.Service
	Messages        message.Service
	AllSkills       []*skills.Skill
	ActiveSkills    []*skills.Skill
	SkillTracker    *skills.Tracker
	ModelName       string
	SummarySchedule SessionSummaryScheduler
	SessionSearch   *store.SessionSearch
}

// AllToolDescriptors returns descriptors for every standard (non-coordinator) tool.
func AllToolDescriptors() []ToolDescriptor {
	var all []ToolDescriptor
	for _, p := range standardPlugins() {
		all = append(all, p.Descriptors()...)
	}
	return all
}

// AllToolNames returns the names of all standard (non-coordinator) tools.
// This is the canonical source of truth; config.allToolNames mirrors this list.
func AllToolNames() []string {
	descs := AllToolDescriptors()
	names := make([]string, len(descs))
	for i, d := range descs {
		names[i] = d.Name
	}
	return names
}

// Registry holds the ordered set of compiled-in tool plugins.
type Registry struct {
	plugins []ToolPlugin
}

// NewRegistry creates a Registry pre-loaded with all standard plugins.
func NewRegistry() *Registry {
	return &Registry{plugins: standardPlugins()}
}

// Build runs every registered plugin and returns the combined tool list.
func (r *Registry) Build(ctx context.Context, deps ToolDeps) []fantasy.AgentTool {
	var all []fantasy.AgentTool
	for _, p := range r.plugins {
		all = append(all, p.Build(ctx, deps)...)
	}
	return all
}

// standardPlugins returns the ordered list of compiled-in plugins.
func standardPlugins() []ToolPlugin {
	return []ToolPlugin{
		execPlugin{},
		filePlugin{},
		searchPlugin{},
		networkPlugin{},
		sessionPlugin{},
		mocodePlugin{},
		lspPlugin{},
		mcpMetaPlugin{},
		thinkPlugin{},
		giteaPlugin{},
		gitOpsPlugin{},
		&sshPlugin{},
	}
}

// ─── builtin/exec ─────────────────────────────────────────────────────────────

type execPlugin struct{}

func (execPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: BashToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: JobOutputToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: JobInputToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: JobKillToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
	}
}

func (execPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		NewBashTool(deps.Permissions, deps.Cfg.WorkingDir(), deps.Cfg.Config().Options.Attribution, deps.ModelName),
		NewJobOutputTool(),
		NewJobInputTool(),
		NewJobKillTool(),
	}
}

// ─── builtin/file ─────────────────────────────────────────────────────────────

type filePlugin struct{}

func (filePlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: EditToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: MultiEditToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: ViewToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: ReadFilesToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: WriteToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: LSToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
	}
}

func (filePlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	cfg := deps.Cfg.Config()
	return []fantasy.AgentTool{
		NewEditTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		NewMultiEditTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		NewViewTool(deps.LSPManager, deps.Permissions, deps.FileTracker, deps.SkillTracker, wd, cfg.Options.SkillsPaths...),
		NewReadFilesTool(deps.Permissions, deps.FileTracker, wd),
		NewWriteTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		NewLsTool(deps.Permissions, wd, cfg.Tools.Ls),
	}
}

// ─── plugin/search ────────────────────────────────────────────────────────────

type searchPlugin struct{}

func (searchPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: GlobToolName, Kind: ToolKindPlugin, Category: CategorySearch},
		{Name: GrepToolName, Kind: ToolKindPlugin, Category: CategorySearch},
		{Name: SourcegraphToolName, Kind: ToolKindPlugin, Category: CategorySearch},
	}
}

func (searchPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	webClient := deps.Cfg.Config().HTTPClient(deps.Cfg.Resolver(), 30)
	return []fantasy.AgentTool{
		NewGlobTool(wd),
		NewGrepTool(wd, deps.Cfg.Config().Tools.Grep),
		NewSourcegraphTool(webClient),
	}
}

// ─── plugin/network ───────────────────────────────────────────────────────────

type networkPlugin struct{}

func (networkPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: FetchToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: CrawlToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: DownloadToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: DownloadDocsToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
	}
}

func (networkPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	cfg := deps.Cfg.Config()
	webClient := cfg.HTTPClient(deps.Cfg.Resolver(), 30)
	downloadClient := cfg.HTTPClient(deps.Cfg.Resolver(), 300)
	retryPolicy := toolutil.DefaultRetryPolicy()
	return []fantasy.AgentTool{
		toolutil.WithRetry(NewFetchTool(deps.Permissions, wd, webClient), retryPolicy),
		toolutil.WithRetry(NewCrawlTool(webClient), retryPolicy),
		toolutil.WithRetry(NewDownloadTool(deps.Permissions, wd, downloadClient), retryPolicy),
		NewDownloadDocsTool(cfg.ResolvedProxyURL(deps.Cfg.Resolver())),
	}
}

// ─── plugin/session ───────────────────────────────────────────────────────────

type sessionPlugin struct{}

func (sessionPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: TodosToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: SessionExportToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: MessageExportToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: SessionSummaryToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: SessionSearchToolName, Kind: ToolKindPlugin, Category: CategorySession},
	}
}

func (sessionPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	t := []fantasy.AgentTool{
		NewTodosTool(deps.Sessions),
		NewSessionExportTool(deps.Messages, deps.Cfg.WorkingDir()),
		NewMessageExportTool(deps.Messages, deps.Cfg.WorkingDir()),
		NewSessionSummaryTool(deps.Sessions, deps.Messages, deps.Cfg.WorkingDir(), SessionSummaryScheduler(deps.SummarySchedule)),
	}
	if deps.SessionSearch != nil {
		t = append(t, NewSessionSearchTool(deps.SessionSearch))
	}
	return t
}

// ─── plugin/mocode ────────────────────────────────────────────────────────────

type mocodePlugin struct{}

func (mocodePlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: MocodeInfoToolName, Kind: ToolKindPlugin, Category: CategoryMocode},
		{Name: MocodeLogsToolName, Kind: ToolKindPlugin, Category: CategoryMocode},
	}
}

func (mocodePlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		NewMocodeInfoTool(deps.Cfg, deps.LSPManager, deps.AllSkills, deps.ActiveSkills, deps.SkillTracker),
		NewMocodeLogsTool(log.MainLogPath(infra.DataDir())),
	}
}

// ─── plugin/lsp ───────────────────────────────────────────────────────────────

type lspPlugin struct{}

func (lspPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: DiagnosticsToolName, Kind: ToolKindPlugin, Category: CategoryLSP},
		{Name: ReferencesToolName, Kind: ToolKindPlugin, Category: CategoryLSP},
		{Name: LSPRestartToolName, Kind: ToolKindPlugin, Category: CategoryLSP},
	}
}

func (lspPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	cfg := deps.Cfg.Config()
	// Mirror coordinator condition: LSP tools appear when LSPs are configured
	// or auto_lsp is nil (default enabled) or explicitly true.
	if len(cfg.LSP) == 0 && cfg.Options.AutoLSP != nil && !*cfg.Options.AutoLSP {
		return nil
	}
	return []fantasy.AgentTool{
		NewDiagnosticsTool(deps.LSPManager),
		NewReferencesTool(deps.LSPManager),
		NewLSPRestartTool(deps.LSPManager),
	}
}

// ─── plugin/mcp_meta ──────────────────────────────────────────────────────────

type mcpMetaPlugin struct{}

func (mcpMetaPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: ListMCPResourcesToolName, Kind: ToolKindPlugin, Category: CategoryMCPMeta},
		{Name: ReadMCPResourceToolName, Kind: ToolKindPlugin, Category: CategoryMCPMeta},
	}
}

func (mcpMetaPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	if len(deps.Cfg.Config().MCP) == 0 {
		return nil
	}
	return []fantasy.AgentTool{
		NewListMCPResourcesTool(deps.Cfg, deps.Permissions),
		NewReadMCPResourceTool(deps.Cfg, deps.Permissions),
	}
}

// ─── plugin/gitea ────────────────────────────────────────────────────────────

type giteaPlugin struct{}

func (giteaPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: IssuesToolName, Kind: ToolKindPlugin, Category: CategoryGitea},
		{Name: PullsToolName, Kind: ToolKindPlugin, Category: CategoryGitea},
		{Name: NotificationsToolName, Kind: ToolKindPlugin, Category: CategoryGitea},
	}
}

func (giteaPlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		NewIssuesTool(),
		NewPullsTool(),
		NewNotificationsTool(),
	}
}

// ─── plugin/think ─────────────────────────────────────────────────────────────

type thinkPlugin struct{}

func (thinkPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: ThinkToolName, Kind: ToolKindPlugin, Category: CategoryReasoning},
	}
}

func (thinkPlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{NewThinkTool()}
}

// ─── plugin/gitops ────────────────────────────────────────────────────────────

type gitOpsPlugin struct{}

func (gitOpsPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: PlanCommitsToolName, Kind: ToolKindPlugin, Category: CategoryGitOps},
		{Name: ExecuteCommitsToolName, Kind: ToolKindPlugin, Category: CategoryGitOps},
	}
}

func (gitOpsPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	return []fantasy.AgentTool{
		NewPlanCommitsTool(wd),
		NewExecuteCommitsTool(wd),
	}
}

// ─── plugin/ssh ──────────────────────────────────────────────────────────────

// sshPlugin is a thin wrapper around the ssh package.  It owns a single
// *ssh.Service (which itself owns the connection pool) so the four SSH
// tools share state.  The Startable hooks let the registry close the
// pool on shutdown.
type sshPlugin struct {
	svc *sshcommon.Service
}

func (p *sshPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: sshcommon.SshExecToolName, Kind: ToolKindPlugin, Category: CategorySSH},
		{Name: sshcommon.SshUploadToolName, Kind: ToolKindPlugin, Category: CategorySSH},
		{Name: sshcommon.SshDownloadToolName, Kind: ToolKindPlugin, Category: CategorySSH},
		{Name: sshcommon.SshListHostsToolName, Kind: ToolKindPlugin, Category: CategorySSH},
	}
}

func (p *sshPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	if p.svc == nil {
		p.svc = sshcommon.NewService()
	}
	return []fantasy.AgentTool{
		NewSshExecTool(p.svc, deps.Permissions),
		NewSshUploadTool(p.svc, deps.Permissions),
		NewSshDownloadTool(p.svc, deps.Permissions),
		NewSshListHostsTool(p.svc),
	}
}

func (p *sshPlugin) Start(_ context.Context) error { return nil }

func (p *sshPlugin) Stop(_ context.Context) error {
	if p.svc == nil {
		return nil
	}
	return p.svc.Close()
}

// coordinatorToolNames returns names of tools owned by coordinator (agent,
// agentic_fetch, transfer_to_agent) that are built outside the standard
// registry due to back-references into coordinator state.
func coordinatorToolNames() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: AgentToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: AgenticFetchToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: TransferToolName, Kind: ToolKindPlugin, Category: CategorySession},
	}
}

// AgentToolName is the name of the coordinator-owned agent delegation tool.
const AgentToolName = "agent"

// NewRegistryWithPlugins creates an empty Registry pre-loaded with the given
// plugins.  Pass no plugins for an empty registry (useful in tests).
func NewRegistryWithPlugins(plugins ...ToolPlugin) *Registry {
	r := &Registry{}
	r.plugins = append(r.plugins, plugins...)
	return r
}

// AddPlugin appends p to the registry's plugin list.
func (r *Registry) AddPlugin(p ToolPlugin) {
	r.plugins = append(r.plugins, p)
}

// StartAll calls Start on every plugin that implements toolutil.Startable.
// It stops on the first error and returns it wrapped with the plugin name.
// Non-startable plugins are silently skipped.
// Inspired by docker-agent/pkg/tools/startable.go.
func (r *Registry) StartAll(ctx context.Context) error {
	for _, p := range r.plugins {
		if s, ok := p.(toolutil.Startable); ok {
			if err := s.Start(ctx); err != nil {
				return fmt.Errorf("StartAll: plugin start failed: %w", err)
			}
		}
	}
	return nil
}

// StopAll calls Stop on every plugin that implements toolutil.Startable.
// It continues through all plugins on error, collecting the last error seen.
// Non-startable plugins are silently skipped.
func (r *Registry) StopAll(ctx context.Context) error {
	var lastErr error
	for _, p := range r.plugins {
		if s, ok := p.(toolutil.Startable); ok {
			if err := s.Stop(ctx); err != nil {
				lastErr = fmt.Errorf("StopAll: plugin stop failed: %w", err)
			}
		}
	}
	return lastErr
}
