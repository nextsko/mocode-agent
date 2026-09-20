package memory

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

// ExtractResult describes a promotion from the session tier to the global tier.
type ExtractResult struct {
	// Entry is the global entry that now holds the knowledge.
	Entry *Entry
	// Created is false when an identical global entry already existed.
	Created bool
	// RelPath is the global entry's root-relative path.
	RelPath string
	// SourceRelPath is the session entry it came from.
	SourceRelPath string
}

// Extract promotes a session memory into the global tier.
//
// Extraction is a *copy*, not a move: the session keeps its record of what
// happened, while the global tier gains the part worth carrying forward. Moving
// would make the session's own history a lie.
//
// It is idempotent. Promoting the same entry twice does not create a second
// copy, because the global filename is derived from the source path rather than
// from the time of extraction — otherwise every re-run would mint a duplicate
// and the global tier would fill with near-identical entries.
func Extract(ctx context.Context, store *Store, sourceRelPath string, opts ExtractOptions) (*ExtractResult, error) {
	source, err := store.Read(sourceRelPath)
	if err != nil {
		return nil, err
	}
	if source.Scope == ScopeGlobal {
		return nil, fmt.Errorf("%w: %s is already in the global tier", ErrInvalidEntry, sourceRelPath)
	}

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = source.Title
	}
	body := strings.TrimSpace(opts.Body)
	if body == "" {
		body = source.Body
	}

	kind := opts.Kind
	if kind == "" {
		kind = source.Kind
	}

	// The date, and therefore the filename, is pinned to the *source* entry.
	// Extracting is a reclassification, not a new event, and deriving the name
	// from the source is what makes a repeat run idempotent.
	e := &Entry{
		Title:        title,
		Scope:        ScopeGlobal,
		Kind:         kind,
		Date:         source.Date,
		Session:      source.Session,
		Tags:         mergeTags(source.Tags, opts.Tags),
		Body:         body,
		PromotedFrom: strings.TrimPrefix(strings.TrimSpace(sourceRelPath), "/"),
	}
	if e.Date.IsZero() {
		// A source with no parseable date still needs a deterministic name, so
		// fall back to the file's own timestamp rather than to "now".
		e.Date = sourceDateFallback(store, source)
	}

	// If the pinned filename already exists, this exact promotion has run
	// before: report it rather than writing a duplicate.
	if existing, path := globalPathFor(store, e); existing {
		got, err := store.Read(path)
		if err == nil {
			return &ExtractResult{Entry: got, Created: false, RelPath: path, SourceRelPath: sourceRelPath}, nil
		}
	}

	res, err := store.Write(ctx, e)
	if err != nil {
		return nil, err
	}
	return &ExtractResult{
		Entry:         e,
		Created:       true,
		RelPath:       res.RelPath,
		SourceRelPath: sourceRelPath,
	}, nil
}

// ExtractOptions customises a promotion; every field defaults to the source.
type ExtractOptions struct {
	// Title overrides the promoted entry's title.
	Title string
	// Body overrides the promoted entry's body, for distilling a long session
	// note into the part worth keeping.
	Body string
	// Kind overrides the promoted entry's kind.
	Kind Kind
	// Tags are added to the source's tags.
	Tags []string
}

// globalPathFor reports whether the entry's global file already exists.
func globalPathFor(store *Store, e *Entry) (bool, string) {
	relPath := path.Join(GlobalDir, e.FileName())
	if _, err := store.Read(relPath); err == nil {
		return true, relPath
	}
	return false, relPath
}

// sourceDateFallback derives a stable date for an entry whose frontmatter had
// none, preferring a timestamp embedded in its filename.
func sourceDateFallback(store *Store, source *Entry) time.Time {
	if base := path.Base(source.RelPath); base != "" {
		if len(base) >= 15 {
			if t, err := time.Parse("20060102T150405", base[:15]); err == nil {
				return t.UTC()
			}
		}
	}
	// Last resort: the file's modification time is still more meaningful than
	// the moment of extraction.
	if source.Path != "" {
		if info, err := statFile(source.Path); err == nil {
			return info.ModTime().UTC()
		}
	}
	return store.now().UTC()
}

// mergeTags combines two tag lists, preserving the source's order first.
func mergeTags(base, extra []string) []string {
	out := make([]string, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return normalizeTags(out)
}

// ExtractAll promotes every entry of a session, for when a whole session turned
// out to be worth keeping.
func ExtractAll(ctx context.Context, store *Store, session string, opts ExtractOptions) ([]*ExtractResult, error) {
	entries, err := store.List(ctx, ListOptions{Scope: ScopeSession, Session: session})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: no session memory for %q", ErrNotFound, session)
	}

	results := make([]*ExtractResult, 0, len(entries))
	for _, e := range entries {
		res, err := Extract(ctx, store, e.RelPath, opts)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}
