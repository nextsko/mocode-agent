package credentials

import (
	"context"
	"fmt"
	"os"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// missSourceEnv is the source name reported to OnMiss by EnvBroker.
const missSourceEnv = "env"

// EnvBroker resolves credentials directly from environment variables. It is
// the cheapest source and holds no state, so Refresh is a plain re-read.
type EnvBroker struct{}

// Compile-time interface conformance check.
var _ search.CredentialBroker = (*EnvBroker)(nil)

// NewEnvBroker creates an environment-backed credential broker.
func NewEnvBroker() *EnvBroker {
	return &EnvBroker{}
}

// Resolve returns the environment variable named by opts.EnvKey. It reports
// a miss (an error wrapping search.ErrCredentialNotFound) when EnvKey is
// empty or the variable is unset or blank.
func (b *EnvBroker) Resolve(_ context.Context, _ string, opts search.ResolveOpts) (string, error) {
	value := b.lookup(opts)
	if value == "" {
		miss(opts, missSourceEnv)
		return "", fmt.Errorf("env broker: %w", search.ErrCredentialNotFound)
	}
	return value, nil
}

// Refresh re-reads the environment; environment credentials are static, so
// refresh is identical to resolve.
func (b *EnvBroker) Refresh(ctx context.Context, providerID string, opts search.ResolveOpts) (string, error) {
	return b.Resolve(ctx, providerID, opts)
}

// lookup reads the configured environment variable, returning "" when no
// key is configured.
func (b *EnvBroker) lookup(opts search.ResolveOpts) string {
	if opts.EnvKey == "" {
		return ""
	}
	return os.Getenv(opts.EnvKey)
}
