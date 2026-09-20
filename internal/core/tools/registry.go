package tools

import (
	"context"
	"fmt"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	corebrowser "github.com/nextsko/mocode-agent/internal/core/browser"
	"github.com/nextsko/mocode-agent/internal/core/config"
	"github.com/nextsko/mocode-agent/internal/core/memory"
	"github.com/nextsko/mocode-agent/internal/core/permission"
	"github.com/nextsko/mocode-agent/internal/core/question"
	"github.com/nextsko/mocode-agent/internal/core/skills"
	fs "github.com/nextsko/mocode-agent/internal/core/tools/core/fs"
	agentquestion "github.com/nextsko/mocode-agent/internal/core/tools/core/question"
	"github.com/nextsko/mocode-agent/internal/core/tools/core/sandbox"
	shell "github.com/nextsko/mocode-agent/internal/core/tools/core/shell"
	"github.com/nextsko/mocode-agent/internal/core/tools/core/web"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/common/ssh"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/nethttp"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/systems/browser"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/systems/gitea"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/systems/gitops"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/systems/ssh"
	agenttools "github.com/nextsko/mocode-agent/internal/core/tools/internalx/agent"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
	"github.com/nextsko/mocode-agent/internal/core/uri"
	ghuri "github.com/nextsko/mocode-agent/internal/core/uri/github"
	"github.com/nextsko/mocode-agent/internal/domain/filetracker"
	"github.com/nextsko/mocode-agent/internal/domain/history"
	"github.com/nextsko/mocode-agent/internal/domain/session"
	"github.com/nextsko/mocode-agent/internal/domain/session/message"
	"github.com/nextsko/mocode-agent/internal/store"
	"github.com/nextsko/mocode-agent/internal/util/infra"
	"github.com/nextsko/mocode-agent/internal/util/log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	CategoryQuestion  ToolCategory = "question"
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
	// CategoryBrowser groups the native browser automation tool.
	CategoryBrowser ToolCategory = "browser"
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
//
// SysML view: each field is a port on the toolkit block. HTTP is the shared
// outbound-HTTP port — plugins must derive clients from it instead of
// building their own transports, so proxy config and the connection pool are
// owned by exactly one place.
type ToolDeps struct {
	Cfg             *config.ConfigStore
	Permissions     permission.Service
	Questions       question.Service
	LSPManager      *lsp.Manager
	History         history.Service
	FileTracker     filetracker.Service
	Sessions        session.Service
	Messages        message.Service
	AllSkills       []*skills.Skill
	ActiveSkills    []*skills.Skill
	SkillTracker    *skills.Tracker
	ModelName       string
	SummarySchedule agenttools.SessionSummaryScheduler
	SessionSearch   *store.SessionSearch
	HTTP            *nethttp.Factory
}

// NewHTTPFactory builds the canonical outbound-HTTP factory from the config
// store: config's proxy-aware transport plus the resolved proxy URL (needed
// by non-http.Client stacks such as go-git). The composition root calls this
// ONCE and shares the result across every agent build.
func NewHTTPFactory(store *config.ConfigStore) *nethttp.Factory {
	cfg := store.Config()
	resolver := store.Resolver()
	return nethttp.NewFactory(cfg.HTTPTransport(resolver), cfg.ResolvedProxyURL(resolver))
}

// buildURIRegistry assembles the internal resource namespaces.
//
// This is the composition root for the mocode:// protocol: every provider is
// wired here, so the namespaces a session can reach are visible in one place
// rather than scattered across the tools that happen to use them.
func buildURIRegistry(deps ToolDeps) *uri.Registry {
	r := uri.NewRegistry()

	// skills: builtin (embedded) plus every configured user directory, so
	// mocode://skills/ lists what is actually installed rather than only what
	// ships with the binary.
	r.Register(skills.NewURIProvider(expandPaths(deps, deps.Cfg.Config().Options.SkillsPaths)...))

	// docs and exp: documented roots, falling back to the workspace conventions
	// so the namespaces are useful with no configuration at all.
	r.Register(uri.NewTreeProvider(uri.AuthorityDocs,
		"Documentation roots for this workspace: design notes, architecture docs and guides.",
		dirTrees(deps, cfgOr(deps, func(o *config.Options) []string { return o.DocsPaths }), []string{"docs"})...))

	r.Register(uri.NewTreeProvider(uri.AuthorityExp,
		"Experience notes: durable write-ups of problems solved and how, one file per lesson.",
		dirTrees(deps, cfgOr(deps, func(o *config.Options) []string { return o.ExpPaths }), []string{".exps", "docs/exps"})...))

	// github is injected only when an HTTP factory exists, so a build without
	// outbound HTTP simply lacks the namespace instead of failing at call time.
	if deps.HTTP != nil {
		r.Register(ghuri.New(deps.HTTP.Client(nethttp.DefaultTimeout)))
	}

	// memory is served from the store, whose root resolves to the app data
	// directory by default. The namespace root is the index rather than a
	// directory listing, so `view mocode://memory` answers "what do we know?"
	// directly.
	if store := memoryStore(deps); store != nil {
		r.Register(memory.NewURIProvider(store, memory.NewFileIndexer(store)))
	}

	// browser reads the live session, which may not be running yet; the
	// provider reports that rather than being absent.
	r.Register(corebrowser.NewURIProvider(browserManager()))

	return r
}

// expandPaths applies the configured variable expansion (and `~`) to each path,
// dropping empties. Discovery expands these before use; doing the same here
// keeps one meaning for "a configured directory" — an unexpanded `~` used to
// silently fail the skills containment test, so the exemption it granted never
// applied.
func expandPaths(deps ToolDeps, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, expandOne(deps, p))
	}
	return out
}

// expandOne resolves `~` and shell variables in a configured path.
func expandOne(deps ToolDeps, p string) string {
	if deps.Cfg == nil {
		return p
	}
	expanded := infra.Long(p)
	if strings.HasPrefix(expanded, "$") {
		if resolved, err := deps.Cfg.Resolver().ResolveValue(expanded); err == nil {
			expanded = resolved
		}
	}
	return expanded
}

// cfgOr reads a path list from config, falling back to workspace-relative
// defaults when it is empty.
func cfgOr(deps ToolDeps, get func(*config.Options) []string) []string {
	if deps.Cfg != nil {
		if cfg := deps.Cfg.Config(); cfg != nil && cfg.Options != nil {
			if paths := get(cfg.Options); len(paths) > 0 {
				return paths
			}
		}
	}
	return nil
}

// dirTrees turns a list of directories into trees, defaulting to workspace
// subdirectories and skipping anything that does not exist.
func dirTrees(deps ToolDeps, configured, defaults []string) []uri.Tree {
	dirs := configured
	if len(dirs) == 0 {
		wd := deps.Cfg.WorkingDir()
		for _, d := range defaults {
			dirs = append(dirs, filepath.Join(wd, d))
		}
	}

	trees := make([]uri.Tree, 0, len(dirs))
	for _, dir := range expandPaths(deps, dirs) {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			// A missing root is normal: most workspaces have docs but no
			// experience notes, and vice versa. The namespace simply lists
			// fewer trees.
			continue
		}
		trees = append(trees, uri.Tree{Label: dir, FS: os.DirFS(dir)})
	}
	return trees
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
// A nil deps.HTTP is self-healed into the config-derived factory so plugins
// and tests never need nil checks.
func (r *Registry) Build(ctx context.Context, deps ToolDeps) []fantasy.AgentTool {
	if deps.HTTP == nil && deps.Cfg != nil {
		deps.HTTP = NewHTTPFactory(deps.Cfg)
	}
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
		sandboxPlugin{},
		questionPlugin{},
		filePlugin{},
		searchPlugin{},
		networkPlugin{},
		sessionPlugin{},
		mocodePlugin{},
		lspPlugin{},
		mcpMetaPlugin{},
		thinkPlugin{},
		learnPlugin{},
		memoryPlugin{},
		browserPlugin{},
		giteaPlugin{},
		gitOpsPlugin{},
		&sshPlugin{},
	}
}

// ─── builtin/sandbox ─────────────────────────────────────────────────────────

// sandboxPlugin exposes the isolated code-execution tools (ts_run / py_run):
// temp working dir, wall-clock timeout, bounded output, uv ephemeral deps.
type sandboxPlugin struct{}

func (sandboxPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: sandbox.TsRunToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: sandbox.PyRunToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
	}
}

func (sandboxPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		sandbox.NewTsRunTool(deps.Permissions, deps.Cfg.WorkingDir()),
		sandbox.NewPyRunTool(deps.Permissions, deps.Cfg.WorkingDir()),
	}
}

// ─── builtin/exec ─────────────────────────────────────────────────────────────

type execPlugin struct{}

func (execPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: shell.BashToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: shell.JobOutputToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: shell.JobInputToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
		{Name: shell.JobKillToolName, Kind: ToolKindBuiltin, Category: CategoryExec},
	}
}

func (execPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		shell.NewBashTool(deps.Permissions, deps.Cfg.WorkingDir(), deps.Cfg.Config().Options.Attribution, deps.ModelName),
		shell.NewJobOutputTool(),
		shell.NewJobInputTool(),
		shell.NewJobKillTool(),
	}
}

// ─── builtin/question ────────────────────────────────────────────────────────

// questionPlugin exposes the AskUser-style question tool (crush-compatible):
// 4 question types, batched tabbed forms, blocking-until-answered.
type questionPlugin struct{}

func (questionPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: agentquestion.QuestionToolName, Kind: ToolKindBuiltin, Category: CategoryQuestion},
	}
}

func (questionPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	if deps.Questions == nil {
		return nil
	}
	return []fantasy.AgentTool{agentquestion.NewQuestionTool(deps.Questions)}
}

// ─── builtin/file ─────────────────────────────────────────────────────────────

type filePlugin struct{}

func (filePlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: fs.EditToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: fs.MultiEditToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: fs.ViewToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: fs.ReadFilesToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: fs.WriteToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
		{Name: fs.LSToolName, Kind: ToolKindBuiltin, Category: CategoryFile},
	}
}

func (filePlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	cfg := deps.Cfg.Config()
	return []fantasy.AgentTool{
		fs.NewEditTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		fs.NewMultiEditTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		fs.NewViewTool(deps.LSPManager, deps.Permissions, deps.FileTracker, deps.SkillTracker, buildURIRegistry(deps), wd, expandPaths(deps, cfg.Options.SkillsPaths)...),
		fs.NewReadFilesTool(deps.Permissions, deps.FileTracker, wd),
		fs.NewWriteTool(deps.LSPManager, deps.Permissions, deps.History, deps.FileTracker, wd),
		fs.NewLsTool(deps.Permissions, wd, cfg.Tools.Ls),
	}
}

// ─── plugin/search ────────────────────────────────────────────────────────────

type searchPlugin struct{}

func (searchPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: fs.GlobToolName, Kind: ToolKindPlugin, Category: CategorySearch},
		{Name: fs.GrepToolName, Kind: ToolKindPlugin, Category: CategorySearch},
		{Name: web.SourcegraphToolName, Kind: ToolKindPlugin, Category: CategorySearch},
	}
}

func (searchPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	webClient := deps.HTTP.Client(nethttp.DefaultTimeout)
	return []fantasy.AgentTool{
		fs.NewGlobTool(wd),
		fs.NewGrepTool(wd, deps.Cfg.Config().Tools.Grep),
		web.NewSourcegraphTool(webClient),
	}
}

// ─── plugin/network ───────────────────────────────────────────────────────────

type networkPlugin struct{}

func (networkPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: web.FetchToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: web.CrawlToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: web.DownloadToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: web.DownloadDocsToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
	}
}

func (networkPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	wd := deps.Cfg.WorkingDir()
	// All web clients derive from the one shared proxy-aware factory.
	// Regression note: these used to be HTTPClient(resolver, 30) and
	// (resolver, 300) — bare ints are nanoseconds, so every fetch/crawl/
	// download died instantly with "context deadline exceeded".
	webClient := deps.HTTP.Client(nethttp.DefaultTimeout)
	downloadClient := deps.HTTP.Client(nethttp.DownloadTimeout)
	retryPolicy := toolutil.DefaultRetryPolicy()
	return []fantasy.AgentTool{
		toolutil.WithRetry(web.NewFetchTool(deps.Permissions, wd, webClient), retryPolicy),
		toolutil.WithRetry(web.NewCrawlTool(webClient), retryPolicy),
		toolutil.WithRetry(web.NewDownloadTool(deps.Permissions, wd, downloadClient), retryPolicy),
		web.NewDownloadDocsTool(deps.HTTP),
	}
}

// ─── plugin/session ───────────────────────────────────────────────────────────

type sessionPlugin struct{}

func (sessionPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: agenttools.TodosToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: agenttools.SessionExportToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: agenttools.MessageExportToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: agenttools.SessionSummaryToolName, Kind: ToolKindPlugin, Category: CategorySession},
		{Name: agenttools.SessionSearchToolName, Kind: ToolKindPlugin, Category: CategorySession},
	}
}

func (sessionPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	t := []fantasy.AgentTool{
		agenttools.NewTodosTool(deps.Sessions),
		agenttools.NewSessionExportTool(deps.Messages, deps.Cfg.WorkingDir()),
		agenttools.NewMessageExportTool(deps.Messages, deps.Cfg.WorkingDir()),
		agenttools.NewSessionSummaryTool(deps.Sessions, deps.Messages, deps.Cfg.WorkingDir(), agenttools.SessionSummaryScheduler(deps.SummarySchedule)),
	}
	if deps.SessionSearch != nil {
		t = append(t, agenttools.NewSessionSearchTool(deps.SessionSearch))
	}
	return t
}

// ─── plugin/mocode ────────────────────────────────────────────────────────────

type mocodePlugin struct{}

func (mocodePlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: agenttools.MocodeInfoToolName, Kind: ToolKindPlugin, Category: CategoryMocode},
		{Name: agenttools.MocodeLogsToolName, Kind: ToolKindPlugin, Category: CategoryMocode},
	}
}

func (mocodePlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{
		agenttools.NewMocodeInfoTool(deps.Cfg, deps.LSPManager, deps.AllSkills, deps.ActiveSkills, deps.SkillTracker),
		agenttools.NewMocodeLogsTool(log.MainLogPath(infra.DataDir())),
	}
}

// ─── plugin/lsp ───────────────────────────────────────────────────────────────

type lspPlugin struct{}

func (lspPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: agenttools.DiagnosticsToolName, Kind: ToolKindPlugin, Category: CategoryLSP},
		{Name: lsp.ReferencesToolName, Kind: ToolKindPlugin, Category: CategoryLSP},
		{Name: lsp.LSPRestartToolName, Kind: ToolKindPlugin, Category: CategoryLSP},
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
		agenttools.NewDiagnosticsTool(deps.LSPManager),
		lsp.NewReferencesTool(deps.LSPManager),
		lsp.NewLSPRestartTool(deps.LSPManager),
	}
}

// ─── plugin/mcp_meta ──────────────────────────────────────────────────────────

type mcpMetaPlugin struct{}

func (mcpMetaPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: mcp.ListMCPResourcesToolName, Kind: ToolKindPlugin, Category: CategoryMCPMeta},
		{Name: mcp.ReadMCPResourceToolName, Kind: ToolKindPlugin, Category: CategoryMCPMeta},
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
		{Name: agenttools.ThinkToolName, Kind: ToolKindPlugin, Category: CategoryReasoning},
	}
}

func (thinkPlugin) Build(_ context.Context, _ ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{agenttools.NewThinkTool()}
}

// ─── plugin/learn ─────────────────────────────────────────────────────────────

// learnPlugin exposes the learn tool, which turns a finished task into a
// durable skill under the highest-priority user skills directory. Provenance
// metadata (origin/revision/timestamps) is stamped into the frontmatter so
// agent-authored skills stay distinguishable from hand-written and bundled
// ones — the precondition for ever curating or archiving them automatically.
//
// Categorised as memory because learn sits at the session→memory→skills
// junction of the dependency DAG (see docs/architecture/00-hermes-blueprint.md
// §7, block B10): it is what carries knowledge out of a session and into the
// skill library.
type learnPlugin struct{}

func (learnPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: agenttools.LearnToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
	}
}

func (learnPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{agenttools.NewLearnTool(agenttools.LearnDeps{
		Permissions: deps.Permissions,
		Root:        learnSkillsRoot(deps),
		Known:       deps.AllSkills,
	})}
}

// learnSkillsRoot resolves where learned skills are written: the highest
// priority user skills path. That directory is already read by skill discovery,
// so a learned skill is picked up on the next session with no extra
// configuration, and `options.skills_paths` remains the single knob for
// redirecting it (to a project-scoped or shared directory, for example).
//
// Path expansion mirrors discoverSkills so the write target and the read target
// are always the same directory.
func learnSkillsRoot(deps ToolDeps) string {
	if deps.Cfg == nil {
		return ""
	}
	cfg := deps.Cfg.Config()
	if cfg == nil || cfg.Options == nil || len(cfg.Options.SkillsPaths) == 0 {
		return ""
	}

	root := infra.Long(cfg.Options.SkillsPaths[0])
	if strings.HasPrefix(root, "$") {
		if resolved, err := deps.Cfg.Resolver().ResolveValue(root); err == nil {
			root = resolved
		}
	}
	return root
}

// ─── plugin/browser ────────────────────────────────────────────────────────────

// browserPlugin exposes the native browser runtime: it drives an already
// installed Chromium-family browser over CDP so the agent can inspect and debug
// real pages. No Node/Playwright toolchain is involved — see
// docs/architecture/07-native-browser-runtime.md.
//
// The session lives in one long-lived Manager rather than per call, because a
// debugging loop (snapshot → click → snapshot → console) must stay on the same
// page across calls.
type browserPlugin struct{}

func (browserPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: browser.BrowserToolName, Kind: ToolKindPlugin, Category: CategoryBrowser},
	}
}

func (browserPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	return []fantasy.AgentTool{browser.NewBrowserTool(browser.Deps{
		Permissions: deps.Permissions,
		Manager:     browserManager(),
	})}
}

func (browserPlugin) Start(context.Context) error { return nil }

func (browserPlugin) Stop(ctx context.Context) error {
	// A headless browser left running after mocode exits is a leaked process, so
	// the session is torn down with the tool registry.
	return browserManager().Close(ctx)
}

// browserManager returns the process-wide browser session manager.
func browserManager() *corebrowser.Manager {
	browserManagerOnce.Do(func() { browserManagerInst = corebrowser.NewManager() })
	return browserManagerInst
}

var (
	browserManagerOnce sync.Once
	browserManagerInst *corebrowser.Manager
)

// ─── plugin/memory ─────────────────────────────────────────────────────────────

// memoryPlugin exposes the session memory tool.
//
// The store lives in one long-lived value rather than being rebuilt per call,
// because the tier an entry lands in depends on the session it belongs to, and
// the index has to reflect every session's writes — not just this call's.
type memoryPlugin struct{}

func (memoryPlugin) Descriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{Name: agenttools.MemoryToolName, Kind: ToolKindPlugin, Category: CategoryMemory},
	}
}

func (memoryPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
	store := memoryStore(deps)
	if store == nil {
		return nil
	}
	return []fantasy.AgentTool{agenttools.NewMemoryTool(agenttools.MemoryDeps{
		Store:   store,
		Indexer: memory.NewFileIndexer(store),
	})}
}

// memoryStore returns the process-wide session memory store.
//
// A nil store means memory is unavailable (no config), in which case the tool is
// simply absent rather than present and failing.
func memoryStore(deps ToolDeps) *memory.Store {
	memoryStoreOnce.Do(func() {
		root := memoryRoot(deps)
		if root == "" {
			return
		}
		memoryStoreInst = memory.NewStore(root)
	})
	return memoryStoreInst
}

// memoryRoot resolves the memory tree root: the configured directory, else
// <data dir>/memories.
//
// A single root rather than a search path: memory has a structured layout
// (global/, sessions/, index.md) that the code owns and the index depends on,
// so multiple roots would fracture the index rather than extend it.
func memoryRoot(deps ToolDeps) string {
	configured := ""
	if deps.Cfg != nil {
		if cfg := deps.Cfg.Config(); cfg != nil && cfg.Options != nil {
			configured = cfg.Options.MemoryDir
		}
	}
	return memory.ResolveRoot(configured, infra.DataDir(), func(p string) string {
		return expandOne(deps, p)
	})
}

var (
	memoryStoreOnce sync.Once
	memoryStoreInst *memory.Store
)

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
	once sync.Once
	svc  *sshcommon.Service
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
	// sync.Once: Build can be called from concurrent agent builds
	// (mode switches, sub-agents); the old nil-check was a data race.
	p.once.Do(func() {
		p.svc = sshcommon.NewService()
	})
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
		{Name: web.AgenticFetchToolName, Kind: ToolKindPlugin, Category: CategoryNetwork},
		{Name: agenttools.TransferToolName, Kind: ToolKindPlugin, Category: CategorySession},
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
