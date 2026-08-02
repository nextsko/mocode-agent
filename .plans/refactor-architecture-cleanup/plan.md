# Refactor: Architecture Cleanup

> 5 个工作流，高内聚低耦合，每步编译验证。

## 约束

- 每步完成后 `go build -buildvcs=false ./...` 必须通过
- 不改外部 API 行为，不改配置格式
- 纯结构搬迁 + 命名归一 + 文档对齐
- 每步独立提交

---

## W1: AGENTS.md 对齐

**目标**：文档描述 = 实际目录树

**操作**：
1. 用 `find internal -maxdepth 3 -type d` 生成真实树
2. 重写 AGENTS.md 的 Architecture section
3. 删除不存在的路径引用（evolution/、coordinator/、extension/、tools/ under core/agent/）
4. 补上遗漏的目录（ctxcompress/、failover/、notify/、subagent_cards/、toolutil/）
5. 更新 Key Patterns section（tools/ 实际位置）

**验证**：人工 diff AGENTS.md 中的每个路径 vs `find` 输出

---

## W2: tools/ 迁入 internal/

**目标**：`tools/` → `internal/core/tools/`，统一 internal boundary

**影响面**：~30 个文件的 import 路径变更（机械替换）

**操作**：
1. `git mv tools internal/core/tools`
2. 全局替换 import path：
   - `github.com/nextsko/mocode-agent/tools"` → `github.com/nextsko/mocode-agent/internal/core/tools"`
   - `github.com/nextsko/mocode-agent/tools/` → `github.com/nextsko/mocode-agent/internal/core/tools/`
3. 处理 `tools/doc.go` 中的 sub-package imports（`tools/builtin/all` → `internal/core/tools/builtin/all` 等）
4. 检查 Taskfile.yaml / scripts / .goreleaser.yml 中的路径引用

**验证**：`go build -buildvcs=false ./...`

---

## W3: 工具文件去冗余命名

**目标**：`bash_bash.go` → `bash.go`，消除 35 个冗余前缀

**模式**：
- `X_X.go` → `X.go`（16 个，如 `edit_edit.go` → `edit.go`）
- `domain_action_action.go` → `domain_action.go`（19 个，如 `gitea_issues_issues.go` → `gitea_issues.go`）

**操作**（逐文件）：
1. `git mv bash_bash.go bash.go`（或目标名冲突时按 package name 命名）
2. 重复 35 次

**验证**：`go build -buildvcs=false ./...`

---

## W4: 拆分 coordinator.go god object

**目标**：1485 行 coordinator → 按职责拆成 3 个 struct

**分析**：coordinator 的 31 个方法自然分为三簇：

| 簇 | 方法 | 目标文件 |
|---|---|---|
| **Agent 生命周期** | Run, buildAgent, buildTools, buildAgentModels, withFailover, Model, UpdateModels, SmallLanguageModel, ActiveAgentID, ActiveAgentSystemPrompt, SetMainAgent | `coordinator.go`（精简后 ~500 行） |
| **Agent 控制** | Cancel, CancelSubagent, CancelAll, ClearQueue, IsBusy, IsSessionBusy, isUnauthorized, refreshOAuth2Token, refreshApiKeyTemplate | `coordinator_control.go` |
| **Subagent + Summary** | runSubAgent, runSubAgentWithMeta, publishSubagentCompleted, updateParentSessionCost, Summarize, SummarizeWithPath, EnqueueSummaryAndDrain, SummarySubscribe, QueuedPrompts, QueuedPromptsList | `coordinator_subagent.go` |

**操作**：
1. 新建 `coordinator_control.go`，move 控制方法（保持 `func (c *coordinator)` receiver）
2. 新建 `coordinator_subagent.go`，move subagent + summary 方法
3. coordinator.go 保留生命周期 + 构建 + struct 定义
4. 不改方法签名，不改调用方——纯文件级搬迁

**验证**：`go build -buildvcs=false ./... && go vet ./...`

---

## W5: WeChat 解耦

**目标**：core 层零 WeChat 直接引用，通过 `messenger.Messenger` interface 完全隔离

**现状**：已有 `domain/messenger` 抽象（Messenger + Sender interface + NoopMessenger），但：
- coordinator.go 硬编码注册 `tools.NewWeChatSendImageTool` 等 3 个 WeChat 专用工具
- admin/server.go 直接 import `integration/wechat` 包（369 处引用中大部分在 admin + ui）
- UI 层有 `ui_wechat.go`

**操作**：
1. **coordinator.go**：把 3 个 WeChat 工具注册移到 integration 层（通过 hook/registration 函数注入），coordinator 只持有 `messenger.Messenger` interface
2. **tools**：把 `screenshot_to_wechat.go`、`send_wechat_file.go`、`send_wechat_image.go` 改名去掉 wechat 前缀，改为通用 messenger 工具
3. **admin/server.go**：提取 WeChat 管理逻辑到 `integration/wechat/admin_handler.go`，server.go 只注册路由
4. **UI**：`ui_wechat.go` 内容保留但通过 event/interface 驱动，不直接 import wechat 包

**验证**：`grep -r 'wechat\|WeChat' internal/core/` → 0 结果（domain/messenger 中的注释除外）

---

## 执行顺序

```
W1 (文档) → W2 (tools 迁移) → W3 (文件重命名) → W4 (coordinator 拆分) → W5 (WeChat 解耦)
```

W1-W3 是纯机械操作（文档/路径/命名），风险最低。
W4 是同包内文件搬迁，不改 API，中等风险。
W5 涉及跨层依赖调整，风险最高，放最后。

## 每步完成后

- `go build -buildvcs=false ./...`
- `go vet ./...`
- git commit（一个 W 一个 commit）
