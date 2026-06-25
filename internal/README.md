# internal/ — Package Map

Quick navigation for contributors. **CLI commands** live in `cmd/`; **TUI slash
commands** live in `slash/` (formerly `commands`).

## Change X → Go Y

| Want to change… | Go to… |
|-----------------|--------|
| CLI subcommands | `cmd/` |
| Chat `/` commands | `slash/` + `ui/model/at_completion.go` |
| Agent conversations | `agent/` (`coordinator.go`) |
| LLM tools | `agent/tools/` (`builtin/` + `plugins/`) |
| Bash execution engine | `shellruntime/shell/` |
| Session/message models | `session/`, `session/message/` |
| Persistence (JSONL) | `store/` |
| Config loading | `config/` |
| TUI | `ui/` — read `ui/AGENTS.md` first |
| Hooks | `hooks/` — see `../HOOKS.md` |
| LSP | `lsp/` |
| WeChat + gateway | `wechat/` + `wechat/gateway/` |
| Memory/knowledge | `knowledge/memory/` |
| Workspace facade | `workspace/` |

## By Domain

### Core

`app`, `agent`, `config`, `permission`, `hooks`, `pubsub`, `skills`

### Persistence

`store` (JSONL), `session` (domain types), `filetracker`, `history`

### Transport

`cmd`, `proto`, `admin`, `workspace`

### Integrations

`wechat`, `authhandler`, `gateway` (merged into `wechat`)

### UI

`ui` (Bubble Tea TUI)

### Runtime Utilities

`shellruntime` (shell/screencap), `fsext`, `ext`, `csync`,
`infra`, `log`, `errcoll`

### Agent Extensions

`evolution`, `knowledge`, `crawler`

## Naming Disambiguation

| Name | A | B |
|------|---|---|
| `cmd` vs `slash` | Cobra CLI | TUI `/` commands |
| `shellruntime` vs `agent/tools` | Bash engine | LLM tool plugins |
| `session` vs `store` | Domain model | JSONL persistence |

## Related

- [AGENTS.md](../AGENTS.md)
- [docs/architecture/control-plane.md](../docs/architecture/control-plane.md)
- [docs/architecture/workspace-types.md](../docs/architecture/workspace-types.md)
