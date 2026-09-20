package credentials

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSingleflightGroupMergesConcurrentCalls(t *testing.T) {
	t.Parallel()
	g := &singleflightGroup{}
	var calls atomic.Int32

	const n = 10
	results := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = g.do(context.Background(), "key", func(context.Context) (string, error) {
				calls.Add(1)
				// Keep the flight open long enough for all callers to
				// join instead of starting their own.
				time.Sleep(250 * time.Millisecond)
				return "shared", nil
			})
		}(i)
	}
	wg.Wait()

	require.EqualValues(t, 1, calls.Load())
	for i := range results {
		require.NoError(t, errs[i])
		require.Equal(t, "shared", results[i])
	}
}

func TestSingleflightGroupDistinctKeysRunIndependently(t *testing.T) {
	t.Parallel()
	g := &singleflightGroup{}
	var calls atomic.Int32
	fn := func(context.Context) (string, error) {
		calls.Add(1)
		return "value", nil
	}

	first, err := g.do(context.Background(), "key-a", fn)
	require.NoError(t, err)
	second, err := g.do(context.Background(), "key-b", fn)
	require.NoError(t, err)

	require.Equal(t, "value", first)
	require.Equal(t, "value", second)
	require.EqualValues(t, 2, calls.Load())
}

func TestSingleflightGroupForgetDropsInFlightCall(t *testing.T) {
	t.Parallel()
	g := &singleflightGroup{}
	var calls atomic.Int32
	release := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		_, err := g.do(context.Background(), "key", func(context.Context) (string, error) {
			calls.Add(1)
			<-release
			return "first", nil
		})
		firstDone <- err
	}()
	require.Eventually(t, func() bool { return calls.Load() == 1 },
		time.Second, 5*time.Millisecond)

	g.forget("key")

	secondDone := make(chan struct{})
	var secondToken string
	var secondErr error
	go func() {
		secondToken, secondErr = g.do(context.Background(), "key", func(context.Context) (string, error) {
			calls.Add(1)
			return "second", nil
		})
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second do blocked on the forgotten in-flight call")
	}

	require.NoError(t, secondErr)
	require.Equal(t, "second", secondToken)

	close(release)
	require.NoError(t, <-firstDone)
	require.EqualValues(t, 2, calls.Load())
}

func TestSingleflightGroupPropagatesFnError(t *testing.T) {
	t.Parallel()
	g := &singleflightGroup{}

	token, err := g.do(context.Background(), "key", func(context.Context) (string, error) {
		return "", errors.New("boom")
	})

	require.Empty(t, token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}
