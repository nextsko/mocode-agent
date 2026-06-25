package toolutil

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

// SessionLoggerKey carries an optional SessionLoggerSink into agent callbacks.
type SessionLoggerKey string

// SessionLoggerContextKeyVal is the context key for the session logger sink.
const SessionLoggerContextKeyVal SessionLoggerKey = "session_logger"

// SessionLoggerSink is the minimal logging surface an agent callback can use
// to record tool calls, reasoning traces, and errors. *sessionlog.Logger
// satisfies it. Declared here so the tool layer can carry a logger through
// context without importing the sessionlog package (avoids a cycle).
type SessionLoggerSink interface {
	LogToolCall(event, data string, meta SessionLogMeta)
	LogThink(event, data string, meta SessionLogMeta)
	LogBug(event, data string, meta SessionLogMeta)
	LogInfo(event, data string, meta SessionLogMeta)
}

// SessionLogMeta mirrors sessionlog.Meta. Duplicated here to keep the tool
// layer free of a sessionlog import; the coordinator adapts between the two.
type SessionLogMeta struct {
	ToolName   string
	ToolCallID string
	AgentID    string
	DurationMs int64
	ErrorType  string
}

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

// GetSessionLogger retrieves the optional session logger sink from context.
// Returns nil when no logger is attached (callers must nil-check).
func GetSessionLogger(ctx context.Context) SessionLoggerSink {
	return GetContextValue[SessionLoggerSink](ctx, SessionLoggerContextKeyVal, nil)
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
