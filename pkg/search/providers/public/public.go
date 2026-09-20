// Package public implements the credential-free aggregate search
// provider: a fan-out over the free HTML engines (Startpage, Google,
// DuckDuckGo, Ecosia, Mojeek) with soft/hard deadlines, straggler
// abort, and cross-engine consensus ranking, mirroring the upstream
// TypeScript public web provider.
package public

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/providers/public/engines"
)

// Provider identity constants.
const (
	// ProviderID is the stable provider identifier.
	ProviderID = "public"

	// ProviderLabel is the human-readable display name.
	ProviderLabel = "Public Web"
)

// Aggregate defaults and bounds, mirroring the upstream provider.
// Aggregates get a wider default window than single engines because
// consensus needs breadth.
const (
	// DefaultSoftDeadline is the fan-out soft deadline: past this point
	// the aggregate returns as soon as it has at least one engine's
	// results. Fast HTML engines answer well under it; slower engines
	// are treated as bonus coverage rather than a latency floor.
	DefaultSoftDeadline = 5 * time.Second

	// DefaultHardDeadline bounds the whole fan-out so one
	// pathologically slow engine can never pin the request.
	DefaultHardDeadline = 30 * time.Second

	// defaultNumResults is the merged result count when the caller
	// gives none.
	defaultNumResults = 15

	// maxNumResults caps the merged result count.
	maxNumResults = 30
)

// PublicProvider aggregates every credential-free engine behind one
// search call: it fans the query out to the engine roster, races the
// soft/hard deadlines, aborts stragglers, and merges what arrived by
// cross-engine consensus. It is explicit-only — the automatic chain
// already walks the individual engines sequentially, so fanning out
// to all of them is a deliberate user choice, not a fallback,
// mirroring the upstream design. The provider needs no credentials;
// the broker is unused and may be nil.
type PublicProvider struct {
	client       *http.Client
	roster       []engines.SubEngine
	softDeadline time.Duration
	hardDeadline time.Duration
}

// Interface conformance check at compile time.
var _ search.ProviderV2 = (*PublicProvider)(nil)

// New creates an aggregate over the default engine roster and the
// default deadlines, defaulting to a plain client when nil.
func New(client *http.Client) *PublicProvider {
	return NewWithConfig(client, nil, 0, 0)
}

// NewWithConfig creates an aggregate with an explicit engine roster
// and deadlines; a nil roster, a zero soft, or a zero hard deadline
// falls back to the respective default. This constructor is the test
// seam for deadline and engine injection.
func NewWithConfig(client *http.Client, roster []engines.SubEngine, softDeadline, hardDeadline time.Duration) *PublicProvider {
	if client == nil {
		client = &http.Client{}
	}
	if len(roster) == 0 {
		roster = engines.DefaultEngines()
	}
	if softDeadline <= 0 {
		softDeadline = DefaultSoftDeadline
	}
	if hardDeadline <= 0 {
		hardDeadline = DefaultHardDeadline
	}
	return &PublicProvider{
		client:       client,
		roster:       roster,
		softDeadline: softDeadline,
		hardDeadline: hardDeadline,
	}
}

// Meta returns the registry metadata for this provider. The priority
// trails every automatic chain member because the aggregate is
// explicit-only. Registration is explicit; there is no init hook, so
// importing the package never mutates a registry.
func Meta() search.ProviderMeta {
	return search.ProviderMeta{
		ID:       ProviderID,
		Label:    ProviderLabel,
		Priority: 60,
		NewFunc:  func() search.ProviderV2 { return New(nil) },
	}
}

// ID implements search.ProviderV2.
func (p *PublicProvider) ID() string { return ProviderID }

// Label implements search.ProviderV2.
func (p *PublicProvider) Label() string { return ProviderLabel }

// IsAvailable implements search.ProviderV2: the aggregate never joins
// the automatic chain — the chain already walks the individual
// engines sequentially, and fanning out to all of them is a deliberate
// user choice. The broker is unused and may be nil.
func (p *PublicProvider) IsAvailable(ctx context.Context, broker search.CredentialBroker) (bool, error) {
	return false, nil
}

// IsExplicitlyAvailable implements search.ProviderV2: an explicit user
// selection may always fan out. The broker is unused and may be nil.
func (p *PublicProvider) IsExplicitlyAvailable(ctx context.Context, broker search.CredentialBroker) (bool, error) {
	return true, nil
}

// Search implements search.ProviderV2. Individual engine failures are
// tolerated; the call fails with 503 semantics when every engine
// fails, and with the chain-level no-content semantics (204) when
// nothing merged but not everyone failed.
func (p *PublicProvider) Search(ctx context.Context, params search.SearchParams) (*search.SearchResponse, error) {
	if strings.TrimSpace(params.Query) == "" {
		return nil, &search.SearchProviderError{
			Provider: ProviderID,
			Message:  "empty query",
		}
	}
	if len(p.roster) == 0 {
		return nil, &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeServerError,
			Message:    "no public engines configured",
			HTTPStatus: 503,
		}
	}

	outcome := fanoutEngines(ctx, p.client, p.roster, params.Query,
		resolveEngineLimit(params), p.softDeadline, p.hardDeadline)
	sources := rankSources(outcome.perEngine, clampAggregateResults(params))

	if len(sources) > 0 {
		return &search.SearchResponse{Provider: ProviderID, Sources: sources}, nil
	}
	if len(outcome.failures) == len(p.roster) {
		return nil, &search.SearchProviderError{
			Provider:   ProviderID,
			Code:       search.ErrorCodeServerError,
			Message:    "All public engines failed: " + formatEngineFailures(outcome.failures),
			HTTPStatus: 503,
		}
	}
	return nil, &search.SearchProviderError{
		Provider:   ProviderID,
		Message:    "Public Web returned no renderable search content.",
		HTTPStatus: 204,
	}
}

// resolveEngineLimit extracts the caller's raw result hint for the
// engines: NumSearchResults wins over Limit and 0 means unset. Each
// engine applies its own default and cap, mirroring the upstream
// pass-through parameters.
func resolveEngineLimit(params search.SearchParams) int {
	if params.NumSearchResults != 0 {
		return params.NumSearchResults
	}
	return params.Limit
}

// clampAggregateResults resolves the merged result count: absent
// values mean the aggregate default and the count is clamped to
// [1, max], mirroring the upstream clampNumResults helper.
func clampAggregateResults(params search.SearchParams) int {
	value := resolveEngineLimit(params)
	if value == 0 {
		return defaultNumResults
	}
	if value > maxNumResults {
		return maxNumResults
	}
	if value < 1 {
		return 1
	}
	return value
}

// formatEngineFailures renders the per-engine failure lines for the
// all-engines-failed error.
func formatEngineFailures(failures []engineFailure) string {
	lines := make([]string, 0, len(failures))
	for _, failure := range failures {
		lines = append(lines, fmt.Sprintf("%s: %v", failure.engineID, failure.err))
	}
	return strings.Join(lines, "; ")
}
