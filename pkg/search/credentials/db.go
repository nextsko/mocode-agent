package credentials

import (
	"context"
	"fmt"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// missSourceDB is the source name reported to OnMiss by DBBroker.
const missSourceDB = "db"

// AuthStore is the persistence port behind DBBroker. It is deliberately
// narrow so any backing implementation (keyring, SQLite, remote service)
// can adapt; DBBroker never imports a concrete storage package.
type AuthStore interface {
	// GetAuthToken returns the persisted token for authID, or ("", nil)
	// when no credential is stored. Real infrastructure failures return a
	// non-nil error, which DBBroker propagates as-is instead of treating
	// it as a miss.
	GetAuthToken(ctx context.Context, authID string) (string, error)
}

// RefreshHook re-issues a credential (e.g. an OAuth token exchange) and
// persists it through the store before returning the fresh value.
type RefreshHook func(ctx context.Context, authID string) (string, error)

// DBBroker resolves credentials from an AuthStore port. Callers that pair
// it with MultiBroker get memoization and single-flight refresh for free;
// standalone use hits the store on every Resolve.
type DBBroker struct {
	store AuthStore
	// refreshHook, when set, backs Refresh with a forced re-issue. Without
	// it Refresh degrades to a store re-read. Configure it before the
	// broker is used concurrently.
	refreshHook RefreshHook
}

// Compile-time interface conformance check.
var _ search.CredentialBroker = (*DBBroker)(nil)

// NewDBBroker creates a store-backed broker. store may be nil, in which
// case every lookup misses.
func NewDBBroker(store AuthStore) *DBBroker {
	return &DBBroker{store: store}
}

// SetRefreshHook installs the force-refresh hook. It must be called before
// the broker is shared across goroutines.
func (b *DBBroker) SetRefreshHook(hook RefreshHook) {
	b.refreshHook = hook
}

// Resolve reads the credential named by opts.AuthID from the store. It
// reports a miss (an error wrapping search.ErrCredentialNotFound) on empty
// AuthID, a nil store or broker, a missing row, or a blank token; genuine
// store failures are wrapped and returned without the sentinel.
func (b *DBBroker) Resolve(ctx context.Context, _ string, opts search.ResolveOpts) (string, error) {
	if b == nil || b.store == nil || opts.AuthID == "" {
		miss(opts, missSourceDB)
		return "", fmt.Errorf("db broker: %w", search.ErrCredentialNotFound)
	}
	token, err := b.store.GetAuthToken(ctx, opts.AuthID)
	if err != nil {
		return "", fmt.Errorf("db broker: get auth %q: %w", opts.AuthID, err)
	}
	if token == "" {
		miss(opts, missSourceDB)
		return "", fmt.Errorf("db broker: auth %q: %w", opts.AuthID, search.ErrCredentialNotFound)
	}
	return token, nil
}

// Refresh force-fetches a fresh credential through the installed hook, or
// falls back to a store re-read when no hook is configured.
func (b *DBBroker) Refresh(ctx context.Context, providerID string, opts search.ResolveOpts) (string, error) {
	if b == nil || b.refreshHook == nil {
		return b.Resolve(ctx, providerID, opts)
	}
	if opts.AuthID == "" {
		miss(opts, missSourceDB)
		return "", fmt.Errorf("db broker: %w", search.ErrCredentialNotFound)
	}
	token, err := b.refreshHook(ctx, opts.AuthID)
	if err != nil {
		return "", fmt.Errorf("db broker: refresh auth %q: %w", opts.AuthID, err)
	}
	if token == "" {
		miss(opts, missSourceDB)
		return "", fmt.Errorf("db broker: refresh auth %q: %w", opts.AuthID, search.ErrCredentialNotFound)
	}
	return token, nil
}
