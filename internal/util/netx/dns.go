// Package netx provides low-level networking helpers that do not depend on
// the rest of the application.
package netx

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// DNSServerEnv is the environment variable that configures an explicit DNS
// server. It accepts a comma separated list; the first entry is the primary
// server and the remaining ones are used as fallbacks. An entry may carry an
// explicit port (for example "127.0.0.1:5353"); otherwise port 53 is used.
const DNSServerEnv = "MOCODE_DNS_SERVER"

// defaultDNSPort is the port used when a configured server has no port.
const defaultDNSPort = "53"

// dnsDialTimeout bounds a single connection attempt to an upstream server.
const dnsDialTimeout = 5 * time.Second

var (
	serversMu sync.RWMutex
	servers   []string
)

// ConfigureDNS installs an explicit DNS resolver when MOCODE_DNS_SERVER is
// set, and reports whether it did so.
//
// It exists because Go's pure Go resolver derives its server list from
// /etc/resolv.conf. On Android/Termux that file does not exist, so the
// resolver falls back to 127.0.0.1:53 and [::1]:53, and every lookup fails
// with "read: connection refused".
//
// When the environment variable is unset or empty, the resolver is left
// untouched and false is returned.
func ConfigureDNS() bool {
	addrs := parseServers(os.Getenv(DNSServerEnv))
	if len(addrs) == 0 {
		return false
	}

	dialer := &net.Dialer{Timeout: dnsDialTimeout}
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		// Honor the network the resolver asked for ("udp" first, "tcp" as a
		// fallback when the response is truncated), but ignore the address:
		// it comes from resolv.conf and points at a local stub that may not
		// exist.
		proto := "udp"
		if strings.HasPrefix(network, "tcp") {
			proto = "tcp"
		}

		var firstErr error
		for _, addr := range addrs {
			conn, err := dialer.DialContext(ctx, proto, addr)
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		return nil, firstErr
	}

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial:     dial,
	}

	serversMu.Lock()
	servers = addrs
	serversMu.Unlock()

	return true
}

// Servers returns the DNS servers configured by the last successful
// [ConfigureDNS] call, in resolution order. It is empty when no explicit
// resolver was installed.
func Servers() []string {
	serversMu.RLock()
	defer serversMu.RUnlock()
	return append([]string(nil), servers...)
}

// parseServers splits a comma separated server list, dropping empty entries
// and normalizing each entry to host:port form.
func parseServers(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, withDefaultPort(part))
	}
	return out
}

// withDefaultPort appends the default DNS port when server has none.
func withDefaultPort(server string) string {
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(strings.Trim(server, "[]"), defaultDNSPort)
}
