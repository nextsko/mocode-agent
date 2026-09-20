package browser

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Manager owns the browser session for a mocode process.
//
// One session, not many: an agent debugging a page makes a long sequence of
// small calls (snapshot → click → snapshot → console) and they must all land on
// the same page. A manager is where that state lives, so the tool layer stays a
// thin translation of parameters into calls.
//
// It also means the session survives across tool calls but is torn down with
// the process, rather than leaking a headless Chrome per invocation.
type Manager struct {
	mu       sync.Mutex
	session  *Session
	kind     Kind
	headless bool
	attached bool
}

// NewManager returns an idle manager.
func NewManager() *Manager { return &Manager{} }

// EnsureOptions describes the session the caller wants.
type EnsureOptions struct {
	// Endpoint attaches to an existing browser instead of launching one.
	// MOCODE_BROWSER_CDP is used when this is empty.
	Endpoint string
	// Headless applies to a launched browser.
	Headless bool
	// Kind pins the browser product. Empty uses the best detected candidate.
	Kind Kind
	// URL to open on a freshly launched browser.
	URL string
	// UserDataDir overrides the profile directory.
	UserDataDir string
}

// Ensure returns the live session, launching or attaching if there is none.
//
// When a session already exists its options are left alone: silently restarting
// the browser because a later call passed a different flag would throw away the
// page state the agent has been building up.
func (m *Manager) Ensure(ctx context.Context, opts EnsureOptions) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session != nil && m.session.Alive() {
		return m.session, nil
	}

	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(EnvCDPEndpoint))
	}

	if endpoint != "" {
		s, err := Attach(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		m.session, m.attached, m.kind = s, true, Kind("attached")
		return s, nil
	}

	c, err := m.pick(ctx, opts.Kind)
	if err != nil {
		return nil, err
	}

	s, err := Launch(ctx, c, LaunchOptions{
		Headless:    opts.Headless,
		URL:         opts.URL,
		UserDataDir: opts.UserDataDir,
	})
	if err != nil {
		return nil, err
	}

	m.session, m.attached, m.kind, m.headless = s, false, c.Kind, opts.Headless
	return s, nil
}

// pick chooses the browser to launch.
func (m *Manager) pick(ctx context.Context, want Kind) (Candidate, error) {
	candidates, err := DetectOrError(ctx, DetectOptions{})
	if err != nil {
		return Candidate{}, err
	}

	if want == "" {
		return candidates[0], nil
	}
	for _, c := range candidates {
		if c.Kind == want {
			return c, nil
		}
	}

	available := make([]string, 0, len(candidates))
	for _, c := range candidates {
		available = append(available, string(c.Kind))
	}
	return Candidate{}, fmt.Errorf("browser %q is not installed; available: %s", want, strings.Join(available, ", "))
}

// Session returns the live session, or nil when none has been started.
func (m *Manager) Session() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil && !m.session.Alive() {
		return nil
	}
	return m.session
}

// Require returns the live session or an error telling the caller to launch.
func (m *Manager) Require() (*Session, error) {
	s := m.Session()
	if s == nil {
		return nil, fmt.Errorf("no browser session is running; call browser with action=launch first")
	}
	return s, nil
}

// Close tears the session down. It is safe to call when idle.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	s := m.session
	m.session = nil
	m.mu.Unlock()

	if s == nil {
		return nil
	}
	return s.Close(ctx)
}

// State describes the manager's session for status output.
func (m *Manager) State() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session == nil || !m.session.Alive() {
		return "No browser session is running."
	}

	var b strings.Builder
	mode := "launched"
	if m.attached {
		mode = "attached"
	}
	fmt.Fprintf(&b, "Session: %s (%s", mode, m.kind)
	if !m.attached {
		state := "headed"
		if m.headless {
			state = "headless"
		}
		fmt.Fprintf(&b, ", %s", state)
	}
	b.WriteString(")\n")
	fmt.Fprintf(&b, "Endpoint: %s\n", m.session.Endpoint())
	fmt.Fprintf(&b, "Target: %s", m.session.TargetID())

	entries, dropped := m.session.Console()
	fmt.Fprintf(&b, "\nConsole messages buffered: %d", len(entries))
	if dropped > 0 {
		fmt.Fprintf(&b, " (%d dropped)", dropped)
	}
	return b.String()
}
