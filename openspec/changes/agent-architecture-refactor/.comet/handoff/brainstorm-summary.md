# Brainstorm Summary

- Change: agent-architecture-refactor
- Date: 2026-06-29

## 已确认的事实（open 阶段）

1. 范围：工具层 + 编排接口（`internal/core/agent/tools/` + Registry/Filter）。不动 SessionAgent/Coordinator 业务逻辑、extension/evo/roundtable/candidate/failover/notify/prompt/ctxcompress。
2. 包位置：同模块顶层 `github.com/package-register/mocode/tools`（非 `internal/`）。
3. 加载：静态 import + init() + `tools.Register()`（`database/sql` drivers 风格）。
4. 迁移：一次性（PR1 脚手架 → PR2 移动 + re-export → PR3 切流 + 删旧路径）。

## 用户补充的关键洞察

- 设计**接口抽象**的真正价值是为后续**外部工具**和 **agent 作为工具**留出门路。
- 不同 framework 实现的 agent/工具可以注册到 mocode，mocode 是 host。
- **自建 Schema 类型的根因**：用户后续可能把 LLM 内核替换为 trpc-agent-go，或自实现整个 agent runtime。toolkit 必须**框架无关**——不依赖 `charm.land/fantasy` 任何类型。

## 决策汇总（脑暴完成）

### 主路径选择

- **External-tool 范围**：A — 纯 in-process，延后外部。外部工具 / agent-as-tool / 多 framework 注册**不在本次重构**，明确写入 design 的 Open Questions 作为未来 change。

### 核心接口设计

- **`Tool.Schema()`**：自建 `tools.Schema` struct（含 Name/Description/Parameters），**不引用 `fantasy` 类型**。提供 `tools/agenttool_adapter.go` 把 `[]Tool` 转为 `[]fantasy.AgentTool`，转换逻辑**仅在 agent 包**。
- **`ToolContext`**：纯 in-process 接口（SessionID/WorkingDir/Permissions/MCP/Callbacks），**不预留 transport 字段**。未来扩展外部时由 `ExternalToolContext` 单独实现。
- **权限检查位置**：在 `ToolContext.Permissions()` 内。Tool 接口本身不感知权限策略。Agent 在调用 `Execute` 前 `Permissions().Allow(name)`。
- **`agentic_fetch` / `roundtable` / `hooked_tool`**：全部留在 `internal/core/agent/`，**不下移到 tools/**。它们是 agent 级 workflow，不是通用工具。
- **核心架构原则**：**Toolkit Framework-Agnostic**。`tools/` 包不导入 `charm.land/fantasy`、`internal/core/agent`、`internal/core/config` 中任何类型。所有 provider/agent 概念通过 `tools/agenttool_adapter.go`（位于 `internal/core/agent/`）在调用边界做转换。

### 关键架构不变量

1. `tools/` 包的 `go.mod` 依赖只有 Go 标准库 + 自身子包。第三方仅可依赖 `charm.land/fantasy` 的**纯类型**（如 `LanguageModel` 在工具调用时不需要）。**实际零依赖**。
2. `tools.Tool` 接口的 `Execute(ctx, ToolContext, args)` 中 `args` 是 `json.RawMessage`，避免 schema 库绑定。
3. `tools.Schema` 是 transport-agnostic 结构体，参数部分用 `map[string]any`（在 adapter 层序列化为 JSON Schema）。
4. `Registry` 的默认 singleton 与 `NewRegistry()` 自定义并存。`Register` / `RegisterProvider` 都走同一锁。
5. `init()` 注册是**幂等**的：同名后注册者覆盖前者 + `slog.Warn`。

## 关键取舍与风险

- **延后外部工具的价值取舍**：本次不交付外部工具 / agent-as-tool，但接口抽象**专门为未来扩展设计**——Tool 接口签名（context/args/result 三段式）天然支持未来加 `Transporter` 包装。
- **自建 Schema 的开销**：需要写一个 Schema → fantasy.ToolSchema 的 adapter（~50 行）。后续若换 trpc-agent-go，只需重写 adapter。
- **agentic_fetch / roundtable 留在 agent 包**：意味着它们不能被 `tools.Default().Get()` 访问，只能由 `Coordinator` 主动注入。这是有意为之——它们是 agent-level workflow，不是通用 LLM tool。
- **零 `fantasy` 依赖 = 工具测试完全脱离 LLM runtime**。`go test ./tools/...` 不需要起 mock provider，测试更快、更确定。

## 测试策略

- **per-tool 测试**：每个 builtin/plugin 的现有 `*_test.go` 跟随其实现文件搬到 `tools/...` 下，断言维持原行为。
- **registry/loader 测试**（新增于 `tools/registry_test.go`）：
  - `init()` 时 `Register` 工作正常
  - 重复 `Register` 触发 `slog.Warn` + 后者覆盖前者
  - 并发 `Register` / `Get` 安全
  - `NewRegistry()` 与 `Default()` 互相独立
- **adapter 测试**（新增于 `internal/core/agent/agenttool_adapter_test.go`）：
  - `[]tools.Tool` → `[]fantasy.AgentTool` 转换保真
  - Schema 字段在转换中无丢失
- **集成测试**：PR3 切流后跑 `go test ./...`，所有原有 per-tool 行为必须通过。

## Spec Patch 候选

将在 design 阶段回写以下 delta spec 变更：

### `specs/tool-contracts/spec.md`

- **新增 Requirement "Schema is framework-agnostic"**：`tools.Schema` 定义在 `tools/contracts.go`，**不引用 `charm.land/fantasy` 或其他 LLM 库类型**。任何 LLM 库的 ToolSchema 通过 adapter 转换。Scenario: 改 Schema 字段不破坏 toolkit 其他包。
- **修订 Requirement "Default Registry with init() loader"**：增加"Registry 不持有任何 LLM provider 句柄"Scenario。
- **修订 Requirement "Tool interface contract"**：增加 Scenario 验证 `Tool.Execute` 不在签名中引用 `fantasy` 类型。

### `specs/agent-toolkit/spec.md`

- **新增 Requirement "Fantasy adapter"**：`internal/core/agent/agenttool_adapter.go` 把 `[]tools.Tool` 转为 `[]fantasy.AgentTool`，**此文件是 toolkit 与 fantasy 的唯一耦合点**。Scenario: 替换 adapter 实现不影响 tools/ 包。
- **修订 Requirement "Agent consumes the new registry"**：明确通过 `FantasyAdapter` 转换，agent 包不再持有 tool 列表。
- **修订 Requirement "One-line tool opt-in"**：scenario 增加"未来通过 `RegisterExternal` 加外部工具时不需要改 agent 包"（此为延伸场景，本 change 不实现 RegisterExternal）。

## 关键未知项（已澄清或延后）

1. ~~外部 tool 的 transport 协议~~ → 延后到未来 change。本次仅在 `tools/contracts.go` 留 doc comment 提示接口设计支持 future transport 包装。
2. ~~外部 tool 的发现机制~~ → 延后。
3. ~~Schema 规约：OpenAI vs Anthropic~~ → 自建 Schema 独立于两者，adapter 负责转换。
4. ~~生命周期：lazy vs eager~~ → 本次 in-process 工具无 lifecycle 概念（编译期 init 一次）。
5. ~~权限沙箱~~ → 沿用现有 `permission.Checker`，ToolContext 内统一入口。

## 下一步

1. 用户确认设计摘要
2. 写 Design Doc 到 `docs/superpowers/specs/2026-06-29-agent-architecture-refactor-design.md`
3. 回写 Spec Patch 到 `openspec/changes/agent-architecture-refactor/specs/`
4. 重新生成 handoff（hash 更新）
5. 运行 `comet-guard design --apply` 推进到 `phase: build`
