package jobs

import (
	"fmt"
	"os"
	"strings"

	"github.com/nextsko/mocode-agent/internal/core/shellruntime/shell"
)

// maxVisibleJobNotifications bounds how many background-job completion
// notices are inlined into a single turn; older ones collapse into a
// "+N more" line (borrowed from Claude Code's MAX_VISIBLE_NOTIFICATIONS).
const maxVisibleJobNotifications = 3

// notificationVerbosity selects how much detail job-completion notices carry
// when folded into a turn (hermes-agent's display.background_process_notifications
// levels). all (default): full block. result: one-line summary only.
// error: only failed/killed jobs are announced at all.
type notificationVerbosity string

const (
	verbosityAll    notificationVerbosity = "all"
	verbosityResult notificationVerbosity = "result"
	verbosityError  notificationVerbosity = "error"
)

// notificationVerbosityFromEnv reads MOCODE_BACKGROUND_NOTIFICATIONS and
// falls back to "all" on anything unparseable.
func notificationVerbosityFromEnv() notificationVerbosity {
	switch os.Getenv("MOCODE_BACKGROUND_NOTIFICATIONS") {
	case "result":
		return verbosityResult
	case "error":
		return verbosityError
	default:
		return verbosityAll
	}
}

func (v notificationVerbosity) keep(st shell.JobStatus) bool {
	if v == verbosityError {
		return st.State == shell.JobStateFailed || st.State == shell.JobStateKilled
	}
	return true
}

// FilterJobNotifications applies the configured verbosity level in place.
func FilterJobNotifications(jobs []shell.JobStatus) []shell.JobStatus {
	v := notificationVerbosityFromEnv()
	if v == verbosityAll {
		return jobs
	}
	out := jobs[:0]
	for _, st := range jobs {
		if v.keep(st) {
			out = append(out, st)
		}
	}
	return out
}

// FormatPendingJobNotifications renders drained terminal job snapshots as a
// compact block that coordinator.Run prepends to the turn's prompt — the push
// model: the model learns jobs finished without polling job_output.
// The verbosity knob (MOCODE_BACKGROUND_NOTIFICATIONS) narrows it to a
// one-liner per job ("result") or failures only ("error").
func FormatPendingJobNotifications(jobs []shell.JobStatus) string {
	jobs = FilterJobNotifications(jobs)
	if len(jobs) == 0 {
		return ""
	}
	if v := notificationVerbosityFromEnv(); v == verbosityResult {
		var b strings.Builder
		b.WriteString("<background_jobs_completed>\n")
		for _, st := range jobs {
			fmt.Fprintf(&b, "- %s [%s] exit=%d: %s\n", st.ID, st.State, st.ExitCode, st.Command)
		}
		b.WriteString("(use job_output for details)</background_jobs_completed>")
		return b.String()
	}
	var b strings.Builder
	b.WriteString("<background_jobs_completed>\nThe following background jobs finished since your last turn:\n")
	visible := jobs
	if len(jobs) > maxVisibleJobNotifications {
		visible = jobs[len(jobs)-maxVisibleJobNotifications:]
	}
	for _, st := range visible {
		desc := st.Description
		if desc == "" {
			desc = st.Command
		}
		capped := ""
		if st.OutputCapped {
			capped = " output-capped"
		}
		fmt.Fprintf(&b, "- %s [%s%s] exit=%d elapsed=%.0fs: %s\n",
			st.ID, st.State, capped, st.ExitCode, float64(st.ElapsedMs)/1000, desc)
	}
	if len(jobs) > maxVisibleJobNotifications {
		fmt.Fprintf(&b, "(+ %d earlier completed jobs not shown — call job_output if you need their results)\n",
			len(jobs)-maxVisibleJobNotifications)
	}
	b.WriteString("</background_jobs_completed>")
	return b.String()
}
