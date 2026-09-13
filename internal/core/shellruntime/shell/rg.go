package shell

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"mvdan.cc/sh/v3/interp"

	"github.com/nextsko/mocode-agent/internal/util/fsext"
)

// The `rg` command. A real `rg` binary is preferred when installed (see
// hostRgPath and pipeUtilsHandler); this Go implementation is the fallback so
// the command also works where ripgrep is absent (notably Windows). It covers a
// useful subset of ripgrep's interface. It deliberately does NOT reuse the grep
// tool's search stack: shellruntime is a lower layer than the tools package, so
// importing it would create a dependency cycle.

// hostRgPath returns the path to a real `rg` binary, or "" when none is
// installed. Cached for the process lifetime, mirroring searchcommon.GetRg.
var hostRgPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("rg")
	if err != nil {
		return ""
	}
	return p
})

// filename modes for the output prefix.
const (
	rgFilenameAuto = iota
	rgFilenameWith
	rgFilenameNo
)

type rgOptions struct {
	pattern    string
	paths      []string
	includes   []string // -g globs (without a leading '!')
	excludes   []string // -g '!glob'
	hidden     bool
	noIgnore   bool
	fixed      bool // -F
	word       bool // -w
	invert     bool // -v
	ignore     bool // -i
	sensitive  bool // -s
	smart      bool // -S
	lineNum    bool
	lineNumSet bool
	filename   int
	onlyMatch  bool // -o
	filesWith  bool // -l
	count      bool // -c
	quiet      bool // -q
	before     int  // -B
	after      int  // -A
	maxCount   int  // -m
}

// rgMatcher is a compiled pattern plus its inversion flag.
type rgMatcher struct {
	re     *regexp.Regexp
	invert bool
}

func (m rgMatcher) match(line string) bool {
	ok := m.re.MatchString(line)
	if m.invert {
		return !ok
	}
	return ok
}

func (m rgMatcher) findAll(line string) []string {
	if m.invert || !m.match(line) {
		return nil
	}
	return m.re.FindAllString(line, -1)
}

func runRg(env pipeEnv, args []string) error {
	const name = "rg"
	opts, err := parseRgArgs(args)
	if err != nil {
		return pipeErr(name, err.Error(), env.stderr)
	}
	if opts.pattern == "" {
		return pipeErr(name, "no pattern given", env.stderr)
	}

	matcher, err := rgCompile(opts)
	if err != nil {
		return pipeErr(name, fmt.Sprintf("invalid pattern %q: %v", opts.pattern, err), env.stderr)
	}

	paths := opts.paths
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var files []string
	anyDir := false
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(env.dir, p)
		}
		info, statErr := os.Stat(abs)
		if statErr != nil {
			fmt.Fprintf(env.stderr, "%s: %s: %v\n", name, p, statErr)
			continue
		}
		if info.IsDir() {
			anyDir = true
			walked, walkErr := rgWalk(abs, opts)
			if walkErr != nil {
				fmt.Fprintf(env.stderr, "%s: %v\n", name, walkErr)
				continue
			}
			files = append(files, walked...)
			continue
		}
		files = append(files, abs)
	}

	withFilename := len(files) > 1 || anyDir
	switch opts.filename {
	case rgFilenameWith:
		withFilename = true
	case rgFilenameNo:
		withFilename = false
	}
	// Like rg, line numbers accompany the filename prefix: shown when searching
	// a directory / multiple files, omitted for a single explicit file, unless
	// the caller forces it with -n/--line-number.
	if !opts.lineNumSet {
		opts.lineNum = withFilename
	}

	matched := false
	for _, f := range files {
		hasMatch, err := rgSearchFile(env, f, withFilename, matcher, opts)
		if err != nil {
			fmt.Fprintf(env.stderr, "%s: %s: %v\n", name, f, err)
			continue
		}
		matched = matched || hasMatch
	}

	if !matched {
		// rg exits 1 when there are no matches; other options do not change it.
		return interp.ExitStatus(1)
	}
	return nil
}

// rgListIgnored lists files under root using fsext's gitignore-aware lister,
// skipping hidden entries unless requested.
func rgListIgnored(root string, opts rgOptions) ([]string, error) {
	entries, _, err := fsext.ListDirectory(root, nil, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", root, err)
	}
	files := make([]string, 0, len(entries))
	for _, p := range entries {
		p = filepath.FromSlash(p)
		if strings.HasSuffix(p, string(filepath.Separator)) {
			continue // directory
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		if !opts.hidden && fdIsHidden(rel) {
			continue
		}
		if !rgGlobAllowed(rel, opts) {
			continue
		}
		files = append(files, p)
	}
	return files, nil
}

// rgWalk lists candidate files under root, delegating to the gitignore-aware
// lister unless --no-ignore is set.
func rgWalk(root string, opts rgOptions) ([]string, error) {
	if !opts.noIgnore {
		return rgListIgnored(root, opts)
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			//nolint:nilerr // unreadable entries are skipped, mirroring rg's tolerance
			return nil
		}
		if path == root {
			return nil
		}
		if !opts.hidden && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		if !rgGlobAllowed(rel, opts) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return files, nil
}

// rgGlobAllowed applies -g include/exclude globs to a path relative to the walk
// root. A glob with a separator matches the full relative path; otherwise it
// matches the base name.
func rgGlobAllowed(rel string, opts rgOptions) bool {
	base := filepath.Base(rel)
	toSlash := filepath.ToSlash(rel)
	match := func(glob string) bool {
		if strings.ContainsAny(glob, "/\\") {
			ok, _ := filepath.Match(filepath.FromSlash(glob), toSlash)
			return ok
		}
		ok, _ := filepath.Match(glob, base)
		return ok
	}
	for _, g := range opts.excludes {
		if match(g) {
			return false
		}
	}
	if len(opts.includes) == 0 {
		return true
	}
	for _, g := range opts.includes {
		if match(g) {
			return true
		}
	}
	return false
}

func rgSearchFile(env pipeEnv, path string, withFilename bool, matcher rgMatcher, opts rgOptions) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read: %w", err)
	}
	if rgLooksBinary(data) {
		return false, nil
	}

	display := rgDisplayPath(path, env.dir)
	lines := rgSplitLines(data)

	if opts.filesWith || opts.count || opts.quiet {
		n := 0
		for _, line := range lines {
			if !matcher.match(line) {
				continue
			}
			n++
			if opts.quiet {
				return true, nil
			}
			if opts.maxCount > 0 && n >= opts.maxCount {
				break
			}
		}
		if n == 0 {
			return false, nil
		}
		switch {
		case opts.filesWith:
			fmt.Fprintln(env.stdout, display)
		case opts.count:
			fmt.Fprintf(env.stdout, "%s:%d\n", display, n)
		}
		return true, nil
	}

	if opts.onlyMatch {
		n := 0
		for i, line := range lines {
			for _, m := range matcher.findAll(line) {
				rgPrint(env.stdout, display, i+1, m, withFilename, opts.lineNum, ":")
				n++
			}
			if opts.maxCount > 0 && n >= opts.maxCount {
				break
			}
		}
		return n > 0, nil
	}

	hit := make([]bool, len(lines))
	found, count := false, 0
	for i, line := range lines {
		if matcher.match(line) {
			hit[i] = true
			found = true
			count++
			if opts.maxCount > 0 && count >= opts.maxCount {
				break
			}
		}
	}
	if !found {
		return false, nil
	}
	rgEmitContext(env.stdout, display, lines, hit, withFilename, opts)
	return true, nil
}

// rgEmitContext prints matching lines with -A/-B/-C context using rg's
// convention: ':' joins match lines, '-' joins context lines, '--' separates
// non-contiguous groups.
func rgEmitContext(w io.Writer, display string, lines []string, hit []bool, withFilename bool, opts rgOptions) {
	lastPrinted := -1
	for i := range lines {
		if !hit[i] {
			continue
		}
		start := i - opts.before
		if start < 0 {
			start = 0
		}
		end := i + opts.after
		if end >= len(lines) {
			end = len(lines) - 1
		}
		if lastPrinted >= 0 {
			if start > lastPrinted+1 {
				fmt.Fprintln(w, "--")
			} else {
				start = lastPrinted + 1
			}
		}
		for j := start; j <= end; j++ {
			sep := "-"
			if hit[j] {
				sep = ":"
			}
			rgPrint(w, display, j+1, lines[j], withFilename, opts.lineNum, sep)
		}
		if end > lastPrinted {
			lastPrinted = end
		}
	}
}

func rgCompile(opts rgOptions) (rgMatcher, error) {
	pat := opts.pattern
	if opts.fixed {
		pat = regexp.QuoteMeta(pat)
	}
	if opts.word {
		pat = `\b(?:` + pat + `)\b`
	}
	ci := opts.ignore
	if !opts.ignore && !opts.sensitive && opts.smart {
		ci = !hasUpper(opts.pattern)
	}
	if ci {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return rgMatcher{}, fmt.Errorf("compile pattern: %w", err)
	}
	return rgMatcher{re: re, invert: opts.invert}, nil
}

func rgPrint(w io.Writer, display string, n int, content string, withFilename, lineNum bool, sep string) {
	var b strings.Builder
	if withFilename {
		b.WriteString(display)
		b.WriteString(sep)
	}
	if lineNum {
		fmt.Fprintf(&b, "%d%s", n, sep)
	}
	b.WriteString(content)
	fmt.Fprintln(w, b.String())
}

func rgLooksBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

func rgSplitLines(data []byte) []string {
	raw := splitLinesKeep(data)
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, strings.TrimSuffix(string(l), "\n"))
	}
	return out
}

func rgDisplayPath(path, dir string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return path
	}
	return rel
}

func parseRgArgs(args []string) (rgOptions, error) {
	opts := rgOptions{lineNum: true, filename: rgFilenameAuto}
	hasPattern := false
	var positionals []string

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positionals = append(positionals, args[i+1:]...)
			i = len(args)
		case a == "-" || !strings.HasPrefix(a, "-"):
			positionals = append(positionals, a)
		case a == "-i" || a == flagIgnoreCase:
			opts.ignore = true
		case a == "-s" || a == "--case-sensitive":
			opts.sensitive = true
		case a == "-S" || a == "--smart-case":
			opts.smart = true
		case a == "-F" || a == "--fixed-strings":
			opts.fixed = true
		case a == "-w" || a == "--word-regexp":
			opts.word = true
		case a == "-v" || a == "--invert-match":
			opts.invert = true
		case a == "-n" || a == "--line-number":
			opts.lineNum = true
			opts.lineNumSet = true
		case a == "-N" || a == "--no-line-number":
			opts.lineNum = false
			opts.lineNumSet = true
		case a == "-H" || a == "--with-filename":
			opts.filename = rgFilenameWith
		case a == "--no-filename":
			opts.filename = rgFilenameNo
		case a == "-l" || a == "--files-with-matches":
			opts.filesWith = true
		case a == "-c" || a == "--count":
			opts.count = true
		case a == "-o" || a == "--only-matching":
			opts.onlyMatch = true
		case a == "-q" || a == flagQuiet:
			opts.quiet = true
		case a == "--hidden":
			opts.hidden = true
		case a == "--no-ignore":
			opts.noIgnore = true
		case a == "-e" || a == "--regexp":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			opts.pattern = v
			hasPattern = true
		case strings.HasPrefix(a, "--regexp="):
			opts.pattern = strings.TrimPrefix(a, "--regexp=")
			hasPattern = true
		case a == "-g" || a == "--glob":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			addRgGlob(&opts, v)
		case strings.HasPrefix(a, "--glob="):
			addRgGlob(&opts, strings.TrimPrefix(a, "--glob="))
		case a == "-m" || a == "--max-count":
			v, err := fdNextValue(args, &i, a)
			if err != nil {
				return opts, err
			}
			n, err := fdUint(v, a)
			if err != nil {
				return opts, err
			}
			opts.maxCount = n
		case strings.HasPrefix(a, "--max-count="):
			n, err := fdUint(strings.TrimPrefix(a, "--max-count="), a)
			if err != nil {
				return opts, err
			}
			opts.maxCount = n
		case a == "-A" || a == "--after-context":
			n, err := rgNextUint(args, &i, a)
			if err != nil {
				return opts, err
			}
			opts.after = n
		case a == "-B" || a == "--before-context":
			n, err := rgNextUint(args, &i, a)
			if err != nil {
				return opts, err
			}
			opts.before = n
		case a == "-C" || a == "--context":
			n, err := rgNextUint(args, &i, a)
			if err != nil {
				return opts, err
			}
			opts.before, opts.after = n, n
		default:
			handled, err := parseRgCluster(&opts, a)
			if err != nil {
				return opts, err
			}
			if !handled {
				return opts, fmt.Errorf("unrecognized option %q", a)
			}
		}
	}

	if !hasPattern {
		if len(positionals) == 0 {
			return opts, fmt.Errorf("no pattern given")
		}
		opts.pattern = positionals[0]
		positionals = positionals[1:]
	}
	opts.paths = positionals
	return opts, nil
}

func rgNextUint(args []string, i *int, flag string) (int, error) {
	v, err := fdNextValue(args, i, flag)
	if err != nil {
		return 0, err
	}
	return fdUint(v, flag)
}

// parseRgCluster handles compact short flags such as -in or -A2, or -g*.go.
func parseRgCluster(opts *rgOptions, a string) (bool, error) {
	if len(a) < 2 || a[0] != '-' || strings.HasPrefix(a, "--") {
		return false, nil
	}
	rest := a[1:]
	for len(rest) > 0 {
		c := rest[0]
		rest = rest[1:]
		switch c {
		case 'i':
			opts.ignore = true
		case 's':
			opts.sensitive = true
		case 'F':
			opts.fixed = true
		case 'w':
			opts.word = true
		case 'v':
			opts.invert = true
		case 'n':
			opts.lineNum = true
			opts.lineNumSet = true
		case 'N':
			opts.lineNum = false
			opts.lineNumSet = true
		case 'H':
			opts.filename = rgFilenameWith
		case 'l':
			opts.filesWith = true
		case 'c':
			opts.count = true
		case 'o':
			opts.onlyMatch = true
		case 'q':
			opts.quiet = true
		case 'e', 'g', 'm', 'A', 'B', 'C':
			if rest == "" {
				return true, fmt.Errorf("option -%c requires an argument", c)
			}
			applyRgValue(opts, c, rest)
			return true, nil
		default:
			return false, nil
		}
	}
	return true, nil
}

func applyRgValue(opts *rgOptions, c byte, v string) {
	switch c {
	case 'e':
		opts.pattern = v
	case 'g':
		addRgGlob(opts, v)
	case 'm':
		if n, err := fdUint(v, "-m"); err == nil {
			opts.maxCount = n
		}
	case 'A':
		if n, err := fdUint(v, "-A"); err == nil {
			opts.after = n
		}
	case 'B':
		if n, err := fdUint(v, "-B"); err == nil {
			opts.before = n
		}
	case 'C':
		if n, err := fdUint(v, "-C"); err == nil {
			opts.before, opts.after = n, n
		}
	}
}

func addRgGlob(opts *rgOptions, g string) {
	if strings.HasPrefix(g, "!") {
		opts.excludes = append(opts.excludes, strings.TrimPrefix(g, "!"))
		return
	}
	opts.includes = append(opts.includes, g)
}
