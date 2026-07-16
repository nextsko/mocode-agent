# Toolkit Migration Test Matrix

## Test Gates

| Gate | Command | Required Before |
|------|---------|-----------------|
| Toolkit scaffold | `go test ./tools/...` | Any runtime cutover. |
| Legacy parity baseline | `go test ./internal/core/agent/tools/...` | Comparing behavior and before deleting legacy. |
| Agent adapter | `go test ./internal/core/agent/...` | Runtime cutover. |
| Build | `go build -buildvcs=false ./...` | End of every major phase. |
| Full repo | `go test -count=1 -short -p=2 -timeout 120s ./...` | Final migration completion. |

## Cross-Cutting Coverage

| Area | Test Required | Acceptance |
|------|---------------|------------|
| Schema conversion | Adapter test serializes `tools.Schema` with required fields. | `required` never disappears; empty-arg tools still produce valid `{}` arguments at provider boundary. |
| Error conversion | Adapter test maps `ToolResult.Error` to LLM-visible tool error. | Missing params are failures, never successful empty content. |
| Metadata conversion | Adapter test maps `ToolResult.Metadata` to legacy/fantasy metadata. | Metadata keys survive for UI/session display. |
| Attachments | Adapter test maps text/image attachments. | Image/text payloads preserve MIME and content. |
| Runtime dependencies | Test all keys required by migrated tools are installed into `agentToolContext`. | Missing service gives clear error; present service is retrievable by typed lookup. |
| Registration | `tools/builtin/all` and `tools/plugins/all` tests assert expected names. | No missing names and no duplicate active names. |
| Dependency boundaries | Script/test scans imports below `tools/...`. | No `fantasy`, `catwalk`, `internal/core/agent`, or `internal/core/config` imports. |

## Builtin Tool Matrix

| Tool | Migration Status | Behavior Risks | Required Tests |
|------|------------------|----------------|----------------|
| `bash` | Legacy-only | Banned commands, safe read-only bypass, permission request payload, command error categorization, auto-background, explicit background, TTY, truncation, `<cwd>` normalization, attribution/model description. | Missing command, safe read-only no permission, blocked command, permission denied, sync success with cwd, stderr/exit code formatting, long output truncation, auto-background starts job, explicit background fast-failure and live-job paths, metadata fields. |
| `job_input` | Migrated but not active | Error shape and metadata parity. | Missing `shell_id`, missing input unless `press_enter`, unknown shell, done shell, writes bytes, metadata. |
| `job_kill` | Migrated but not active | Description uses full markdown in both old/new; metadata on kill error. | Missing `shell_id`, unknown shell, kill success, kill error with metadata. |
| `job_output` | Migrated but not active | `bashNoOutput`, wait behavior, interactive hint, metadata parity. | Missing `shell_id`, unknown shell, no output, stdout/stderr merge, nonzero exit code, interactive running hint, wait blocks until done. |
| `edit` | Legacy-only | Read-before-edit, stale file detection, create/delete/replace modes, unique match, replace_all, CRLF preservation, permission payload, history, tracker, LSP diagnostics. | Missing file_path, create new file, existing file create error, must read before edit, stale file error, old_string not found, multiple matches, replace_all success, delete content, same content error, permission denied, metadata additions/removals, CRLF preservation, LSP notification. |
| `multiedit` | Legacy-only | Sequential edits, exact-match semantics, rollback/partial behavior, history/tracker/LSP. | Missing path, no edits, old_string not found, multiple operations order, replace_all, metadata, partial failure behavior. |
| `view` | Legacy-only | Builtin skill resource, outside-workdir permission, skill path exemption, image support, line numbers, size limit, max line length, UTF-8 validation, suggestions, file tracker, LSP diagnostics. | Missing path, file not found with suggestions, directory error, large file error, offset/limit, has-more note, invalid UTF-8, image with/without support, builtin skill file, skill tracker mark loaded, outside-workdir permission. |
| `read_files` | Legacy-only | Glob expansion, max file count, concurrent ordering, outside-workdir permissions, per-file errors, UTF-8/binary, truncation, tracker. | Empty paths, too many paths, glob no match/error, success multiple files, partial failures, directory, too large, invalid UTF-8, suggestions, permission denied, tracker records reads, metadata counts. |
| `write` | Legacy-only | Content required, stale read check, directory error, mkdir, permission payload, history, tracker, LSP diagnostics, diff metadata. | Missing path, empty content, directory target, stale file, no-op same content, new file success, overwrite success, permission denied, metadata diff, history version, tracker read, LSP notify. |
| `ls` | Legacy-only | Config options, hidden/system filtering, max entries, directory permissions, stable tree formatting. | Missing/default path behavior, nonexistent path, file path vs directory, recursive depth/config, ignore patterns, outside-workdir permission if applicable, output formatting. |

## Plugin Tool Matrix

| Group | Tools | Status | Required Tests |
|-------|-------|--------|----------------|
| Search | `glob`, `grep`, `sourcegraph` | `glob`/`grep` migrated; `sourcegraph` legacy-only | Preserve `grep` config timeout and regex cache or document intentional change; search output parity; literal text; include patterns; truncation; rg fallback. |
| Reasoning | `think` | Migrated | Metadata and required schema parity. |
| Session todos | `todos` | Migrated | Runtime dependency lookup, missing session, invalid status error mapping, just-started/completed metadata, save failure. |
| Network basic | `web_fetch`, `web_search` | Migrated | Missing URL/query, max results clamp, provider failure, large content temp file path, proxy/client behavior. |
| Network extended | `crawl`, `fetch`, `download`, `download_docs` | Legacy-only | Permissions for local writes/fetches, retry wrapper parity, timeout/client selection, binary-safe download, docs clone behavior, error formatting. |
| SSH | `ssh_exec`, `ssh_upload`, `ssh_download`, `ssh_list_hosts` | Legacy-only | Shared service lifecycle, permission payloads, host parsing, upload/download metadata, list hosts config parsing, connection pool close. |
| Git ops | `git_plan_commits`, `git_execute_commits` | Legacy-only | Status/diff parsing, grouping semantics, no auto-add-all, commit execution ordering, failure recovery. |
| Gitea | `gitea_issues`, `gitea_pulls`, `gitea_notifications` | Legacy-only | `tea` availability handling, filters, JSON parsing, limit/state behavior. |
| Diagnostics | `mocode_info`, `mocode_logs` | Legacy-only | Config/runtime fields, log path, truncation/line count, no secrets. |
| LSP plugins | `diagnostics`, `references`, `lsp_restart` | Legacy-only | AutoLSP gating, manager nil behavior, per-file/project diagnostics, restart behavior. |
| MCP meta | `list_mcp_resources`, `read_mcp_resource` | Legacy-only | Empty MCP config gating, permission checks, server not found, resource not found. |
| Session/export | `session_export`, `session_search`, `session_summary`, `message_export` | Legacy-only | Runtime dependency lookup, output paths, summary scheduler, search availability. |
| WeChat/export | `send_wechat_file`, `send_wechat_image`, `screenshot_to_wechat` | Legacy-only | Missing gateway/runtime dependency, file/image validation, permission or UX errors. |

## Cutover Tests

| Test | Acceptance |
|------|------------|
| Runtime registry parity | The set from legacy `AllToolNames()` equals new toolkit standard names plus intentional agent-owned workflow tools. |
| Provider schema request | Final provider request contains every tool schema with required `arguments` fields and JSON object schemas. |
| UI imports | UI no longer imports `internal/core/agent/tools` after deletion. |
| No dual active implementation | Runtime does not expose both old and new implementations under same name. |
| Startup/shutdown | Startable providers such as SSH close resources on shutdown. |

## First Implementation Slice

1. Add adapter tests for schema/error/metadata conversion before changing runtime.
2. Fix already migrated `grep` parity gap: restore config timeout/runtime dependency and regex cache, or document intentional change.
3. Add parity tests for `job_input`, `job_kill`, `job_output`, `think`, `todos`, `glob`, `grep`, `web_fetch`, `web_search` because they already exist in both trees.
4. Migrate remaining builtins one by one using test-first parity cases.
5. Only after builtins are green, move plugin groups by dependency order.
