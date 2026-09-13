package slash

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanCommandText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "real escape sequence",
			in:   "\x1b[38;2;104;255;214m/history",
			want: "/history",
		},
		{
			name: "esc-less bracket residue",
			in:   "[38;2;223;219;221mShow help & key bindings",
			want: "Show help & key bindings",
		},
		{
			name: "esc-less bare residue",
			in:   "8;2;104;255;214mBrowse past sessions",
			want: "Browse past sessions",
		},
		{
			name: "esc-less leading semicolon",
			in:   ";2;104;255;214mMCP servers",
			want: "MCP servers",
		},
		{
			name: "short residue",
			in:   "219;221mToggle transparent background",
			want: "Toggle transparent background",
		},
		{
			name: "plain text untouched",
			in:   "Switch model",
			want: "Switch model",
		},
		{
			name: "bracketed prose survives",
			in:   "Run [1] before [a]",
			want: "Run [1] before [a]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, CleanCommandText(tt.in))
		})
	}
}

func TestCustomCommandDisplayLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  CustomCommand
		want string
	}{
		{
			name: "path with residue in directory name",
			cmd:  CustomCommand{ID: "user:ABCDEFG", Path: "8;2;104;255;214m/history.md"},
			want: "/history",
		},
		{
			name: "nested clean path",
			cmd:  CustomCommand{ID: "user:ABCDEFG", Path: "knowledge/init_kng.md"},
			want: "knowledge/init_kng",
		},
		{
			name: "windows separators",
			cmd:  CustomCommand{ID: "user:ABCDEFG", Path: `ops\deploy.md`},
			want: "ops/deploy",
		},
		{
			name: "falls back to id",
			cmd:  CustomCommand{ID: "user:ABCDEFG"},
			want: "user:ABCDEFG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.cmd.DisplayLabel())
		})
	}
}
