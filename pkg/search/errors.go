package search

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Error codes classifying SearchProviderError failures.
const (
	// ErrorCodeBilling marks exhausted credits, quota, or funds.
	ErrorCodeBilling = "billing"

	// ErrorCodeUnauthorized marks a missing or invalid credential (401).
	ErrorCodeUnauthorized = "unauthorized"

	// ErrorCodeForbidden marks rejected access (403).
	ErrorCodeForbidden = "forbidden"

	// ErrorCodeRateLimited marks throttling (429).
	ErrorCodeRateLimited = "rate_limited"

	// ErrorCodeServerError marks upstream 5xx failures.
	ErrorCodeServerError = "server_error"
)

// SearchProviderError describes a provider failure with its classification
// and the originating HTTP status when one exists.
type SearchProviderError struct {
	// Provider is the failing provider ID.
	Provider string `json:"provider"`

	// Code is one of the ErrorCode* classification constants.
	Code string `json:"code"`

	// Message is the human-readable failure description.
	Message string `json:"message"`

	// HTTPStatus is the upstream status code, or 0 for non-HTTP errors.
	HTTPStatus int `json:"http_status"`
}

// Error implements the error interface.
func (e *SearchProviderError) Error() string {
	if e.Provider == "" {
		return e.Message
	}
	return e.Provider + ": " + e.Message
}

// creditBodyPattern matches billing-exhaustion signals in upstream bodies.
var creditBodyPattern = regexp.MustCompile(`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

// classifyHTTPError maps an HTTP status and response body onto a classified
// provider error. Body-level billing signals win over status codes; 402 also
// maps to billing. It returns nil when nothing matches, leaving the caller
// free to raise a provider-specific error.
func classifyHTTPError(status int, body string) *SearchProviderError {
	if creditBodyPattern.MatchString(body) {
		return &SearchProviderError{
			Code:       ErrorCodeBilling,
			Message:    "credits exhausted",
			HTTPStatus: status,
		}
	}
	switch {
	case status == 402:
		return &SearchProviderError{
			Code:       ErrorCodeBilling,
			Message:    "402 credits exhausted",
			HTTPStatus: status,
		}
	case status == 401:
		return &SearchProviderError{
			Code:       ErrorCodeUnauthorized,
			Message:    "401 unauthorized",
			HTTPStatus: status,
		}
	case status == 403:
		return &SearchProviderError{
			Code:       ErrorCodeForbidden,
			Message:    "403 forbidden",
			HTTPStatus: status,
		}
	case status == 429:
		return &SearchProviderError{
			Code:       ErrorCodeRateLimited,
			Message:    "429 rate limited",
			HTTPStatus: status,
		}
	case status >= 500:
		return &SearchProviderError{
			Code:       ErrorCodeServerError,
			Message:    fmt.Sprintf("%d server error", status),
			HTTPStatus: status,
		}
	}
	return nil
}

// Failure records one provider's contribution to an aggregate chain error.
type Failure struct {
	// Provider is the failing provider ID.
	Provider string

	// Err is the underlying error.
	Err error
}

// formatFailure renders the message portion of one failure line,
// humanizing authorization errors for end users.
func formatFailure(f Failure) string {
	var spe *SearchProviderError
	if errors.As(f.Err, &spe) {
		if spe.HTTPStatus == 401 || spe.HTTPStatus == 403 {
			return fmt.Sprintf(
				"authorization failed (%d). Check API key or base URL.",
				spe.HTTPStatus,
			)
		}
		return spe.Message
	}
	if f.Err != nil {
		return f.Err.Error()
	}
	return "unknown error"
}

// formatFailures aggregates failures into one multi-line "provider: error"
// report, one line per failed provider.
func formatFailures(failures []Failure) string {
	lines := make([]string, 0, len(failures))
	for _, f := range failures {
		lines = append(lines, fmt.Sprintf("%s: %s", f.Provider, formatFailure(f)))
	}
	return strings.Join(lines, "\n")
}
