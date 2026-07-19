# pubsub 集成：summary 完成事件推回 UI

> 修复方案 B 的关键依赖。M2 PR 主要内容。
> 见 [02-async-fix-options.md](02-async-fix-options.md)

## 现状：app.events 已经是 `Broker[tea.Msg]`

### 已有接入范式

`internal/core/app/app.go:64`：

```go
events *pubsub.Broker[tea.Msg]
```

**整个 app 事件总线已经收敛为 `tea.Msg`**——所有 `setupSubscriber` 转发时直接
`broker.Publish(pubsub.UpdatedEvent, tea.Msg(event))`（`app.go:578`）。

### 已有 9 个订阅源

`internal/core/app/app.go:534-552`：

| name | 来源 | 类型 |
|---|---|---|
| `permissions` | `app.Permissions.Subscribe` | `permission.Request` |
| `permissions-notifications` | `app.Permissions.SubscribeNotifications` | `permission.Notification` |
| `agent-notifications` | `app.agentNotifications.Subscribe` | `notify.Notification` |
| `mcp` | `mcp.SubscribeEvents` | `mcp.Event` |
| `lsp` | `SubscribeLSPEvents` | `lsp.Event` |
| `skills` | `skills.SubscribeEvents` | `skills.Event` |
| `sessions` | `app.Sessions.Subscribe` | `session.Session` |
| `messages` | `app.Messages.Subscribe` | `message.Message` |
| `history` | `app.History.Subscribe` | `history.File` |

每个源类型不同，通过泛型 `setupSubscriber[T any]`（`app.go:556`）反射式转成 `tea.Msg`
——这意味着**订阅源的 payload 是结构体**，到 app.events 边界才转 `tea.Msg`。

### 推回 UI 的链路

`internal/core/app/app.go:608-630`：

```go
func (app *App) Subscribe(program *tea.Program) {
    defer log.RecoverPanic("app.Subscribe", func() { ... })
    tuiCtx, tuiCancel := context.WithCancel(context.Background())
    defer tuiCancel()
    events := app.events.Subscribe(tuiCtx)
    for event := range events {
        program.Send(toTeaMsg(event))  // ← 此处需要 toTeaMsg 转换
    }
}
```

`internal/transport/cmd/root.go:122`：

```go
go ws.Subscribe(program)
```

> ⚠️ **细节**：app.go:629 的 `toTeaMsg(event)` 接受 `pubsub.Event[tea.Msg]`，
> payload 已经是 `tea.Msg`，所以 `toTeaMsg` 大概率是 type switch 分发到具体的
> `Update` handler。M2 实施时要核对 `toTeaMsg` 的实现确认 `SummaryResultMsg` 走哪个分支。

---

## M2 改造：summary 完成事件接入

### 方案选型

**采用「扩展 app.events 事件类型」**：增加一个 `SummaryCompletedMsg` 类型，让 UI 层
能 type switch 路由。不新设独立 broker（理由：app.events 已是统一入口，加新 broker
会破坏架构一致性）。

### 数据流

```
[slash `/summary`]
    ↓ tea.Cmd 立即返回 InfoMsg
[UI 立即显示 "Summarizing session…"]
    ↓ m.com.Workspace.AgentEnqueueSummary(sessionID)
[Coordinator.EnqueueSummaryAndDrain]
    ↓ c.summaryQueue.Add + c.drainQueuedSummaries
[goroutine: c.Summarize(context.Background(), sessionID)]
    ↓ 成功/失败
[summaryDone broker.Publish(SummaryCompletedMsg{SessionID, Path, Err})]
    ↓ setupSubscriber 转发
[app.events 收到 SummaryCompletedMsg]
    ↓ Subscribe loop → toTeaMsg
[UI Update 收到 SummaryCompletedMsg → type switch → InfoMsg/ReportError]
```

### SummaryCompletedMsg 定义

新文件 `internal/core/app/events_summary.go`（或者挂在现有 events 文件下）：

```go
package app

import "github.com/yourname/mocode/internal/transport/workspace"

// SummaryCompletedMsg is delivered to the TUI when an asynchronous
// session summary finishes (success or failure). Used by both the
// LLM-driven session_summary tool path and the /summary slash command.
type SummaryCompletedMsg struct {
    SessionID string
    Path      string  // empty on failure
    Err       error   // nil on success
}

func (SummaryCompletedMsg) teaMsgImpl() {}  // 或类似 marker
```

### Coordinator 暴露 broker

`internal/core/agent/coordinator.go`：

```go
type coordinator struct {
    // ...
    SummaryDone *pubsub.Broker[SummaryCompletedMsg]  // ← 新增
}

func NewCoordinator(...) (*coordinator, error) {
    // ...
    c.SummaryDone = pubsub.NewBroker[SummaryCompletedMsg]()
    return c, nil
}
```

> 注：`pubsub.NewBroker[T]()` 构造范式需要核对 `internal/util/pubsub/broker.go`
> 的构造函数签名（grep 没找到 `NewBroker` 字面量，可能叫别的）。M2 实施时校对。

### drainQueuedSummaries 改造

`internal/core/agent/session_summary_queue.go:41-49` 改为：

```go
func (c *coordinator) drainQueuedSummaries() {
    for _, sessionID := range c.summaryQueue.Drain() {
        sessionID := sessionID
        go func() {
            path, err := c.SummarizeWithPath(context.Background(), sessionID)
            c.SummaryDone.Publish(pubsub.UpdatedEvent, SummaryCompletedMsg{
                SessionID: sessionID,
                Path:      path,
                Err:       err,
            })
        }()
    }
}
```

新增 `c.SummarizeWithPath(ctx, sessionID) (string, error)`：

```go
func (c *coordinator) SummarizeWithPath(ctx context.Context, sessionID string) (string, error) {
    providerCfg, ok := c.cfg.Config().Providers.Get(c.currentAgent.Model().ModelCfg.Provider)
    if !ok {
        return "", errModelProviderNotConfigured
    }
    path, err := c.currentAgent.SummarizeEx(ctx, sessionID, getProviderOptions(c.currentAgent.Model(), providerCfg))
    return path, err
}
```

`SummarizeEx` 是 `currentAgent.Summarize` 的变体（多返回 path）——M2 实施时要看
`agent_lifecycle.go:594` 的签名决定是改 `Summarize` 还是新加 `SummarizeEx`。

### setupSubscriber 接入

`internal/core/app/app.go:534-552` 追加：

```go
setupSubscriber(ctx, app.serviceEventsWG, "summary",
    func(ctx context.Context) <-chan pubsub.Event[app.SummaryCompletedMsg] {
        return app.AgentCoordinator.SummaryDone.Subscribe(ctx)
    },
    app.events,
)
```

> 由于 setupSubscriber 是泛型闭包转发到 `tea.Msg`，payload 类型 `SummaryCompletedMsg`
> 要实现 `tea.Msg` 接口（`IsZero() bool` 或 type switch 兼容）。看具体 tea.Msg 定义。

### UI Update type switch

`internal/ui/model/ui.go` 或 `ui_dialogs.go` 的 `Update(msg tea.Msg)` 增加分支：

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

---

## session 级隔离（多 session 并行时）

**风险**：`app.events` 是**全局订阅**——意味着如果 TUI 同时打开了多个 session 标签，
每个 session 的 summary 完成事件**所有 session 的 UI 都会收到**。

**当前评估**：M2 阶段可接受。理由：
- summary 是低频操作（用户主动触发）
- TUI 多 session 并行场景罕见
- 即便多 session 收到通知，因为 `SessionID` 在 payload 里，UI 可按 session 过滤

**未来改进**：
- 把 broker 改为 `pubsub.Broker[SessionScopedEvent]`
- UI 订阅时过滤 `event.SessionID == m.session.ID`

## 与现有 9 个源共存

- topic 名 "summary" 与现有 8 个不冲突
- broker payload 类型 `SummaryCompletedMsg` 是新类型，与现有 `permission.Request` /
  `notify.Notification` 等独立
- `setupSubscriber` 泛型无需改

## 验收标准（M2）

1. `go test ./internal/core/app/ -run TestSummaryPubSub_Routes`：
   - 启动 mock broker，发布 `SummaryCompletedMsg{SessionID: "s1", Path: "/tmp/x.md"}`
   - 验证 `app.events` 收到该 Msg
2. `go test ./internal/core/agent/ -run TestDrainQueuedSummaries_PublishesOnDone`：
   - mock Summarize 返回 `(path, nil)`，验证 SummaryDone 发出
3. `go test ./internal/core/agent/ -run TestDrainQueuedSummaries_PublishesOnError`：
   - mock Summarize 返回 `("", errMock)`，验证 Err 字段
4. `go build ./...` 通过
5. 手动：执行 `/summary`，确认 UI 显示完成态 InfoMsg