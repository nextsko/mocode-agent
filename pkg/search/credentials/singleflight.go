package credentials

import (
	"context"

	"golang.org/x/sync/singleflight"
)

// singleflightGroup merges concurrent calls that share a key into a single
// execution of the underlying function. Joined callers observe the first
// caller's context: if that context is cancelled, the whole shared flight
// fails.
type singleflightGroup struct {
	group singleflight.Group
}

// do executes fn once for all concurrent callers of key and returns the
// shared result.
func (g *singleflightGroup) do(ctx context.Context, key string, fn func(context.Context) (string, error)) (string, error) {
	value, err, _ := g.group.Do(key, func() (any, error) {
		return fn(ctx)
	})
	if err != nil {
		return "", err
	}
	token, _ := value.(string)
	return token, nil
}

// forget discards the in-flight call state for key so the next do starts a
// fresh execution instead of joining a stale flight.
func (g *singleflightGroup) forget(key string) {
	g.group.Forget(key)
}
