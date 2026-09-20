package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

// Provider exposes the memory tree as `mocode://memory/...`.
//
// The root resolves to the *index*, derived live rather than read from disk, so
// `view mocode://memory` always reflects what is actually stored — including
// memory another session wrote, which is the whole point of the index recording
// session locations.
type Provider struct {
	store   *Store
	indexer Indexer
	tree    uri.Provider
}

// NewURIProvider wires the memory namespace to a store. indexer nil uses the
// file-scanning implementation.
func NewURIProvider(store *Store, indexer Indexer) uri.Provider {
	if store == nil {
		return nil
	}
	if indexer == nil {
		indexer = NewFileIndexer(store)
	}

	var trees []uri.Tree
	if _, err := os.Stat(store.Root()); err == nil {
		trees = append(trees, uri.Tree{Label: "memory", FS: os.DirFS(store.Root())})
	}

	p := uri.NewTreeProvider(uri.AuthorityMemory,
		"Session and global memory: durable notes written by past sessions. "+
			"Index at mocode://memory/, durable entries under global/, per-session notes under sessions/<session>/.",
		trees...)
	// Entries describe themselves by their own title, which is more useful in a
	// listing than the first line of the file.
	p.Describe = describeMemoryFile

	return &Provider{store: store, indexer: indexer, tree: p}
}

// Authority implements [uri.Provider].
func (p *Provider) Authority() string { return uri.AuthorityMemory }

// Summary implements [uri.Provider].
func (p *Provider) Summary() string { return p.tree.Summary() }

// Resolve implements [uri.Provider].
func (p *Provider) Resolve(ctx context.Context, u uri.URI) (*uri.Content, error) {
	// The root is the index, derived from the live tree. A stored index.md would
	// be one hand-edit away from lying about what is in the tree.
	if u.IsListing() {
		idx, err := p.indexer.Rebuild(ctx)
		if err != nil {
			return nil, fmt.Errorf("building the memory index: %w", err)
		}
		return &uri.Content{
			Kind:        uri.KindText,
			Text:        string(p.indexer.Render(idx)),
			Title:       "memory index",
			Description: fmt.Sprintf("%d entries (%d global, %d session)", idx.Total(), len(idx.Global), idx.SessionEntryCount()),
			Source:      u.String(),
		}, nil
	}

	// Everything else is a file in the tree.
	if err := checkPath(p.store, u.Path); err != nil {
		return nil, err
	}
	return p.tree.Resolve(ctx, u)
}

// checkPath turns a missing path into the protocol's not-found error, so the
// caller sees the same vocabulary regardless of which layer noticed. A
// directory is allowed through: the tree provider lists it.
func checkPath(store *Store, relPath string) error {
	if _, err := store.Read(relPath); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	abs := filepath.Join(store.Root(), filepath.FromSlash(relPath))
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return nil
	}
	return fmt.Errorf("%w: %s", uri.ErrNotFound, relPath)
}

// describeMemoryFile summarises a memory entry by its own title.
func describeMemoryFile(relPath string, data []byte) string {
	e, err := ParseEntry(data)
	if err != nil {
		return ""
	}
	label := string(e.Scope)
	if e.Kind != "" {
		label += "/" + string(e.Kind)
	}
	desc := fmt.Sprintf("[%s] %s", label, e.Title)
	if len(e.Tags) > 0 {
		desc += " · " + joinTags(e.Tags)
	}
	return desc
}

func joinTags(tags []string) string {
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += ","
		}
		out += t
	}
	return out
}
