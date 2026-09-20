package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// LaunchOptions configures a browser launch.
type LaunchOptions struct {
	// Headless runs without a visible window. Defaults to true.
	Headless bool
	// UserDataDir overrides the profile directory. Empty means a per-session
	// temporary directory (a fresh profile, so no user cookies or logins are
	// touched).
	UserDataDir string
	// URL to open. Empty means about:blank.
	URL string
	// ExtraArgs are appended verbatim, for the rare flag we do not model.
	ExtraArgs []string
	// LaunchTimeout bounds the wait for the browser to announce its debug
	// endpoint. Defaults to 30s: a cold Chromium start on a loaded machine is
	// slow, and failing early is worse than waiting.
	LaunchTimeout time.Duration
}

// devtoolsURLRe picks the endpoint Chrome prints on stderr at startup.
var devtoolsURLRe = regexp.MustCompile(`(ws://[^\s]+)`)

// parseDevToolsURL extracts the debug endpoint from one line of browser output.
func parseDevToolsURL(line string) (string, bool) {
	if !strings.Contains(line, "DevTools listening on") {
		return "", false
	}
	m := devtoolsURLRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// launchArgs builds the argv for a browser launch.
//
// This is a pure function so the flag matrix (headless, sandbox, profile,
// container quirks) is unit-testable without starting anything.
func launchArgs(c Candidate, opts LaunchOptions, profileDir string, runningAsRoot bool) []string {
	args := append([]string{}, c.PrefixArgs...)

	// Ask the browser to choose its own port and tell us which one. Allocating a
	// free port ourselves and handing it over is racy: between the probe and the
	// browser binding it, another process can take it.
	args = append(args, "--remote-debugging-port=0")

	if opts.Headless {
		// "new" is the modern headless; the old mode renders differently and
		// fakes the user agent less convincingly.
		args = append(args, "--headless=new")
	}

	args = append(args,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-features=Translate,BackForwardCache",
		"--disable-sync",
		// Keep the window off-screen and out of the user's way when headed.
		"--window-size=1440,900",
	)

	// Chromium refuses to start as root with its sandbox enabled, which is the
	// normal situation inside a container. Detect it rather than making every
	// container user discover the flag themselves.
	if runningAsRoot {
		args = append(args, "--no-sandbox", "--disable-dev-shm-usage")
	}

	if profileDir != "" {
		args = append(args, "--user-data-dir="+profileDir)
	}

	// The requested URL is deliberately NOT passed here. Chrome applies it
	// asynchronously, so attaching straight after startup can land on the
	// about:blank tab before the navigation commits, and the session would
	// silently hold the wrong page. Launch always starts blank and the URL is
	// applied to the page we actually attached to (see Launch).
	args = append(args, "about:blank")

	args = append(args, opts.ExtraArgs...)
	return args
}

// profileDirFor returns the profile directory to use, accounting for snap's
// confinement: a snap browser can only read directories under ~/snap/<name>/.
func profileDirFor(c Candidate, opts LaunchOptions) (string, error) {
	if opts.UserDataDir != "" {
		return opts.UserDataDir, nil
	}
	if c.NeedsSnapProfile {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("snap profile needs a home directory: %w", err)
		}
		dir := filepath.Join(home, "snap", string(c.Kind), "common", ".mocode-profile")
		return dir, os.MkdirAll(dir, 0o700)
	}
	dir, err := os.MkdirTemp("", "mocode-browser-*")
	if err != nil {
		return "", fmt.Errorf("creating profile directory: %w", err)
	}
	return dir, nil
}

// Session is a live browser plus one attached page.
type Session struct {
	conn *cdpConn

	cmd *exec.Cmd
	// exited is closed once the browser process is reaped. It is a separate
	// channel from any "wait" result on purpose: a receive-only-once pattern
	// makes a second Alive() call report a dead process as alive.
	exited  chan struct{}
	exitErr error

	targetID  string
	sessionID string

	console    *consoleBuffer
	network    *networkBuffer
	routes     *routeTable
	attached   bool
	endpoint   string
	profileDir string

	closeOnce sync.Once
	closeErr  error
}

// Attach connects to an already-running browser's CDP endpoint.
//
// endpoint may be a ws:// URL, or an http://host:port address which is resolved
// through CDP's own /json/version discovery.
func Attach(ctx context.Context, endpoint string) (*Session, error) {
	wsURL, product, err := discoverEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	conn, err := dialCDP(ctx, wsURL)
	if err != nil {
		return nil, err
	}

	s := &Session{
		conn:     conn,
		console:  newConsoleBuffer(defaultConsoleBuffer),
		network:  newNetworkBuffer(defaultNetworkBuffer),
		routes:   &routeTable{},
		attached: true,
		endpoint: wsURL,
	}
	s.registerPersistentHandlers()
	if err := s.attachPage(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = product
	return s, nil
}

// Launch starts a browser process and connects to it.
func Launch(ctx context.Context, c Candidate, opts LaunchOptions) (*Session, error) {
	if opts.LaunchTimeout <= 0 {
		opts.LaunchTimeout = 30 * time.Second
	}

	profileDir, err := profileDirFor(c, opts)
	if err != nil {
		return nil, err
	}

	args := launchArgs(c, opts, profileDir, runningAsRoot())

	cmd := exec.Command(c.Path, args...)
	// Chrome writes "DevTools listening on ws://..." to stderr, which is how we
	// learn the port it picked. Capturing stderr is therefore load-bearing, not
	// just diagnostics.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("capturing browser stderr: %w", err)
	}
	cmd.Stdout = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", c.Path, err)
	}

	s := &Session{
		cmd:        cmd,
		exited:     make(chan struct{}),
		console:    newConsoleBuffer(defaultConsoleBuffer),
		network:    newNetworkBuffer(defaultNetworkBuffer),
		routes:     &routeTable{},
		profileDir: profileDir,
	}

	go func() {
		s.exitErr = cmd.Wait()
		close(s.exited)
	}()

	endpoint, err := readDevToolsURL(ctx, stderr, opts.LaunchTimeout, s.exited)
	if err != nil {
		// A browser that died on startup is the common failure here (missing
		// shared libraries, sandbox refusal), so reap it before reporting.
		_ = cmd.Process.Kill()
		return nil, err
	}
	s.endpoint = endpoint

	conn, err := dialCDP(ctx, endpoint)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	s.conn = conn
	s.registerPersistentHandlers()

	if err := s.attachPage(ctx); err != nil {
		_ = s.Close(ctx)
		return nil, err
	}

	// Navigating here rather than on the command line guarantees the requested
	// page is the one the session holds, and that only one tab is open.
	if u := strings.TrimSpace(opts.URL); u != "" {
		if err := s.Navigate(ctx, u); err != nil {
			_ = s.Close(ctx)
			return nil, err
		}
	}
	return s, nil
}

// readDevToolsURL streams browser stderr until the debug endpoint appears.
//
// The stream must keep being drained after that: a browser whose stderr pipe
// fills up blocks. A goroutine therefore scans to EOF regardless of when we
// return.
func readDevToolsURL(ctx context.Context, stderr anyReader, timeout time.Duration, exited <-chan struct{}) (string, error) {
	found := make(chan string, 1)
	scanDone := make(chan struct{})

	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			if url, ok := parseDevToolsURL(scanner.Text()); ok {
				select {
				case found <- url:
				default:
				}
			}
		}
		// Nothing to do about a read error here: the interesting failure
		// (no endpoint) is already reported through the timeout or the exit.
		_ = scanner.Err()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case url := <-found:
		return url, nil
	case <-exited:
		return "", errors.New("browser exited before announcing its debug endpoint")
	case <-timer.C:
		return "", fmt.Errorf("browser did not announce its debug endpoint within %s", timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type anyReader interface{ Read([]byte) (int, error) }

// runningAsRoot reports whether Chromium's sandbox will refuse to start.
func runningAsRoot() bool { return isRoot() }

// registerPersistentHandlers wires the session-wide event captures. It must be
// called exactly once per connection: On appends, and a second registration
// after a tab switch would record every console message twice.
func (s *Session) registerPersistentHandlers() {
	s.conn.On("Runtime.consoleAPICalled", s.onConsole)
	s.conn.On("Runtime.exceptionThrown", s.onException)
	s.conn.On("Network.requestWillBeSent", s.onRequestWillBeSent)
	s.conn.On("Network.responseReceived", s.onResponseReceived)
	s.conn.On("Network.loadingFailed", s.onLoadingFailed)
	s.conn.On("Fetch.requestPaused", s.onRequestPaused)
}

// attachPage creates (or reuses) a page target and attaches to it with session
// flattening, so every later command can be routed by sessionId over the one
// browser-level socket.
func (s *Session) attachPage(ctx context.Context) error {
	// Reuse an existing page when attaching to the user's browser; otherwise the
	// user's current tab would be ignored and a new one spawned.
	targetID, err := s.pickTarget(ctx)
	if err != nil {
		return err
	}
	return s.switchPage(ctx, targetID)
}

// switchPage attaches to targetID and makes it the session's current page.
//
// Attaching is what multi-tab support is built on: the session's target and
// session ids are simply repointed, and every existing action keeps working on
// whatever page is current. Domains must be re-enabled per page session — they
// are session-scoped, not browser-scoped.
func (s *Session) switchPage(ctx context.Context, targetID string) error {
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	err := s.conn.Call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &attached)
	if err != nil {
		return fmt.Errorf("attaching to page: %w", err)
	}
	if attached.SessionID == "" {
		return errors.New("attaching to page: browser returned no session id")
	}

	s.targetID = targetID
	s.sessionID = attached.SessionID

	for _, m := range []string{"Page.enable", "Runtime.enable", "DOM.enable", "Network.enable"} {
		if err := s.conn.Call(ctx, s.sessionID, m, nil, nil); err != nil {
			return fmt.Errorf("%s: %w", m, err)
		}
	}

	// Routes installed on a previous page must keep applying after a tab
	// switch: Fetch.enable is session-scoped too.
	if s.routes.len() > 0 {
		if err := s.installFetch(ctx); err != nil {
			return err
		}
	}
	return nil
}

// pickTarget finds a page target, falling back to creating one.
func (s *Session) pickTarget(ctx context.Context) (string, error) {
	targets, err := s.pageTargets(ctx)
	if err != nil {
		return "", err
	}

	fallback := ""
	for _, t := range targets {
		// Prefer a page that actually holds a document. A browser launched with
		// a URL also exposes the about:blank tab it started from, and attaching
		// to that one would silently read an empty page instead of the site the
		// caller asked for.
		if isBlankURL(t.URL) {
			if fallback == "" {
				fallback = t.TargetID
			}
			continue
		}
		return t.TargetID, nil
	}
	if fallback != "" {
		return fallback, nil
	}

	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := s.conn.Call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		return "", fmt.Errorf("creating a page target: %w", err)
	}
	if created.TargetID == "" {
		return "", errors.New("creating a page target: browser returned no target id")
	}
	return created.TargetID, nil
}

// Navigate points the attached page at url and waits for the load event.
func (s *Session) Navigate(ctx context.Context, url string) error {
	if err := s.navigate(ctx, url); err != nil {
		return err
	}
	return s.waitLoad(ctx)
}

// navigate issues the navigation call without waiting for the page to load.
func (s *Session) navigate(ctx context.Context, url string) error {
	var res struct {
		ErrorText string `json:"errorText"`
	}
	if err := s.conn.Call(ctx, s.sessionID, "Page.navigate", map[string]any{"url": url}, &res); err != nil {
		return err
	}
	if res.ErrorText != "" && !strings.Contains(res.ErrorText, "net::ERR_ABORTED") {
		return fmt.Errorf("navigating to %s: %s", url, res.ErrorText)
	}
	return nil
}

// waitLoad blocks until the current page fires its load event.
//
// The handler is unsubscribed on return — a wait that leaked its handler would
// accumulate one per navigation and, with tabs in play, cross-talk between
// pages. Filtering on the session id keeps another tab's load from releasing
// this wait early.
func (s *Session) waitLoad(ctx context.Context) error {
	loaded := make(chan struct{}, 1)
	off := s.conn.On("Page.loadEventFired", func(sessionID string, _ json.RawMessage) {
		if sessionID != "" && sessionID != s.sessionID {
			return
		}
		select {
		case loaded <- struct{}{}:
		default:
		}
	})
	defer off()

	// Redirects and aborted loads mean the load event is a best-effort signal:
	// wait briefly, then carry on rather than failing a navigation that in fact
	// succeeded.
	select {
	case <-loaded:
	case <-time.After(15 * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Evaluate runs an expression in the page and decodes its JSON value into out.
func (s *Session) Evaluate(ctx context.Context, expression string, out any) error {
	var res struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
			Subtype     string          `json:"subtype"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}

	err := s.conn.Call(ctx, s.sessionID, "Runtime.evaluate", map[string]any{
		"expression": expression,
		// Ask for a value rather than a remote handle, so callers get data.
		"returnByValue": true,
		"awaitPromise":  true,
	}, &res)
	if err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		msg := res.ExceptionDetails.Text
		if res.ExceptionDetails.Exception != nil && res.ExceptionDetails.Exception.Description != "" {
			msg = res.ExceptionDetails.Exception.Description
		}
		return fmt.Errorf("evaluating %s: %s", truncate(expression, 80), msg)
	}
	if out == nil {
		return nil
	}
	if res.Result.Subtype == "null" || len(res.Result.Value) == 0 {
		return nil
	}
	return decodeInto(res.Result.Value, out)
}

// Endpoint returns the browser-level debug URL this session is using.
func (s *Session) Endpoint() string { return s.endpoint }

// TargetID returns the page target id.
func (s *Session) TargetID() string { return s.targetID }

// Close detaches and, when we launched the browser, shuts it down.
//
// A browser we started is asked to exit over CDP first (graceful, flushes the
// profile), then killed if it does not. A browser we merely attached to is left
// running — killing the user's own Chrome would be hostile.
func (s *Session) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		if s.conn != nil {
			if s.attached {
				_ = s.conn.Call(ctx, "", "Target.closeTarget", map[string]any{"targetId": s.targetID}, nil)
			} else {
				_ = s.conn.Call(ctx, "", "Browser.close", nil, nil)
			}
			_ = s.conn.Close()
		}

		if s.cmd != nil && s.cmd.Process != nil {
			select {
			case <-s.exited:
			case <-time.After(5 * time.Second):
				// Browser.close was ignored or the process is wedged; take it
				// down rather than leaking a headless Chrome.
				_ = s.cmd.Process.Kill()
				<-s.exited
			}
		}

		if s.profileDir != "" && !s.attached {
			_ = os.RemoveAll(s.profileDir)
		}
		s.closeErr = nil
	})
	return s.closeErr
}

// Alive reports whether the underlying process (or connection) is still up.
func (s *Session) Alive() bool {
	if s.cmd != nil {
		select {
		case <-s.exited:
			return false
		default:
			return true
		}
	}
	if s.conn == nil {
		return false
	}
	select {
	case <-s.conn.closed:
		return false
	default:
		return true
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// isBlankURL reports whether a page target holds no real document.
//
// A browser started with a URL still exposes the about:blank tab it began with,
// so "the first page target" is not reliably the page the caller asked for.
func isBlankURL(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "about:blank", "about:newtab", "chrome://newtab/":
		return true
	}
	return false
}
