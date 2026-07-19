package netcommon

import (
	"net/http"
	"time"
)

// DefaultHTTPTimeout is the standard request timeout for web tools. Tools that
// move large payloads (download) override it via NewHTTPClient.
const DefaultHTTPTimeout = 30 * time.Second

// sharedTransport builds the connection-pool tuning every web tool used to
// duplicate inline: a generous idle pool with per-host limiting and a long
// idle lifetime, so repeated fetches reuse connections without exhausting file
// descriptors.
func sharedTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 10
	t.IdleConnTimeout = 90 * time.Second
	return t
}

// DefaultHTTPClient returns an *http.Client with the shared connection pool and
// the standard 30s request timeout. Most web tools (web_search, web_fetch,
// fetch, sourcegraph) call this when no client is injected.
func DefaultHTTPClient() *http.Client {
	return NewHTTPClient(DefaultHTTPTimeout)
}

// NewHTTPClient returns an *http.Client using the shared connection pool with a
// caller-chosen request timeout. Use this when a tool needs the same pool
// tuning but a different timeout (e.g. download uses 5 minutes).
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: sharedTransport(),
	}
}
