// Package sandbox provides isolated code-execution tools (ts_run / py_run):
// each run gets a throwaway working directory, a wall-clock timeout, and a
// bounded combined-output capture. Runtime discovery prefers bun (TS) and uv
// (Python ephemeral dependencies); plain node/python are the fallbacks.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	// DefaultTimeout bounds a sandboxed run unless the model asks for more.
	DefaultTimeout = 60 * time.Second
	// MaxTimeout is the hard ceiling for the model-requested timeout.
	MaxTimeout = 5 * time.Minute
	// outputCapBytes bounds retained combined output (head+tail halves,
	// middle dropped) so a chatty program cannot blow up the context.
	outputCapBytes = 1 << 20
)

// result carries everything a tool response needs from one sandboxed run.
type result struct {
	Output   string // bounded combined stdout+stderr (interleaved)
	ExitCode int
	Duration time.Duration
	Runtime  string // resolved runtime label, e.g. "bun 1.1.0" / "uv python"
}

// runner executes a prepared working directory with the given runtime argv.
func runInDir(ctx context.Context, dir string, argv []string, env []string, timeout time.Duration) (result, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}

	var buf cappedBuffer
	// Interleave stdout and stderr the way a terminal would.
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	start := time.Now()
	err := cmd.Run()
	res := result{
		Output:   buf.String(),
		Duration: time.Since(start).Truncate(time.Millisecond),
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		res.Output += fmt.Sprintf("\n[sandbox] timed out after %s", timeout)
		return res, nil
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("spawn %s: %w", argv[0], err)
	}
	res.ExitCode = 0
	return res, nil
}

// cappedBuffer keeps at most outputCapBytes: a stable head half plus a
// rolling tail half, middle dropped with an omission marker.
type cappedBuffer struct {
	head    []byte
	tail    []byte
	total   int
	omitted int64
}

const halfCap = outputCapBytes / 2

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.total += len(p)
	rest := p
	if len(b.head) < halfCap {
		room := halfCap - len(b.head)
		if room >= len(rest) {
			b.head = append(b.head, rest...)
			return len(p), nil
		}
		b.head = append(b.head, rest[:room]...)
		rest = rest[room:]
	}
	b.tail = append(b.tail, rest...)
	if excess := len(b.tail) - halfCap; excess > 0 {
		b.omitted += int64(excess)
		copy(b.tail, b.tail[excess:])
		b.tail = b.tail[:len(b.tail)-excess]
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	var out bytes.Buffer
	out.Write(b.head)
	if b.omitted > 0 {
		fmt.Fprintf(&out, "\n... [%d bytes omitted] ...\n", b.omitted)
	}
	out.Write(b.tail)
	return out.String()
}

// runtimeCache memoizes runtime lookups for the process lifetime.
var (
	runtimeMu    sync.Mutex
	runtimeCache = map[string]string{}
)

// lookupRuntime resolves a runtime binary by trying candidates in order and
// memoizing the first hit (empty string when none is available).
func lookupRuntime(candidates ...string) string {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	key := candidates[0]
	if v, ok := runtimeCache[key]; ok {
		return v
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			runtimeCache[key] = path
			return path
		}
	}
	runtimeCache[key] = ""
	return ""
}
