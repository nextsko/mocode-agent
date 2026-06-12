package callback_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil/callback"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

type echoParams struct {
	Text string `json:"text"`
}

// echoTool returns a tool that echoes the "text" field from its JSON input.
func echoTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"echo",
		"echoes input",
		func(_ context.Context, p echoParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.NewTextResponse("echo:" + p.Text), nil
		},
	)
}

func runTool(t *testing.T, tool fantasy.AgentTool, input string) (fantasy.ToolResponse, error) {
	t.Helper()
	return tool.Run(context.Background(), fantasy.ToolCall{
		ID:    "call-1",
		Name:  tool.Info().Name,
		Input: input,
	})
}

// ─── Pass-through ─────────────────────────────────────────────────────────────

func TestWrap_NoBeforeFunc_PassesThrough(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	wrapped := callback.Wrap(inner)
	// Wrap with no funcs returns inner unchanged.
	assert.Equal(t, inner, wrapped)
}

func TestWrap_NilResult_PassesThrough(t *testing.T) {
	t.Parallel()
	called := false
	beforeFn := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		called = true
		return nil, nil // pass-through
	})

	input := `{"text":"hello"}`
	resp, err := runTool(t, callback.Wrap(echoTool(), beforeFn), input)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Contains(t, resp.Content, "echo:hello")
}

// ─── Short-circuit ────────────────────────────────────────────────────────────

func TestWrap_CustomResult_ShortCircuits(t *testing.T) {
	t.Parallel()
	customResp := fantasy.NewTextResponse("intercepted")
	innerCalled := false

	innerTool := fantasy.NewAgentTool(
		"never",
		"should not be called",
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			innerCalled = true
			return fantasy.NewTextResponse("inner"), nil
		},
	)

	beforeFn := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return &callback.BeforeResult{CustomResult: &customResp}, nil
	})

	resp, err := runTool(t, callback.Wrap(innerTool, beforeFn), `{}`)
	require.NoError(t, err)
	assert.False(t, innerCalled, "inner tool must not be called when short-circuited")
	assert.Equal(t, "intercepted", resp.Content)
}

// ─── Input modification ───────────────────────────────────────────────────────

func TestWrap_ModifiedInput_ForwardedToInner(t *testing.T) {
	t.Parallel()
	beforeFn := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		modified, err := json.Marshal(echoParams{Text: "modified"})
		require.NoError(t, err)
		return &callback.BeforeResult{ModifiedInput: string(modified)}, nil
	})

	resp, err := runTool(t, callback.Wrap(echoTool(), beforeFn), `{"text":"original"}`)
	require.NoError(t, err)
	assert.Contains(t, resp.Content, "echo:modified")
}

// ─── Error propagation ────────────────────────────────────────────────────────

func TestWrap_BeforeFuncError_AbortsTool(t *testing.T) {
	t.Parallel()
	sentinelErr := errors.New("before error")
	beforeFn := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return nil, sentinelErr
	})

	_, err := runTool(t, callback.Wrap(echoTool(), beforeFn), `{"text":"x"}`)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinelErr)
}

// ─── Chain ordering ───────────────────────────────────────────────────────────

func TestWrap_MultipleBeforeFuncs_ExecutedInOrder(t *testing.T) {
	t.Parallel()
	var order []int
	fn1 := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		order = append(order, 1)
		return nil, nil
	})
	fn2 := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		order = append(order, 2)
		return nil, nil
	})

	_, err := runTool(t, callback.Wrap(echoTool(), fn1, fn2), `{"text":"x"}`)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
}

// ─── Args exposure ────────────────────────────────────────────────────────────

func TestWrap_BeforeArgs_Populated(t *testing.T) {
	t.Parallel()
	var capturedArgs *callback.BeforeArgs
	beforeFn := callback.BeforeFunc(func(_ context.Context, args *callback.BeforeArgs) (*callback.BeforeResult, error) {
		capturedArgs = args
		return nil, nil
	})

	input := `{"text":"check"}`
	_, err := runTool(t, callback.Wrap(echoTool(), beforeFn), input)
	require.NoError(t, err)
	require.NotNil(t, capturedArgs)
	assert.Equal(t, "echo", capturedArgs.ToolName)
	assert.Equal(t, "call-1", capturedArgs.ToolCallID)
	assert.Equal(t, input, capturedArgs.Input)
}

// ─── Interface delegation ────────────────────────────────────────────────────

func TestWrap_Info_DelegatedToInner(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	wrapped := callback.Wrap(inner, func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return nil, nil
	})
	assert.Equal(t, inner.Info().Name, wrapped.Info().Name)
}

// ─── AfterFunc / WrapAfter / WrapFull ────────────────────────────────────────

func TestWrapAfter_NoAfterFunc_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	wrapped := callback.WrapAfter(inner)
	assert.Equal(t, inner, wrapped)
}

func TestWrapAfter_NilResult_PassesThrough(t *testing.T) {
	t.Parallel()
	called := false
	afterFn := callback.AfterFunc(func(_ context.Context, _ *callback.AfterArgs) (*callback.AfterResult, error) {
		called = true
		return nil, nil
	})

	resp, err := runTool(t, callback.WrapAfter(echoTool(), afterFn), `{"text":"world"}`)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Contains(t, resp.Content, "echo:world")
}

func TestWrapAfter_CustomResponse_ReplacesOriginal(t *testing.T) {
	t.Parallel()
	replacement := fantasy.NewTextResponse("replaced")
	afterFn := callback.AfterFunc(func(_ context.Context, _ *callback.AfterArgs) (*callback.AfterResult, error) {
		return &callback.AfterResult{CustomResponse: &replacement}, nil
	})

	resp, err := runTool(t, callback.WrapAfter(echoTool(), afterFn), `{"text":"original"}`)
	require.NoError(t, err)
	assert.Equal(t, "replaced", resp.Content)
}

func TestWrapAfter_OverrideError_ReplacesRunErr(t *testing.T) {
	t.Parallel()
	// Make the inner tool return an error.
	errTool := fantasy.NewAgentTool(
		"err",
		"always errors",
		func(_ context.Context, _ struct{}, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			return fantasy.ToolResponse{}, errors.New("inner error")
		},
	)
	newErr := errors.New("overridden error")
	afterFn := callback.AfterFunc(func(_ context.Context, args *callback.AfterArgs) (*callback.AfterResult, error) {
		if args.RunErr != nil {
			return &callback.AfterResult{OverrideError: newErr}, nil
		}
		return nil, nil
	})

	_, err := callback.WrapAfter(errTool, afterFn).Run(context.Background(), fantasy.ToolCall{
		ID: "x", Name: "err", Input: "{}",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, newErr)
}

func TestWrapAfter_AfterFuncError_AbortChain(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("after abort")
	afterFn := callback.AfterFunc(func(_ context.Context, _ *callback.AfterArgs) (*callback.AfterResult, error) {
		return nil, sentinel
	})
	_, err := runTool(t, callback.WrapAfter(echoTool(), afterFn), `{"text":"x"}`)
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestWrapAfter_MultipleAfterFuncs_ExecutedInOrder(t *testing.T) {
	t.Parallel()
	var order []int
	fn1 := callback.AfterFunc(func(_ context.Context, _ *callback.AfterArgs) (*callback.AfterResult, error) {
		order = append(order, 1)
		return nil, nil
	})
	fn2 := callback.AfterFunc(func(_ context.Context, _ *callback.AfterArgs) (*callback.AfterResult, error) {
		order = append(order, 2)
		return nil, nil
	})
	_, err := runTool(t, callback.WrapAfter(echoTool(), fn1, fn2), `{"text":"x"}`)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
}

func TestWrapAfter_AfterArgs_Populated(t *testing.T) {
	t.Parallel()
	var captured *callback.AfterArgs
	afterFn := callback.AfterFunc(func(_ context.Context, args *callback.AfterArgs) (*callback.AfterResult, error) {
		captured = args
		return nil, nil
	})
	input := `{"text":"check"}`
	_, err := runTool(t, callback.WrapAfter(echoTool(), afterFn), input)
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "echo", captured.ToolName)
	assert.Equal(t, "call-1", captured.ToolCallID)
	assert.Equal(t, input, captured.Input)
	assert.Contains(t, captured.Response.Content, "echo:check")
	assert.NoError(t, captured.RunErr)
}

func TestWrapFull_BothChainsFire(t *testing.T) {
	t.Parallel()
	var order []string
	before := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		order = append(order, "before")
		return nil, nil
	})
	after := callback.AfterFunc(func(_ context.Context, _ *callback.AfterArgs) (*callback.AfterResult, error) {
		order = append(order, "after")
		return nil, nil
	})
	_, err := runTool(t, callback.WrapFull(echoTool(), []callback.BeforeFunc{before}, []callback.AfterFunc{after}), `{"text":"x"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"before", "after"}, order)
}

func TestWrapFull_BeforeShortCircuit_AfterStillFires(t *testing.T) {
	t.Parallel()
	shortResp := fantasy.NewTextResponse("short")
	afterCalled := false
	before := callback.BeforeFunc(func(_ context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		return &callback.BeforeResult{CustomResult: &shortResp}, nil
	})
	after := callback.AfterFunc(func(_ context.Context, args *callback.AfterArgs) (*callback.AfterResult, error) {
		afterCalled = true
		assert.Equal(t, "short", args.Response.Content)
		return nil, nil
	})
	resp, err := runTool(t, callback.WrapFull(echoTool(), []callback.BeforeFunc{before}, []callback.AfterFunc{after}), `{}`)
	require.NoError(t, err)
	assert.True(t, afterCalled, "after-func must still run when before short-circuits")
	assert.Equal(t, "short", resp.Content)
}

func TestWrapFull_EmptySlices_ReturnsInner(t *testing.T) {
	t.Parallel()
	inner := echoTool()
	wrapped := callback.WrapFull(inner, nil, nil)
	assert.Equal(t, inner, wrapped)
}

// ─── Context cancellation ────────────────────────────────────────────────────

func TestWrap_ContextCancelled_BeforeFuncReceivesIt(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	var ctxErr error
	beforeFn := callback.BeforeFunc(func(ctx context.Context, _ *callback.BeforeArgs) (*callback.BeforeResult, error) {
		ctxErr = ctx.Err()
		return nil, nil // let it through; inner will get the cancelled ctx too
	})

	_, _ = callback.Wrap(echoTool(), beforeFn).Run(ctx, fantasy.ToolCall{
		ID:    "c",
		Name:  "echo",
		Input: `{"text":"x"}`,
	})
	assert.ErrorIs(t, ctxErr, context.Canceled)
}
