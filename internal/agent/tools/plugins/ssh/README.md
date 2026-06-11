# SSH Plugin for mocode

A small, dependency-light plugin that gives the mocode agent four
SSH-related tools:

| Tool             | Purpose                                  |
|------------------|------------------------------------------|
| `ssh_exec`       | Run a command on a remote host           |
| `ssh_upload`     | `scp` a local file to a remote path      |
| `ssh_download`   | `scp` a remote file to a local path      |
| `ssh_list_hosts` | List the `Host` entries in `~/.ssh/config` |

## Why `golang.org/x/crypto/ssh` only?

Everything uses the official `x/crypto/ssh` package — **no extra Go
dependencies** are pulled in:

- The `ssh` client implements RFC 4253.
- `ssh-agent` support rides on `x/crypto/ssh/agent`.
- File transfer uses the simple `scp(1)` wire protocol, so the remote
  side only needs `scp` on its `$PATH` (no SFTP server required).

## Authentication chain

When a tool needs a connection it tries, in order:

1. The local `ssh-agent` (via `SSH_AUTH_SOCK`).
2. Well-known private keys in `~/.ssh/`: `id_ed25519`, `id_rsa`,
   `id_ecdsa`, `id_dsa`.
3. The `IdentityFile` from the matched `~/.ssh/config` entry.
4. A static password, **only** when the user has explicitly configured
   one (it never enters the LLM prompt).

A new auth strategy is a one-liner:

```go
ssh.NewPool(ssh.WithAuthMethod(func(spec ssh.HostSpec) ([]ssh.AuthMethod, error) {
    return []ssh.AuthMethod{ssh.PublicKeys(mySigner)}, nil
}))
```

## Host resolution

`ssh_exec`/`ssh_upload`/`ssh_download` accept any of:

- An alias defined in `~/.ssh/config` — **the recommended form** because
  it keeps real hostnames and credentials out of the LLM prompt.
- `user@host`
- `user@host:port`
- `host:port` (user defaults to the current OS user)

Use `ssh_list_hosts` to discover which aliases exist.

## Security posture

- **Host key verification is on by default** — connections are refused
  if the host key is not in `~/.ssh/known_hosts`.  The tool never
  silently trusts unknown hosts.
- **No TTY**, no shell allocation: commands run non-interactively and
  cannot be hijacked by escape sequences.
- **Banned command prefix list** — `rm -rf /`, `mkfs.*`, `shutdown`,
  `reboot`, `dd if=/dev/urandom of=/dev/sd*`, etc. are rejected at the
  tool layer before any permission prompt is shown.
- **Per-call permission request** — every remote action goes through
  the same `permission.Service` as `bash`/`edit`, so the user can
  allow/deny in the same UI.
- **Path jail-break check** — `..` segments in remote paths are
  rejected by `ssh_upload` and `ssh_download`.
- **Timeouts everywhere** — TCP dial (10s), SSH handshake (15s),
  command execution (30s default, configurable per-call).

## Connection reuse

The plugin owns a single `*Pool` of refcounted SSH connections, keyed
by `host:port`.  Repeated calls to the same host reuse the underlying
TCP/SSH session; idle connections are evicted after 5 minutes (see
`Evict()` in `client.go`).  The pool is closed on `Stop()`, which the
registry calls on shutdown.

## Files

```
ssh/
├── client.go            # Pool, Client, auth strategies, scp wire
├── config.go            # ~/.ssh/config resolver, HostSpec
├── helpers.go           # permission wrapper, ban list, formatters
├── service.go           # shared state (one per process)
├── ssh_exec.go          # ssh_exec tool
├── ssh_upload.go        # ssh_upload tool
├── ssh_download.go      # ssh_download tool
├── ssh_list_hosts.go    # ssh_list_hosts tool
├── ssh_exec.md          # tool descriptions (embedded)
├── ssh_upload.md
├── ssh_download.md
├── ssh_list_hosts.md
├── ssh_test.go
├── ssh_list_hosts_test.go
└── README.md
```

## Testing

```bash
go test ./internal/agent/tools/plugins/ssh/...
```

The unit tests cover the parser, resolver, ban list, and path
sanitiser.  They do **not** dial out to real hosts — to smoke-test
the wire protocols use a local sshd or a `sshd -D` test fixture.
