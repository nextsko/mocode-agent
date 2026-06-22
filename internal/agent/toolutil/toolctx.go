package toolutil

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
