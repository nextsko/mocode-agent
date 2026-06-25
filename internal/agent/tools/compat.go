// Package tools re-exports constants and type aliases from the tool
// sub-packages so that callers (UI, agent, config) can continue using the
// tools.XxxToolName / tools.XxxParams surface unchanged.
package tools

import (
	"github.com/package-register/mocode/internal/agent/tools/builtin/bash"
	"github.com/package-register/mocode/internal/agent/tools/builtin/edit"
	"github.com/package-register/mocode/internal/agent/tools/builtin/job_kill"
	"github.com/package-register/mocode/internal/agent/tools/builtin/job_output"
	"github.com/package-register/mocode/internal/agent/tools/builtin/ls"
	"github.com/package-register/mocode/internal/agent/tools/builtin/multiedit"
	"github.com/package-register/mocode/internal/agent/tools/builtin/read_files"
	"github.com/package-register/mocode/internal/agent/tools/builtin/view"
	"github.com/package-register/mocode/internal/agent/tools/builtin/write"
	"github.com/package-register/mocode/internal/agent/tools/filter"
	"github.com/package-register/mocode/internal/agent/tools/plugins/crawl"
	"github.com/package-register/mocode/internal/agent/tools/plugins/diagnostics"
	"github.com/package-register/mocode/internal/agent/tools/plugins/download"
	"github.com/package-register/mocode/internal/agent/tools/plugins/download_docs"
	"github.com/package-register/mocode/internal/agent/tools/plugins/fetch"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitea_issues"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitea_notifications"
	"github.com/package-register/mocode/internal/agent/tools/plugins/gitea_pulls"
	"github.com/package-register/mocode/internal/agent/tools/plugins/glob"
	"github.com/package-register/mocode/internal/agent/tools/plugins/grep"
	"github.com/package-register/mocode/internal/agent/tools/plugins/list_mcp_resources"
	"github.com/package-register/mocode/internal/agent/tools/plugins/lsp_restart"
	"github.com/package-register/mocode/internal/agent/tools/plugins/message_export"
	"github.com/package-register/mocode/internal/agent/tools/plugins/mocode_info"
	"github.com/package-register/mocode/internal/agent/tools/plugins/mocode_logs"
	"github.com/package-register/mocode/internal/agent/tools/plugins/netcommon"
	"github.com/package-register/mocode/internal/agent/tools/plugins/read_mcp_resource"
	"github.com/package-register/mocode/internal/agent/tools/plugins/references"
	"github.com/package-register/mocode/internal/agent/tools/plugins/screenshot_to_wechat"
	"github.com/package-register/mocode/internal/agent/tools/plugins/send_wechat_file"
	"github.com/package-register/mocode/internal/agent/tools/plugins/send_wechat_image"
	"github.com/package-register/mocode/internal/agent/tools/plugins/session_export"
	"github.com/package-register/mocode/internal/agent/tools/plugins/session_summary"
	"github.com/package-register/mocode/internal/agent/tools/plugins/sourcegraph"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh_exec"
	"github.com/package-register/mocode/internal/agent/tools/plugins/sshcommon"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh_download"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh_list_hosts"
	"github.com/package-register/mocode/internal/agent/tools/plugins/ssh_upload"
	"github.com/package-register/mocode/internal/agent/tools/plugins/think"
	"github.com/package-register/mocode/internal/agent/tools/plugins/todos"
	"github.com/package-register/mocode/internal/agent/tools/plugins/web_fetch"
	"github.com/package-register/mocode/internal/agent/tools/plugins/web_search"
)

// ─── builtin/bash ─────────────────────────────────────────────────────────────

const (
	BashToolName      = bash.BashToolName
	JobOutputToolName = job_output.JobOutputToolName
	JobKillToolName   = job_kill.JobKillToolName
	BashNoOutput      = bash.BashNoOutput
)

type (
	BashParams                = bash.BashParams
	BashPermissionsParams     = bash.BashPermissionsParams
	BashResponseMetadata      = bash.BashResponseMetadata
	JobOutputParams           = job_output.JobOutputParams
	JobOutputResponseMetadata = job_output.JobOutputResponseMetadata
	JobKillParams             = job_kill.JobKillParams
	JobKillResponseMetadata   = job_kill.JobKillResponseMetadata
)

// ─── builtin/file tools ───────────────────────────────────────────────────────

const (
	EditToolName      = edit.EditToolName
	MultiEditToolName = multiedit.MultiEditToolName
	ViewToolName      = view.ViewToolName
	WriteToolName     = write.WriteToolName
	LSToolName        = ls.LSToolName
	ReadFilesToolName = read_files.ReadFilesToolName
	ViewResourceSkill = view.ViewResourceSkill
)

type (
	EditParams                 = edit.EditParams
	EditPermissionsParams      = edit.EditPermissionsParams
	EditResponseMetadata       = edit.EditResponseMetadata
	MultiEditOperation         = multiedit.MultiEditOperation
	MultiEditParams            = multiedit.MultiEditParams
	MultiEditPermissionsParams = multiedit.MultiEditPermissionsParams
	MultiEditResponseMetadata  = multiedit.MultiEditResponseMetadata
	FailedEdit                 = multiedit.FailedEdit
	ViewParams                 = view.ViewParams
	ViewPermissionsParams      = view.ViewPermissionsParams
	ViewResourceType           = view.ViewResourceType
	ViewResponseMetadata       = view.ViewResponseMetadata
	WriteParams                = write.WriteParams
	WritePermissionsParams     = write.WritePermissionsParams
	WriteResponseMetadata      = write.WriteResponseMetadata
	LSParams                   = ls.LSParams
	LSPermissionsParams        = ls.LSPermissionsParams
	TreeNode                   = ls.TreeNode
	ReadFilesParams            = read_files.ReadFilesParams
	ReadFilesPermissionsParams = read_files.ReadFilesPermissionsParams
	ReadFilesResult            = read_files.ReadFilesResult
	ReadFilesResponseMetadata  = read_files.ReadFilesResponseMetadata
)

// ─── plugins/search ───────────────────────────────────────────────────────────

const (
	GrepToolName        = grep.GrepToolName
	GlobToolName        = glob.GlobToolName
	SourcegraphToolName = sourcegraph.SourcegraphToolName
)

type (
	GrepParams                  = grep.GrepParams
	GrepMatch                   = grep.GrepMatch
	GrepResponseMetadata        = grep.GrepResponseMetadata
	GlobParams                  = glob.GlobParams
	GlobResponseMetadata        = glob.GlobResponseMetadata
	SourcegraphParams           = sourcegraph.SourcegraphParams
	SourcegraphResponseMetadata = sourcegraph.SourcegraphResponseMetadata
)

// ─── plugins/network ──────────────────────────────────────────────────────────

const (
	FetchToolName        = fetch.FetchToolName
	CrawlToolName        = crawl.CrawlToolName
	DownloadToolName     = download.DownloadToolName
	DownloadDocsToolName = download_docs.DownloadDocsToolName
)

const (
	WebFetchToolName      = netcommon.WebFetchToolName
	WebSearchToolName     = netcommon.WebSearchToolName
	LargeContentThreshold = netcommon.LargeContentThreshold
)

type (
	FetchParams               = fetch.FetchParams
	FetchPermissionsParams    = fetch.FetchPermissionsParams
	WebFetchParams            = netcommon.WebFetchParams
	WebSearchParams           = netcommon.WebSearchParams
	CrawlParams               = crawl.CrawlParams
	DownloadParams            = download.DownloadParams
	DownloadPermissionsParams = download.DownloadPermissionsParams
	DownloadDocsParams        = download_docs.DownloadDocsParams
)

// Function re-exports — agent-level callers keep using tools.New* unchanged.
var (
	NewFetchTool        = fetch.NewFetchTool
	NewCrawlTool        = crawl.NewCrawlTool
	NewDownloadTool     = download.NewDownloadTool
	NewDownloadDocsTool = download_docs.NewDownloadDocsTool
	NewWebFetchTool     = web_fetch.NewWebFetchTool
	NewWebSearchTool    = web_search.NewWebSearchTool
	FetchURLAndConvert  = netcommon.FetchURLAndConvert
	NewGlobTool         = glob.NewGlobTool
	NewGrepTool         = grep.NewGrepTool
	NewSourcegraphTool  = sourcegraph.NewSourcegraphTool
	ResetCache          = grep.ResetCache
	NewEditTool         = edit.NewEditTool
	NewMultiEditTool    = multiedit.NewMultiEditTool
	NewViewTool         = view.NewViewTool
	NewWriteTool        = write.NewWriteTool
	NewLsTool           = ls.NewLsTool
	NewReadFilesTool    = read_files.NewReadFilesTool
	NewBashTool         = bash.NewBashTool
	NewJobOutputTool    = job_output.NewJobOutputTool
	NewJobKillTool      = job_kill.NewJobKillTool
)

// ─── plugins/lsp ──────────────────────────────────────────────────────────────

const (
	DiagnosticsToolName = diagnostics.DiagnosticsToolName
	ReferencesToolName  = references.ReferencesToolName
	LSPRestartToolName  = lsp_restart.LSPRestartToolName
)

type (
	DiagnosticsParams = diagnostics.DiagnosticsParams
	ReferencesParams  = references.ReferencesParams
	LSPRestartParams  = lsp_restart.LSPRestartParams
)

// ─── plugins/mocode ───────────────────────────────────────────────────────────

const (
	MocodeInfoToolName = mocode_info.MocodeInfoToolName
	MocodeLogsToolName = mocode_logs.MocodeLogsToolName
)

type (
	MocodeInfoParams = mocode_info.MocodeInfoParams
	MocodeLogsParams = mocode_logs.MocodeLogsParams
)

// ─── plugins/session ──────────────────────────────────────────────────────────

const (
	TodosToolName          = todos.TodosToolName
	SessionExportToolName  = session_export.SessionExportToolName
	MessageExportToolName  = message_export.MessageExportToolName
	SessionSummaryToolName = session_summary.SessionSummaryToolName
)

type (
	TodosParams             = todos.TodosParams
	TodoItem                = todos.TodoItem
	TodosResponseMetadata   = todos.TodosResponseMetadata
	SessionExportParams     = session_export.SessionExportParams
	MessageExportParams     = message_export.MessageExportParams
	SessionSummaryParams    = session_summary.SessionSummaryParams
	SessionSummaryScheduler = session_summary.SessionSummaryScheduler
	SessionSummaryMetadata  = session_summary.SessionSummaryMetadata
)

// ─── plugins/mcp ──────────────────────────────────────────────────────────────

const (
	ListMCPResourcesToolName = list_mcp_resources.ListMCPResourcesToolName
	ReadMCPResourceToolName  = read_mcp_resource.ReadMCPResourceToolName
)

type (
	ListMCPResourcesParams            = list_mcp_resources.ListMCPResourcesParams
	ListMCPResourcesPermissionsParams = list_mcp_resources.ListMCPResourcesPermissionsParams
	ReadMCPResourceParams             = read_mcp_resource.ReadMCPResourceParams
	ReadMCPResourcePermissionsParams  = read_mcp_resource.ReadMCPResourcePermissionsParams
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
	GiteaIssuesToolName        = gitea_issues.IssuesToolName
	GiteaPullsToolName         = gitea_pulls.PullsToolName
	GiteaNotificationsToolName = gitea_notifications.NotificationsToolName
)

type (
	GiteaIssuesParams        = gitea_issues.IssuesParams
	GiteaPullsParams         = gitea_pulls.PullsParams
	GiteaNotificationsParams = gitea_notifications.NotificationsParams
)

var (
	NewGiteaIssuesTool        = gitea_issues.NewIssuesTool
	NewGiteaPullsTool         = gitea_pulls.NewPullsTool
	NewGiteaNotificationsTool = gitea_notifications.NewNotificationsTool
)

// ─── plugins/ssh ──────────────────────────────────────────────────────────────

const (
	SshExecToolName      = sshcommon.SshExecToolName
	SshUploadToolName    = sshcommon.SshUploadToolName
	SshDownloadToolName  = sshcommon.SshDownloadToolName
	SshListHostsToolName = sshcommon.SshListHostsToolName
)

type (
	SshExecParams                = ssh_exec.SshExecParams
	SshExecPermissionsParams     = ssh_exec.SshExecPermissionsParams
	SshExecResponseMetadata      = ssh_exec.SshExecResponseMetadata
	SshUploadParams              = ssh_upload.SshUploadParams
	SshUploadPermissionsParams   = ssh_upload.SshUploadPermissionsParams
	SshUploadResponseMetadata    = ssh_upload.SshUploadResponseMetadata
	SshDownloadParams            = ssh_download.SshDownloadParams
	SshDownloadPermissionsParams = ssh_download.SshDownloadPermissionsParams
	SshDownloadResponseMetadata  = ssh_download.SshDownloadResponseMetadata
	SshListHostsParams           = ssh_list_hosts.SshListHostsParams
	SshListHostsResponseMetadata = ssh_list_hosts.SshListHostsResponseMetadata
	HostInfo                     = ssh_list_hosts.HostInfo
)

var (
	NewSshExecTool      = ssh_exec.NewSshExecTool
	NewSshUploadTool    = ssh_upload.NewSshUploadTool
	NewSshDownloadTool  = ssh_download.NewSshDownloadTool
	NewSshListHostsTool = ssh_list_hosts.NewSshListHostsTool
	NewSSHService       = sshcommon.NewService
)

// ─── filter ───────────────────────────────────────────────────────────────────

type FilterFunc = filter.FilterFunc

var (
	FilterApply          = filter.Apply
	FilterChain          = filter.Chain
	FilterIncludeNames   = filter.IncludeNames
	FilterExcludeNames   = filter.ExcludeNames
)

// ─── plugins/wechat ───────────────────────────────────────────────────────────

const (
	WeChatSendImageToolName  = send_wechat_image.WeChatSendImageToolName
	WeChatSendFileToolName   = send_wechat_file.WeChatSendFileToolName
	WeChatScreenshotToolName = screenshot_to_wechat.WeChatScreenshotToolName
)

var (
	NewWeChatSendImageTool  = send_wechat_image.NewWeChatSendImageTool
	NewWeChatSendFileTool   = send_wechat_file.NewWeChatSendFileTool
	NewWeChatScreenshotTool = screenshot_to_wechat.NewWeChatScreenshotTool
)
