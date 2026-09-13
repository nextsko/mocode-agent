package netcommon

import (
	"net/http"
	"time"

	"github.com/nextsko/mocode-agent/internal/core/tools/external/nethttp"
)

// DefaultHTTPTimeout is the standard request timeout for web tools. Tools that
// move large payloads (download) override it via NewHTTPClient.
const DefaultHTTPTimeout = 30 * time.Second

// defaultFactory is the fallback port used when a tool is constructed without
// an injected client (tests, ad-hoc wiring). It shares ONE pool-tuned
// transport across all fallback clients instead of building a transport per
// call, which silently defeated connection reuse.
var defaultFactory = nethttp.NewFactory(nil, "")

// DefaultHTTPClient returns an *http.Client with the shared connection pool and
// the standard 30s request timeout. Prefer injecting a config-derived
// nethttp.Factory (registry does); this is the env-proxy fallback.
func DefaultHTTPClient() *http.Client {
	return NewHTTPClient(DefaultHTTPTimeout)
}

// NewHTTPClient returns an *http.Client using the shared connection pool with a
// caller-chosen request timeout. Use this when a tool needs the same pool
// tuning but a different timeout (e.g. download uses 5 minutes).
func NewHTTPClient(timeout time.Duration) *http.Client {
	return defaultFactory.Client(timeout)
}
