// Package gitea provides tools that delegate to the tea CLI for Gitea operations.
package giteacommon

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"

	"github.com/nextsko/mocode-agent/internal/util/log"
)

// getTea is a one-time lookup for the tea binary. Returns empty string if not
// found in $PATH.
var getTea = sync.OnceValue(func() string {
	path, err := exec.LookPath("tea")
	if err != nil {
		if log.Initialized() {
			slog.Warn("tea (Gitea CLI) not found in $PATH. Gitea tools will be unavailable.")
		}
		return ""
	}
	return path
})

// teaCmd builds an *exec.Cmd for the tea binary with the given arguments.
// Returns nil if tea is not available.
func teaCmd(ctx context.Context, args ...string) *exec.Cmd {
	name := getTea()
	if name == "" {
		return nil
	}
	return exec.CommandContext(ctx, name, args...)
}

// GetTea returns the path to the tea binary, or empty if unavailable.
func GetTea() string {
	return getTea()
}

// TeaCmd builds an *exec.Cmd for the tea binary with the given arguments.
func TeaCmd(ctx context.Context, args ...string) *exec.Cmd {
	return teaCmd(ctx, args...)
}

// ErrTeaNotFound is the standard message returned when tea is absent.
const ErrTeaNotFound = "tea CLI not found in $PATH. Install from https://gitea.com/gitea/tea"
