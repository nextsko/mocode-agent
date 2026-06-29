---
change: agent-architecture-refactor
design-doc: docs/superpowers/specs/2026-06-29-agent-architecture-refactor-design.md
base-ref: a915455dfa1b56cd2046b8ac726cb6f5d4335e34
---

# 实施计划：Agent Architecture Refactor

> **执行指南**：本计划将 Design Doc §8 的三段式拆分落地为可勾选的任务。每个任务工作量约 1–4 小时，改动量 < ~200 行。所有"导入重写类"任务在 PR2 末尾用脚本一次性处理，不拆为独立任务。
>
> **前置阅读**：实施者必须先读 `docs/superpowers/specs/2026-06-29-agent-architecture-refactor-design.md`，本计划不重复其中的接口语义。

---

## Overview

本计划将 `internal/core/agent/tools/` 下 40+ 工具搬迁到顶级包 `github.com/package-register/mocode/tools/`，并通过 `Tool` / `ToolProvider` / `ToolContext` 接口让工具集**框架无关**。`Coordinator` 与 `charm.land/fantasy` 的耦合点收敛到 `agenttool_adapter.go` 一个文件，为将来切换到 `trpc-agent-go` 或自研 kernel 留出一行式可改的接缝。迁移分三个 PR（契约脚手架 → 实现搬迁 → 切换删除），每个 PR 都保持 `go build ./...` 与 `go test ./...` 全绿。

---

## Current State（基线事实）

执行者需在动手前确认以下事实，这些是本计划的依赖基线。

| 事实 | 路径 | 影响 |
|---|---|---|
| 工具集大本营 | `internal/core/agent/tools/` | 9 个子目录 + 多个根级 `.go` 文件 |
| 内置工具 | `internal/core/agent/tools/builtin/{bash,edit,view,ls,write,multiedit,read_files,job_input,job_kill,job_output}/` | 10 个，逐个 `New<Name>Tool(...)` 工厂 |
| 插件工具 | `internal/core/agent/tools/plugins/` | 30+ 个，含 `sshcommon` / `gitopscommon` / `giteacommon` / `netcommon` / `searchcommon` 五个共享 helpers |
| MCP 桥 | `internal/core/agent/tools/mcp/` | 状态机 + `GetStates()` 包级函数，被 `agent_lifecycle.go` 直接调用 |
| LSP 桥 | `internal/core/agent/tools/lsp/` | `Manager` 持有客户端；`diagnostics` / `references` / `lsp_restart` 三个插件依赖 |
| 过滤器 | `internal/core/agent/tools/filter/` | 当前基于 `fantasy.AgentTool`，需重写为基于 `tools.Tool` |
| 共享重导出 | `internal/core/agent/tools/compat.go` + `tools.go` | 大量类型别名与 `var NewXxxTool = ...` 函数别名 |
| 根级文件 | `registry.go`、`registry_filter.go`、`tools.go`、`compat.go`、`fetch_types.go`、`transfer.go`、`mcp-tools.go` | 持有 `ToolDescriptor` / `ToolPlugin` / `AllToolNames` / `TransferToolName` 等 |
| Agent 消费点 | `coordinator.go`、`agent.go`、`agent_lifecycle.go`、`hooked_tool.go`、`agentic_fetch_tool.go`、`roundtable_tool.go` | 通过 `a.tools.Copy()` 拿 `[]fantasy.AgentTool` 切片运行时分发 |
| 关键 god file | `internal/ui/model/ui.go` (~3240 行) | 未来可能调整，本计划不在其上改动；但其引用的 `tools.BashToolName` 等符号受 compat.go 重导出保护，PR3 后路径需一次性同步 |

**新顶级包目标位置**：`tools/`（模块路径 `github.com/package-register/mocode/tools/`），与 `internal/core/agent/tools/` **同模块但不同包**——这是 PR2 期间出现两个 `tools` 导入路径并存的根本原因。

---

## File Map（PR3 完成后的稳态）

| 新文件 | 职责 |
|---|---|
| `tools/doc.go` | 包文档，说明 toolkit 目的与导入政策 |
| `tools/contracts.go` | `Tool`、`ToolProvider`、`ToolContext`、`ToolResult`、`Schema`、`Capability`、`PermissionChecker`、`MCPHandles`、`ToolCallbacks`、`Attachment` |
| `tools/registry.go` | `Registry` 结构 + `NewRegistry`、`Register`、`RegisterProvider`、`Get`、`Names`、`All` |
| `tools/loader.go` | init() 注册模式 + 重复名 `slog.Warn` |
| `tools/filter/filter.go` | 重写后的过滤器（基于 `tools.Tool`，非 `fantasy.AgentTool`） |
| `tools/filter/filter_test.go` | 迁移并扩展现有测试 |
| `tools/registry_test.go` | 注册表 + loader 测试 |
| `tools/builtin/all/all.go` | umbrella 空白导入 10 个 builtin |
| `tools/plugins/all/all.go` | umbrella 空白导入 30+ 插件 |
| `tools/builtin/<name>/<name>.go` | 搬迁的内置工具 |
| `tools/plugins/<name>/<name>.go` | 搬迁的插件 |
| `tools/mcp/{init,prompts,resources,tools}.go` | 搬迁的 MCP 桥 |
| `tools/lsp/{client,handlers,manager,util/...}.go` | 搬迁的 LSP 桥 |
| `internal/core/agent/agenttool_adapter.go` | **唯一**的 `charm.land/fantasy` 导入点 |

---

## PR1 — Contracts + Scaffold（契约 + 脚手架）

**目标**：`tools/` 包就位、空编译通过、依赖边界 CI 启用。**不改任何调用点**；agent 仍走旧路径。

**入口验证命令**：

```bash
go build -buildvcs=false ./tools/...
go test -race ./tools/...
go list -deps ./tools/... | grep -E "(fantasy|catwalk|internal/core/(agent|config))"   # 必须空
```

---

### 1.1 创建 tools/ 目录与 doc.go（scaffold 组）

**文件**：

- 新建 `tools/doc.go`

**验收**：`go doc github.com/package-register/mocode/tools` 可见包说明；`go build ./tools/...` 通过。

**依赖**：无。

**Commit**：`feat(tools): scaffold toolkit package with package doc`

---

### 1.2 定义核心契约 `contracts.go`（scaffold 组）

**文件**：

- 新建 `tools/contracts.go`

**内容要点**（完全照 Design Doc §5）：

- `Tool` 接口四方法（`Name` / `Description` / `Schema` / `Execute`）
- `ToolProvider` 接口（`Name` + `Tools() []Tool`）
- `ToolContext` 接口五方法
- `PermissionChecker` / `MCPHandles` / `ToolCallbacks` 子接口
- `Schema` 结构体（`Name` + `Description` + `Parameters map[string]any`）
- `ToolResult` 结构体（`Content` + `Error` + `Attachments` + `Metadata`）
- `Capability` 类型 + `Capable` 接口
- `Attachment` 类型（从 `message.Attachment` 复用或本地别名）

**强制约束**：不引入 `charm.land/fantasy`、`charm.land/catwalk`、`internal/core/agent`、`internal/core/config` 任一导入。

**验收**：`go build ./tools/...` 通过；`go vet` 不报 unused interface。

**依赖**：1.1。

**Commit**：`feat(tools): define Tool/ToolProvider/ToolContext contracts`

---

### 1.3 实现 `registry.go` 与 `loader.go`（scaffold 组）

**文件**：

- 新建 `tools/registry.go`
- 新建 `tools/loader.go`

**内容要点**（Design Doc §6）：

- `Registry` 结构：`mu sync.RWMutex`、`tools map[string]Tool`、`meta map[string]sourceMeta`
- `sourceMeta`： `Source` + `Package` + `RegisteredAt`
- `NewRegistry()` 返回独立实例
- `Register(name, tool)` / `RegisterProvider(p)` / `Get` / `Names` / `All`
- 包级默认 `defaultRegistry` + `Register` / `RegisterProvider` / `Get` / `Names` / `All` / `Default()` 转发函数
- 重复名：第二次 `Register` 覆盖，触发 `slog.Warn("duplicate tool registration", "name", ..., "prev", ..., "new", ...)`

**验收**：对 `Default()` 调 `Register` / `Get` / `Names` 行为正确，`-race` 下 1000 goroutine 并发无 panic。

**依赖**：1.2。

**Commit**：`feat(tools): implement registry with init-time loader`

---

### 1.4 实现 `filter/filter.go` v2（scaffold 组）

**文件**：

- 新建 `tools/filter/filter.go`

**注意**：这是**新版**，旧 `internal/core/agent/tools/filter/` 暂时保留（PR2 末尾一并切换）。新版基于 `tools.Tool` 而非 `fantasy.AgentTool`：

- `FilterFunc func(tctx ToolContext, t Tool) bool`
- `Apply(tctx, tools, fns...)` / `Chain(fns...)` / `IncludeCapabilities(...)` / `ExcludeNames(...)` / `BySource(...)`
- `IncludeNames` 操作 `t.Name()`

**验收**：`go test ./tools/filter/...` 通过（用 1.6 的测试套件迁移过来）。

**依赖**：1.2。

**Commit**：`feat(tools): add framework-agnostic filter`

---

### 1.5 在 `tools/filter/filter_test.go` 移植测试（scaffold 组）

**文件**：

- 新建 `tools/filter/filter_test.go`

**内容**：把 `internal/core/agent/tools/filter/filter_test.go` 中所有用例改为基于 `tools.Tool`（用 1 个 stub 实现 `Tool` 四方法）；`TestIncludeNames` / `TestExcludeNames` / `TestChain` / `TestFilterFunc_ReceivesContext` 等逐个重写。

**验收**：`go test -race ./tools/filter/...` 全部 PASS。

**依赖**：1.4。

**Commit**：`test(tools/filter): port filter tests to new Tool contract`

---

### 1.6 编写 `registry_test.go`（scaffold 组）

**文件**：

- 新建 `tools/registry_test.go`

**必含用例**：

- `TestRegister_FromInit` 验证 init 阶段注册成功后 `Default().Get` 能拿到
- `TestRegister_DuplicateOverwritesAndWarns`（用 bytes.Buffer 捕获 slog 输出）
- `TestRegister_ConcurrentSafe`（`-race` 下 200 goroutine 同时 `Register` / `Get`）
- `TestNewRegistry_Isolated`（在两个独立 `NewRegistry()` 上注册同名，互不干扰）
- `TestRegisterProvider_PopulatesAll`（单 provider 返回 3 个 tool，全部可 `Get`）
- `TestGet_UnknownName` 返回 `(nil, false)`
- `TestNames_Sorted` 验证返回按字典序

**验收**：`go test -race ./tools/...` 全部 PASS。

**依赖**：1.3。

**Commit**：`test(tools): cover registry and loader behaviour`

---

### 1.7 添加 `agentToolContext` 适配骨架（agent-adapter 组）

**文件**：

- 新建 `internal/core/agent/agent_tool_context.go`

**内容**：

- `type agentToolContext struct { ... }` 持有 `sessionID` / `workingDir` / `permissions` / `mcpHandles` / `callbacks`
- 实现 `tools.ToolContext` 五方法（从 `context.Context` / `permission.Service` / `mcp.GetStates()` / `toolutil.Callbacks` 拿数据）
- `NewToolContext(ctx context.Context, sess session.Service, perms permission.Service, cbs *toolutil.Callbacks) (tools.ToolContext, *cleanup)` 工厂
- 暂时**不**接入 `Coordinator`，只确保能编译

**验收**：`go build ./internal/core/agent/...` 通过；无任何 `tools` 的运行时调用。

**依赖**：1.2。

**Commit**：`feat(agent): add agentToolContext adapter skeleton`

---

### 1.8 编写 `agent_tool_context_test.go`（agent-adapter 组）

**文件**：

- 新建 `internal/core/agent/agent_tool_context_test.go`

**用例**：

- 构造 fake `permission.Service` 验证 `Permissions().Allow(name)` 透传
- 构造 fake `mcp` 状态验证 `MCP().Servers()` 返回正确
- 验证 `SessionID()` / `WorkingDir()` 与输入一致
- 验证 `Callbacks().Before/After` 转发到 mock 回调

**验收**：`go test -race -run TestAgentToolContext ./internal/core/agent/...` PASS。

**依赖**：1.7。

**Commit**：`test(agent): cover agentToolContext adapter`

---

### 1.9 启用 CI 依赖边界检查（scaffold 组）

**文件**：

- 修改 `Taskfile.yaml`，在 `lint:` 任务前增加 `lint:deps` 任务：

  ```yaml
  lint:deps:
    desc: Verify  has no LLM-runtime or agent-package imports
    cmds:
      - "! go list -deps ./... | grep -E '(fantasy|catwalk|internal/core/(agent|config))'"
  ```

- 修改 `lint:` 任务使其依赖 `lint:deps`

**验收**：`task lint:deps` 在 PR1 当前代码上 PASS（空输出）；故意向 `contracts.go` 加 `charm.land/fantasy` 导入后 `task lint:deps` FAIL。

**依赖**：1.2、1.3、1.4。

**Commit**：`ci: add  dependency boundary check`

---

### 1.10 PR1 收尾质量门

按 AGENTS.md "Post-Change Local Pipeline" 跑：

```bash
go build -buildvcs=false .
go test -race ./... ./internal/core/agent/...
task fmt
task lint:fix
task lint:deps
```

预期：全绿。`internal/core/agent/` 旧路径**未触动**。

**PR1 标签建议**：`v0.1.0-toolkit-scaffold`

---

## PR2 — Migrate Implementations（实现搬迁）

**目标**：40+ 工具实现搬到 `...`，旧路径通过 re-export 兼容；期间 `go build ./...` 全程绿。

**入口验证命令**：

```bash
go build -buildvcs=false ./...
go test -race ./...
go list -deps ./... | grep -E "(fantasy|catwalk|internal/core/(agent|config))"   # 必须空
```

**搬迁顺序原则**：先内置（无外部依赖）→ 再插件（builtin-only 依赖）→ 最后桥（mcp/lsp，依赖整个 agent 上下文）。

---

### 2.1 搬迁共享 helpers（plugins 组前置）

**文件**：

- 新建 `plugins/sshcommon/`（从 `internal/core/agent/plugins/sshcommon/` 复制）
- 新建 `plugins/gitopscommon/`
- 新建 `plugins/giteacommon/`
- 新建 `plugins/netcommon/`
- 新建 `plugins/searchcommon/`

**操作**：

- `cp -r internal/core/agent/plugins/sshcommon/* plugins/sshcommon/`
- 对每个 helper，改包名为 `package sshcommon` 等；**保留**所有导出符号
- 在新位置加 `init()` 空白（留作未来注册点，本 PR 不在此处注册）

**验收**：`go build ./plugins/sshcommon/...` 等 5 个 helper 全部 PASS。

**依赖**：1.3。

**Commit**：`refactor(tools): relocate shared plugin helpers`

---

### 2.2 搬迁 `builtin/bash/`（builtin 组）

**文件**：

- 新建 `builtin/bash/`
- 从 `internal/core/agent/builtin/bash/` 复制 `bash.go` / `safe.go` / `*.tpl` / `*_test.go`
- 改 import 路径：`internal/core/agent/toolutil` → 暂时保留（下一步处理）；`internal/core/permission` → 暂时保留

**验收**：`go test ./builtin/bash/...` PASS。

**依赖**：1.3。

**Commit**：`refactor(builtin): move bash tool to new path`

---

### 2.3 搬迁 `builtin/edit/`、`view/`、`write/`、`ls/`（builtin 组）

**文件**：4 个新目录，各复制自 `internal/core/agent/builtin/<name>/`。

**注意**：`edit` / `view` / `write` 依赖 `internal/core/agent/lsp` 的 `*lsp.Manager`。**先搬迁 lsp（见 2.20）再回到这里**，或在本步暂时保留 `internal/core/agent/lsp` 旧路径作为依赖。

**验收**：4 个目录的测试单独运行全 PASS。

**依赖**：2.2（模式相同，允许并行执行）。

**Commit**：`refactor(builtin): move file tools to new path`

---

### 2.4 搬迁 `builtin/multiedit/`、`read_files/`（builtin 组）

**文件**：2 个新目录。

**验收**：`go test ./builtin/multiedit/... ./builtin/read_files/...` PASS。

**依赖**：2.3 模式一致，可并行。

**Commit**：`refactor(builtin): move multiedit and read_files`

---

### 2.5 搬迁 `builtin/job_input/`、`job_kill/`、`job_output/`（builtin 组）

**文件**：3 个新目录。

**注意**：这三个 tool 引用 `internal/core/agent/shellruntime/shell` 的 job 注册表。搬迁后**仍引用旧路径**，PR3 才统一改 import。

**验收**：3 个目录测试 PASS。

**依赖**：2.4 模式一致。

**Commit**：`refactor(builtin): move job_* tools`

---

### 2.6 创建 `builtin/all/all.go`（builtin 组）

**文件**：

- 新建 `builtin/all/all.go`
- 内容：`package all` + 10 个 `_ "github.com/package-register/mocode/builtin/<name>"` 空白导入

**验收**：

- `go build ./builtin/all/...` PASS
- `builtin/all/all_test.go` 断言 `tools.Default().Names()` 包含所有 10 个 builtin 名（用 `require.Contains`）

**依赖**：2.2–2.5。

**Commit**：`feat(builtin/all): add umbrella blank-import for builtins`

---

### 2.7 旧 `internal/core/agent/builtin/` 改为 re-export（builtin 组）

**文件**：

- 修改 `internal/core/agent/builtin/<name>/<name>.go`（10 个文件）
- 把包主体清空，改为：

  ```go
  // Package <name> is a compatibility re-export. New code should import
  // github.com/package-register/mocode/builtin/<name> directly.
  package <name>

  import "github.com/package-register/mocode/builtin/<name>"

  type <Name>Params = <pkg>.<Name>Params
  // ... 所有导出符号逐个别名 ...
  var New<Name>Tool = <pkg>.New<Name>Tool
  ```

**验收**：

- `go build ./...` 全绿
- `go test ./internal/core/agent/builtin/...` 旧测试仍 PASS（re-export 透传）

**依赖**：2.6。

**Commit**：`refactor(agent/builtin): re-export from new toolkit path`

---

### 2.8 搬迁单 tool 插件 `think/`、`todos/`（plugins 组）

**文件**：

- 新建 `plugins/think/`
- 新建 `plugins/todos/`

**验收**：两个目录测试 PASS。

**依赖**：2.1。

**Commit**：`refactor(plugins): move think and todos`

---

### 2.9 搬迁 `glob/`、`grep/`、`web_search/`、`web_fetch/`（plugins 组）

**文件**：4 个新目录。

**验收**：4 个目录测试 PASS。

**依赖**：2.8 模式。

**Commit**：`refactor(plugins): move search and web tools`

---

### 2.10 搬迁 `crawl/`、`fetch/`、`download/`、`download_docs/`（plugins 组）

**文件**：4 个新目录。

**验收**：4 个目录测试 PASS。

**依赖**：2.9 模式。

**Commit**：`refactor(plugins): move network tools`

---

### 2.11 搬迁 4 个 ssh 工具（plugins 组）

**文件**：

- 新建 `plugins/ssh_list_hosts/`
- 新建 `plugins/ssh_exec/`
- 新建 `plugins/ssh_download/`
- 新建 `plugins/ssh_upload/`

**注意**：这 4 个 tool 引用 `plugins/sshcommon`（2.1 已搬迁），改 import 路径。

**验收**：4 个目录测试 PASS。

**依赖**：2.1。

**Commit**：`refactor(plugins): move ssh tools to new path`

---

### 2.12 搬迁 2 个 git 工具（plugins 组）

**文件**：

- 新建 `plugins/git_plan_commits/`
- 新建 `plugins/git_execute_commits/`

**依赖**：`gitopscommon`（2.1）。

**验收**：2 个目录测试 PASS。

**Commit**：`refactor(plugins): move git tools`

---

### 2.13 搬迁 3 个 gitea 工具（plugins 组）

**文件**：

- 新建 `plugins/gitea_issues/`
- 新建 `plugins/gitea_pulls/`
- 新建 `plugins/gitea_notifications/`

**依赖**：`giteacommon`（2.1）。

**验收**：3 个目录测试 PASS。

**Commit**：`refactor(plugins): move gitea tools`

---

### 2.14 搬迁 `sourcegraph/`（plugins 组）

**文件**：新建 `plugins/sourcegraph/`。

**验收**：目录测试 PASS。

**Commit**：`refactor(plugins): move sourcegraph tool`

---

### 2.15 搬迁 4 个微信工具（plugins 组）

**文件**：

- 新建 `plugins/send_wechat_file/`
- 新建 `plugins/send_wechat_image/`
- 新建 `plugins/screenshot_to_wechat/`
- 新建 `plugins/message_export/`

**注意**：`send_wechat_*` 引用 `internal/integration/wechat` 的 messenger 端口。搬迁后**仍引用旧路径**（PR3 才统一改）；但 `internal/integration/wechat` 不能反过来引用 `...`，这是单向的，合规。

**验收**：4 个目录测试 PASS。

**Commit**：`refactor(plugins): move wechat tools`

---

### 2.16 搬迁 3 个 session 工具（plugins 组）

**文件**：

- 新建 `plugins/session_export/`
- 新建 `plugins/session_search/`
- 新建 `plugins/session_summary/`

**依赖**：依赖 `session` / `message` / `history` 等 domain service。搬迁后**保持 service 引用**（在 `internal/core/agent` 之外可用）。

**验收**：3 个目录测试 PASS。

**Commit**：`refactor(plugins): move session tools`

---

### 2.17 搬迁 `mocode_info/`、`mocode_logs/`、`lsp_restart/`（plugins 组）

**文件**：3 个新目录。

**验收**：3 个目录测试 PASS。

**Commit**：`refactor(plugins): move mocode and lsp_restart tools`

---

### 2.18 搬迁 `list_mcp_resources/`、`read_mcp_resource/`（plugins 组）

**文件**：2 个新目录。

**依赖**：依赖 `mcp` 包（2.21）。

**验收**：2 个目录测试 PASS。

**Commit**：`refactor(plugins): move mcp meta tools`

---

### 2.19 创建 `plugins/all/all.go`（plugins 组）

**文件**：

- 新建 `plugins/all/all.go`
- 内容：`package all` + 30+ `_ "github.com/package-register/mocode/plugins/<name>"` 空白导入

**验收**：

- `go build ./plugins/all/...` PASS
- `plugins/all/all_test.go` 断言 `tools.Default().Names()` 包含所有预期 plugin 名

**依赖**：2.8–2.18。

**Commit**：`feat(plugins/all): add umbrella blank-import for plugins`

---

### 2.20 旧 plugin 目录 re-export（plugins 组）

**文件**：对 `internal/core/agent/plugins/<name>/<name>.go`（30+ 文件）做与 2.7 相同的 re-export 处理。

**验收**：

- `go build ./...` 全绿
- `go test ./internal/core/agent/plugins/...` 旧测试仍 PASS

**依赖**：2.19。

**Commit**：`refactor(agent/plugins): re-export from new toolkit path`

---

### 2.21 搬迁 `mcp/`（mcp-lsp 组）

**文件**：

- 新建 `mcp/`
- 复制 `init.go` / `prompts.go` / `resources.go` / `tools.go` / `mcp-tools.go` / 测试
- 改包名为 `package mcp`（保持不变）
- 把当前的"被 `agent_lifecycle.go` 直接调用的 `mcp.GetStates()`"包装为 `tools.ToolProvider`：

  ```go
  // Bridge 是 MCP 桥的 ToolProvider 形式
  type Bridge struct{}
  func (Bridge) Name() string { return "mcp" }
  func (Bridge) Tools() []tools.Tool { /* 反射 GetStates() 构造 */ }

  func init() { tools.RegisterProvider(Bridge{}) }
  ```

- 旧 `mcp.GetStates()` 保留（供 `agent_lifecycle.go` 直接调用，PR3 才切）

**验收**：

- `go test ./mcp/...` PASS
- `go build ./...` 全绿

**依赖**：1.3。

**Commit**：`refactor(mcp): move MCP bridge to new path with ToolProvider adapter`

---

### 2.22 搬迁 `lsp/`（mcp-lsp 组）

**文件**：

- 新建 `lsp/`
- 复制 `client.go` / `handlers.go` / `manager.go` / `util/` 及测试
- 改 import 路径：`charm.land/fantasy` **保留**（LSP 内部需要 `fantasy` 类型）—— 这是设计妥协，需要在 PR2 末尾评估（见 Risks 6.1）

**验收**：`go test ./lsp/...` PASS。

**依赖**：1.3。

**Commit**：`refactor(lsp): move LSP bridge to new path`

> **⚠️ 风险见 Risks 6.1**：`lsp/` 当前会因 LSP 实现需要而引入 `charm.land/fantasy`。**`task lint:deps` 必失败**。本任务的实施者必须**显式**把 `lsp` 加入 `lint:deps` 的 grep 例外列表（用 `lsp` 子路径单独 grep），并在 PR2 末尾用 `internal/core/agent/agenttool_adapter.go` 内的薄包装代替 `lsp` 直接对 `fantasy` 的依赖。本任务的 DoD 包含：在 PR3 收尾时**移出** `lsp` 的 `fantasy` 导入。

---

### 2.23 `internal/core/agent/mcp/`、`lsp/` 改为 re-export（mcp-lsp 组）

**文件**：

- 旧 `internal/core/agent/mcp/{init,prompts,resources,tools}.go` 改为对 `mcp` 的 re-export
- 旧 `internal/core/agent/lsp/{client,handlers,manager,util}.go` 改为对 `lsp` 的 re-export

**验收**：`go build ./...` 全绿；agent 端 `mcp.GetStates()` 调用仍能找到符号。

**依赖**：2.21、2.22。

**Commit**：`refactor(agent/tools): re-export mcp and lsp from new path`

---

### 2.24 旧根级文件 re-export（cutover 组前置）

**文件**（原 7 个，改为 7 个 shim）：

- `internal/core/agent/compat.go` → 仅保留 `transfer.go` / `fetch_types.go` / 根级别名
- `internal/core/agent/tools.go` → re-export 来自 `tools` 的新符号
- `internal/core/agent/registry.go` / `registry_filter.go` → 标记 `// Deprecated: moved to tools` 并把 `AllToolNames` / `AllToolDescriptors` / `ToolDeps` 等改为对 `tools` 的薄包装，或保留为独立 facade

**验收**：`go build ./...` 全绿；`go test ./internal/core/agent/...` 全绿（无行为变化）。

**依赖**：2.7、2.20、2.23。

**Commit**：`refactor(agent/tools): re-export root-level symbols`

---

### 2.25 一次性 import 重写（脚本，非独立 task）

**脚本**：`scripts/rewrite_tool_imports.sh`（新增，提交进仓）

**内容**：

- 对所有 `internal/core/agent/**/*.go` 跑 `gofmt -r` 或 `sed`：
  - `internal/core/agent/builtin/<n>` → `github.com/package-register/mocode/builtin/<n>`
  - `internal/core/agent/plugins/<n>` → `github.com/package-register/mocode/plugins/<n>`
  - `internal/core/agent/mcp` → `github.com/package-register/mocode/mcp`
  - `internal/core/agent/lsp` → `github.com/package-register/mocode/lsp`
  - `internal/core/agent/filter` → `github.com/package-register/mocode/filter`
- 跑完 `go build -buildvcs=false ./...` 确认全绿
- 跑 `grep -r "internal/core/agent/tools" internal/core/agent/ internal/ui/ internal/store/ internal/integration/` 只剩 `internal/core/agent/*.go` 自身

**验收**：`go build ./...` 全绿；`go test -race ./...` 全绿；`grep -r "internal/core/agent/builtin\|internal/core/agent/plugins\|internal/core/agent/mcp\|internal/core/agent/lsp\|internal/core/agent/filter" --include="*.go" | grep -v "^internal/core/agent/"` 为空（允许 shim 文件本身保留旧路径引用）。

**依赖**：2.24。

**Commit**：`refactor: rewrite imports to new  path`

---

### 2.26 PR2 收尾质量门

```bash
go build -buildvcs=false .
go test -race ./...
task fmt
task lint:fix
task lint:deps   # 期望空（除 lsp 例外外）
git status --short
```

**期望**：

- `internal/core/agent/` 旧路径仍存在（作为 re-export shim）
- `...` 已有 40+ 实现 + umbrella `all`
- `go test ./...` 全绿（每个搬过来的 tool 测试独立 PASS）
- `go test ./internal/core/agent/...` 全绿（通过 shim 仍走旧路径）

**PR2 标签建议**：`v0.2.0-toolkit-migrated`

---

## PR3 — Cutover + Delete Old（切换 + 删除）

**目标**：`Coordinator` 改用 `*tools.Registry` 走 `Tool` 接口；`agenttool_adapter.go` 成为唯一 `fantasy` 耦合点；旧 `internal/core/agent/` 删除。

**入口验证命令**：

```bash
go build -buildvcs=false .
go test -race ./...
task lint:deps
grep -r "internal/core/agent/tools" --include="*.go" | grep -v "^internal/core/agent/"   # 必须空
```

---

### 3.1 实现 `agenttool_adapter.go`（agent-adapter 组）

**文件**：

- 新建 `internal/core/agent/agenttool_adapter.go`

**内容**（Design Doc §4 唯一耦合点）：

- `type Adapter struct{ registry *tools.Registry }`
- `func NewAdapter(r *tools.Registry) *Adapter`
- `func (a *Adapter) ToFantasyTools() []fantasy.AgentTool` — 遍历 `r.All()`，逐个转 `fantasy.AgentTool`（把 `tools.Tool` 四方法包成 `fantasy.NewAgentTool(...)`）
- `func (a *Adapter) Execute(ctx, call, tctx tools.ToolContext) (fantasy.ToolResponse, error)` — 解析 call.Input 为 `json.RawMessage`，调 `t.Execute`，把 `ToolResult` 转回 `fantasy.ToolResponse`
- **唯一**一处 `charm.land/fantasy` 导入（在 `internal/core/agent/` 内）

**验收**：`go build ./...` PASS；`grep -rl "charm.land/fantasy" internal/core/agent/` 仅返回 `agenttool_adapter.go`。

**依赖**：1.3、2.25。

**Commit**：`feat(agent): add single-point fantasy adapter`

---

### 3.2 编写 `agenttool_adapter_test.go`（agent-adapter 组）

**文件**：

- 新建 `internal/core/agent/agenttool_adapter_test.go`

**用例**：

- `TestAdapter_TranslatesToolToFantasy` 验证 `Name` / `Description` / `Schema` 字段透传
- `TestAdapter_ExecuteReturnsContent` 成功 `ToolResult{Content: "ok"}` → `fantasy.NewTextResponse("ok")`
- `TestAdapter_ExecuteReturnsError` `ToolResult{Error: ...}` → 第二个返回值非 nil
- `TestAdapter_ExecuteSkipsContentOnError` `ToolResult{Error: ..., Content: "partial"}` → 返回的内容不含 "partial"
- `TestAdapter_PreservesProviderExecuted` 调用元数据

**验收**：`go test -race -run TestAdapter ./internal/core/agent/...` 全 PASS。

**依赖**：3.1。

**Commit**：`test(agent): cover fantasy adapter translation`

---

### 3.3 `Coordinator` 改用 `*tools.Registry`（cutover 组）

**文件**：

- 修改 `internal/core/agent/coordinator.go`

**改动**：

- `coordinator` 结构增加字段 `toolsReg *tools.Registry`（可注入）
- 构造时默认 `tools.Default()`
- 工具查找从 `for _, t := range a.tools` 改为 `a.toolsReg.Get(name)`：

  ```go
  // 旧
  for _, t := range agentTools { if t.Info().Name == name { t.Execute(...) } }
  // 新
  t, ok := a.toolsReg.Get(name)
  if !ok { return /* not found error */ }
  tctx := buildToolContext(ctx, ...)
  if !tctx.Permissions().Allow(name) { return tools.NewPermissionDeniedResponse() }
  return a.adapter.Execute(ctx, call, t, tctx)
  ```

- 移除 `a.tools` 字段对 `fantasy.AgentTool` 切片的依赖；`SetTools([]fantasy.AgentTool)` 标记为 deprecated 但保留 stub 避免破坏 `extension` 包的调用

**验收**：

- `go test ./internal/core/agent/...` 全 PASS
- `grep "fantasy.AgentTool" internal/core/agent/coordinator.go` 只剩注释 / 弃用 stub 引用

**依赖**：3.1。

**Commit**：`refactor(agent): route Coordinator through tools.Registry and adapter`

---

### 3.4 集成 smoke 测试（cutover 组）

**文件**：

- 新建 `internal/core/agent/coordinator_registry_test.go`

**用例**：

- 用 mock provider 跑一个最小 LLM 调用，触发 `bash` tool
- 断言 `tools.Default().Get("bash")` 被调到
- 断言权限被拒时返回 `ToolResult.Error` 而非 panic

**验收**：`go test -race -run TestCoordinatorRegistry ./internal/core/agent/...` PASS。

**依赖**：3.3。

**Commit**：`test(agent): smoke-test Coordinator routing through new registry`

---

### 3.5 `agent_lifecycle.go` 加 umbrella 空白导入（cutover 组）

**文件**：

- 修改 `internal/core/agent/agent_lifecycle.go` 或 `agent.go`（任选一，推荐 `agent.go` 顶部 import 块）

**改动**：在 import 块加入：

```go
_ "github.com/package-register/mocode/builtin/all"
_ "github.com/package-register/mocode/plugins/all"
```

**验收**：`go build ./...` PASS；`tools.Default().Names()` 在 main 启动后包含全部 40+ 名。

**依赖**：2.25。

**Commit**：`feat(agent): opt in to builtin/all and plugins/all`

---

### 3.6 协调 agent-level 工具（转移、agentic_fetch、hooked_tool）（cutover 组）

**文件**：

- 修改 `internal/core/agent/hooked_tool.go`：把 `fantasy.AgentTool` 入参改为 `tools.Tool`
- 修改 `internal/core/agent/agentic_fetch_tool.go`：把内部子工具构造从 `tools.NewXxxTool` 改为通过 `tools.Default().Get(...)` 取；若无，直接用 `tools.NewXxxTool` 工厂（避免双重注册）
- 修改 `internal/core/agent/roundtable_tool.go`：同上

**验收**：`go test ./internal/core/agent/...` 全 PASS；`grep "internal/core/agent/tools" internal/core/agent/*.go` 命中 0 条（因为 3.5 的 import 是下划线，不入调用方）。

**依赖**：3.3。

**Commit**：`refactor(agent): route agent-level tools through new registry`

---

### 3.7 删除 `internal/core/agent/`（cutover 组）

**文件**：整目录删除：

```bash
rm -rf internal/core/agent/
```

**验收**：

- `git status` 删除了 ~50 个文件
- `go build ./...` 全绿
- `grep -r "internal/core/agent/tools" --include="*.go" .` 为空

**依赖**：3.6、3.7 之前的全部。

**Commit**：`chore: remove legacy internal/core/agent/ directory`

---

### 3.8 解决 lsp 的 `fantasy` 依赖例外（mcp-lsp 组收尾）

**文件**：

- 修改 `lsp/` 内的 `fantasy` 引用，改为接收 `tools.Tool` 形式的 LSP 客户端（由 `internal/core/agent/lsp_bridge.go` 新文件适配 `fantasy` 类型）
- 修改 `Taskfile.yaml` 的 `lint:deps`，移除 `lsp` 的例外

**验收**：

- `go list -deps ./... | grep -E "(fantasy|catwalk|internal/core/(agent|config))"` 全空
- `task lint:deps` PASS

**依赖**：2.22（原 lsp 搬迁）、3.1（adapter 已就位）。

**Commit**：`refactor(lsp): remove direct fantasy dependency`

---

### 3.9 文档与贡献指南更新（tests-docs 组）

**文件**：

- 修改 `AGENTS.md`（仓库根）
- 新建 `docs/architecture/toolkit.md`
- 修改 `CONTRIBUTING.md`（如不存在则新建）

**内容要点**：

- AGENTS.md 新增章节 "Tool Layout" 说明：
  - 所有 tool 位于 ``
  - 添加新 tool = 新建子目录 + 在 `builtin/all/` 或 `plugins/all/` 加一行空白导入
  - 严禁在 `...` 内 import `charm.land/fantasy` / `charm.land/catwalk` / `internal/core/agent` / `internal/core/config`
- `docs/architecture/toolkit.md` 包含：包拓扑图、`Tool` 接口签名、添加新 tool 的 5 步配方、adapter 工作原理
- `CONTRIBUTING.md` 新增 "How to add a new tool" 一节，模板：

  ```go
  package mytool

  import "github.com/package-register/mocode/tools"

  type myTool struct{}
  func (*myTool) Name() string { return "mytool" }
  func (*myTool) Description() string { return "..." }
  func (*myTool) Schema() tools.Schema { return tools.Schema{...} }
  func (*myTool) Execute(ctx context.Context, tctx tools.ToolContext, args json.RawMessage) (tools.ToolResult, error) {
      // ...
  }
  func init() { tools.Register("mytool", &myTool{}) }
  ```

  并附 `all.go` 增量一行。

**验收**：

- 新人按 CONTRIBUTING.md 步骤能在 30 分钟内添加一个空 tool
- `docs/architecture/toolkit.md` 链接从 `AGENTS.md` 可见

**依赖**：3.8。

**Commit**：`docs: describe new tool layout and one-line opt-in`

---

### 3.10 最终质量门

```bash
go build -buildvcs=false .
go test -race ./...
task fmt
task lint:fix
task lint:deps
git status --short
git diff --stat
```

**期望**：

- `internal/core/agent/` 已不存在
- `` 完整，有 `contracts.go` / `registry.go` / `loader.go` / `filter/` / `builtin/all/` / `plugins/all/` / `mcp/` / `lsp/`
- `internal/core/agent/agenttool_adapter.go` 是 `internal/core/agent/` 内**唯一**的 `fantasy` 引用
- `grep -r "charm.land/fantasy" internal/core/agent/ | wc -l` == 1
- `go list -deps ./... | grep -E "(fantasy|catwalk|internal/core/(agent|config))"` 为空

**PR3 标签建议**：`v0.3.0-toolkit-cutover`（cutover commit 加 tag）

---

## Testing Strategy（分四类）

### 3.10.1 Per-tool 测试（搬迁时迁移）

- 每个 tool 的 `*_test.go` 随 `cp -r` 一同搬到 `...`
- 测试断言保持不变，行为不变
- 验收：`go test -race ./builtin/...` / `./plugins/...` / `./mcp/...` / `./lsp/...` 全 PASS
- 失败 → 对比 pre-refactor 状态：同一个 tool 在旧路径也是 PASS 吗？若不是，PR2 引入 bug；若是，旧测试在新路径应自动通过

### 3.10.2 Registry / loader 测试（新增）

- 文件：`registry_test.go`（PR1 1.6）、`filter/filter_test.go`（PR1 1.5）
- 覆盖：init-time 注册、duplicate 警告、并发安全、NewRegistry 隔离、RegisterProvider、`Get` 未知名返回 `false`、filter 组合

### 3.10.3 Adapter 测试（新增）

- 文件：`internal/core/agent/agenttool_adapter_test.go`（PR3 3.2）
- 覆盖：`Tool → fantasy.AgentTool` 字段透传；`ToolResult.Error` → `fantasy.ToolResponse`；Content 抑制；ProviderExecuted 元数据

### 3.10.4 集成测试

- 文件：`internal/core/agent/coordinator_registry_test.go`（PR3 3.4）
- 覆盖：Coordinator 走 `tools.Default().Get("bash").Execute(...)` 路径；权限拒绝不 panic；mock provider 单次 LLM 调用成功

**关键回归点**：

- `internal/core/agent/coordinator_test.go`（既存）的黄金文件（`testdata/*.json`）在 PR3 跑过一次后，如行为无变化，**不应**更新
- 若 `task test:record` 被触发，生成的快照需人工 review 确认是 LLM 文本微小变化而非 tool 调度路径错误

---

## Risks & Mitigations（参照 Design Doc §10）

| 编号 | 风险 | 影响 | 缓解 | 触发 task |
|---|---|---|---|---|
| 1 | 40+ 文件搬迁是大 diff | PR review 困难 | PR2 内分内置（2.2–2.7）→ 单 tool 插件（2.8–2.18）→ umbrella（2.19）→ re-export（2.20），每个子批独立可测 | PR2 全部 |
| 2 | init() 顺序不确定导致重复注册 | 工具覆盖 | `Register` 已设计为幂等 + `slog.Warn`，`meta` 字段记录来源 | 1.3、1.6 |
| 3 | 旧测试断言旧 import 路径 | 旧测试不能直接搬 | PR2 期间 shim 透传，旧测试可继续在原路径运行；PR3 后全部改路径 | 2.7、2.20、2.23、2.24、2.25 |
| 4 | `lsp/` 因 LSP 实现需要而引入 `fantasy` | 违反"零依赖"约束，CI 红 | PR2 末 + PR3 3.8 显式处理：把 LSP 对 `fantasy` 的依赖挪到 `internal/core/agent/lsp_bridge.go`，lsp 只暴露 `tools.Tool` 接口 | 2.22、3.8 |
| 5 | agentic_fetch / roundtable / hooked_tool 不在 registry 中 | coordinator 拿不到 | Design Doc §10 已 trade-off；Coordinator 通过 `tools.Default().Get(...)` 拿；agentic_fetch 自己用 `New*Tool` 工厂构造子工具，不注册到 default | 3.6 |
| 6 | `Coordinator` 切换路径时若遗漏某个边角，tool 调用全断 | 严重 | 3.3 完成后跑 `coordinator_test.go` 全套；3.4 加 mock provider smoke | 3.3、3.4 |
| 7 | `agent_lifecycle.go` 用 `mcp.GetStates()` 直接拿状态，绕过 `Bridge.Tools()` | 切换期数据不一致 | PR2 保留旧 `mcp.GetStates()`（不删）；PR3 改用 `Bridge` 内的同一状态源 | 2.21、3.3 |
| 8 | UI god file（`ui.go` ~3240 行）引用 `tools.BashToolName` 等符号 | 路径变更后编译失败 | compat shim 在 PR2 末覆盖所有 30+ 工具名常量；`grep "tools\." internal/ui/model/ui.go` 在 2.25 一次性扫描 | 2.24、2.25 |
| 9 | `task test:record` 在 PR3 后被无意识触发，覆盖黄金文件 | 难以察觉的回归 | 3.10 末尾**不**跑 `test:record`；只跑普通 `go test -race` | 3.10 |
| 10 | AGENTS.md 文档更新遗漏 | 新贡献者困惑 | 3.9 显式列文档改动；3.10 最后 `git status --short` 检查 `AGENTS.md` / `CONTRIBUTING.md` / `docs/architecture/toolkit.md` 是否都改了 | 3.9、3.10 |

---

## Rollback Plan（每 PR 独立可回滚）

### PR1 回滚

- `git revert <PR1 merge commit>` 或 `git reset --hard <PR1 base ref>`
- 风险：无，因为 agent 调用点未改
- 验证：`go build -buildvcs=false ./...` 与 base ref 一致

### PR2 回滚

- `git revert <PR2 merge commit>`
- 风险：中等——如果已有下游 PR 基于 PR2，revert 会带冲突；此时应 **revert single commit 而非整 merge**
- 验证：
  - `go build -buildvcs=false ./...` PASS
  - `go test -race ./...` PASS
  - 旧 `internal/core/agent/` 恢复，新 `` 也保留（因为 PR1 已独立合入）
- **回滚后**：`builtin/all/` 和 `plugins/all/` 仍可作为"无人 import 的孤儿"存在，无害；后续可在独立清理 PR 中删除

### PR3 回滚

- `git revert <PR3 merge commit>`
- **先做这一步**：把 `internal/core/agent/` 从 git 历史中恢复

  ```bash
  git revert <PR3 merge commit>   # 撤销删除
  git checkout <PR2 HEAD> -- internal/core/agent/
  git add internal/core/agent/
  git commit -m "revert PR3: restore legacy  directory"
  ```

- 风险：高——因为 `Coordinator` 已切到新路径，单纯 revert 不会让 agent 跑回旧路径；需要回退 `Coordinator` 的 commit（可能跨多 commit）
- 验证：
  - `git diff <base ref> HEAD -- internal/core/agent/agenttool_adapter.go` 不应有遗留
  - 旧 `coordinator.go` 仍 `for _, t := range agentTools` 风格
  - `go test -race ./...` 全绿

**通用建议**：每个 PR 合并前**单独打 tag**（建议 `v0.1.0-toolkit-scaffold` / `v0.2.0-toolkit-migrated` / `v0.3.0-toolkit-cutover`），便于 `git revert <tag>^` 精确回滚。

---

## CI / Quality Gate（每 PR 末尾必跑）

按 `AGENTS.md` 的 "Post-Change Local Pipeline"：

```bash
# 1. 编译
go build -buildvcs=false .

# 2. 测试（按 PR 范围缩窄）
go test -race ./...                          # PR2、PR3
go test -race ./...           # PR1
go test -race ./internal/core/agent/...      # PR1、PR3（adapter 验证）

# 3. 格式化
task fmt
# or
gofumpt -w .

# 4. Lint
task lint:fix

# 5. 自定义依赖边界（本 change 特有）
task lint:deps
# 等价命令：
#   go list -deps ./... | grep -E '(fantasy|catwalk|internal/core/(agent|config))'

# 6. 路径残留扫描
grep -r "internal/core/agent/tools" --include="*.go" . \
  | grep -v "^internal/core/agent/"   # PR3 末尾必须空

# 7. adapter 唯一性扫描
grep -rl "charm.land/fantasy" internal/core/agent/ \
  | wc -l                                    # PR3 末尾 == 1
```

**期望**：

- PR1：1–5 全绿，6 仍可命中（`internal/core/agent/` 未删）
- PR2：1–5 全绿；6 仍命中旧 re-export shim 文件本身（允许）
- PR3：1–5 全绿，6 空，7 == 1

---

## Out of Scope Reminder（明确不在本计划内）

以下事项由 Design Doc §2 Non-Goals 与 §11 Open Questions 标记为**未来 change**，本计划**严禁**实施；若 reviewer 发现超出范围，直接回退相关 commit：

1. **外部 tool runtime**（WASM / Go plugin `.so` / subprocess transport / gRPC / HTTP）—— 留待 `external-tool-runtime` change
2. **Agent-as-tool** / 多框架 adapter / 任何"包另一个 agent 当 tool"—— 留待 `agent-as-tool` change
3. **`trpc-agent-go` 或 `langchaingo` 适配**—— 留待 kernel-swap change（本计划只确保**接缝位置已收窄**）
4. **新 tool、tool 行为变更、capability 新增**—— 严禁
5. **`` 独立 `go.mod`**—— 留待有第二个 consumer 出现时
6. **Tool marketplace** / 远端 registry 服务—— 留待 `tool-marketplace` change
7. **`SessionAgent` / `extension` / `evo` / `evolution` / `roundtable` / `candidate` / `failover` / `notify` / `prompt` / `ctxcompress` 的任何修改**—— Design Doc §2 显式排除
8. **`internal/ui/model/ui.go` 业务逻辑调整**—— 本计划只动其 import 路径（2.25 由脚本完成），不动其 ~3240 行业务代码

如果实施过程中发现以上任何一项是迁移的"前置条件"，立即**停止**并在 result 中报告 blocker，由 leader 决定是扩大 scope 还是拆出新 change。

---

## 任务组覆盖核对（vs `tasks.md` 7 个组）

| tasks.md 任务组 | 本计划对应 | 状态 |
|---|---|---|
| 1. Scaffold the toolkit package | PR1 §1.1–1.6, 1.9 | ✅ 全覆盖 |
| 2. Build the agent-side adapter | PR1 §1.7–1.8；PR3 §3.1–3.2 | ✅ 全覆盖（分两阶段：骨架 + 切换） |
| 3. Move built-in tools | PR2 §2.2–2.7 | ✅ 全覆盖（10 个工具 + umbrella + shim） |
| 4. Move plugin tools | PR2 §2.1（共享 helpers）+ 2.8–2.20 | ✅ 全覆盖（30+ 工具） |
| 5. Move MCP and LSP bridges | PR2 §2.21–2.23, 3.8 | ✅ 全覆盖（MCP + LSP + shim + 依赖清零） |
| 6. Cutover the agent to the new registry | PR3 §3.3–3.7 | ✅ 全覆盖（Coordinator 改用 + umbrella 导入 + 删旧目录） |
| 7. Tests, lint, docs | PR1 §1.6；PR2 §2.6, 2.19 测试；PR3 §3.9 文档；每 PR 末尾 quality gate | ✅ 全覆盖 |

7 个任务组全部映射到本计划的具体章节，无遗漏。
