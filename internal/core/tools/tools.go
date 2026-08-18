// Package tools is the public façade for the agent tool system.
//
// Since the three-directory hard restructure (core/ internalx/ external/ —
// see doc.go) the implementations live in subpackages; this façade keeps the
// historical tools.X import paths stable for consumers in core/agent, ui and
// transport. Alias rules: types use `type X = pkg.X` (identity preserved for
// type switches), consts and constructors use plain alias declarations.
package tools

import (
	"context"

	"charm.land/fantasy"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	fs "github.com/nextsko/mocode-agent/internal/core/tools/core/fs"
	shell "github.com/nextsko/mocode-agent/internal/core/tools/core/shell"
	web "github.com/nextsko/mocode-agent/internal/core/tools/core/web"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/mcp"
	"github.com/nextsko/mocode-agent/internal/core/tools/external/plugins/netcommon"
	gitops "github.com/nextsko/mocode-agent/internal/core/tools/external/systems/gitops"
	wechat "github.com/nextsko/mocode-agent/internal/core/tools/external/systems/wechat"
	agenttools "github.com/nextsko/mocode-agent/internal/core/tools/internalx/agent"
	"github.com/nextsko/mocode-agent/internal/core/tools/internalx/lsp"
)

// ---------------------------------------------------------------------------
// core/shell — bash and background jobs

const (
	BashToolName      = shell.BashToolName
	JobOutputToolName = shell.JobOutputToolName
	JobInputToolName  = shell.JobInputToolName
	JobKillToolName   = shell.JobKillToolName
)

var (
	BashNoOutput     = shell.BashNoOutput
	NewBashTool      = shell.NewBashTool
	NewJobOutputTool = shell.NewJobOutputTool
	NewJobInputTool  = shell.NewJobInputTool
	NewJobKillTool   = shell.NewJobKillTool
)

type (
	BashParams                = shell.BashParams
	BashPermissionsParams     = shell.BashPermissionsParams
	BashResponseMetadata      = shell.BashResponseMetadata
	JobOutputParams           = shell.JobOutputParams
	JobOutputResponseMetadata = shell.JobOutputResponseMetadata
	JobKillParams             = shell.JobKillParams
	JobKillResponseMetadata   = shell.JobKillResponseMetadata
)

// ---------------------------------------------------------------------------
// core/fs — file tools

const (
	EditToolName      = fs.EditToolName
	MultiEditToolName = fs.MultiEditToolName
	WriteToolName     = fs.WriteToolName
	ViewToolName      = fs.ViewToolName
	ReadFilesToolName = fs.ReadFilesToolName
	LSToolName        = fs.LSToolName
	GlobToolName      = fs.GlobToolName
	GrepToolName      = fs.GrepToolName
)

var (
	NewEditTool      = fs.NewEditTool
	NewMultiEditTool = fs.NewMultiEditTool
	NewWriteTool     = fs.NewWriteTool
	NewViewTool      = fs.NewViewTool
	NewReadFilesTool = fs.NewReadFilesTool
	NewLsTool        = fs.NewLsTool
	NewGlobTool      = fs.NewGlobTool
	NewGrepTool      = fs.NewGrepTool
)

type (
	EditParams            = fs.EditParams
	EditPermissionsParams = fs.EditPermissionsParams
	EditResponseMetadata  = fs.EditResponseMetadata
	MultiEditParams       = fs.MultiEditParams
	ViewParams            = fs.ViewParams
	ReadFilesParams       = fs.ReadFilesParams
	GlobParams            = fs.GlobParams
	GlobResponseMetadata  = fs.GlobResponseMetadata
	GrepParams            = fs.GrepParams
	GrepResponseMetadata  = fs.GrepResponseMetadata
	LSParams              = fs.LSParams
	LSPermissionsParams   = fs.LSPermissionsParams
)

// ---------------------------------------------------------------------------
// core/web — web tools (formerly net)

const (
	AgenticFetchToolName = web.AgenticFetchToolName
	FetchToolName        = web.FetchToolName
	CrawlToolName        = web.CrawlToolName
	DownloadToolName     = web.DownloadToolName
	DownloadDocsToolName = web.DownloadDocsToolName
	SourcegraphToolName  = web.SourcegraphToolName
)

var (
	NewFetchTool        = web.NewFetchTool
	NewCrawlTool        = web.NewCrawlTool
	NewDownloadTool     = web.NewDownloadTool
	NewDownloadDocsTool = web.NewDownloadDocsTool
)

type (
	AgenticFetchParams            = web.AgenticFetchParams
	AgenticFetchPermissionsParams = web.AgenticFetchPermissionsParams
	FetchParams                   = web.FetchParams
	FetchPermissionsParams        = web.FetchPermissionsParams
	CrawlParams                   = web.CrawlParams
	DownloadParams                = web.DownloadParams
	DownloadPermissionsParams     = web.DownloadPermissionsParams
	SourcegraphParams             = web.SourcegraphParams
)

// ---------------------------------------------------------------------------
// internalx/agent — agent-adjacent tools (think/todos/transfer/diagnostics/sessionops)

const (
	ThinkToolName          = agenttools.ThinkToolName
	TodosToolName          = agenttools.TodosToolName
	TransferToolName       = agenttools.TransferToolName
	MocodeInfoToolName     = agenttools.MocodeInfoToolName
	MocodeLogsToolName     = agenttools.MocodeLogsToolName
	DiagnosticsToolName    = agenttools.DiagnosticsToolName
	SessionExportToolName  = agenttools.SessionExportToolName
	SessionSummaryToolName = agenttools.SessionSummaryToolName
	SessionSearchToolName  = agenttools.SessionSearchToolName
	MessageExportToolName  = agenttools.MessageExportToolName
)

var (
	NewThinkTool          = agenttools.NewThinkTool
	NewTodosTool          = agenttools.NewTodosTool
	NewTransferTool       = agenttools.NewTransferTool
	NewSessionSummaryTool = agenttools.NewSessionSummaryTool
)

// ---------------------------------------------------------------------------
// internalx/lsp — LSP tools

const (
	ReferencesToolName = lsp.ReferencesToolName
	LSPRestartToolName = lsp.LSPRestartToolName
)

var (
	NewReferencesTool = lsp.NewReferencesTool
	NewLSPRestartTool = lsp.NewLSPRestartTool
)

type (
	ReferencesParams = lsp.ReferencesParams
	LSPRestartParams = lsp.LSPRestartParams
)

// ---------------------------------------------------------------------------
// external/mcp — MCP bridge

const (
	ListMCPResourcesToolName = mcp.ListMCPResourcesToolName
	ReadMCPResourceToolName  = mcp.ReadMCPResourceToolName
)

var (
	NewListMCPResourcesTool = mcp.NewListMCPResourcesTool
	NewReadMCPResourceTool  = mcp.NewReadMCPResourceTool
	GetMCPTools             = mcp.GetMCPTools
)

// MCPToolStruct is re-exported because the coordinator iterates MCP tools and
// touches its Name field (alias, NOT a new type: keep identity for any
// caller doing type switches).
type MCPToolStruct = mcp.MCPToolStruct

// ---------------------------------------------------------------------------
// external/systems — gitops and wechat

const (
	PlanCommitsToolName    = gitops.PlanCommitsToolName
	ExecuteCommitsToolName = gitops.ExecuteCommitsToolName

	WeChatSendImageToolName  = wechat.WeChatSendImageToolName
	WeChatSendFileToolName   = wechat.WeChatSendFileToolName
	WeChatScreenshotToolName = wechat.WeChatScreenshotToolName
)

var (
	NewPlanCommitsTool    = gitops.NewPlanCommitsTool
	NewExecuteCommitsTool = gitops.NewExecuteCommitsTool

	NewWeChatSendImageTool  = wechat.NewWeChatSendImageTool
	NewWeChatSendFileTool   = wechat.NewWeChatSendFileTool
	NewWeChatScreenshotTool = wechat.NewWeChatScreenshotTool
)

type (
	ExecuteCommitsParams    = gitops.ExecuteCommitsParams
	ExecuteResponseMetadata = gitops.ExecuteResponseMetadata
	PlanCommitsParams       = gitops.PlanCommitsParams
)

// ---------------------------------------------------------------------------
// external/plugins/netcommon — search providers

const (
	WebFetchToolName      = netcommon.WebFetchToolName
	WebSearchToolName     = netcommon.WebSearchToolName
	LargeContentThreshold = netcommon.LargeContentThreshold
)

var (
	NewWebFetchTool    = web.NewWebFetchTool
	NewWebSearchTool   = web.NewWebSearchTool
	FetchURLAndConvert = netcommon.FetchURLAndConvert
)

type (
	WebFetchParams  = netcommon.WebFetchParams
	WebSearchParams = netcommon.WebSearchParams
)

// Context keys – callers must use these when writing values into context so
// that all tool sub-packages can read them back correctly (delegated to
// toolutil so every sub-package shares the SAME key instances).
const (
	SessionIDContextKey = toolutil.SessionIDContextKeyVal
	MessageIDContextKey = toolutil.MessageIDContextKeyVal
	ModelNameContextKey = toolutil.ModelNameContextKeyVal
)

var NewSourcegraphTool = web.NewSourcegraphTool

// ViewResourceSkill re-export (view tool resource-kind constant).
const ViewResourceSkill = fs.ViewResourceSkill

// ResetCache clears the shared search regex caches (searchcommon-backed).
var ResetCache = fs.ResetCache

// ---------------------------------------------------------------------------
// Context helpers (delegated to toolutil so every sub-package shares keys)

// GetSessionFromContext retrieves the session ID from the context.
func GetSessionFromContext(ctx context.Context) string {
	return toolutil.GetSessionFromContext(ctx)
}

// GetMessageFromContext retrieves the message ID from the context.
func GetMessageFromContext(ctx context.Context) string {
	return toolutil.GetMessageFromContext(ctx)
}

// GetSupportsImagesFromContext retrieves whether the model supports images from the context.
func GetSupportsImagesFromContext(ctx context.Context) bool {
	return toolutil.GetSupportsImagesFromContext(ctx)
}

// GetModelNameFromContext retrieves the model name from the context.
func GetModelNameFromContext(ctx context.Context) string {
	return toolutil.GetModelNameFromContext(ctx)
}

// SupportsImagesContextKey mirrors toolutil's key for callers that write the flag.
const SupportsImagesContextKey = toolutil.SupportsImagesContextKeyVal

// NewPermissionDeniedResponse returns a tool response indicating the user
// denied permission, with StopTurn set so the agent loop does not continue.
func NewPermissionDeniedResponse() fantasy.ToolResponse {
	return toolutil.NewPermissionDeniedResponse()
}

// FirstLineDescription returns just the first non-empty line from the embedded
// markdown description. The full description can be used by setting
// MOCODE_SHORT_TOOL_DESCRIPTIONS=0.
func FirstLineDescription(content []byte) string {
	return toolutil.FirstLineDescription(content)
}

// UI-facing parameter/metadata aliases (chat renderers and permission dialogs).
type (
	MultiEditResponseMetadata  = fs.MultiEditResponseMetadata
	ViewResponseMetadata       = fs.ViewResponseMetadata
	WriteParams                = fs.WriteParams
	WritePermissionsParams     = fs.WritePermissionsParams
	DiagnosticsParams          = agenttools.DiagnosticsParams
	MultiEditPermissionsParams = fs.MultiEditPermissionsParams
	ViewPermissionsParams      = fs.ViewPermissionsParams
	TodosParams                = agenttools.TodosParams
	TodosResponseMetadata      = agenttools.TodosResponseMetadata
	ThinkParams                = agenttools.ThinkParams
	TransferParams             = agenttools.TransferParams
)
