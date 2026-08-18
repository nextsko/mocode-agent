package sshcommon

import (
	"context"
	"sync"
	"time"
)

// Service is the shared state for every SSH tool.
//
// A single Service owns one Pool and one Resolver, both of which are
// goroutine-safe.  Pass the same Service to every NewSsh*Tool factory
// in your process so connections are reused.
type Service struct {
	mu       sync.Mutex
	pool     *Pool
	resolver *Resolver
}

// NewService returns an unstarted Service.  The pool and resolver are
// created lazily on first use so an empty Service has no side-effects.
func NewService() *Service {
	return &Service{}
}

// Pool returns the underlying connection pool, initialising it on the
// first call.  Always returns the same instance.
func (s *Service) Pool() *Pool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool == nil {
		s.pool = NewPool(
			WithDialTimeout(10*time.Second),
			WithHandshakeTimeout(15*time.Second),
			WithMaxIdle(5*time.Minute),
		)
	}
	return s.pool
}

// Resolver returns the host resolver, initialising it on the first call.
func (s *Service) Resolver() *Resolver {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resolver == nil {
		s.resolver = NewResolver(nil)
	}
	return s.resolver
}

// Close shuts down the pool.  Safe to call on a nil/empty Service.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool == nil {
		return nil
	}
	err := s.pool.Close()
	s.pool = nil
	return err
}

// ─── shared helpers used by the four tools ─────────────────────────────────

// AcquireClient resolves a host string and pulls a Client from the pool.
func (s *Service) AcquireClient(ctx context.Context, host string) (*Client, HostSpec, error) {
	spec, err := s.Resolver().Resolve(host)
	if err != nil {
		return nil, HostSpec{}, err
	}
	c, err := s.Pool().Get(spec)
	if err != nil {
		return nil, spec, err
	}
	return c, spec, nil
}
