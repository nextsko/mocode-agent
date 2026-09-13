package jobs

import (
	"strings"
	"testing"

	"github.com/nextsko/mocode-agent/internal/core/shellruntime/shell"
	"github.com/stretchr/testify/require"
)

func job(id string) shell.JobStatus {
	return shell.JobStatus{ID: id, State: shell.JobStateCompleted, Command: "cmd-" + id, ExitCode: 0, ElapsedMs: 1500}
}

func TestFormatPendingJobNotificationsEmpty(t *testing.T) {
	require.Empty(t, FormatPendingJobNotifications(nil))
}

func TestFormatPendingJobNotificationsWithinLimit(t *testing.T) {
	out := FormatPendingJobNotifications([]shell.JobStatus{job("01"), job("02")})
	require.Contains(t, out, "<background_jobs_completed>")
	require.Contains(t, out, "cmd-01")
	require.Contains(t, out, "cmd-02")
	require.NotContains(t, out, "+ ")
}

func TestFormatPendingJobNotificationsCollapsesOverflow(t *testing.T) {
	jobs := []shell.JobStatus{job("01"), job("02"), job("03"), job("04"), job("05")}
	out := FormatPendingJobNotifications(jobs)
	require.Contains(t, out, "+ 2 earlier completed jobs not shown", "overflow must collapse")
	require.Contains(t, out, "cmd-05", "newest jobs stay visible")
	require.NotContains(t, out, "cmd-01", "oldest jobs collapse away")
	require.True(t, strings.Count(out, "exit=") == 3, "exactly MAX_VISIBLE=3 entries rendered")
}

func TestFormatPendingJobNotificationsMarksOutputCapped(t *testing.T) {
	capped := job("07")
	capped.OutputCapped = true
	capped.State = shell.JobStateKilled
	out := FormatPendingJobNotifications([]shell.JobStatus{capped})
	require.Contains(t, out, "output-capped")
}

func TestFilterJobNotificationsVerbosity(t *testing.T) {
	ok := job("01") // completed
	bad := job("02")
	bad.State = shell.JobStateFailed

	t.Run("default all keeps everything", func(t *testing.T) {
		require.Len(t, FilterJobNotifications([]shell.JobStatus{ok, bad}), 2)
	})

	t.Run("result keeps everything but renders one-liners", func(t *testing.T) {
		t.Setenv("MOCODE_BACKGROUND_NOTIFICATIONS", "result")
		require.Len(t, FilterJobNotifications([]shell.JobStatus{ok, bad}), 2)
		out := FormatPendingJobNotifications([]shell.JobStatus{ok, bad})
		require.Contains(t, out, "use job_output for details")
		require.NotContains(t, out, "elapsed=")
	})

	t.Run("error keeps only failures", func(t *testing.T) {
		t.Setenv("MOCODE_BACKGROUND_NOTIFICATIONS", "error")
		got := FilterJobNotifications([]shell.JobStatus{ok, bad})
		require.Len(t, got, 1)
		require.Equal(t, shell.JobStateFailed, got[0].State)
		require.Empty(t, FormatPendingJobNotifications(nil), "error mode with no failures injects nothing")
	})
}
