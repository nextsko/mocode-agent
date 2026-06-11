// Package tools re-exports constants and type aliases from the tool
// sub-packages so that callers (UI, agent, config) can continue using the
// tools.XxxToolName / tools.XxxParams surface unchanged.
package tools

import (
	"github.com/package-register/mocode/internal/agent/tools/builtin/exec"
	"github.com/package-register/mocode/internal/agent/tools/builtin/file"
	"github.com/package-register/mocode/internal/agent/tools/filter"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitea"
	lsptool "github.com/package-register/mocode/internal/agent/tools/plugins/lsp"
	"github.com/package-register/mocode/internal/agent/tools/plugins/mcp"
	"github.com/package-register/mocode/internal/agent/tools/plugins/mocode"
	"github.com/package-register/mocode/internal/agent/tools/plugins/network"
	"github.com/package-register/mocode/internal/agent/tools/plugins/search"
	sessiontool "github.com/package-register/mocode/internal/agent/tools/plugins/session"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh"
	"github.com/package-register/mocode/internal/agent/tools/plugins/think"
)

// ─── builtin/exec ─────────────────────────────────────────────────────────────

const (
	BashToolName      = exec.BashToolName
	JobOutputToolName = exec.JobOutputToolName
	JobKillToolName   = exec.JobKillToolName
	BashNoOutput      = exec.BashNoOutput
)

type (
	BashParams                = exec.BashParams
	BashPermissionsParams     = exec.BashPermissionsParams
	BashResponseMetadata      = exec.BashResponseMetadata
	JobOutputParams           = exec.JobOutputParams
	JobOutputResponseMetadata = exec.JobOutputResponseMetadata
	JobKillParams             = exec.JobKillParams
	JobKillResponseMetadata   = exec.JobKillResponseMetadata
)

// ─── builtin/file ─────────────────────────────────────────────────────────────

const (
	EditToolName      = file.EditToolName
	MultiEditToolName = file.MultiEditToolName
	ViewToolName      = file.ViewToolName
	WriteToolName     = file.WriteToolName
	LSToolName        = file.LSToolName
	ReadFilesToolName = file.ReadFilesToolName
	ViewResourceSkill = file.ViewResourceSkill
)

type (
	EditParams                 = file.EditParams
	EditPermissionsParams      = file.EditPermissionsParams
	EditResponseMetadata       = file.EditResponseMetadata
	MultiEditOperation         = file.MultiEditOperation
	MultiEditParams            = file.MultiEditParams
	MultiEditPermissionsParams = file.MultiEditPermissionsParams
	MultiEditResponseMetadata  = file.MultiEditResponseMetadata
	FailedEdit                 = file.FailedEdit
	ViewParams                 = file.ViewParams
	ViewPermissionsParams      = file.ViewPermissionsParams
	ViewResourceType           = file.ViewResourceType
	ViewResponseMetadata       = file.ViewResponseMetadata
	WriteParams                = file.WriteParams
	WritePermissionsParams     = file.WritePermissionsParams
	WriteResponseMetadata      = file.WriteResponseMetadata
	LSParams                   = file.LSParams
	LSPermissionsParams        = file.LSPermissionsParams
	TreeNode                   = file.TreeNode
	ReadFilesParams            = file.ReadFilesParams
	ReadFilesPermissionsParams = file.ReadFilesPermissionsParams
	ReadFilesResult            = file.ReadFilesResult
	ReadFilesResponseMetadata  = file.ReadFilesResponseMetadata
)

// ─── plugins/search ───────────────────────────────────────────────────────────

const (
	GrepToolName        = search.GrepToolName
	GlobToolName        = search.GlobToolName
	SourcegraphToolName = search.SourcegraphToolName
)

type (
	GrepParams                  = search.GrepParams
	GrepMatch                   = search.GrepMatch
	GrepResponseMetadata        = search.GrepResponseMetadata
	GlobParams                  = search.GlobParams
	GlobResponseMetadata        = search.GlobResponseMetadata
	SourcegraphParams           = search.SourcegraphParams
	SourcegraphResponseMetadata = search.SourcegraphResponseMetadata
)

// ─── plugins/network ──────────────────────────────────────────────────────────

const (
	FetchToolName        = network.FetchToolName
	CrawlToolName        = network.CrawlToolName
	DownloadToolName     = network.DownloadToolName
	DownloadDocsToolName = network.DownloadDocsToolName
)

const (
	WebFetchToolName      = network.WebFetchToolName
	WebSearchToolName     = network.WebSearchToolName
	LargeContentThreshold = network.LargeContentThreshold
)

type (
	FetchParams               = network.FetchParams
	FetchPermissionsParams    = network.FetchPermissionsParams
	WebFetchParams            = network.WebFetchParams
	WebSearchParams           = network.WebSearchParams
	CrawlParams               = network.CrawlParams
	DownloadParams            = network.DownloadParams
	DownloadPermissionsParams = network.DownloadPermissionsParams
	DownloadDocsParams        = network.DownloadDocsParams
)

// Function re-exports — agent-level callers keep using tools.New* unchanged.
var (
	// network
	NewFetchTool        = network.NewFetchTool
	NewCrawlTool        = network.NewCrawlTool
	NewDownloadTool     = network.NewDownloadTool
	NewDownloadDocsTool = network.NewDownloadDocsTool
	NewWebFetchTool     = network.NewWebFetchTool
	NewWebSearchTool    = network.NewWebSearchTool
	FetchURLAndConvert  = network.FetchURLAndConvert
	// search
	NewGlobTool        = search.NewGlobTool
	NewGrepTool        = search.NewGrepTool
	NewSourcegraphTool = search.NewSourcegraphTool
	ResetCache         = search.ResetCache
	// file
	NewEditTool      = file.NewEditTool
	NewMultiEditTool = file.NewMultiEditTool
	NewViewTool      = file.NewViewTool
	NewWriteTool     = file.NewWriteTool
	NewLsTool        = file.NewLsTool
	NewReadFilesTool = file.NewReadFilesTool
	// exec
	NewBashTool      = exec.NewBashTool
	NewJobOutputTool = exec.NewJobOutputTool
	NewJobKillTool   = exec.NewJobKillTool
)

// ─── plugins/lsp ──────────────────────────────────────────────────────────────

const (
	DiagnosticsToolName = lsptool.DiagnosticsToolName
	ReferencesToolName  = lsptool.ReferencesToolName
	LSPRestartToolName  = lsptool.LSPRestartToolName
)

type (
	DiagnosticsParams = lsptool.DiagnosticsParams
	ReferencesParams  = lsptool.ReferencesParams
	LSPRestartParams  = lsptool.LSPRestartParams
)

// ─── plugins/mocode ───────────────────────────────────────────────────────────

const (
	MocodeInfoToolName = mocode.MocodeInfoToolName
	MocodeLogsToolName = mocode.MocodeLogsToolName
)

type (
	MocodeInfoParams = mocode.MocodeInfoParams
	MocodeLogsParams = mocode.MocodeLogsParams
)

// ─── plugins/session ──────────────────────────────────────────────────────────

const (
	TodosToolName          = sessiontool.TodosToolName
	SessionExportToolName  = sessiontool.SessionExportToolName
	MessageExportToolName  = sessiontool.MessageExportToolName
	SessionSummaryToolName = sessiontool.SessionSummaryToolName
)

type (
	TodosParams             = sessiontool.TodosParams
	TodoItem                = sessiontool.TodoItem
	TodosResponseMetadata   = sessiontool.TodosResponseMetadata
	SessionExportParams     = sessiontool.SessionExportParams
	MessageExportParams     = sessiontool.MessageExportParams
	SessionSummaryParams    = sessiontool.SessionSummaryParams
	SessionSummaryScheduler = sessiontool.SessionSummaryScheduler
	SessionSummaryMetadata  = sessiontool.SessionSummaryMetadata
)

// ─── plugins/mcp ──────────────────────────────────────────────────────────────

const (
	ListMCPResourcesToolName = mcp.ListMCPResourcesToolName
	ReadMCPResourceToolName  = mcp.ReadMCPResourceToolName
)

type (
	ListMCPResourcesParams            = mcp.ListMCPResourcesParams
	ListMCPResourcesPermissionsParams = mcp.ListMCPResourcesPermissionsParams
	ReadMCPResourceParams             = mcp.ReadMCPResourceParams
	ReadMCPResourcePermissionsParams  = mcp.ReadMCPResourcePermissionsParams
)

// ─── plugins/think ────────────────────────────────────────────────────────────

const ThinkToolName = think.ThinkToolName

type (
	ThinkParams           = think.ThinkParams
	ThinkResponseMetadata = think.ThinkResponseMetadata
)

var NewThinkTool = think.NewThinkTool

// ─── plugins/gitea ────────────────────────────────────────────────────────────

const (
	GiteaIssuesToolName        = gitea.IssuesToolName
	GiteaPullsToolName         = gitea.PullsToolName
	GiteaNotificationsToolName = gitea.NotificationsToolName
)

type (
	GiteaIssuesParams        = gitea.IssuesParams
	GiteaPullsParams         = gitea.PullsParams
	GiteaNotificationsParams = gitea.NotificationsParams
)

var (
	NewGiteaIssuesTool        = gitea.NewIssuesTool
	NewGiteaPullsTool         = gitea.NewPullsTool
	NewGiteaNotificationsTool = gitea.NewNotificationsTool
)

// ─── plugins/ssh ────────────────────────────────────────────────────────────

const (
	SshExecToolName      = ssh.SshExecToolName
	SshUploadToolName    = ssh.SshUploadToolName
	SshDownloadToolName  = ssh.SshDownloadToolName
	SshListHostsToolName = ssh.SshListHostsToolName
)

type (
	SshExecParams                = ssh.SshExecParams
	SshExecPermissionsParams     = ssh.SshExecPermissionsParams
	SshExecResponseMetadata      = ssh.SshExecResponseMetadata
	SshUploadParams              = ssh.SshUploadParams
	SshUploadPermissionsParams   = ssh.SshUploadPermissionsParams
	SshUploadResponseMetadata    = ssh.SshUploadResponseMetadata
	SshDownloadParams            = ssh.SshDownloadParams
	SshDownloadPermissionsParams = ssh.SshDownloadPermissionsParams
	SshDownloadResponseMetadata  = ssh.SshDownloadResponseMetadata
	SshListHostsParams           = ssh.SshListHostsParams
	SshListHostsResponseMetadata = ssh.SshListHostsResponseMetadata
	HostInfo                     = ssh.HostInfo
)

var (
	NewSshExecTool      = ssh.NewSshExecTool
	NewSshUploadTool    = ssh.NewSshUploadTool
	NewSshDownloadTool  = ssh.NewSshDownloadTool
	NewSshListHostsTool = ssh.NewSshListHostsTool
	NewSSHService       = ssh.NewService
)

// ─── filter ───────────────────────────────────────────────────────────────────

// FilterFunc is re-exported so callers can write tools.FilterFunc without
// importing the filter sub-package directly.
type FilterFunc = filter.FilterFunc

var (
	// FilterApply applies FilterFuncs to a tool list.
	FilterApply = filter.Apply
	// FilterChain combines multiple FilterFuncs with AND semantics.
	FilterChain = filter.Chain
	// FilterIncludeNames keeps only tools whose name is in the allow-list.
	FilterIncludeNames = filter.IncludeNames
	// FilterExcludeNames drops any tool whose name is in the block-list.
	FilterExcludeNames = filter.ExcludeNames
)
