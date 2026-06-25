Upload a local file to a remote host over SSH (uses the scp(1) wire
protocol, so the remote side only needs `scp` in `$PATH` — no SFTP
server required).

Use this tool to ship a build artifact, deploy a config, or push a
single file to a server.  For directories, archive them first
(`tar -czf - … | ssh host 'tar -xzf -'`) and use `ssh_exec`.

`local` must point to an existing regular file.  `remote` must be an
absolute path; relative paths and `..` segments are rejected.  If
`mode` is empty the source file's mode is preserved; otherwise pass an
octal string such as `"0644"`.

The transfer is one-shot (whole file in memory) — for files larger
than a few hundred megabytes prefer the streaming pattern shown above.
