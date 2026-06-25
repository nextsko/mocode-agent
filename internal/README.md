# internal/ — Package Map

Quick navigation for contributors. `internal/` is organized as layered
containers; each container holds related packages at the same architectural
layer. **CLI commands** live in `transport/cmd/`; **TUI slash commands** live
in `ui/slash/`.

## Change X → Go Y

| Want to change… | Go to… |
|-----------------|--------|
| CLI subcommands | `transport/cmd/` |
| Chat `/` commands | `ui/slash/` + `ui/model/at_completion.go` |
| Agent conversations | `core/agent/` (`coordinator.go`) |
| LLM tools | `core/agent/tools/` (`builtin/` + `plugins/`) |
| Bash execution engine | `core/shellruntime/shell/` |
| Session/message models | `domain/session/`, `domain/session/message/` |
| Persistence (JSONL) | `store/` |
| Config loading | `core/config/` |
| TUI | `ui/` — read `ui/AGENTS.md` first |
| Hooks | `core/hooks/` — see `../HOOKS.md` |
| WeChat + gateway | `integration/wechat/` + `integration/wechat/gateway/` |
| Memory/knowledge | `core/knowledge/memory/` |
| Workspace facade | `transport/workspace/` |

## Layered Containers

| Container | Layer | Packages |
|-----------|-------|----------|
| `util/` | 0 — cross-cutting primitives | `ext`, `csync`, `infra`, `fsext`, `diff`, `log`, `errcoll`, `version`, `pubsub`, `anim` |
| `domain/` | 1 — pure models | `session`, `types`, `history`, `filetracker` |
| `store/` | 2 — persistence | (JSONL repository) |
| `core/` | 3 — application core | `agent`, `app`, `config`, `permission`, `hooks`, `skills`, `knowledge`, `evolution`, `crawler`, `evaluation`, `shellruntime` |
| `transport/` | 4 — entry points | `cmd`, `admin`, `workspace` |
| `integration/` | 4 — external integrations | `wechat`, `authhandler` |
| `ui/` | 4 — TUI | (Bubble Tea; `slash/` lives here) |

Dependency direction is strictly downward: `transport`/`integration`/`ui` →
`core` → `store`/`domain` → `util`. No package imports a higher layer.

## By Domain

### Core

`core/app`, `core/agent`, `core/config`, `core/permission`, `core/hooks`,
`util/pubsub`, `core/skills`

### Persistence

`store` (JSONL), `domain/session` (domain types), `domain/filetracker`,
`domain/history`

### Transport

`transport/cmd`, `transport/admin`, `transport/workspace`

### Integrations

`integration/wechat`, `integration/authhandler` (`gateway/` lives under `wechat`)

### UI

`ui` (Bubble Tea TUI)

### Runtime Utilities

`core/shellruntime` (shell/screencap), `util/fsext`, `util/ext`, `util/csync`, `util/anim`,
`util/infra`, `util/log`, `util/errcoll`

### Agent Extensions

`core/agent/evolution` (self-evolution), `core/knowledge`, `core/crawler`

## Naming Disambiguation

| Name | A | B |
|------|---|---|
| `transport/cmd` vs `ui/slash` | Cobra CLI | TUI `/` commands |
| `core/shellruntime` vs `core/agent/tools` | Bash engine | LLM tool plugins |
| `domain/session` vs `store` | Domain model | JSONL persistence |

## Related

- [AGENTS.md](../AGENTS.md)
- [docs/architecture/control-plane.md](../docs/architecture/control-plane.md)
- [docs/architecture/workspace-types.md](../docs/architecture/workspace-types.md)
