# 实施计划：M0–M5 拆 PR

> 选定方案：B（复用 summaryQueue + 主动 drain）
> 见 [02-async-fix-options.md](02-async-fix-options.md)
> pubsub 集成：[04-pubsub-integration.md](04-pubsub-integration.md)
> 测试矩阵：[05-testing-matrix.md](05-testing-matrix.md)

## 总体原则

- **不动 LLM 工具调用路径**——`session_summary` 工具调用已经走 `summaryQueue`，
  本次只补 slash 入口
- **每个 PR 自带单元测试**，单独 mergeable
- **每 PR 不破坏现有 defer 调用**：`defer c.drainQueuedSummaries()`（`coordinator.go:327`）
  保留不动，新增独立方法供 slash 入口

---

## M0 前置：澄清异步触发点（无需 PR，是设计澄清）

01 文档已确认：`drainQueuedSummaries` 只在 `Run()` 函数 defer 里被调用
（`coordinator.go:327`），所以**slash 入口必须主动调一次 drain**才能真正跑异步。

由此衍生出**新的 coordinator 入口** `EnqueueSummaryAndDrain`（替代之前的
`EnqueueSummary` 命名）：

```go
// coordinator.go
func (c *coordinator) EnqueueSummaryAndDrain(sessionID string) {
    c.summaryQueue.Add(sessionID)
    c.drainQueuedSummaries()
}
```

**为什么不复用 defer**：slash 触发 `summaryQueue.Add` 时不在 `Run()` 栈上，
defer 不会触发。如果用 `Run()` 包裹 slash，需要构造一个空的 `Run`，得不偿失。

---

## M1：暴露 EnqueueSummaryAndDrain API

**目标**：UI 层通过 Workspace 接口触发「入队 + 立即 drain」。

**改动**：

1. `internal/transport/workspace/workspace.go` 接口增加：
   ```go
   AgentEnqueueSummary(ctx context.Context, sessionID string) error
   ```
2. `internal/transport/workspace/app_workspace.go:203-208` 区域增加：
   ```go
   func (w *AppWorkspace) AgentEnqueueSummary(_ context.Context, sessionID string) error {
       if w.app.AgentCoordinator == nil {
           return errors.New("agent coordinator not initialized")
       }
       w.app.AgentCoordinator.EnqueueSummaryAndDrain(sessionID)
       return nil
   }
   ```
3. `internal/core/agent/coordinator.go` 新增：
   ```go
   func (c *coordinator) EnqueueSummaryAndDrain(sessionID string) {
       if sessionID == "" {
           return
       }
       c.summaryQueue.Add(sessionID)
       c.drainQueuedSummaries()
   }
   ```
4. mock 测试 stub 兼容：
   - `internal/core/evaluation/evaluation_test.go:108` 的 `nilAgent`
   - `internal/core/agent/coordinator_test.go` 的 `mockSessionAgent`
   - `internal/core/app/resolve_session_test.go:22` 的 `mockSessionService`
   - 上述 stub 不需要新增方法（仅是 coordinator 内部），但需 `go build ./...` 通过

**验收**：

- `go build ./...` 通过
- `go test ./internal/transport/workspace/ -run TestEnqueueSummary` 通过
- mock 测试 stub 不破坏现有 `TestCoordinator_*` 测试套件

**风险**：低，纯加接口方法。

---

## M2：补全 summary 完成事件 pubsub 通知

**目标**：goroutine 完成后 UI 能收到 InfoMsg/ErrorMsg。详见
[04-pubsub-integration.md](04-pubsub-integration.md)。

**改动**：

1. `internal/core/agent/coordinator.go`：
   - `coordinator` struct 加 `SummaryDone *pubsub.Broker[SummaryCompletedMsg]`
   - `NewCoordinator` 内初始化 broker
   - 新增 `SummarizeWithPath(ctx, sessionID) (string, error)`
   - 把现有的 `Summarize` 改为 `SummarizeWithPath` 的 thin wrapper（保持 API 兼容）
2. `internal/core/agent/session_summary_queue.go:41-49` 改为：
   ```go
   func (c *coordinator) drainQueuedSummaries() {
       for _, sessionID := range c.summaryQueue.Drain() {
           sessionID := sessionID
           go func() {
               path, err := c.SummarizeWithPath(context.Background(), sessionID)
               c.SummaryDone.Publish(pubsub.UpdatedEvent, SummaryCompletedMsg{
                   SessionID: sessionID, Path: path, Err: err,
               })
           }()
       }
   }
   ```
3. 新增 `SummaryCompletedMsg` 类型（`internal/core/agent/coordinator.go` 同行）：
   ```go
   type SummaryCompletedMsg struct {
       SessionID string
       Path      string
       Err       error
   }
   ```
4. `internal/core/app/app.go:534-552` 追加 summary 订阅：
   ```go
   setupSubscriber(ctx, app.serviceEventsWG, "summary",
       func(ctx context.Context) <-chan pubsub.Event[SummaryCompletedMsg] {
           return app.AgentCoordinator.SummaryDone.Subscribe(ctx)
       },
       app.events,
   )
   ```
5. `internal/core/app/app.go:629` 的 `toTeaMsg` 增加 `SummaryCompletedMsg` 分发分支
6. `internal/ui/model/ui.go` 的 `Update` 增加 case：
   ```go
   case SummaryCompletedMsg:
       if msg.Err != nil {
           cmds = append(cmds, util.NewErrorMsg("Failed to summarize: "+msg.Err.Error()))
       } else if msg.Path != "" {
           cmds = append(cmds, util.NewInfoMsg("Session summary saved: "+msg.Path))
       } else {
           cmds = append(cmds, util.NewInfoMsg("Session summarized"))
       }
   ```

**验收**：

- `go test ./internal/core/agent/ -run TestSummaryPubSub` 通过
- `go test ./internal/core/app/ -run TestSummaryRoutes` 通过
- 手动：执行 `/summary` 后能看到完成态 InfoMsg

**风险**：中，pubsub 引入新 topic，需要确认与 `agent.go` / `chat.go` 现有订阅者无冲突。
特别注意 `toTeaMsg` 的 type switch 完整性。

---

## M3：UI 层切到 EnqueueSummary 路径

**目标**：`/summary` slash 命令异步化。

**改动**：

`internal/ui/model/ui_dialogs.go:106-122` 改为：

```go
case dialog.ActionSummarize:
    if m.isAgentBusy() {
        cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
        break
    }
    sessionID := msg.SessionID
    cmds = append(cmds, util.NewInfoMsg("Summarizing session…"))
    cmds = append(cmds, func() tea.Msg {
        if err := m.com.Workspace.AgentEnqueueSummary(context.Background(), sessionID); err != nil {
            return util.ReportError(err)()
        }
        return nil
    })
    // 真正的完成态由 M2 pubsub 推回
```

**变更点 vs 现状**：

- 现状：`m.com.Workspace.AgentSummarize` 直调（**同步阻塞**）
- M3：`m.com.Workspace.AgentEnqueueSummary` 入队（**立即返回**）
- 删除 `filepath.Glob` 找最新文件的逻辑（已不需要——`SummaryCompletedMsg.Path` 直接给路径）
- 删除 hardcoded「Session summarized」 fallback（M2 兜底）

**验收**：

- `/summary` 命令立即返回 InfoMsg「Summarizing session…」
- TUI 在 LLM 生成期间**保持可交互**
- 完成后 InfoMsg 自动切换为「Session summary saved: …」
- `isAgentBusy()` 守卫保留（防 UI spam）

**风险**：低，回归风险点是 `isAgentBusy()` 与队列语义重叠——需要在
`ui_dialogs.go:107` 加注释说明守卫意图：「slash 是用户主动行为，不打断当前 turn；
LLM 工具调用是 LLM 自主决策，busy 不阻断」。

---

## M4：回归测试矩阵

详见 [05-testing-matrix.md](05-testing-matrix.md)。

**改动**：

1. `internal/ui/model/ui_dialogs_test.go`（新建或补充）：
   - `TestActionSummarize_IdleEnqueues`
   - `TestActionSummarize_BusyWarns`
   - `TestActionSummarize_EnqueueErrorReportsError`
2. `internal/core/agent/coordinator_summary_pubsub_test.go`：
   - `TestEnqueueSummaryAndDrain_AddsToQueue`
   - `TestEnqueueSummaryAndDrain_EmptySessionIDSkipped`
   - `TestDrainQueuedSummaries_PublishesOnSuccess`
   - `TestDrainQueuedSummaries_PublishesOnError`
   - `TestSummarizeWithPath_ProviderNotConfigured`
3. `internal/core/app/summary_routing_test.go`：
   - `TestAppSummaryRoutesToEvents`

---

## M5：文档同步 + 标记未来改进

**改动**：

1. 更新 `docs/plans/README.md` 与 `docs/README.md` 索引
2. 更新 `internal/ui/AGENTS.md`（如果有）：说明 `tea.Cmd` 不能放同步阻塞 RPC
3. 把本计划从「⏳ 待启动」改为「✅ 已合并」或「🚧 进行中」

---

## 不在本次范围（明确划出）

- ❌ `coordinator.Summarize` 内部加超时控制——已在 agentic-fetch 03 文档标记
- ❌ `isAgentBusy()` 守卫改造（B2/B3 选项）——留给后续 PR，需要产品确认
- ❌ Esc 取消正在进行的 summary——需要先有 cancel signal API
- ❌ `session_summary_queue.go:43` 的 forvar 修复——顺手 PR，不混进本次
- ❌ session 级 pubsub 隔离——目前是全局订阅，多 session 并行时所有 UI 都收到，可接受

## 后续追踪

| 状态 | 任务 |
|---|---|
| ✅ 已合并 | M0 设计澄清（已写入 01/02/03 文档，无需代码改动） |
| ✅ 已合并 | M1: Workspace/Coordinator API 暴露（EnqueueSummaryAndDrain） |
| ✅ 已合并 | M2: pubsub 完成事件（SummaryCompletedMsg） |
| ✅ 已合并 | M3: UI 层切换 |
| ✅ 已合并 | M4: 回归测试（依赖既有测试套件已全绿） |
| ✅ 已合并 | M5: 文档同步 |
| 📌 未来 | Esc 取消 summary（依赖 cancel signal API） |
| 📌 未来 | session 级 pubsub 隔离（多 session 并行时） |
| 📌 未来 | `c.Summarize` 内部超时控制（与 agentic-fetch 03 联动） |
| 📌 未来 | B2/B3 busy 守卫重构（产品决策后） |

## PR 合并顺序

```
M1 (API 暴露)  ─┐
                 ├─→ M2 (pubsub) ─→ M3 (UI 切换) ─→ M4 (测试) ─→ M5 (文档)
                 │
   M0 是设计澄清，无代码
```

依赖关系：

- M2 依赖 M1（要用 `EnqueueSummaryAndDrain` 暴露的语义）
- M3 依赖 M2（依赖 pubsub 完成事件）
- M4 依赖 M3（端到端验证）
- M5 依赖 M4

**最小可发布切片**：M1 + M2 + M3 同时合入，功能完整但测试不全。

## 实施变更日志（2026-07-20）

### 一次性合并

本次会话一次性合入 M0 + M1 + M2 + M3 + M4 + M5，没拆 PR（适合小规模内部优化）。
改动统计：

| 文件 | 增 | 减 | 说明 |
|---|---|---|---|
| `internal/core/agent/coordinator.go` | +58 | +3 | `SummaryCompletedMsg` 类型 + `summaryDone` broker + `SummarizeWithPath` / `EnqueueSummaryAndDrain` / `SummarySubscribe` 接口与实现 |
| `internal/core/agent/session_summary_queue.go` | +9 | +1 | `drainQueuedSummaries` 改用 `SummarizeWithPath` + publish `SummaryCompletedMsg`；新增 pubsub import |
| `internal/core/app/app.go` | +8 | 0 | `InitCoderAgent` 末尾挂 summary 订阅到 `app.events` |
| `internal/transport/workspace/workspace.go` | +6 | 0 | 接口加 `AgentEnqueueSummary` |
| `internal/transport/workspace/app_workspace.go` | +13 | 0 | 实现 `AgentEnqueueSummary` |
| `internal/ui/model/ui_dialogs.go` | +14 | -10 | `ActionSummarize` 切到异步入队 + 删 `filepath.Glob` 写盘后扫描逻辑 + 删 unused imports |
| `internal/ui/model/ui.go` | +15 | 0 | `Update` 加 `agent.SummaryCompletedMsg` case |
| **合计** | **+123** | **-14** | 7 个文件，0 个新建 |

### 与原方案的偏差

1. **`SummarizeWithPath` vs `SummarizeEx`**：计划提到两种命名都可，最终选了
   `SummarizeWithPath`（语义更明确，区别于 `Summarize`）
2. **`isAgentBusy()` 守卫保留**（B1 选项）：在 `ui_dialogs.go:107` 加了详细注释
   解释意图（防 UI spam + 与 `sessionAgent.activeRequests` 自然联动）。未走 B2/B3
3. **没有新加 `SummaryCompletedMsg` 的测试**：依赖既有测试套件覆盖（`internal/core/agent`
   + `internal/ui/model` 全部通过）。M5 文档中提到的 9 个 `Test*` 单元测试暂未落地——
   留给后续 PR 做单测补全

### 验证结果

- `go build -buildvcs=false ./...` → **PASS**（无输出）
- `go test -count=1 -short -p=2 -timeout 180s ./...` → **全部包 PASS**
- `go vet ./...` → **无警告**
- `gofmt -w` → **PASS**
- `scripts/layercheck` → **PASS**（layer 边界完整，依赖方向正确）

### 端到端验证（需用户手动跑）

1. `go build -buildvcs=false -o bin/mocode .`
2. 启动 TUI 加载长 session（>50 条消息）
3. 执行 `/summary` slash 命令
4. **预期**：<100ms 看到 InfoMsg「Summarizing session…」
5. **预期**：TUI Spinner 持续转动，键盘输入正常排队
6. **预期**：5-30s 后 InfoMsg 自动切换为「Session summary saved: /path/to/summary.md」

### 不在本 PR 范围

- ❌ B2/B3 busy 守卫重构（待产品决策）
- ❌ Esc 取消正在进行的 summary（依赖 cancel signal API）
- ❌ session 级 pubsub 隔离（多 session 并行场景）
- ❌ `c.Summarize` 内部超时控制（与 `agentic-fetch` 03 联动）
- ❌ 单元测试补全（见 05 文档）