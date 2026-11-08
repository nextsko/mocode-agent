# Changelog

All notable changes to mocode are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/). The git tag is the source of truth;
`internal/util/version/version.go` mirrors it for `go install` builds.

## [0.25.0] - 2026-11-09

### Added

- **ACP 客户端文件系统桥接**: 会话声明 `fs.readTextFile` / `fs.writeTextFile` 后，
  `view` 读取与 `write` 写入走客户端（虚拟/未保存文件），本地磁盘不更新；
  结果明确标注 `acp-client-fs`，避免 agent 用本地 bash 误验证。
- 按会话注册/注销客户端 FS，关闭会话自动清理。
- 真实 E2E：客户端提供虚拟 `/virtual/hello.txt`，agent 读取后写入 `/virtual/out.txt`，
  客户端收到内容且含 HELLO_FS。

## [0.24.0] - 2026-11-09

### Added

- **TUI Knowledge 命令组**: `/flywheel`、`/context`、`/persona`、`/conflicts`，
  直接触发对应工具并汇报，知识库不再只能靠自然语言调用。
- **Admin `/api/knowledge`**: 返回飞轮报告与记忆图节点/边计数，面板可展示累积状态。
- 文档补充 ACP 能力边界：本地工具不委派客户端 fs/terminal（同机工作区设计）。

## [0.23.0] - 2026-11-09

### Added

- **ACP plan 更新**: 复用 mocode 既有的 `todos` 工具（session.Todos）作为唯一数据源，
  实时映射为 `plan` 更新；不新增重复的 plan 工具。
- **session/load 回放扩展**: 除文本外，回放 tool_call / tool_result 历史。
- 真实 E2E：agent 调用 todos 设置两步计划 → 客户端收到 `plan`（in_progress + pending）；
  load 回放包含工具事件。

## [0.22.0] - 2026-11-09

### Added

- **ACP 补齐工具完成态**: tool 角色的结果消息现在也会转成 `tool_call_update`
  （completed/failed），不再只有 pending 的 `tool_call`。
- **HTTP / SSE MCP**: `session/new` 的 MCP servers 支持 stdio/HTTP/SSE（含 headers），
  initialize 如实声明 `mcpCapabilities.http/sse`。
- **reject_always 会话内记忆**: 选择 always reject 后，同一工具在本会话后续请求
  不再打扰客户端，直接拒绝。
- E2E 复验：最小 stdio MCP server 挂载后 `toolUpdates=2`（创建 + 完成），ECHO 成功。

## [0.21.0] - 2026-11-09

### Added

- **ACP 对齐官方实现**：
  - `session/load` 先回放历史（user/agent chunk）再响应，符合 v1 规范；
  - 按会话挂载并连接客户端传来的 **stdio MCP servers**（env 支持，关闭时清理）；
  - 权限选项改为官方四种 kind（allow_once/allow_always/reject_once/reject_always），
    optionId 对齐示例；
  - `session/cancel` 会同时取消挂起的权限请求，避免客户端等不到 cancelled。
- 真实 E2E 新增：`session/load` 回放验证、最小 stdio MCP server 挂载并由 agent
  实际调用（echo_tool 返回 ECHO:HELLO）。

## [0.20.0] - 2026-11-09

### Added

- **ACP v1（`mocode acp`）**: 完整实现握手、会话生命周期（new/load/resume/list/
  delete/close）、prompt/cancel、模式与模型配置、流式 session/update
  （消息/思考/tool_call/命令/模式/配置）、权限桥 `session/request_permission`。
- **真实端到端验证**: 最小 ACP 客户端拉起 mocode，完成 initialize → session/new →
  prompt("只回复 OK") 流式返回；并验证 bash 工具经 permission 批准后执行、
  结果回传（tool_call_update）。
- **memgraph recall 多词 AND 查询**: 多词查询要求全部命中，避免跨节点误匹配。

### Known gaps

- ACP `mcpServers` 未按会话动态挂载；`session/load` 不回放历史；
  客户端 fs/terminal/elicitation 未被本地工具使用；未发送 plan 更新。

## [0.19.2] - 2026-11-09

### Fixed

- **冲突判定语义**: 只有功能性关系（timezone/language/name/role/…）才视为单值冲突，
  `prefers`、`relates_to` 等多值关系不再被误报为“待澄清”；
  事实（kind=fact）同 key 不同 value 仍会报冲突。
- 由子代理真实跑图谱自测时发现：用户同时偏好两件事被误判为矛盾。

## [0.19.1] - 2026-11-09

### Fixed

- **CJK 名称 ID 冲突**: 纯中文/非 ASCII 名称经 slug 后曾退化为 `preference` 之类，
  多条偏好会合并成同一节点；现在追加原始名称的稳定哈希后缀，保证唯一。
- **图谱视图 ID 误导**: Mermaid 节点标签现在同时显示真实 id（`id=...`），
  子代理不会再照抄安全化后的下划线标识去调用 relate。
- 以上问题由 mocode 自身作为子代理真实使用图谱时发现并回归验证。

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
