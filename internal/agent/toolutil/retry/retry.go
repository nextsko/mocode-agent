// Package retry provides a RetryPolicy and a tool decorator that retries
// transient tool-execution failures with configurable backoff.
package retry

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"time"

	"charm.land/fantasy"
)

// Policy configures retry behaviour for a wrapped tool.
type Policy struct {
	// MaxAttempts is the total number of attempts including the first try.
	// Values < 2 disable retrying.
	MaxAttempts int

	// InitialInterval is the pause applied before the second attempt.
	InitialInterval time.Duration

	// BackoffFactor controls how the interval grows after each failed attempt.
	// A factor of 2 doubles the wait on every retry.
	BackoffFactor float64

	// MaxInterval caps the computed delay when greater than zero.
	MaxInterval time.Duration

	// Jitter adds a random fraction of the current delay to prevent thundering herds.
	Jitter bool

	// ShouldRetry decides whether a given attempt should be retried.
	// Defaults to DefaultShouldRetry when nil.
	ShouldRetry func(ctx context.Context, attempt int, err error) bool
}

// DefaultPolicy returns a sensible retry policy: 3 attempts, 200 ms initial
// delay, factor 2, max 5 s, jitter enabled, retrying on transient errors.
func DefaultPolicy() Policy {
	return Policy{
		MaxAttempts:     3,
		InitialInterval: 200 * time.Millisecond,
		BackoffFactor:   2,
		MaxInterval:     5 * time.Second,
		Jitter:          true,
	}
}

// DefaultShouldRetry retries on network timeouts, EOF, and temporary network
// errors. It never retries context cancellation or deadline-exceeded errors.
func DefaultShouldRetry(ctx context.Context, _ int, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// delay computes the sleep duration for attempt n (1-indexed, first retry = 1).
func (p Policy) delay(attempt int) time.Duration {
	if p.InitialInterval <= 0 {
		return 0
	}
	d := float64(p.InitialInterval) * math.Pow(p.BackoffFactor, float64(attempt-1))
	if p.MaxInterval > 0 && d > float64(p.MaxInterval) {
		d = float64(p.MaxInterval)
	}
	if p.Jitter && d > 0 {
		d += rand.Float64() * d * 0.2 //nolint:gosec
	}
	return time.Duration(d)
}

// shouldRetry delegates to the configured predicate or DefaultShouldRetry.
func (p Policy) shouldRetry(ctx context.Context, attempt int, err error) bool {
	fn := p.ShouldRetry
	if fn == nil {
		fn = DefaultShouldRetry
	}
	return fn(ctx, attempt, err)
}

// Wrap returns a new fantasy.AgentTool that automatically retries the wrapped
// tool according to p on transient failures.  Non-retriable errors and tool
// responses where IsError is true are returned immediately without retrying.
func Wrap(inner fantasy.AgentTool, p Policy) fantasy.AgentTool {
	if p.MaxAttempts < 2 {
		return inner // retry disabled
	}
	return &retryTool{inner: inner, policy: p}
}

type retryTool struct {
	inner  fantasy.AgentTool
	policy Policy
}

// Unwrap returns the inner tool so that capability.As[T] can walk the chain.
func (r *retryTool) Unwrap() fantasy.AgentTool { return r.inner }

func (r *retryTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var (
		resp fantasy.ToolResponse
		err  error
	)
	for attempt := 1; attempt <= r.policy.MaxAttempts; attempt++ {
		resp, err = r.inner.Run(ctx, call)

		// Success (no exec error): return immediately.
		if err == nil {
			return resp, nil
		}

		// If the context is done, stop immediately.
		if ctx.Err() != nil {
			return resp, err
		}

		// Check whether to retry.
		if attempt == r.policy.MaxAttempts || !r.policy.shouldRetry(ctx, attempt, err) {
			return resp, err
		}

		// Wait before next attempt.
		if d := r.policy.delay(attempt); d > 0 {
			select {
			case <-ctx.Done():
				return resp, ctx.Err()
			case <-time.After(d):
			}
		}
	}
	return resp, err
}

// Info delegates to the inner tool so that tool metadata is preserved.
func (r *retryTool) Info() fantasy.ToolInfo { return r.inner.Info() }

// ProviderOptions delegates to the inner tool.
func (r *retryTool) ProviderOptions() fantasy.ProviderOptions { return r.inner.ProviderOptions() }

// SetProviderOptions delegates to the inner tool.
func (r *retryTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	r.inner.SetProviderOptions(opts)
}
