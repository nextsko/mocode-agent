# Changelog

All notable changes to mocode are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/). The git tag is the source of truth;
`internal/util/version/version.go` mirrors it for `go install` builds.

## [0.19.0] - 2026-11-09

### Fixed

- **memgraph Explain 串链**: 证据链路径改为全程携带节点 ID，不再按名字反查；
  同名节点各自解析到自己的证据，输出按置信度+路径稳定排序。
- **同进程并发**: 同一项目目录共享一个 Store，避免并发工具调用在内存图与
  快照写入上互相覆盖。
- 新增同名不同 ID 的确定性回归测试。
- **Navigate 丢事件竞态**: 先订阅 `Page.loadEventFired` 再发 Page.navigate，
  避免快页面在“响应返回→订阅”之间触发 load 导致等待 15s 超时（并行测试下会偶发）。

- 真实子代理验证：用 mocode 自身作为 DSH 子代理完成“只读分析”和“写码+跑测试”
  两个任务，并据其反馈修复上述问题。

## [0.18.0] - 2026-11-09

### Added

- **图谱记忆（memgraph）**: 可追溯的结构化记忆图。节点（user/persona/preference/
  fact/goal/decision/task/skill/project/event/artifact）与边（prefers/knows/decided/
  supersedes/contradicts/evidenced_by/…）均带证据（来源+引用+时间）与置信度；
  支持合并观测、时间有效性、冲突检测、supersede 演化、陈旧衰减。
- **人格与用户画像卡**: `memgraph action=persona` 输出 prompt-ready 卡片
  （角色/特质/偏好/目标/待澄清冲突）。
- **证据链解释**: `explain` 回答“为什么相信这个”，输出路径 + 原始证据。
- **导出可视化**: graph.json / graph.mmd / graph.html（Mermaid）。
- 追加式事件日志 events.jsonl，支持审计与回放；forget 支持隐私删除。

## [0.17.0] - 2026-11-09

### Added

- **能力飞轮（flywheel）**: 长任务归档 → 最简成功路径蒸馏 → 技能判定
  （create/update/upgrade/skip，引擎按证据执行，不手改版本）→ 复用记分 →
  报表与 Mermaid 导出；技能带版本历史与成功率。
- **上下文自调控入口**：`flywheel action=context` 返回预算内的紧凑摘要，
  模型按需拉取细节，避免整段历史进上下文。
- 设计吸收 Voyager / Reflexion / MemGPT / A-MEM 的技能库、语言化反馈、
  分层记忆与动态链接模式；文档见 `docs/guides/14-flywheel.md`。

## [0.16.0] - 2026-11-09

### Added

- **审美进化闭环**: `ui_dissect action=diff` 现在会把每次回归的分数变化写入
  `.mocode/ui/evolution.jsonl`；`action=retro` 增加“进化记录”（改善/退化/平均 Δ）；
  新增 `action=evolve` 把复盘沉淀为 learn-ready 的规则手册 skill 草稿（ui-taste-rules）。
- 真实验证：两页真实页面 map → diff → retro → evolve 全链路通过。

## [0.15.0] - 2026-11-09

### Added

- **动态 UI 流（ui_dissect action=dynamic）**: 以 click/fill/press/hover/scroll/wait 步骤驱动真实页面，
  每步测量状态并对比上一状态的盒子变化；需要浏览器，静态兜底会明确拒绝。
- **审美复盘（ui_dissect action=retro）**: 跨页聚合 findings 与 0–100 审美分，
  输出分数趋势、重复问题、以及沉淀出的规则；每次 map 自动刷新，
  产物 `.mocode/ui/taste.md|json` + 带时间戳的历史快照。
- 地图新增 `score` 字段；`system`/`taste` 的加载会忽略非地图 JSON（brand/system/taste）。

## [0.14.0] - 2026-11-09

### Added

- **UI 能力扩展（ui_dissect）**: 在 0.13.0 的 map/review/replicate 之上补齐闭环：
  - 跨页组件指纹聚类 + 站点设计系统（`action=system`）；
  - 盒子级视觉回归 diff（moved/resized/restyled/added/removed/component）；
  - 品牌令牌基线与 off-brand 对比（`brand` / `tokens`）；
  - 多框架复刻骨架（HTML / React / Vue / Svelte）；
  - learn 可直接消化的 skill 草稿（`skill`）；
  - 无浏览器时的静态 HTML 兜底（goquery 解析，结构与语义可用）。
  真实页面验证覆盖 example.com 与 go.dev/learn 的双页聚类、diff、令牌对比、四框架复刻与 skill 草稿。

## [0.13.0] - 2026-11-09

### Added

- **UI 审查与复刻（ui_dissect）**: 浏览器能力升级为结构理解。新工具 `ui_dissect`
  用 CDP 测量真实页面（DOM 路径 / 盒子 / 计算样式），切成组件（重复卡片、地标、
  大区块），产出审美 findings（对比度 / 字号阶梯 / 间距节奏 / 色板 / 触控目标 /
  溢出 / alt / h1）、设计令牌，并可按组件 id 生成 HTML+CSS 骨架；结果沉淀到
  `.mocode/ui/`。设计文档见 `docs/guides/13-ui-dissect.md`。

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
- Tool contracts moved to the leaf `core/tools/contract` package (re-exported
  as `tools.*` aliases); an `agenttooladapter` converts `tools.Tool` into the
  runtime `fantasy.AgentTool`; all twelve agent tools are migrated and that
  package no longer imports the framework (layercheck enforces it); the
  every tool tree (fs, web, shell, sandbox, question, lsp, filter, ssh, gitea,
  gitops, browser, wechat) is ported, with layercheck forbidding the framework
  in each package; the screenshot tool and the dynamic MCP bridge (image/media
  payloads now travel as ToolResult attachments) are ported too. The adapter now emits
  legacy-compatible flat metadata and preserves StopTurn on tool-level errors
  (permission denials still end the turn). The coordinator supplies a per-call
  context with session id, working directory and a prompting permission
  checker. layercheck keeps the contract package dependency-free (leaf).
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
