package toolctx_test

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil/callback"
	"github.com/package-register/mocode/internal/agent/toolutil/toolctx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── WithToolCallID / ToolCallIDFromCtx ───────────────────────────────────────

func TestToolCallIDFromCtx_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := toolctx.WithToolCallID(context.Background(), "call-42")
	id, ok := toolctx.ToolCallIDFromCtx(ctx)
	require.True(t, ok)
	assert.Equal(t, "call-42", id)
}

func TestToolCallIDFromCtx_NotSet_ReturnsFalse(t *testing.T) {
	t.Parallel()
	_, ok := toolctx.ToolCallIDFromCtx(context.Background())
	assert.False(t, ok)
}

func TestToolCallIDFromCtx_NilContext_ReturnsFalse(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // intentional nil context for robustness test
	_, ok := toolctx.ToolCallIDFromCtx(nil)
	assert.False(t, ok)
}

func TestWithToolCallID_OverridesExistingValue(t *testing.T) {
	t.Parallel()
	ctx := toolctx.WithToolCallID(context.Background(), "first")
	ctx = toolctx.WithToolCallID(ctx, "second")
	id, ok := toolctx.ToolCallIDFromCtx(ctx)
	require.True(t, ok)
	assert.Equal(t, "second", id)
}

// ─── Injection via callback.Wrap ─────────────────────────────────────────────

func TestCallbackWrap_InjectsToolCallIDIntoCtx(t *testing.T) {
	t.Parallel()
	var capturedID string
	var capturedOK bool

	// A BeforeFunc that reads the call ID from context.
	readID := callback.BeforeFunc(func(ctx context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		capturedID, capturedOK = toolctx.ToolCallIDFromCtx(ctx)
		return nil, nil
	})

	inner := fantasy.NewAgentTool("t", "desc",
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
	wrapped := callback.Wrap(inner, readID)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "injected-id", Name: "t", Input: "{}",
	})
	require.NoError(t, err)
	require.True(t, capturedOK)
	assert.Equal(t, "injected-id", capturedID)
}

func TestInnerTool_CanReadToolCallIDFromCtx(t *testing.T) {
	t.Parallel()
	var innerSawID string

	inner := fantasy.NewAgentTool("probe", "reads call id",
		func(ctx context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if id, ok := toolctx.ToolCallIDFromCtx(ctx); ok {
				innerSawID = id
			}
			return fantasy.NewTextResponse("ok"), nil
		},
	)
	noop := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return nil, nil
	})
	wrapped := callback.Wrap(inner, noop)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "deep-id", Name: "probe", Input: "{}",
	})
	require.NoError(t, err)
	assert.Equal(t, "deep-id", innerSawID)
}
