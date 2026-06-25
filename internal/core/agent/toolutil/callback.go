package toolutil

import (
	"context"
	"fmt"

	"charm.land/fantasy"
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
type BeforeResult struct {
	CustomResult  *fantasy.ToolResponse
	ModifiedInput string
}

// BeforeFunc is called before a wrapped tool executes.
type BeforeFunc func(ctx context.Context, args *BeforeArgs) (*BeforeResult, error)

// AfterArgs holds the information available to an AfterFunc after the inner
// tool has produced a response.
type AfterArgs struct {
	ToolCallID string
	ToolName   string
	Input      string
	Response   fantasy.ToolResponse
	RunErr     error
}

// AfterResult is the value returned by an AfterFunc.
type AfterResult struct {
	CustomResponse *fantasy.ToolResponse
	OverrideError  error
}

// AfterFunc is called after the wrapped tool executes.
type AfterFunc func(ctx context.Context, args *AfterArgs) (*AfterResult, error)

// wrappedTool decorates an AgentTool with chains of BeforeFuncs and AfterFuncs.
type wrappedTool struct {
	inner   fantasy.AgentTool
	befores []BeforeFunc
	afters  []AfterFunc
}

// Wrap returns a new AgentTool that runs befores (in order) before delegating
// to inner.
func Wrap(inner fantasy.AgentTool, befores ...BeforeFunc) fantasy.AgentTool {
	if len(befores) == 0 {
		return inner
	}
	return &wrappedTool{inner: inner, befores: befores}
}

// WrapAfter returns a new AgentTool that runs afters (in order) after the
// inner tool responds.
func WrapAfter(inner fantasy.AgentTool, afters ...AfterFunc) fantasy.AgentTool {
	if len(afters) == 0 {
		return inner
	}
	return &wrappedTool{inner: inner, afters: afters}
}

// WrapFull returns a new AgentTool with both before and after chains.
func WrapFull(inner fantasy.AgentTool, befores []BeforeFunc, afters []AfterFunc) fantasy.AgentTool {
	if len(befores) == 0 && len(afters) == 0 {
		return inner
	}
	return &wrappedTool{inner: inner, befores: befores, afters: afters}
}

// Unwrap returns the inner tool so that As[T] can walk the chain.
func (w *wrappedTool) Unwrap() fantasy.AgentTool { return w.inner }

func (w *wrappedTool) Info() fantasy.ToolInfo { return w.inner.Info() }

func (w *wrappedTool) ProviderOptions() fantasy.ProviderOptions {
	return w.inner.ProviderOptions()
}

func (w *wrappedTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	w.inner.SetProviderOptions(opts)
}

// Run executes the before-chain, delegates to the inner tool (unless
// short-circuited), and then runs the after-chain on the response.
func (w *wrappedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	ctx = WithToolCallID(ctx, call.ID)

	currentInput := call.Input

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
			return w.runAfterChain(ctx, call.ID, call.Name, currentInput, *result.CustomResult, nil)
		}
		if result.ModifiedInput != "" {
			currentInput = result.ModifiedInput
			beforeArgs.Input = currentInput
		}
	}

	forwarded := fantasy.ToolCall{
		ID:    call.ID,
		Name:  call.Name,
		Input: currentInput,
	}
	resp, runErr := w.inner.Run(ctx, forwarded)

	return w.runAfterChain(ctx, call.ID, call.Name, currentInput, resp, runErr)
}

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
			afterArgs.Response = *result.CustomResponse
		}
		if result.OverrideError != nil {
			afterArgs.RunErr = result.OverrideError
		}
	}
	return afterArgs.Response, afterArgs.RunErr
}
