package config

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type NetworkOptions struct {
	Enabled  bool   `json:"enabled,omitempty" jsonschema:"description=Enable configured network proxy,default=false"`
	ProxyURL string `json:"proxy_url,omitempty" jsonschema:"description=HTTP/SOCKS proxy URL,example=http://127.0.0.1:7890"`
	NoProxy  string `json:"no_proxy,omitempty" jsonschema:"description=Comma-separated hosts that bypass proxy,example=localhost,127.0.0.1"`
}

func (c *Config) HTTPClient(resolver VariableResolver, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: c.HTTPTransport(resolver),
	}
}

func (c *Config) HTTPTransport(resolver VariableResolver) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = c.ProxyFunc(resolver)
	return transport
}

func (c *Config) ProxyFunc(resolver VariableResolver) func(*http.Request) (*url.URL, error) {
	envProxy := http.ProxyFromEnvironment
	raw := c.ResolvedProxyURL(resolver)
	if raw == "" {
		return envProxy
	}
	proxyURL, err := url.Parse(raw)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return envProxy
	}
	noProxy := splitNoProxy(c.Options.Network.NoProxy)
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil || bypassProxy(req.URL.Hostname(), noProxy) {
			return nil, nil
		}
		return proxyURL, nil
	}
}

func (c *Config) ResolvedProxyURL(resolver VariableResolver) string {
	if c == nil || c.Options == nil || c.Options.Network == nil || !c.Options.Network.Enabled || strings.TrimSpace(c.Options.Network.ProxyURL) == "" {
		return ""
	}
	raw := strings.TrimSpace(c.Options.Network.ProxyURL)
	if resolver != nil {
		if resolved, err := resolver.ResolveValue(raw); err == nil && strings.TrimSpace(resolved) != "" {
			raw = strings.TrimSpace(resolved)
		}
	}
	return raw
}

func splitNoProxy(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func bypassProxy(host string, rules []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	for _, rule := range rules {
		switch {
		case rule == "*":
			return true
		case host == rule:
			return true
		case strings.HasPrefix(rule, ".") && strings.HasSuffix(host, rule):
			return true
		case strings.HasPrefix(rule, "*.") && strings.HasSuffix(host, strings.TrimPrefix(rule, "*")):
			return true
		}
	}
	return false
}
