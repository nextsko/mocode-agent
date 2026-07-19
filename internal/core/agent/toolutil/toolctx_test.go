package toolutil_test

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/agent/toolutil"
)

// ─── WithToolCallID / ToolCallIDFromCtx ───────────────────────────────────────

func TestToolCallIDFromCtx_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := toolutil.WithToolCallID(context.Background(), "call-42")
	id, ok := toolutil.ToolCallIDFromCtx(ctx)
	require.True(t, ok)
	assert.Equal(t, "call-42", id)
}

func TestToolCallIDFromCtx_NotSet_ReturnsFalse(t *testing.T) {
	t.Parallel()
	_, ok := toolutil.ToolCallIDFromCtx(context.Background())
	assert.False(t, ok)
}

func TestToolCallIDFromCtx_NilContext_ReturnsFalse(t *testing.T) {
	t.Parallel()
	//nolint:staticcheck // intentional nil context for robustness test
	_, ok := toolutil.ToolCallIDFromCtx(nil)
	assert.False(t, ok)
}

func TestWithToolCallID_OverridesExistingValue(t *testing.T) {
	t.Parallel()
	ctx := toolutil.WithToolCallID(context.Background(), "first")
	ctx = toolutil.WithToolCallID(ctx, "second")
	id, ok := toolutil.ToolCallIDFromCtx(ctx)
	require.True(t, ok)
	assert.Equal(t, "second", id)
}

// ─── Injection via toolutil.Wrap ─────────────────────────────────────────────

func TestCallbackWrap_InjectsToolCallIDIntoCtx(t *testing.T) {
	t.Parallel()
	var capturedID string
	var capturedOK bool

	// A BeforeFunc that reads the call ID from context.
	readID := toolutil.BeforeFunc(func(ctx context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		capturedID, capturedOK = toolutil.ToolCallIDFromCtx(ctx)
		return nil, nil
	})

	inner := fantasy.NewAgentTool("t", "desc",
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("ok"), nil
		},
	)
	wrapped := toolutil.Wrap(inner, readID)
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
			if id, ok := toolutil.ToolCallIDFromCtx(ctx); ok {
				innerSawID = id
			}
			return fantasy.NewTextResponse("ok"), nil
		},
	)
	noop := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return nil, nil
	})
	wrapped := toolutil.Wrap(inner, noop)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "deep-id", Name: "probe", Input: "{}",
	})
	require.NoError(t, err)
	assert.Equal(t, "deep-id", innerSawID)
}
