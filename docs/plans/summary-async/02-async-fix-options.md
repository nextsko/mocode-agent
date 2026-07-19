# `/summary` 异步化修复方案对比

## 修复目标

- TUI 在 `/summary` 期间**保持交互**（键盘、鼠标、Spinning 正常）
- 立即返回「Summarizing…」InfoMsg，goroutine 完成后切换为「Session summary saved: …」
- 与现有 `session_summary` 工具调用路径**行为一致**

## 关键前提澄清

在对比方案前，先固化两个事实（与 01 文档保持一致）：

1. **`drainQueuedSummaries` 只在 `Run()` 函数 `defer` 里被调用**
   （`coordinator.go:327`）——所以"复用 summaryQueue"方案需要 **slash 入口主动调一次
   drain**，否则入队后没人触发执行
2. **`isAgentBusy()` 是 slash 入口独有的守卫**——LLM 工具调用 `session_summary`
   不检查 busy 状态（LLM 当前 turn 期间就能 schedule）。这意味着异步化时
   busy 守卫的语义需要单独决策

---

## 三种候选方案

### 方案 A：就地异步化（最小改动）

把 `ui_dialogs.go:111-122` 的 `tea.Cmd` 内部改成 `go func` + channel 回灌。

```go
case dialog.ActionSummarize:
    cmds = append(cmds, func() tea.Msg {
        workspace := m.com.Workspace
        sessionID := msg.SessionID
        return util.NewInfoMsg("Summarizing session…")  // 立即响应
    })
    // 真正的 goroutine
    cmds = append(cmds, func() tea.Msg {
        go func() {
            err := m.com.Workspace.AgentSummarize(context.Background(), msg.SessionID)
            // …回灌 InfoMsg 需要 newMcp pubsub 或 channel
        }()
        return nil
    })
```

**问题**：

- Bubble Tea 不允许从 goroutine 直接 `Send` Msg（需要 `tea.Program` 引用）
- 需要在 UI model 里持有 `*tea.Program` 才能异步回灌——目前 `m.com.Workspace`
  不持有 program 引用
- 改造面会扩散到 `ui_completions.go`、`ui_dialogs.go`、`ui.go` 等多处
- 重复造轮子：pubsub 已有，channel 是次优实现

**评估**：❌ 不推荐，破坏现有架构

---

### 方案 B：复用 `summaryQueue`（推荐）✅

让 `ActionSummarize` 也走 `coordinator.summaryQueue`，slash 和工具调用共用一条
异步路径。

```go
case dialog.ActionSummarize:
    if m.isAgentBusy() {
        cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
        break
    }
    // 立即返回「开始」消息
    cmds = append(cmds, util.NewInfoMsg("Summarizing session…"))
    // 入队 + 主动 drain（与工具调用路径的关键差异）
    sessionID := msg.SessionID
    cmds = append(cmds, func() tea.Msg {
        m.com.Workspace.AgentEnqueueSummary(context.Background(), sessionID)
        return nil
    })
    // …完成态 InfoMsg 通过 pubsub 推回 UI（见 04 文档）
```

**API 改动**：

1. `Workspace.AgentEnqueueSummary(ctx, sessionID) error` → `Coordinator.EnqueueSummaryAndDrain(sessionID)`
2. `coordinator.go` 新增方法：
   ```go
   func (c *coordinator) EnqueueSummaryAndDrain(sessionID string) {
       c.summaryQueue.Add(sessionID)
       c.drainQueuedSummaries()  // 立即触发，不等 defer
   }
   ```
3. `app_workspace.go` 实现：
   ```go
   func (w *AppWorkspace) AgentEnqueueSummary(_ context.Context, sessionID string) error {
       if w.app.AgentCoordinator == nil {
           return errors.New("agent coordinator not initialized")
       }
       w.app.AgentCoordinator.EnqueueSummaryAndDrain(sessionID)
       return nil
   }
   ```
4. 完成态通知：见 [04-pubsub-integration.md](04-pubsub-integration.md)

**优势**：

- 与 `session_summary` 工具调用路径**完全同构**（入队 + drain 一致）
- 行为一致：busy 时 warn、idle 时立即返回、goroutine 跑完后通知
- 改动面最小：只动 `ui_dialogs.go`、`app_workspace.go`、`coordinator.go` 三处
- **`EnqueueSummaryAndDrain` 命名明确语义**，未来若想分离"仅入队"和"立即触发"也清晰

**劣势**：

- 需要新 pubsub topic 给完成事件
- `isAgentBusy()` 守卫的语义需要重新审视（见下方「语义差异」）

---

### 方案 C：纯前端调度（不上后端队列）

保留 `Workspace.AgentSummarize` 同步语义，但在 UI 层加一个**假的进度反馈**。

```go
cmds = append(cmds, func() tea.Msg {
    go func() {
        // 30ms 后推送「正在生成」
        time.Sleep(30 * time.Millisecond)
        // …
    }()
    return util.NewInfoMsg("Summarizing session…")
})
```

**评估**：❌ 拒绝——治标不治本，TUI 仍然无法响应 Spinner，且没有任何机制能让
AgentSummarize 不阻塞 Update loop。

---

## 选定方案：B（复用 summaryQueue + 主动 drain）

理由：与现有异步路径同构、改动面小、行为一致、验证充分（`drainQueuedSummaries`
已跑通）。

---

## 语义差异（产品需确认）

### 差异 1：busy 守卫

| 入口 | busy 时行为 |
|---|---|
| LLM 工具调用 `session_summary` | 不检查 busy → 接受 schedule |
| **`/summary` slash 现状** | 检查 busy → warn 拒绝 |
| **`/summary` slash 修复后（B 方案默认）** | 检查 busy → warn 拒绝 |

**风险**：用户看到 LLM 能 schedule、slash 不能，体验割裂。

**选项**：

- **B1**（保守）：保留 `isAgentBusy()` 守卫，注释说明意图是「防 UI spam」
- **B2**（激进）：去掉 busy 守卫，依赖 `summaryQueue` map 去重 + `summaryQueue.Add`
  在 `session_summary_queue.go:24` 已经天然去重
- **B3**（折中）：保留 busy 守卫，但若 busy 中调用走"延迟执行"分支（drain 时机延后
  到 turn 结束），与工具调用行为完全对齐

**推荐**：B1，注释清楚「slash 是用户主动行为，需要不打断当前 turn；LLM 工具调用是
LLM 自主决策，busy 不阻断」。

### 差异 2：重复入队语义

`summaryQueue` 的 `Add` 是 map 写（`session_summary_queue.go:24`）——重复 sessionID
会**覆盖**。这意味着：

- **工具调用 `session_summary(action=schedule)` 多次**：每次都覆盖前一次，效果上
  是"最后一次 schedule 生效"
- **slash 重复触发**：当前是 busy 守卫拦截；修复后若守卫保留则进不来，若去掉则
  会覆盖

**风险**：低（summary 是低频操作，重复触发的概率低）

### 差异 3：写盘文件路径

`ui_dialogs.go:118-121` 当前用 `filepath.Glob` 找最新写盘文件——这是**写后 glob**，
依赖文件系统同步可见。如果改成异步 + pubsub：

```go
// 当前（写后 glob）
matches, _ := filepath.Glob(pattern)
if len(matches) > 0 {
    return util.NewInfoMsg("Session summary saved: " + matches[len(matches)-1])
}
```

应该改成 **`SummaryResult` 直接带路径**（见 [04-pubsub-integration.md](04-pubsub-integration.md)
§「SummaryResult 结构」）：

```go
type SummaryResult struct {
    SessionID string
    Path      string  // 写盘后填入；失败时为空
    Err       error
}
```

`Summarize` 函数成功返回时把 `Path` 填入 → pubsub 推 → UI 渲染「Session summary
saved: <Path>」。

**收益**：不再依赖 glob，避免多 session 并发时 glob 命中错误的最新文件。

---

## 实施风险

| 风险 | 等级 | 缓解 |
|---|---|---|
| pubsub topic 与现有冲突 | 低 | 起新名如 `summary.completed` |
| `isAgentBusy()` 守卫与队列语义重复 | 低 | 保留 B1 守卫，doc 注释说明 |
| 写盘失败如何通知 UI | 中 | pubsub 推 `util.ReportError`，需在 `ui.go` 订阅 |
| 长 session 压缩耗时长 | 中 | 用户已能接受工具调用耗时，slash 接受同样耗时合理 |
| `context.Background()` 无超时保护 | 中 | 已在 03 文档标记待评估；不在本 PR 修复范围 |
| `drainQueuedSummaries` 与现有 defer 调用冲突 | 低 | drain 是幂等的（Drain 返回后 map 清空）——重复调用安全 |
| 多 session 并行 summary | 中 | pubsub 是全局订阅，所有 UI 都收——见 [04-pubsub-integration.md](04-pubsub-integration.md) §「session 级隔离」 |

## 验收标准

1. `/summary` 命令在 TUI 中**立即返回**「Summarizing session…」（<100ms）
2. 期间 TUI Spinner 持续转动、键盘输入正常排队（Esc 取消如果支持）
3. 完成后 UI 显示「Session summary saved: …」
4. 失败时显示「Failed to summarize: …」
5. `go test ./internal/core/agent/ -run TestEnqueueSummary` 通过
6. 现有 `session_summary` 工具调用行为**不变**（回归测试）
7. **新增**：busy 状态下 `/summary` 仍被守卫拒绝（与现状一致）
8. **新增**：连续两次 `/summary`（间隔 <1s）行为可预测——B1 选型下第二次 warn 拒绝