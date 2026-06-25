package sshcommon

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
	"github.com/package-register/mocode/internal/agent/toolutil"
	"github.com/package-register/mocode/internal/permission"
)

// DescribeFailure converts a transport error into a stable, user-friendly message.
func DescribeFailure(action, host string, err error) string {
	return fmt.Sprintf("ssh %s on %s failed: %s", action, host, err.Error())
}

// MustSessionID fails the tool if the session ID is missing from context.
func MustSessionID(ctx context.Context) (string, error) {
	id := toolutil.GetSessionFromContext(ctx)
	if id == "" {
		return "", fmt.Errorf("session ID is required for ssh operations")
	}
	return id, nil
}

// requestPermission is a thin wrapper around permission.Service.Request
// that keeps the tool call sites short.  Returns (granted, err) exactly
// like permission.Service.Request does.  If `description` is non-empty
// it overrides the caller's default description (useful when the
// caller wants a polished human-readable sentence).
func RequestPermission(
	ctx context.Context,
	svc permission.Service,
	sessionID, toolName, action, host, description string,
	call fantasy.ToolCall,
	params any,
) (bool, error) {
	if svc == nil {
		// No permission service wired up: allow (matches the bash tool
		// behaviour when permissions are not configured).
		return true, nil
	}
	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s %s on %s", toolName, action, host)
	}
	granted, err := svc.Request(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		Path:        host,
		ToolCallID:  call.ID,
		ToolName:    toolName,
		Action:      action,
		Description: desc,
		Params:      params,
	})
	if err != nil {
		return false, err
	}
	return granted, nil
}

// bannedCommandPrefixes is the conservative set of remote commands that
// are dangerous enough to reject at the tool layer.  Pattern matching
// is intentionally coarse — false positives are cheap, false negatives
// are not.
var bannedCommandPrefixes = []string{
	"rm -rf /",
	"rm -fr /",
	":(){:|:&};:",
	"mkfs.",
	"dd if=/dev/urandom of=/dev/sd",
	"shutdown",
	"reboot",
	"halt",
	"poweroff",
	"init 0",
	"init 6",
}

// isCommandBanned returns a non-empty reason if the command is on the
// deny list, "" if it passes.
func IsCommandBanned(cmd string) string {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, bad := range bannedCommandPrefixes {
		if strings.HasPrefix(lower, bad) {
			return bad
		}
	}
	return ""
}

// truncateForPrompt is used when building the human-readable description
// in a permission request — we want a one-liner.
func TruncateForPrompt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// truncateForDisplay caps stdout/stderr in tool responses to keep the
// LLM context manageable.  30 KB mirrors builtin/exec/bash.go.
const maxDisplayBytes = 30 * 1024

func TruncateForDisplay(s string) string {
	if len(s) <= maxDisplayBytes {
		return s
	}
	half := maxDisplayBytes / 2
	return s[:half] + "\n…[truncated]…\n" + s[len(s)-half:]
}

// isPathSafe performs a basic jail-break check.  The remote end is
// already its own sandbox; this helper exists to catch the obvious
// ".." escapes in *upload* and *download* paths so the LLM cannot
// accidentally request `ssh_upload` with `../../etc/passwd`.
func IsPathSafe(p string) bool {
	if p == "" {
		return false
	}
	// Normalise the path.  We accept both POSIX and Windows separators
	// because remote hosts vary.
	cleaned := strings.ReplaceAll(p, "\\", "/")
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}
