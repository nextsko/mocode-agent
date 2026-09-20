package search

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Sentinel errors returned by Chain.Execute.
var (
	// ErrNoProviderConfigured is returned when the chain has no candidates
	// to try (empty registry or fully blacklisted).
	ErrNoProviderConfigured = errors.New("no web search provider configured")

	// ErrAllProvidersFailed is returned when every candidate failed; the
	// wrapped message lists each provider failure.
	ErrAllProvidersFailed = errors.New("all web search providers failed")
)

// Chain timing bounds, mirroring the upstream TypeScript dispatcher.
const (
	// DefaultChainTimeout bounds one Execute call when Timeout is unset.
	DefaultChainTimeout = 60 * time.Second

	// MaxChainTimeout caps a configured Timeout.
	MaxChainTimeout = 5 * time.Minute
)

// Chain executes a web search across ordered providers, falling through on
// failure until one produces a renderable response.
type Chain struct {
	// Registry supplies and lazily loads candidate providers.
	Registry *Registry

	// Broker is the credential source handed to providers: it drives
	// availability probes and is injected into params.AuthStorage when the
	// caller did not set one. May be nil for credential-free chains.
	Broker CredentialBroker

	// ForcedID, when set, restricts execution to that single provider and
	// probes it with IsExplicitlyAvailable. Explicit user selection
	// ignores the registry blacklist.
	ForcedID string

	// Timeout bounds one Execute call; zero means DefaultChainTimeout and
	// values above MaxChainTimeout are clamped.
	Timeout time.Duration
}

// Execute walks the candidate providers in order: load the singleton, probe
// availability, search, and return the first renderable response. A response
// is renderable when it has a non-empty answer or at least one source.
// Unavailable or failing providers are recorded as failures and the chain
// falls through; when every candidate fails, Execute returns an error
// wrapping ErrAllProvidersFailed with the aggregated failure report.
func (c *Chain) Execute(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	if c.Registry == nil {
		return nil, errors.New("search: chain has no registry")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	if params.AuthStorage == nil {
		params.AuthStorage = c.Broker
	}

	var failures []Failure
	for _, id := range c.candidates() {
		provider, err := c.Registry.Load(id)
		if err != nil {
			failures = append(failures, Failure{Provider: id, Err: err})
			continue
		}

		available, err := c.probe(ctx, provider)
		if err != nil {
			failures = append(failures, Failure{Provider: id, Err: err})
			continue
		}
		if !available {
			failures = append(failures, Failure{
				Provider: id,
				Err:      errors.New("provider unavailable"),
			})
			continue
		}

		resp, err := provider.Search(ctx, params)
		if err != nil {
			failures = append(failures, Failure{Provider: id, Err: err})
			continue
		}
		if !hasContent(resp) {
			failures = append(failures, Failure{
				Provider: id,
				Err:      errors.New("no renderable search content"),
			})
			continue
		}
		return resp, nil
	}

	if len(failures) == 0 {
		return nil, ErrNoProviderConfigured
	}
	return nil, fmt.Errorf("%w: %s", ErrAllProvidersFailed, formatFailures(failures))
}

// candidates returns the ordered provider IDs for this execution: just the
// forced provider when one is set, otherwise the registry order.
func (c *Chain) candidates() []string {
	if c.ForcedID != "" {
		return []string{c.ForcedID}
	}
	return c.Registry.Order()
}

// probe checks provider availability, using the lenient explicit probe for
// a forced provider and the strict probe for the automatic chain.
func (c *Chain) probe(ctx context.Context, provider ProviderV2) (bool, error) {
	if c.ForcedID != "" {
		return provider.IsExplicitlyAvailable(ctx, c.Broker)
	}
	return provider.IsAvailable(ctx, c.Broker)
}

// withTimeout applies the configured chain timeout with default and cap.
func (c *Chain) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultChainTimeout
	}
	if timeout > MaxChainTimeout {
		timeout = MaxChainTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

// hasContent reports whether a response carries renderable content, per the
// chain success contract: a non-empty answer or at least one source.
func hasContent(resp *SearchResponse) bool {
	return resp != nil && (resp.Answer != "" || len(resp.Sources) > 0)
}
