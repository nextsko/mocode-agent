// This file holds the default SafetyGate and its pattern sets.
//
// The patterns are adapted from trpc-agent-go's defaultSafetyGate. They are
// deliberately high-precision: false positives translate directly into
// rejected candidates, so each pattern is something genuinely unusual to
// find in a legitimate patch body that will be injected into an agent
// system prompt.
package gates

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// DefaultSafetyGate scans Title, Description, and Body for secrets,
// dangerous shell markers, and path-traversal patterns.
type DefaultSafetyGate struct{}

// NewDefaultSafetyGate returns the built-in SafetyGate.
func NewDefaultSafetyGate() SafetyGate { return &DefaultSafetyGate{} }

// Scan implements SafetyGate.
func (g *DefaultSafetyGate) Scan(_ context.Context, c *Candidate) (*Report, error) {
	if c == nil {
		return &Report{Gate: "safety", Passed: true}, nil
	}
	body := strings.Join([]string{c.Title, c.Description, c.Body}, "\n")
	reasons := make([]string, 0, 4)
	if name, ok := containsSecret(body); ok {
		reasons = append(reasons, fmt.Sprintf("suspected secret pattern: %s", name))
	}
	if name, ok := containsDangerousShell(body); ok {
		reasons = append(reasons, fmt.Sprintf("dangerous shell pattern: %s", name))
	}
	if name, ok := containsPathTraversal(body); ok {
		reasons = append(reasons, fmt.Sprintf("path traversal pattern: %s", name))
	}
	return &Report{Gate: "safety", Passed: len(reasons) == 0, Reasons: reasons}, nil
}

type namedPattern struct {
	name string
	re   *regexp.Regexp
}

// secretPatterns detect high-confidence credential shapes.
var secretPatterns = []namedPattern{
	{"aws_access_key_id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"aws_secret_key", regexp.MustCompile(`(?i)aws[_\s-]?secret[_\s-]?access[_\s-]?key\s*[:=]\s*[A-Za-z0-9/+=]{30,}`)},
	{"generic_api_key", regexp.MustCompile(`(?i)\b(api[_-]?key|secret[_-]?key|access[_-]?token|bearer)\s*[:=]\s*['"]?[A-Za-z0-9_\-]{20,}['"]?`)},
	{"private_key_block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"github_token", regexp.MustCompile(`\bghp_[A-Za-z0-9]{36}\b`)},
	{"openai_key", regexp.MustCompile(`\bsk-[A-Za-z0-9]{32,}\b`)},
}

// dangerousShellPatterns detect destructive recipes that should never appear
// in a skill body the agent is told to follow.
var dangerousShellPatterns = []namedPattern{
	// rm -rf against the filesystem root is always destructive. We split this
	// into two precise patterns rather than one broad one: a bare `rm -rf /`
	// (with nothing after the slash) and `rm -rf /<system-dir>` (usr, etc, bin,
	// ...). This avoids false-positives on legitimate cleanups like
	// `rm -rf /tmp/build` or `rm -rf /home/user/dist`, which the broad pattern
	// incorrectly caught. Adapted from trpc-agent-go, refined for precision.
	{"rm_rf_root", regexp.MustCompile(`(?i)\brm\s+-rf\s+/(?:\s|$)`)},
	{"rm_rf_sysdir", regexp.MustCompile(`(?i)\brm\s+-rf\s+/(?:usr|etc|bin|sbin|var|root|lib|boot|proc|sys|dev)(?:\b|/)`)},
	{"rm_rf_home", regexp.MustCompile(`(?i)\brm\s+-rf\s+\$HOME\b`)},
	{"dd_raw_disk", regexp.MustCompile(`(?i)\bdd\s+[^\n]*of=/dev/[sh]d`)},
	{"curl_pipe_shell", regexp.MustCompile(`(?i)curl\s+[^\n]*\|\s*(sh|bash|zsh)\b`)},
	{"wget_pipe_shell", regexp.MustCompile(`(?i)wget\s+[^\n]*\|\s*(sh|bash|zsh)\b`)},
	{"fork_bomb", regexp.MustCompile(`:\(\)\{\s*:\|:&\s*\};:`)},
}

// pathTraversalPatterns detect filesystem navigation that is almost always
// wrong or malicious in a persisted prompt patch.
var pathTraversalPatterns = []namedPattern{
	{"double_dot_write", regexp.MustCompile(`(?i)(write|save|copy|mv|cp)[^.\n]{0,40}\.\./\.\.`)},
	{"etc_passwd", regexp.MustCompile(`/etc/passwd`)},
	{"etc_shadow", regexp.MustCompile(`/etc/shadow`)},
	{"ssh_private", regexp.MustCompile(`\.ssh/id_(rsa|ed25519|ecdsa)`)},
}

func containsSecret(body string) (string, bool) {
	for _, p := range secretPatterns {
		if p.re.MatchString(body) {
			return p.name, true
		}
	}
	return "", false
}

func containsDangerousShell(body string) (string, bool) {
	for _, p := range dangerousShellPatterns {
		if p.re.MatchString(body) {
			return p.name, true
		}
	}
	return "", false
}

func containsPathTraversal(body string) (string, bool) {
	for _, p := range pathTraversalPatterns {
		if p.re.MatchString(body) {
			return p.name, true
		}
	}
	return "", false
}
