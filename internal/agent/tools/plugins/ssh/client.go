package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// AuthMethod is a pluggable authentication strategy.
//
// New methods can be added without touching the Client/Pool types: any
// function with this signature can be passed to WithAuthMethod.
type AuthMethod func(HostSpec) ([]ssh.AuthMethod, error)

// Client is a long-lived SSH connection bound to a single HostSpec.
//
// A Client owns exactly one *ssh.Client and the underlying TCP connection.
// Callers that need parallel sessions on the same host should ask the Pool
// for a Client per goroutine.
type Client struct {
	spec   HostSpec
	conn   net.Conn
	client *ssh.Client

	// Refcount: how many active borrows.  When this drops to zero the
	// connection is eligible for eviction by the pool.
	refs int
}

// Close releases the underlying connection.  Safe to call multiple times.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	return nil
}

// Pool is a goroutine-safe cache of SSH clients keyed by host string.
//
// Entries are refcounted.  A Client returned by Get must eventually be
// returned with Put; the same Client may be handed out to multiple
// callers concurrently — the ssh.Client is safe for concurrent use.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*Client // key = spec.String()
	maxIdle time.Duration
	lastUse map[string]time.Time

	// auth is the chain of AuthMethod functions tried in order.
	auth []AuthMethod
	// hostKeyCallback overrides the default known_hosts check.
	hostKeyCallback ssh.HostKeyCallback

	// dialTimeout is the per-TCP-dial timeout.
	dialTimeout time.Duration
	// handshakeTimeout is the per-SSH-handshake timeout.
	handshakeTimeout time.Duration
}

// PoolOption configures the Pool.
type PoolOption func(*Pool)

// WithDialTimeout sets the TCP dial timeout (default 10s).
func WithDialTimeout(d time.Duration) PoolOption {
	return func(p *Pool) { p.dialTimeout = d }
}

// WithHandshakeTimeout sets the SSH handshake timeout (default 15s).
func WithHandshakeTimeout(d time.Duration) PoolOption {
	return func(p *Pool) { p.handshakeTimeout = d }
}

// WithAuthMethod appends an auth strategy to the pool's chain.
func WithAuthMethod(a AuthMethod) PoolOption {
	return func(p *Pool) { p.auth = append(p.auth, a) }
}

// WithHostKeyCallback overrides the known_hosts verification.
func WithHostKeyCallback(cb ssh.HostKeyCallback) PoolOption {
	return func(p *Pool) { p.hostKeyCallback = cb }
}

// WithMaxIdle sets how long a connection may sit unused before being
// evicted on the next Evict() sweep (default 5m).
func WithMaxIdle(d time.Duration) PoolOption {
	return func(p *Pool) { p.maxIdle = d }
}

// NewPool returns an empty pool.  The default auth chain tries keys in this
// order:
//
//  1. SSH_AUTH_SOCK agent
//  2. ~/.ssh/id_ed25519, id_rsa, id_ecdsa, id_dsa
//  3. HostSpec.IdentityFile (from ssh config)
//
// Password auth must be opted into explicitly with WithAuthMethod.
func NewPool(opts ...PoolOption) *Pool {
	p := &Pool{
		clients:          make(map[string]*Client),
		lastUse:          make(map[string]time.Time),
		dialTimeout:      10 * time.Second,
		handshakeTimeout: 15 * time.Second,
		maxIdle:          5 * time.Minute,
	}
	for _, o := range opts {
		o(p)
	}
	// Always seed the default auth chain so callers can still append.
	if len(p.auth) == 0 {
		p.auth = []AuthMethod{AgentAuth(), DefaultKeyAuth(), IdentityFileAuth()}
	}
	if p.hostKeyCallback == nil {
		p.hostKeyCallback = defaultHostKeyCallback()
	}
	return p
}

// Get returns a usable Client for spec, opening a new connection if needed.
// The caller MUST eventually call Put to release the reference.
func (p *Pool) Get(spec HostSpec) (*Client, error) {
	key := spec.String()

	p.mu.Lock()
	if c, ok := p.clients[key]; ok {
		if c.client != nil {
			c.refs++
			p.lastUse[key] = time.Now()
			p.mu.Unlock()
			return c, nil
		}
		// Stale entry: drop and reconnect.
		delete(p.clients, key)
		delete(p.lastUse, key)
	}
	p.mu.Unlock()

	c, err := p.dial(spec)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	// Someone may have raced us.  Keep the existing one if so.
	if existing, ok := p.clients[key]; ok && existing.client != nil {
		_ = c.Close()
		existing.refs++
		p.lastUse[key] = time.Now()
		p.mu.Unlock()
		return existing, nil
	}
	c.refs = 1
	p.clients[key] = c
	p.lastUse[key] = time.Now()
	p.mu.Unlock()
	return c, nil
}

// Put releases a reference previously obtained from Get.  When the last
// reference is dropped the connection is NOT closed immediately — the pool
// will close it on the next Evict sweep once maxIdle has elapsed.
func (p *Pool) Put(c *Client) {
	if c == nil {
		return
	}
	key := c.spec.String()
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.clients[key]; ok && entry == c {
		if c.refs > 0 {
			c.refs--
		}
		p.lastUse[key] = time.Now()
	}
}

// Close shuts down every connection in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var firstErr error
	for k, c := range p.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close %s: %w", k, err)
		}
		delete(p.clients, k)
		delete(p.lastUse, k)
	}
	return firstErr
}

// Evict closes any connection that has been idle longer than maxIdle.
func (p *Pool) Evict() {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, t := range p.lastUse {
		if now.Sub(t) > p.maxIdle {
			if c, ok := p.clients[k]; ok && c.refs == 0 {
				_ = c.Close()
				delete(p.clients, k)
				delete(p.lastUse, k)
			}
		}
	}
}

// ─── dial ────────────────────────────────────────────────────────────────────

func (p *Pool) dial(spec HostSpec) (*Client, error) {
	addr := net.JoinHostPort(spec.Host, strconv.Itoa(spec.EffectivePort()))

	// 1. TCP dial
	d := net.Dialer{Timeout: p.dialTimeout}
	ctx, cancel := context.WithTimeout(context.Background(), p.dialTimeout)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial %s: %w", addr, err)
	}

	// 2. Build auth methods
	var methods []ssh.AuthMethod
	for _, a := range p.auth {
		ams, err := a(spec)
		if err != nil {
			// Auth method failure is non-fatal — try the next one.
			continue
		}
		methods = append(methods, ams...)
	}
	if len(methods) == 0 {
		_ = conn.Close()
		return nil, errors.New("ssh: no usable auth method (no key file, no agent, no password)")
	}

	// 3. SSH handshake
	cfg := &ssh.ClientConfig{
		User:            spec.User,
		Auth:            methods,
		HostKeyCallback: p.hostKeyCallback,
		Timeout:         p.handshakeTimeout,
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh: handshake %s: %w", addr, err)
	}
	c := ssh.NewClient(clientConn, chans, reqs)

	return &Client{spec: spec, conn: conn, client: c}, nil
}

// ─── exec / sftp ────────────────────────────────────────────────────────────

// ExecResult is the structured outcome of a remote command.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Took     time.Duration
}

// Exec runs cmd on the remote host with the given timeout.  The returned
// error is non-nil only for transport / I/O failures; non-zero exit codes
// are reported via ExitCode and do NOT cause err to be set.
func (c *Client) Exec(ctx context.Context, cmd string, timeout time.Duration) (*ExecResult, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("ssh: client is closed")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sess, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh: open session: %w", err)
	}
	defer sess.Close()

	var stdout, stderr strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()

	select {
	case <-cctx.Done():
		_ = sess.Signal(ssh.SIGTERM)
		<-done
		return &ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 124, // conventional "timed out"
			Took:     time.Since(start),
		}, nil
	case err := <-done:
		res := &ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			Took:     time.Since(start),
		}
		if exitErr, ok := err.(*ssh.ExitError); ok {
			res.ExitCode = exitErr.ExitStatus()
			return res, nil
		}
		if err != nil {
			return res, fmt.Errorf("ssh: run failed: %w", err)
		}
		res.ExitCode = 0
		return res, nil
	}
}

// Upload copies local → remote using the scp(1) wire protocol.  This
// avoids pulling in github.com/pkg/sftp just for one tool.
//
//	`mode` is the remote file mode (e.g. 0o644).  0 means "preserve source".
func (c *Client) Upload(ctx context.Context, local, remote string, mode os.FileMode) error {
	if c == nil || c.client == nil {
		return errors.New("ssh: client is closed")
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return fmt.Errorf("ssh: read local: %w", err)
	}
	if mode == 0 {
		if st, statErr := os.Stat(local); statErr == nil {
			mode = st.Mode().Perm()
		} else {
			mode = 0o644
		}
	}
	return scpUpload(c, remote, mode, data)
}

// Download copies remote → local using the scp(1) wire protocol.
func (c *Client) Download(ctx context.Context, remote, local string) error {
	if c == nil || c.client == nil {
		return errors.New("ssh: client is closed")
	}
	sess, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh: open session: %w", err)
	}
	defer sess.Close()

	if dir := filepath.Dir(local); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("ssh: mkdir local %s: %w", dir, err)
		}
	}
	f, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("ssh: create local %s: %w", local, err)
	}
	defer f.Close()

	sess.Stdout = f
	// -f = "from" (source) mode: server streams file to us.
	if err := sess.Run(fmt.Sprintf("scp -f %q", remote)); err != nil {
		return fmt.Errorf("ssh: scp -f %s: %w", remote, err)
	}
	return nil
}

// scpUpload pushes data to a remote `scp -t` sink.
func scpUpload(c *Client, remote string, mode os.FileMode, data []byte) error {
	sess, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh: open session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("ssh: stdin pipe: %w", err)
	}
	if err := sess.Start(fmt.Sprintf("scp -t %q", remote)); err != nil {
		return fmt.Errorf("ssh: scp -t %s: %w", remote, err)
	}

	// 1. Send the C{mode} {size} {name}\n header.
	header := fmt.Sprintf("C%04o %d %s\n", mode, len(data), filepath.Base(remote))
	if _, err := stdin.Write([]byte(header)); err != nil {
		return fmt.Errorf("ssh: scp header: %w", err)
	}
	// 2. Payload.
	if _, err := stdin.Write(data); err != nil {
		return fmt.Errorf("ssh: scp payload: %w", err)
	}
	// 3. NUL terminator.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return fmt.Errorf("ssh: scp terminator: %w", err)
	}
	if err := stdin.Close(); err != nil {
		return fmt.Errorf("ssh: scp close: %w", err)
	}
	if err := sess.Wait(); err != nil {
		return fmt.Errorf("ssh: scp wait: %w", err)
	}
	return nil
}

// ─── auth strategies ────────────────────────────────────────────────────────

// AgentAuth returns an AuthMethod that uses the local ssh-agent.
func AgentAuth() AuthMethod {
	return func(_ HostSpec) ([]ssh.AuthMethod, error) {
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, errors.New("SSH_AUTH_SOCK not set")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("dial agent: %w", err)
		}
		// Note: we intentionally do NOT close conn — the agent client owns it.
		return []ssh.AuthMethod{ssh.PublicKeysCallback(agent.NewClient(conn).Signers)}, nil
	}
}

// DefaultKeyAuth returns an AuthMethod that tries well-known key file
// locations: id_ed25519, id_rsa, id_ecdsa, id_dsa.
func DefaultKeyAuth() AuthMethod {
	return func(spec HostSpec) ([]ssh.AuthMethod, error) {
		dir := SSHDirPath()
		if dir == "" {
			return nil, errors.New("no home dir")
		}
		candidates := []string{"id_ed25519", "id_rsa", "id_ecdsa", "id_dsa"}
		var methods []ssh.AuthMethod
		for _, name := range candidates {
			path := filepath.Join(dir, name)
			if !fileExists(path) {
				continue
			}
			key, err := readPrivateKey(path)
			if err != nil {
				continue
			}
			methods = append(methods, ssh.PublicKeys(key))
		}
		if len(methods) == 0 {
			return nil, errors.New("no usable default key")
		}
		return methods, nil
	}
}

// IdentityFileAuth returns an AuthMethod that uses the IdentityFile from
// the resolved HostSpec.  Returns an error if no identity file is set.
func IdentityFileAuth() AuthMethod {
	return func(spec HostSpec) ([]ssh.AuthMethod, error) {
		if spec.IdentityFile == "" {
			return nil, errors.New("no IdentityFile in host spec")
		}
		key, err := readPrivateKey(spec.IdentityFile)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(key)}, nil
	}
}

// PasswordAuth returns an AuthMethod that uses a static password.  This
// should generally not be used — prefer agent or key auth.
func PasswordAuth(pw string) AuthMethod {
	return func(_ HostSpec) ([]ssh.AuthMethod, error) {
		if pw == "" {
			return nil, errors.New("empty password")
		}
		return []ssh.AuthMethod{ssh.Password(pw)}, nil
	}
}

// ─── internal helpers ────────────────────────────────────────────────────────

func readPrivateKey(path string) (ssh.Signer, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}
	// Try plain first, then with an empty passphrase (most CI keys).
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		// Could be encrypted — surface the error so the user can fix it.
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return signer, nil
}

// defaultHostKeyCallback loads ~/.ssh/known_hosts and refuses the
// connection if the host key is not present (no TOFU).
func defaultHostKeyCallback() ssh.HostKeyCallback {
	path := KnownHostsPath()
	if path == "" || !fileExists(path) {
		// No known_hosts — refuse rather than silently trust anything.
		return func(_ string, _ net.Addr, _ ssh.PublicKey) error {
			return errors.New("ssh: no known_hosts file; host key verification refused")
		}
	}
	// We deliberately ignore the callback error from New(): on first use
	// the file may be empty.  The callback still returns a sensible error
	// for unknown hosts.
	cb, _ := knownhosts.New(path)
	return cb
}
