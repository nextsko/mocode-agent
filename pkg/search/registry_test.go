package search

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubProvider is a minimal ProviderV2 satisfying registry tests.
type stubProvider struct{ id string }

// Compile-time interface conformance check.
var _ ProviderV2 = (*stubProvider)(nil)

func (p *stubProvider) ID() string    { return p.id }
func (p *stubProvider) Label() string { return p.id }

func (p *stubProvider) IsAvailable(context.Context, CredentialBroker) (bool, error) {
	return true, nil
}

func (p *stubProvider) IsExplicitlyAvailable(context.Context, CredentialBroker) (bool, error) {
	return true, nil
}

func (p *stubProvider) Search(context.Context, SearchParams) (*SearchResponse, error) {
	return nil, fmt.Errorf("stub %s: search not implemented", p.id)
}

func TestRegistryRegistrationIsLazy(t *testing.T) {
	t.Parallel()
	var constructions atomic.Int32
	reg := NewRegistry()
	require.NoError(t, reg.Register(ProviderMeta{
		ID:       "exa",
		Label:    "Exa",
		Priority: 10,
		NewFunc: func() ProviderV2 {
			constructions.Add(1)
			return &stubProvider{id: "exa"}
		},
	}))

	require.Equal(t, []string{"exa"}, reg.Order())
	require.EqualValues(t, 0, constructions.Load()) // lazy: register never constructs
}

func TestRegistryLoadIsSingleton(t *testing.T) {
	t.Parallel()
	var constructions atomic.Int32
	reg := NewRegistry()
	require.NoError(t, reg.Register(ProviderMeta{
		ID:    "exa",
		Label: "Exa",
		NewFunc: func() ProviderV2 {
			constructions.Add(1)
			return &stubProvider{id: "exa"}
		},
	}))

	first, err := reg.Load("exa")
	require.NoError(t, err)
	second, err := reg.Load("exa")
	require.NoError(t, err)

	require.True(t, first == second)
	require.EqualValues(t, 1, constructions.Load())
}

func TestRegistryLoadConcurrentSingleton(t *testing.T) {
	t.Parallel()
	var constructions atomic.Int32
	reg := NewRegistry()
	require.NoError(t, reg.Register(ProviderMeta{
		ID:    "exa",
		Label: "Exa",
		NewFunc: func() ProviderV2 {
			constructions.Add(1)
			return &stubProvider{id: "exa"}
		},
	}))

	const n = 10
	instances := make([]ProviderV2, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			instances[i], errs[i] = reg.Load("exa")
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, constructions.Load())
	for _, p := range instances {
		require.True(t, p == instances[0])
	}
}

func TestRegistryLoadUnknownProvider(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	p, err := reg.Load("ghost")

	require.Nil(t, p)
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown provider "ghost"`)
}

func TestRegistryOrderSortsByPriority(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	for _, m := range []ProviderMeta{
		{ID: "brave", Label: "Brave", Priority: 30, NewFunc: func() ProviderV2 { return &stubProvider{id: "brave"} }},
		{ID: "exa", Label: "Exa", Priority: 10, NewFunc: func() ProviderV2 { return &stubProvider{id: "exa"} }},
		{ID: "tavily", Label: "Tavily", Priority: 20, NewFunc: func() ProviderV2 { return &stubProvider{id: "tavily"} }},
	} {
		require.NoError(t, reg.Register(m))
	}

	require.Equal(t, []string{"exa", "tavily", "brave"}, reg.Order())
}

func TestRegistryOrderTieBreaksByRegistration(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	for _, m := range []ProviderMeta{
		{ID: "first", Priority: 10, NewFunc: func() ProviderV2 { return &stubProvider{id: "first"} }},
		{ID: "second", Priority: 10, NewFunc: func() ProviderV2 { return &stubProvider{id: "second"} }},
		{ID: "zeroth", Priority: 5, NewFunc: func() ProviderV2 { return &stubProvider{id: "zeroth"} }},
	} {
		require.NoError(t, reg.Register(m))
	}

	require.Equal(t, []string{"zeroth", "first", "second"}, reg.Order())
}

func TestRegistryOrderAndExcludedBlacklist(t *testing.T) {
	t.Parallel()
	reg := NewRegistry("tavily")
	for _, m := range []ProviderMeta{
		{ID: "exa", Priority: 10, NewFunc: func() ProviderV2 { return &stubProvider{id: "exa"} }},
		{ID: "tavily", Priority: 1, NewFunc: func() ProviderV2 { return &stubProvider{id: "tavily"} }},
		{ID: "brave", Priority: 20, NewFunc: func() ProviderV2 { return &stubProvider{id: "brave"} }},
	} {
		require.NoError(t, reg.Register(m))
	}

	require.Equal(t, []string{"exa", "brave"}, reg.Order())
	require.True(t, reg.Excluded("tavily"))
	require.False(t, reg.Excluded("exa"))
}

func TestRegistryRegisterValidation(t *testing.T) {
	t.Parallel()
	factory := func() ProviderV2 { return &stubProvider{id: "exa"} }

	t.Run("empty ID", func(t *testing.T) {
		t.Parallel()
		require.Error(t, NewRegistry().Register(ProviderMeta{NewFunc: factory}))
	})

	t.Run("nil NewFunc", func(t *testing.T) {
		t.Parallel()
		require.Error(t, NewRegistry().Register(ProviderMeta{ID: "exa"}))
	})

	t.Run("duplicate ID", func(t *testing.T) {
		t.Parallel()
		reg := NewRegistry()
		require.NoError(t, reg.Register(ProviderMeta{ID: "exa", NewFunc: factory}))
		require.Error(t, reg.Register(ProviderMeta{ID: "exa", NewFunc: factory}))
	})
}

func TestRegistryFactoryReturningNilIsRejected(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	require.NoError(t, reg.Register(ProviderMeta{
		ID:      "exa",
		Label:   "Exa",
		NewFunc: func() ProviderV2 { return nil },
	}))

	p, err := reg.Load("exa")

	require.Nil(t, p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "factory returned nil")
}
