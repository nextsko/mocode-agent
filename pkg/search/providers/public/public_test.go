package public

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
	"github.com/nextsko/mocode-agent/pkg/search/providers/public/engines"
)

// stubEngine is a scriptable SubEngine for fan-out tests: it observes
// the query and limit, waits out its delay unless cancelled first, and
// reports cancellation on a channel closed exactly once.
type stubEngine struct {
	id      string
	delay   time.Duration
	sources []search.SearchSource
	err     error

	cancelOnce sync.Once
	cancelled  chan struct{}

	mu      sync.Mutex
	query   string
	limit   int
	fetched bool
}

// newStubEngine builds a stub with the given scripted outcome.
func newStubEngine(id string, delay time.Duration, sources []search.SearchSource, err error) *stubEngine {
	return &stubEngine{
		id:        id,
		delay:     delay,
		sources:   sources,
		err:       err,
		cancelled: make(chan struct{}),
	}
}

// ID implements engines.SubEngine.
func (s *stubEngine) ID() string { return s.id }

// Fetch implements engines.SubEngine.
func (s *stubEngine) Fetch(ctx context.Context, _ *http.Client, queryString string, limit int) ([]search.SearchSource, error) {
	s.mu.Lock()
	s.query = queryString
	s.limit = limit
	s.fetched = true
	s.mu.Unlock()
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			s.cancelOnce.Do(func() { close(s.cancelled) })
			return nil, ctx.Err()
		}
	}
	return s.sources, s.err
}

// wasCancelled reports whether Fetch observed context cancellation.
func (s *stubEngine) wasCancelled() bool {
	select {
	case <-s.cancelled:
		return true
	default:
		return false
	}
}

// eventuallyCancelled waits for the straggler abort to reach the stub.
func (s *stubEngine) eventuallyCancelled(t *testing.T) {
	t.Helper()
	select {
	case <-s.cancelled:
		return
	case <-time.After(2 * time.Second):
		t.Fatalf("engine %s was never cancelled by the straggler abort", s.id)
	}
}

func TestMeta(t *testing.T) {
	t.Parallel()

	meta := Meta()
	require.Equal(t, ProviderID, meta.ID)
	require.Equal(t, ProviderLabel, meta.Label)
	require.NotNil(t, meta.NewFunc)
}

func TestAvailabilityIsExplicitOnly(t *testing.T) {
	t.Parallel()

	provider := New(nil)
	available, err := provider.IsAvailable(context.Background(), nil)
	require.NoError(t, err)
	require.False(t, available, "the aggregate never joins the automatic chain")

	explicit, err := provider.IsExplicitlyAvailable(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, explicit, "an explicit selection may always fan out")
}

func TestSearchMergesConsensusAndReturnsFast(t *testing.T) {
	t.Parallel()

	shared := []search.SearchSource{{Title: "Shared", URL: "https://shared.example.com/"}}
	onlyA := []search.SearchSource{{Title: "A", URL: "https://a.example.com/"}}
	roster := []engines.SubEngine{
		newStubEngine("e1", 0, append(append([]search.SearchSource{}, onlyA...), shared...), nil),
		newStubEngine("e2", 5*time.Millisecond, shared, nil),
		newStubEngine("e3", 0, shared, nil),
	}
	provider := NewWithConfig(nil, roster, time.Minute, 2*time.Minute)

	start := time.Now()
	resp, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Equal(t, ProviderID, resp.Provider)
	require.Empty(t, resp.Answer)
	require.Len(t, resp.Sources, 2)
	require.Equal(t, "https://shared.example.com/", resp.Sources[0].URL)
	// Every engine settled well before the soft deadline.
	require.Less(t, elapsed, time.Second)
}

func TestSearchPassesRawLimitToEngines(t *testing.T) {
	t.Parallel()

	stub := newStubEngine("e1", 0, []search.SearchSource{{Title: "S", URL: "https://s.example.com/"}}, nil)
	provider := NewWithConfig(nil, []engines.SubEngine{stub}, time.Minute, time.Minute)

	_, err := provider.Search(context.Background(), search.SearchParams{Query: "q", Limit: 7})
	require.NoError(t, err)
	require.Equal(t, 7, stub.limit)

	_, err = provider.Search(context.Background(), search.SearchParams{
		Query:            "q",
		Limit:            7,
		NumSearchResults: 3,
	})
	require.NoError(t, err)
	require.Equal(t, 3, stub.limit, "NumSearchResults wins over Limit")
}

func TestSearchSingleEngineFailureTolerated(t *testing.T) {
	t.Parallel()

	good := []search.SearchSource{{Title: "Good", URL: "https://good.example.com/"}}
	roster := []engines.SubEngine{
		newStubEngine("fast", 0, good, nil),
		newStubEngine("broken", 0, nil, &search.SearchProviderError{
			Provider:   "broken",
			Code:       search.ErrorCodeRateLimited,
			Message:    "429 rate limited",
			HTTPStatus: 429,
		}),
	}
	provider := NewWithConfig(nil, roster, time.Minute, time.Minute)

	resp, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	require.NoError(t, err)
	require.Len(t, resp.Sources, 1)
	require.Equal(t, "https://good.example.com/", resp.Sources[0].URL)
}

func TestSearchAllEnginesFailed(t *testing.T) {
	t.Parallel()

	boom := func(id string) *stubEngine {
		return newStubEngine(id, 0, nil, &search.SearchProviderError{
			Provider:   id,
			Code:       search.ErrorCodeServerError,
			Message:    "exploded",
			HTTPStatus: 500,
		})
	}
	roster := []engines.SubEngine{boom("a"), boom("b")}
	provider := NewWithConfig(nil, roster, time.Minute, time.Minute)

	_, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, ProviderID, spe.Provider)
	require.Equal(t, search.ErrorCodeServerError, spe.Code)
	require.Equal(t, 503, spe.HTTPStatus)
	require.Contains(t, spe.Message, "All public engines failed")
	require.Contains(t, spe.Message, "a: a: exploded")
	require.Contains(t, spe.Message, "b: b: exploded")
}

func TestSearchZeroResultsNotAllFailed(t *testing.T) {
	t.Parallel()

	roster := []engines.SubEngine{
		newStubEngine("empty", 0, nil, nil), // empty success
		newStubEngine("broken", 0, nil, &search.SearchProviderError{
			Provider: "broken",
			Message:  "challenge",
		}),
	}
	provider := NewWithConfig(nil, roster, time.Minute, time.Minute)

	_, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, 204, spe.HTTPStatus)
	require.Contains(t, spe.Message, "no renderable search content")
}

func TestSearchEmptyQuery(t *testing.T) {
	t.Parallel()

	provider := New(nil)
	_, err := provider.Search(context.Background(), search.SearchParams{Query: "   "})
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	require.Equal(t, ProviderID, spe.Provider)
	require.Equal(t, "empty query", spe.Message)
}

func TestSearchSoftDeadlineDropsStraggler(t *testing.T) {
	t.Parallel()

	fast := newStubEngine("fast", 5*time.Millisecond,
		[]search.SearchSource{{Title: "Fast", URL: "https://fast.example.com/"}}, nil)
	slow := newStubEngine("slow", 30*time.Second, nil, nil)
	provider := NewWithConfig(nil, []engines.SubEngine{fast, slow}, 150*time.Millisecond, time.Minute)

	start := time.Now()
	resp, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	elapsed := time.Since(start)
	require.NoError(t, err)
	// The soft deadline fired with one success in hand: the slow
	// engine's absence costs coverage, not latency.
	require.Len(t, resp.Sources, 1)
	require.Equal(t, "https://fast.example.com/", resp.Sources[0].URL)
	require.Less(t, elapsed, time.Second)
	slow.eventuallyCancelled(t)
}

func TestSearchSoftDeadlineWaitsForFirstSuccess(t *testing.T) {
	t.Parallel()

	late := newStubEngine("late", 250*time.Millisecond,
		[]search.SearchSource{{Title: "Late", URL: "https://late.example.com/"}}, nil)
	straggler := newStubEngine("straggler", 30*time.Second, nil, nil)
	provider := NewWithConfig(nil, []engines.SubEngine{late, straggler},
		40*time.Millisecond, time.Minute)

	start := time.Now()
	resp, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	elapsed := time.Since(start)
	require.NoError(t, err)
	// The soft deadline fired with nothing in hand, so the aggregate
	// kept waiting for the first success instead of returning empty.
	require.Len(t, resp.Sources, 1)
	require.Equal(t, "https://late.example.com/", resp.Sources[0].URL)
	require.GreaterOrEqual(t, elapsed, 200*time.Millisecond)
	require.Less(t, elapsed, 2*time.Second)
	straggler.eventuallyCancelled(t)
}

func TestSearchHardDeadlineCancelsEverything(t *testing.T) {
	t.Parallel()

	a := newStubEngine("a", 30*time.Second, nil, nil)
	b := newStubEngine("b", 30*time.Second, nil, nil)
	provider := NewWithConfig(nil, []engines.SubEngine{a, b},
		40*time.Millisecond, 250*time.Millisecond)

	start := time.Now()
	_, err := provider.Search(context.Background(), search.SearchParams{Query: "go"})
	elapsed := time.Since(start)
	require.Error(t, err)
	var spe *search.SearchProviderError
	require.ErrorAs(t, err, &spe)
	// Nothing merged and not every engine settled, so the aggregate
	// surfaces the chain-level no-content semantics.
	require.Equal(t, 204, spe.HTTPStatus)
	require.Less(t, elapsed, 2*time.Second)
	a.eventuallyCancelled(t)
	b.eventuallyCancelled(t)
}

func TestSearchParentCancellationPropagates(t *testing.T) {
	t.Parallel()

	a := newStubEngine("a", 30*time.Second, nil, nil)
	provider := NewWithConfig(nil, []engines.SubEngine{a}, time.Minute, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := provider.Search(ctx, search.SearchParams{Query: "go"})
	elapsed := time.Since(start)
	require.Error(t, err)
	require.Less(t, elapsed, 2*time.Second)
	a.eventuallyCancelled(t)
}
