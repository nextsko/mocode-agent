// Package failover wraps a primary fantasy.LanguageModel with one or more
// fallback models. When the primary fails, requests are retried against the
// fallbacks before the first chunk of output is produced, mirroring the
// failover-before-first-chunk strategy used by trpc-agent-go.
//
// This is the only model-layer resilience capability worth borrowing from
// trpc-agent-go for mocode: it lets a primary provider (e.g. the configured
// large model) fail over to a backup when it errors, without touching the
// catwalk/fantasy provider abstraction.
package failover

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"charm.land/fantasy"
)

// LanguageModel is a failover-aware fantasy.LanguageModel.
type LanguageModel struct {
	primary   fantasy.LanguageModel
	fallbacks []fantasy.LanguageModel
}

// Option configures a failover LanguageModel.
type Option func(*options)

type options struct {
	primary   fantasy.LanguageModel
	fallbacks []fantasy.LanguageModel
}

// WithPrimary sets the primary model tried first.
func WithPrimary(m fantasy.LanguageModel) Option {
	return func(o *options) { o.primary = m }
}

// WithFallback adds a fallback model, tried in order after the primary.
// May be called multiple times.
func WithFallback(m fantasy.LanguageModel) Option {
	return func(o *options) { o.fallbacks = append(o.fallbacks, m) }
}

// New builds a failover model. The primary is required; at least one fallback
// is required (otherwise there is nothing to fail over to).
func New(opts ...Option) (*LanguageModel, error) {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	if o.primary == nil {
		return nil, errors.New("failover: primary model is required")
	}
	if len(o.fallbacks) == 0 {
		return nil, errors.New("failover: at least one fallback model is required")
	}
	for i, f := range o.fallbacks {
		if f == nil {
			return nil, fmt.Errorf("failover: fallback model at index %d is nil", i)
		}
	}
	return &LanguageModel{primary: o.primary, fallbacks: o.fallbacks}, nil
}

// MustNew is like New but panics on error. Intended for package-level
// declarations where the inputs are statically known.
func MustNew(opts ...Option) *LanguageModel {
	m, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return m
}

// Provider returns the primary model's provider identifier.
func (m *LanguageModel) Provider() string { return m.primary.Provider() }

// Model returns the primary model's name.
func (m *LanguageModel) Model() string { return m.primary.Model() }

// candidates returns primary followed by fallbacks.
func (m *LanguageModel) candidates() []fantasy.LanguageModel {
	out := make([]fantasy.LanguageModel, 0, 1+len(m.fallbacks))
	out = append(out, m.primary)
	out = append(out, m.fallbacks...)
	return out
}

// Generate tries each candidate in order, returning the first successful
// response. A candidate "fails" if Generate returns an error. If every
// candidate errors, the last error is returned wrapped with the tried count.
func (m *LanguageModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	var lastErr error
	for i, c := range m.candidates() {
		resp, err := c.Generate(ctx, call)
		if err == nil {
			return resp, nil
		}
		lastErr = fmt.Errorf("candidate %d (%s/%s): %w", i, c.Provider(), c.Model(), err)
	}
	return nil, fmt.Errorf("failover: all %d candidates failed; last error: %w", 1+len(m.fallbacks), lastErr)
}

// GenerateObject mirrors Generate for structured-output generation.
func (m *LanguageModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	var lastErr error
	for i, c := range m.candidates() {
		resp, err := c.GenerateObject(ctx, call)
		if err == nil {
			return resp, nil
		}
		lastErr = fmt.Errorf("candidate %d (%s/%s): %w", i, c.Provider(), c.Model(), err)
	}
	return nil, fmt.Errorf("failover: all %d candidates failed; last error: %w", 1+len(m.fallbacks), lastErr)
}

// Stream returns a streaming sequence. Failover happens before the first
// non-error part: if a candidate's Stream call returns an error, or its first
// emitted part is an error part, the next candidate is tried. Once a candidate
// emits a non-error part, it owns the rest of the stream.
func (m *LanguageModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	return m.stream(ctx, call, func(c fantasy.LanguageModel, ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
		return c.Stream(ctx, call)
	})
}

// StreamObject mirrors Stream for structured-output streaming.
func (m *LanguageModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.streamObject(ctx, call, func(c fantasy.LanguageModel, ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
		return c.StreamObject(ctx, call)
	})
}

// stream implements the failover-before-first-non-error-chunk algorithm for
// text streaming. It peeks the first part of each candidate; an error setup or
// an error first-part advances to the next candidate, otherwise it commits and
// replays the peeked part before yielding the remainder.
func (m *LanguageModel) stream(
	ctx context.Context,
	call fantasy.Call,
	open func(fantasy.LanguageModel, context.Context, fantasy.Call) (fantasy.StreamResponse, error),
) (fantasy.StreamResponse, error) {
	var (
		peeked   fantasy.StreamPart
		committed iter.Seq[fantasy.StreamPart]
		lastErr   error
	)
	for _, c := range m.candidates() {
		seq, err := open(c, ctx, call)
		if err != nil {
			lastErr = fmt.Errorf("%s/%s: %w", c.Provider(), c.Model(), err)
			continue
		}
		// Peek the first part. Use a capture to defer the rest.
		var rest []fantasy.StreamPart
		gotFirst := false
		isError := false
		seq(func(p fantasy.StreamPart) bool {
			if !gotFirst {
				peeked = p
				gotFirst = true
				isError = p.Type == fantasy.StreamPartTypeError
				// Stop draining; we'll decide whether to commit.
				return false
			}
			rest = append(rest, p)
			return true
		})
		if !gotFirst {
			// Sequence ended with no parts. Treat as success (empty stream).
			return func(yield func(fantasy.StreamPart) bool) {}, nil
		}
		if isError {
			lastErr = fmt.Errorf("%s/%s: stream error part", c.Provider(), c.Model())
			continue
		}
		// Commit to this candidate: yield peeked, then buffered rest.
		committed = seq
		buf := rest
		return func(yield func(fantasy.StreamPart) bool) {
			if !yield(peeked) {
				return
			}
			for _, p := range buf {
				if !yield(p) {
					return
				}
			}
			// Drain any remaining parts not yet consumed during peek.
			committed(func(p fantasy.StreamPart) bool {
				return yield(p)
			})
		}, nil
	}
	return nil, fmt.Errorf("failover: all %d stream candidates failed; last error: %w", 1+len(m.fallbacks), lastErr)
}

// streamObject is the structured-output analogue of stream.
func (m *LanguageModel) streamObject(
	ctx context.Context,
	call fantasy.ObjectCall,
	open func(fantasy.LanguageModel, context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error),
) (fantasy.ObjectStreamResponse, error) {
	var lastErr error
	for _, c := range m.candidates() {
		seq, err := open(c, ctx, call)
		if err != nil {
			lastErr = fmt.Errorf("%s/%s: %w", c.Provider(), c.Model(), err)
			continue
		}
		// For object streaming we commit on a successful open (the structured
		// object protocol does not surface a discrete error-first part to peek).
		return seq, nil
	}
	return nil, fmt.Errorf("failover: all %d object-stream candidates failed; last error: %w", 1+len(m.fallbacks), lastErr)
}

// Compile-time assertion that LanguageModel satisfies fantasy.LanguageModel.
var _ fantasy.LanguageModel = (*LanguageModel)(nil)
