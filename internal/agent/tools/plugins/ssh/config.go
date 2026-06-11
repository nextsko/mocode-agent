// Package ssh implements SSH client tools for the mocode agent.
//
// config.go resolves and normalises "host" strings into the canonical
// (host, port, user) triple that the SSH client pool needs to dial.
package ssh

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/kevinburke/ssh_config"
)

// HostSpec is the canonical form of an SSH destination.
//
// All fields are populated after Resolve(); the only optional field at
// construction time is Alias (the entry in the user's ssh config).
type HostSpec struct {
	Alias       string // the value passed in (may be empty)
	Host        string // resolved hostname or IP
	Port        int    // 0 means "use default 22"
	User        string // empty means "use current OS user"
	IdentityFile string // path to private key (may be empty)
	ProxyJump   string // optional JumpHost
}

// String renders the spec in a form suitable for log lines and error
// messages — never includes credentials.
func (h HostSpec) String() string {
	if h.User == "" {
		return net.JoinHostPort(h.Host, strconv.Itoa(h.EffectivePort()))
	}
	return fmt.Sprintf("%s@%s:%d", h.User, h.Host, h.EffectivePort())
}

// EffectivePort returns the port to dial, defaulting to 22.
func (h HostSpec) EffectivePort() int {
	if h.Port == 0 {
		return 22
	}
	return h.Port
}

// ─── resolver ────────────────────────────────────────────────────────────────

// configLoader is the subset of kevinburke/ssh_config used here.  Defining an
// interface makes the resolver testable without touching the filesystem.
type configLoader interface {
	Get(host, key string) (string, error)
}

type defaultConfigLoader struct{}

func (defaultConfigLoader) Get(host, key string) (string, error) {
	return ssh_config.GetStrict(host, key)
}

// Resolver turns a user-supplied "host" string into a HostSpec.
//
// Resolution order:
//  1. If the input contains '@' or ':' or is a raw IP, parse it directly.
//  2. Otherwise look the input up in the user's ~/.ssh/config.
//  3. Apply OS-user defaults for missing fields.
type Resolver struct {
	mu     sync.RWMutex
	cache  map[string]HostSpec
	loader configLoader
}

// NewResolver builds a Resolver.  Pass nil to use the real ssh_config package.
func NewResolver(loader configLoader) *Resolver {
	if loader == nil {
		loader = defaultConfigLoader{}
	}
	return &Resolver{cache: make(map[string]HostSpec), loader: loader}
}

// Resolve returns the canonical HostSpec for the given input.
//
// The input can be:
//   - An alias defined in ~/.ssh/config ("prod", "staging-web-1")
//   - user@host                (port defaults to 22)
//   - user@host:port
//   - host:port                (user defaults to current OS user)
//   - host                     (user and port default)
func (r *Resolver) Resolve(input string) (HostSpec, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return HostSpec{}, errors.New("ssh: empty host spec")
	}

	r.mu.RLock()
	if cached, ok := r.cache[input]; ok {
		r.mu.RUnlock()
		return cached, nil
	}
	r.mu.RUnlock()

	spec, err := r.resolve(input)
	if err != nil {
		return HostSpec{}, err
	}

	r.mu.Lock()
	r.cache[input] = spec
	r.mu.Unlock()
	return spec, nil
}

func (r *Resolver) resolve(input string) (HostSpec, error) {
	// Fast path: explicit user@host[:port] form.
	if explicit := parseExplicit(input); explicit.Alias != "" {
		return r.applyDefaults(explicit), nil
	}

	// Slow path: look up in ~/.ssh/config.  If the alias is not configured
	// ssh_config.GetStrict returns ErrNoMatch; in that case we fall back to
	// using the raw input as the HostName (which is what plain ssh(1) does
	// for bare hostnames).
	rawHost, err := r.loader.Get(input, "HostName")
	if err != nil || rawHost == "" {
		rawHost = input
	}

	portStr, _ := r.loader.Get(input, "Port")
	userStr, _ := r.loader.Get(input, "User")
	idFile, _ := r.loader.Get(input, "IdentityFile")
	proxy, _ := r.loader.Get(input, "ProxyJump")

	port, err := parsePort(portStr)
	if err != nil {
		return HostSpec{}, fmt.Errorf("ssh: invalid Port for %q: %w", input, err)
	}

	spec := HostSpec{
		Alias:        input,
		Host:         rawHost,
		Port:         port,
		User:         userStr,
		IdentityFile: idFile,
		ProxyJump:    proxy,
	}
	return r.applyDefaults(spec), nil
}

func (r *Resolver) applyDefaults(spec HostSpec) HostSpec {
	if spec.User == "" {
		if u, err := user.Current(); err == nil && u.Username != "" {
			spec.User = u.Username
		}
	}
	if spec.IdentityFile != "" {
		spec.IdentityFile = expandPath(spec.IdentityFile)
	}
	return spec
}

// ─── parsing helpers ────────────────────────────────────────────────────────

// parseExplicit returns a non-empty Alias only if the input is a fully
// qualified user@host[:port] / host:port form.  Bare hostnames (no '@'
// and no colon outside IPv6 brackets) fall through to config lookup.
func parseExplicit(input string) HostSpec {
	// Split off the optional user@ prefix.
	user := ""
	rest := input
	if at := strings.LastIndex(input, "@"); at >= 0 {
		// Make sure '@' is outside the bracketed part.
		if !strings.Contains(input[:at], ":") || strings.HasPrefix(input, "[") {
			user = input[:at]
			rest = input[at+1:]
		}
	}

	// user@host with no port — return early so the resolver stops here.
	if user != "" && rest != "" && !strings.ContainsAny(rest, "[:") {
		return HostSpec{Host: rest, Port: 0, User: user, Alias: input}
	}

	// IPv6 literal: [::1]:22 or [::1]
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return HostSpec{} // malformed
		}
		host := rest[1:end]
		port := 0
		if rest[end+1:] != "" {
			if !strings.HasPrefix(rest[end+1:], ":") {
				return HostSpec{}
			}
			p, err := strconv.Atoi(rest[end+2:])
			if err != nil {
				return HostSpec{}
			}
			port = p
		}
		return HostSpec{Host: host, Port: port, User: user, Alias: input}
	}

	// host:port form
	if colon := strings.LastIndex(rest, ":"); colon >= 0 {
		// Reject "host:" (empty port) and multi-colon (IPv6 without brackets).
		if colon == 0 || colon == len(rest)-1 || strings.Count(rest, ":") > 1 {
			return HostSpec{}
		}
		port, err := strconv.Atoi(rest[colon+1:])
		if err != nil {
			return HostSpec{}
		}
		return HostSpec{Host: rest[:colon], Port: port, User: user, Alias: input}
	}

	// bare hostname → let config lookup handle it
	return HostSpec{}
}

func parsePort(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if p < 1 || p > 65535 {
		return 0, fmt.Errorf("port out of range: %d", p)
	}
	return p, nil
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~") {
		if u, err := user.Current(); err == nil {
			p = filepath.Join(u.HomeDir, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// ─── known_hosts ─────────────────────────────────────────────────────────────

// KnownHostsPath returns the standard path to the user's known_hosts file.
// Exposed so tests can override it.
var KnownHostsPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

// ConfigPath returns the standard path to the user's ssh config file.
var ConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// SSHConfigPath returns the standard path to the user's ssh directory.
var SSHDirPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh")
}

// fileExists is a tiny indirection so tests can stub it.
var fileExists = func(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// readFile is a tiny indirection so tests can stub it.
var readFile = func(path string) ([]byte, error) {
	if path == "" {
		return nil, io.EOF
	}
	return os.ReadFile(path)
}
