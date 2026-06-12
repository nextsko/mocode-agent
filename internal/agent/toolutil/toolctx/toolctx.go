// Package toolctx provides context helpers for propagating tool call metadata
// through the call stack.
//
// Inspired by ToolCallIDFromContext in trpc-agent-go/tool/context.go.
//
// The callback.wrappedTool.Run injects the call ID before invoking the
// before-chain, so any code running inside the tool (including inner layers
// of a decorator stack) can read it without extra parameters.
//
// Usage:
//
//	// Inside a tool implementation:
//	if id, ok := toolctx.ToolCallIDFromCtx(ctx); ok {
//	    log.Printf("processing call %s", id)
//	}
package toolctx

import "context"

type ctxKey struct{}

// WithToolCallID returns a new context carrying the given tool call ID.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// ToolCallIDFromCtx retrieves the tool call ID injected by WithToolCallID.
// Returns ("", false) when no ID is present or ctx is nil.
func ToolCallIDFromCtx(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok
}
