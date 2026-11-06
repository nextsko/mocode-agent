package ext

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSmartIsAbs(t *testing.T) {
	t.Parallel()

	// On Windows, paths rooted at the current volume (e.g. `\a`) count as
	// absolute too; elsewhere only OS-absolute paths do.
	rootedExpected := runtime.GOOS == "windows"

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "empty string", input: "", expected: false},
		{name: "relative filename", input: "a", expected: false},
		{name: "relative nested path", input: "a/b", expected: false},
		{name: "dot relative", input: "./a", expected: false},
		{name: "parent relative", input: "../a/b", expected: false},
		{name: "unix absolute", input: "/a", expected: true},
		{name: "unix absolute nested", input: "/a/b/c", expected: true},
		{name: "root only", input: "/", expected: true},
		{name: "volume rooted", input: `\a`, expected: rootedExpected},
		{name: "windows drive absolute", input: `C:\a`, expected: runtime.GOOS == "windows"},
		{name: "windows drive forward slashes", input: "C:/a", expected: runtime.GOOS == "windows"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, SmartIsAbs(tt.input))
		})
	}
}

func TestSmartJoin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		one      string
		two      string
		expected string
	}{
		{name: "empty base and second", one: "", two: "", expected: ""},
		{name: "empty base joins second", one: "", two: "b", expected: "b"},
		{name: "empty second keeps base", one: "a", two: "", expected: "a"},
		{name: "two relative parts", one: "a", two: "b", expected: filepath.Join("a", "b")},
		{name: "relative base with nested relative", one: "a", two: "b/c", expected: filepath.Join("a", "b/c")},
		{name: "absolute base with relative tail", one: "/a", two: "b", expected: filepath.Join("/a", "b")},
		{name: "absolute second replaces base", one: "a", two: "/b", expected: "/b"},
		{name: "both absolute second wins", one: "/a", two: "/b", expected: "/b"},
		{name: "trailing slash base with absolute second", one: "/a/", two: "/b", expected: "/b"},
		{name: "dot segments cleaned", one: "/a", two: "../b", expected: "/b"},
		{name: "redundant slash in base", one: "/a//", two: "b", expected: filepath.Join("/a//", "b")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, SmartJoin(tt.one, tt.two))
		})
	}
}
