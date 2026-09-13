package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/interp"
)

const (
	rgFunc     = "func"
	rgPackage  = "package"
	rgFooUpper = "a.go:2:func Foo() {}"
	rgFooLower = "sub/c.go:1:func foo() {}"
)

func rgTestTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\nfunc Foo() {}\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), "hello world\nHELLO again\n")
	mustWrite(t, filepath.Join(dir, "ignored.go"), "package ignored\n")
	mustWrite(t, filepath.Join(dir, ".hidden.go"), "package hidden\n")
	mustWrite(t, filepath.Join(dir, "sub", "c.go"), "func foo() {}\n")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.go\n")
	return dir
}

func rgSet(out string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l == "" {
			continue
		}
		set[filepath.ToSlash(l)] = true
	}
	return set
}

func assertRgSet(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	if len(got) != len(wantSet) {
		t.Fatalf("got %v, want %v", got, wantSet)
	}
	for k := range wantSet {
		if !got[k] {
			t.Fatalf("missing %q (got %v)", k, got)
		}
	}
}

func TestRg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "recursive with filename and line",
			args: []string{rgFunc},
			want: []string{rgFooUpper, rgFooLower},
		},
		{
			name: "single file omits filename",
			args: []string{"-i", "hello", "b.txt"},
			want: []string{"hello world", "HELLO again"},
		},
		{
			name: "fixed strings",
			args: []string{"-F", "func Foo"},
			want: []string{rgFooUpper},
		},
		{
			name: "word regexp",
			args: []string{"-w", "foo"},
			want: []string{rgFooLower},
		},
		{
			name: "invert match",
			args: []string{"-v", "Foo", "a.go"},
			want: []string{"package main"},
		},
		{
			name: "files with matches",
			args: []string{"-l", rgFunc},
			want: []string{"a.go", "sub/c.go"},
		},
		{
			name: "count",
			args: []string{"-c", rgFunc},
			want: []string{"a.go:1", "sub/c.go:1"},
		},
		{
			name: "only matching",
			args: []string{"-o", "[A-Za-z]+oo"},
			want: []string{"a.go:2:Foo", "sub/c.go:1:foo"},
		},
		{
			name: "glob include",
			args: []string{"-g", "*.go", rgPackage},
			want: []string{"a.go:1:package main"},
		},
		{
			name: "hidden included",
			args: []string{"--hidden", rgPackage},
			want: []string{"a.go:1:package main", ".hidden.go:1:package hidden"},
		},
		{
			name: "no-ignore",
			args: []string{"--no-ignore", "ignored"},
			want: []string{"ignored.go:1:package ignored"},
		},
		{
			name: "directory path",
			args: []string{"foo", "sub"},
			want: []string{rgFooLower},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, stderr, err := runPipe(t, "rg", tt.args, "", rgTestTree(t))
			if err != nil {
				t.Fatalf("rg error: %v (stderr: %s)", err, stderr)
			}
			assertRgSet(t, rgSet(out), tt.want)
		})
	}
}

func TestRgNoMatchExitStatus(t *testing.T) {
	_, _, err := runPipe(t, "rg", []string{"zzz-no-match"}, "", rgTestTree(t))
	if err == nil {
		t.Fatal("expected exit status 1 for no matches")
	}
	var st interp.ExitStatus
	if !errors.As(err, &st) {
		t.Fatalf("expected interp.ExitStatus, got %T (%v)", err, err)
	}
	if st != interp.ExitStatus(1) {
		t.Fatalf("exit status = %d, want 1", st)
	}
}

func TestRgThroughInterpreter(t *testing.T) {
	if hostRgPath() != "" {
		t.Skip("real rg binary present; the Go fallback is not exercised")
	}
	t.Setenv("MOCODE_PIPE_UTILS", "true")

	dir := rgTestTree(t)
	s := NewShell(&Options{WorkingDir: dir})
	out, _, err := s.Exec(context.Background(), `rg func`)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	got := rgSet(out)
	for _, want := range []string{rgFooUpper, rgFooLower} {
		if !got[want] {
			t.Fatalf("interpreter rg missing %q (got %v)", want, got)
		}
	}
}
