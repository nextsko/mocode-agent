// Package memory implements mocode's file-based session memory.
//
// Memory is stored as markdown files, one entry per file, because the durable
// artefact should be something a human can read, grep, diff and commit without
// any tooling. The layout has two tiers:
//
//	<root>/index.md                     the global index (auto-maintained)
//	<root>/global/<name>.md             durable, cross-project knowledge
//	<root>/sessions/<session>/<name>.md written during one session
//	<root>/.archive/<name>.md           forgotten entries, kept recoverable
//
// The global index records where every session's memory lives, so knowledge
// captured in one session stays findable from any other.
//
// See docs/design/18-session-memory-protocol.md.
package memory

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scope is the tier a memory belongs to.
type Scope string

const (
	// ScopeSession is memory written during a session. It lives with that
	// session and is the raw material for extraction.
	ScopeSession Scope = "session"
	// ScopeGlobal is durable, cross-project knowledge. It is the tier the agent
	// should consult before re-deriving something.
	ScopeGlobal Scope = "global"
)

// ValidScopes lists both tiers, session first because it is the default.
func ValidScopes() []Scope { return []Scope{ScopeSession, ScopeGlobal} }

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool { return s == ScopeSession || s == ScopeGlobal }

// Kind classifies what a memory is, so a listing can be scanned quickly.
type Kind string

const (
	KindNote     Kind = "note"
	KindLesson   Kind = "lesson"
	KindDecision Kind = "decision"
	KindFact     Kind = "fact"
)

// ValidKinds lists the kinds an entry may declare.
func ValidKinds() []Kind { return []Kind{KindNote, KindLesson, KindDecision, KindFact} }

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	for _, v := range ValidKinds() {
		if k == v {
			return true
		}
	}
	return false
}

// IndexFileName is the auto-maintained index at the root of the memory tree.
const IndexFileName = "index.md"

// Layout directory names.
const (
	// GlobalDir holds promoted, durable entries.
	GlobalDir = "global"
	// SessionsDir holds one directory per session.
	SessionsDir = "sessions"
	// ArchiveDir holds forgotten entries, so forgetting is never destructive.
	ArchiveDir = ".archive"
)

// Entry is one memory file.
type Entry struct {
	// Title is the human-readable one-liner.
	Title string
	// Scope is the tier.
	Scope Scope
	// Kind classifies the entry.
	Kind Kind
	// Date is when the memory was written.
	Date time.Time
	// Session is the session the knowledge came from. For a global entry this
	// is the origin session, not the one that promoted it: a memory is evidence
	// of an experience, and the session that later decided to keep it is not what
	// it is evidence of. The promoting session is deliberately not recorded —
	// PromotedFrom already carries the origin path.
	Session string
	// Tags are free-form labels for retrieval.
	Tags []string
	// Body is the markdown content after the frontmatter.
	Body string
	// PromotedFrom records the session-relative path this entry was extracted
	// from, when it came from another tier.
	PromotedFrom string

	// Path is the absolute file path. Set by the store, not by the caller.
	Path string `yaml:"-"`
	// RelPath is the path relative to the memory root, which is what the URI
	// namespace and the index use.
	RelPath string `yaml:"-"`
}

// Slug is the kebab-case form of the title used in the filename.
func (e *Entry) Slug() string { return slugify(e.Title) }

// frontmatter is the serialised form. It mirrors [Entry]'s persisted fields in
// a stable order, separate from the struct so that Path/RelPath can never leak
// into a file.
type frontmatter struct {
	Title        string   `yaml:"title"`
	Date         string   `yaml:"date"`
	Scope        Scope    `yaml:"scope"`
	Kind         Kind     `yaml:"kind"`
	Session      string   `yaml:"session,omitempty"`
	Tags         []string `yaml:"tags,omitempty"`
	PromotedFrom string   `yaml:"promoted_from,omitempty"`
}

// maxTitleLength bounds a title. Long titles make unreadable filenames and
// unreadable index rows.
const maxTitleLength = 120

// maxSlugLength bounds the slug segment of a filename. Filenames have length
// limits on every platform and a slug only needs to be recognisable.
const maxSlugLength = 60

var (
	// ErrInvalidEntry means the entry cannot be written as-is.
	ErrInvalidEntry = errors.New("invalid memory entry")
	// ErrNotFound means no entry exists at the requested path.
	ErrNotFound = errors.New("memory not found")
	// ErrUnsafePath means a path would escape the memory root.
	ErrUnsafePath = errors.New("unsafe memory path")
)

// Validate reports whether the entry can be written.
func (e *Entry) Validate() error {
	if e == nil {
		return fmt.Errorf("%w: nil entry", ErrInvalidEntry)
	}
	if strings.TrimSpace(e.Title) == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidEntry)
	}
	if len([]rune(e.Title)) > maxTitleLength {
		return fmt.Errorf("%w: title exceeds %d characters", ErrInvalidEntry, maxTitleLength)
	}
	if !e.Scope.Valid() {
		return fmt.Errorf("%w: scope must be one of %s", ErrInvalidEntry, scopeList())
	}
	if e.Kind == "" {
		e.Kind = KindNote
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: kind must be one of %s", ErrInvalidEntry, kindList())
	}
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Errorf("%w: body is required (a title with no content is not a memory)", ErrInvalidEntry)
	}
	if e.Scope == ScopeSession && strings.TrimSpace(e.Session) == "" {
		return fmt.Errorf("%w: a session memory needs the session it belongs to", ErrInvalidEntry)
	}
	if e.Date.IsZero() {
		e.Date = time.Now().UTC()
	}
	return nil
}

// FileName builds the entry's filename: <timestamp>-<session>-<slug>.md.
//
// Time and session are both in the name on purpose. Time makes a directory
// listing chronological with no tooling, and the session prefix keeps an
// extracted copy traceable to where it came from even after it leaves the
// session tree.
func (e *Entry) FileName() string {
	stamp := e.Date.UTC().Format("20060102T150405")

	parts := []string{stamp}
	if s := sessionShort(e.Session); s != "" {
		parts = append(parts, s)
	}
	parts = append(parts, e.Slug())

	return strings.Join(parts, "-") + ".md"
}

// RelDir is the directory the entry lives in, relative to the memory root.
func (e *Entry) RelDir() string {
	if e.Scope == ScopeGlobal {
		return GlobalDir
	}
	s := sessionShort(e.Session)
	if s == "" {
		s = "unknown"
	}
	return path.Join(SessionsDir, s)
}

// Render serialises the entry to markdown bytes.
func (e *Entry) Render() ([]byte, error) {
	fm, err := yaml.Marshal(frontmatter{
		Title:        e.Title,
		Date:         e.Date.UTC().Format(time.RFC3339),
		Scope:        e.Scope,
		Kind:         e.Kind,
		Session:      e.Session,
		Tags:         normalizeTags(e.Tags),
		PromotedFrom: e.PromotedFrom,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding memory frontmatter: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(e.Body))
	b.WriteString("\n")
	return []byte(b.String()), nil
}

// frontmatterRe splits a document into its YAML block and its body.
var frontmatterRe = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n?(.*)$`)

// ParseEntry parses a memory file.
func ParseEntry(data []byte) (*Entry, error) {
	m := frontmatterRe.FindSubmatch(data)
	if m == nil {
		// A file with no frontmatter is still readable as a plain note, so that
		// a hand-written entry dropped into the tree is not silently ignored.
		body := strings.TrimSpace(string(data))
		if body == "" {
			return nil, fmt.Errorf("%w: empty file", ErrInvalidEntry)
		}
		return &Entry{Title: firstLine(body), Kind: KindNote, Body: body, Date: time.Time{}}, nil
	}

	var fm frontmatter
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidEntry, err)
	}

	date, err := time.Parse(time.RFC3339, strings.TrimSpace(fm.Date))
	if err != nil {
		// An unparseable date is not fatal: the entry is still useful, and the
		// missing date shows up in the index rather than hiding the content.
		date = time.Time{}
	}

	e := &Entry{
		Title:        fm.Title,
		Scope:        fm.Scope,
		Kind:         fm.Kind,
		Date:         date,
		Session:      fm.Session,
		Tags:         fm.Tags,
		Body:         strings.TrimSpace(string(m[2])),
		PromotedFrom: fm.PromotedFrom,
	}
	if e.Title == "" {
		e.Title = firstLine(e.Body)
	}
	if e.Kind == "" {
		e.Kind = KindNote
	}
	if e.Scope == "" {
		e.Scope = ScopeGlobal
	}
	return e, nil
}

// normalizeTags lowercases, trims and deduplicates tags, preserving order.
func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// slugify converts a title into a filename-safe slug.
func slugify(s string) string {
	var b strings.Builder
	lastDash := true // leading dashes are suppressed

	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r > 0x7f:
			// Non-ASCII is dropped rather than transliterated: a wrong
			// transliteration is worse than a shorter slug, and the title in the
			// frontmatter keeps the original text intact.
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	if slug == "" {
		// A title made entirely of non-ASCII still needs a usable filename.
		slug = "memory"
	}
	return slug
}

// sessionShort returns the short form of a session id used in paths and
// filenames. Synthetic ids (`<msg>$$<call>`) contain characters that are not
// path-safe, so the segment is sanitised rather than trusted.
func sessionShort(session string) string {
	s := strings.TrimSpace(session)
	if s == "" {
		return ""
	}
	// Take the first path-safe run of the id.
	const shortLen = 8

	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			if b.Len() > 0 {
				if b.Len() >= shortLen {
					return b.String()[:shortLen]
				}
				return b.String()
			}
		}
		if b.Len() >= shortLen {
			return b.String()
		}
	}
	return b.String()
}

// SessionShort exposes the session-prefix derivation for the tool layer.
func SessionShort(session string) string { return sessionShort(session) }

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line != "" {
			return truncateRunes(line, maxTitleLength)
		}
	}
	return ""
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func scopeList() string { return joinScopes(ValidScopes()) }

func joinScopes(scopes []Scope) string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, string(s))
	}
	return strings.Join(out, ", ")
}

func kindList() string {
	kinds := ValidKinds()
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, string(k))
	}
	return strings.Join(out, ", ")
}
