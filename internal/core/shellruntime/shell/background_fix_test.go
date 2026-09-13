package shell

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- A1: bounded head+tail buffer (P1) ---

func TestSyncBufferBoundedLargeSingleWrite(t *testing.T) {
	sb := &syncBuffer{}
	half := backgroundOutputHalfBytes

	n, err := sb.Write(bytes.Repeat([]byte("a"), half))
	require.NoError(t, err)
	require.Equal(t, half, n)

	// A single write larger than the tail budget: only its last half survives.
	big := bytes.Repeat([]byte("b"), 2<<20) // 2 MiB
	n, err = sb.Write(big)
	require.NoError(t, err)
	require.Equal(t, len(big), n)

	// Len reports every byte ever observed.
	require.Equal(t, int64(half+len(big)), sb.total)
	// Retained memory is capped.
	require.LessOrEqual(t, len(sb.head)+len(sb.tail), backgroundOutputCapBytes)
	require.Equal(t, int64((2<<20)-half), sb.omitted)

	s := sb.String()
	require.Contains(t, s, "bytes omitted")
	require.True(t, strings.HasPrefix(s, "aaaa"), "head must keep the oldest bytes")
	require.True(t, strings.HasSuffix(s, "bbbb"), "tail must keep the newest bytes")
}

func TestSyncBufferBoundedIncrementalWrites(t *testing.T) {
	sb := &syncBuffer{}
	half := backgroundOutputHalfBytes

	sb.Write(bytes.Repeat([]byte("H"), half)) // fill head

	// 1536 × 1 KiB chunks roll the tail; only the last half stays.
	for i := 0; i < 1536; i++ {
		sb.Write(bytes.Repeat([]byte("x"), 1024))
	}
	require.Equal(t, int64(half+1536*1024), sb.total)
	require.Equal(t, int64(1536*1024-half), sb.omitted)
	require.Equal(t, half, len(sb.tail))
	require.True(t, strings.HasSuffix(sb.String(), strings.Repeat("x", 64)))
}

func TestSyncBufferSmallOutputUnomittedAndTailAligned(t *testing.T) {
	sb := &syncBuffer{}
	sb.WriteString("line1\nline2\nline3\nline4\n")

	require.Equal(t, int64(24), sb.total)
	require.Zero(t, sb.omitted)
	require.NotContains(t, sb.String(), "omitted")

	// Small output lives in the head segment; the tail view must still see it.
	require.Equal(t, "line1\nline2\nline3\nline4\n", sb.Tail(64))

	// Tail(8) aligns to a line boundary: "line4\n".
	require.Equal(t, "line4\n", sb.Tail(8))
}

func TestSyncBufferIdleMs(t *testing.T) {
	sb := &syncBuffer{}
	require.Equal(t, int64(0), sb.IdleMs()) // nothing written yet
	sb.WriteString("x")
	require.GreaterOrEqual(t, sb.IdleMs(), int64(0))
}

// --- A3: session ownership (P4) ---

func TestBackgroundShellSessionOwnership(t *testing.T) {
	require.True(t, (&BackgroundShell{}).BelongsTo("anything"), "unowned jobs stay accessible")
	require.True(t, (&BackgroundShell{SessionID: "s1"}).BelongsTo("s1"))
	require.False(t, (&BackgroundShell{SessionID: "s1"}).BelongsTo("s2"))
}

// --- A4: output sanitation env (P5) ---

func TestBackgroundShellSanitizedEnv(t *testing.T) {
	ctx := t.Context()
	manager := newBackgroundShellManager()

	bgShell, err := manager.Start(ctx, t.TempDir(), nil, "echo hello", "",
		BackgroundShellOptions{SessionID: "sess-a"})
	require.NoError(t, err)
	defer manager.KillAll(ctx)

	for _, kv := range []string{"NO_COLOR=1", "TERM=dumb", "PAGER=cat", "GIT_PAGER=cat", "GH_PAGER=cat"} {
		require.True(t, slices.Contains(bgShell.Shell.GetEnv(), kv), "missing sanitized env %s", kv)
	}
	require.Equal(t, "sess-a", bgShell.SessionID)
}

// --- ID epoch prefix ---

func TestBackgroundJobIDHasEpochPrefix(t *testing.T) {
	ctx := t.Context()
	manager := newBackgroundShellManager()

	bg1, err := manager.Start(ctx, t.TempDir(), nil, "echo a", "")
	require.NoError(t, err)
	bg2, err := manager.Start(ctx, t.TempDir(), nil, "echo b", "")
	require.NoError(t, err)
	defer manager.KillAll(ctx)

	require.Contains(t, bg1.ID, "-", "job ID must carry the epoch prefix: %s", bg1.ID)
	require.Contains(t, bg2.ID, "-", "job ID must carry the epoch prefix: %s", bg2.ID)
	require.NotEqual(t, bg1.ID, bg2.ID)
}

// --- promote signal (Ctrl+B) ---

func TestPromoteSignalOneShot(t *testing.T) {
	require.Nil(t, PromoteSignal(), "no signal before anyone fires it")
	PromotePendingBash()
	select {
	case <-PromoteSignal():
	default:
		t.Fatal("fired signal must be receivable")
	}
	// Still receivable (closed channel broadcasts), one-shot per fire.
	select {
	case <-PromoteSignal():
	default:
		t.Fatal("closed signal must keep broadcasting")
	}
}

// --- P3: event-driven Done channel ---

func TestBackgroundShellDoneChannelClosesOnCompletion(t *testing.T) {
	ctx := t.Context()
	manager := newBackgroundShellManager()

	bgShell, err := manager.Start(ctx, t.TempDir(), nil, "echo hello", "")
	require.NoError(t, err)
	defer manager.KillAll(ctx)

	select {
	case <-bgShell.Done():
		// closed on completion — expected
	case <-time.After(10 * time.Second):
		t.Fatal("Done channel was not closed after the job finished")
	}
	require.True(t, bgShell.IsDone())
}

// --- P2: push notification on terminal state ---

func TestBackgroundShellOnCompleteFiresOnce(t *testing.T) {
	ctx := t.Context()
	manager := newBackgroundShellManager()

	var calls atomic.Int32
	bg, err := manager.Start(ctx, t.TempDir(), nil, "echo hi", "",
		BackgroundShellOptions{OnComplete: func(bs *BackgroundShell) { calls.Add(1) }})
	require.NoError(t, err)
	bg.Wait() // hooks run before the done channel closes — no Eventually needed
	require.Equal(t, int32(1), calls.Load())
}

func TestManagerJobCompletionHooksAndDrain(t *testing.T) {
	ctx := t.Context()
	manager := newBackgroundShellManager()

	var hooked atomic.Int32
	manager.SetOnJobComplete(func(bs *BackgroundShell) { hooked.Add(1) })

	bg, err := manager.Start(ctx, t.TempDir(), nil, "echo hi", "",
		BackgroundShellOptions{SessionID: "sess-drain"})
	require.NoError(t, err)
	bg.Wait()

	require.Equal(t, int32(1), hooked.Load(), "manager-wide hook must fire once")

	drained := manager.DrainCompletedNotifications("sess-drain")
	require.Len(t, drained, 1)
	require.Equal(t, "sess-drain", drained[0].SessionID)
	require.Equal(t, JobStateCompleted, drained[0].State)
	require.Empty(t, manager.DrainCompletedNotifications("sess-drain"), "drain must clear the queue")
	require.Empty(t, manager.DrainCompletedNotifications("other"), "foreign session must drain nothing")
}

// --- P5: output persistence ---

func TestBackgroundOutputPersistedToDisk(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	manager := newBackgroundShellManager()
	manager.SetOutputDir(dir)

	bg, err := manager.Start(ctx, t.TempDir(), nil, "echo persisted", "")
	require.NoError(t, err)
	bg.Wait()

	require.NotEmpty(t, bg.outPath, "audit out path must be recorded")
	data, err := os.ReadFile(bg.outPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "persisted")

	// Retention sweep removes the tracking entry together with the files.
	bg.completedAt.Store(time.Now().Add(-time.Duration(CompletedJobRetentionMinutes+1) * time.Minute).Unix())
	manager.Cleanup()
	_, err = os.Stat(bg.outPath)
	require.True(t, os.IsNotExist(err), "cleanup must delete audit files")
}

// --- runaway output guard ---

func TestSyncBufferOverflowFiresOnce(t *testing.T) {
	var fired atomic.Int32
	sb := &syncBuffer{killLimit: 8, onOverflow: func() { fired.Add(1) }}
	sb.WriteString("1234567890") // crosses the limit once…
	sb.WriteString("more bytes") // …and again — must fire only once (async guard)
	require.Eventually(t, func() bool { return fired.Load() == 1 }, time.Second, 5*time.Millisecond)
	require.True(t, sb.OutputCapped())
}

func TestManagerOutputKillCancelsRunawayJob(t *testing.T) {
	ctx := t.Context()
	manager := newBackgroundShellManager()
	manager.SetOutputKillBytes(64)

	bg, err := manager.Start(ctx, t.TempDir(), nil,
		"while true; do echo spamspamspam; done", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = manager.Kill(bg.ID) })

	require.Eventually(t, func() bool {
		return bg.IsDone()
	}, 10*time.Second, 50*time.Millisecond, "runaway job must be cancelled")

	st := bg.Status()
	require.True(t, st.OutputCapped, "status must report the cap")
	require.Equal(t, JobStateKilled, st.State)
}
