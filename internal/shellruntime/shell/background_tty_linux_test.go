//go:build linux

package shell

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackgroundShellManager_StartTTYUsesTerminal(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	bgShell, err := manager.Start(
		t.Context(),
		t.TempDir(),
		nil,
		`if test -t 1; then printf 'tty=yes\n'; else printf 'tty=no\n'; fi`,
		"tty check",
		BackgroundShellOptions{TTY: true},
	)
	require.NoError(t, err)
	require.True(t, bgShell.TTY)

	bgShell.Wait()

	stdout, stderr, done, execErr := bgShell.GetOutput()
	require.True(t, done)
	require.NoError(t, execErr)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "tty=yes")
}

func TestBackgroundShellManager_KillTTYProcess(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	bgShell, err := manager.Start(
		t.Context(),
		t.TempDir(),
		nil,
		"sleep 10",
		"tty sleep",
		BackgroundShellOptions{TTY: true},
	)
	require.NoError(t, err)
	require.True(t, bgShell.TTY)

	require.Eventually(t, func() bool {
		_, _, done, _ := bgShell.GetOutput()
		return !done
	}, time.Second, 50*time.Millisecond)

	require.NoError(t, manager.Kill(bgShell.ID))
	require.True(t, bgShell.WaitContext(context.Background()))

	_, _, done, execErr := bgShell.GetOutput()
	require.True(t, done)
	require.Error(t, execErr)
	require.True(t, IsInterrupt(execErr) || strings.Contains(strings.ToLower(execErr.Error()), "signal"))
}
