package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubHome swaps the home-directory helpers used by config.go for the
// duration of a test.  It restores the originals via t.Cleanup.
func stubHome(t *testing.T, dir string) {
	t.Helper()
	prevDir, prevKH, prevCfg, prevExists, prevRead := SSHDirPath, KnownHostsPath, ConfigPath, fileExists, readFile
	SSHDirPath = func() string { return dir }
	KnownHostsPath = func() string { return filepath.Join(dir, "known_hosts") }
	ConfigPath = func() string { return filepath.Join(dir, "config") }
	fileExists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	readFile = func(p string) ([]byte, error) { return os.ReadFile(p) }
	t.Cleanup(func() {
		SSHDirPath = prevDir
		KnownHostsPath = prevKH
		ConfigPath = prevCfg
		fileExists = prevExists
		readFile = prevRead
	})
}

func TestParseSshConfig_Basic(t *testing.T) {
	in := `# Top-level comment
Host prod
    HostName 10.0.0.1
    User deploy
    Port 2222
    IdentityFile ~/.ssh/prod_ed25519
    ProxyJump bastion

Host *.example.com
    User guest

Host stag*
    HostName staging.internal
`
	got := parseSshConfig(in)

	if len(got) != 1 {
		t.Fatalf("want 1 non-wildcard host, got %d: %+v", len(got), got)
	}
	prod := got[0]
	if prod.Alias != "prod" {
		t.Errorf("alias = %q", prod.Alias)
	}
	if prod.HostName != "10.0.0.1" {
		t.Errorf("hostname = %q", prod.HostName)
	}
	if prod.User != "deploy" {
		t.Errorf("user = %q", prod.User)
	}
	if prod.Port != 2222 {
		t.Errorf("port = %d", prod.Port)
	}
	if prod.IdentityFile != "~/.ssh/prod_ed25519" {
		t.Errorf("identity = %q", prod.IdentityFile)
	}
	if prod.ProxyJump != "bastion" {
		t.Errorf("proxy = %q", prod.ProxyJump)
	}

	// `stag*` is a wildcard, must be filtered.
	for _, h := range got {
		if strings.ContainsAny(h.Alias, "*?") {
			t.Errorf("wildcard leaked through: %q", h.Alias)
		}
	}
}

func TestParseSshConfig_Empty(t *testing.T) {
	if got := parseSshConfig(""); len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
	if got := parseSshConfig("# only comments\n\n"); len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

func TestParseExplicit(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
		user string
		ok   bool
	}{
		{"user@host", "host", 0, "user", true},
		{"user@host:22", "host", 22, "user", true},
		{"host:2222", "host", 2222, "", true},
		{"[::1]:22", "::1", 22, "", true},
		{"[2001:db8::1]", "2001:db8::1", 0, "", true},
		{"bare", "", 0, "", false}, // bare hostnames fall through to config lookup
		{"", "", 0, "", false},
		{"host:", "", 0, "", false},
	}
	for _, c := range cases {
		got := parseExplicit(c.in)
		if (got.Alias != "") != c.ok {
			t.Errorf("parseExplicit(%q) ok = %v (alias=%q)", c.in, c.ok, got.Alias)
			continue
		}
		if !c.ok {
			continue
		}
		if got.Host != c.host {
			t.Errorf("parseExplicit(%q).Host = %q, want %q", c.in, got.Host, c.host)
		}
		if got.Port != c.port {
			t.Errorf("parseExplicit(%q).Port = %d, want %d", c.in, got.Port, c.port)
		}
		if got.User != c.user {
			t.Errorf("parseExplicit(%q).User = %q, want %q", c.in, got.User, c.user)
		}
	}
}

func TestParsePort(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"22", 22, false},
		{" 22 ", 22, false},
		{"65535", 65535, false},
		{"65536", 0, true},
		{"0", 0, true},
		{"abc", 0, true},
	} {
		got, err := parsePort(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parsePort(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
		}
		if err == nil && got != c.want {
			t.Errorf("parsePort(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIsCommandBanned(t *testing.T) {
	for _, c := range []struct {
		in     string
		banned bool
	}{
		{"ls -la", false},
		{"echo hello", false},
		{"rm -rf /", true},
		{"rm -fr /etc", true},
		{"shutdown -h now", true},
		{"reboot", true},
		{"mkfs.ext4 /dev/sda1", true},
		{"dd if=/dev/zero of=/dev/sda", false}, // not in the conservative list
	} {
		got := isCommandBanned(c.in)
		if (got != "") != c.banned {
			t.Errorf("isCommandBanned(%q) = %q, banned=%v", c.in, got, c.banned)
		}
	}
}

func TestIsPathSafe(t *testing.T) {
	for _, c := range []struct {
		in   string
		safe bool
	}{
		{"/var/log/app.log", true},
		{"/home/user/file", true},
		{"relative/file", true},
		{"/etc/../etc/passwd", false},
		{"..", false},
		{"../etc", false},
		{"a/b/../../c", false},
		{"", false},
		{"C:\\Users\\foo", true}, // Windows path with no ".."
		{"C:\\..\\evil", false},
	} {
		if got := isPathSafe(c.in); got != c.safe {
			t.Errorf("isPathSafe(%q) = %v, want %v", c.in, got, c.safe)
		}
	}
}

func TestParseMode(t *testing.T) {
	for _, c := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"0644", 0o644, false},
		{"0755", 0o755, false},
		{"0777", 0o777, false},
		{"888", 0, true}, // '8' is not octal
		{"abc", 0, true},
	} {
		got, err := parseMode(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseMode(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && int(got) != c.want {
			t.Errorf("parseMode(%q) = %o, want %o", c.in, got, c.want)
		}
	}
}

func TestResolver_ExplicitForm(t *testing.T) {
	// No real ~/.ssh/config access in tests: point at an empty tempdir.
	stubHome(t, t.TempDir())
	r := NewResolver(nil)

	cases := []struct {
		in   string
		host string
		port int
		user string
	}{
		{"root@10.0.0.5:2222", "10.0.0.5", 2222, "root"},
		{"admin@127.0.0.1", "127.0.0.1", 22, "admin"},
		{"127.0.0.1:2200", "127.0.0.1", 2200, ""}, // user falls back to current OS user
	}
	for _, c := range cases {
		spec, err := r.Resolve(c.in)
		if err != nil {
			t.Errorf("Resolve(%q): %v", c.in, err)
			continue
		}
		if spec.Host != c.host {
			t.Errorf("Resolve(%q).Host = %q, want %q", c.in, spec.Host, c.host)
		}
		if spec.EffectivePort() != c.port {
			t.Errorf("Resolve(%q).Port = %d, want %d", c.in, spec.EffectivePort(), c.port)
		}
		if c.user != "" && spec.User != c.user {
			t.Errorf("Resolve(%q).User = %q, want %q", c.in, spec.User, c.user)
		}
	}
}

func TestResolver_CacheHit(t *testing.T) {
	stubHome(t, t.TempDir())
	r := NewResolver(nil)
	a, err := r.Resolve("user@1.2.3.4:22")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Resolve("user@1.2.3.4:22")
	if err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Errorf("cache miss: %q vs %q", a, b)
	}
}

func TestResolver_EmptyInput(t *testing.T) {
	r := NewResolver(nil)
	if _, err := r.Resolve(""); err == nil {
		t.Errorf("Resolve(\"\") should error")
	}
}
