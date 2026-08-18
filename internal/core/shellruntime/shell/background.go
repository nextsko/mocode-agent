package shell

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nextsko/mocode-agent/internal/util/csync"
)

const (
	// MaxBackgroundJobs is the maximum number of concurrent background jobs allowed
	MaxBackgroundJobs = 50
	// CompletedJobRetentionMinutes is how long to keep completed jobs before auto-cleanup (8 hours)
	CompletedJobRetentionMinutes = 8 * 60
	// BackgroundKillGracePeriod is how long Kill waits for a cooperative shutdown
	// before escalating or returning.
	BackgroundKillGracePeriod = 750 * time.Millisecond
	// BackgroundKillForcePeriod is how long Kill waits after a forced terminate.
	BackgroundKillForcePeriod = 500 * time.Millisecond
)

// syncBuffer is a thread-safe wrapper around bytes.Buffer that also tracks
// when the last write happened, so observers can distinguish "producing
// output" from "quiet — maybe waiting for input".
type syncBuffer struct {
	buf        bytes.Buffer
	mu         sync.RWMutex
	lastWriteMs atomic.Int64
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	n, err = sb.buf.Write(p)
	if n > 0 {
		sb.lastWriteMs.Store(time.Now().UnixMilli())
	}
	return n, err
}

func (sb *syncBuffer) WriteString(s string) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	n, err = sb.buf.WriteString(s)
	if n > 0 {
		sb.lastWriteMs.Store(time.Now().UnixMilli())
	}
	return n, err
}

func (sb *syncBuffer) String() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.String()
}

// Len returns the number of buffered bytes without copying.
func (sb *syncBuffer) Len() int {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.Len()
}

// Tail returns at most the last n bytes of the buffer, cut at a line boundary
// when possible so incremental reads stay line-aligned.
func (sb *syncBuffer) Tail(n int) string {
	if n <= 0 {
		return ""
	}
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	b := sb.buf.Bytes()
	if len(b) <= n {
		return string(b)
	}
	cut := len(b) - n
	if i := bytes.IndexByte(b[cut:], '\n'); i >= 0 {
		cut += i + 1
	}
	return string(b[cut:])
}

// IdleMs reports milliseconds since the last write (0 when nothing written
// yet and the caller should fall back to started-at).
func (sb *syncBuffer) IdleMs() int64 {
	last := sb.lastWriteMs.Load()
	if last == 0 {
		return 0
	}
	return time.Now().UnixMilli() - last
}

// BackgroundShell represents a shell running in the background.
type BackgroundShell struct {
	ID          string
	Command     string
	Description string
	Shell       *Shell
	WorkingDir  string
	TTY         bool
	ctx         context.Context
	cancel      context.CancelFunc
	stdout      *syncBuffer
	stderr      *syncBuffer
	done        chan struct{}
	exitErr     error
	// runner is assigned by the start goroutine (TTY path) and read by
	// WriteInput/Kill/Status from other goroutines — every access must go
	// through setRunner/runnerRef (the old bare field access was a data race).
	runner      backgroundRunner
	runnerMu    sync.Mutex
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
}

type BackgroundShellOptions struct {
	TTY bool
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
}

// BackgroundShellManager manages background shell instances.
type BackgroundShellManager struct {
	shells *csync.Map[string, *BackgroundShell]
}

var (
	backgroundManager     *BackgroundShellManager
	backgroundManagerOnce sync.Once
	idCounter             atomic.Uint64
)

// newBackgroundShellManager creates a new BackgroundShellManager instance.
func newBackgroundShellManager() *BackgroundShellManager {
	return &BackgroundShellManager{
		shells: csync.NewMap[string, *BackgroundShell](),
	}
}

// GetBackgroundShellManager returns the singleton background shell manager.
func GetBackgroundShellManager() *BackgroundShellManager {
	backgroundManagerOnce.Do(func() {
		backgroundManager = newBackgroundShellManager()
	})
	return backgroundManager
}

// Start creates and starts a new background shell with the given command.
func (m *BackgroundShellManager) Start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command string, description string, opts ...BackgroundShellOptions) (*BackgroundShell, error) {
	// The job limit protects against runaway concurrency, so it must count
	// only LIVE jobs — completed-but-retained jobs (8h retention) used to
	// exhaust the cap and block new work until cleanup.
	running := 0
	for s := range m.shells.Seq() {
		if !s.IsDone() {
			running++
		}
	}
	if running >= MaxBackgroundJobs {
		return nil, fmt.Errorf("maximum number of background jobs (%d) reached. Please terminate or wait for some jobs to complete", MaxBackgroundJobs)
	}
	options := BackgroundShellOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}

	id := fmt.Sprintf("%03X", idCounter.Add(1))

	shell := NewShell(&Options{
		WorkingDir: workingDir,
		BlockFuncs: blockFuncs,
	})

	shellCtx, cancel := context.WithCancel(ctx)

	bgShell := &BackgroundShell{
		ID:          id,
		Command:     command,
		Description: description,
		WorkingDir:  workingDir,
		Shell:       shell,
		TTY:         options.TTY,
		ctx:         shellCtx,
		cancel:      cancel,
		stdout:      &syncBuffer{},
		stderr:      &syncBuffer{},
		done:        make(chan struct{}),
	}

	m.shells.Set(id, bgShell)

	bgShell.startedAt.Store(time.Now().Unix())

	// For non-TTY jobs, open a stdin pipe so the agent can answer prompts or
	// feed a long-running interactive process via WriteInput. TTY jobs receive
	// input through the PTY master inside their runner instead.
	if !options.TTY {
		stdinR, stdinW, err := os.Pipe()
		if err != nil {
			// The shell was already registered above; without this Take it
			// would linger forever as a "running" ghost that never completes
			// and leaks a MaxBackgroundJobs slot.
			_, _ = m.shells.Take(id)
			cancel()
			return nil, fmt.Errorf("create stdin pipe: %w", err)
		}
		bgShell.stdinReader = stdinR
		bgShell.stdinWriter = stdinW
	}

	go func() {
		defer close(bgShell.done)
		// Closing the pipe ends signals EOF to the interpreter and unblocks any
		// WriteInput stuck on a full buffer.
		defer closeStdinPipe(bgShell.stdinReader, bgShell.stdinWriter)
		var err error
		if options.TTY {
			var runner backgroundRunner
			runner, err = startTTYBackgroundProcess(shellCtx, shell.GetWorkingDir(), shell.GetEnv(), shell.blockFuncs, command, bgShell.stdout)
			if err == nil {
				bgShell.setRunner(runner)
				err = runner.Wait()
			}
		} else {
			err = shell.ExecStreamWithStdin(shellCtx, command, bgShell.stdinReader, bgShell.stdout, bgShell.stderr)
		}

		bgShell.exitErr = err
		bgShell.exitCode.Store(int32(ExitCode(err)))
		bgShell.completedAt.Store(time.Now().Unix())
	}()

	return bgShell, nil
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

// Get retrieves a background shell by ID.
func (m *BackgroundShellManager) Get(id string) (*BackgroundShell, bool) {
	return m.shells.Get(id)
}

// Remove removes a background shell from the manager without terminating it.
// This is useful when a shell has already completed and you just want to clean up tracking.
func (m *BackgroundShellManager) Remove(id string) error {
	_, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}
	return nil
}

// Kill terminates a background shell by ID.
func (m *BackgroundShellManager) Kill(id string) error {
	shell, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}

	shell.cancel()
	if r := shell.runnerRef(); r != nil {
		_ = r.Terminate(false)
	}
	if shell.waitFor(BackgroundKillGracePeriod) {
		return nil
	}
	if r := shell.runnerRef(); r != nil {
		_ = r.Terminate(true)
		if shell.waitFor(BackgroundKillForcePeriod) {
			return nil
		}
	}
	// The job refused to die (uninterruptible syscall, ignored signals).
	// Re-register it so it stays observable via job_output instead of
	// becoming an invisible orphan that the manager has forgotten about.
	m.shells.Set(id, shell)
	return fmt.Errorf("background shell %s is still shutting down (kept for observation; retry later)", id)
}

// BackgroundShellInfo contains information about a background shell.
type BackgroundShellInfo struct {
	ID          string
	Command     string
	Description string
}

// List returns all background shell IDs.
func (m *BackgroundShellManager) List() []string {
	ids := make([]string, 0, m.shells.Len())
	for id := range m.shells.Seq2() {
		ids = append(ids, id)
	}
	return ids
}

// Statuses returns structured status snapshots for every tracked job.
func (m *BackgroundShellManager) Statuses() []JobStatus {
	out := make([]JobStatus, 0, m.shells.Len())
	for shell := range m.shells.Seq() {
		out = append(out, shell.Status())
	}
	return out
}

// Cleanup removes completed jobs that have been finished for more than the retention period
func (m *BackgroundShellManager) Cleanup() int {
	now := time.Now().Unix()
	retentionSeconds := int64(CompletedJobRetentionMinutes * 60)

	var toRemove []string
	for shell := range m.shells.Seq() {
		completedAt := shell.completedAt.Load()
		if completedAt > 0 && now-completedAt > retentionSeconds {
			toRemove = append(toRemove, shell.ID)
		}
	}

	for _, id := range toRemove {
		_ = m.Remove(id)
	}

	return len(toRemove)
}

// KillAll terminates all background shells. The provided context bounds how
// long the function waits for each shell to exit.
func (m *BackgroundShellManager) KillAll(ctx context.Context) {
	shells := slices.Collect(m.shells.Seq())
	m.shells.Reset(map[string]*BackgroundShell{})

	var wg sync.WaitGroup
	for _, shell := range shells {
		wg.Go(func() {
			shell.cancel()
			if r := shell.runnerRef(); r != nil {
				_ = r.Terminate(false)
			}
			select {
			case <-shell.done:
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
}

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
	done := bs.IsDone()
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
