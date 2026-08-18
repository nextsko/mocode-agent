Get the latest output from a background shell by ID; wait=true blocks with a bounded timeout and returns early when the job looks like it is waiting for interactive input.

<usage>
- Provide the shell ID returned from a background bash execution
- Returns the LAST ~16KB per stream (line-aligned), not the whole history — total byte counts are shown when output was clipped
- Indicates whether the shell has completed, its exit code, and how long since its last output byte (idle)
- Set wait=true to block until completion, the wait budget expires (wait_timeout_sec, default 60, max 120), or the job goes quiet with a prompt-looking tail — then it returns immediately so you can answer
</usage>

<features>
- View output from running background processes without flooding context with full logs
- Check completion state, exit code, elapsed time, and output idleness
- LIKELY WAITING FOR INPUT detection: quiet job + prompt-shaped tail (e.g. "[y/N]", "Password:", trailing '?') is flagged in the summary — the classic TTY hang
- Bounded blocking wait that can never hang the turn: completes, times out, or detects a prompt
</features>

<tips>
- Long-running servers: poll WITHOUT wait to peek at progress; each call returns the recent tail
- Interactive prompts (confirmations, passwords): the summary line flags them — respond with job_input (press_enter=true for line-oriented prompts) or abort with job_kill
- Use wait=true with a small wait_timeout_sec when you expect the job to finish soon; the tool still returns early the moment a prompt is detected
- idle near 0 while state=running means the job is actively producing output — it is not stuck
</tips>
