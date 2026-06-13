package tools

import (
	"context"
	"fmt"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/tools/builtin/exec"
	"github.com/package-register/mocode/internal/agent/tools/builtin/file"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitea"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitops"
	lsptool "github.com/package-register/mocode/internal/agent/tools/plugins/lsp"
	"github.com/package-register/mocode/internal/agent/tools/plugins/mcp"
	"github.com/package-register/mocode/internal/agent/tools/plugins/mocode"
	"github.com/package-register/mocode/internal/agent/tools/plugins/network"
	"github.com/package-register/mocode/internal/agent/tools/plugins/search"
	sessiontool "github.com/package-register/mocode/internal/agent/tools/plugins/session"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh"
	"github.com/package-register/mocode/internal/agent/tools/plugins/think"
	"github.com/package-register/mocode/internal/agent/toolutil/capability"
	"github.com/package-register/mocode/internal/agent/toolutil/retry"
	"github.com/package-register/mocode/internal/config"
	"github.com/package-register/mocode/internal/filetracker"
	"github.com/package-register/mocode/internal/history"
	"github.com/package-register/mocode/internal/knowledge/memory"
	"github.com/package-register/mocode/internal/lsp"
	"github.com/package-register/mocode/internal/permission"
	"github.com/package-register/mocode/internal/session"
	"github.com/package-register/mocode/internal/session/message"
	"github.com/package-register/mocode/internal/skills"
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
	Memory          memory.Service
	AllSkills       []*skills.Skill
	ActiveSkills    []*skills.Skill
	SkillTracker    *skills.Tracker
	ModelName       string
	SummarySchedule SessionSummaryScheduler
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
		memoryPlugin{},
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
		{Name: JobKillToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
	}
}

func (execPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		exec.NewBashTool(deps.Permissions, deps.Cfg.WorkingDir(), deps.Cfg.Config().Options.Attribution, deps.ModelName),
		exec.NewJobOutputTool(),
		exec.NewJobKillTool(),
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
		file.NewEditTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		file.NewMultiEditTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		file.NewViewTool(deps.LSPManager, deps.Permissions, deps.FileTracker, deps.SkillTracker, wd, cfg.Options.SkillsPaths...),
		file.NewReadFilesTool(deps.Permissions, deps.FileTracker, wd),
		file.NewWriteTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		file.NewLsTool(deps.Permissions, wd, cfg.Tools.Ls),
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
		search.NewGlobTool(wd),
		search.NewGrepTool(wd, deps.Cfg.Config().Tools.Grep),
		search.NewSourcegraphTool(webClient),
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
	retryPolicy := retry.DefaultPolicy()
	return []fantasy.AgentTool{
		retry.Wrap(network.NewFetchTool(deps.Permissions, wd, webClient), retryPolicy),
		retry.Wrap(network.NewCrawlTool(webClient), retryPolicy),
		retry.Wrap(network.NewDownloadTool(deps.Permissions, wd, downloadClient), retryPolicy),
		network.NewDownloadDocsTool(cfg.ResolvedProxyURL(deps.Cfg.Resolver())),
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
	}
}

func (sessionPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		sessiontool.NewTodosTool(deps.Sessions),
		sessiontool.NewSessionExportTool(deps.Messages, deps.Cfg.WorkingDir()),
		sessiontool.NewMessageExportTool(deps.Messages, deps.Cfg.WorkingDir()),
		sessiontool.NewSessionSummaryTool(deps.Sessions, deps.Messages, deps.Cfg.WorkingDir(), sessiontool.SessionSummaryScheduler(deps.SummarySchedule)),
	}
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
		mocode.NewMocodeInfoTool(deps.Cfg, deps.LSPManager, deps.AllSkills, deps.ActiveSkills, deps.SkillTracker),
		mocode.NewMocodeLogsTool(deps.Cfg.Config().Options.DataDirectory),
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
		lsptool.NewDiagnosticsTool(deps.LSPManager),
		lsptool.NewReferencesTool(deps.LSPManager),
		lsptool.NewLSPRestartTool(deps.LSPManager),
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
		mcp.NewListMCPResourcesTool(deps.Cfg, deps.Permissions),
		mcp.NewReadMCPResourceTool(deps.Cfg, deps.Permissions),
	}
}

// ─── plugin/memory ────────────────────────────────────────────────────────────

type memoryPlugin struct{}

func (memoryPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: memory.AddToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
		{Name: memory.UpdateToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
		{Name: memory.DeleteToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
		{Name: memory.ClearToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
		{Name: memory.SearchToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
		{Name: memory.LoadToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
	}
}

func (memoryPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	if deps.Memory == nil {
		return nil
	}
	return deps.Memory.Tools()
}

// ─── plugin/gitea ────────────────────────────────────────────────────────────

type giteaPlugin struct{}

func (giteaPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: gitea.IssuesToolName, Kind: ToolKindPlugin, Category: CategoryGitea},
		{Name: gitea.PullsToolName, Kind: ToolKindPlugin, Category: CategoryGitea},
		{Name: gitea.NotificationsToolName, Kind: ToolKindPlugin, Category: CategoryGitea},
	}
}

func (giteaPlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		gitea.NewIssuesTool(),
		gitea.NewPullsTool(),
		gitea.NewNotificationsTool(),
	}
}

// ─── plugin/think ─────────────────────────────────────────────────────────────

type thinkPlugin struct{}

func (thinkPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: think.ThinkToolName, Kind: ToolKindPlugin, Category: CategoryReasoning},
	}
}

func (thinkPlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{think.NewThinkTool()}
}

// ─── plugin/gitops ────────────────────────────────────────────────────────────

type gitOpsPlugin struct{}

func (gitOpsPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: gitops.PlanCommitsToolName, Kind: ToolKindPlugin, Category: CategoryGitOps},
		{Name: gitops.ExecuteCommitsToolName, Kind: ToolKindPlugin, Category: CategoryGitOps},
	}
}

func (gitOpsPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	return []fantasy.AgentTool{
		gitops.NewPlanCommitsTool(wd),
		gitops.NewExecuteCommitsTool(wd),
	}
}

// ─── plugin/ssh ──────────────────────────────────────────────────────────────

// sshPlugin is a thin wrapper around the ssh package.  It owns a single
// *ssh.Service (which itself owns the connection pool) so the four SSH
// tools share state.  The Startable hooks let the registry close the
// pool on shutdown.
type sshPlugin struct {
	svc *ssh.Service
}

func (p *sshPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: ssh.SshExecToolName, Kind: ToolKindPlugin, Category: CategorySSH},
		{Name: ssh.SshUploadToolName, Kind: ToolKindPlugin, Category: CategorySSH},
		{Name: ssh.SshDownloadToolName, Kind: ToolKindPlugin, Category: CategorySSH},
		{Name: ssh.SshListHostsToolName, Kind: ToolKindPlugin, Category: CategorySSH},
	}
}

func (p *sshPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	if p.svc == nil {
		p.svc = ssh.NewService()
	}
	return []fantasy.AgentTool{
		ssh.NewSshExecTool(p.svc, deps.Permissions),
		ssh.NewSshUploadTool(p.svc, deps.Permissions),
		ssh.NewSshDownloadTool(p.svc, deps.Permissions),
		ssh.NewSshListHostsTool(p.svc),
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

// StartAll calls Start on every plugin that implements capability.Startable.
// It stops on the first error and returns it wrapped with the plugin name.
// Non-startable plugins are silently skipped.
// Inspired by docker-agent/pkg/tools/startable.go.
func (r *Registry) StartAll(ctx context.Context) error {
	for _, p := range r.plugins {
		if s, ok := p.(capability.Startable); ok {
			if err := s.Start(ctx); err != nil {
				return fmt.Errorf("StartAll: plugin start failed: %w", err)
			}
		}
	}
	return nil
}

// StopAll calls Stop on every plugin that implements capability.Startable.
// It continues through all plugins on error, collecting the last error seen.
// Non-startable plugins are silently skipped.
func (r *Registry) StopAll(ctx context.Context) error {
	var lastErr error
	for _, p := range r.plugins {
		if s, ok := p.(capability.Startable); ok {
			if err := s.Stop(ctx); err != nil {
				lastErr = fmt.Errorf("StopAll: plugin stop failed: %w", err)
			}
		}
	}
	return lastErr
}
