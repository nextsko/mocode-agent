package ext

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapitalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty string", input: "", expected: ""},
		{name: "already capitalized", input: "Hello", expected: "Hello"},
		{name: "single lowercase word", input: "hello", expected: "Hello"},
		{name: "sentence", input: "hello world", expected: "Hello World"},
		{name: "all caps lowered", input: "HELLO WORLD", expected: "Hello World"},
		{name: "mixed case", input: "hello WORLD", expected: "Hello World"},
		{name: "leading and trailing spaces kept", input: "  spaced  out  ", expected: "  Spaced  Out  "},
		{name: "internal spaces not collapsed", input: "multiple   spaces", expected: "Multiple   Spaces"},
		{name: "chinese untouched", input: "中文测试", expected: "中文测试"},
		{name: "chinese then ascii word", input: "中文 abc", expected: "中文 Abc"},
		{name: "accented latin", input: "élan vital", expected: "Élan Vital"},
		{name: "underscore is not a word boundary", input: "a_b c-d", expected: "A_b C-D"},
		{name: "digits then letters", input: "123abc", expected: "123Abc"},
		{name: "whitespace only", input: "   ", expected: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, Capitalize(tt.input))
		})
	}
}

func TestNormalizeSpace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty string", input: "", expected: ""},
		{name: "whitespace only", input: " \t\r\n ", expected: ""},
		{name: "trim both ends", input: "  hello  ", expected: "hello"},
		{name: "tab becomes four spaces", input: "a\tb", expected: "a    b"},
		{name: "leading tabs trimmed", input: "\t\tx", expected: "x"},
		{name: "crlf becomes lf", input: "line1\r\nline2", expected: "line1\nline2"},
		{name: "crlf with surrounding blank lines trimmed", input: "\r\n\r\nx\r\n", expected: "x"},
		{name: "multiple blank lines trimmed", input: "\n\n x \n\n", expected: "x"},
		{name: "inner spaces preserved", input: "a  b", expected: "a  b"},
		{name: "inner newlines preserved", input: "a\n\nb", expected: "a\n\nb"},
		{name: "tab and crlf combined indentation trimmed", input: "\r\n\tcode\r\n", expected: "code"},
		{name: "tab expanded when not leading", input: "x\r\n\ty", expected: "x\n    y"},
		{name: "lone carriage return kept", input: "a\rb", expected: "a\rb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, NormalizeSpace(tt.input))
		})
	}
}

func TestIsValidBase64(t *testing.T) {
	t.Parallel()

	// Real PNG header encoded in standard base64.
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	pngBase64 := base64.StdEncoding.EncodeToString(pngHeader)

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "empty string", input: "", expected: false},
		{name: "valid no padding", input: "SGVsbG8gV29ybGQh", expected: true},
		{name: "valid with padding", input: "YQ==", expected: true},
		{name: "non-ASCII bytes", input: "abc\x80def", expected: false},
		{name: "ASCII but not base64", input: "hello world!!!", expected: false},
		{name: "raw encoding no padding", input: "YQ", expected: false},
		{name: "trailing whitespace", input: "YQ==\n", expected: false},
		{name: "valid PNG header base64", input: pngBase64, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, IsValidBase64(tt.input))
		})
	}
}
