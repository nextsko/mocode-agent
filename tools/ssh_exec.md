Run a shell command on a remote host over SSH.

Use this tool when you need to inspect or modify state on a machine you
have SSH access to (servers, dev boards, cloud VMs, containers, etc.).

The `host` argument accepts any of:

- An alias defined in `~/.ssh/config` (preferred — keeps real hostnames
  and credentials out of the prompt).
- `user@host`
- `user@host:port`

The `command` is passed to a non-interactive shell.  It is subject to a
small built-in deny list (e.g. `rm -rf /`, `mkfs.*`, `shutdown`) that is
checked before the request is even shown to the user.

Authentication is tried in this order:

1. `SSH_AUTH_SOCK` (ssh-agent)
2. `~/.ssh/id_ed25519`, `id_rsa`, `id_ecdsa`, `id_dsa`
3. `IdentityFile` from the matched `~/.ssh/config` entry
4. Password (only when explicitly configured — never sent through the LLM)

The default command timeout is 30 seconds; pass `timeout_secs` to change
it.  On timeout the remote process receives `SIGTERM` and the response
is marked with `timed_out: true` and `exit_code: 124`.
