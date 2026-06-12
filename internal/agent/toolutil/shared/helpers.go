// Package shared provides low-level utility helpers shared across all tool
// sub-packages.  It must not import any package from the tools tree to avoid
// import cycles.
package shared

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"charm.land/fantasy"
)

type (
	SessionIDContextKey string
	MessageIDContextKey string
	SupportsImagesKey   string
	ModelNameKey        string
)

const (
	// SessionIDContextKeyVal is the context key for the session ID.
	SessionIDContextKeyVal SessionIDContextKey = "session_id"
	// MessageIDContextKeyVal is the context key for the message ID.
	MessageIDContextKeyVal MessageIDContextKey = "message_id"
	// SupportsImagesContextKeyVal is the context key for the image support flag.
	SupportsImagesContextKeyVal SupportsImagesKey = "supports_images"
	// ModelNameContextKeyVal is the context key for the model name.
	ModelNameContextKeyVal ModelNameKey = "model_name"
)

// GetContextValue is a generic helper that retrieves a typed value from context.
func GetContextValue[T any](ctx context.Context, key any, defaultValue T) T {
	value := ctx.Value(key)
	if value == nil {
		return defaultValue
	}
	if typedValue, ok := value.(T); ok {
		return typedValue
	}
	return defaultValue
}

// GetSessionFromContext retrieves the session ID from the context.
func GetSessionFromContext(ctx context.Context) string {
	return GetContextValue(ctx, SessionIDContextKeyVal, "")
}

// GetMessageFromContext retrieves the message ID from the context.
func GetMessageFromContext(ctx context.Context) string {
	return GetContextValue(ctx, MessageIDContextKeyVal, "")
}

// GetSupportsImagesFromContext retrieves whether the model supports images.
func GetSupportsImagesFromContext(ctx context.Context) bool {
	return GetContextValue(ctx, SupportsImagesContextKeyVal, false)
}

// GetModelNameFromContext retrieves the model name from the context.
func GetModelNameFromContext(ctx context.Context) string {
	return GetContextValue(ctx, ModelNameContextKeyVal, "")
}

// NewPermissionDeniedResponse returns a tool response for a permission denial.
func NewPermissionDeniedResponse() fantasy.ToolResponse {
	resp := fantasy.NewTextErrorResponse("User denied permission")
	resp.StopTurn = true
	return resp
}

// FirstLineDescription returns the first non-empty line from the embedded
// markdown description.
func FirstLineDescription(content []byte) string {
	if !testing.Testing() {
		if v, err := strconv.ParseBool(os.Getenv("MOCODE_SHORT_TOOL_DESCRIPTIONS")); err == nil && !v {
			return strings.TrimSpace(string(content))
		}
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
