package capability_test

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil/callback"
	"github.com/package-register/mocode/internal/agent/toolutil/capability"
	"github.com/package-register/mocode/internal/agent/toolutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// simpleTool is a bare AgentTool (no Unwrap, no Instructions).
type simpleTool struct{ name string }

func (s *simpleTool) Info() fantasy.ToolInfo { return fantasy.ToolInfo{Name: s.name} }
func (s *simpleTool) ProviderOptions() fantasy.ProviderOptions {
	return fantasy.ProviderOptions{}
}
func (s *simpleTool) SetProviderOptions(_ fantasy.ProviderOptions) {}
func (s *simpleTool) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return fantasy.NewTextResponse("ok"), nil
}

// instructableTool implements both AgentTool and Instructable.
type instructableTool struct {
	simpleTool
	instructions string
}

func (i *instructableTool) Instructions() string { return i.instructions }

// wrapperTool wraps an inner AgentTool and implements Unwrapper.
type wrapperTool struct {
	simpleTool
	inner fantasy.AgentTool
}

func (w *wrapperTool) Unwrap() fantasy.AgentTool { return w.inner }

// ─── As[T] — basic ───────────────────────────────────────────────────────────

func TestAs_BareToolFound(t *testing.T) {
	t.Parallel()
	tool := &simpleTool{name: "bare"}
	got, ok := capability.As[*simpleTool](tool)
	require.True(t, ok)
	assert.Equal(t, tool, got)
}

func TestAs_InterfaceMiss_ReturnsZeroFalse(t *testing.T) {
	t.Parallel()
	tool := &simpleTool{name: "bare"}
	got, ok := capability.As[capability.Instructable](tool)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestAs_NilInput_ReturnsZeroFalse(t *testing.T) {
	t.Parallel()
	got, ok := capability.As[capability.Instructable](nil)
	assert.False(t, ok)
	assert.Nil(t, got)
}

// ─── As[T] — chain traversal ─────────────────────────────────────────────────

func TestAs_TwoLayerChain_FindsInner(t *testing.T) {
	t.Parallel()
	inner := &instructableTool{simpleTool: simpleTool{name: "inner"}, instructions: "help text"}
	outer := &wrapperTool{simpleTool: simpleTool{name: "outer"}, inner: inner}

	got, ok := capability.As[capability.Instructable](outer)
	require.True(t, ok)
	assert.Equal(t, "help text", got.Instructions())
}

func TestAs_ThreeLayerChain_FindsDeepInner(t *testing.T) {
	t.Parallel()
	deepInner := &instructableTool{simpleTool: simpleTool{name: "deep"}, instructions: "deep help"}
	mid := &wrapperTool{simpleTool: simpleTool{name: "mid"}, inner: deepInner}
	outer := &wrapperTool{simpleTool: simpleTool{name: "outer"}, inner: mid}

	got, ok := capability.As[capability.Instructable](outer)
	require.True(t, ok)
	assert.Equal(t, "deep help", got.Instructions())
}

func TestAs_ChainMiss_ReturnsZeroFalse(t *testing.T) {
	t.Parallel()
	inner := &simpleTool{name: "inner"}
	outer := &wrapperTool{simpleTool: simpleTool{name: "outer"}, inner: inner}

	got, ok := capability.As[capability.Instructable](outer)
	assert.False(t, ok)
	assert.Nil(t, got)
}

// dualTool is both Instructable and Unwrapper at the outermost layer.
type dualTool struct {
	simpleTool
	instructions string
	inner        fantasy.AgentTool
}

func (d *dualTool) Instructions() string      { return d.instructions }
func (d *dualTool) Unwrap() fantasy.AgentTool { return d.inner }

func TestAs_OutermostMatchWinsBeforeUnwrap(t *testing.T) {
	t.Parallel()
	// outer itself is instructable AND wraps another instructable —
	// the outermost match should be returned first.
	inner := &instructableTool{simpleTool: simpleTool{name: "inner"}, instructions: "inner help"}
	outer := &dualTool{
		simpleTool:   simpleTool{name: "outer"},
		instructions: "outer help",
		inner:        inner,
	}
	got, ok := capability.As[capability.Instructable](outer)
	require.True(t, ok)
	assert.Equal(t, "outer help", got.Instructions())
}

// ─── Instructable / GetInstructions ──────────────────────────────────────────

func TestGetInstructions_InstructableTool_ReturnsString(t *testing.T) {
	t.Parallel()
	tool := &instructableTool{simpleTool: simpleTool{name: "t"}, instructions: "do this"}
	assert.Equal(t, "do this", capability.GetInstructions(tool))
}

func TestGetInstructions_NonInstructable_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	tool := &simpleTool{name: "plain"}
	assert.Equal(t, "", capability.GetInstructions(tool))
}

func TestGetInstructions_WrappedInstructable_WalksChain(t *testing.T) {
	t.Parallel()
	inner := &instructableTool{simpleTool: simpleTool{name: "i"}, instructions: "chain help"}
	outer := &wrapperTool{simpleTool: simpleTool{name: "o"}, inner: inner}
	assert.Equal(t, "chain help", capability.GetInstructions(outer))
}

func TestGetInstructions_NilTool_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", capability.GetInstructions(nil))
}

// ─── Real decorator stack integration ────────────────────────────────────────
// These tests verify that Unwrap() is wired on the actual retry and callback
// wrappers, so As[T] can find a capability buried under multiple decorators.

func TestAs_ThroughRetryDecorator(t *testing.T) {
	t.Parallel()
	inner := &instructableTool{simpleTool: simpleTool{name: "inner"}, instructions: "via retry"}
	wrapped := retry.Wrap(inner, retry.Policy{MaxAttempts: 2})

	got, ok := capability.As[capability.Instructable](wrapped)
	require.True(t, ok)
	assert.Equal(t, "via retry", got.Instructions())
}

func TestAs_ThroughCallbackDecorator(t *testing.T) {
	t.Parallel()
	inner := &instructableTool{simpleTool: simpleTool{name: "inner"}, instructions: "via callback"}
	noop := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return nil, nil
	})
	wrapped := callback.Wrap(inner, noop)

	got, ok := capability.As[capability.Instructable](wrapped)
	require.True(t, ok)
	assert.Equal(t, "via callback", got.Instructions())
}

func TestAs_ThroughCallbackThenRetry_FullStack(t *testing.T) {
	t.Parallel()
	// Stack: callback → retry → instructableTool
	inner := &instructableTool{simpleTool: simpleTool{name: "deep"}, instructions: "deep stack"}
	withRetry := retry.Wrap(inner, retry.Policy{MaxAttempts: 2})
	noop := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return nil, nil
	})
	withCallback := callback.Wrap(withRetry, noop)

	got, ok := capability.As[capability.Instructable](withCallback)
	require.True(t, ok)
	assert.Equal(t, "deep stack", got.Instructions())
}

func TestRetryWrap_Unwrap_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := &simpleTool{name: "base"}
	wrapped := retry.Wrap(inner, retry.Policy{MaxAttempts: 3})
	// When retry is active, Unwrap should return inner.
	got, ok := capability.As[*simpleTool](wrapped)
	require.True(t, ok)
	assert.Equal(t, inner, got)
}

func TestCallbackWrap_Unwrap_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := &simpleTool{name: "base"}
	noop := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return nil, nil
	})
	wrapped := callback.Wrap(inner, noop)
	got, ok := capability.As[*simpleTool](wrapped)
	require.True(t, ok)
	assert.Equal(t, inner, got)
}
