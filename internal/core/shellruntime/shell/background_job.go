package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BackgroundShell represents a shell running in the background.
type BackgroundShell struct {
	ID          string
	Command     string
	Description string
	Shell       *Shell
	WorkingDir  string
	TTY         bool
	// SessionID is the agent session that started this job ("" = unowned).
	SessionID  string
	ctx        context.Context
	cancel     context.CancelFunc
	stdout     *syncBuffer
	stderr     *syncBuffer
	done       chan struct{}
	exitErr    error
	// runner is assigned by the start goroutine (TTY path) and read by
	// WriteInput/Kill/Status from other goroutines — every access must go
	// through setRunner/runnerRef (the old bare field access was a data race).
	runner   backgroundRunner
	runnerMu sync.Mutex
	// completedAt/completed-at bookkeeping for retention and status.
	completedAt atomic.Int64 // Unix timestamp when job completed (0 if still running)
	startedAt   atomic.Int64 // Unix timestamp when job started (0 if never started)
	exitCode    atomic.Int32 // captured exit code (0 before completion)
	// stdinWriter feeds the non-TTY interpreter's stdin pipe; nil for TTY jobs
	// where input flows through the runner (the PTY master).
	stdinWriter io.Writer
	// stdinReader is the interpreter-facing end of the stdin pipe; closed when
	// the job finishes to signal EOF.
	stdinReader *os.File
	stdinMu     sync.Mutex
	// onComplete is the per-job terminal hook from BackgroundShellOptions.
	onComplete func(*BackgroundShell)
	// outPath/errPath are the optional on-disk audit copies of the output
	// streams; empty when persistence is off or failed.
	outPath string
	errPath string
}

// BackgroundShellOptions struct is the per-job configuration for Start.
type BackgroundShellOptions struct {
	TTY bool
	// SessionID records which agent session started the job, so the job_*
	// tools can refuse cross-session access (borrowed from gemini-cli's
	// per-session ownership model). Empty keeps the job accessible from any
	// session (legacy callers, tests).
	SessionID string
	// OnComplete, when non-nil, is invoked exactly once after the job
	// reaches a terminal state — the hook that lets the app layer push
	// "background job finished" notifications instead of the model polling
	// job_output. It runs on the job's runner goroutine, before the done
	// channel closes, and must not block.
	OnComplete func(*BackgroundShell)
}

type backgroundRunner interface {
	Wait() error
	Terminate(force bool) error
	// WriteStdin sends bytes to the process stdin. For TTY jobs this writes to
	// the PTY master. Implementations that cannot accept input return an error.
	WriteStdin(p []byte) (int, error)
}

func (bs *BackgroundShell) setRunner(r backgroundRunner) {
	bs.runnerMu.Lock()
	bs.runner = r
	bs.runnerMu.Unlock()
}

func (bs *BackgroundShell) runnerRef() backgroundRunner {
	bs.runnerMu.Lock()
	defer bs.runnerMu.Unlock()
	return bs.runner
}

// JobState is the lifecycle state of a background job.
type JobState string

const (
	// JobStateRunning means the command has not finished yet.
	JobStateRunning JobState = "running"
	// JobStateCompleted means the command exited successfully (exit code 0).
	JobStateCompleted JobState = "completed"
	// JobStateFailed means the command exited with a non-zero code or errored.
	JobStateFailed JobState = "failed"
	// JobStateKilled means the command was cancelled/interrupted.
	JobStateKilled JobState = "killed"
)

// JobStatus is a structured, copy-free snapshot of a background job's state.
// It is surfaced to the agent so it can observe task progress and decide
// whether to send more input, wait, or kill the job.
type JobStatus struct {
	ID          string   `json:"id"`
	State       JobState `json:"state"`
	Command     string   `json:"command"`
	Description string   `json:"description,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	TTY         bool     `json:"tty,omitempty"`
	Interactive bool     `json:"interactive,omitempty"`
	StartedAtMs int64    `json:"started_at_ms"`
	ElapsedMs   int64    `json:"elapsed_ms"`
	Done        bool     `json:"done"`
	ExitCode    int      `json:"exit_code,omitempty"`
	StdoutBytes int      `json:"stdout_bytes"`
	StderrBytes int      `json:"stderr_bytes"`
	// IdleMs is milliseconds since the last output byte (stdout or stderr).
	// 0 for finished jobs. Together with State it lets the agent tell
	// "slow but working" apart from "stalled, likely waiting for input".
	IdleMs int64 `json:"idle_ms,omitempty"`
	// LikelyPrompting is true when the job is running, has been quiet for
	// a beat, and the tail of its output looks like an interactive prompt
	// ("[y/N]", "Password:", a trailing "?" or ":", ...). This is the signal
	// that unblocks the classic TTY hang: answer it with job_input.
	LikelyPrompting bool `json:"likely_prompting,omitempty"`
	// OutputCapped is true when the job was cancelled because one of its
	// streams produced more than the total-output kill limit.
	OutputCapped bool `json:"output_capped,omitempty"`
}

// BackgroundShellInfo contains information about a background shell.
type BackgroundShellInfo struct {
	ID          string
	Command     string
	Description string
}

func closeStdinPipe(r *os.File, w io.Writer) {
	if cw, ok := w.(io.Closer); ok {
		_ = cw.Close()
	}
	if r != nil {
		_ = r.Close()
	}
}

// stdinRunnerWriter adapts a backgroundRunner to io.Writer so the bounded
// write helper can treat the pipe path and the TTY path uniformly.
type stdinRunnerWriter struct{ r backgroundRunner }

func (w stdinRunnerWriter) Write(p []byte) (int, error) { return w.r.WriteStdin(p) }

// promptIdleThreshold is how long a running job must stay quiet before its
// output tail is checked against the prompt heuristics.
const promptIdleThreshold = 2 * time.Second

// looksPrompting inspects the tail of a job's combined output for shapes
// that commonly mean "an interactive program is blocked waiting for the user".
// Conservative by design: progress bars and log lines must not match.
func looksPrompting(tail string) bool {
	if tail == "" {
		return false
	}
	// Only the last few lines matter.
	lines := strings.Split(tail, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			last = lines[i]
			break
		}
	}
	low := strings.ToLower(strings.TrimSpace(last))
	if low == "" {
		return false
	}
	switch {
	case strings.HasSuffix(low, "?"),
		strings.HasSuffix(low, ":"),
		strings.HasSuffix(low, ">"),
		strings.Contains(low, "y/n"),
		strings.Contains(low, "yes/no"),
		strings.Contains(low, "[y"),
		strings.Contains(low, "password"),
		strings.Contains(low, "passphrase"),
		strings.Contains(low, "press any key"),
		strings.Contains(low, "press enter"),
		strings.Contains(low, "do you want"),
		strings.Contains(low, "proceed?"),
		strings.Contains(low, "continue?"):
		return true
	}
	return false
}

// GetOutput returns the current output of a background shell.
func (bs *BackgroundShell) GetOutput() (stdout string, stderr string, done bool, err error) {
	select {
	case <-bs.done:
		return bs.stdout.String(), bs.stderr.String(), true, bs.exitErr
	default:
		return bs.stdout.String(), bs.stderr.String(), false, nil
	}
}

// GetTailOutput is the incremental-friendly variant of GetOutput: it returns
// at most the last maxStdout/maxStderr bytes of each stream (line-aligned),
// so polling a chatty long-running job no longer ships its whole log history
// into the model context on every job_output call.
func (bs *BackgroundShell) GetTailOutput(maxStdout, maxStderr int) (stdout string, stderr string, truncated bool, done bool, err error) {
	truncated = bs.stdout.Len() > maxStdout || bs.stderr.Len() > maxStderr
	select {
	case <-bs.done:
		return bs.stdout.Tail(maxStdout), bs.stderr.Tail(maxStderr), truncated, true, bs.exitErr
	default:
		return bs.stdout.Tail(maxStdout), bs.stderr.Tail(maxStderr), truncated, false, nil
	}
}

// WriteInput sends bytes to the running command's stdin (pipe for non-TTY
// jobs, PTY master for TTY jobs). The write is bounded in size and duration so
// a process that never reads stdin cannot block the agent indefinitely.
func (bs *BackgroundShell) WriteInput(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	const maxInput = 1 << 16 // 64 KiB
	if len(p) > maxInput {
		p = p[:maxInput]
	}

	bs.stdinMu.Lock()
	defer bs.stdinMu.Unlock()

	var w io.Writer
	switch {
	case bs.stdinWriter != nil:
		w = bs.stdinWriter
	case bs.runnerRef() != nil:
		w = stdinRunnerWriter{bs.runnerRef()}
	default:
		return 0, fmt.Errorf("background shell %s does not accept stdin input", bs.ID)
	}

	type writeResult struct {
		n   int
		err error
	}
	done := make(chan writeResult, 1)
	go func() {
		n, err := w.Write(p)
		done <- writeResult{n, err}
	}()

	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(5 * time.Second):
		// Buffer full and the command is not draining it. The deferred close
		// in the runner goroutine unblocks this writer when the job ends.
		return 0, fmt.Errorf("timed out writing to background shell %s (input buffer full or command not reading stdin)", bs.ID)
	}
}

// Status returns a structured snapshot of the job's lifecycle state.
func (bs *BackgroundShell) Status() JobStatus {
	// A job is terminal when its done channel closed OR its runner already
	// recorded a completion timestamp — the latter covers completion hooks,
	// which run just before the channel closes (see notifyTerminal).
	done := bs.IsDone() || bs.completedAt.Load() > 0
	exitCode := int(bs.exitCode.Load())

	state := JobStateRunning
	if done {
		switch {
		case IsInterrupt(bs.exitErr):
			state = JobStateKilled
		case exitCode == 0 && bs.exitErr == nil:
			state = JobStateCompleted
		default:
			// Non-zero exit code, or a non-exit error (parse/permission/TTY).
			state = JobStateFailed
		}
	}

	startED := bs.startedAt.Load()
	end := bs.completedAt.Load()
	if end == 0 && startED > 0 {
		end = time.Now().Unix()
	}
	var elapsedMs int64
	if startED > 0 && end >= startED {
		elapsedMs = (end - startED) * 1000
	}

	// Output activity: how long since the last byte arrived. For running jobs
	// a long idle combined with a prompt-looking tail is the classic "blocked
	// waiting for user input" signature the agent needs to see.
	idleMs := int64(0)
	likelyPrompting := false
	if !done {
		stdoutIdle := bs.stdout.IdleMs()
		stderrIdle := bs.stderr.IdleMs()
		idleMs = max(stdoutIdle, stderrIdle)
		if idleMs >= promptIdleThreshold.Milliseconds() {
			tail := bs.stdout.Tail(512)
			if t := bs.stderr.Tail(512); t != "" {
				tail += "\n" + t
			}
			likelyPrompting = looksPrompting(tail)
		}
	}

	return JobStatus{
		ID:              bs.ID,
		State:           state,
		Command:         bs.Command,
		Description:     bs.Description,
		WorkingDir:      bs.WorkingDir,
		SessionID:       bs.SessionID,
		TTY:             bs.TTY,
		Interactive:     bs.stdinWriter != nil || bs.runnerRef() != nil,
		StartedAtMs:     startED * 1000,
		ElapsedMs:       elapsedMs,
		Done:            done,
		ExitCode:        exitCode,
		StdoutBytes:     bs.stdout.Len(),
		StderrBytes:     bs.stderr.Len(),
		IdleMs:          idleMs,
		LikelyPrompting: likelyPrompting,
		OutputCapped:    bs.stdout.OutputCapped() || bs.stderr.OutputCapped(),
	}
}

// IsDone checks if the background shell has finished execution.
func (bs *BackgroundShell) IsDone() bool {
	select {
	case <-bs.done:
		return true
	default:
		return false
	}
}

// Done returns a channel closed on job completion so callers can wait
// event-driven (select) instead of polling IsDone on a ticker.
func (bs *BackgroundShell) Done() <-chan struct{} {
	return bs.done
}

// BelongsTo reports whether the job may be accessed from the given session.
// Jobs started without a session ID (legacy callers, tests) stay accessible
// to every session.
func (bs *BackgroundShell) BelongsTo(sessionID string) bool {
	return bs.SessionID == "" || bs.SessionID == sessionID
}

// Wait blocks until the background shell completes.
func (bs *BackgroundShell) Wait() {
	<-bs.done
}

func (bs *BackgroundShell) WaitContext(ctx context.Context) bool {
	select {
	case <-bs.done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (bs *BackgroundShell) waitFor(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-bs.done:
		return true
	case <-timer.C:
		return false
	}
}

// WaitFor blocks up to the timeout for the job to finish. It is the exported
// form used by the tools layer (bash startup yield window).
func (bs *BackgroundShell) WaitFor(timeout time.Duration) bool {
	return bs.waitFor(timeout)
}

// removeOutputFiles deletes a finished job's audit files (retention sweep).
func (bs *BackgroundShell) removeOutputFiles() {
	for _, p := range []string{bs.outPath, bs.errPath} {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}
