package search

import "context"

// ProviderV2 is the contract every web search provider implements. The
// IsAvailable / IsExplicitlyAvailable pair mirrors the dual probing
// semantics of the upstream TypeScript chain: strict admission for the
// automatic fallback order and a lenient check when the user explicitly
// selected the provider.
type ProviderV2 interface {
	// ID returns the stable provider identifier (e.g. "exa").
	ID() string

	// Label returns the human-readable display name.
	Label() string

	// IsAvailable reports whether the provider can service a request right
	// now. It drives automatic chain admission; unavailable providers are
	// recorded as failures and skipped. Implementations must consult the
	// passed broker exclusively and never a sibling credential store.
	IsAvailable(ctx context.Context, broker CredentialBroker) (bool, error)

	// IsExplicitlyAvailable reports whether the provider should run when
	// the user explicitly selected it, even if IsAvailable would reject it
	// for the automatic chain (e.g. providers with an unauthenticated
	// fallback). Implementations without such a fallback should mirror
	// IsAvailable.
	IsExplicitlyAvailable(ctx context.Context, broker CredentialBroker) (bool, error)

	// Search executes one web search. Credentials must be resolved through
	// params.AuthStorage.
	Search(ctx context.Context, params SearchParams) (*SearchResponse, error)
}
