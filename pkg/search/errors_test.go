package search

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyHTTPError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		status   int
		body     string
		wantCode string
		wantNil  bool
	}{
		{"billing body beats status", 500, "You have insufficient credits", ErrorCodeBilling, false},
		{"quota body beats status", 400, "quota exceeded for this key", ErrorCodeBilling, false},
		{"credits exhausted body", 200, "credits exhausted on account", ErrorCodeBilling, false},
		{"402 maps to billing", 402, "", ErrorCodeBilling, false},
		{"401 unauthorized", 401, "", ErrorCodeUnauthorized, false},
		{"403 forbidden", 403, "", ErrorCodeForbidden, false},
		{"429 rate limited", 429, "", ErrorCodeRateLimited, false},
		{"500 server error", 500, "", ErrorCodeServerError, false},
		{"503 server error", 503, "", ErrorCodeServerError, false},
		{"404 unmatched", 404, "", "", true},
		{"200 unmatched", 200, "", "", true},
		{"302 unmatched", 302, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyHTTPError(tc.status, tc.body)
			if tc.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.wantCode, got.Code)
			require.Equal(t, tc.status, got.HTTPStatus)
			require.NotEmpty(t, got.Message)
		})
	}
}

func TestSearchProviderErrorMessage(t *testing.T) {
	t.Parallel()
	withProvider := &SearchProviderError{Provider: "exa", Code: ErrorCodeBilling, Message: "credits exhausted"}
	require.Equal(t, "exa: credits exhausted", withProvider.Error())

	bare := &SearchProviderError{Code: ErrorCodeUnauthorized, Message: "401 unauthorized"}
	require.Equal(t, "401 unauthorized", bare.Error())
}

func TestFormatFailures(t *testing.T) {
	t.Parallel()
	failures := []Failure{
		{
			Provider: "exa",
			Err: &SearchProviderError{
				Provider: "exa", Code: ErrorCodeBilling,
				Message: "credits exhausted", HTTPStatus: 402,
			},
		},
		{
			Provider: "brave",
			Err: &SearchProviderError{
				Provider: "brave", Code: ErrorCodeUnauthorized,
				Message: "401 unauthorized", HTTPStatus: 401,
			},
		},
		{Provider: "kagi", Err: errors.New("boom")},
	}

	want := "exa: credits exhausted\n" +
		"brave: authorization failed (401). Check API key or base URL.\n" +
		"kagi: boom"
	require.Equal(t, want, formatFailures(failures))
}

func TestFormatFailuresEmpty(t *testing.T) {
	t.Parallel()
	require.Equal(t, "", formatFailures(nil))
	require.Equal(t, "", formatFailures([]Failure{}))
}

func TestFormatFailureSeesWrappedProviderError(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("provider call: %w", &SearchProviderError{
		Provider: "exa", Code: ErrorCodeForbidden,
		Message: "403 forbidden", HTTPStatus: 403,
	})

	line := formatFailure(Failure{Provider: "exa", Err: wrapped})

	require.Equal(t, "authorization failed (403). Check API key or base URL.", line)
}

func TestFormatFailureNilError(t *testing.T) {
	t.Parallel()
	require.Equal(t, "unknown error", formatFailure(Failure{Provider: "exa"}))
}
