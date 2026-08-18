// Package tools is the public façade for the agent tool system.
// All context keys, helpers, and types are delegated to the internal/shared sub-package so
// that sub-packages (builtin, plugins) read and write the SAME context keys.
package tools

import (
	"context"

	"charm.land/fantasy"
	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
	"github.com/nextsko/mocode-agent/internal/core/tools/gitops"
	"github.com/nextsko/mocode-agent/internal/core/tools/mcp"
	"github.com/nextsko/mocode-agent/internal/core/tools/net"
	"github.com/nextsko/mocode-agent/internal/core/tools/plugins/netcommon"
	"github.com/nextsko/mocode-agent/internal/core/tools/wechat"
)

// Network tool re-exports from netcommon.
const (
	WebFetchToolName      = netcommon.WebFetchToolName
	WebSearchToolName     = netcommon.WebSearchToolName
	LargeContentThreshold = netcommon.LargeContentThreshold
)

// Re-exports from the net subpackage (web tool implementations live in
// tools/net since the 0.8.x restructure; the façade keeps the historical
// tools.X import paths stable for consumers outside the tools tree).
const (
	AgenticFetchToolName = net.AgenticFetchToolName
	FetchToolName        = net.FetchToolName
	DownloadToolName     = net.DownloadToolName
	SourcegraphToolName  = net.SourcegraphToolName
)

var (
	NewSourcegraphTool = net.NewSourcegraphTool
	NewWebFetchTool    = net.NewWebFetchTool
	NewWebSearchTool   = net.NewWebSearchTool
)

// Parameter type aliases from the net subpackage (kept stable for external
// callers such as coordinator-owned tools and UI permission dialogs).
type (
	AgenticFetchParams            = net.AgenticFetchParams
	AgenticFetchPermissionsParams = net.AgenticFetchPermissionsParams
	DownloadParams                = net.DownloadParams
	DownloadPermissionsParams     = net.DownloadPermissionsParams
	FetchParams                   = net.FetchParams
	FetchPermissionsParams        = net.FetchPermissionsParams
	SourcegraphParams             = net.SourcegraphParams
)

var FetchURLAndConvert = netcommon.FetchURLAndConvert

// Network tool type aliases from netcommon.
type (
	WebFetchParams  = netcommon.WebFetchParams
	WebSearchParams = netcommon.WebSearchParams
)

// Tool type aliases - re-exported so external callers can use tools.XxxParams.

// Context key type aliases – re-exported so callers outside the tools tree
// (e.g. internal/agent/agent.go) use the exact same types as the sub-packages.
type (
	SessionIDContextKeyType   = toolutil.SessionIDContextKey
	MessageIDContextKeyType   = toolutil.MessageIDContextKey
	SupportsImagesContextType = toolutil.SupportsImagesKey
	ModelNameContextKeyType   = toolutil.ModelNameKey
)

// Context keys – callers must use these when writing values into context so
// that all tool sub-packages can read them back correctly.
var (
	SessionIDContextKey      = toolutil.SessionIDContextKeyVal
	MessageIDContextKey      = toolutil.MessageIDContextKeyVal
	SupportsImagesContextKey = toolutil.SupportsImagesContextKeyVal
	ModelNameContextKey      = toolutil.ModelNameContextKeyVal
)

// getContextValue is a generic helper that retrieves a typed value from context.
// If the value is not found or has the wrong type, it returns the default value.
func getContextValue[T any](ctx context.Context, key any, defaultValue T) T {
	return toolutil.GetContextValue[T](ctx, key, defaultValue)
}

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

// NewPermissionDeniedResponse returns a tool response indicating the user
// denied permission, with StopTurn set so the agent loop does not toolutil.
func NewPermissionDeniedResponse() fantasy.ToolResponse {
	return toolutil.NewPermissionDeniedResponse()
}

// FirstLineDescription returns just the first non-empty line from the embedded
// markdown description. The full description can be used by setting
// MOCODE_SHORT_TOOL_DESCRIPTIONS=0.
func FirstLineDescription(content []byte) string {
	return toolutil.FirstLineDescription(content)
}

// Re-exports from the gitops and wechat subpackages (moved out of the root in
// the 0.8.x restructure; alias re-exports keep the historical tools.X import
// paths stable for consumers outside the tools tree — coordinator and UI).
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

	// mcp subpackage (MCP bridge meta tools moved out of the root).
	NewListMCPResourcesTool = mcp.NewListMCPResourcesTool
	NewReadMCPResourceTool  = mcp.NewReadMCPResourceTool
	GetMCPTools             = mcp.GetMCPTools
)

// MCP bridge re-exports (meta tools live in tools/mcp).
const (
	ListMCPResourcesToolName = mcp.ListMCPResourcesToolName
	ReadMCPResourceToolName  = mcp.ReadMCPResourceToolName
)

// MCPToolStruct is re-exported because the coordinator iterates MCP tools and
// touches its Name field (alias, NOT a new type: keep identity for any
// caller doing type switches).
type MCPToolStruct = mcp.MCPToolStruct
