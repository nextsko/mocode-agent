package shell

import (
	"os"
	"strconv"
	"sync/atomic"

	"github.com/nextsko/mocode-agent/internal/util/pubsub"
)

// BackgroundTasksDisabled reports whether background jobs are globally
// disabled via MO_CODE_DISABLE_BACKGROUND_TASKS (kill switch, same semantics
// as Claude Code's CLAUDE_CODE_DISABLE_BACKGROUND_TASKS).
func BackgroundTasksDisabled() bool {
	v, _ := strconv.ParseBool(os.Getenv("MO_CODE_DISABLE_BACKGROUND_TASKS"))
	return v
}

// sanitizeBackgroundEnv forces pagers and color output off for non-TTY
// background jobs (borrowed from Codex's UNIFIED_EXEC_ENV): the captured
// stream is only ever read by the agent, and an interactive pager would hang
// the job forever. TTY jobs are exempt — fidelity is the point of a PTY.
func sanitizeBackgroundEnv(shell *Shell) {
	shell.SetEnv("NO_COLOR", "1")
	shell.SetEnv("TERM", "dumb")
	shell.SetEnv("PAGER", "cat")
	shell.SetEnv("GIT_PAGER", "cat")
	shell.SetEnv("GH_PAGER", "cat")
}

// maxPendingNotifications bounds the push-notification backlog so a flood of
// finishing jobs cannot grow memory without limit; oldest entries drop first.
const maxPendingNotifications = 64

// notifyTerminal records the job's terminal snapshot and fires the
// completion hooks. Runs on the job's runner goroutine before the done
// channel closes, so by the time Wait/IsDone report completion the
// notification is already queued.
func (m *BackgroundShellManager) notifyTerminal(bs *BackgroundShell) {
	st := bs.Status()
	m.pendingMu.Lock()
	if len(m.pending) >= maxPendingNotifications {
		m.pending = m.pending[1:]
	}
	m.pending = append(m.pending, st)
	m.pendingMu.Unlock()

	if bs.onComplete != nil {
		bs.onComplete(bs)
	}
	if fn := m.onJobComplete.Load(); fn != nil {
		(*fn)(bs)
	}
}

// DrainCompletedNotifications returns terminal job snapshots recorded since
// the previous drain, filtered by sessionID when non-empty, and clears the
// matching entries. It is the pull side of the push model: the agent layer
// can fold these into its next turn so the model learns jobs finished
// without polling job_output.
func (m *BackgroundShellManager) DrainCompletedNotifications(sessionID string) []JobStatus {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	var out []JobStatus
	keep := m.pending[:0]
	for _, st := range m.pending {
		if sessionID == "" || st.SessionID == sessionID {
			out = append(out, st)
		} else {
			keep = append(keep, st)
		}
	}
	m.pending = keep
	return out
}

// --- promote: move the synchronously-waited bash to the background ---

var promoteCh atomic.Pointer[chan struct{}]

// PromotePendingBash fires the one-shot promote signal (Ctrl+B in the TUI):
// every bash tool call currently blocked in its synchronous wait window
// hands its job back as a background ID instead of waiting for the timeout.
// Claude Code's Ctrl+B semantics; tmux users press it twice.
func PromotePendingBash() {
	ch := make(chan struct{})
	close(ch)
	promoteCh.Store(&ch)
}

// PromoteSignal returns the current promote channel (nil when never fired —
// a nil channel in select disables the case, so callers can select blindly).
func PromoteSignal() <-chan struct{} {
	if p := promoteCh.Load(); p != nil {
		return *p
	}
	return nil
}

// SetOnJobComplete installs a manager-wide callback fired exactly once for
// every job that reaches a terminal state. Call it once during app startup.
func (m *BackgroundShellManager) SetOnJobComplete(fn func(*BackgroundShell)) {
	m.onJobComplete.Store(&fn)
}

// SetOutputDir enables output persistence: every job's stdout/stderr is
// tee'd to <dir>/<unixts>-<id>.{out,err}. Call it once during app startup.
func (m *BackgroundShellManager) SetOutputDir(dir string) {
	m.outputDir.Store(&dir)
}

// SetOutputKillBytes overrides the per-stream total-output kill limit
// (test hook; 0 keeps the production default).
func (m *BackgroundShellManager) SetOutputKillBytes(n int64) {
	m.outputKillBytes.Store(n)
}

var _ = pubsub.CreatedEvent // pubsub reserved for future notify topics
