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
	abcLines   = "a\nb\nc\n"
	abLines    = "a\nb\n"
	abcdefText = "abcdef"
	abcCSV     = "a,b,c\n"
)

// runPipe invokes a pipe command directly through its table entry, which is
// possible because the commands take a plain pipeEnv rather than an
// interp.HandlerContext.
func runPipe(t *testing.T, cmd string, args []string, input, dir string) (stdout, stderr string, err error) {
	t.Helper()
	run, ok := pipeCommands[cmd]
	if !ok {
		t.Fatalf("no such pipe command %q", cmd)
	}
	var out, errBuf strings.Builder
	env := pipeEnv{
		dir:    dir,
		stdin:  strings.NewReader(input),
		stdout: &out,
		stderr: &errBuf,
	}
	err = run(env, args)
	return out.String(), errBuf.String(), err
}

func TestHead(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{"default lines", nil, "1\n2\n3\n", "1\n2\n3\n"},
		{"first two", []string{"-n", "2"}, abcLines, abLines},
		{"attached count", []string{"-n1"}, abLines, "a\n"},
		{"all but last", []string{"-n", "-1"}, abcLines, abLines},
		{"bytes", []string{"-c", "3"}, abcdefText, "abc"},
		{"obsolete count", []string{"-2"}, abcLines, abLines},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "head", tt.args, tt.input, "")
			if err != nil {
				t.Fatalf("head error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("head%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestHeadMultipleFilesHeaders(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "1\n2\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), "3\n4\n")

	got, _, err := runPipe(t, "head", []string{"a.txt", "b.txt"}, "", dir)
	if err != nil {
		t.Fatalf("head error: %v", err)
	}
	want := "==> a.txt <==\n1\n2\n\n==> b.txt <==\n3\n4\n"
	if got != want {
		t.Fatalf("head multi-file =\n%q\nwant\n%q", got, want)
	}
}

func TestTail(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{"last two", []string{"-n", "2"}, abcLines, "b\nc\n"},
		{"from second line", []string{"-n", "+2"}, abcLines, "b\nc\n"},
		{"last bytes", []string{"-c", "3"}, abcdefText, "def"},
		{"bytes from start", []string{"-c", "+3"}, abcdefText, "cdef"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "tail", tt.args, tt.input, "")
			if err != nil {
				t.Fatalf("tail error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("tail%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestWC(t *testing.T) {
	const in = "a b\nc\n"
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"lines", []string{"-l"}, "2\n"},
		{"words", []string{"-w"}, "3\n"},
		{"bytes", []string{"-c"}, "6\n"},
		{"chars", []string{"-m"}, "6\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "wc", tt.args, in, "")
			if err != nil {
				t.Fatalf("wc error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("wc%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestTee(t *testing.T) {
	dir := t.TempDir()
	got, _, err := runPipe(t, "tee", []string{"out.txt"}, "hello\nworld\n", dir)
	if err != nil {
		t.Fatalf("tee error: %v", err)
	}
	if got != "hello\nworld\n" {
		t.Fatalf("tee stdout = %q", got)
	}
	content, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("read tee output: %v", err)
	}
	if string(content) != "hello\nworld\n" {
		t.Fatalf("tee file = %q", string(content))
	}

	// append mode
	if _, _, err := runPipe(t, "tee", []string{"-a", "out.txt"}, "more\n", dir); err != nil {
		t.Fatalf("tee -a error: %v", err)
	}
	content, _ = os.ReadFile(filepath.Join(dir, "out.txt"))
	if string(content) != "hello\nworld\nmore\n" {
		t.Fatalf("tee -a file = %q", string(content))
	}
}

func TestSort(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{"lexical", nil, "b\na\nc\n", abcLines},
		{"reverse", []string{"-r"}, "b\na\nc\n", "c\nb\na\n"},
		{"numeric", []string{"-n"}, "10\n2\n1\n", "1\n2\n10\n"},
		{"unique", []string{"-u"}, "a\na\nb\n", abLines},
		{"key field", []string{"-t", ",", "-k", "2"}, "x,3\ny,1\nz,2\n", "y,1\nz,2\nx,3\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "sort", tt.args, tt.input, "")
			if err != nil {
				t.Fatalf("sort error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("sort%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestUniq(t *testing.T) {
	const in = "a\na\nb\n"
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"dedupe", nil, abLines},
		{"count", []string{"-c"}, "      2 a\n      1 b\n"},
		{"only repeats", []string{"-d"}, "a\n"},
		{"only unique", []string{"-u"}, "b\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "uniq", tt.args, in, "")
			if err != nil {
				t.Fatalf("uniq error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("uniq%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestCut(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{"field two", []string{"-d", ",", "-f", "2"}, abcCSV, "b\n"},
		{"fields one and three", []string{"-d", ",", "-f", "1,3"}, abcCSV, "a,c\n"},
		{"field range", []string{"-d", ",", "-f", "2-3"}, abcCSV, "b,c\n"},
		{"characters", []string{"-c", "2-3"}, "hello\n", "el\n"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "cut", tt.args, tt.input, "")
			if err != nil {
				t.Fatalf("cut error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("cut%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestTr(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{"translate range", []string{"a-z", "A-Z"}, "abc", "ABC"},
		{"delete digits", []string{"-d", "0-9"}, "a1b2", "ab"},
		{"squeeze spaces", []string{"-s", " "}, "a   b", "a b"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := runPipe(t, "tr", tt.args, tt.input, "")
			if err != nil {
				t.Fatalf("tr error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("tr%v = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestPipeCommandErrorIsNonFatalExitStatus(t *testing.T) {
	_, stderr, err := runPipe(t, "head", []string{"-Z"}, "x\n", "")
	if err == nil {
		t.Fatal("expected error for invalid option")
	}
	var status interp.ExitStatus
	if !errors.As(err, &status) {
		t.Fatalf("expected interp.ExitStatus, got %T", err)
	}
	if status != interp.ExitStatus(1) {
		t.Fatalf("exit status = %d, want 1", status)
	}
	if !strings.Contains(stderr, "invalid option") {
		t.Fatalf("stderr = %q, want 'invalid option'", stderr)
	}
}

func TestPipeUtilsEnabledFlag(t *testing.T) {
	t.Setenv("MOCODE_PIPE_UTILS", "true")
	if !pipeUtilsEnabled() {
		t.Fatal("MOCODE_PIPE_UTILS=true should enable pipe utils")
	}
	t.Setenv("MOCODE_PIPE_UTILS", "false")
	if pipeUtilsEnabled() {
		t.Fatal("MOCODE_PIPE_UTILS=false should disable pipe utils")
	}
}

// TestPipeUtilsThroughInterpreter exercises the full path: the middleware is
// registered, the interpreter's pipes are wired, and the exit status is set.
func TestPipeUtilsThroughInterpreter(t *testing.T) {
	t.Setenv("MOCODE_PIPE_UTILS", "true")
	s := NewShell(&Options{WorkingDir: t.TempDir()})

	out, _, err := s.Exec(context.Background(), "printf 'a\\nb\\nc\\n' | head -2")
	if err != nil {
		t.Fatalf("Exec error: %v", err)
	}
	if out != abLines {
		t.Fatalf("piped head output = %q, want %q", out, abLines)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
