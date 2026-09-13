// Package gitea provides tools that delegate to the tea CLI for Gitea operations.
package giteacommon

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/nextsko/mocode-agent/internal/util/log"
)

// teaRetryAfter bounds how long a failed `tea` lookup is remembered. The old
// sync.OnceValue cache made "tea missing" permanent for the process lifetime:
// installing tea mid-session still left Gitea tools dead until restart.
const teaRetryAfter = 5 * time.Minute

var (
	teaMu      sync.Mutex
	teaPath    string    // resolved path, non-empty once found (cached forever)
	teaMissAt  time.Time // when the last failed lookup happened (zero = never tried)
	teaChecked bool      // whether at least one lookup has run
)

// getTea resolves the tea binary path. Positive results are cached for the
// process lifetime; negative results expire after teaRetryAfter so a
// mid-session install is picked up.
func getTea() string {
	teaMu.Lock()
	defer teaMu.Unlock()
	if teaPath != "" {
		return teaPath
	}
	if teaChecked && time.Since(teaMissAt) < teaRetryAfter {
		return ""
	}
	teaChecked = true
	path, err := exec.LookPath("tea")
	if err != nil {
		teaMissAt = time.Now()
		if log.Initialized() {
			slog.Debug("tea (Gitea CLI) not found in $PATH; will retry later.", "retry_after", teaRetryAfter.String())
		}
		return ""
	}
	teaPath = path
	return path
}

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
