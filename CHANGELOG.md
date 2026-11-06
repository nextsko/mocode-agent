# Changelog

All notable changes to mocode are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/). The git tag is the source of truth;
`internal/util/version/version.go` mirrors it for `go install` builds.

## [0.12.0] - 2026-11-07

### Added

- **Native MCP server**: `mocode mcp serve` exposes `run` and `models` tools over
  stdio JSON-RPC (newline-delimited). Replaces the earlier external bridge
  approach; verified against DeepSeek Harness (`mcp__mocode__run`).
- Explicit DNS resolver for hosts without `/etc/resolv.conf`
  (`MOCODE_DNS_SERVER`), with unit tests for the resolver wiring.
- Apache-2.0 `LICENSE` and `NOTICE` with upstream attribution.
- Scheduled full-repository lint report (`.github/workflows/lint-full.yml`) and
  `scripts/lint-full.sh`; per-linter summary + artifact.
- `-race` CI job for concurrency-sensitive packages.
- `internal/ui/pills` rendering package extracted from the UI model, with unit
  tests.
- `internal/core/agent/llmconv` message adapter package (domain ⇄ framework
  conversion, tool-result helpers).
- Config is now composed from provider/tooling/runtime/automation sections and
  exposes a read-only `View` plus `Clone` snapshots.

### Changed

- Domain layer no longer imports the LLM framework or provider SDKs; layercheck
  enforces this, and `agent/messages` keeps the same rule.
- WeChat account manager is created at the command boundary and injected into
  the UI dependency container instead of being a package-level singleton.
- Docker MCP availability probing is encapsulated in a `dockerMCPProbe` value.
- `go.mod` language version is now 1.27 (promoted-field composite literals).
- Lint policy: errcheck gates new code; gosec G104 de-duplicated; wrapcheck
  ignores common stdlib signatures; switches with `default` count as exhaustive.
- File/dir permissions tightened to 0600/0750 across config, sessions, caches.

### Fixed

- `session_search` index is now updated on create/update/delete/truncate instead
  of only being rebuilt when empty; the FTS query itself was fixed (session join
  and snippet column) and now has consistency tests.
- CDP connections no longer leak the HTTP response body on failed dials.
- HTTP/exec call sites accept contexts (noctx debt cleared).
- Session index rebuild errors are surfaced instead of silently swallowed.
- Cleared the entire errcheck backlog (120 findings) and staticcheck/unconvert
  warnings in touched packages.

### Documentation

- New guides: native MCP server + client integration, and this changelog.
- Refreshed hotspot inventory and enforced layer rules in `tools/doc.go`.

## Earlier history

Releases before 0.12.0 are tracked by git tags only; see `git log` for details.
