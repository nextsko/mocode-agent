// Package credentials implements the CredentialBroker contract defined in
// pkg/search: an environment-backed broker, a store-backed broker over the
// AuthStore port, and a MultiBroker that combines them (environment first,
// store second) with memoized resolves and single-flight force-refresh.
package credentials

import (
	"context"
	"fmt"
	"sync"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// credentialKeySep separates provider and auth IDs in cache and
// single-flight keys.
const credentialKeySep = "\x00"

// MultiBroker combines an EnvBroker and a DBBroker (either may be nil),
// resolving environment credentials first and falling back to the store.
// Store results are memoized; refreshes invalidate the cache and re-fetch
// under single-flight so concurrent callers trigger one upstream refresh.
type MultiBroker struct {
	env *EnvBroker
	db  *DBBroker

	cache     sync.Map // credentialKey -> resolved token
	resolveSF singleflightGroup
	refreshSF singleflightGroup
}

// Compile-time interface conformance check.
var _ search.CredentialBroker = (*MultiBroker)(nil)

// NewMultiBroker creates a broker that tries env first and db second.
// Either argument may be nil to disable that source.
func NewMultiBroker(env *EnvBroker, db *DBBroker) *MultiBroker {
	return &MultiBroker{env: env, db: db}
}

// Resolve returns the credential for providerID: environment first, then
// the memoized store value, then a single-flight store fetch. When every
// source misses it returns an error wrapping search.ErrCredentialNotFound.
func (b *MultiBroker) Resolve(ctx context.Context, providerID string, opts search.ResolveOpts) (string, error) {
	if token, ok := b.resolveEnv(ctx, providerID, opts); ok {
		return token, nil
	}
	if b.db == nil {
		miss(opts, missSourceDB)
		return "", notFoundError(providerID)
	}

	key := credentialKey(providerID, opts.AuthID)
	if cached, ok := b.cache.Load(key); ok {
		return cached.(string), nil
	}
	token, err := b.resolveSF.do(ctx, key, func(ctx context.Context) (string, error) {
		return b.db.Resolve(ctx, providerID, opts)
	})
	if err != nil {
		return "", err
	}
	b.cache.Store(key, token)
	return token, nil
}

// resolveEnv consults the environment source when configured, reporting
// whether it produced a value. Misses fall through to the store.
func (b *MultiBroker) resolveEnv(ctx context.Context, providerID string, opts search.ResolveOpts) (string, bool) {
	if b.env == nil {
		return "", false
	}
	token, err := b.env.Resolve(ctx, providerID, opts)
	if err != nil {
		return "", false
	}
	return token, true
}

// credentialKey builds the cache and single-flight key for a credential.
// The NUL separator keeps provider and auth IDs unambiguous.
func credentialKey(providerID, authID string) string {
	return providerID + credentialKeySep + authID
}

// notFoundError builds the aggregate all-sources-miss error.
func notFoundError(providerID string) error {
	return fmt.Errorf("multi broker %q: %w", providerID, search.ErrCredentialNotFound)
}

// miss reports a failed source to the OnMiss callback when set.
func miss(opts search.ResolveOpts, source string) {
	if opts.OnMiss != nil {
		opts.OnMiss(source)
	}
}
