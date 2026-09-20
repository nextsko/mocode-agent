// Package browser is mocode's native browser runtime: it discovers a
// Chromium-family browser already installed on the machine and drives it over
// the Chrome DevTools Protocol, with no Node/Playwright/npm dependency.
//
// The design mirrors the repository's existing "probe first" pattern used for
// rg/fd/jq: use what the host already has, and degrade with a diagnosable error
// rather than bundling a copy. See docs/architecture/07-native-browser-runtime.md.
package browser

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind identifies a supported browser product. Only Chromium-family browsers
// are listed: Firefox and WebKit speak different protocols and are explicitly
// out of scope (see the design doc §7).
type Kind string

// Supported browser kinds.
const (
	KindChrome   Kind = "chrome"
	KindChromium Kind = "chromium"
	KindEdge     Kind = "msedge"
	KindBrave    Kind = "brave"
	KindVivaldi  Kind = "vivaldi"
	KindOpera    Kind = "opera"
)

// Source records how a candidate was found, so that status output and error
// messages can explain *why* a browser was or was not usable instead of just
// reporting a bare path.
type Source string

// Detection sources.
const (
	SourceEnv       Source = "env"        // MOCODE_BROWSER_PATH override
	SourcePath      Source = "path"       // resolved through $PATH
	SourceWellKnown Source = "well-known" // platform install directory
	SourceSnap      Source = "snap"       // Linux snap
	SourceFlatpak   Source = "flatpak"    // Linux flatpak
)

// Verification records how far a candidate was validated, so output can
// distinguish "ran and answered" from "we deliberately did not run it".
type Verification string

const (
	// VerNotRun means validation was skipped. This is the normal outcome on
	// Windows: running `chrome.exe --version` there *launches a browser window*
	// instead of printing a version, so probing would have side effects. The
	// version is read from the install layout instead.
	VerNotRun Verification = "not-run"
	// VerOK means the candidate ran and reported itself.
	VerOK Verification = "ok"
	// VerFailed means the candidate was probed but did not answer.
	VerFailed Verification = "failed"
)

// EnvBrowserPath overrides detection with an explicit executable.
const EnvBrowserPath = "MOCODE_BROWSER_PATH"

// EnvCDPEndpoint skips launching entirely and attaches to an existing endpoint
// (for example a Chrome the user started with --remote-debugging-port).
const EnvCDPEndpoint = "MOCODE_BROWSER_CDP"

// Candidate is one discoverable Chromium-family browser installation.
type Candidate struct {
	Kind Kind
	// Path is the executable to run. For flatpak installs this is the flatpak
	// launcher, not the sandboxed browser binary.
	Path string
	// PrefixArgs must be passed before any browser flags (flatpak: "run <app>").
	PrefixArgs []string
	Source     Source
	// Version is best effort; empty when it could not be determined.
	Version string
	// Verification records whether the candidate was run and what happened.
	Verification Verification
	// NeedsSnapProfile is set for snap-confined browsers, which cannot read a
	// profile directory outside their own $SNAP_USER_DATA.
	NeedsSnapProfile bool
}

// String renders a candidate the way status output shows it.
func (c Candidate) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)", c.Kind, c.Source)
	if c.Version != "" {
		fmt.Fprintf(&b, " %s", c.Version)
	}
	fmt.Fprintf(&b, "\n    %s", c.Path)
	if len(c.PrefixArgs) > 0 {
		fmt.Fprintf(&b, " %s", strings.Join(c.PrefixArgs, " "))
	}
	switch c.Verification {
	case VerFailed:
		b.WriteString("\n    (version probe failed — the file exists but did not run)")
	case VerNotRun:
		b.WriteString("\n    (not probed: this platform's browser launches instead of printing a version)")
	}
	if c.NeedsSnapProfile {
		b.WriteString("\n    (snap confined: profile lives under ~/snap)")
	}
	return b.String()
}

// DetectOptions carries the platform surface detection touches. Every field is
// optional and defaults to the real system; tests supply fakes so that all
// three platforms' lookup tables can be exercised on any host.
type DetectOptions struct {
	// GOOS overrides runtime.GOOS.
	GOOS string
	// Getenv overrides os.Getenv.
	Getenv func(string) string
	// Stat overrides os.Stat.
	Stat func(string) (os.FileInfo, error)
	// ReadDir overrides os.ReadDir. Used on Windows to read the version from
	// the install layout without running anything.
	ReadDir func(string) ([]os.DirEntry, error)
	// LookPath overrides exec.LookPath.
	LookPath func(string) (string, error)
	// Probe reports the version string of a candidate, and whether it ran.
	// A nil Probe disables probing entirely.
	Probe func(ctx context.Context, bin string, prefix []string) (version string, ok bool)
	// ProbeTimeout bounds each version probe. Defaults to 5s.
	ProbeTimeout time.Duration
}

// Detect returns every Chromium-family browser it can find, best candidate
// first. It never returns an error: an empty slice means "nothing usable", and
// [DetectOrError] explains what was tried.
func Detect(ctx context.Context, opts DetectOptions) []Candidate {
	e := normalize(opts)

	var found []Candidate
	seen := map[string]bool{}

	add := func(c Candidate) {
		// Deduplicate on the resolved executable + prefix: the same install is
		// often reachable from both $PATH and a well-known directory.
		key := c.Path + "\x00" + strings.Join(c.PrefixArgs, "\x00")
		if seen[key] {
			return
		}
		seen[key] = true
		found = append(found, c)
	}

	// 1. Explicit override wins outright.
	if p := strings.TrimSpace(e.getenv(EnvBrowserPath)); p != "" {
		c := Candidate{Kind: kindFromPath(p), Path: p, Source: SourceEnv}
		if !e.validate(ctx, &c) {
			// A broken override is reported by DetectOrError, which names the
			// setting and the path, rather than surfacing a phantom candidate.
			return []Candidate{}
		}
		add(c)
		return sortCandidates(found)
	}

	// 2. $PATH.
	for _, name := range pathNamesFor(e.goos) {
		resolved, err := e.lookPath(name)
		if err != nil || resolved == "" {
			continue
		}
		c := Candidate{Kind: kindFromPath(resolved), Path: resolved, Source: SourcePath}
		if !e.validate(ctx, &c) {
			continue
		}
		add(c)
	}

	// 3. Platform install directories.
	for _, s := range wellKnownFor(e.goos, e.getenv) {
		if _, err := e.stat(s.path); err != nil {
			continue
		}
		c := Candidate{
			Kind:             s.kind,
			Path:             s.path,
			PrefixArgs:       s.prefix,
			Source:           s.source,
			NeedsSnapProfile: s.needsSnapProfile,
		}
		if !e.validate(ctx, &c) {
			continue
		}
		add(c)
	}

	// 4. flatpak: the browser binary lives inside a sandbox, so we can only
	// launch it through the flatpak launcher.
	if e.goos == "linux" {
		if flatpak, err := e.lookPath("flatpak"); err == nil && flatpak != "" {
			for _, app := range flatpakApps {
				c := Candidate{
					Kind:       app.kind,
					Path:       flatpak,
					PrefixArgs: []string{"run", app.id},
					Source:     SourceFlatpak,
				}
				if !e.validate(ctx, &c) || c.Verification != VerOK {
					// Unlike a file on disk, a flatpak "candidate" is only real
					// if it actually runs; a missing app must not be reported.
					continue
				}
				add(c)
			}
		}
	}

	return sortCandidates(found)
}

// DetectOrError is [Detect] plus an actionable error when nothing was found.
// The error lists everything that was tried, so a user on an unusual
// distribution can see exactly which paths were consulted.
func DetectOrError(ctx context.Context, opts DetectOptions) ([]Candidate, error) {
	found := Detect(ctx, opts)
	if len(found) > 0 {
		return found, nil
	}

	e := normalize(opts)
	var b strings.Builder
	b.WriteString("no Chromium-family browser found")
	if p := strings.TrimSpace(e.getenv(EnvBrowserPath)); p != "" {
		fmt.Fprintf(&b, "\n  %s was set to %q but that path did not exist or did not run", EnvBrowserPath, p)
	}
	fmt.Fprintf(&b, "\n  looked on $PATH for: %s", strings.Join(pathNamesFor(e.goos), ", "))
	for _, s := range wellKnownFor(e.goos, e.getenv) {
		fmt.Fprintf(&b, "\n  checked: %s", s.path)
	}
	if e.goos == "linux" {
		b.WriteString("\n  flatpak apps checked: ")
		for i, app := range flatpakApps {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(app.id)
		}
	}
	fmt.Fprintf(&b, "\n  hint: install Google Chrome or Chromium, or point %s at an existing binary", EnvBrowserPath)
	b.WriteString("\n  note: Firefox and WebKit are not supported (they do not speak CDP)")
	return nil, fmt.Errorf("%s", b.String())
}

// ─── platform tables ─────────────────────────────────────────────────────────

type installSpec struct {
	kind             Kind
	path             string
	source           Source
	prefix           []string
	needsSnapProfile bool
}

type flatpakApp struct {
	kind Kind
	id   string
}

// flatpakApps are the Chromium-family apps we look for in a flatpak sandbox.
var flatpakApps = []flatpakApp{
	{KindChrome, "com.google.Chrome"},
	{KindChromium, "org.chromium.Chromium"},
	{KindEdge, "com.microsoft.Edge"},
	{KindBrave, "com.brave.Browser"},
	{KindVivaldi, "com.vivaldi.Vivaldi"},
}

// pathNamesFor lists the executables to look for on $PATH, most preferred
// first. Kept as data rather than build-tagged files so that every platform's
// table stays testable from any host.
func pathNamesFor(goos string) []string {
	if goos == "windows" {
		return []string{"chrome.exe", "chromium.exe", "msedge.exe", "brave.exe", "vivaldi.exe"}
	}
	return []string{
		"google-chrome-stable", "google-chrome",
		"chromium", "chromium-browser",
		"microsoft-edge-stable", "microsoft-edge",
		"brave-browser", "brave",
		"vivaldi-stable", "vivaldi",
		"google-chrome-beta",
	}
}

// wellKnownFor returns the platform's conventional install locations.
func wellKnownFor(goos string, getenv func(string) string) []installSpec {
	switch goos {
	case "windows":
		var specs []installSpec
		for _, root := range []string{getenv("ProgramFiles"), getenv("ProgramFiles(x86)"), getenv("LOCALAPPDATA")} {
			if strings.TrimSpace(root) == "" {
				continue
			}
			for _, rel := range []string{
				`Google\Chrome\Application\chrome.exe`,
				`Microsoft\Edge\Application\msedge.exe`,
				`BraveSoftware\Brave-Browser\Application\brave.exe`,
				`Vivaldi\Application\vivaldi.exe`,
				`Chromium\Application\chrome.exe`,
			} {
				specs = append(specs, installSpec{
					kind:   kindFromPath(rel),
					path:   joinFor(goos, root, rel),
					source: SourceWellKnown,
				})
			}
		}
		return specs

	case "darwin":
		var specs []installSpec
		for _, root := range []string{"/Applications", joinFor(goos, homeDir(getenv), "Applications")} {
			for _, app := range []struct {
				bundle string
				bin    string
			}{
				{"Google Chrome.app", "Google Chrome"},
				{"Chromium.app", "Chromium"},
				{"Microsoft Edge.app", "Microsoft Edge"},
				{"Brave Browser.app", "Brave Browser"},
				{"Vivaldi.app", "Vivaldi"},
				{"Opera.app", "Opera"},
			} {
				specs = append(specs, installSpec{
					kind:   kindFromBundle(app.bundle),
					path:   joinFor(goos, root, app.bundle, "Contents", "MacOS", app.bin),
					source: SourceWellKnown,
				})
			}
		}
		return specs

	default: // linux and anything else POSIX
		return []installSpec{
			{kind: KindChrome, path: "/usr/bin/google-chrome-stable", source: SourceWellKnown},
			{kind: KindChrome, path: "/usr/bin/google-chrome", source: SourceWellKnown},
			{kind: KindChrome, path: "/opt/google/chrome/chrome", source: SourceWellKnown},
			{kind: KindChromium, path: "/usr/bin/chromium", source: SourceWellKnown},
			{kind: KindChromium, path: "/usr/bin/chromium-browser", source: SourceWellKnown},
			{kind: KindChromium, path: "/usr/lib/chromium/chromium", source: SourceWellKnown},
			{kind: KindEdge, path: "/usr/bin/microsoft-edge-stable", source: SourceWellKnown},
			{kind: KindEdge, path: "/usr/bin/microsoft-edge", source: SourceWellKnown},
			{kind: KindBrave, path: "/usr/bin/brave-browser", source: SourceWellKnown},
			{kind: KindBrave, path: "/opt/brave.com/brave/brave-browser", source: SourceWellKnown},
			{kind: KindVivaldi, path: "/usr/bin/vivaldi-stable", source: SourceWellKnown},
			// snap: confined, so the profile must live in $SNAP_USER_DATA.
			{kind: KindChromium, path: "/snap/bin/chromium", source: SourceSnap, needsSnapProfile: true},
			{kind: KindEdge, path: "/snap/bin/microsoft-edge", source: SourceSnap, needsSnapProfile: true},
			{kind: KindBrave, path: "/snap/bin/brave", source: SourceSnap, needsSnapProfile: true},
		}
	}
}

// ─── env + validation ────────────────────────────────────────────────────────

func normalize(opts DetectOptions) *detectEnv {
	e := &detectEnv{
		goos:     opts.GOOS,
		getenv:   opts.Getenv,
		stat:     opts.Stat,
		readDir:  opts.ReadDir,
		lookPath: opts.LookPath,
		probe:    opts.Probe,
		timeout:  opts.ProbeTimeout,
	}
	if e.goos == "" {
		e.goos = runtime.GOOS
	}
	if e.getenv == nil {
		e.getenv = os.Getenv
	}
	if e.stat == nil {
		e.stat = os.Stat
	}
	if e.readDir == nil {
		e.readDir = os.ReadDir
	}
	if e.lookPath == nil {
		e.lookPath = exec.LookPath
	}
	if e.timeout <= 0 {
		e.timeout = 5 * time.Second
	}
	if e.probe == nil {
		timeout := e.timeout
		e.probe = func(ctx context.Context, bin string, prefix []string) (string, bool) {
			return probeVersion(ctx, timeout, bin, prefix)
		}
	}
	return e
}

type detectEnv struct {
	goos     string
	getenv   func(string) string
	stat     func(string) (os.FileInfo, error)
	readDir  func(string) ([]os.DirEntry, error)
	lookPath func(string) (string, error)
	probe    func(ctx context.Context, bin string, prefix []string) (string, bool)
	timeout  time.Duration
}

// validate confirms a candidate is a real executable and records how far it
// could be verified. It reports whether the candidate should be kept: a path
// that does not exist is not a browser, and returning it would let a typo in
// MOCODE_BROWSER_PATH masquerade as a working install.
func (e *detectEnv) validate(ctx context.Context, c *Candidate) bool {
	// flatpak is exempt from the stat check: its "binary" is the launcher and
	// the real app lives inside a sandbox, so only running it proves anything.
	if c.Source != SourceFlatpak {
		info, err := e.stat(c.Path)
		if err != nil || info.IsDir() {
			return false
		}
	}

	if e.goos == "windows" && c.Source != SourceFlatpak {
		// Running a Windows browser with --version opens a window rather than
		// printing anything, so probing would have side effects. Chrome, Edge,
		// Brave and friends keep the version in a sibling directory name, which
		// is free to read.
		c.Version = e.versionFromInstallDir(c.Path)
		c.Verification = VerNotRun
		return true
	}

	v, ok := e.probe(ctx, c.Path, c.PrefixArgs)
	c.Version = v
	if ok {
		c.Verification = VerOK
	} else {
		c.Verification = VerFailed
	}
	return true
}

// versionDirRe matches the version-named install directories Windows browsers
// leave beside their executable (e.g. "...\Application\152.0.7977.83\").
var versionDirRe = regexp.MustCompile(`^(\d{1,4}(?:\.\d{1,4}){1,3})$`)

// versionFromInstallDir reads a Windows browser's version from the layout of
// its install directory, spawning nothing.
func (e *detectEnv) versionFromInstallDir(exePath string) string {
	dir := parentDir(exePath)
	if dir == "" {
		return ""
	}
	entries, err := e.readDir(dir)
	if err != nil {
		return ""
	}

	best := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		m := versionDirRe.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		// Several versions can be present during an upgrade; take the highest.
		if compareVersions(versionParts(m[1]), versionParts(best)) > 0 {
			best = m[1]
		}
	}
	return best
}

func versionParts(v string) []int {
	if v == "" {
		return nil
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// compareVersions returns <0, 0 or >0.
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var ai, bi int
		if i < len(a) {
			ai = a[i]
		}
		if i < len(b) {
			bi = b[i]
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}

// parentDir returns the directory containing path, or "" when there is none.
// It understands both separators so the windows tables work from any host.
func parentDir(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i > 0 {
		return p[:i]
	}
	return ""
}

// versionRe extracts a browser version. It is bounded to 4 components because
// an unbounded pattern happily matches timestamps out of log noise
// ("0914/102041.503" once parsed as version 102041.503), and it must follow
// whitespace so a version embedded in a path cannot match.
var versionRe = regexp.MustCompile(`(?:^|\s)(\d{1,4}(?:\.\d{1,4}){1,3})`)

func extractVersion(out string) string {
	// Only the first non-empty line is considered: `--version` output puts the
	// version there, and later lines are diagnostics that can contain numbers.
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := versionRe.FindStringSubmatch(line); m != nil {
			return m[1]
		}
		return ""
	}
	return ""
}

// probeVersion runs the candidate with --version and reports what it printed.
// A timeout is essential: a broken wrapper on $PATH would otherwise hang
// detection indefinitely.
//
// Callers must not use this on Windows — see detectEnv.validate.
func probeVersion(ctx context.Context, timeout time.Duration, bin string, prefix []string) (string, bool) {
	args := append(append([]string{}, prefix...), "--version")
	out, err := runWithTimeout(ctx, timeout, bin, args...)
	if err != nil {
		// Keep whatever was printed before the timeout: some builds report a
		// version and then fail to exit.
		if v := extractVersion(out); v != "" {
			return v, true
		}
		return "", false
	}
	if v := extractVersion(out); v != "" {
		return v, true
	}
	return "", strings.TrimSpace(out) != ""
}

func runWithTimeout(parent context.Context, d time.Duration, bin string, args ...string) (string, error) {
	if d > 0 {
		var cancel context.CancelFunc
		parent, cancel = context.WithTimeout(parent, d)
		defer cancel()
	}
	cmd := exec.CommandContext(parent, bin, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func kindFromPath(p string) Kind {
	// Match against the whole path, not just the file name: a Chromium install
	// ships its executable as chrome.exe, so the product is only identifiable
	// from the directory it lives in.
	l := strings.ToLower(p)
	switch {
	case strings.Contains(l, "edge"):
		return KindEdge
	case strings.Contains(l, "brave"):
		return KindBrave
	case strings.Contains(l, "vivaldi"):
		return KindVivaldi
	case strings.Contains(l, "opera"):
		return KindOpera
	case strings.Contains(l, "chromium"):
		return KindChromium
	default:
		return KindChrome
	}
}

func kindFromBundle(bundle string) Kind {
	return kindFromPath(strings.TrimSuffix(bundle, ".app"))
}

// joinFor builds a path for the target OS rather than the host OS. The install
// tables are data for a specific platform, and they must stay correct (and
// testable) even when a test exercises them from a different one — which is why
// this cannot be filepath.Join.
func joinFor(goos string, parts ...string) string {
	sep := "/"
	if goos == "windows" {
		sep = "\\"
	}
	return strings.Join(parts, sep)
}

func homeDir(getenv func(string) string) string {
	if h := getenv("HOME"); h != "" {
		return h
	}
	return getenv("USERPROFILE")
}

// kindRank orders products by how good a debugging target they are: upstream
// Chrome first, then Chromium, then derivatives.
func kindRank(k Kind) int {
	switch k {
	case KindChrome:
		return 0
	case KindChromium:
		return 1
	case KindEdge:
		return 2
	case KindBrave:
		return 3
	case KindVivaldi:
		return 4
	default:
		return 5
	}
}

// sourceRank prefers an explicit override, then $PATH, then install dirs.
func sourceRank(s Source) int {
	switch s {
	case SourceEnv:
		return 0
	case SourcePath:
		return 1
	case SourceWellKnown:
		return 2
	case SourceSnap:
		return 3
	default:
		return 4
	}
}

// verificationRank puts confirmed-working browsers first, then ones we could
// not check on this platform, then ones that failed.
func verificationRank(v Verification) int {
	switch v {
	case VerOK:
		return 0
	case VerNotRun:
		return 1
	default:
		return 2
	}
}

func sortCandidates(in []Candidate) []Candidate {
	if in == nil {
		return []Candidate{}
	}
	sort.SliceStable(in, func(i, j int) bool {
		if ri, rj := verificationRank(in[i].Verification), verificationRank(in[j].Verification); ri != rj {
			return ri < rj
		}
		if ri, rj := kindRank(in[i].Kind), kindRank(in[j].Kind); ri != rj {
			return ri < rj
		}
		if si, sj := sourceRank(in[i].Source), sourceRank(in[j].Source); si != sj {
			return si < sj
		}
		return in[i].Path < in[j].Path
	})
	return in
}

// Report renders a human-readable detection summary for the status action.
func Report(ctx context.Context, opts DetectOptions) string {
	e := normalize(opts)
	candidates := Detect(ctx, opts)

	var b strings.Builder
	fmt.Fprintf(&b, "Platform: %s\n", e.goos)
	if p := strings.TrimSpace(e.getenv(EnvBrowserPath)); p != "" {
		fmt.Fprintf(&b, "%s=%s (overrides detection)\n", EnvBrowserPath, p)
	}
	if ep := strings.TrimSpace(e.getenv(EnvCDPEndpoint)); ep != "" {
		fmt.Fprintf(&b, "%s=%s (attach mode; no browser is launched)\n", EnvCDPEndpoint, ep)
	}

	if len(candidates) == 0 {
		if _, err := DetectOrError(ctx, opts); err != nil {
			fmt.Fprintf(&b, "\nNo usable browser found.\n\n%s\n", err)
		}
		return b.String()
	}

	fmt.Fprintf(&b, "\nFound %d browser(s), best first:\n", len(candidates))
	for i, c := range candidates {
		fmt.Fprintf(&b, "\n  %d. %s\n", i+1, c)
	}
	return b.String()
}
