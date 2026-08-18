List the `Host` entries declared in `~/.ssh/config`.

Use this tool to discover which aliases are available before calling
`ssh_exec` / `ssh_upload` / `ssh_download`.  The output is a Markdown
table with one row per alias and the columns: `HostName`, `User`,
`Port`, `IdentityFile`, `ProxyJump`.

Wildcard patterns (`Host *`, `Host *.example.com`) are filtered out —
they match too broadly to be useful as a tool input.

The tool only reads the local config file; it does not contact any
remote host and therefore does not need a permission request.
