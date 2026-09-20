package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

// fakeGitHub serves the two upstreams the provider talks to, and records the
// requests so the URL construction can be asserted — that is where the subtle
// bugs live (double-escaping a path turns "a/b" into "a%2Fb" and 404s).
type fakeGitHub struct {
	raw map[string]string // path after the host -> body
	api map[string]string // path+query -> JSON body

	rawStatus int
	apiStatus int

	rawRequests []string
	apiRequests []string
	authHeaders []string
}

func (f *fakeGitHub) start(t *testing.T) (*Provider, *fakeGitHub) {
	t.Helper()

	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath, not Path: the server decodes Path, so asserting on it
		// would hide exactly the escaping bug this test exists to catch.
		f.rawRequests = append(f.rawRequests, r.URL.EscapedPath())
		f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
		if f.rawStatus != 0 && f.rawStatus != http.StatusOK {
			w.WriteHeader(f.rawStatus)
			return
		}
		body, ok := f.raw[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path + "?" + r.URL.RawQuery
		f.apiRequests = append(f.apiRequests, key)
		if f.apiStatus != 0 && f.apiStatus != http.StatusOK {
			w.WriteHeader(f.apiStatus)
			return
		}
		body, ok := f.api[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))

	t.Cleanup(raw.Close)
	t.Cleanup(api.Close)

	return New(raw.Client(), WithBaseURLs(raw.URL, api.URL), WithToken("")), f
}

func TestProvider_AuthorityAndSummary(t *testing.T) {
	t.Parallel()

	p, _ := (&fakeGitHub{}).start(t)
	assert.Equal(t, uri.AuthorityGitHub, p.Authority())
	assert.Contains(t, p.Summary(), "mocode://github/")
}

// A reference without owner/repo cannot be resolved, and the error must show
// the shape that works rather than just failing.
func TestProvider_RequiresOwnerAndRepo(t *testing.T) {
	t.Parallel()

	p, _ := (&fakeGitHub{}).start(t)
	for _, raw := range []string{"mocode://github", "mocode://github/only-owner"} {
		_, err := p.Resolve(context.Background(), mustParse(t, raw))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "<owner>/<repo>")
	}
}

func TestProvider_ReadsFileOverRaw(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{raw: map[string]string{
		"/cli/cli/HEAD/README.md": "# cli\n",
	}}
	p, _ := f.start(t)

	content, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/cli/cli/README.md"))
	require.NoError(t, err)

	assert.Equal(t, uri.KindText, content.Kind)
	assert.Equal(t, "# cli\n", content.Text)
	assert.Equal(t, "README.md", content.Title)

	// A nested path must keep its separators: escaping the whole string would
	// request a single segment named "docs%2Fintro.md" and 404.
	assert.Equal(t, []string{"/cli/cli/HEAD/README.md"}, f.rawRequests)
}

func TestProvider_NestedPathKeepsSeparators(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{raw: map[string]string{
		"/o/r/main/docs/plans/01-x.md": "nested\n",
	}}
	p, _ := f.start(t)

	content, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/docs/plans/01-x.md?ref=main"))
	require.NoError(t, err)
	assert.Equal(t, "nested\n", content.Text)
	assert.Equal(t, []string{"/o/r/main/docs/plans/01-x.md"}, f.rawRequests)
}

// A path with characters that need escaping must be escaped per segment, and
// only per segment.
func TestProvider_EscapesSegmentsIndividually(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{}
	p, _ := f.start(t)

	// The upstream will 404, which sends us to the listing; the assertion is on
	// the request the raw fetch actually made.
	_, _ = p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/a b/c#d.md"))

	require.NotEmpty(t, f.rawRequests)
	assert.Equal(t, "/o/r/HEAD/a%20b/c%23d.md", f.rawRequests[0])
	assert.NotContains(t, f.rawRequests[0], "%2F", "separators must not be escaped")
}

// A file that raw cannot serve falls through to the contents API, which is how
// directories are discovered — so a 404 from raw is not an error by itself.
func TestProvider_FallsThroughToDirectoryListing(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{api: map[string]string{
		"/repos/o/r/contents/?ref=HEAD": `[
			{"name":"README.md","path":"README.md","type":"file","size":10},
			{"name":"docs","path":"docs","type":"dir","size":0},
			{"name":"src","path":"src","type":"dir","size":0}
		]`,
	}}
	p, _ := f.start(t)

	content, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r"))
	require.NoError(t, err)

	assert.Equal(t, uri.KindListing, content.Kind)
	require.Len(t, content.Entries, 3)

	// Directories sort first: the shape the model should scan.
	assert.Equal(t, "docs", content.Entries[0].Name)
	assert.True(t, content.Entries[0].Dir)
	assert.Equal(t, "src", content.Entries[1].Name)
	assert.Equal(t, "README.md", content.Entries[2].Name)

	for _, e := range content.Entries {
		assert.True(t, strings.HasPrefix(e.URI, "mocode://github/o/r/"), "%s must be addressable", e.Name)
	}
}

func TestProvider_ListsSubdirectory(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{api: map[string]string{
		"/repos/o/r/contents/docs?ref=HEAD": `[{"name":"a.md","path":"docs/a.md","type":"file","size":3}]`,
	}}
	p, _ := f.start(t)

	content, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/docs"))
	require.NoError(t, err)
	require.Len(t, content.Entries, 1)
	assert.Equal(t, "a.md", content.Entries[0].Name)
}

// A single object is returned by the contents API for a file path; reporting
// that as an empty directory would be actively misleading.
func TestProvider_FileObjectFromAPIExplainsItself(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{api: map[string]string{
		"/repos/o/r/contents/big.bin?ref=HEAD": `{"type":"file","path":"big.bin"}`,
	}}
	p, _ := f.start(t)

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/big.bin"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be read directly")
}

// Unauthenticated GitHub API calls are limited to 60/hour. A bare 403 reads as
// a permissions problem and sends the agent hunting for the wrong cause.
func TestProvider_RateLimitIsExplained(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{apiStatus: http.StatusForbidden}
	p, _ := f.start(t)

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
	assert.Contains(t, err.Error(), "GITHUB_TOKEN")
}

func TestProvider_NotFoundHintsAtTheRef(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{apiStatus: http.StatusNotFound}
	p, _ := f.start(t)

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/nope.md"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "owner")
	assert.Contains(t, err.Error(), "ref")
}

func TestProvider_ImageExtensionReturnsImage(t *testing.T) {
	t.Parallel()

	png := "\x89PNG\r\n\x1a\n-fake"
	f := &fakeGitHub{raw: map[string]string{"/o/r/HEAD/docs/a.png": png}}
	p, _ := f.start(t)

	content, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/docs/a.png"))
	require.NoError(t, err)

	assert.Equal(t, uri.KindImage, content.Kind)
	assert.Equal(t, "image/png", content.MIME)
	assert.Equal(t, png, string(content.Data))
}

// Binary that is not a known image must be refused with a clear reason rather
// than returned as mojibake.
func TestProvider_BinaryIsRefused(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{raw: map[string]string{"/o/r/HEAD/x.bin": "\xff\xfe\x00"}}
	p, _ := f.start(t)

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/x.bin"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not UTF-8")
}

func TestProvider_RejectsOversizedFile(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{raw: map[string]string{"/o/r/HEAD/big.txt": strings.Repeat("x", 5000)}}
	p, _ := f.start(t)
	p.maxFileSize = 1000

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/big.txt"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
}

func TestProvider_TokenFromOption(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{raw: map[string]string{"/o/r/HEAD/a.md": "x"}}
	raw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.authHeaders = append(f.authHeaders, r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, "x")
	}))
	defer raw.Close()

	p := New(raw.Client(), WithBaseURLs(raw.URL, raw.URL), WithToken("secret-token"))
	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/a.md"))
	require.NoError(t, err)

	require.Len(t, f.authHeaders, 1)
	assert.Equal(t, "Bearer secret-token", f.authHeaders[0])
}

// The token also comes from the environment, which is how a user raises the API
// rate limit without any config change.
func TestProvider_TokenFromEnvironment(t *testing.T) {
	// No t.Parallel: t.Setenv forbids it.
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		_, _ = fmt.Fprint(w, "x")
	}))
	defer srv.Close()

	t.Setenv("GITHUB_TOKEN", "env-token")
	p := New(srv.Client(), WithBaseURLs(srv.URL, srv.URL))

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/a.md"))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Bearer env-token", got[0])
}

func TestProvider_DefaultRefIsHEAD(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{raw: map[string]string{"/o/r/HEAD/a.md": "x"}}
	p, _ := f.start(t)

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r/a.md"))
	require.NoError(t, err)
	assert.Equal(t, "/o/r/HEAD/a.md", f.rawRequests[0])
}

func TestParseRepoRef(t *testing.T) {
	t.Parallel()

	u := mustParse(t, "mocode://github/o/r.git/a/b.md?ref=v1.2.3")
	ref, err := parseRepoRef(u)
	require.NoError(t, err)

	assert.Equal(t, "o", ref.owner)
	assert.Equal(t, "r", ref.repo, "a trailing .git is accepted and stripped")
	assert.Equal(t, "a/b.md", ref.path)
	assert.Equal(t, "v1.2.3", ref.ref)
}

func TestEscapePath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "", escapePath(""))
	assert.Equal(t, "a/b.md", escapePath("a/b.md"))
	assert.Equal(t, "a%20b/c%23d.md", escapePath("a b/c#d.md"))
	assert.Equal(t, "a%3Fb", escapePath("a?b"))
}

func TestImageMIME(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"a.png": "image/png", "A.PNG": "image/png", "b.jpg": "image/jpeg",
		"c.jpeg": "image/jpeg", "d.gif": "image/gif", "e.webp": "image/webp",
	} {
		got, ok := imageMIME(name)
		require.True(t, ok, name)
		assert.Equal(t, want, got)
	}

	for _, name := range []string{"a.md", "b.go", "c.txt", "d.png.txt"} {
		_, ok := imageMIME(name)
		assert.False(t, ok, name)
	}
}

// The provider must never invent content: an unexpected API payload is an
// error, not an empty listing.
func TestProvider_MalformedAPIPayloadIsAnError(t *testing.T) {
	t.Parallel()

	f := &fakeGitHub{api: map[string]string{
		"/repos/o/r/contents/?ref=HEAD": `not json at all`,
	}}
	p, _ := f.start(t)

	_, err := p.Resolve(context.Background(), mustParse(t, "mocode://github/o/r"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected response")
}

func TestStatusErrorIncludesHint(t *testing.T) {
	t.Parallel()

	err := &statusError{code: 500, url: "http://x", hint: "try again"}
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "try again")

	plain := &statusError{code: 500, url: "http://x"}
	assert.NotContains(t, plain.Error(), "(")

	// The rate-limit path must be reachable via errors.As, which is how the
	// caller distinguishes "no such file" from "try again later".
	var se *statusError
	require.True(t, errors.As(error(&statusError{code: 404}), &se))
}

func mustParse(t *testing.T, raw string) uri.URI {
	t.Helper()
	u, err := uri.Parse(raw)
	require.NoError(t, err)
	return u
}
