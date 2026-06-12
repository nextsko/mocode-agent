// Package callback provides before/after interceptor decorators that wrap a
// fantasy.AgentTool.  Inspired by BeforeToolCallbackStructured and
// AfterToolCallbackStructured in trpc-agent-go/tool/callbacks.go.
//
// The decorator is intentionally distinct from the shell-based hook system
// (internal/agent/hooked_tool.go): hooks run user shell commands via the hooks
// engine, whereas this package runs in-process Go functions that can inspect,
// modify, and short-circuit tool I/O.
//
// Usage:
//
//	// Before-only (intercept / modify input, short-circuit):
//	wrapped := callback.Wrap(innerTool, myBeforeFunc)
//
//	// After-only (observe / replace response):
//	wrapped := callback.WrapAfter(innerTool, myAfterFunc)
//
//	// Both before and after:
//	wrapped := callback.WrapFull(innerTool,
//	    []callback.BeforeFunc{myBeforeFunc},
//	    []callback.AfterFunc{myAfterFunc},
//	)
package callback

import (
	"context"
	"fmt"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil/toolctx"
)

// BeforeArgs holds the information available to a BeforeFunc before the inner
// tool is called.
type BeforeArgs struct {
	// ToolCallID is the model-issued identifier for this tool call.
	ToolCallID string
	// ToolName is the name of the tool being called.
	ToolName string
	// Input is the raw JSON arguments string (as received from the model).
	// A BeforeFunc may read it; to modify it use BeforeResult.ModifiedInput.
	Input string
}

// BeforeResult is the value returned by a BeforeFunc.
// A nil return is equivalent to &BeforeResult{} (pass-through with no change).
type BeforeResult struct {
	// CustomResult, when non-nil, is returned immediately and the inner tool
	// is never called.  Subsequent BeforeFuncs in the chain are also skipped.
	CustomResult *fantasy.ToolResponse
	// ModifiedInput, when non-empty, replaces the original JSON input before
	// it is forwarded to the inner tool (or the next BeforeFunc).
	ModifiedInput string
}

// BeforeFunc is called before a wrapped tool executes.
// Return (nil, nil) to pass through unchanged.
// Return a non-nil CustomResult to short-circuit the inner tool.
// Return a non-nil error to abort execution with that error.
type BeforeFunc func(ctx context.Context, args *BeforeArgs) (*BeforeResult, error)

// AfterArgs holds the information available to an AfterFunc after the inner
// tool (or a BeforeFunc short-circuit) has produced a response.
type AfterArgs struct {
	// ToolCallID is the model-issued identifier for this tool call.
	ToolCallID string
	// ToolName is the name of the tool that was called.
	ToolName string
	// Input is the final JSON arguments string forwarded to the inner tool.
	Input string
	// Response is the response produced by the inner tool (or by a
	// BeforeFunc short-circuit).
	Response fantasy.ToolResponse
	// RunErr is the error returned by the inner tool, if any.
	RunErr error
}

// AfterResult is the value returned by an AfterFunc.
// A nil return is equivalent to &AfterResult{} (pass-through with no change).
type AfterResult struct {
	// CustomResponse, when non-nil, replaces the response that will be
	// returned to the caller.  Subsequent AfterFuncs still run, but they
	// receive the replaced response in AfterArgs.Response.
	CustomResponse *fantasy.ToolResponse
	// OverrideError, when non-nil, replaces the error returned to the caller.
	OverrideError error
}

// AfterFunc is called after the wrapped tool executes (or after a BeforeFunc
// short-circuit).  The args.RunErr field is nil when the inner tool succeeded.
// Return (nil, nil) to pass through unchanged.
// Return a non-nil error to abort the after-chain with that error.
type AfterFunc func(ctx context.Context, args *AfterArgs) (*AfterResult, error)

// wrappedTool decorates an AgentTool with chains of BeforeFuncs and AfterFuncs.
type wrappedTool struct {
	inner   fantasy.AgentTool
	befores []BeforeFunc
	afters  []AfterFunc
}

// Wrap returns a new AgentTool that runs befores (in order) before delegating
// to inner.  An empty befores slice is valid and returns inner unchanged.
func Wrap(inner fantasy.AgentTool, befores ...BeforeFunc) fantasy.AgentTool {
	if len(befores) == 0 {
		return inner
	}
	return &wrappedTool{inner: inner, befores: befores}
}

// WrapAfter returns a new AgentTool that runs afters (in order) after the
// inner tool responds.  An empty afters slice is valid and returns inner
// unchanged.
func WrapAfter(inner fantasy.AgentTool, afters ...AfterFunc) fantasy.AgentTool {
	if len(afters) == 0 {
		return inner
	}
	return &wrappedTool{inner: inner, afters: afters}
}

// WrapFull returns a new AgentTool with both before and after chains.
// Either slice may be nil/empty; if both are empty inner is returned unchanged.
func WrapFull(inner fantasy.AgentTool, befores []BeforeFunc, afters []AfterFunc) fantasy.AgentTool {
	if len(befores) == 0 && len(afters) == 0 {
		return inner
	}
	return &wrappedTool{inner: inner, befores: befores, afters: afters}
}

// Unwrap returns the inner tool so that capability.As[T] can walk the chain.
func (w *wrappedTool) Unwrap() fantasy.AgentTool { return w.inner }

// Info delegates to the inner tool unchanged so the caller sees the real name
// and description.
func (w *wrappedTool) Info() fantasy.ToolInfo { return w.inner.Info() }

// ProviderOptions delegates to the inner tool.
func (w *wrappedTool) ProviderOptions() fantasy.ProviderOptions { return w.inner.ProviderOptions() }

// SetProviderOptions delegates to the inner tool.
func (w *wrappedTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	w.inner.SetProviderOptions(opts)
}

// Run executes the before-chain, delegates to the inner tool (unless
// short-circuited), and then runs the after-chain on the response.
func (w *wrappedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	// Inject the tool call ID so inner implementations can read it via
	// toolctx.ToolCallIDFromCtx without needing extra parameters.
	ctx = toolctx.WithToolCallID(ctx, call.ID)

	currentInput := call.Input

	// ── Before chain ────────────────────────────────────────────────────────
	beforeArgs := &BeforeArgs{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Input:      currentInput,
	}

	for _, fn := range w.befores {
		result, err := fn(ctx, beforeArgs)
		if err != nil {
			return fantasy.ToolResponse{}, fmt.Errorf("before-tool callback for %q: %w", call.Name, err)
		}
		if result == nil {
			continue
		}
		if result.CustomResult != nil {
			// Short-circuit: skip the inner tool and go directly to after-chain.
			return w.runAfterChain(ctx, call.ID, call.Name, currentInput, *result.CustomResult, nil)
		}
		if result.ModifiedInput != "" {
			currentInput = result.ModifiedInput
			beforeArgs.Input = currentInput
		}
	}

	// ── Inner tool ──────────────────────────────────────────────────────────
	forwarded := fantasy.ToolCall{
		ID:    call.ID,
		Name:  call.Name,
		Input: currentInput,
	}
	resp, runErr := w.inner.Run(ctx, forwarded)

	// ── After chain ─────────────────────────────────────────────────────────
	return w.runAfterChain(ctx, call.ID, call.Name, currentInput, resp, runErr)
}

// runAfterChain iterates the after-chain and returns the (possibly replaced)
// response and error.
func (w *wrappedTool) runAfterChain(
	ctx context.Context,
	toolCallID, toolName, input string,
	resp fantasy.ToolResponse,
	runErr error,
) (fantasy.ToolResponse, error) {
	if len(w.afters) == 0 {
		return resp, runErr
	}

	afterArgs := &AfterArgs{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Input:      input,
		Response:   resp,
		RunErr:     runErr,
	}

	for _, fn := range w.afters {
		result, err := fn(ctx, afterArgs)
		if err != nil {
			return afterArgs.Response, fmt.Errorf("after-tool callback for %q: %w", toolName, err)
		}
		if result == nil {
			continue
		}
		if result.CustomResponse != nil {
			// Replace the response for subsequent after-funcs.
			afterArgs.Response = *result.CustomResponse
		}
		if result.OverrideError != nil {
			afterArgs.RunErr = result.OverrideError
		}
	}
	return afterArgs.Response, afterArgs.RunErr
}
