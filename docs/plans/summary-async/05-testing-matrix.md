# 回归测试矩阵

> M4 PR 主要内容。
> 见 [03-implementation-plan.md](03-implementation-plan.md) §「M4：回归测试矩阵」

## 测试分层

| 层 | 工具 | 覆盖 |
|---|---|---|
| 单元 | `go test -run` 单函数 | 入队语义、drain 行为、pubsub 发出 |
| 集成 | `go test ./internal/core/app/` | app.events 路由链 |
| UI | `go test ./internal/ui/model/` | Update type switch 分发 |
| 端到端 | 手动 + slog | TUI 体感（键盘、Spinner、消息） |

## 单元测试矩阵

### `internal/core/agent/coordinator_summary_test.go`（新建）

| 测试名 | 前置 | 步骤 | 预期 |
|---|---|---|---|
| `TestEnqueueSummaryAndDrain_AddsToQueue` | 空 queue | 调 `EnqueueSummaryAndDrain("s1")` | queue 含 "s1" |
| `TestEnqueueSummaryAndDrain_EmptySessionIDSkipped` | 空 queue | 调 `EnqueueSummaryAndDrain("")` | queue 仍空，无 drain |
| `TestEnqueueSummaryAndDrain_DuplicateIDOverwrites` | queue 含 "s1" | 再调 `EnqueueSummaryAndDrain("s1")` | queue 仍含 "s1"（map 写语义） |
| `TestSummarizeWithPath_ProviderNotConfigured` | provider 未注册 | 调 `SummarizeWithPath(ctx, "s1")` | 返回 `("", errModelProviderNotConfigured)` |
| `TestSummarizeWithPath_SuccessEmitsPath` | mock Summarize 返回 `("/tmp/x.md", nil)` | 调 `SummarizeWithPath(ctx, "s1")` | 返回 `("/tmp/x.md", nil)` |
| `TestSummarizeWithPath_FailureEmitsEmptyPath` | mock Summarize 返回 `("", errMock)` | 调 `SummarizeWithPath(ctx, "s1")` | 返回 `("", errMock)` |
| `TestDrainQueuedSummaries_PublishesOnSuccess` | queue 含 "s1"；mock Summarize 成功 | 调 `drainQueuedSummaries` | 收到 `SummaryCompletedMsg{SessionID: "s1", Path: "/tmp/x.md", Err: nil}` |
| `TestDrainQueuedSummaries_PublishesOnError` | queue 含 "s1"；mock Summarize 失败 | 调 `drainQueuedSummaries` | 收到 `SummaryCompletedMsg{SessionID: "s1", Path: "", Err: errMock}` |
| `TestDrainQueuedSummaries_EmptyQueue` | 空 queue | 调 `drainQueuedSummaries` | 不发出任何事件，不 panic |

### `internal/core/app/summary_routing_test.go`（新建）

| 测试名 | 前置 | 步骤 | 预期 |
|---|---|---|---|
| `TestAppSummaryRoutesToEvents` | 启动 app，订阅 app.events | publish `SummaryCompletedMsg{SessionID: "s1", Path: "/tmp/x.md"}` | app.events 收到 `tea.Msg` 形态的 SummaryCompletedMsg |
| `TestAppSummaryDoesNotBlockOnFullSubscriber` | 订阅者 buffer 满 | publish | publisher 不阻塞（pubsub broker 行为） |

### `internal/ui/model/ui_dialogs_test.go`（新建或补充）

| 测试名 | 前置 | 步骤 | 预期 |
|---|---|---|---|
| `TestActionSummarize_IdleEnqueues` | mock `isAgentBusy=false`；mock workspace | 发送 `ActionSummarize{SessionID: "s1"}` | 收到 `AgentEnqueueSummary` 调用；收到 InfoMsg「Summarizing session…」 |
| `TestActionSummarize_BusyWarns` | mock `isAgentBusy=true` | 发送 `ActionSummarize{SessionID: "s1"}` | 收到 Warn；**不**调用 `AgentEnqueueSummary` |
| `TestActionSummarize_EnqueueErrorReportsError` | mock workspace.AgentEnqueueSummary 返回 err | 发送 `ActionSummarize{SessionID: "s1"}` | 收到 ReportError msg |
| `TestSummaryCompletedMsg_RoutesToInfoMsg` | 现有 Update 函数 | 发送 `SummaryCompletedMsg{Path: "/tmp/x.md", Err: nil}` | 收到 `util.NewInfoMsg("Session summary saved: /tmp/x.md")` cmd |
| `TestSummaryCompletedMsg_RoutesToErrorMsg` | 现有 Update 函数 | 发送 `SummaryCompletedMsg{Err: errMock}` | 收到 `util.NewErrorMsg("Failed to summarize: …")` cmd |
| `TestSummaryCompletedMsg_RoutesToFallbackInfoMsg` | 现有 Update 函数 | 发送 `SummaryCompletedMsg{Path: "", Err: nil}` | 收到 `util.NewInfoMsg("Session summarized")` cmd |

## 集成测试矩阵

### `internal/transport/workspace/workspace_integration_test.go`（新建）

| 测试名 | 前置 | 步骤 | 预期 |
|---|---|---|---|
| `TestWorkspaceEnqueueSummary_CallsCoordinator` | mock app.AgentCoordinator | 调 `AppWorkspace.AgentEnqueueSummary(ctx, "s1")` | coordinator.EnqueueSummaryAndDrain 被调用 |
| `TestWorkspaceEnqueueSummary_NilCoordinatorReturnsError` | app.AgentCoordinator = nil | 调 `AppWorkspace.AgentEnqueueSummary` | 返回 `"agent coordinator not initialized"` |

## 端到端手动验证

### V1：基本异步化验证

1. 启动 mocode TUI
2. 在 slog 启用 `slog.LevelDebug`
3. 执行 `/summary`
4. **预期**：<100ms 收到 InfoMsg「Summarizing session…」
5. **预期**：slog 看到 `coordinator.EnqueueSummaryAndDrain called` + `drainQueuedSummaries goroutine started`
6. **预期**：TUI Spinner 持续转动，键盘可输入字符（输入会进入队列不丢）
7. **预期**：5-30s 后 slog 看到 `SummaryCompletedMsg published` + TUI 出现 InfoMsg「Session summary saved: …」

### V2：busy 守卫验证

1. 启动 LLM turn（输入 prompt 等待回复）
2. **不**等 LLM 返回，立即执行 `/summary`
3. **预期**：立刻看到 Warn「Agent is busy, please wait before summarizing session…」
4. **预期**：slog 无 `EnqueueSummaryAndDrain called`
5. 等 LLM 返回后再次执行 `/summary`
6. **预期**：正常异步流程

### V3：失败路径验证

1. 构造一个故意失败的 provider（如 API key 失效）
2. 执行 `/summary`
3. **预期**：立即 InfoMsg「Summarizing session…」
4. **预期**：5-30s 后 TUI 出现 ErrorMsg「Failed to summarize: …」

### V4：连续两次 `/summary` 验证（B1 守卫语义）

1. 第一次执行 `/summary`，立即看到 InfoMsg「Summarizing session…」
2. 间隔 1s 第二次执行 `/summary`
3. **预期（B1）**：第二次被 busy 守卫拦截（虽然 slash 不像 LLM 那样有 turn，isAgentBusy
   仍然会因为队列未消费而 busy——**需要 M1 测试时确认 `isAgentBusy()` 语义**）
4. **备选预期**：两次都成功入队，drain 触发两次 goroutine（last-write-wins on map）

> ⚠️ V4 行为依赖 `isAgentBusy()` 在 drain 期间是否标记 busy。M3 实施时需要明确。

## 性能基准（非必需）

如果想量化改进，记录 `/summary` 端到端时间：

| 场景 | 现状 | 目标 |
|---|---|---|
| `/summary` 提交到 UI 响应 | 同步阻塞 5-30s | <100ms |
| TUI Spinner 转动帧率 | 0 FPS | >10 FPS |
| 鼠标/键盘响应延迟 | 5-30s | <50ms |

## 覆盖范围检查

修复涉及 4 个文件：

- `internal/core/agent/coordinator.go` → unit tests above
- `internal/core/agent/session_summary_queue.go` → `TestDrainQueuedSummaries_*`
- `internal/transport/workspace/app_workspace.go` → `TestWorkspaceEnqueueSummary_*`
- `internal/ui/model/ui_dialogs.go` → `TestActionSummarize_*`

**覆盖率目标**：每个改动函数 ≥80% 行覆盖。

## 不在测试范围

- 端到端 LLM 流式响应正确性（属于 `agent_lifecycle_test.go` 已覆盖范围）
- 真实 provider 调用（mock 替代）
- 磁盘写盘的 fsync 行为（属于 `sessionexport_test.go` 已覆盖）

## 验收 checklist

- [ ] 所有单测通过
- [ ] `go test ./...` 全绿
- [ ] mock 测试 stub 兼容现有套件
- [ ] V1-V4 端到端手动验证通过
- [ ] 性能基准（可选）记录
- [ ] 覆盖率 ≥80%