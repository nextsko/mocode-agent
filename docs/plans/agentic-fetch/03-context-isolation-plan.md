# 子 Agent context 隔离改造计划

> 记录时间：2026-07-20  
> 状态：草案（待评审）  
> 依赖：先看 `01-context-canceled-failure.md`

---

## 🎯 目标

把子 Agent（sub-agent）的 `context.Context` 生命周期与父 Agent 解耦，避免父 context 的 cancel 影响子任务执行；同时把"软取消"（网络超时）vs"硬取消"（用户主动）分开处理。

---

## 🧠 核心问题

`internal/core/agent/agent_lifecycle.go:115`
```go
genCtx, cancel := context.WithCancel(ctx)
```

直接把父 ctx 作为父 context，导致：
1. 父 ctx deadline 到期 → 子 Agent 立即被砍
2. 父 ctx 被 CancelAll() → 全部已派生子 Agent 一锅端
3. 父 ctx 的任何 cancellation（LLM stream cancel、provider close）→ 子 Agent 半路被打断

但实际上，用户的 Esc 是要打断主 agent 的思考，子 agent 一般已经在做机械检索，让它继续才合理。

---

## 📐 改造方案

### 1. 子 Agent 独立 context

新增 `agent_lifecycle.go:genCtxWithBudget(parentCtx context.Context, budget time.Duration) (context.Context, context.CancelFunc, context.CancelReason)`：

```go
func genCtxWithBudget(parentCtx context.Context, budget time.Duration) (context.Context, context.CancelFunc, *cancelReason) {
    // 1) 子 Agent 自己的 deadline，不继承父 ctx 的 deadline
    subCtx, cancel := context.WithTimeout(
        context.WithoutCancel(parentCtx),  // 关键：丢掉父 cancel，仅保留 Values
        budget,
    )
    return subCtx, cancel, &cancelReason{}
}
```

调用点：`agent_lifecycle.go:115` 替换为 `genCtxWithBudget(ctx, 90*time.Second)`。

为什么不直接 `WithoutCancel`：
- 仍然需要保留 `ctx.Done()` 链路（用于取消时通知内层 SDK）
- 仍然需要保留 `context.Value()`（用于 traceID、sessionID）

### 2. 软取消 vs 硬取消分类

新增 `internal/core/agent/cancellation.go`：

```go
package agent

// CancelReason 区分硬取消与软取消
type CancelReason int

const (
    CancelNone CancelReason = iota
    CancelUser       // 用户主动 Esc
    CancelParent     // 父 Agent 主动 cancel
    CancelTimeout    // 子 Agent 自己 deadline
    CancelNetwork    // Provider SDK RequestTimeout
    CancelShutdown   // 应用退出
)

// ClassifyCancel 把 err 翻译成具体原因
func ClassifyCancel(err error) CancelReason {
    switch {
    case err == nil:
        return CancelNone
    case errors.Is(err, ErrUserCancelled):
        return CancelUser
    case errors.Is(err, context.DeadlineExceeded):
        return CancelTimeout
    case errors.Is(err, context.Canceled):
        // 父取消 vs 网络取消 还要细查
        return context.Cause(parentCtx) == context.Canceled ? CancelParent : CancelNetwork
    default:
        return CancelNone
    }
}
```

调用点：
- `coordinator.go:1515`：把 `err` 包成 `&ToolError{Kind: ToolErrorCancelled, Reason: ClassifyCancel(err), Retryable: false}` 而非裸 string
- `noninteractive` 模式：根据 `Reason` 决定是 return nil 还是 output stderr

### 3. 工具层重试对网络错误放行

`internal/core/agent/toolutil/retry.go:48`：

```go
func DefaultShouldRetry(err error) bool {
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return false  // 仍然不算可重试
    }
    if isNetworkError(err) {
        return true
    }
    return false
}
```

注意：**只重试网络类错误**，不重试业务类 4xx（404、429 视情况）。

### 4. Anthropic SDK 的 timeout 配置

`internal/core/agent/agentic_fetch_tool.go` 构造 providers 时，注入：

```go
options := []option.RequestOption{
    option.WithRequestTimeout(120 * time.Second),
    option.WithMaxRetries(2),
}
```

这样 Provider 的 RequestTimeout 不再依赖父 ctx 的隐式 deadline。

### 5. UI 层友好提示

`internal/ui/chat/agent.go`：当 ToolResult 带 `CanceledReason: CancelNetwork` 时，渲染：
```
🛟 子 Agent 因网络超时被取消（可能是端点不可用）
   └ 自动降级到 DuckDuckGo 继续搜索
```

而不是一句干瘪的 `context canceled`。

---

## 📋 改造顺序

1. **M1 内部 PR**：仅 `agent_lifecycle.go` 改 `genCtxWithBudget`，加单元测试
   - 风险：低
   - 测试：`agent_lifecycle_test.go` 新增 `TestGenCtxWithBudget_*`
2. **M2 内部 PR**：新增 `cancellation.go` + `ClassifyCancel`，Coordinator 包错
   - 风险：中（影响所有 sub-agent 调用）
   - 测试：扩 `coordinator_test.go`，覆盖每种 Reason
3. **M3 内部 PR**：retry 放行网络错误
   - 风险：中
   - 测试：扩 `retry_test.go`
4. **M4 内部 PR**：UI 渲染 CanceledReason
   - 风险：低（纯 UI 表现）
5. **M5 内部 PR**：Anthropic SDK `WithRequestTimeout` + `WithMaxRetries`
   - 风险：低（只对 Provider 生效）

每一步独立可回滚。

---

## ⚖️ 不在范围内

- 替换 Anthropic SDK Timeout 机制（如改用 stream ping 协议）— 太底层
- 让 Provider 自己 retry — Provider SDK 已经在 retry，M3 补的是工具层重试
- 引入 Context-Sentinel 进程级 cancel bus — 留给更大的"全 agent context observability"工作

---

## ✅ 验收

- [ ] 端点不可用（如 GitHub 404 + DuckDuckGo 慢），子 Agent 不再因父 ctx cancel 被打断
- [ ] 用户 Esc 仍然能立刻打断子 Agent（在合理 budget 内最多 1s）
- [ ] ToolResult 包含 `CanceledReason`，UI 能渲染不同提示
- [ ] 网络错误自动重试 N 次（默认 2）
- [ ] 所有现有 `agent` 包测试不回归
