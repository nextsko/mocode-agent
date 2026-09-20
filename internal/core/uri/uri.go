// Package uri implements mocode's internal resource protocol.
//
// The scheme is `mocode://<authority>/<path>`, and it exists so the agent can
// address resources that are not files on disk: embedded skills, remote GitHub
// sources, and the live browser session. A scheme (rather than a pile of
// special cases) means one tool resolves everything, one place documents the
// namespaces, and new sources are additive.
//
// See docs/design/17-internal-uri-protocol.md.
package uri

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Scheme is the protocol prefix.
const Scheme = "mocode"

// Authorities. Each is owned by exactly one Provider.
const (
	// AuthoritySkills serves builtin (embedded) and user-installed skills.
	AuthoritySkills = "skills"
	// AuthorityDocs serves the configured documentation roots.
	AuthorityDocs = "docs"
	// AuthorityExp serves the experience-notes library (the .exps convention).
	AuthorityExp = "exp"
	// AuthorityGitHub serves files from public GitHub repositories.
	AuthorityGitHub = "github"
	// AuthorityBrowser serves the live browser session.
	AuthorityBrowser = "browser"
	// AuthorityMemory serves the session and global memory tree.
	AuthorityMemory = "memory"
)

// Prefix is the full scheme prefix, the form that appears in prompts.
const Prefix = Scheme + "://"

var (
	// ErrNotURI means the string is not a mocode:// reference at all.
	ErrNotURI = errors.New("not a mocode:// reference")
	// ErrUnknownAuthority means no provider owns the authority.
	ErrUnknownAuthority = errors.New("unknown authority")
	// ErrNotFound means the authority exists but the resource does not.
	ErrNotFound = errors.New("resource not found")
	// ErrIsDirectory reports a directory where a file was requested.
	ErrIsDirectory = errors.New("path is a directory")
)

// URI is a parsed mocode:// reference.
type URI struct {
	// Authority selects the provider, e.g. "skills".
	Authority string
	// Path is the resource path within the authority, slash-separated and
	// without a leading slash. Empty means "list this authority".
	Path string
	// Query carries provider-specific options such as `ref` for github.
	Query url.Values
	// Raw is the original string, kept for error messages and display.
	Raw string
}

// IsURI reports whether s looks like a mocode:// reference.
//
// It is deliberately a cheap prefix test: it is used on hot paths (every View
// call, every prompt render) where a full parse would be wasted. Anything that
// passes still goes through [Parse].
func IsURI(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), Prefix)
}

// Parse parses a mocode:// reference.
//
// Parsing is strict on purpose. A malformed reference that silently degraded
// into a filesystem path would be read as "file not found", which sends the
// agent hunting for a path bug instead of showing it the typo it actually made.
func Parse(raw string) (URI, error) {
	trimmed := strings.TrimSpace(raw)
	if !IsURI(trimmed) {
		return URI{}, fmt.Errorf("%w: %q", ErrNotURI, raw)
	}

	rest := strings.TrimPrefix(trimmed, Prefix)
	if rest == "" {
		return URI{}, fmt.Errorf("%w: %q has no authority", ErrNotURI, raw)
	}

	// Split the query off before the path so a `?` inside a query value cannot
	// be mistaken for path content.
	rawQuery := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest, rawQuery = rest[:i], rest[i+1:]
	}

	authority := rest
	path := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority, path = rest[:i], rest[i+1:]
	}
	authority = strings.ToLower(strings.TrimSpace(authority))
	if authority == "" {
		return URI{}, fmt.Errorf("%w: %q has no authority", ErrNotURI, raw)
	}
	if !isValidAuthority(authority) {
		return URI{}, fmt.Errorf("%w: %q", ErrNotURI, authority)
	}

	cleanPath, err := cleanPath(path)
	if err != nil {
		return URI{}, fmt.Errorf("%q: %w", raw, err)
	}

	query := url.Values{}
	if rawQuery != "" {
		parsed, err := url.ParseQuery(rawQuery)
		if err != nil {
			return URI{}, fmt.Errorf("%q: invalid query: %w", raw, err)
		}
		query = parsed
	}

	return URI{Authority: authority, Path: cleanPath, Query: query, Raw: trimmed}, nil
}

// isValidAuthority constrains the namespace to a bare word so that a typo like
// `mocode://skills/x y` is rejected rather than routed somewhere surprising.
func isValidAuthority(a string) bool {
	for _, r := range a {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// cleanPath normalises the path and rejects anything that could escape the
// provider's root. The providers resolve inside embedded filesystems and cloned
// repositories as well as directories, so traversal must be rejected here, once,
// rather than trusted to every provider.
func cleanPath(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	if strings.ContainsRune(p, '\\') {
		return "", errors.New("path must use forward slashes")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("path contains a control character")
		}
	}

	segments := strings.Split(p, "/")
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		switch seg {
		case "":
			return "", errors.New("path contains an empty segment")
		case ".", "..":
			return "", fmt.Errorf("path may not contain %q", seg)
		}
		out = append(out, seg)
	}
	return strings.Join(out, "/"), nil
}

// String renders the URI back to its canonical form.
func (u URI) String() string {
	var b strings.Builder
	b.WriteString(Prefix)
	b.WriteString(u.Authority)
	if u.Path != "" {
		b.WriteString("/")
		b.WriteString(u.Path)
	}
	if len(u.Query) > 0 {
		b.WriteString("?")
		b.WriteString(u.Query.Encode())
	}
	return b.String()
}

// IsListing reports whether the URI addresses a collection rather than a file.
func (u URI) IsListing() bool { return u.Path == "" }

// Base returns the URI with the last path segment removed, for "go up" links in
// listings.
func (u URI) Base() URI {
	if u.Path == "" {
		return u
	}
	parent := ""
	if i := strings.LastIndexByte(u.Path, '/'); i >= 0 {
		parent = u.Path[:i]
	}
	u.Path = parent
	return u
}

// Join returns a child URI, used to build listing entries.
func (u URI) Join(segment string) URI {
	child := u
	if u.Path == "" {
		child.Path = segment
	} else {
		child.Path = u.Path + "/" + segment
	}
	child.Raw = ""
	return child
}

// ValidAuthorities lists the authorities the protocol defines, in the order
// they should be presented to a model.
func ValidAuthorities() []string {
	return []string{AuthoritySkills, AuthorityDocs, AuthorityExp, AuthorityGitHub, AuthorityBrowser, AuthorityMemory}
}
