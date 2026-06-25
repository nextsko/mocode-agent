# Structure Governance Baseline

> Baseline for the mocode structure governance roadmap (multi-review R1, 2026-06-26).

## internal/ Top-Level Packages (43)

| Package | Domain | Notes |
|---------|--------|-------|
| admin | Transport | Local admin HTTP UI (127.0.0.1) |
| agent | Core | LLM agents, coordinator, tools |
| app | Core | In-process composition root |
| authhandler | Integrations | OAuth login handlers |
| backend | Transport | Transport-agnostic RPC business layer |
| client | Transport | Remote workspace SDK |
| cmd | Transport | Cobra CLI (not slash commands) |
| commands | UI | TUI `/` slash commands (rename target: slash) |
| config | Core | Mocode.json loading |
| crawler | Utils | Used by network fetch plugins |
| cron | Dead | Zero imports — candidate for removal |
| csync | Utils | Concurrent helpers |
| diff | Utils | Diff utilities |
| errcoll | Observability | Tool error JSONL collector |
| evolution | Agent | `.mocode/patches` learning |
| filepathext | Utils | Path helpers |
| filetracker | Domain | File touch tracking interface |
| fsext | Utils | Filesystem extensions |
| gateway | Integrations | WeChat long-running entry (merge into wechat) |
| history | Domain | Prompt history interface |
| hooks | Core | PreToolUse shell hooks |
| infra | Utils | Home/data directory paths |
| knowledge | Domain | Memory + kngs templates |
| log | Observability | slog setup |
| lsp | Core | LSP client manager |
| minimax | Integrations | MiniMax provider helpers |
| orchestration | Agent | Swarm orchestration |
| permission | Core | Tool permission checks |
| proto | Transport | API DTOs |
| pubsub | Core | Internal event bus |
| server | Transport | Unix socket / named pipe RPC |
| session | Domain | Session + message models |
| skills | Core | Agent skills discovery |
| store | Persistence | JSONL file storage |
| stringext | Utils | String helpers |
| swagger | Transport | Generated OpenAPI docs |
| tools | Runtime | Shell/screencap engine (rename target: shellruntime) |
| types | Domain | Shared DTO aliases |
| ui | UI | Bubble Tea TUI |
| version | Utils | Build version |
| web | Transport | Go HTTP+WS web chat server |
| wechat | Integrations | WeChat bot + butler |
| workspace | Facade | Frontend workspace interface |

## Multi-Review P0 Checklist

- [ ] AGENTS.md architecture matches code (store not db/sqlc)
- [ ] internal/README.md navigation table exists
- [ ] Log path unified (`logs/mocode.log`)
- [ ] Memory tools wired via storeMemoryService.Tools()
- [ ] MemoryStore persists to `memory/entries.jsonl`
- [ ] Legacy session/store.go paths migrated or removed
- [ ] Web server defaults to localhost with auth scaffold
- [ ] Admin HTTP requires token
- [ ] Agent core tests re-enabled (no `//go:build ignore`)

## Multi-Review P1 Checklist

- [ ] HTTP control plane documented (`docs/architecture/control-plane.md`)
- [ ] Shared httputil middleware (logging, auth)
- [ ] gateway merged into wechat
- [ ] tools vs agent/tools naming resolved
- [ ] commands renamed to slash
- [ ] cron removed or wired
- [ ] God files split (ui.go, coordinator.go, app.go)
- [ ] Message Update debounced during streaming

## Definition of Done (Every Phase)

1. `go build -buildvcs=false .`
2. `go test` for touched packages (full `./...` when broad)
3. `task fmt`
4. `task lint:fix` (or documented nolint)
5. AGENTS.md / internal/README.md updated if boundaries changed
6. Semantic commit with emoji prefix

## Known Exceptions

- Windows: some `internal/fsext` tests may fail on path separators (pre-existing).
- `internal/session/session_snapshot.go`: stub until file-based snapshots ship.

## Related Docs

- [AGENTS.md](../../AGENTS.md)
- [internal/README.md](../../internal/README.md)
- [control-plane.md](../architecture/control-plane.md)
- [workspace-types.md](../architecture/workspace-types.md)
