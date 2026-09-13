package shell

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

// The `fd` command. A real `fd` binary is preferred when installed (see
// hostFdPath and pipeUtilsHandler); this Go implementation is the fallback so
// the command also works where `fd` is absent (notably Windows). It mirrors the
// common subset of fd's interface: regex (default) or glob (-g) matching on file
// names, gitignore-aware traversal, hidden-file skipping, type/extension
// filters and depth/result limits.

// hostFdPath returns the path to a real `fd` binary, or "" when none is
// installed. Cached for the process lifetime, mirroring searchcommon.GetRg.
var hostFdPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("fd")
	if err != nil {
		return ""
	}
	return p
})

// fdOptions is the parsed command line for the `fd` fallback.
type fdOptions struct {
	pattern       string
	root          string
	hidden        bool
	noIgnore      bool
	absolute      bool
	glob          bool
	caseSensitive bool
	ignoreCase    bool
	exts          []string
	fileOnly      bool
	dirOnly       bool
	maxDepth      int
	maxResults    int
}

func runFd(env pipeEnv, args []string) error {
	const name = "fd"
	opts, err := parseFdArgs(args)
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}

	root := opts.root
	if !filepath.IsAbs(root) {
		root = filepath.Join(env.dir, root)
	}
	info, statErr := os.Stat(root)
	if statErr != nil {
		return pipeErr(name, fmt.Sprintf("%s: %v", opts.root, statErr), env.stderr)
	}
	if !info.IsDir() {
		return pipeErr(name, fmt.Sprintf("%s: is not a directory", opts.root), env.stderr)
	}

	matcher, err := fdMatcher(opts.pattern, opts.caseSensitive, opts.ignoreCase, opts.glob)
	if err != nil {
		return pipeErr(name, fmt.Sprintf("invalid pattern %q: %v", opts.pattern, err), env.stderr)
	}

	entries, walkErr := fdWalk(root, opts)
	if walkErr != nil {
		return pipeErr(name, walkErr.Error(), env.stderr)
	}

	count := 0
	for _, p := range entries {
		if !fdMatch(root, p, matcher, opts) {
			continue
		}
		fmt.Fprintln(env.stdout, fdDisplayPath(p, env.dir, opts.absolute))
		count++
		if opts.maxResults > 0 && count >= opts.maxResults {
			break
		}
	}
	return nil
}

// fdWalk collects candidate paths under root. Unless opts.noIgnore is set it
// reuses the gitignore-aware lister from fsext (same ignore semantics as the
// glob/ls tools); otherwise it does a plain walk.
func fdWalk(root string, opts fdOptions) ([]string, error) {
	if !opts.noIgnore {
		entries, _, err := fsext.ListDirectory(root, nil, opts.maxDepth, 0)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", root, err)
		}
		for i, p := range entries {
			entries[i] = filepath.FromSlash(p)
		}
		return entries, nil
	}

	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // unreadable entries are skipped, mirroring fd's tolerance
			return nil // skip unreadable entries
		}
		if path == root {
			return nil
		}
		if opts.maxDepth > 0 && fdDepth(root, path) > opts.maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !opts.hidden && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			entries = append(entries, path+string(filepath.Separator))
			return nil
		}
		entries = append(entries, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Strings(entries)
	return entries, nil
}

// fdMatch reports whether an entry (as returned by fdWalk) satisfies the
// type/extension/visibility/name filters.
func fdMatch(root, p string, match func(string) bool, opts fdOptions) bool {
	raw := fdTrimDirSep(p)
	isDir := raw != p

	rel, err := filepath.Rel(root, raw)
	if err != nil {
		rel = raw
	}
	if !opts.hidden && fdIsHidden(rel) {
		return false
	}
	if opts.fileOnly && isDir {
		return false
	}
	if opts.dirOnly && !isDir {
		return false
	}

	base := filepath.Base(raw)
	if len(opts.exts) > 0 {
		if isDir {
			return false
		}
		ok := false
		for _, e := range opts.exts {
			if strings.HasSuffix(base, "."+e) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return match(base)
}

// fdMatcher compiles the fd pattern into a predicate over file names. Empty
// patterns match everything. Glob mode uses filepath.Match; regex mode uses
// Go's regexp with fd's default smart-case (case-insensitive unless the pattern
// contains an uppercase letter).
func fdMatcher(pattern string, caseSensitive, ignoreCase, glob bool) (func(string) bool, error) {
	if pattern == "" {
		return func(string) bool { return true }, nil
	}
	ci := ignoreCase
	if !caseSensitive && !ignoreCase {
		ci = !hasUpper(pattern)
	}
	if glob {
		pat := pattern
		if ci {
			pat = strings.ToLower(pat)
		}
		return func(base string) bool {
			if ci {
				base = strings.ToLower(base)
			}
			ok, _ := filepath.Match(pat, base)
			return ok
		}, nil
	}
	expr := pattern
	if ci {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	return re.MatchString, nil
}

func parseFdArgs(args []string) (fdOptions, error) {
	opts := fdOptions{root: "."}
	var positionals []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positionals = append(positionals, args[i+1:]...)
			i = len(args)
		case a == "-" || !strings.HasPrefix(a, "-"):
			positionals = append(positionals, a)
		case a == "-H" || a == "--hidden":
			opts.hidden = true
		case a == "-I" || a == "--no-ignore":
			opts.noIgnore = true
		case a == "-a" || a == "--absolute-path":
			opts.absolute = true
		case a == "-g" || a == "--glob":
			opts.glob = true
		case a == "-i" || a == flagIgnoreCase:
			opts.ignoreCase = true
		case a == "-s" || a == "--case-sensitive":
			opts.caseSensitive = true
		case a == "-L" || a == "--follow" || a == "-1" || a == "-c" || a == "--color":
			// accepted for compatibility; no effect in this fallback
		case a == "-e" || a == "--extension":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			opts.exts = append(opts.exts, strings.TrimPrefix(v, "."))
		case strings.HasPrefix(a, "--extension="):
			opts.exts = append(opts.exts, strings.TrimPrefix(strings.TrimPrefix(a, "--extension="), "."))
		case strings.HasPrefix(a, "-e") && len(a) > 2:
			opts.exts = append(opts.exts, strings.TrimPrefix(a[2:], "."))
		case a == "-t" || a == "--type":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			applyFdType(&opts, v)
		case strings.HasPrefix(a, "--type="):
			applyFdType(&opts, strings.TrimPrefix(a, "--type="))
		case strings.HasPrefix(a, "-t") && len(a) > 2:
			applyFdType(&opts, a[2:])
		case a == "-d" || a == "--max-depth":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			n, err := fdUint(v, a)
			if err != nil {
				return opts, err
			}
			opts.maxDepth = n
		case strings.HasPrefix(a, "--max-depth="):
			n, err := fdUint(strings.TrimPrefix(a, "--max-depth="), a)
			if err != nil {
				return opts, err
			}
			opts.maxDepth = n
		case a == "--max-results":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			n, err := fdUint(v, a)
			if err != nil {
				return opts, err
			}
			opts.maxResults = n
		case strings.HasPrefix(a, "--max-results="):
			n, err := fdUint(strings.TrimPrefix(a, "--max-results="), a)
			if err != nil {
				return opts, err
			}
			opts.maxResults = n
		default:
			return opts, fmt.Errorf("unrecognized option %q", a)
		}
	}

	switch {
	case len(positionals) == 0:
	case len(positionals) == 1:
		opts.pattern = positionals[0]
	case len(positionals) == 2:
		opts.pattern = positionals[0]
		opts.root = positionals[1]
	default:
		return opts, fmt.Errorf("too many paths given")
	}
	return opts, nil
}

func applyFdType(opts *fdOptions, v string) {
	switch v {
	case "f", "file":
		opts.fileOnly = true
	case "d", "dir", "directory":
		opts.dirOnly = true
	default:
		// other types (symlink, executable, empty, socket, ...) are ignored
	}
}

func fdNextValue(args []string, i *int, flag string) (string, error) {
	if *i+1 >= len(args) {
		return "", fmt.Errorf("option %s requires an argument", flag)
	}
	*i++
	return args[*i], nil
}

func fdUint(s, flag string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid value for %s: %q", flag, s)
	}
	return n, nil
}

// fdIsHidden reports whether any component of rel (relative to the search root)
// is a dot-file, matching fd's default hidden handling.
func fdIsHidden(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if len(part) > 1 && part[0] == '.' {
			return true
		}
	}
	return false
}

func fdDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0
	}
	return len(strings.Split(filepath.ToSlash(rel), "/"))
}

func fdTrimDirSep(p string) string {
	p = strings.TrimSuffix(p, string(filepath.Separator))
	return strings.TrimSuffix(p, "/")
}

func fdDisplayPath(p, dir string, absolute bool) string {
	p = fdTrimDirSep(p)
	if absolute {
		return p
	}
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return p
	}
	return rel
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}
