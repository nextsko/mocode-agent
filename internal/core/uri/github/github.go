// Package github serves public GitHub repository content as
// `mocode://github/<owner>/<repo>/<path>`.
//
// Single files are fetched over raw.githubusercontent.com rather than by
// cloning: the existing crawler clones a whole repository into memory, which is
// the right trade for "bring me every doc in this repo" and the wrong one for
// "show me this one file". Raw fetch also needs no git, no disk and no clone
// budget.
//
// Directory listings do need the API, which is unauthenticated-rate-limited;
// GITHUB_TOKEN (or GH_TOKEN) raises that limit and is picked up automatically.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

// DefaultMaxFileSize caps a single fetched file. Source files are small; the cap
// exists so that pointing the scheme at a release asset or a generated bundle
// cannot blow up the agent's context.
const DefaultMaxFileSize = 1 << 20 // 1 MiB

// Provider resolves the github authority.
type Provider struct {
	client *http.Client
	// maxFileSize is the cap for a single read.
	maxFileSize int64
	// token is sent as a bearer token when non-empty.
	token string

	// rawBase and apiBase are overridable so tests can point them at a local
	// server instead of the real GitHub.
	rawBase string
	apiBase string
}

// Option customises a Provider.
type Option func(*Provider)

// WithToken sets the API token. Empty falls back to GITHUB_TOKEN then GH_TOKEN.
func WithToken(token string) Option {
	return func(p *Provider) { p.token = strings.TrimSpace(token) }
}

// WithMaxFileSize overrides the per-file cap.
func WithMaxFileSize(n int64) Option {
	return func(p *Provider) {
		if n > 0 {
			p.maxFileSize = n
		}
	}
}

// WithBaseURLs overrides the upstream hosts. Used by tests.
func WithBaseURLs(raw, api string) Option {
	return func(p *Provider) {
		if raw != "" {
			p.rawBase = strings.TrimSuffix(raw, "/")
		}
		if api != "" {
			p.apiBase = strings.TrimSuffix(api, "/")
		}
	}
}

// New builds a Provider. client nil uses a default client with a timeout.
func New(client *http.Client, opts ...Option) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	p := &Provider{
		client:      client,
		maxFileSize: DefaultMaxFileSize,
		rawBase:     "https://raw.githubusercontent.com",
		apiBase:     "https://api.github.com",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Authority implements [uri.Provider].
func (p *Provider) Authority() string { return uri.AuthorityGitHub }

// Summary implements [uri.Provider].
func (p *Provider) Summary() string {
	return "Public GitHub repositories: mocode://github/<owner>/<repo>/<path>[?ref=<branch|tag|sha>]. Read a file directly, or a directory to list it."
}

// repoRef is the parsed `<owner>/<repo>/<path>` form.
type repoRef struct {
	owner string
	repo  string
	path  string
	ref   string
}

// Resolve implements [uri.Provider].
func (p *Provider) Resolve(ctx context.Context, u uri.URI) (*uri.Content, error) {
	ref, err := parseRepoRef(u)
	if err != nil {
		return nil, err
	}

	if ref.path == "" {
		return p.list(ctx, u, ref)
	}

	content, err := p.readFile(ctx, u, ref)
	if err != nil {
		return nil, err
	}
	if content != nil {
		return content, nil
	}

	// Not a file: it may be a directory. Trying the listing second keeps the
	// common case (a file) to one request.
	return p.list(ctx, u, ref)
}

// parseRepoRef splits the URI path into owner, repo, path and ref.
func parseRepoRef(u uri.URI) (repoRef, error) {
	segments := strings.Split(u.Path, "/")
	if u.Path == "" {
		segments = nil
	}
	if len(segments) < 2 {
		return repoRef{}, fmt.Errorf(
			"%w: %s needs <owner>/<repo>/<path>, e.g. mocode://github/cli/cli/README.md (or just <owner>/<repo> to list a repository)",
			uri.ErrNotFound, u.String())
	}

	ref := repoRef{
		owner: segments[0],
		repo:  strings.TrimSuffix(segments[1], ".git"),
		ref:   firstNonEmpty(u.Query.Get("ref"), "HEAD"),
	}
	if len(segments) > 2 {
		ref.path = strings.Join(segments[2:], "/")
	}
	return ref, nil
}

// readFile fetches one file over raw. It returns (nil, nil) when the path is not
// a file, so the caller can fall through to a directory listing.
func (p *Provider) readFile(ctx context.Context, u uri.URI, ref repoRef) (*uri.Content, error) {
	raw := fmt.Sprintf("%s/%s/%s/%s/%s", p.rawBase, ref.owner, ref.repo, url.PathEscape(ref.ref), escapePath(ref.path))

	body, err := p.get(ctx, raw, "text/plain")
	if err != nil {
		var httpErr *statusError
		if errors.As(err, &httpErr) && httpErr.code == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(io.LimitReader(body, p.maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", u.String(), err)
	}
	if int64(len(data)) > p.maxFileSize {
		return nil, fmt.Errorf("%s is larger than the %d byte limit; fetch it directly with the fetch tool if you need it",
			u.String(), p.maxFileSize)
	}

	name := path.Base(ref.path)
	if mime, ok := imageMIME(name); ok {
		return &uri.Content{
			Kind:  uri.KindImage,
			Data:  data,
			MIME:  mime,
			Title: name,
			// The raw URL is the canonical, clickable origin for the bytes.
			Source: raw,
		}, nil
	}

	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s is not UTF-8 text (%d bytes); it may be a binary file", u.String(), len(data))
	}

	return &uri.Content{
		Kind:        uri.KindText,
		Text:        string(data),
		Title:       name,
		Description: fmt.Sprintf("%s/%s@%s", ref.owner, ref.repo, ref.ref),
		Source:      raw,
	}, nil
}

// list renders a directory (or a repository root) using the contents API.
func (p *Provider) list(ctx context.Context, u uri.URI, ref repoRef) (*uri.Content, error) {
	api := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		p.apiBase, ref.owner, ref.repo, escapePath(ref.path), url.QueryEscape(ref.ref))

	body, err := p.get(ctx, api, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("reading listing for %s: %w", u.String(), err)
	}

	// The API returns a single object for a file path and an array for a
	// directory, so both shapes have to be accepted.
	var items []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		var single struct {
			Type string `json:"type"`
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &single) == nil && single.Type == "file" {
			// A file that raw could not serve (large, or a submodule) is
			// reported as such rather than as an empty directory.
			return nil, fmt.Errorf("%s is a file that could not be read directly (it may exceed GitHub's raw size limit)", u.String())
		}
		return nil, fmt.Errorf("unexpected response listing %s: %w", u.String(), err)
	}

	entries := make([]uri.Entry, 0, len(items))
	for _, item := range items {
		name := item.Name
		entries = append(entries, uri.Entry{
			Name:        name,
			URI:         u.Join(name).String(),
			Dir:         item.Type == "dir",
			Size:        item.Size,
			Description: item.Type,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})

	return &uri.Content{
		Kind:        uri.KindListing,
		Entries:     entries,
		Title:       u.String(),
		Description: fmt.Sprintf("%s/%s@%s", ref.owner, ref.repo, ref.ref),
	}, nil
}

// get performs a GET and turns non-2xx responses into actionable errors.
func (p *Provider) get(ctx context.Context, url, accept string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", url, err)
	}
	req.Header.Set("Accept", accept)
	if token := p.resolvedToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %s: %w", url, err)
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp.Body, nil
	case resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &statusError{code: resp.StatusCode, url: url,
			hint: "check the owner, repository, path and ref"}
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		_ = resp.Body.Close()
		// Unauthenticated GitHub API calls are limited to 60/hour; a bare 403
		// otherwise reads as a permissions problem and sends the agent hunting
		// for the wrong cause.
		return nil, &statusError{code: resp.StatusCode, url: url,
			hint: "GitHub's rate limit was reached — set GITHUB_TOKEN to raise it, or read a file directly instead of listing a directory"}
	default:
		_ = resp.Body.Close()
		return nil, &statusError{code: resp.StatusCode, url: url}
	}
}

func (p *Provider) resolvedToken() string {
	if p.token != "" {
		return p.token
	}
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return ""
}

// statusError carries the HTTP status plus a hint about what to do next.
type statusError struct {
	code int
	url  string
	hint string
}

func (e *statusError) Error() string {
	msg := fmt.Sprintf("GitHub returned %d for %s", e.code, e.url)
	if e.hint != "" {
		msg += " (" + e.hint + ")"
	}
	return msg
}

// escapePath escapes each path segment, keeping the separators. Escaping the
// whole string would turn "/" into "%2F" and address a nonexistent path.
func escapePath(p string) string {
	if p == "" {
		return ""
	}
	segments := strings.Split(p, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
