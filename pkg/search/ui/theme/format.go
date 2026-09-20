package theme

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

// avgMonthDays is the 30.44-day month (365.25/12) used by FormatAge.
const avgMonthDays = 30.44

// avgYearDays is the 365.25-day year used by FormatAge.
const avgYearDays = 365.25

// FormatAge buckets an age in seconds into a short human string: "just
// now" (<60s), "Nm ago" (<1h), "Nh ago" (<1d), "Nd ago" (<7d), "Nw ago"
// (<30d), "Nmo ago" (<360d, 30.44-day months), else "Ny ago". Zero or
// negative ages return "" (no data), matching the TS formatAge.
func FormatAge(ageSeconds int64) string {
	if ageSeconds <= 0 {
		return ""
	}
	if ageSeconds < 60 {
		return "just now"
	}
	mins := ageSeconds / 60
	if mins < 60 {
		return strconv.FormatInt(mins, 10) + "m ago"
	}
	hours := mins / 60
	if hours < 24 {
		return strconv.FormatInt(hours, 10) + "h ago"
	}
	days := hours / 24
	if days < 7 {
		return strconv.FormatInt(days, 10) + "d ago"
	}
	if days < 30 {
		return strconv.FormatInt(days/7, 10) + "w ago"
	}
	if days < 360 {
		months := int(math.Round(float64(days) / avgMonthDays))
		return strconv.Itoa(months) + "mo ago"
	}
	years := int(math.Round(float64(days) / avgYearDays))
	return strconv.Itoa(years) + "y ago"
}

// FormatAgeOrDate prefers FormatAge and falls back to the raw
// publishedDate string (typically an ISO date) when the age is missing.
func FormatAgeOrDate(ageSeconds int64, publishedDate string) string {
	if s := FormatAge(ageSeconds); s != "" {
		return s
	}
	return publishedDate
}

// GetDomain extracts the host from a URL, stripping any "www." prefix and
// lowercasing it like the WHATWG URL hostname. Unparseable inputs (or
// inputs without a host) are returned unchanged.
func GetDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	host := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(host, "www.")
}

// pluralize pluralizes a label based on the count, mirroring the TS
// pluralize: 1 keeps the label; ch/sh/s/x/z endings add "es"; a
// consonant+y ending becomes "ies"; anything else adds "s".
func pluralize(label string, count int) string {
	if count == 1 {
		return label
	}
	lower := strings.ToLower(label)
	switch {
	case strings.HasSuffix(lower, "ch"),
		strings.HasSuffix(lower, "sh"),
		strings.HasSuffix(lower, "s"),
		strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "z"):
		return label + "es"
	case len(lower) >= 2 && !isVowel(lower[len(lower)-2]) &&
		strings.HasSuffix(lower, "y"):
		return label[:len(label)-1] + "ies"
	default:
		return label + "s"
	}
}

// isVowel reports whether the ASCII byte b is a vowel.
func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}
