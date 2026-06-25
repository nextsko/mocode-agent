package toolutil_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/package-register/mocode/internal/core/agent/toolutil"
)

// stubTool is a minimal fantasy.AgentTool that records call count and can be
// configured to fail a given number of times before succeeding.
type stubTool struct {
	calls   atomic.Int32
	failFor int   // fail this many times, then succeed
	err     error // error to return on failure
}

func (s *stubTool) Run(_ context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	n := int(s.calls.Add(1))
	if n <= s.failFor {
		return fantasy.ToolResponse{}, s.err
	}
	return fantasy.NewTextResponse("ok"), nil
}

func (s *stubTool) Info() fantasy.ToolInfo                       { return fantasy.ToolInfo{Name: "stub"} }
func (s *stubTool) ProviderOptions() fantasy.ProviderOptions     { return fantasy.ProviderOptions{} }
func (s *stubTool) SetProviderOptions(_ fantasy.ProviderOptions) {}

// fastPolicy returns a policy with zero delays for test speed.
func fastPolicy(maxAttempts int) toolutil.RetryPolicy {
	return toolutil.RetryPolicy{
		MaxAttempts:     maxAttempts,
		InitialInterval: 0,
		BackoffFactor:   1,
	}
}

func TestRetry_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()
	stub := &stubTool{failFor: 0}
	tool := toolutil.WithRetry(stub, fastPolicy(3))

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Content)
	require.Equal(t, int32(1), stub.calls.Load())
}

func TestRetry_SuccessAfterTransientFailures(t *testing.T) {
	t.Parallel()
	netErr := &net.OpError{Op: "read", Err: &timeoutError{}}
	stub := &stubTool{failFor: 2, err: netErr}
	tool := toolutil.WithRetry(stub, fastPolicy(4))

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Content)
	require.Equal(t, int32(3), stub.calls.Load()) // failed 2x, succeeded 3rd
}

func TestRetry_ExhaustsAttemptsReturnsLastError(t *testing.T) {
	t.Parallel()
	netErr := &net.OpError{Op: "read", Err: &timeoutError{}}
	stub := &stubTool{failFor: 10, err: netErr}
	tool := toolutil.WithRetry(stub, fastPolicy(3))

	_, err := tool.Run(context.Background(), fantasy.ToolCall{})
	require.Error(t, err)
	require.Equal(t, int32(3), stub.calls.Load()) // exactly MaxAttempts tries
}

func TestRetry_NonRetriableErrorNotRetried(t *testing.T) {
	t.Parallel()
	permanentErr := errors.New("permission denied")
	stub := &stubTool{failFor: 10, err: permanentErr}
	tool := toolutil.WithRetry(stub, fastPolicy(5))

	_, err := tool.Run(context.Background(), fantasy.ToolCall{})
	require.Error(t, err)
	require.Equal(t, int32(1), stub.calls.Load()) // only one attempt
}

func TestRetry_EOFIsRetried(t *testing.T) {
	t.Parallel()
	stub := &stubTool{failFor: 2, err: io.EOF}
	tool := toolutil.WithRetry(stub, fastPolicy(5))

	resp, err := tool.Run(context.Background(), fantasy.ToolCall{})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Content)
	require.Equal(t, int32(3), stub.calls.Load())
}

func TestRetry_ContextCancellationStopsRetrying(t *testing.T) {
	t.Parallel()
	netErr := &net.OpError{Op: "read", Err: &timeoutError{}}
	stub := &stubTool{failFor: 10, err: netErr}

	p := toolutil.RetryPolicy{
		MaxAttempts:     10,
		InitialInterval: 50 * time.Millisecond,
		BackoffFactor:   1,
		ShouldRetry: func(_ context.Context, _ int, _ error) bool {
			return true // always says yes, but ctx cancellation should stop
		},
	}
	tool := toolutil.WithRetry(stub, p)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := tool.Run(ctx, fantasy.ToolCall{})
	require.Error(t, err)
	// Should not reach MaxAttempts=10 within the context timeout
	require.Less(t, int(stub.calls.Load()), 10)
}

func TestRetry_MaxAttemptsBelowTwoDisablesRetry(t *testing.T) {
	t.Parallel()
	netErr := &net.OpError{Op: "read", Err: &timeoutError{}}
	stub := &stubTool{failFor: 5, err: netErr}

	// MaxAttempts < 2 → Wrap returns inner directly, no retry wrapper
	tool := toolutil.WithRetry(stub, toolutil.RetryPolicy{MaxAttempts: 1})
	require.Equal(t, stub, tool) // same pointer, no wrapper
}

func TestRetry_InfoDelegatedToInner(t *testing.T) {
	t.Parallel()
	stub := &stubTool{}
	tool := toolutil.WithRetry(stub, fastPolicy(3))
	require.Equal(t, "stub", tool.Info().Name)
}

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()
	p := toolutil.DefaultRetryPolicy()
	require.Equal(t, 3, p.MaxAttempts)
	require.True(t, p.Jitter)
	require.Greater(t, p.InitialInterval, time.Duration(0))
}

func TestDefaultShouldRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	require.False(t, toolutil.DefaultShouldRetry(ctx, 1, nil))
	require.False(t, toolutil.DefaultShouldRetry(ctx, 1, context.Canceled))
	require.False(t, toolutil.DefaultShouldRetry(ctx, 1, context.DeadlineExceeded))
	require.True(t, toolutil.DefaultShouldRetry(ctx, 1, io.EOF))
	require.True(t, toolutil.DefaultShouldRetry(ctx, 1, io.ErrUnexpectedEOF))
	require.True(t, toolutil.DefaultShouldRetry(ctx, 1, &net.OpError{Op: "r", Err: &timeoutError{}}))
	require.False(t, toolutil.DefaultShouldRetry(ctx, 1, errors.New("other")))
}

// timeoutError implements net.Error with Timeout() == true.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
