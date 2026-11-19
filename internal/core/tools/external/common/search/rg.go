package searchcommon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nextsko/mocode-agent/internal/util/fsext"
	"github.com/nextsko/mocode-agent/internal/util/log"
)

// GetRg returns the path to ripgrep if available.
func GetRg() string {
	return getRg()
}

var getRg = sync.OnceValue(func() string {
	path, err := exec.LookPath("rg")
	if err != nil {
		if log.Initialized() {
			slog.Warn("Ripgrep (rg) not found in $PATH. Some grep features might be limited or slower.")
		}
		return ""
	}
	return path
})

// GetRgCmd builds an ripgrep --files command for glob matching.
func GetRgCmd(ctx context.Context, globPattern string) *exec.Cmd {
	name := getRg()
	if name == "" {
		return nil
	}
	args := []string{"--files", "-L", "--null"}
	if globPattern != "" {
		if !filepath.IsAbs(globPattern) && !strings.HasPrefix(globPattern, "/") {
			globPattern = "/" + globPattern
		}
		args = append(args, "--glob", globPattern)
	}
	return exec.CommandContext(ctx, name, args...)
}

// GetRgSearchCmd builds an ripgrep search command for grep matching.
func GetRgSearchCmd(ctx context.Context, pattern, path, include string) *exec.Cmd {
	name := getRg()
	if name == "" {
		return nil
	}
	args := []string{"--json", "-H", "-n", "-0", pattern}
	if include != "" {
		args = append(args, "--glob", include)
	}
	args = append(args, path)

	return exec.CommandContext(ctx, name, args...)
}

// RunRipgrep executes a ripgrep --files command and returns matched paths.
// Output is streamed and the process is killed as soon as limit matches are
// collected — an unbounded `rg --files` over a huge mount (the zxxw trace:
// 7.7 minutes on /data01) burned its whole latency budget before truncation
// ever applied. Returns stoppedEarly=true when the process was killed after
// reaching the limit, so callers can flag truncation without rerunning.
func RunRipgrep(cmd *exec.Cmd, searchRoot string, limit int) ([]string, bool, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("ripgrep stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("ripgrep start: %w", err)
	}

	var matches []string
	stoppedEarly := false
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		absPath := scanner.Text() // --null separated
		if absPath == "" {
			continue
		}
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(searchRoot, absPath)
		}
		if fsext.SkipHidden(absPath) {
			continue
		}
		matches = append(matches, absPath)
		if limit > 0 && len(matches) >= limit {
			stoppedEarly = true
			_ = cmd.Process.Kill()
			break
		}
	}

	waitErr := cmd.Wait()
	if !stoppedEarly {
		if scannerErr := scanner.Err(); scannerErr != nil && waitErr == nil {
			return matches, false, fmt.Errorf("ripgrep output: %w", scannerErr)
		}
		if waitErr != nil {
			if ee, ok := waitErr.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				return nil, false, nil
			}
			return matches, false, fmt.Errorf("ripgrep: %w\n%s", waitErr, stderr.String())
		}
	}
	// stoppedEarly: scanner.Err() and waitErr reflect the deliberate kill —
	// both ignored.
	return matches, stoppedEarly, nil
}
