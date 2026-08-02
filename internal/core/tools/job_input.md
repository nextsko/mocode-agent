Send text to the stdin of a running background shell so it can answer prompts, confirmations, or feed an interactive process.

<usage>
- Provide the shell ID of a running background bash job
- Provide the text to write; set press_enter=true to append a newline for line-oriented prompts (e.g. y/n confirmations, "Press ENTER to continue")
- The job must still be running and accept stdin (job_output reports interactive=true for such jobs)
</usage>

<features>
- Answer interactive prompts from a background process
- Feed piped input incrementally without restarting the command
- Works for the default non-TTY emulator path on all platforms and for TTY jobs on Linux
</features>

<tips>
- Check job_output first to see whether the job is interactive and still running
- Use press_enter=true for confirmation prompts that expect a single line
- A command that never reads stdin will fill its buffer; writes time out instead of blocking forever
</tips>
