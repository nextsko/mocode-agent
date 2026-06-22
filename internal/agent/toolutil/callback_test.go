package toolutil_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type echoParams struct {
	Text string `json:"text"`
}

func echoTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"echo",
		"echoes input",
		func(_ context.Context, p echoParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("echo:" + p.Text), nil
		},
	)
}

func mustUnmarshal[T any](t *testing.T, raw string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(raw), &v))
	return v
}

// ─── Wrap (before-only) ─────────────────────────────────────────────────────

func TestWrap_NoBefores_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	got := toolutil.Wrap(inner)
	require.Equal(t, inner, got) // same pointer, no wrapper
}

func TestWrap_BeforePassThrough_RunsInner(t *testing.T) {
	t.Parallel()
	called := false
	noop := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		called = true
		return nil, nil
	})
	wrapped := toolutil.Wrap(echoTool(), noop)
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	require.True(t, called)
	assert.Contains(t, resp.Content, "echo:hi")
}

func TestWrap_BeforeShortCircuit_SkipsInner(t *testing.T) {
	t.Parallel()
	innerCalled := false

	shortCircuit := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		custom := fantasy.NewTextResponse("blocked!")
		return &toolutil.BeforeResult{CustomResult: &custom}, nil
	})

	// wrap the echoTool so we can detect if it ran
	inner := fantasy.NewAgentTool("echo", "echoes",
		func(_ context.Context, _ echoParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			innerCalled = true
			return fantasy.NewTextResponse("echo"), nil
		},
	)
	wrapped := toolutil.Wrap(inner, shortCircuit)
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "blocked!", resp.Content)
	assert.False(t, innerCalled)
}

func TestWrap_BeforeModifiesInput(t *testing.T) {
	t.Parallel()
	modify := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return &toolutil.BeforeResult{ModifiedInput: `{"text":"modified"}`}, nil
	})
	wrapped := toolutil.Wrap(echoTool(), modify)
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"original"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "echo:modified")
}

func TestWrap_BeforeError_Stops(t *testing.T) {
	t.Parallel()
	fail := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return nil, errors.New("before failed")
	})
	wrapped := toolutil.Wrap(echoTool(), fail)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before-tool callback")
}

// ─── WrapAfter (after-only) ─────────────────────────────────────────────────

func TestWrapAfter_NoAfters_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	got := toolutil.WrapAfter(inner)
	require.Equal(t, inner, got)
}

func TestWrapAfter_PassThrough_RunsAfter(t *testing.T) {
	t.Parallel()
	afterRan := false
	after := toolutil.AfterFunc(func(_ context.Context, _ *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		afterRan = true
		return nil, nil
	})
	wrapped := toolutil.WrapAfter(echoTool(), after)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.True(t, afterRan)
}

func TestWrapAfter_ReplacesResponse(t *testing.T) {
	t.Parallel()
	replace := toolutil.AfterFunc(func(_ context.Context, _ *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		custom := fantasy.NewTextResponse("replaced")
		return &toolutil.AfterResult{CustomResponse: &custom}, nil
	})
	wrapped := toolutil.WrapAfter(echoTool(), replace)
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "replaced", resp.Content)
}

// ─── WrapFull (both before and after) ───────────────────────────────────────

func TestWrapFull_BothEmpty_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	got := toolutil.WrapFull(inner, nil, nil)
	require.Equal(t, inner, got)
}

func TestWrapFull_BothChainsRun(t *testing.T) {
	t.Parallel()
	beforeRan := false
	afterRan := false

	before := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		beforeRan = true
		return nil, nil
	})
	after := toolutil.AfterFunc(func(_ context.Context, _ *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		afterRan = true
		return nil, nil
	})

	wrapped := toolutil.WrapFull(echoTool(), []toolutil.BeforeFunc{before}, []toolutil.AfterFunc{after})
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.True(t, beforeRan)
	assert.True(t, afterRan)
	assert.Contains(t, resp.Content, "echo:hi")
}

func TestWrapFull_BeforeShortCircuit_StillRunsAfterChain(t *testing.T) {
	t.Parallel()
	afterRan := false

	shortCircuit := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		custom := fantasy.NewTextResponse("blocked")
		return &toolutil.BeforeResult{CustomResult: &custom}, nil
	})
	after := toolutil.AfterFunc(func(_ context.Context, args *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		afterRan = true
		assert.Equal(t, "blocked", args.Response.Content)
		return nil, nil
	})

	wrapped := toolutil.WrapFull(echoTool(), []toolutil.BeforeFunc{shortCircuit}, []toolutil.AfterFunc{after})
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.True(t, afterRan)
	assert.Equal(t, "blocked", resp.Content)
}

// ─── Multi-func chains ──────────────────────────────────────────────────────

func TestWrap_MultipleBefores_RunInOrder(t *testing.T) {
	t.Parallel()
	var order []string

	b1 := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		order = append(order, "b1")
		return nil, nil
	})
	b2 := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		order = append(order, "b2")
		return nil, nil
	})

	wrapped := toolutil.Wrap(echoTool(), b1, b2)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"b1", "b2"}, order)
}

func TestWrap_BeforeShortCircuit_SkipsSubsequentBefores(t *testing.T) {
	t.Parallel()
	b2Ran := false

	b1 := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		custom := fantasy.NewTextResponse("done")
		return &toolutil.BeforeResult{CustomResult: &custom}, nil
	})
	b2 := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		b2Ran = true
		return nil, nil
	})

	wrapped := toolutil.Wrap(echoTool(), b1, b2)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.False(t, b2Ran)
}

func TestWrapFull_MultipleAfters_SequentialReplacement(t *testing.T) {
	t.Parallel()

	a1 := toolutil.AfterFunc(func(_ context.Context, _ *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		custom := fantasy.NewTextResponse("a1")
		return &toolutil.AfterResult{CustomResponse: &custom}, nil
	})
	a2 := toolutil.AfterFunc(func(_ context.Context, args *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		assert.Equal(t, "a1", args.Response.Content)
		return nil, nil
	})

	wrapped := toolutil.WrapFull(echoTool(), nil, []toolutil.AfterFunc{a1, a2})
	resp, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "a1", resp.Content)
}

// ─── Info / ProviderOptions delegation ──────────────────────────────────────

func TestWrappedTool_InfoDelegated(t *testing.T) {
	t.Parallel()
	noop := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return nil, nil
	})
	wrapped := toolutil.Wrap(echoTool(), noop)
	assert.Equal(t, "echo", wrapped.Info().Name)
}

func TestWrappedTool_ProviderOptionsDelegated(t *testing.T) {
	t.Parallel()
	noop := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return nil, nil
	})
	wrapped := toolutil.Wrap(echoTool(), noop)

	opts := wrapped.ProviderOptions()
	wrapped.SetProviderOptions(opts)
}

// ─── BeforeArgs / AfterArgs correctness ─────────────────────────────────────

func TestBeforeArgs_ReceivesCallIDAndInput(t *testing.T) {
	t.Parallel()
	var gotArgs toolutil.BeforeArgs

	capture := toolutil.BeforeFunc(func(_ context.Context, args *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		gotArgs = *args
		return nil, nil
	})

	wrapped := toolutil.Wrap(echoTool(), capture)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "call-99", Name: "echo", Input: `{"text":"hi"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "call-99", gotArgs.ToolCallID)
	assert.Equal(t, "echo", gotArgs.ToolName)
	assert.Equal(t, `{"text":"hi"}`, gotArgs.Input)
}

func TestAfterArgs_ReceivesOriginalInputNotModified(t *testing.T) {
	t.Parallel()
	var gotArgs toolutil.AfterArgs

	modify := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return &toolutil.BeforeResult{ModifiedInput: `{"text":"changed"}`}, nil
	})
	capture := toolutil.AfterFunc(func(_ context.Context, args *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		gotArgs = *args
		return nil, nil
	})

	wrapped := toolutil.WrapFull(echoTool(),
		[]toolutil.BeforeFunc{modify},
		[]toolutil.AfterFunc{capture},
	)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "echo", Input: `{"text":"original"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, `{"text":"changed"}`, gotArgs.Input)
}

// ─── AfterError propagation ─────────────────────────────────────────────────

func TestWrapAfter_InnerError_Propagated(t *testing.T) {
	t.Parallel()
	inner := fantasy.NewAgentTool("fail", "fails",
		func(_ context.Context, _ echoParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, errors.New("boom")
		},
	)
	observe := toolutil.AfterFunc(func(_ context.Context, args *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		require.Error(t, args.RunErr)
		return nil, nil
	})
	wrapped := toolutil.WrapAfter(inner, observe)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "fail", Input: `{"text":""}`,
	})
	require.Error(t, err)
}

func TestWrapAfter_OverrideError(t *testing.T) {
	t.Parallel()
	inner := fantasy.NewAgentTool("fail", "fails",
		func(_ context.Context, _ echoParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, errors.New("original")
		},
	)
	override := toolutil.AfterFunc(func(_ context.Context, _ *toolutil.AfterArgs) (*toolutil.AfterResult, error) {
		return &toolutil.AfterResult{OverrideError: errors.New("overridden")}, nil
	})
	wrapped := toolutil.WrapAfter(inner, override)
	_, err := wrapped.Run(context.Background(), fantasy.ToolCall{
		ID: "1", Name: "fail", Input: `{"text":""}`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overridden")
}

// ─── Unwrap ─────────────────────────────────────────────────────────────────

func TestWrappedTool_Unwrap(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	noop := toolutil.BeforeFunc(func(_ context.Context, _ *toolutil.BeforeArgs) (*toolutil.BeforeResult, error) {
		return nil, nil
	})
	wrapped := toolutil.Wrap(inner, noop)

	if u, ok := wrapped.(toolutil.Unwrapper); ok {
		assert.Equal(t, inner, u.Unwrap())
	} else {
		t.Fatal("wrapped tool does not implement Unwrapper")
	}
}

// Keep mustUnmarshal referenced to avoid unused warnings.
var _ = mustUnmarshal[echoParams]
