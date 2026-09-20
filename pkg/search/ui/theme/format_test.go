package theme

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatAgeBuckets(t *testing.T) {
	t.Parallel()
	const day = 86400
	cases := []struct {
		name string
		in   int64
		want string
	}{
		{"zero", 0, ""},
		{"negative", -5, ""},
		{"30s", 30, "just now"},
		{"59s", 59, "just now"},
		{"2m", 120, "2m ago"},
		{"5h", 5 * 3600, "5h ago"},
		{"3d", 3 * day, "3d ago"},
		{"2w", 14 * day, "2w ago"},
		{"45d", 45 * day, "1mo ago"},
		{"5mo", 152 * day, "5mo ago"},
		{"2y", 730 * day, "2y ago"},
		{"exactly1m", 60, "1m ago"},
		{"exactly1h", 3600, "1h ago"},
		{"exactly1d", day, "1d ago"},
		{"exactly1w", 7 * day, "1w ago"},
		{"29d still weeks", 29 * day, "4w ago"},
		{"30d rounds to 1mo", 30 * day, "1mo ago"},
		{"359d max months", 359 * day, "12mo ago"},
		{"360d first year", 360 * day, "1y ago"},
		{"365d", 365 * day, "1y ago"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, FormatAge(tc.in), tc.name)
	}
}

func TestFormatAgeOrDateFallback(t *testing.T) {
	t.Parallel()
	require.Equal(t, "2m ago", FormatAgeOrDate(120, "2024-01-01"))
	require.Equal(t, "2024-01-01", FormatAgeOrDate(0, "2024-01-01"))
	require.Equal(t, "", FormatAgeOrDate(0, ""))
}

func TestGetDomain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"https://www.example.com/path?q=1", "example.com"},
		{"https://example.com", "example.com"},
		{"http://EXAMPLE.com:8080/x", "example.com"},
		{"https://sub.www.example.com/", "sub.www.example.com"},
		{"example.com", "example.com"},
		{"not a url", "not a url"},
		{"", ""},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, GetDomain(tc.in), tc.in)
	}
}

func TestPluralize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label string
		count int
		want  string
	}{
		{"source", 1, "source"},
		{"source", 3, "sources"},
		{"line", 2, "lines"},
		{"entry", 2, "entries"},
		{"city", 3, "cities"},
		{"day", 2, "days"},
		{"box", 2, "boxes"},
		{"match", 2, "matches"},
		{"Name", 2, "Names"}, // case preserved, suffix rule case-insensitive
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, pluralize(tc.label, tc.count), tc.label)
	}
}
