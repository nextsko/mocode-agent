package query

import (
	"regexp"
	"strings"
)

// directiveField is the canonical constraint field a directive maps to.
type directiveField string

// Canonical directive target fields.
const (
	fieldSite     directiveField = "site"
	fieldInURL    directiveField = "inurl"
	fieldInTitle  directiveField = "intitle"
	fieldInText   directiveField = "intext"
	fieldFiletype directiveField = "filetype"
	fieldBefore   directiveField = "before"
	fieldAfter    directiveField = "after"
	fieldLang     directiveField = "lang"
)

// directivePattern matches an optional +/- sign, a directive name, and a
// colon-separated value (name:value).
var directivePattern = regexp.MustCompile(`(?i)^([+-]?)([a-z][a-z-]*):(.*)$`)

// DIRECTIVE_FIELDS maps directive names to their canonical field.
var DIRECTIVE_FIELDS = map[string]directiveField{
	"site":     fieldSite,
	"domain":   fieldSite,
	"host":     fieldSite,
	"inurl":    fieldInURL,
	"url":      fieldInURL,
	"intitle":  fieldInTitle,
	"title":    fieldInTitle,
	"intext":   fieldInText,
	"inbody":   fieldInText,
	"inanchor": fieldInText,
	"filetype": fieldFiletype,
	"ext":      fieldFiletype,
	"before":   fieldBefore,
	"until":    fieldBefore,
	"after":    fieldAfter,
	"since":    fieldAfter,
	"lang":     fieldLang,
	"language": fieldLang,
}

// ALL_MODES lists the allin*: directives that capture every following
// plain term into their field.
var ALL_MODES = map[string]directiveField{
	"allintitle": fieldInTitle,
	"allinurl":   fieldInURL,
	"allintext":  fieldInText,
}

// yearFirstDatePattern matches YYYY, YYYY-MM, and YYYY-MM-DD dates with
// -, /, or . separators.
var yearFirstDatePattern = regexp.MustCompile(
	`^(\d{4})(?:[-/.](\d{1,2})(?:[-/.](\d{1,2}))?)?$`)

// yearLastDatePattern matches MM/DD/YYYY dates (also - and . separators);
// the first field is treated as the day when it exceeds 12.
var yearLastDatePattern = regexp.MustCompile(
	`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$`)

// relativeAgePattern matches relative publish dates such as "2 days ago".
var relativeAgePattern = regexp.MustCompile(
	`(?i)^(\d+)\s*(minute|min|hour|hr|day|week|month|mo|year|yr|[mhdwy])s?\s+ago$`)

// relativeUnitSeconds maps relative-age unit names to their duration in
// seconds.
var relativeUnitSeconds = map[string]int64{
	"m":      60,
	"min":    60,
	"minute": 60,
	"h":      3600,
	"hr":     3600,
	"hour":   3600,
	"d":      86400,
	"day":    86400,
	"w":      604800,
	"week":   604800,
	"mo":     2592000,
	"month":  2592000,
	"y":      31536000,
	"yr":     31536000,
	"year":   31536000,
}

// siteSchemePattern strips a leading URL scheme from a site value.
var siteSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://`)

// normalizeSite lowercases a site: value and strips the scheme, a *.
// wildcard prefix, and any trailing slash or dot.
func normalizeSite(value string) string {
	site := strings.ToLower(strings.TrimSpace(value))
	site = siteSchemePattern.ReplaceAllString(site, "")
	site = strings.TrimPrefix(site, "*.")
	site = strings.TrimRight(site, "/.")
	return site
}
