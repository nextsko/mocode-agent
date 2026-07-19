# `/summary` slash 命令同步阻塞 TUI 根因

## 现象

在 TUI 中执行 `/summary` slash 命令（或 Commands 对话框里的 `Summarize Session` 项），
期间整个终端界面**完全冻结**——键盘无响应、鼠标事件堆积、Spinner 不动。LLM 流式
响应一旦结束 + 写盘完成后才解锁。体感是「卡住十几秒到几十秒」。

## 与 `session_summary` 工具调用对比

| 入口 | 文件:行 | 调度方式 | TUI 阻塞 |
|---|---|---|---|
| LLM 工具调用 `session_summary` | `tools/session_summary_session_summary.go:48-74` | `summaryQueue.Add` → `coordinator.go:641` 入队 → **`Run` 退出时 `defer c.drainQueuedSummaries()`** 异步 | **否** |
| **`/summary` slash 命令** | `internal/ui/dialog/commands_items.go:289` → `ui_dialogs.go:106-122` | `tea.Cmd` 同步执行 → `Workspace.AgentSummarize(ctx, sessionID)` 直调 `coordinator.Summarize` | **是** |
| **Commands 对话框 `Summarize Session`** | `internal/ui/dialog/commands_items.go:161` → 同上 handler | 同上 | **是** |

> ⚠️ **重要修正（与 02/03 笔记之前描述的差异）**：
> 之前 02 文档的对比表里把 `session_summary` 路径描述成"真异步"——但 `drainQueuedSummaries`
> **只在 `Run` 函数的 `defer` 里被调用**（`coordinator.go:327`），所以实际行为是：
>
> 1. 工具调用 `session_summary(action=schedule)` → `summaryQueue.Add`（O(1) 内存操作）
> 2. LLM 返回 → `Run` 收尾 → `defer c.drainQueuedSummaries()`
> 3. 此刻才真正 `go func` 异步跑 summary
>
> **意味着 `session_summary` 工具调用在 LLM turn 中间是 0 延迟的，但 summary 实际生成要
> 等到 turn 结束才启动**。这一延迟对工具调用路径几乎无感（用户已经在等 LLM 回复），
> 但对 slash 入口同样套用此模式就有问题——slash 没有"turn 结束"概念。**修复方向因此需要
> 调整**：要么新增独立的"slash enqueue"路径，要么在 `AgentSummarize` 内部立即触发
> drain。见 [03-implementation-plan.md](03-implementation-plan.md) §「M0 前置：澄清异步触发点」。

## 关键代码

### 1. TUI 同步路径（阻塞）

`internal/ui/model/ui_dialogs.go:106-122`：

```go
case dialog.ActionSummarize:
    if m.isAgentBusy() {
        cmds = append(cmds, util.ReportWarn("Agent is busy, please wait before summarizing session..."))
        break
    }
    cmds = append(cmds, func() tea.Msg {
        err := m.com.Workspace.AgentSummarize(context.Background(), msg.SessionID)
        if err != nil {
            return util.ReportError(err)()
        }
        // 写盘后 glob 找最新 summary 文件
        pattern := filepath.Join(m.com.Workspace.WorkingDir(),
            sessionexport.SummaryDir,
            "summary-"+sessionexport.SanitizeName(msg.SessionID)+"-*.md")
        matches, _ := filepath.Glob(pattern)
        if len(matches) > 0 {
            return util.NewInfoMsg("Session summary saved: " + matches[len(matches)-1])
        }
        return util.NewInfoMsg("Session summarized")
    })
```

`func() tea.Msg { ... }` 是 Bubble Tea 的 `tea.Cmd`，由 Update 循环同步执行——
**Update 不返回就不处理下一帧**，整个 TUI 静止。

### 2. Workspace → Coordinator 直调

`internal/transport/workspace/app_workspace.go:203-208`：

```go
func (w *AppWorkspace) AgentSummarize(ctx context.Context, sessionID string) error {
    if w.app.AgentCoordinator == nil {
        return errors.New("agent coordinator not initialized")
    }
    return w.app.AgentCoordinator.Summarize(ctx, sessionID)
}
```

无 goroutine、无队列包装。

### 3. Coordinator.Summarize 也是同步

`internal/core/agent/coordinator.go:1326-1333`：

```go
func (c *coordinator) Summarize(ctx context.Context, sessionID string) error {
    providerCfg, ok := c.cfg.Config().Providers.Get(c.currentAgent.Model().ModelCfg.Provider)
    if !ok {
        return errModelProviderNotConfigured
    }
    _, err := c.currentAgent.Summarize(ctx, sessionID, getProviderOptions(c.currentAgent.Model(), providerCfg))
    return err
}
```

直调 `currentAgent.Summarize`（`agent_lifecycle.go:594`），整个 LLM 流式响应 + 解析
+ 写盘全在调用栈里串行完成。

### 4. 反例：异步路径存在，但触发时机晚于预期

`internal/core/agent/session_summary_queue.go:41-49`：

```go
func (c *coordinator) drainQueuedSummaries() {
    for _, sessionID := range c.summaryQueue.Drain() {
        sessionID := sessionID  // 已被 gopls forvar 标记为不必要拷贝（下次顺手修）
        go func() {
            if err := c.Summarize(context.Background(), sessionID); err != nil {
                slog.Error("scheduled session summary failed", "session_id", sessionID, "error", err)
            }
        }()
    }
}
```

调用方只有一个：`internal/core/agent/coordinator.go:327`：

```go
// 在 Run() 函数尾部
defer c.drainQueuedSummaries()
```

所以**真正的异步触发点**：

- 工具调用路径：LLM 当前 turn 结束 → `Run` defer → drain goroutine
- **slash 入口**：没有 Run() 包裹，套此 defer 不会触发

**设计影响**：让 slash 走 `summaryQueue.Add` 之后，必须**主动调一次 drain**
（要么新方法 `EnqueueSummaryAndDrain`，要么在 `EnqueueSummary` 内部直接 drain）。
`session_summary_queue.go:42` 的循环也可以跳过 defer 检查直接 drain，因为入队者已经
明确意图是"立刻执行"。

**ctx 切断已验证可行**：`drainQueuedSummaries:45` 用 `context.Background()` 切断原 ctx，
意味着 `c.Summarize` 已经被验证可以无 ctx 安全地跑——slash 入口可以完全套用。

## 触发条件量化

- `/summary` 阻塞时长 ≈ LLM 流式生成 summary 的时间 + 写盘时间
- 长 session（千条消息）走 `agent_lifecycle.go:594` 之前还会先做上下文压缩（`ctxcompress`）
  → 阻塞时长可能进一步拉长
- Agent 在 busy 状态（`m.isAgentBusy()`）时 slash 会被守卫拦截并 warn，不会触发阻塞；
  阻塞只在 idle 状态下发生
- **回归场景**：在 `summaryQueue` 已经有挂起的 session 时执行 `/summary`：
  - 当前行为（同步路径）：直接覆盖队列，前面挂起的 session 被覆盖（见
    `session_summary_queue.go:24` map 写入语义），本次同步执行一次
  - 修复后行为（异步路径）：如果用 `summaryQueue.Add` 复用，前一个挂起 session
    会被覆盖——**这种行为差异需要产品决策**（见 [02-async-fix-options.md](02-async-fix-options.md) §「语义差异」）

## 与 AgentBusy 守卫的微妙耦合

`ui_dialogs.go:107-110` 的 `m.isAgentBusy()` 守卫有两个用途：

1. **防并发触发**：用户连续按 Enter 时不会重复触发
2. **隐性 busy 阻断**：如果上一个 turn 还在等 LLM，slash 直接 warn

但在工具调用路径里，**busy 状态时 LLM 也可能调用 `session_summary`**
——所以 busy 不是 summary 调用的硬阻塞。换言之：

- **busy + slash `/summary`** → 拒绝（合理，因为 turn 还没结束）
- **busy + LLM 工具调用 `session_summary`** → 接受（合理，因为 LLM 当前 turn 决定 schedule）

异步化后，slash 路径的"busy 拒绝"可能反而是体验退化——用户能看到工具调用能 schedule，
但 slash 不能。需要产品确认：
- A. 保留 busy 拒绝（与现状一致）
- B. 去掉 busy 守卫，依赖 `summaryQueue` map 去重（更激进）

见 [02-async-fix-options.md](02-async-fix-options.md) §「语义差异」。

## 为什么这条 bug 没被早期发现

1. `session_summary` 工具调用场景**已经把异步通道铺好了**，开发者做工具调用路径时
   自然使用了队列
2. `summarize` slash 命令是早期 UI 功能，可能是 AgentSummarize 接口直接套进 `tea.Cmd`
   后没回头验证 TUI 体感
3. `isAgentBusy()` 守卫掩盖了一部分问题——并发触发时只 warn 不执行，看起来"安全"，
   但单用户视角的 idle 阻塞没人测

## 验证步骤

1. 打开长 session（>50 条消息）
2. `m.com.Workspace.AgentSummarize` 入口处加 `slog.Debug` + `time.Now()`
3. TUI 中执行 `/summary`
4. 在 TUI 主循环 Update 入口加 `slog.Debug` 看是否在阻塞期间有 frame tick
5. 预期：阻塞期间 0 个 tick；解除后 1 个 tick

修复后预期：阻塞期间正常 tick（>10 FPS），`/summary` 立即返回 InfoMsg「Summarizing…」，
goroutine 完成后 InfoMsg 切换成完成态。