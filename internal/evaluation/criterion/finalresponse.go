package criterion

import (
	"regexp"
	"strings"
)

// Score evaluates the FinalResponse criterion against actualText and returns a
// normalized score in [0, 1].
//
// Scoring by Mode:
//   - MatchExact:    1.0 on exact equality, else 0.0.
//   - MatchContains: 1.0 if Expected is a substring, else 0.0.
//   - MatchRegex:    1.0 if actualText matches the Expected pattern, else 0.0.
//     A compile error on the pattern is returned as an error (score 0.0).
func (f *FinalResponse) Score(actualText string) (float64, string, error) {
	if f == nil {
		return 1.0, "final-response: criterion disabled", nil
	}
	actual, expected := actualText, f.Expected
	if f.CaseInsensitive {
		actual = strings.ToLower(actual)
		expected = strings.ToLower(expected)
	}
	switch f.Mode {
	case MatchExact:
		if actual == expected {
			return 1.0, "final-response(exact): matched", nil
		}
		return 0.0, "final-response(exact): text did not match", nil
	case MatchContains:
		if expected == "" || strings.Contains(actual, expected) {
			return 1.0, "final-response(contains): substring present", nil
		}
		return 0.0, "final-response(contains): substring absent", nil
	case MatchRegex:
		re, err := regexp.Compile(expected)
		if err != nil {
			return 0.0, "final-response(regex): invalid pattern", err
		}
		if re.MatchString(actual) {
			return 1.0, "final-response(regex): pattern matched", nil
		}
		return 0.0, "final-response(regex): pattern did not match", nil
	default:
		return 1.0, "final-response: unknown mode, skipped", nil
	}
}
