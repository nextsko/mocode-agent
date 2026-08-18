// Package nethttp is the toolkit's single outbound-HTTP port.
//
// In SysML terms this package is the interface block every network-bound tool
// block connects through: tools never construct *http.Client or *http.Transport
// on their own, they ask a Factory for one. In directory-as-structure terms,
// this directory IS the "how we dial out" concept — nothing else in the tools
// tree owns transport construction.
//
// The port has exactly two dependencies: net/http and time. It must not import
// charm.land/fantasy, charm.land/catwalk, core/agent, or core/config — the
// layercheck constraint guard enforces this. Config wiring (proxy URL,
// resolver) happens at the composition site (registry / coordinator), which
// hands the factory a ready-made base transport.
package nethttp

import (
	"net/http"
	"time"
)

// Canonical timeouts for outbound HTTP. Centralised here so the "30 vs
// 30*time.Second" class of bug cannot recur silently: durations are only ever
// expressed as time.Duration values.
const (
	// DefaultTimeout is the standard request budget for web tools
	// (fetch, crawl, sourcegraph, search).
	DefaultTimeout = 30 * time.Second

	// DownloadTimeout is the long budget for large payload transfers
	// (download tool).
	DownloadTimeout = 5 * time.Minute
)

// Factory produces HTTP clients that all share one connection pool and one
// proxy configuration. Construct it once at the composition root and inject
// it through ToolDeps.HTTP; deriving clients is cheap, connections are reused.
type Factory struct {
	transport http.RoundTripper
	proxyURL  string
}

// NewFactory wraps base into the single shared, pool-tuned transport.
//
// base is typically config's proxy-aware transport
// (cfg.HTTPTransport(resolver)); nil falls back to http.DefaultTransport.
// The base is cloned before tuning so a shared base (like the package-level
// default) is never mutated. proxyURL is the resolved proxy URL ("" = none);
// it is exposed via ProxyURL for non-http.Client stacks such as go-git,
// which configure proxies through their own options.
func NewFactory(base http.RoundTripper, proxyURL string) *Factory {
	if base == nil {
		base = http.DefaultTransport
	}
	if t, ok := base.(*http.Transport); ok {
		t = t.Clone()
		t.MaxIdleConns = 100
		t.MaxIdleConnsPerHost = 10
		t.IdleConnTimeout = 90 * time.Second
		base = t
	}
	return &Factory{transport: base, proxyURL: proxyURL}
}

// Client returns an *http.Client with the shared transport and the given
// per-request timeout. The timeout must be a time.Duration value — never a
// bare integer literal (30 meant 30ns and killed every fetch).
func (f *Factory) Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: f.transport,
	}
}

// Transport exposes the shared transport for stacks that take a RoundTripper
// instead of a *http.Client.
func (f *Factory) Transport() http.RoundTripper { return f.transport }

// ProxyURL returns the resolved proxy URL ("" when none is configured).
// Intended for stacks like go-git that proxy through their own options
// rather than an http.Client.
func (f *Factory) ProxyURL() string { return f.proxyURL }
