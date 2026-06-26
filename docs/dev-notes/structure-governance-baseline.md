# Structure Governance Baseline

> Baseline for the mocode structure governance roadmap (multi-review R1, 2026-06-26).

## internal/ Top-Level Packages (43)

| Package | Domain | Notes |
|---------|--------|-------|
| admin | Transport | Local admin HTTP UI (127.0.0.1) |
| agent | Core | LLM agents, coordinator, tools |
| app | Core | In-process composition root |
| authhandler | Integrations | OAuth login handlers |
| backend | Transport | Removed — was RPC business layer for daemon |
| client | Transport | Removed — was remote workspace SDK |
| cmd | Transport | Cobra CLI (not slash commands) |
| commands | UI | TUI `/` slash commands (rename target: slash) |
| config | Core | Mocode.json loading |
| crawler | Utils | Used by network fetch plugins |
| cron | Dead | Zero imports — candidate for removal |
| csync | Utils | Concurrent helpers |
| diff | Utils | Diff utilities |
| errcoll | Observability | Tool error JSONL collector |
| evolution | Agent | `.mocode/patches` learning |
| ext | Utils | Stdlib extensions (string + path helpers; merged from stringext + filepathext) |
| filetracker | Domain | File touch tracking interface |
| fsext | Utils | Filesystem extensions |
| gateway | Integrations | WeChat long-running entry (merge into wechat) |
| history | Domain | Prompt history interface |
| hooks | Core | PreToolUse shell hooks |
| infra | Utils | Home/data directory paths |
| knowledge | Domain | Memory + kngs templates |
| log | Observability | slog setup |
| lsp | Core | LSP client manager |
| permission | Core | Tool permission checks |
| proto | Transport | API DTOs |
| pubsub | Core | Internal event bus |
| server | Transport | Removed — was Unix socket / named pipe RPC |
| session | Domain | Session + message models |
| skills | Core | Agent skills discovery |
| store | Persistence | JSONL file storage |
| stringext | Utils | Merged into `ext` (2026-06-26) |
| swagger | Transport | Removed — was generated OpenAPI docs |
| tools | Runtime | Shell/screencap engine (rename target: shellruntime) |
| types | Domain | Shared DTO aliases |
| ui | UI | Bubble Tea TUI |
| version | Utils | Build version |
| web | Transport | Removed — was browser chat HTTP+WS server |
| wechat | Integrations | WeChat bot + butler |
| workspace | Facade | Frontend workspace interface |

## Multi-Review P0 Checklist

- [ ] AGENTS.md architecture matches code (store not db/sqlc)
- [ ] internal/README.md navigation table exists
- [ ] Log path unified (`logs/mocode.log`)
- [ ] Memory tools wired via storeMemoryService.Tools()
- [ ] MemoryStore persists to `memory/entries.jsonl`
- [ ] Legacy session/store.go paths migrated or removed
- [ ] Admin HTTP requires token
- [ ] Agent core tests re-enabled (no `//go:build ignore`)

## Multi-Review P1 Checklist

- [ ] HTTP control plane documented (`docs/architecture/control-plane.md`)
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

## Multi-Review R2 (2026-06-26) - core/domain/util landed

Status: restructuring applied + ui/anim -> util/anim; `go build ./...` green;
`go test ./...` green except the Windows `util/fsext` path-separator caveat.
`gofmt -l .` clean. Zero stale old-path imports remain repo-wide.

Findings:

- P0: none blocking. `main.go` import updated correctly to
  `internal/transport/cmd` (verified minimal diff).
- P1 (partially addressed): layer invariants documented but NOT lint-enforced.
  Upward deps from `core/` (pre-existing, exposed by the move):
  - `core/app` -> `ui/styles` (styles depends on `ui/diffview`; invert via DI).
    FIXED: `core/app` -> `ui/anim` removed by moving `ui/anim` -> `util/anim`
    (anim was a pure spinner, only dep `util/csync`).
  - `core/agent/tools/plugins/{screenshot_to_wechat,send_wechat_image,
    send_wechat_file}` -> `integration/wechat` (legit integration boundary).
  Fix options: invert app->ui via DI; add a `depguard` lint rule to lock the
  layer boundaries going forward (no depguard in `.golangci.yml` yet).
- P2: `core/agent` is aggregating many concerns (agent + candidate + failover
  + evolution + tools + roundtable + subagent_cards). Watch for god-package drift.
- P2: 333 files in one logical change; prefer layered commits
  (util -> domain -> store -> core -> transport) for reviewability.
- P2: `util/fsext` path-separator tests remain a permanent Windows CI hazard.


## Absorption: trpc-agent-go evolution quality-gates (2026-06-26)

Created `internal/core/agent/evolution/gates/` sub-package adapting trpc-agent-go
gate chain to the mocode Patch model. Self-contained (no import cycle): defines
Candidate/Outcome/Report/Verdict types, a Pipeline running gates in fixed order
(SpecGate -> SafetyGate -> EffectivenessGate -> HumanGate), and default impls.
SafetyGate ships high-precision secret/shell/path-traversal regex sets (one
improvement over the source: `rm -rf /` anchor relaxed to drop trailing \\b so the
bare-root case is caught). 19 tests pass.

Wired into Producer via `WithGates(pipeline)` option + `gateCandidate` helper;
both emit paths (emitRulePatch, produceQualityPatch) now validate candidates
before persistence, so bad patches (secrets, dangerous patterns, failed-session
output) never reach the system-prompt injection path. Nil pipeline = legacy
direct-publish (backward compatible).

## Pre-existing build break (not from this work)

`internal/core/shellruntime/shell/shell.go` has an in-flight `ExecStreamWithStdin`
feature that changed `newInterp` signature to take `io.Reader` but left a call site
at line 336 unupdated -> `go build ./...` fails there. This is unrelated to the
evolution/gates work (which builds and tests green). Do not revert; fix the call
site to pass stdin separately.


## Absorption: trpc-agent-go web-search providers (2026-06-26)

web_search was hardcoded to a single DuckDuckGo HTML scraper with no fallback.
Added a `Provider` abstraction in netcommon so the plugin can run a fallback
chain instead of being locked to one backend:

- `Provider` interface (Name + Search) + `MultiProvider` (tries providers in
  order, first non-empty/error-free result wins, last error surfaced if all fail).
- `DuckDuckGoProvider` wraps the existing HTML-lite scraper (backward compat).
- `DuckDuckGoInstantAnswerProvider` queries the Instant Answer API for structured
  answers/abstracts/definitions (adapted from trpc-agent-go duckduckgo tool).
- `DefaultSearchProvider()` returns the chain: HTML scraper first (richest
  snippets), Instant Answer API as factual/structured fallback.

web_search.go: NewWebSearchTool(client) now uses the default chain;
NewWebSearchToolWithProvider(client, provider) allows a custom chain. All
existing callers (agentic_fetch_tool.go, compat.go) compile unchanged.
7 provider tests pass; main binary builds green.


## Layer enforcement: scripts/layercheck (2026-06-26)

The R2 review flagged that layer invariants were documented but not enforced.
A golangci-lint v2 depguard rule was attempted but rejected: the installed
golangci-lint is v1.64.8 and cannot read the existing `version: "2"` config
("you are using a configuration file for golangci-lint v2 with golangci-lint v1").
That binary/config skew is pre-existing and out of scope. Instead, a
version-independent verifier was added: `go run ./scripts/layercheck` parses
`go list` output and fails (exit 1) on any upward dependency. It currently
reports the 4 known violations and confirms the ui/anim -> util/anim fix held:

- core/agent/tools/plugins/{screenshot_to_wechat,send_wechat_file,send_wechat_image} -> integration/wechat
- core/app -> ui/styles

Unit tests cover layerOf() and extractImports(). The remaining violations are
tracked below; fix them and layercheck should report zero.


## Layer coupling: ALL violations eliminated (2026-06-26)

layercheck now reports ZERO upward dependency violations. All four edges
flagged by the R2 review are resolved via dependency inversion through
domain-layer ports:

- WeChat tools (screenshot_to_wechat, send_wechat_file, send_wechat_image):
  use domain/messenger port. integration/wechat provides the adapter
  (AsMessenger); coordinator injects it via SetMessenger. Fixed a nil-return
  bug in messenger_adapter.go (ActiveSender now returns messenger.Sender,
  the nil-able interface, not the value type).
- core/app -> ui/styles: new domain/theme port (SpinnerThemer +
  SpinnerColors). ui/styles provides the adapter (SpinnerThemer struct);
  app.SetSpinnerThemer wires it from transport/cmd, mirroring SetMessenger.

Dependency direction is now strictly downward everywhere:
  transport/integration/ui -> core -> store/domain -> util.
Verify with: go run ./scripts/layercheck (exit 0 = clean).


## Multi-Review R3 (2026-06-26) - structure stable, all areas absorbed

State verified: go build ./... green (clean cache), layercheck = 0 upward
violations, gofmt clean across the repo. All three absorption areas named in
the objective are present and tested:

- agent advancement: core/agent/extension (Manager + lifecycle Events, adapts
  trpc-agent-go PluginManager/Callbacks; has panic recovery so a misbehaving
  extension cannot break the core loop).
- web search: netcommon.Provider + MultiProvider fallback chain + Instant
  Answer API provider (7 tests).
- self-evolution: evolution/ (patch learning + gates/ quality-gate pipeline,
  19 tests) + evo/ (/evo self-iteration mode + fixation store modeled on
  trpc-agent-go CandidateStore/ActivePointer).

Findings:

- P0: none. Build green, layering enforced, no violations.
- P1 (RESOLVED): extension.Manager.RegisterOrError rejects shadowing duplicates
  (mirrors trpc-agent-go Collect invariant); MustRegister panics at construction
  time; coordinator.RegisterExtension now uses RegisterOrError and logs on
  rejection instead of silently replacing. 6 tests cover the behavior.
- P2: evo/ and evolution/ are complementary (no import coupling, different
  responsibilities: evo = self-iteration mode runtime, evolution = patch
  learning loop). Keep separate; do not merge.
- Info: 7 files were unformatted (concurrent worktree edits); ran gofumpt,
  repo now 100% gofmt-clean.


## Defect fix: extension AbortRun now honored (2026-26)

The extension Decision.AbortRun field documents that it "requests the
coordinator stop the run" (guardrails: budget limits, safety stops). Audit
found it was only LOGGED at the fireExtensions site and never acted on — a
safety-stop extension would silently fail to stop the run.

Fixed: fireExtensions now returns the *extension.Decision, and the Run
method honors a BeforeRun AbortRun by returning early with a descriptive
error ("run aborted by extension: <reason>") before any work begins. This is
the only safe abort point; after work starts the agent owns the turn.

Architectural note from the same audit: trpc-agent-go todoenforcer operates
at the model-step level (inspect every final response, flip Done). mocode
extension fires at the run level (once per coordinator.Run). The hard
todo-contract enforcement would need fantasy-agent callbacks (model-step
granularity), a separate larger effort; documented so it is not attempted at
the wrong layer.


## Absorption: todo nudge (soft todoenforcer) (2026-26)

Absorbed the soft variant of trpc-agent-go todoenforcer: buildTodoNudge
produces a system-message reminder of open (pending/in-progress) todos,
injected at each model step (PrepareStep) alongside error corrections.
This mirrors how errorLearner.CorrectionContext is injected and empirically
reduces early-exit failures by surfacing open work at every step.

Design note: the hard variant (flip Done to prevent exit while todos open)
needs model-step Done-flipping inside the fantasy loop, which is a separate
larger effort. The soft nudge is the feasible, bounded form that fits the
existing hook surface without fantasy internals. 4 tests cover it.


## Consolidation: shared HTTP client (2026-26)

web_search, web_fetch, fetch, sourcegraph, and download each duplicated the
same ~8-line HTTP client+transport setup (MaxIdleConns=100, MaxIdleConnsPerHost=10,
IdleConnTimeout=90s, with only the request timeout varying). Extracted into
netcommon: DefaultHTTPClient() (30s timeout) + NewHTTPClient(timeout) for the
download variant (5min). The duplicated transport boilerplate now lives in
exactly one place (netcommon.sharedTransport), down from 5 copies (~40 lines
removed). Each plugin keeps its client-injection point (if client == nil).


## Hot-path fix: NewPatchStore no longer MkdirAll (2026-26)

injectEvolutionContext runs on every coordinator.Run (every agent turn). It
constructed a fresh PatchStore via NewPatchStore, which called os.MkdirAll
to create the patches directory — even when only reading. On a fresh install
with no evolution data this created an empty directory on every single turn.

Fixed: NewPatchStore is now a pure constructor (no I/O). The read path
(List) already tolerates a missing dir (returns nil), and the write path
(CreatePatch) creates the directory tree on demand via its subdir loop.
Eliminates a needless syscall per agent turn with zero behavior change.


## Gates wired into production (2026-26)

The evolution quality-gates (evolution/gates) were built and tested (19 tests)
and the WithGates Producer option existed, but cmd/evolve.go — the actual
invocation point — did NOT use them. The safety gates were available but inactive.

Fixed: cmd/evolve.go now constructs a gates.Pipeline (SpecGate + SafetyGate)
and passes it via WithGates. This makes the secret/shell/path-traversal
filtering active in production — a recurring error that leaks a secret
(e.g. an API key in the error body) is now BLOCKED before persistence,
rather than persisted as a patch and injected into every future system prompt.

Two follow-on fixes discovered while wiring:
- gate rejection now soft-skips (logs + continues to next cluster) instead of
  hard-aborting the entire Produce run (the original wiring returned an error
  that propagated as a fatal failure; the doc said "skip" but the code said "abort").
- Produce loop now skips empty IDs from soft-skipped patches instead of
  appending them to the created list.

Proven end-to-end: gated_e2e_test seeds a session error containing a GitHub
token, calls Produce with gates, asserts 0 patches created and empty context.


## R4 audit: rm_rf safety regex precision fix (2026-26)

R4 multi-review of authored code found a false-positive risk in the
SafetyGate rm_rf_root regex. The original pattern `rm -rf /\b` (from
trpc-agent-go) missed the bare `rm -rf /` case (slash + space has no word
boundary), so I dropped the trailing `\b` to catch it. But that over-
corrected: `rm -rf /tmp/build` (a legitimate cleanup) also matched.

Refined into two precise patterns: bare root `rm -rf /(?:\s|$)` and
destructive system dirs `rm -rf /(?:usr|etc|bin|sbin|var|root|lib|boot|...)`.
Now catches genuinely destructive paths while allowing /tmp, /home, and
relative cleanups. Added TestSafetyAllowsLegitTmpCleanup with 4 cases
locking in the precision. Verified empirically against 9 inputs.

P0: none. P1: this precision issue (fixed). All gates tests pass.

## Related Docs

- [AGENTS.md](../../AGENTS.md)
- [internal/README.md](../../internal/README.md)
- [control-plane.md](../architecture/control-plane.md)
- [workspace-types.md](../architecture/workspace-types.md)
