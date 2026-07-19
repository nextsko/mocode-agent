Download a file from a remote host to the local machine over SSH
(uses the scp(1) wire protocol).

Use this tool to pull a log file, fetch a build artifact, or grab a
single config from a server.  For directories use `ssh_exec` with a
tarring pipeline.

`remote` must be an absolute path; relative paths and `..` segments
are rejected.  `local` is the destination — the tool refuses to
silently overwrite an existing file; delete it first if you really
want to clobber it.

Like `ssh_upload`, the transfer is one-shot (whole file in memory).
For multi-hundred-MB downloads stream with `ssh host 'cat remote' >
local` via `ssh_exec` instead.
