package browser

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

// Provider exposes the live browser session as `mocode://browser/...`.
//
// The point is that page state is not a file: it only exists while the session
// does. Addressing it through the same scheme as everything else means the agent
// reads a rendered page and a skill's SKILL.md the same way, and that a page's
// content can be pulled into context without a separate tool call.
type Provider struct {
	manager *Manager
}

// NewURIProvider wires the browser namespace to a session manager.
func NewURIProvider(m *Manager) uri.Provider {
	if m == nil {
		m = NewManager()
	}
	return &Provider{manager: m}
}

// Authority implements [uri.Provider].
func (p *Provider) Authority() string { return uri.AuthorityBrowser }

// Summary implements [uri.Provider].
func (p *Provider) Summary() string {
	return "The live browser session: mocode://browser/snapshot (the page's structure), /text (its visible text), /console (logs and page errors), /page (url and title), /screenshot (PNG)."
}

// browserResources is the fixed set of things a session can be asked for. A
// closed set is deliberate: it makes mocode://browser/ self-describing, so the
// agent can list it and see exactly what is available.
var browserResources = []struct {
	name string
	desc string
}{
	{"page", "Current URL, title and readiness"},
	{"snapshot", "Addressable page structure as refs (the same view the snapshot action returns)"},
	{"text", "Visible text of the page"},
	{"console", "Buffered console output and uncaught page errors"},
	{"screenshot", "Screenshot of the viewport as PNG"},
}

// Resolve implements [uri.Provider].
func (p *Provider) Resolve(ctx context.Context, u uri.URI) (*uri.Content, error) {
	session := p.manager.Session()

	if u.IsListing() {
		return p.listing(session != nil), nil
	}

	// The resource name is validated first: whether "text" is a browser
	// resource does not depend on a session, and answering a typo with "no
	// session" would send the agent off to launch a browser it does not need.
	name := u.Path
	if strings.Contains(name, "/") {
		return nil, fmt.Errorf("%w: %s is not a browser resource; see mocode://browser/ for the list",
			uri.ErrNotFound, u.String())
	}
	if !isBrowserResource(name) {
		return nil, fmt.Errorf("%w: unknown browser resource %q; available: %s",
			uri.ErrNotFound, name, browserResourceNames())
	}

	if session == nil {
		return nil, fmt.Errorf(
			"%w: no browser session is running, so %s has nothing to read; call the browser tool with action=launch first",
			uri.ErrNotFound, u.String())
	}

	switch name {
	case "page":
		return p.page(ctx, session, u)
	case "snapshot":
		return p.snapshot(ctx, session, u)
	case "text":
		return p.text(ctx, session, u)
	case "console":
		return p.console(session, u)
	case "screenshot":
		return p.screenshot(ctx, session, u)
	default:
		// Unreachable: isBrowserResource gated this above.
		return nil, fmt.Errorf("%w: unknown browser resource %q", uri.ErrNotFound, name)
	}
}

// isBrowserResource reports whether name is one of the fixed resources.
func isBrowserResource(name string) bool {
	for _, r := range browserResources {
		if r.name == name {
			return true
		}
	}
	return false
}

// browserResourceNames lists the resource names for error messages.
func browserResourceNames() string {
	names := make([]string, 0, len(browserResources))
	for _, r := range browserResources {
		names = append(names, r.name)
	}
	return strings.Join(names, ", ")
}

// listing describes the resource set, marking it unavailable when there is no
// session — which is itself the useful answer to "why is this empty?".
func (p *Provider) listing(hasSession bool) *uri.Content {
	entries := make([]uri.Entry, 0, len(browserResources))
	for _, r := range browserResources {
		desc := r.desc
		if !hasSession {
			desc += " (needs a running session: browser action=launch)"
		}
		entries = append(entries, uri.Entry{
			Name:        r.name,
			URI:         uri.URI{Authority: uri.AuthorityBrowser, Path: r.name}.String(),
			Description: desc,
		})
	}

	summary := "Live browser session"
	if !hasSession {
		summary = "No browser session is running — launch one with the browser tool (action=launch)"
	}
	return &uri.Content{Kind: uri.KindListing, Entries: entries, Title: "mocode://browser/", Description: summary}
}

func (p *Provider) page(ctx context.Context, s *Session, u uri.URI) (*uri.Content, error) {
	var info struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		Ready string `json:"ready"`
	}
	script := `({ url: location.href, title: document.title, ready: document.readyState })`
	if err := s.Evaluate(ctx, script, &info); err != nil {
		return nil, fmt.Errorf("reading the page: %w", err)
	}

	return &uri.Content{
		Kind:        uri.KindText,
		Text:        fmt.Sprintf("URL: %s\nTitle: %s\nReady state: %s\n", info.URL, info.Title, info.Ready),
		Title:       "page",
		Description: info.Title,
		Source:      u.String(),
	}, nil
}

func (p *Provider) snapshot(ctx context.Context, s *Session, u uri.URI) (*uri.Content, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return &uri.Content{
		Kind:        uri.KindText,
		Text:        snap.Render(),
		Title:       "snapshot",
		Description: snap.Title,
		Source:      u.String(),
	}, nil
}

// text returns the page's visible text, which is what an agent usually wants
// when it would otherwise reach for an HTTP fetch.
func (p *Provider) text(ctx context.Context, s *Session, u uri.URI) (*uri.Content, error) {
	const script = `(() => {
  const body = document.body;
  if (!body) return '';
  // innerText respects styling, so hidden nodes are excluded — which is the
  // whole reason to read the rendered page instead of fetching its source.
  return (body.innerText || '').replace(/\n{3,}/g, '\n\n').trim();
})()`

	var text string
	if err := s.Evaluate(ctx, script, &text); err != nil {
		return nil, fmt.Errorf("reading the page text: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: the page has no visible text (it may still be loading, or render inside a frame)", uri.ErrNotFound)
	}

	return &uri.Content{
		Kind:        uri.KindText,
		Text:        text,
		Title:       "text",
		Description: truncate(text, 80),
		Source:      u.String(),
	}, nil
}

func (p *Provider) console(s *Session, u uri.URI) (*uri.Content, error) {
	entries, dropped := s.Console()
	if len(entries) == 0 {
		return &uri.Content{
			Kind:        uri.KindText,
			Text:        "No console messages have been captured for this page.\n",
			Title:       "console",
			Description: "empty",
			Source:      u.String(),
		}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d console message(s)", len(entries))
	if dropped > 0 {
		fmt.Fprintf(&b, " (%d older message(s) dropped)", dropped)
	}
	b.WriteString(":\n\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "%s\n", e)
	}

	var errors int
	for _, e := range entries {
		if e.Level == "error" || e.Level == "exception" {
			errors++
		}
	}

	return &uri.Content{
		Kind:        uri.KindText,
		Text:        b.String(),
		Title:       "console",
		Description: fmt.Sprintf("%d message(s), %d error(s)", len(entries), errors),
		Source:      u.String(),
	}, nil
}

func (p *Provider) screenshot(ctx context.Context, s *Session, u uri.URI) (*uri.Content, error) {
	data, err := s.Screenshot(ctx, false)
	if err != nil {
		return nil, err
	}
	return &uri.Content{
		Kind:        uri.KindImage,
		Data:        data,
		MIME:        "image/png",
		Title:       "screenshot",
		Description: fmt.Sprintf("viewport screenshot, %d bytes", len(data)),
		Source:      u.String(),
	}, nil
}
