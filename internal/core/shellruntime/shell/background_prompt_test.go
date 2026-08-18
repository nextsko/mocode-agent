package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSyncBufferTail(t *testing.T) {
	t.Parallel()

	sb := &syncBuffer{}
	sb.WriteString("line1\nline2\nline3\n")

	require.Equal(t, "line1\nline2\nline3\n", sb.Tail(1024), "small buffer returned whole")
	require.Equal(t, "line3\n", sb.Tail(7), "tail cut at line boundary")
	require.Equal(t, "", sb.Tail(5), "budget smaller than any full line drops the partial line")
	require.Equal(t, "", sb.Tail(0), "zero budget returns empty")
}

func TestSyncBufferIdleTracking(t *testing.T) {
	t.Parallel()

	sb := &syncBuffer{}
	require.Zero(t, sb.IdleMs(), "never written → 0")

	sb.WriteString("x")
	idle := sb.IdleMs()
	require.GreaterOrEqual(t, idle, int64(0))
	require.Less(t, idle, int64(1000), "fresh write → small idle")
}

func TestLooksPrompting(t *testing.T) {
	t.Parallel()

	prompting := []string{
		"Do you want to continue? [y/N]",
		"Enter password:",
		"Continue?",
		"press any key to continue",
		"Press ENTER to confirm",
		"Proceed? [yes/no]",
		"please type your passphrase:",
		"npm warn some log line\nConfigure? >",
		"\n\nWarning: blah\nOverwrite file? ",
	}
	for _, s := range prompting {
		require.True(t, looksPrompting(s), "should detect prompt in %q", s)
	}

	notPrompting := []string{
		"",
		"\n\n\n",
		"build step 3/14 done",
		"Listening on http://0.0.0.0:3000",
		"transfer complete: 100%",
		"error: something failed",
		"GET /health 200 3ms",
	}
	for _, s := range notPrompting {
		require.False(t, looksPrompting(s), "should NOT flag %q", s)
	}
}

func TestBackgroundShell_PromptDetection(t *testing.T) {
	t.Parallel()

	// A `cat` job consumes stdin forever: simulate a program that printed a
	// prompt and is now blocked waiting for input. We write the prompt via
	// WriteInput (cat echoes it), then after the idle threshold the status
	// should flag LikelyPrompting.
	manager := newBackgroundShellManager()
	bg, err := manager.Start(t.Context(), t.TempDir(), nil, "cat", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bg.ID) })

	require.Eventually(t, func() bool {
		return bg.Status().Interactive
	}, time.Second, 20*time.Millisecond)

	_, err = bg.WriteInput([]byte("Continue? [y/N] "))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		st := bg.Status()
		return st.LikelyPrompting && st.IdleMs >= promptIdleThreshold.Milliseconds()
	}, 5*time.Second, 100*time.Millisecond, "quiet job with prompt-shaped tail must be flagged")

	st := bg.Status()
	require.False(t, st.Done)
	require.True(t, st.IdleMs > 0)
}

func TestBackgroundShell_NoFalsePromptOnBusyJob(t *testing.T) {
	t.Parallel()

	// A job that keeps producing non-prompt output must never be flagged.
	manager := newBackgroundShellManager()
	bg, err := manager.Start(t.Context(), t.TempDir(), nil,
		`for i in 1 2 3 4 5 6 7 8; do echo "tick $i"; sleep 0.3; done`, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bg.ID) })

	require.Eventually(t, func() bool {
		out, _, _, _ := bg.GetOutput()
		return strings.Contains(out, "tick")
	}, 2*time.Second, 50*time.Millisecond)

	time.Sleep(2 * promptIdleThreshold)
	st := bg.Status()
	require.False(t, st.LikelyPrompting, "busy non-prompt job must not be flagged")
}

func TestBackgroundShell_GetTailOutput(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()
	bg, err := manager.Start(t.Context(), t.TempDir(), nil, "seq 1 2000", "")
	require.NoError(t, err)
	bg.Wait()

	stdout, stderr, truncated, done, err := bg.GetTailOutput(256, 256)
	require.NoError(t, err)
	require.True(t, done)
	require.True(t, truncated)
	require.Empty(t, stderr)
	require.Less(t, len(stdout), 300)
	require.Contains(t, stdout, "2000", "tail must contain the END of output")

	full, _, d, _ := bg.GetOutput()
	require.Greater(t, len(full), len(stdout))
	require.True(t, d)
}

func TestBackgroundShellManager_Start_LimitCountsRunningOnly(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()

	// Fill the map with COMPLETED jobs beyond the limit — must not block.
	for i := 0; i < 3; i++ {
		bg, err := manager.Start(t.Context(), t.TempDir(), nil, "true", "")
		require.NoError(t, err)
		bg.Wait()
	}
	require.Equal(t, 3, manager.shells.Len())

	// New job still starts because zero jobs are running.
	bg, err := manager.Start(t.Context(), t.TempDir(), nil, "sleep 5", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bg.ID) })
	require.False(t, bg.IsDone())
}
