package search

import (
	"context"
	"errors"
)

// ErrCredentialNotFound is the sentinel error returned by CredentialBroker
// implementations when no configured source (environment or backing store)
// can supply the requested credential. Callers detect it with errors.Is.
var ErrCredentialNotFound = errors.New("credential not found")

// CredentialBroker is the only credential source web search providers may
// consult. Centralizing resolution keeps environment overrides, persisted
// credentials, single-flight refresh, and forced invalidation coherent in
// one place instead of racing per-provider refresh helpers.
type CredentialBroker interface {
	// Resolve returns the credential for providerID, consulting cheaper
	// sources first. It returns an error wrapping ErrCredentialNotFound
	// when every source misses.
	Resolve(ctx context.Context, providerID string, opts ResolveOpts) (string, error)

	// Refresh force-invalidates any cached credential for providerID and
	// re-fetches it, merging concurrent refreshes into one upstream call.
	// It is used after a provider reports an authorization failure.
	Refresh(ctx context.Context, providerID string, opts ResolveOpts) (string, error)
}

// ResolveOpts parameterizes a single credential lookup.
type ResolveOpts struct {
	// EnvKey names the environment variable backing the provider ("" when
	// the provider has no environment credential).
	EnvKey string

	// AuthID identifies the credential row in the backing auth store (""
	// when the provider has no persisted credential).
	AuthID string

	// SessionID scopes credential round-robin and sticky selection.
	SessionID string

	// OnMiss, when non-nil, is invoked once per source that failed to
	// supply a credential (e.g. "env", "db") for metrics collection.
	OnMiss func(source string)
}
