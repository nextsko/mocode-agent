package uri

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// Tree is one searchable file tree behind an authority.
//
// A tree is an [fs.FS] rather than a directory path so the same provider serves
// an embedded filesystem (builtin skills), an on-disk directory (docs, `.exps`)
// and, later, anything else that can present an fs.FS.
type Tree struct {
	// Label names the tree in listings and provenance, e.g. "builtin".
	Label string
	// FS is the tree root. Paths in a URI are resolved relative to it.
	FS fs.FS
}

// TreeProvider serves an authority backed by one or more file trees, searched
// in order.
//
// Order is the override mechanism: for skills the embedded tree is first and a
// user directory second, so a user skill of the same name wins only where
// something explicit asks for it, while reads stay predictable.
type TreeProvider struct {
	authority string
	summary   string
	trees     []Tree

	// Describe optionally summarises a file for listings. Returning "" falls
	// back to the first heading or the first non-empty line.
	Describe func(relPath string, data []byte) string
}

// NewTreeProvider builds a provider over the given trees. Trees with a nil FS
// are ignored, so a caller can pass an optional root unconditionally.
func NewTreeProvider(authority, summary string, trees ...Tree) *TreeProvider {
	kept := make([]Tree, 0, len(trees))
	for _, t := range trees {
		if t.FS == nil {
			continue
		}
		kept = append(kept, t)
	}
	return &TreeProvider{authority: authority, summary: summary, trees: kept}
}

// Authority implements [Provider].
func (p *TreeProvider) Authority() string { return p.authority }

// Summary implements [Provider].
func (p *TreeProvider) Summary() string { return p.summary }

// Resolve implements [Provider].
func (p *TreeProvider) Resolve(_ context.Context, u URI) (*Content, error) {
	if u.IsListing() {
		return p.list(u, "")
	}

	// A path that names a directory lists it. Checking this before the read
	// gives "here is what is inside" instead of "is a directory", which is what
	// the caller actually wants when exploring.
	if p.isDir(u.Path) {
		return p.list(u, u.Path)
	}

	data, tree, rel, err := p.read(u.Path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: %w (only UTF-8 text is served)", u.String(), ErrNotFound)
	}

	title := path.Base(rel)
	desc := ""
	if p.Describe != nil {
		desc = p.Describe(rel, data)
	}
	if desc == "" {
		desc = firstLineOf(data)
	}

	return &Content{
		Kind:        KindText,
		Text:        string(data),
		Title:       title,
		Description: desc,
		Source:      fmt.Sprintf("%s (%s)", u.String(), tree),
	}, nil
}

// read finds the first tree containing relPath and returns its contents.
func (p *TreeProvider) read(relPath string) (data []byte, treeLabel, rel string, err error) {
	for _, t := range p.trees {
		b, rerr := fs.ReadFile(t.FS, relPath)
		if rerr == nil {
			return b, t.Label, relPath, nil
		}
		if !isNotExist(rerr) {
			return nil, "", "", fmt.Errorf("%s: %w", relPath, rerr)
		}
	}

	available := p.roots()
	return nil, "", "", fmt.Errorf("%w: %s (looked in %s)",
		ErrNotFound, relPath, strings.Join(available, ", "))
}

// isDir reports whether relPath names a directory in any tree.
func (p *TreeProvider) isDir(relPath string) bool {
	for _, t := range p.trees {
		if info, err := fs.Stat(t.FS, relPath); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// list renders the children of a directory (or the trees' roots when dir is
// empty). Entries are merged across trees and deduplicated, first tree winning.
func (p *TreeProvider) list(u URI, dir string) (*Content, error) {
	seen := map[string]Entry{}

	for _, t := range p.trees {
		target := dir
		if target == "" {
			target = "."
		}

		entries, err := fs.ReadDir(t.FS, target)
		if err != nil {
			// A missing directory in one tree is not an error: the resource may
			// simply live in another tree.
			if isNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("listing %s: %w", target, err)
		}

		for _, entry := range entries {
			name := entry.Name()
			// Hidden entries are noise in a listing (and, for embedded trees,
			// cannot appear anyway because embed skips dotfiles).
			if strings.HasPrefix(name, ".") {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}

			child := dir
			if child == "" {
				child = name
			} else {
				child = dir + "/" + name
			}

			item := Entry{Name: name, URI: u.Join(name).String(), Dir: entry.IsDir()}
			if info, ierr := entry.Info(); ierr == nil {
				item.Size = info.Size()
			}
			if !entry.IsDir() {
				item.Description = p.describeEntry(t, child)
			}
			seen[name] = item
		}
	}

	if len(seen) == 0 && dir != "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, dir)
	}

	entries := make([]Entry, 0, len(seen))
	for _, e := range seen {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		// Directories first, then alphabetical: the shape a file browser uses.
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})

	return &Content{
		Kind:        KindListing,
		Entries:     entries,
		Title:       u.String(),
		Description: p.summary,
	}, nil
}

// describeEntry summarises a file for a listing, cheaply: only small files are
// opened, so listing a large tree cannot become an expensive operation.
func (p *TreeProvider) describeEntry(t Tree, rel string) string {
	const maxDescribeSize = 8 * 1024

	info, err := fs.Stat(t.FS, rel)
	if err != nil || info.Size() > maxDescribeSize {
		return ""
	}
	data, err := fs.ReadFile(t.FS, rel)
	if err != nil {
		return ""
	}
	if p.Describe != nil {
		if d := p.Describe(rel, data); d != "" {
			return d
		}
	}
	return firstLineOf(data)
}

// roots lists the tree labels for error messages.
func (p *TreeProvider) roots() []string {
	out := make([]string, 0, len(p.trees))
	for _, t := range p.trees {
		out = append(out, t.Label)
	}
	return out
}

// firstLineOf extracts a usable one-liner: the first markdown heading if there
// is one, otherwise the first non-empty line.
//
// A leading YAML frontmatter block is skipped first. Without that, every
// SKILL.md and every frontmattered doc would be summarised by its first field
// ("name: jq"), which tells the reader nothing about whether to open it.
func firstLineOf(data []byte) string {
	lines := strings.Split(string(data), "\n")

	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		start = 1
		for start < len(lines) {
			if strings.TrimSpace(lines[start]) == "---" {
				start++
				break
			}
			start++
		}
	}

	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || line == "---" {
			continue
		}
		line = strings.TrimPrefix(line, "# ")
		line = strings.TrimPrefix(line, "## ")
		return truncate(line, 100)
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// isNotExist reports whether err means "no such file or directory".
//
// This must use errors.Is, not a sentinel comparison: os.DirFS wraps the failure
// in *fs.PathError, so comparing directly misses it. The consequence was
// platform-visible — on Windows the raw OS message ("The system cannot find the
// file specified") escaped instead of the provider's "looked in …" guidance, and
// a file missing from one tree aborted the search instead of trying the next.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
