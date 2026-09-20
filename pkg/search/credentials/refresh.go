package credentials

import (
	"context"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// Refresh implements the force-refresh protocol: environment credentials
// are returned as-is (they are static), while store-backed credentials have
// their cache entry invalidated, any in-flight resolve forgotten, and a
// fresh value fetched under single-flight. Concurrent refreshes of the same
// credential collapse into one upstream call. It is the entry point for the
// "provider reported 401, force a new token" flow.
func (b *MultiBroker) Refresh(ctx context.Context, providerID string, opts search.ResolveOpts) (string, error) {
	if token, ok := b.resolveEnv(ctx, providerID, opts); ok {
		return token, nil
	}
	if b.db == nil {
		miss(opts, missSourceDB)
		return "", notFoundError(providerID)
	}

	key := credentialKey(providerID, opts.AuthID)
	b.cache.Delete(key)
	b.resolveSF.forget(key)
	token, err := b.refreshSF.do(ctx, key, func(ctx context.Context) (string, error) {
		return b.db.Refresh(ctx, providerID, opts)
	})
	if err != nil {
		return "", err
	}
	b.cache.Store(key, token)
	return token, nil
}
