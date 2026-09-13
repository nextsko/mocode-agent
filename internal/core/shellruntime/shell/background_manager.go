package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// BackgroundShellManager manages background shell instances.
type BackgroundShellManager struct {
	shells *csync.Map[string, *BackgroundShell]
	// onJobComplete is an optional manager-wide terminal-state hook set
	// once by the app layer during startup (see SetOnJobComplete). It runs
	// in addition to each job's own BackgroundShellOptions.OnComplete.
	onJobComplete atomic.Pointer[func(*BackgroundShell)]
	// outputDir, when set (SetOutputDir), persists every job's output to
	// <dir>/<unixts>-<id>.{out,err} so a crash leaves an auditable trail
	// (gemini-cli's log-file model).
	outputDir atomic.Pointer[string]
	// outputKillBytes overrides backgroundOutputKillBytes when > 0 (tests).
	outputKillBytes atomic.Int64
	// pendingMu guards pending — terminal job snapshots waiting to be
	// drained into an agent turn (push-notification backlog).
	pendingMu sync.Mutex
	pending   []JobStatus
}

var (
	backgroundManager     *BackgroundShellManager
	backgroundManagerOnce sync.Once
	idCounter             atomic.Uint64
	// idEpoch prefixes job IDs with the manager's birth minute (base36) so
	// IDs from a previous process life are never reused in audit files or
	// model context after a restart.
	idEpoch atomic.Value // string
)

// newIDEpoch derives a short base36 prefix from the current unix minute.
func newIDEpoch() string {
	return strconv.FormatInt(time.Now().Unix()/60, 36)
}

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

// openJobOutputFiles creates the audit files for a new job. Failures
// degrade to memory-only capture — persistence must never fail a job.
func (m *BackgroundShellManager) openJobOutputFiles(id string) (out *os.File, errF *os.File, outPath, errPath string) {
	dir := ""
	if p := m.outputDir.Load(); p != nil {
		dir = *p
	}
	if dir == "" {
		return nil, nil, "", ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, "", ""
	}
	ts := time.Now().Unix()
	outPath = filepath.Join(dir, fmt.Sprintf("%d-%s.out", ts, id))
	errPath = filepath.Join(dir, fmt.Sprintf("%d-%s.err", ts, id))
	out, _ = os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	errF, _ = os.OpenFile(errPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	return out, errF, outPath, errPath
}

func closeFile(f *os.File) {
	if f != nil {
		_ = f.Sync()
		_ = f.Close()
	}
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

	epoch, _ := idEpoch.Load().(string)
	if epoch == "" {
		epoch = newIDEpoch()
		idEpoch.Store(epoch)
	}
	id := fmt.Sprintf("%s-%03X", epoch, idCounter.Add(1))

	shell := NewShell(&Options{
		WorkingDir: workingDir,
		BlockFuncs: blockFuncs,
	})
	if !options.TTY {
		sanitizeBackgroundEnv(shell)
	}

	shellCtx, cancel := context.WithCancel(ctx)

	bgShell := &BackgroundShell{
		ID:          id,
		Command:     command,
		Description: description,
		WorkingDir:  workingDir,
		Shell:       shell,
		TTY:         options.TTY,
		SessionID:   options.SessionID,
		ctx:         shellCtx,
		cancel:      cancel,
		stdout:      &syncBuffer{},
		stderr:      &syncBuffer{},
		done:        make(chan struct{}),
		onComplete:  options.OnComplete,
	}

	m.shells.Set(id, bgShell)

	bgShell.startedAt.Store(time.Now().Unix())

	// Runaway guard: cancel the job once either stream's TOTAL output
	// crosses the kill limit (memory is bounded by the head-tail buffer;
	// this stops the process itself from churning forever).
	kill := m.outputKillBytes.Load()
	if kill <= 0 {
		kill = backgroundOutputKillBytes
	}
	overflow := func() { bgShell.cancel() }
	for _, sb := range []*syncBuffer{bgShell.stdout, bgShell.stderr} {
		sb.killLimit = kill
		sb.onOverflow = overflow
	}

	// Audit trail: tee output to per-job files when persistence is on.
	outFile, errFile, outPath, errPath := m.openJobOutputFiles(id)
	bgShell.outPath, bgShell.errPath = outPath, errPath

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

	// Tee writers: memory buffer always, disk file when available.
	var stdoutW, stderrW io.Writer = bgShell.stdout, bgShell.stderr
	if outFile != nil {
		stdoutW = io.MultiWriter(bgShell.stdout, outFile)
	}
	if errFile != nil {
		stderrW = io.MultiWriter(bgShell.stderr, errFile)
	}

	go func() {
		// Defer order matters on Windows: audit files must close BEFORE the
		// done channel (LIFO — these two run last), or a retention cleanup
		// racing in right after Wait() cannot delete still-open files.
		defer close(bgShell.done)
		defer closeStdinPipe(bgShell.stdinReader, bgShell.stdinWriter)
		defer closeFile(errFile)
		defer closeFile(outFile)
		var err error
		if options.TTY {
			var runner backgroundRunner
			runner, err = startTTYBackgroundProcess(shellCtx, shell.GetWorkingDir(), shell.GetEnv(), shell.blockFuncs, command, stdoutW)
			if err == nil {
				bgShell.setRunner(runner)
				err = runner.Wait()
			}
		} else {
			err = shell.ExecStreamWithStdin(shellCtx, command, bgShell.stdinReader, stdoutW, stderrW)
		}

		bgShell.exitErr = err
		bgShell.exitCode.Store(int32(ExitCode(err)))
		bgShell.completedAt.Store(time.Now().Unix())

		// Fire the push notification before the done channel closes, so
		// waiters observe completion only after the hooks have run.
		m.notifyTerminal(bgShell)
	}()

	return bgShell, nil
}

// Get retrieves a background shell by ID.
func (m *BackgroundShellManager) Get(id string) (*BackgroundShell, bool) {
	return m.shells.Get(id)
}

// Remove removes a background shell from the manager without terminating it.
// This is useful when a shell has already completed and you just want to clean up tracking.
// Audit files of finished jobs are deleted with their tracking entry; files
// of running jobs stay (their runner goroutine is still writing them).
func (m *BackgroundShellManager) Remove(id string) error {
	shell, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}
	if shell.IsDone() {
		shell.removeOutputFiles()
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
			shell.removeOutputFiles()
			toRemove = append(toRemove, shell.ID)
		}
	}

	for _, id := range toRemove {
		_ = m.Remove(id)
	}

	return len(toRemove)
}

// terminateAndWait cancels a job and escalates to a force-terminate when the
// cooperative shutdown does not finish in time — the same escalation Kill
// uses, factored out so KillAll cannot leak processes that ignore the first
// signal. ctx bounds how long the final force attempt may block.
func terminateAndWait(bs *BackgroundShell, ctx context.Context) {
	bs.cancel()
	if r := bs.runnerRef(); r != nil {
		_ = r.Terminate(false)
	}
	if bs.waitFor(BackgroundKillGracePeriod) {
		return
	}
	if r := bs.runnerRef(); r != nil {
		_ = r.Terminate(true)
	}
	if bs.waitFor(BackgroundKillForcePeriod) {
		return
	}
	select {
	case <-bs.done:
	case <-ctx.Done():
	}
}

// KillAll terminates all background shells. The provided context bounds how
// long the function waits for each shell to exit.
func (m *BackgroundShellManager) KillAll(ctx context.Context) {
	shells := slices.Collect(m.shells.Seq())
	m.shells.Reset(map[string]*BackgroundShell{})

	var wg sync.WaitGroup
	for _, bs := range shells {
		wg.Go(func() {
			terminateAndWait(bs, ctx)
		})
	}
	wg.Wait()
}
