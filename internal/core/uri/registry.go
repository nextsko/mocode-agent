package uri

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Kind classifies resolved content so the caller can render it correctly.
type Kind string

const (
	// KindText is UTF-8 text (markdown, source code, JSON…).
	KindText Kind = "text"
	// KindImage is binary image data.
	KindImage Kind = "image"
	// KindListing is a directory-style index of a URI's children.
	KindListing Kind = "listing"
)

// Entry is one item in a [KindListing].
type Entry struct {
	// Name is the path segment relative to the listing's URI.
	Name string
	// URI is the addressable reference for the entry.
	URI string
	// Dir marks an entry that is itself a listing.
	Dir bool
	// Size in bytes, when known.
	Size int64
	// Description is a one-line summary (a skill's gist, a doc's first heading).
	Description string
}

// Content is what a [Provider] returns.
type Content struct {
	Kind Kind
	// Text holds the payload for KindText and a rendered index for KindListing.
	Text string
	// Data holds raw bytes for KindImage.
	Data []byte
	// Entries is populated for KindListing.
	Entries []Entry
	// MIME describes Data for KindImage.
	MIME string
	// Title and Description feed the caller's resource metadata.
	Title       string
	Description string
	// Source is a human-readable origin (a directory path or a URL) so the agent
	// can tell where the bytes actually came from.
	Source string
}

// Provider resolves one authority of the protocol.
//
// Implementations live next to the subsystem they wrap (skills, browser, …)
// rather than here, so this package stays free of dependencies and the
// composition root decides what is reachable.
type Provider interface {
	// Authority is the namespace this provider owns.
	Authority() string
	// Summary is a one-line description used in listings and prompt text.
	Summary() string
	// Resolve fetches the resource. A URI with an empty path asks for a listing
	// of the authority's root.
	Resolve(ctx context.Context, u URI) (*Content, error)
}

// Registry routes URIs to providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

// Register adds a provider. Later registrations replace earlier ones for the
// same authority, which lets a caller override a default.
func (r *Registry) Register(p Provider) {
	if p == nil || p.Authority() == "" {
		return
	}
	r.providers[p.Authority()] = p
}

// Lookup returns the provider for an authority.
func (r *Registry) Lookup(authority string) (Provider, bool) {
	p, ok := r.providers[strings.ToLower(authority)]
	return p, ok
}

// Resolve parses and resolves a reference.
//
// Unknown authorities are reported with the list of known ones: a mistyped
// namespace is the most likely mistake, and the answer is cheap.
func (r *Registry) Resolve(ctx context.Context, raw string) (*Content, URI, error) {
	u, err := Parse(raw)
	if err != nil {
		return nil, URI{}, err
	}

	p, ok := r.Lookup(u.Authority)
	if !ok {
		return nil, u, fmt.Errorf("%w %q; known authorities: %s",
			ErrUnknownAuthority, u.Authority, strings.Join(r.Authorities(), ", "))
	}

	content, err := p.Resolve(ctx, u)
	if err != nil {
		return nil, u, err
	}
	if content.Source == "" {
		content.Source = u.String()
	}
	return content, u, nil
}

// Authorities lists the registered authorities, sorted for stable output.
func (r *Registry) Authorities() []string {
	out := make([]string, 0, len(r.providers))
	for a := range r.providers {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Describe renders the namespace map for a prompt or an error message, so the
// model can see what is addressable without being told in two places.
func (r *Registry) Describe() string {
	if len(r.providers) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Internal resources use the mocode:// scheme. These namespaces are available:\n")
	for _, a := range r.Authorities() {
		fmt.Fprintf(&b, "  mocode://%s/ — %s\n", a, r.providers[a].Summary())
	}
	b.WriteString("\nAddress a namespace without a path to list what it contains.")
	return b.String()
}

// RootListing builds a synthetic listing of every authority. Providers that can
// describe their root cheaply are asked; a failing provider is reported as an
// entry with an error note rather than breaking the whole listing.
func (r *Registry) RootListing(ctx context.Context) *Content {
	entries := make([]Entry, 0, len(r.providers))
	for _, a := range r.Authorities() {
		u := URI{Authority: a}
		entry := Entry{Name: a, URI: u.String(), Dir: true, Description: r.providers[a].Summary()}

		if content, err := r.providers[a].Resolve(ctx, u); err != nil {
			entry.Description = fmt.Sprintf("%s (unavailable: %v)", entry.Description, err)
		} else if content.Kind == KindListing {
			entry.Description = fmt.Sprintf("%s (%d item(s))", entry.Description, len(content.Entries))
		}
		entries = append(entries, entry)
	}

	return &Content{
		Kind:        KindListing,
		Entries:     entries,
		Title:       "mocode://",
		Description: "Internal resource namespaces",
		Source:      Prefix,
	}
}

// RenderListing formats a listing as text. It is the single place the shape of a
// listing is decided, so every authority looks the same to the model.
func RenderListing(u URI, c *Content) string {
	var b strings.Builder
	title := u.String()
	fmt.Fprintf(&b, "%s\n", title)
	if c.Description != "" {
		fmt.Fprintf(&b, "%s\n", c.Description)
	}

	if len(c.Entries) == 0 {
		b.WriteString("\n(empty — nothing is published in this namespace)\n")
		return b.String()
	}

	fmt.Fprintf(&b, "\n%d item(s):\n", len(c.Entries))
	for _, e := range c.Entries {
		name := e.Name
		if e.Dir {
			name += "/"
		}
		fmt.Fprintf(&b, "  %-34s", name)
		if e.Description != "" {
			fmt.Fprintf(&b, " %s", e.Description)
		} else if e.Size > 0 {
			fmt.Fprintf(&b, " %d bytes", e.Size)
		}
		b.WriteString("\n")
	}
	return b.String()
}
