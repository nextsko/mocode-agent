package browser

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHost stands in for the OS so every platform's lookup table can be
// exercised on any machine. Without this, the Linux-only branches (snap,
// flatpak) could only ever be tested on Linux — which is exactly where they are
// hardest to test.
type fakeHost struct {
	goos     string
	env      map[string]string
	files    map[string]bool     // paths that exist
	dirs     map[string]bool     // paths that exist and are directories
	readDir  map[string][]string // dir path -> entry names
	onPath   map[string]string   // executable name -> resolved path
	versions map[string]string   // "bin prefix... --version" -> raw output
	dead     map[string]bool     // commands whose --version fails
	probes   *[]string           // records every probed argv, to prove no spawn happened
}

func (h *fakeHost) opts() DetectOptions {
	return DetectOptions{
		GOOS: h.goos,
		Getenv: func(k string) string {
			return h.env[k]
		},
		Stat: func(p string) (os.FileInfo, error) {
			if h.dirs[p] {
				return fakeFileInfo{name: filepath.Base(p), dir: true}, nil
			}
			if h.files[p] {
				return fakeFileInfo{name: filepath.Base(p)}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadDir: func(p string) ([]os.DirEntry, error) {
			names, ok := h.readDir[p]
			if !ok {
				return nil, os.ErrNotExist
			}
			out := make([]os.DirEntry, 0, len(names))
			for _, n := range names {
				out = append(out, fakeDirEntry{name: n, dir: true})
			}
			return out, nil
		},
		LookPath: func(name string) (string, error) {
			if p, ok := h.onPath[name]; ok {
				return p, nil
			}
			return "", os.ErrNotExist
		},
		Probe: func(_ context.Context, bin string, prefix []string) (string, bool) {
			// Mirror probeVersion's argv exactly, so test keys read like real
			// commands and cannot silently drift from the production probing.
			argv := append(append([]string{bin}, prefix...), "--version")
			if h.probes != nil {
				*h.probes = append(*h.probes, strings.Join(argv, " "))
			}
			key := strings.Join(argv, " ")
			if h.dead[key] {
				return "", false
			}
			if raw, ok := h.versions[key]; ok {
				// Apply the same extraction production does, so tests assert on
				// the version the runtime would actually hold.
				return extractVersion(raw), true
			}
			return "", false
		},
		ProbeTimeout: time.Second,
	}
}

type fakeFileInfo struct {
	name string
	dir  bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string      { return f.name }
func (f fakeDirEntry) IsDir() bool       { return f.dir }
func (f fakeDirEntry) Type() os.FileMode { return 0o755 }

func (f fakeDirEntry) Info() (os.FileInfo, error) {
	return fakeFileInfo{name: f.name, dir: f.dir}, nil
}

func linuxHost() *fakeHost {
	return &fakeHost{
		goos:     "linux",
		env:      map[string]string{"HOME": "/home/dev"},
		files:    map[string]bool{},
		dirs:     map[string]bool{},
		readDir:  map[string][]string{},
		onPath:   map[string]string{},
		versions: map[string]string{},
		dead:     map[string]bool{},
	}
}

// windowsHost mirrors a real Windows layout: the executable sits next to a
// directory named after the version.
func windowsHost() *fakeHost {
	return &fakeHost{
		goos: "windows",
		env: map[string]string{
			"ProgramFiles":      `C:\Program Files`,
			"ProgramFiles(x86)": `C:\Program Files (x86)`,
			"LOCALAPPDATA":      `C:\Users\dev\AppData\Local`,
			"USERPROFILE":       `C:\Users\dev`,
		},
		files:   map[string]bool{},
		dirs:    map[string]bool{},
		readDir: map[string][]string{},
		onPath:  map[string]string{},
		dead:    map[string]bool{},
	}
}

// ─── linux ───────────────────────────────────────────────────────────────────

// TestDetect_Linux_PathAndWellKnown covers the ordinary Linux install: distro
// packages on $PATH plus a vendor install under /opt.
func TestDetect_Linux_PathAndWellKnown(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.onPath["chromium"] = "/usr/bin/chromium"
	h.onPath["google-chrome-stable"] = "/usr/bin/google-chrome-stable"
	h.files["/usr/bin/chromium"] = true
	h.files["/usr/bin/google-chrome-stable"] = true
	h.versions["/usr/bin/chromium --version"] = "Chromium 141.0.7390.0"
	h.versions["/usr/bin/google-chrome-stable --version"] = "Google Chrome 141.0.7390.55"

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 2)

	// Chrome outranks Chromium even though Chromium is reachable too.
	assert.Equal(t, KindChrome, got[0].Kind)
	assert.Equal(t, "141.0.7390.55", got[0].Version)
	assert.Equal(t, VerOK, got[0].Verification)
	assert.Equal(t, SourcePath, got[0].Source)

	assert.Equal(t, KindChromium, got[1].Kind)
}

// TestDetect_Linux_SnapMarksProfileConstraint is the regression test for the
// snap confinement trap: a snap Chromium cannot read a profile directory
// outside its own $SNAP_USER_DATA, so the candidate must say so.
func TestDetect_Linux_SnapMarksProfileConstraint(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.files["/snap/bin/chromium"] = true
	h.versions["/snap/bin/chromium --version"] = "Chromium 141.0.7390.0"

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 1)
	assert.Equal(t, SourceSnap, got[0].Source)
	assert.True(t, got[0].NeedsSnapProfile, "snap installs must be flagged so the profile lands under ~/snap")
	assert.Equal(t, VerOK, got[0].Verification)
}

// TestDetect_Linux_FlatpakUsesLauncher covers the other Linux sandbox: the real
// binary is unreachable, so we must go through `flatpak run`.
func TestDetect_Linux_FlatpakUsesLauncher(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.onPath["flatpak"] = "/usr/bin/flatpak"
	h.versions["/usr/bin/flatpak run com.google.Chrome --version"] = "Google Chrome 141.0.7390.55"

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 1)

	assert.Equal(t, SourceFlatpak, got[0].Source)
	assert.Equal(t, "/usr/bin/flatpak", got[0].Path, "flatpak installs launch through the flatpak binary")
	assert.Equal(t, []string{"run", "com.google.Chrome"}, got[0].PrefixArgs)
	assert.Equal(t, VerOK, got[0].Verification)
}

// A flatpak app that is not installed must not be reported at all: unlike a
// file on disk there is nothing to stat, so only a working run proves it exists.
func TestDetect_Linux_FlatpakSkipsAbsentApps(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.onPath["flatpak"] = "/usr/bin/flatpak"

	assert.Empty(t, Detect(context.Background(), h.opts()),
		"an uninstallable flatpak app must not appear as a candidate")
}

// ─── darwin ──────────────────────────────────────────────────────────────────

func TestDetect_Darwin_AppBundles(t *testing.T) {
	t.Parallel()

	h := &fakeHost{
		goos: "darwin",
		env:  map[string]string{"HOME": "/Users/dev"},
		files: map[string]bool{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome": true,
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser": true,
		},
		dirs:    map[string]bool{},
		readDir: map[string][]string{},
		onPath:  map[string]string{},
		versions: map[string]string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --version": "Google Chrome 141.0.7390.55",
		},
		dead: map[string]bool{},
	}

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 2)

	assert.Equal(t, KindChrome, got[0].Kind)
	assert.Equal(t, VerOK, got[0].Verification)

	// Brave's probe was not stubbed, so it stays a candidate but unverified —
	// the "file exists but did not answer" case — and sorts after the verified one.
	assert.Equal(t, KindBrave, got[1].Kind)
	assert.Equal(t, VerFailed, got[1].Verification)
	assert.Contains(t, got[1].String(), "version probe failed")
}

// ─── windows ─────────────────────────────────────────────────────────────────

// TestDetect_Windows_NeverRunsTheBrowser is the most important test in this
// file. On Windows `chrome.exe --version` does not print a version: it *launches
// a browser window* and then hangs. Probing there would pop up a browser as a
// side effect of merely listing what is installed, so detection must read the
// version out of the install layout instead and spawn nothing.
func TestDetect_Windows_NeverRunsTheBrowser(t *testing.T) {
	t.Parallel()

	var probes []string
	h := windowsHost()
	h.probes = &probes
	h.files[`C:\Program Files\Google\Chrome\Application\chrome.exe`] = true
	h.readDir[`C:\Program Files\Google\Chrome\Application`] = []string{"152.0.7977.83", "chrome.exe"}

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 1)

	assert.Empty(t, probes, "detection must never execute a Windows browser")
	assert.Equal(t, "152.0.7977.83", got[0].Version, "the version comes from the sibling directory name")
	assert.Equal(t, VerNotRun, got[0].Verification)
	assert.Contains(t, got[0].String(), "not probed")
}

// During an upgrade more than one version directory can be present.
func TestDetect_Windows_TakesHighestVersionDirectory(t *testing.T) {
	t.Parallel()

	h := windowsHost()
	h.files[`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`] = true
	h.readDir[`C:\Program Files (x86)\Microsoft\Edge\Application`] = []string{
		"151.0.1.1", "153.0.4234.32", "152.0.4191.66", "SetupMetrics", "153.0.9.9",
	}

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 1)
	assert.Equal(t, "153.0.4234.32", got[0].Version,
		"a numeric comparison must beat a lexicographic one (153.0.9.9 < 153.0.4234.32)")
}

func TestDetect_Windows_ProgramFiles(t *testing.T) {
	t.Parallel()

	h := windowsHost()
	h.files[`C:\Program Files\Google\Chrome\Application\chrome.exe`] = true
	h.files[`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`] = true
	h.files[`C:\Users\dev\AppData\Local\Chromium\Application\chrome.exe`] = true
	h.onPath["chrome.exe"] = `C:\Program Files\Google\Chrome\Application\chrome.exe`

	got := Detect(context.Background(), h.opts())

	kinds := map[Kind]int{}
	chromePaths := 0
	for _, c := range got {
		kinds[c.Kind]++
		if strings.Contains(c.Path, `Google\Chrome`) {
			chromePaths++
		}
	}

	assert.Equal(t, 1, kinds[KindChrome], "the ProgramFiles Chrome")
	assert.Equal(t, 1, kinds[KindChromium],
		"a browser under ...\\Chromium\\ must not be misreported as Chrome just because its exe is chrome.exe")
	assert.Equal(t, 1, kinds[KindEdge])
	assert.Equal(t, 1, chromePaths, "a browser reachable from both $PATH and ProgramFiles must be deduplicated")
}

// ─── override ────────────────────────────────────────────────────────────────

// TestDetect_EnvOverrideWins pins the documented escape hatch: when detection
// guesses wrong, an explicit path must short-circuit everything else.
func TestDetect_EnvOverrideWins(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.env[EnvBrowserPath] = "/opt/custom/chrome"
	h.files["/opt/custom/chrome"] = true
	h.files["/usr/bin/chromium"] = true
	h.onPath["chromium"] = "/usr/bin/chromium"
	h.versions["/opt/custom/chrome --version"] = "Google Chrome 142.0.0.1"

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 1, "the override must be the only candidate")
	assert.Equal(t, SourceEnv, got[0].Source)
	assert.Equal(t, "/opt/custom/chrome", got[0].Path)
	assert.Equal(t, VerOK, got[0].Verification)
}

// A typo in MOCODE_BROWSER_PATH must not surface a phantom candidate that then
// fails much later with a confusing error; it is dropped here and explained by
// DetectOrError instead. Silently substituting a different browser would be
// worse: the user asked for a specific one.
func TestDetect_EnvOverrideMissingFileIsNotACandidate(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.env[EnvBrowserPath] = "/opt/typo/chrome"
	h.files["/usr/bin/chromium"] = true

	assert.Empty(t, Detect(context.Background(), h.opts()))

	_, err := DetectOrError(context.Background(), h.opts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/opt/typo/chrome")
	assert.Contains(t, err.Error(), "did not exist or did not run")
}

// A browser that exists but refuses to report a version must still be returned,
// just ranked below verified ones — otherwise a host would report no browser.
func TestDetect_UnverifiedButPresentIsStillReturned(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.files["/usr/bin/chromium"] = true

	got := Detect(context.Background(), h.opts())
	require.Len(t, got, 1)
	assert.Equal(t, VerFailed, got[0].Verification)
	assert.Contains(t, got[0].String(), "version probe failed")
}

// A directory where an executable should be is not a browser.
func TestDetect_DirectoryIsNotACandidate(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.env[EnvBrowserPath] = "/usr/bin"
	h.dirs["/usr/bin"] = true

	assert.Empty(t, Detect(context.Background(), h.opts()))
}

func TestDetect_None(t *testing.T) {
	t.Parallel()

	assert.Empty(t, Detect(context.Background(), linuxHost().opts()))
}

func TestDetectOrError_ListsWhatWasTried(t *testing.T) {
	t.Parallel()

	_, err := DetectOrError(context.Background(), linuxHost().opts())
	require.Error(t, err)

	msg := err.Error()
	for _, want := range []string{
		"/usr/bin/google-chrome-stable",
		"/snap/bin/chromium",
		"/usr/bin/chromium-browser",
		"com.google.Chrome",
		EnvBrowserPath,
		"Firefox and WebKit are not supported",
	} {
		assert.Contains(t, msg, want, "the error must explain what was tried")
	}
}

func TestDetectOrError_SucceedsWhenFound(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.files["/usr/bin/chromium"] = true
	h.versions["/usr/bin/chromium --version"] = "Chromium 141.0.0.0"

	got, err := DetectOrError(context.Background(), h.opts())
	require.NoError(t, err)
	require.Len(t, got, 1)
}

// ─── parsing helpers ─────────────────────────────────────────────────────────

// TestExtractVersion_IgnoresLogNoise is the regression test for a real defect
// caught by the on-machine smoke test: chrome.exe's diagnostics were parsed as a
// version, so detection reported "102041.503" — a timestamp lifted out of a log
// line like "[31104:44316:0914/102041.503:ERROR:...]".
func TestExtractVersion_IgnoresLogNoise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"chrome", "Google Chrome 141.0.7390.55\n", "141.0.7390.55"},
		{"chromium", "Chromium 141.0.7390.0\n", "141.0.7390.0"},
		{"edge", "Microsoft Edge 141.0.3537.57\n", "141.0.3537.57"},
		{"brave", "Brave Browser 1.71.118\n", "1.71.118"},
		{"leading blank line", "\n\nChromium 141.0.0.0\n", "141.0.0.0"},
		{
			name: "log noise with a timestamp",
			in:   "[31104:44316:0914/102041.503:ERROR:new_tab_ui.cc:54] Requested load\n",
			want: "",
		},
		{
			// The version on the first line wins; diagnostics below it must not
			// be consulted at all.
			name: "version then diagnostics",
			in:   "Google Chrome 141.0.7390.55\n[0914/102041.503:ERROR:x.cc] boom\n",
			want: "141.0.7390.55",
		},
		{"version embedded in a path", "/opt/141.0.7390.55/chrome --version\n", ""},
		{"no version at all", "no version here\n", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extractVersion(tt.in))
		})
	}
}

func TestKindFromPath(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]Kind{
		"/usr/bin/google-chrome":                                     KindChrome,
		"/usr/bin/chromium":                                          KindChromium,
		"/usr/bin/chromium-browser":                                  KindChromium,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`:     KindEdge,
		`C:\Users\dev\AppData\Local\Chromium\Application\chrome.exe`: KindChromium,
		"/usr/bin/brave-browser":                                     KindBrave,
		"/usr/bin/vivaldi-stable":                                    KindVivaldi,
		"/usr/bin/opera":                                             KindOpera,
		"/opt/google/chrome/chrome":                                  KindChrome,
	} {
		assert.Equal(t, want, kindFromPath(path), "path %q", path)
	}
}

// joinFor must produce target-OS separators so the install tables stay correct
// (and assertable) when a test runs them from a different platform.
func TestJoinFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		joinFor("darwin", "/Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"))
	assert.Equal(t, `C:\Program Files\Google\Chrome\Application\chrome.exe`,
		joinFor("windows", `C:\Program Files`, `Google\Chrome\Application\chrome.exe`))
}

func TestParentDir(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `C:\Program Files\Google\Chrome\Application`,
		parentDir(`C:\Program Files\Google\Chrome\Application\chrome.exe`))
	assert.Equal(t, "/Applications/Google Chrome.app/Contents/MacOS",
		parentDir("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"))
	assert.Equal(t, "", parentDir("chrome.exe"))
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	assert.Positive(t, compareVersions(versionParts("153.0.4234.32"), versionParts("153.0.9.9")),
		"numeric, not lexicographic")
	assert.Zero(t, compareVersions(versionParts("141.0.7390.55"), versionParts("141.0.7390.55")))
	assert.Negative(t, compareVersions(versionParts("141.0"), versionParts("141.0.1")))
	assert.Negative(t, compareVersions(nil, versionParts("1.0")))
	assert.Zero(t, compareVersions(nil, nil))
}

func TestSortCandidates_VerificationFirstThenProduct(t *testing.T) {
	t.Parallel()

	in := []Candidate{
		{Kind: KindBrave, Path: "/b", Verification: VerFailed},
		{Kind: KindEdge, Path: "/e", Verification: VerOK},
		{Kind: KindChrome, Path: "/c", Verification: VerOK},
		{Kind: KindChrome, Path: "/c2", Verification: VerNotRun},
	}

	got := sortCandidates(in)
	require.Len(t, got, 4)
	assert.Equal(t, "/c", got[0].Path, "verified chrome first")
	assert.Equal(t, "/e", got[1].Path, "verified edge next")
	assert.Equal(t, "/c2", got[2].Path, "not-probed chrome beats a failed probe")
	assert.Equal(t, "/b", got[3].Path)
}

func TestReport_EmptyExplainsWhy(t *testing.T) {
	t.Parallel()

	report := Report(context.Background(), linuxHost().opts())
	assert.Contains(t, report, "linux")
	assert.Contains(t, report, "No usable browser found")
	assert.Contains(t, report, "/usr/bin/google-chrome-stable")
}

func TestReport_ListsCandidates(t *testing.T) {
	t.Parallel()

	h := linuxHost()
	h.env[EnvCDPEndpoint] = "ws://127.0.0.1:9222/devtools/browser/abc"
	h.files["/usr/bin/chromium"] = true
	h.versions["/usr/bin/chromium --version"] = "Chromium 141.0.7390.0"

	report := Report(context.Background(), h.opts())
	assert.Contains(t, report, "Found 1 browser(s)")
	assert.Contains(t, report, "chromium")
	assert.Contains(t, report, EnvCDPEndpoint, "attach mode must be surfaced so it is clear no browser is launched")
	assert.Contains(t, report, "no browser is launched")
}

// ─── real host ───────────────────────────────────────────────────────────────

// TestDetect_RealHost is a smoke test against the actual machine rather than a
// fake, so the platform table cannot pass while being wrong about reality. It
// skips instead of failing where no browser is installed (CI), because "no
// browser here" is not a defect in detection.
func TestDetect_RealHost(t *testing.T) {
	var probes []string
	opts := DetectOptions{}
	if runtime.GOOS == "windows" {
		// Guard the side-effect guarantee on the real machine too: if this ever
		// starts launching browsers, the test must not be what discovers it.
		opts.Probe = func(_ context.Context, bin string, prefix []string) (string, bool) {
			probes = append(probes, bin)
			return "", false
		}
	}

	got := Detect(context.Background(), opts)
	if len(got) == 0 {
		t.Skip("no Chromium-family browser installed on this host")
	}

	t.Logf("detected %d browser(s) on %s:", len(got), runtime.GOOS)
	for i, c := range got {
		t.Logf("  %d. %s", i+1, c)
	}

	for _, c := range got {
		assert.NotEmpty(t, c.Path)
		assert.NotEqual(t, SourceFlatpak, c.Source, "flatpak only exists on linux")
		if c.Version != "" {
			assert.Regexp(t, `^\d{1,4}(\.\d{1,4}){1,3}$`, c.Version,
				"a reported version must be a browser version, not log noise")
		}
	}

	if runtime.GOOS == "windows" {
		assert.Empty(t, probes, "detection must never execute a Windows browser")
		for _, c := range got {
			assert.Equal(t, VerNotRun, c.Verification)
		}
		t.Logf("versions read from the install layout: %v", versionsOf(got))
	}
}

func versionsOf(cs []Candidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, string(c.Kind)+"="+c.Version)
	}
	return out
}
