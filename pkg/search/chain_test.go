package search

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// chainMockProvider is a configurable ProviderV2 used to drive Chain tests.
type chainMockProvider struct {
	id         string
	label      string
	available  bool
	explicitOK bool
	probeErr   error
	searchErr  error
	answer     string
	sources    []SearchSource
	blockOnCtx bool
	probes     atomic.Int32
	searches   atomic.Int32
	gotParams  SearchParams
}

// Compile-time interface conformance check.
var _ ProviderV2 = (*chainMockProvider)(nil)

func (p *chainMockProvider) ID() string    { return p.id }
func (p *chainMockProvider) Label() string { return p.label }

func (p *chainMockProvider) IsAvailable(_ context.Context, _ CredentialBroker) (bool, error) {
	p.probes.Add(1)
	if p.probeErr != nil {
		return false, p.probeErr
	}
	return p.available, nil
}

func (p *chainMockProvider) IsExplicitlyAvailable(_ context.Context, _ CredentialBroker) (bool, error) {
	p.probes.Add(1)
	if p.probeErr != nil {
		return false, p.probeErr
	}
	if p.explicitOK {
		return true, nil
	}
	return p.available, nil
}

func (p *chainMockProvider) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	p.searches.Add(1)
	p.gotParams = params
	if p.blockOnCtx {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.searchErr != nil {
		return nil, p.searchErr
	}
	return &SearchResponse{Provider: p.id, Answer: p.answer, Sources: p.sources}, nil
}

// chainFakeBroker is a no-op CredentialBroker for injection assertions.
type chainFakeBroker struct{}

// Compile-time interface conformance check.
var _ CredentialBroker = chainFakeBroker{}

func (chainFakeBroker) Resolve(context.Context, string, ResolveOpts) (string, error) {
	return "", ErrCredentialNotFound
}

func (chainFakeBroker) Refresh(context.Context, string, ResolveOpts) (string, error) {
	return "", ErrCredentialNotFound
}

// newTestChain builds a Chain over freshly registered mock providers.
func newTestChain(broker CredentialBroker, metas ...ProviderMeta) *Chain {
	reg := NewRegistry()
	for _, meta := range metas {
		if err := reg.Register(meta); err != nil {
			panic(fmt.Sprintf("register %q: %v", meta.ID, err))
		}
	}
	return &Chain{Registry: reg, Broker: broker}
}

// mockMeta wraps a mock provider into registerable metadata.
func mockMeta(p *chainMockProvider, priority int) ProviderMeta {
	return ProviderMeta{
		ID:       p.id,
		Label:    p.label,
		Priority: priority,
		NewFunc:  func() ProviderV2 { return p },
	}
}

func TestChainExecuteSingleSuccess(t *testing.T) {
	t.Parallel()
	p := &chainMockProvider{id: "exa", label: "Exa", available: true, answer: "hello"}
	chain := newTestChain(nil, mockMeta(p, 10))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "go generics"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "exa", resp.Provider)
	require.Equal(t, "hello", resp.Answer)
	require.EqualValues(t, 1, p.searches.Load())
	require.EqualValues(t, 1, p.probes.Load())
	require.Equal(t, "go generics", p.gotParams.Query)
}

func TestChainExecuteFallsBackOnFailure(t *testing.T) {
	t.Parallel()
	failing := &chainMockProvider{id: "brave", label: "Brave", available: true, searchErr: errors.New("boom")}
	working := &chainMockProvider{
		id:        "exa",
		label:     "Exa",
		available: true,
		answer:    "recovered",
		sources:   []SearchSource{{Title: "Doc", URL: "https://example.com"}},
	}
	chain := newTestChain(nil, mockMeta(failing, 10), mockMeta(working, 20))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.NoError(t, err)
	require.Equal(t, "exa", resp.Provider)
	require.EqualValues(t, 1, failing.searches.Load())
	require.EqualValues(t, 1, working.searches.Load())
}

func TestChainExecuteAllProvidersFail(t *testing.T) {
	t.Parallel()
	first := &chainMockProvider{id: "brave", label: "Brave", available: true, searchErr: errors.New("boom")}
	second := &chainMockProvider{id: "kagi", label: "Kagi", available: true, searchErr: errors.New("bang")}
	chain := newTestChain(nil, mockMeta(first, 10), mockMeta(second, 20))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.Nil(t, resp)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrAllProvidersFailed)
	require.Contains(t, err.Error(), "brave: boom")
	require.Contains(t, err.Error(), "kagi: bang")
}

func TestChainExecuteSkipsUnavailableProvider(t *testing.T) {
	t.Parallel()
	unavailable := &chainMockProvider{id: "kagi", label: "Kagi", available: false}
	working := &chainMockProvider{id: "exa", label: "Exa", available: true, answer: "ok"}
	chain := newTestChain(nil, mockMeta(unavailable, 10), mockMeta(working, 20))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.NoError(t, err)
	require.Equal(t, "exa", resp.Provider)
	require.EqualValues(t, 0, unavailable.searches.Load())
	require.EqualValues(t, 1, working.searches.Load())
}

func TestChainExecuteTreatsEmptyResponseAsFailure(t *testing.T) {
	t.Parallel()
	empty := &chainMockProvider{id: "mojeek", label: "Mojeek", available: true}
	working := &chainMockProvider{
		id:        "exa",
		label:     "Exa",
		available: true,
		sources:   []SearchSource{{Title: "Only sources", URL: "https://example.com"}},
	}
	chain := newTestChain(nil, mockMeta(empty, 10), mockMeta(working, 20))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.NoError(t, err)
	require.Equal(t, "exa", resp.Provider)
	require.EqualValues(t, 1, empty.searches.Load())
}

func TestChainExecuteProbeErrorRecorded(t *testing.T) {
	t.Parallel()
	flaky := &chainMockProvider{id: "kagi", label: "Kagi", probeErr: errors.New("probe blew up")}
	working := &chainMockProvider{id: "exa", label: "Exa", available: true, answer: "ok"}
	chain := newTestChain(nil, mockMeta(flaky, 10), mockMeta(working, 20))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.NoError(t, err)
	require.Equal(t, "exa", resp.Provider)
	require.EqualValues(t, 0, flaky.searches.Load())
}

func TestChainExecuteForcedUsesExplicitProbe(t *testing.T) {
	t.Parallel()
	forced := &chainMockProvider{id: "kagi", label: "Kagi", available: false, explicitOK: true, answer: "explicit"}
	var otherLoads atomic.Int32
	otherMeta := ProviderMeta{
		ID:       "exa",
		Label:    "Exa",
		Priority: 20,
		NewFunc: func() ProviderV2 {
			otherLoads.Add(1)
			return &chainMockProvider{id: "exa", label: "Exa", available: true}
		},
	}
	chain := newTestChain(nil, mockMeta(forced, 10), otherMeta)
	chain.ForcedID = "kagi"

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.NoError(t, err)
	require.Equal(t, "kagi", resp.Provider)
	require.EqualValues(t, 1, forced.searches.Load())
	require.EqualValues(t, 0, otherLoads.Load())
}

func TestChainExecuteForcedUnavailableFails(t *testing.T) {
	t.Parallel()
	forced := &chainMockProvider{id: "kagi", label: "Kagi", available: false, explicitOK: false}
	chain := newTestChain(nil, mockMeta(forced, 10))
	chain.ForcedID = "kagi"

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrAllProvidersFailed)
	require.EqualValues(t, 0, forced.searches.Load())
}

func TestChainExecuteUnknownForcedProviderFails(t *testing.T) {
	t.Parallel()
	chain := newTestChain(nil)
	chain.ForcedID = "ghost"

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrAllProvidersFailed)
}

func TestChainExecuteNoCandidates(t *testing.T) {
	t.Parallel()
	chain := newTestChain(nil)

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrNoProviderConfigured)
}

func TestChainExecuteInjectsBrokerIntoParams(t *testing.T) {
	t.Parallel()
	broker := chainFakeBroker{}
	p := &chainMockProvider{id: "exa", label: "Exa", available: true, answer: "ok"}
	chain := newTestChain(broker, mockMeta(p, 10))

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, p.gotParams.AuthStorage == broker)
}

func TestChainExecuteRespectsCallerAuthStorage(t *testing.T) {
	t.Parallel()
	broker := chainFakeBroker{}
	p := &chainMockProvider{id: "exa", label: "Exa", available: true, answer: "ok"}
	chain := newTestChain(chainFakeBroker{}, mockMeta(p, 10))

	_, err := chain.Execute(context.Background(), SearchParams{Query: "q", AuthStorage: broker})

	require.NoError(t, err)
	require.True(t, p.gotParams.AuthStorage == broker)
}

func TestChainExecuteAppliesTimeout(t *testing.T) {
	t.Parallel()
	p := &chainMockProvider{id: "exa", label: "Exa", available: true, blockOnCtx: true}
	chain := newTestChain(nil, mockMeta(p, 10))
	chain.Timeout = 50 * time.Millisecond

	resp, err := chain.Execute(context.Background(), SearchParams{Query: "q"})

	require.Nil(t, resp)
	require.ErrorIs(t, err, ErrAllProvidersFailed)
}
