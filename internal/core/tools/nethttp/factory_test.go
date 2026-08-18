package nethttp

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFactoryClientsShareOneTransport(t *testing.T) {
	t.Parallel()
	f := NewFactory(nil, "")
	c1 := f.Client(DefaultTimeout)
	c2 := f.Client(DownloadTimeout)
	assert.Same(t, f.Transport(), c1.Transport, "clients must share the single pooled transport")
	assert.Same(t, f.Transport(), c2.Transport, "clients must share the single pooled transport")
}

func TestFactoryTimeoutIsDurationScale(t *testing.T) {
	t.Parallel()
	// Regression guard: the registry once passed bare `30` (30 nanoseconds)
	// as the timeout, so every HTTP request died instantly. Assert the
	// canonical constants are second-scale and that Client honors them.
	assert.GreaterOrEqual(t, DefaultTimeout, 10*time.Second)
	assert.GreaterOrEqual(t, DownloadTimeout, time.Minute)

	f := NewFactory(nil, "")
	assert.Equal(t, DefaultTimeout, f.Client(DefaultTimeout).Timeout)
	assert.Equal(t, DownloadTimeout, f.Client(DownloadTimeout).Timeout)
}

func TestFactoryDoesNotMutateSharedBase(t *testing.T) {
	t.Parallel()
	base := http.DefaultTransport.(*http.Transport)
	beforeIdle := base.IdleConnTimeout
	NewFactory(base, "")
	assert.Equal(t, beforeIdle, base.IdleConnTimeout, "must not mutate the caller's transport")
}

func TestFactoryTunesClonedTransport(t *testing.T) {
	t.Parallel()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.MaxIdleConns = 1
	f := NewFactory(base, "http://127.0.0.1:7890")
	tuned, ok := f.Transport().(*http.Transport)
	assert.True(t, ok, "transport should remain a *http.Transport")
	assert.Equal(t, 100, tuned.MaxIdleConns)
	assert.Equal(t, 10, tuned.MaxIdleConnsPerHost)
	assert.NotSame(t, base, tuned, "tuning must apply to a clone")
	assert.Equal(t, "http://127.0.0.1:7890", f.ProxyURL())
}
