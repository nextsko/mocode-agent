package credentials

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/pkg/search"
)

// fakeAuthStore is an in-memory AuthStore double for broker tests.
type fakeAuthStore struct {
	mu    sync.Mutex
	token string
	err   error
	gets  atomic.Int32
}

// Compile-time interface conformance check.
var _ AuthStore = (*fakeAuthStore)(nil)

func (s *fakeAuthStore) GetAuthToken(_ context.Context, _ string) (string, error) {
	s.gets.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	return s.token, nil
}

func (s *fakeAuthStore) set(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
}

func TestMultiBrokerEnvTakesPriorityOverDB(t *testing.T) {
	t.Setenv("MOCODE_TEST_SEARCH_KEY", "env-token")
	store := &fakeAuthStore{token: "db-token"}
	broker := NewMultiBroker(NewEnvBroker(), NewDBBroker(store))

	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{
		EnvKey: "MOCODE_TEST_SEARCH_KEY",
		AuthID: "exa",
	})

	require.NoError(t, err)
	require.Equal(t, "env-token", token)
	require.EqualValues(t, 0, store.gets.Load())
}

func TestMultiBrokerFallsBackToDBWhenEnvMisses(t *testing.T) {
	t.Setenv("MOCODE_TEST_SEARCH_KEY", "")
	store := &fakeAuthStore{token: "db-token"}
	broker := NewMultiBroker(NewEnvBroker(), NewDBBroker(store))

	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{
		EnvKey: "MOCODE_TEST_SEARCH_KEY",
		AuthID: "exa",
	})

	require.NoError(t, err)
	require.Equal(t, "db-token", token)
	require.EqualValues(t, 1, store.gets.Load())
}

func TestMultiBrokerAllSourcesMiss(t *testing.T) {
	t.Setenv("MOCODE_TEST_SEARCH_KEY", "")
	store := &fakeAuthStore{token: ""}
	broker := NewMultiBroker(NewEnvBroker(), NewDBBroker(store))

	var missed []string
	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{
		EnvKey: "MOCODE_TEST_SEARCH_KEY",
		AuthID: "exa",
		OnMiss: func(source string) { missed = append(missed, source) },
	})

	require.Empty(t, token)
	require.ErrorIs(t, err, search.ErrCredentialNotFound)
	require.Equal(t, []string{"env", "db"}, missed)
}

func TestMultiBrokerNoSourcesConfigured(t *testing.T) {
	t.Parallel()
	broker := NewMultiBroker(nil, nil)

	var missed []string
	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{
		AuthID: "exa",
		OnMiss: func(source string) { missed = append(missed, source) },
	})

	require.Empty(t, token)
	require.ErrorIs(t, err, search.ErrCredentialNotFound)
	require.Equal(t, []string{"db"}, missed)
}

func TestMultiBrokerCachesDBResult(t *testing.T) {
	t.Parallel()
	store := &fakeAuthStore{token: "v1"}
	broker := NewMultiBroker(nil, NewDBBroker(store))
	opts := search.ResolveOpts{AuthID: "exa"}

	first, err := broker.Resolve(context.Background(), "exa", opts)
	require.NoError(t, err)
	second, err := broker.Resolve(context.Background(), "exa", opts)
	require.NoError(t, err)

	require.Equal(t, "v1", first)
	require.Equal(t, "v1", second)
	require.EqualValues(t, 1, store.gets.Load())
}

func TestMultiBrokerForceRefreshReturnsFreshValue(t *testing.T) {
	t.Parallel()
	store := &fakeAuthStore{token: "old"}
	db := NewDBBroker(store)
	db.SetRefreshHook(func(_ context.Context, _ string) (string, error) {
		store.set("new")
		return "new", nil
	})
	broker := NewMultiBroker(nil, db)
	opts := search.ResolveOpts{AuthID: "exa"}

	first, err := broker.Resolve(context.Background(), "exa", opts)
	require.NoError(t, err)
	require.Equal(t, "old", first)

	store.set("new")
	cachedAgain, err := broker.Resolve(context.Background(), "exa", opts)
	require.NoError(t, err)
	require.Equal(t, "old", cachedAgain) // still memoized

	refreshed, err := broker.Refresh(context.Background(), "exa", opts)
	require.NoError(t, err)
	require.Equal(t, "new", refreshed)

	afterRefresh, err := broker.Resolve(context.Background(), "exa", opts)
	require.NoError(t, err)
	require.Equal(t, "new", afterRefresh)
}

func TestMultiBrokerConcurrentRefreshCollapsesToOneCall(t *testing.T) {
	t.Parallel()
	store := &fakeAuthStore{token: "old"}
	db := NewDBBroker(store)
	var hookCalls atomic.Int32
	db.SetRefreshHook(func(_ context.Context, _ string) (string, error) {
		hookCalls.Add(1)
		// Give the other goroutines time to join the in-flight call.
		time.Sleep(250 * time.Millisecond)
		store.set("fresh")
		return "fresh", nil
	})
	broker := NewMultiBroker(nil, db)
	opts := search.ResolveOpts{AuthID: "exa"}

	const n = 10
	results := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = broker.Refresh(context.Background(), "exa", opts)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, hookCalls.Load())
	for _, token := range results {
		require.Equal(t, "fresh", token)
	}
}

func TestMultiBrokerEnvOnlyRefreshReturnsEnvValue(t *testing.T) {
	t.Setenv("MOCODE_TEST_SEARCH_KEY", "env-token")
	broker := NewMultiBroker(NewEnvBroker(), nil)

	token, err := broker.Refresh(context.Background(), "exa", search.ResolveOpts{
		EnvKey: "MOCODE_TEST_SEARCH_KEY",
		AuthID: "exa",
	})

	require.NoError(t, err)
	require.Equal(t, "env-token", token)
}

func TestDBBrokerRefreshWithoutHookReReadsStore(t *testing.T) {
	t.Parallel()
	store := &fakeAuthStore{token: "v1"}
	broker := NewDBBroker(store)

	token, err := broker.Refresh(context.Background(), "exa", search.ResolveOpts{AuthID: "exa"})

	require.NoError(t, err)
	require.Equal(t, "v1", token)
	require.EqualValues(t, 1, store.gets.Load())
}

func TestDBBrokerEmptyAuthIDIsMiss(t *testing.T) {
	t.Parallel()
	store := &fakeAuthStore{token: "v1"}
	broker := NewDBBroker(store)

	var missed []string
	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{
		OnMiss: func(source string) { missed = append(missed, source) },
	})

	require.Empty(t, token)
	require.ErrorIs(t, err, search.ErrCredentialNotFound)
	require.Equal(t, []string{"db"}, missed)
	require.EqualValues(t, 0, store.gets.Load())
}

func TestDBBrokerPropagatesStoreErrors(t *testing.T) {
	t.Parallel()
	store := &fakeAuthStore{err: errors.New("disk exploded")}
	broker := NewDBBroker(store)

	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{AuthID: "exa"})

	require.Empty(t, token)
	require.Error(t, err)
	require.NotErrorIs(t, err, search.ErrCredentialNotFound)
	require.Contains(t, err.Error(), "disk exploded")
}

func TestDBBrokerRefreshHookErrorPropagates(t *testing.T) {
	t.Parallel()
	db := NewDBBroker(&fakeAuthStore{})
	db.SetRefreshHook(func(_ context.Context, _ string) (string, error) {
		return "", errors.New("oauth endpoint down")
	})

	token, err := db.Refresh(context.Background(), "exa", search.ResolveOpts{AuthID: "exa"})

	require.Empty(t, token)
	require.ErrorContains(t, err, "oauth endpoint down")
}

func TestEnvBrokerUnsetVariableIsMiss(t *testing.T) {
	t.Setenv("MOCODE_TEST_SEARCH_KEY", "")
	broker := NewEnvBroker()

	var missed []string
	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{
		EnvKey: "MOCODE_TEST_SEARCH_KEY",
		OnMiss: func(source string) { missed = append(missed, source) },
	})

	require.Empty(t, token)
	require.ErrorIs(t, err, search.ErrCredentialNotFound)
	require.Equal(t, []string{"env"}, missed)
}

func TestEnvBrokerEmptyKeyIsMiss(t *testing.T) {
	t.Parallel()
	broker := NewEnvBroker()

	token, err := broker.Resolve(context.Background(), "exa", search.ResolveOpts{})

	require.Empty(t, token)
	require.ErrorIs(t, err, search.ErrCredentialNotFound)
}
