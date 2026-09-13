package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fdGoPattern = `\.go$`
	fdFooGo     = "foo.go"
	fdSubBazGo  = "sub/baz.go"
)

func fdTestTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite(t, filepath.Join(dir, fdFooGo), "")
	mustWrite(t, filepath.Join(dir, "bar.txt"), "")
	mustWrite(t, filepath.Join(dir, "ignored.go"), "")
	mustWrite(t, filepath.Join(dir, ".hidden.go"), "")
	mustWrite(t, filepath.Join(dir, "sub", "baz.go"), "")
	mustWrite(t, filepath.Join(dir, "sub", "qux.txt"), "")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.go\n")
	return dir
}

func fdSet(out string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l == "" {
			continue
		}
		set[filepath.ToSlash(l)] = true
	}
	return set
}

func TestFd(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "regex skips hidden and ignored",
			args: []string{fdGoPattern},
			want: []string{fdFooGo, fdSubBazGo},
		},
		{
			name: "extension filter",
			args: []string{"-e", "txt"},
			want: []string{"bar.txt", "sub/qux.txt"},
		},
		{
			name: "dirs only",
			args: []string{"-t", "d"},
			want: []string{"sub"},
		},
		{
			name: "glob mode",
			args: []string{"-g", "*.txt"},
			want: []string{"bar.txt", "sub/qux.txt"},
		},
		{
			name: "hidden included",
			args: []string{"-H", "-e", "go"},
			want: []string{".hidden.go", fdFooGo, fdSubBazGo},
		},
		{
			name: "no-ignore",
			args: []string{"-I", "ignored"},
			want: []string{"ignored.go"},
		},
		{
			name: "max depth",
			args: []string{"-d", "1", fdGoPattern},
			want: []string{fdFooGo},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			out, stderr, err := runPipe(t, "fd", tt.args, "", fdTestTree(t))
			if err != nil {
				t.Fatalf("fd error: %v (stderr: %s)", err, stderr)
			}
			got := fdSet(out)
			want := fdSet(strings.Join(tt.want, "\n"))
			if len(got) != len(want) {
				t.Fatalf("fd %v = %v, want %v", tt.args, got, want)
			}
			for k := range want {
				if !got[k] {
					t.Fatalf("fd %v missing %q (got %v)", tt.args, k, got)
				}
			}
		})
	}
}

func TestFdMaxResults(t *testing.T) {
	out, _, err := runPipe(t, "fd", []string{"--max-results", "1", fdGoPattern}, "", fdTestTree(t))
	if err != nil {
		t.Fatalf("fd error: %v", err)
	}
	if n := len(fdSet(out)); n != 1 {
		t.Fatalf("fd --max-results 1 returned %d entries, want 1 (%q)", n, out)
	}
}

func TestFdThroughInterpreter(t *testing.T) {
	if hostFdPath() != "" {
		t.Skip("real fd binary present; the Go fallback is not exercised")
	}
	t.Setenv("MOCODE_PIPE_UTILS", "true")

	dir := fdTestTree(t)
	s := NewShell(&Options{WorkingDir: dir})
	out, _, err := s.Exec(context.Background(), `fd '\.go$'`)
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	got := fdSet(out)
	for _, want := range []string{fdFooGo, fdSubBazGo} {
		if !got[want] {
			t.Fatalf("interpreter fd missing %q (got %v)", want, got)
		}
	}
}

func TestFdInvalidOption(t *testing.T) {
	_, stderr, err := runPipe(t, "fd", []string{"-Z"}, "", fdTestTree(t))
	if err == nil {
		t.Fatal("expected error for invalid option")
	}
	if !strings.Contains(stderr, "unrecognized option") {
		t.Fatalf("stderr = %q, want 'unrecognized option'", stderr)
	}
}

func TestFdMissingRoot(t *testing.T) {
	_, stderr, err := runPipe(t, "fd", []string{"x", "does-not-exist"}, "", t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(stderr, "does-not-exist") {
		t.Fatalf("stderr = %q, want the offending path", stderr)
	}
}
