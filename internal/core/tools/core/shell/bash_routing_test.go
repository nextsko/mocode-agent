package shell

import (
	"errors"
	"strings"
	"testing"
)

func TestMissingShellCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stderr string
		want   string
	}{
		{
			name:   "mvdan quoted not found",
			stderr: `"head": executable file not found in $PATH`,
			want:   "head",
		},
		{
			name:   "mvdan unquoted not found",
			stderr: `grep: executable file not found in $PATH`,
			want:   "grep",
		},
		{
			name:   "posix command not found",
			stderr: "grep: command not found",
			want:   "grep",
		},
		{
			name:   "prefixed posix command not found",
			stderr: "bash: sed: command not found",
			want:   "sed",
		},
		{
			name:   "windows cmd not recognized",
			stderr: `'head' is not recognized as an internal or external command`,
			want:   "head",
		},
		{
			name:   "powershell term not recognized",
			stderr: `The term 'tail' is not recognized as the name of a cmdlet`,
			want:   "tail",
		},
		{
			name:   "unrelated failure yields empty",
			stderr: "cannot open file: no such file or directory",
			want:   "",
		},
		{
			name:   "empty stderr yields empty",
			stderr: "",
			want:   "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := missingShellCommand(tt.stderr); got != tt.want {
				t.Fatalf("missingShellCommand(%q) = %q, want %q", tt.stderr, got, tt.want)
			}
		})
	}
}

func TestCommandRoutingHint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		stderr    string
		wantEmpty bool
		wantHas   []string
	}{
		{
			name:    "head routes to view",
			stderr:  `"head": executable file not found in $PATH`,
			wantHas: []string{"hint:", "`head`", "`view`"},
		},
		{
			name:    "grep routes to grep tool",
			stderr:  "grep: command not found",
			wantHas: []string{"`grep` tool"},
		},
		{
			name:    "rg routes to grep tool",
			stderr:  "rg: command not found",
			wantHas: []string{"`grep` tool"},
		},
		{
			name:    "find routes to glob",
			stderr:  "'find' is not recognized as an internal or external command",
			wantHas: []string{"`glob`"},
		},
		{
			name:      "unroutable missing command yields no hint",
			stderr:    "tea: command not found",
			wantEmpty: true,
		},
		{
			name:      "no missing command yields no hint",
			stderr:    "fatal: not a git repository",
			wantEmpty: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := commandRoutingHint(tt.stderr)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("commandRoutingHint(%q) = %q, want empty", tt.stderr, got)
				}
				return
			}
			if got == "" {
				t.Fatalf("commandRoutingHint(%q) = empty, want a hint", tt.stderr)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Fatalf("commandRoutingHint(%q) = %q, missing %q", tt.stderr, got, want)
				}
			}
		})
	}
}

func TestFormatOutputAppendsRoutingHint(t *testing.T) {
	t.Run("missing command gets a hint", func(t *testing.T) {
		got := formatOutput("", `"head": executable file not found in $PATH`, errors.New("exit status 127"))
		if !strings.Contains(got, "executable file not found") {
			t.Fatalf("diagnostic lost, got %q", got)
		}
		if !strings.Contains(got, "hint:") {
			t.Fatalf("expected routing hint, got %q", got)
		}
		if !strings.Contains(got, "`view`") {
			t.Fatalf("expected view suggestion, got %q", got)
		}
	})

	t.Run("ordinary failure keeps no hint", func(t *testing.T) {
		got := formatOutput("", "fatal: not a git repository", errors.New("exit status 128"))
		if strings.Contains(got, "hint:") {
			t.Fatalf("unexpected routing hint for ordinary failure: %q", got)
		}
	})

	t.Run("success has no hint", func(t *testing.T) {
		got := formatOutput("hello\n", "", nil)
		if strings.Contains(got, "hint:") {
			t.Fatalf("unexpected hint on success: %q", got)
		}
	})
}
