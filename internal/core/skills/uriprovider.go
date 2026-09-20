package skills

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/uri"
)

// NewURIProvider exposes skills as `mocode://skills/<name>/<file>`.
//
// This replaces the hardcoded prefix check that used to live in the View tool.
// The mapping is unchanged — `mocode://skills/<rel>` still resolves to
// `builtin/<rel>` in the embedded filesystem — but it is now registered in the
// protocol's registry alongside every other namespace, so one resolver serves
// all of them and the scheme is documented in one place.
//
// userDirs are searched after the embedded tree, so a user skill or note of the
// same name is reachable without shadowing the shipped one for builtin reads.
func NewURIProvider(userDirs ...string) uri.Provider {
	trees := []uri.Tree{{Label: "builtin", FS: builtinTree()}}
	for _, dir := range userDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		// A missing user directory is normal (no user skills installed); the
		// tree is skipped rather than failing the whole namespace.
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		trees = append(trees, uri.Tree{Label: dir, FS: os.DirFS(dir)})
	}

	p := uri.NewTreeProvider(uri.AuthoritySkills,
		"Agent Skills already installed: builtin skills ship with mocode, user skills live in the configured skills directories.",
		trees...)
	// Listings show each skill's trigger description, which is the thing that
	// tells the agent whether to open the file at all.
	p.Describe = describeSkillFile
	return p
}

// builtinTree returns the embedded skills rooted so that a URI path maps
// directly onto it: `mocode://skills/jq/SKILL.md` → `jq/SKILL.md`.
func builtinTree() fs.FS {
	sub, err := fs.Sub(BuiltinFS(), "builtin")
	if err != nil {
		// embed.FS with a `builtin/*` pattern always contains "builtin"; a nil
		// return here would be a build-time impossibility, and degrading to an
		// empty tree keeps the provider usable rather than panicking.
		return nil
	}
	return sub
}

// describeSkillFile summarises a skill by its description, falling back to the
// first heading for non-SKILL.md files such as references.
func describeSkillFile(relPath string, data []byte) string {
	if !strings.HasSuffix(relPath, SkillFileName) {
		return ""
	}
	s, err := ParseContent(data)
	if err != nil {
		return ""
	}
	return SkillGist(s.Description)
}

// SkillsDirIn reports whether absPath lies inside any of dirs, after resolving
// symlinks. Callers use it to decide that a path is trusted (readable without a
// permission prompt, and without size limits).
//
// Both sides go through symlink resolution: a path could otherwise be inside a
// skills directory via a symlink and still compare as outside, or the reverse.
func SkillsDirIn(absPath string, dirs []string) bool {
	resolved, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		resolved = absPath
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return false
	}

	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		resolvedDir, derr := filepath.EvalSymlinks(dir)
		if derr != nil {
			resolvedDir = dir
		}
		resolvedDir, derr = filepath.Abs(resolvedDir)
		if derr != nil {
			continue
		}
		rel, rerr := filepath.Rel(resolvedDir, resolved)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		return true
	}
	return false
}

// ExpandDirs expands `~` and `$VAR` in each directory and drops the ones that
// do not exist.
//
// Discovery expands these paths before use (see the coordinator's skill
// discovery), while the View tool historically received them raw. That
// inconsistency made a `~/.config/...` entry silently fail the containment test,
// so the exemption it was meant to grant never applied. Expanding once here
// keeps a single meaning for "a configured skills directory".
func ExpandDirs(dirs []string, expand func(string) string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if expand != nil {
			dir = expand(dir)
		}
		out = append(out, dir)
	}
	return out
}

// URIRegistry wires the skills namespace into a registry.
func URIRegistry(userDirs ...string) *uri.Registry {
	r := uri.NewRegistry()
	r.Register(NewURIProvider(userDirs...))
	return r
}

// ResolveURI reads a builtin or user skill file through the protocol. It is a
// convenience for callers that already know they are addressing a skill.
func ResolveURI(ctx context.Context, raw string, userDirs ...string) (*uri.Content, error) {
	content, _, err := URIRegistry(userDirs...).Resolve(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", raw, err)
	}
	return content, nil
}
