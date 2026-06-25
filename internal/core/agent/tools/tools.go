// Package tools is the public façade for the agent tool system.  All context
// keys, helpers, and types are delegated to the internal/shared sub-package so
// that sub-packages (builtin, plugins) read and write the SAME context keys.
package tools

import (
	"context"

	"charm.land/fantasy"

	"github.com/package-register/mocode/internal/core/agent/toolutil"
)

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
