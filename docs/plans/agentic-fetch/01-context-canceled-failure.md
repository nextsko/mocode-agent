# agentic_fetch 工具：端点不可用时的 context canceled 根因分析

> 记录时间：2026-07-20  
> 背景：在会话中调用 `agentic_fetch` 查找 Z.ai MCP server 配置，先碰到 404，再发起子 Agent 重新搜索时直接 `context canceled`，整个工具调用硬失败

---

## 🐛 现象

两次调用 `agentic_fetch` 均失败：

1. **URL 模式**：请求 `https://github.com/z-ai-community/z-ai-mcp-server` 返回 `404 Not Found`
2. **搜索模式**：改用 web_search 重新尝试，子 Agent 整个流返回 `error generating response: context canceled`

第二类是真正的硬失败——既没有结果也没有降级。后续任何 `agentic_fetch` 工具调用只要走"搜索 + sub-agent 分析" 模式，端点慢或 404 都会爆掉同一个错误。

---

## 🔬 根因链路

### 1. agentic_fetch 搜索模式会启动子 Agent

`internal/core/agent/agentic_fetch_tool.go:88`
```go
return c.runSubAgent(ctx, subAgentParams{...})
```

### 2. 子 Agent 继承了父工具的 context

`internal/core/agent/coordinator.go:1401` → `internal/core/agent/agent_lifecycle.go:115`
```go
genCtx, cancel := context.WithCancel(ctx)  // 直接继承父 ctx
```

### 3. 子 Agent 链路串行执行，慢/不可用端点会拉长总耗时

子 Agent 会依次：
- `web_search` → 慢时可能撞上 SearchProvider 的限频等待
- `web_fetch` → 每个搜索结果都跑 `tools/plugins/netcommon/httpclient.go` 30 秒 HTTP 超时
- LLM 分析 → 调用 stream，需要网络

端点不可用时，每一项都接近 30s 截止，多次轮询很容易超过某个隐含时间上限。

### 4. Anthropic SDK 的 RequestTimeout 会主动 cancel context

`charmbracelet/anthropic-sdk-go/requestconfig.go:431`
```go
if cfg.RequestTimeout != time.Duration(0) && isBeforeContextDeadline(time.Now().Add(cfg.RequestTimeout), ctx) {
    ctx, cancel = context.WithTimeout(ctx, cfg.RequestTimeout)
}
```

更隐蔽的是 `bodyWithTimeout.Close()`：`responseconfig.go:336-359`
```go
type bodyWithTimeout struct {
    stop func()  // timer 到期会主动 cancel
    rc   io.ReadCloser
}

func (b *bodyWithTimeout) Close() error {
    err := b.rc.Close()
    b.stop()  // body 关闭时也会触发 cancel
    return err
}
```

即使 stream 正常完成，response body 关闭也会让 `ctx.Err()` 变成 `context.Canceled`。

### 5. Fantasy retry 明确不重试 context 取消

`internal/core/agent/toolutil/retry.go:48`
```go
func DefaultShouldRetry(err error) bool {
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        return false  // 直接放弃
    }
    ...
}
```

### 6. 最终错误

`internal/core/agent/coordinator.go:1515`
```go
return fantasy.NewTextErrorResponse(fmt.Sprintf("error generating response: %s", err)), ...
```

把 `context.Canceled` 错误原样塞到 ToolResult 中，没区分"用户取消"vs"超时"vs"网络断开"。

---

## ⚠️ 为什么端点不可用时特别频繁

| 环节 | 正常情况 | 端点不可用 |
|---|---|---|
| `web_fetch` 单请求 | 几百 ms | 30 s 超时 |
| `web_search` 单次 | 1-3 s | 可能空结果或超时 |
| 子 Agent 总耗时 | 5-15 s | 30 s × N + LLM 时间 |
| context 被 cancel 概率 | 低 | 极高 |

端点不可用时，所有 HTTP 请求都 hang 到 30s 才返回错误。子 Agent 为了"更广泛地搜索"会做多轮 search + fetch，每一轮都在消耗父 context 的寿命。只要任意一环触发 timeout/cancel，整条链路就以 `context.Canceled` 硬失败，且**没有重试、没有降级**。

---

## 🩺 已有的旁路

`docs/guides/04-gfw-workarounds.md` 记录过的现象：

| 工具 | 失败原因 |
|---|---|
| `fetch` | 直连被 GFW 阻塞，`context deadline exceeded` |
| `agentic_fetch` | 同上 |
| `download` | 同上 |
| `mcp_gitea_*` | MCP 服务未注册 |

可见不只是 `agentic_fetch`，所有走 `http.DefaultClient` 直连的工具在 GFW 下都会超时。临时绕过方案是写 Go 临时程序显式 `http.Transport.Proxy` 指向本地代理。

但这只是**绕过工具**，没有解决工具本身在端点不可用时的可靠降级。

---

## 🎯 改造方向（详见 02/03）

1. **区分软取消 vs 硬取消**（02）
   - 用户按 Esc → `context.Canceled`，正确
   - Provider RequestTimeout → 包装为可重试错误
   - body close 触发 → 包装为正常的"已完成"信号

2. **子 Agent context 隔离**（03）
   - 子 Agent 不应继承父 Agent 的 deadline
   - 子 Agent 自己设置独立的合理 timeout（如 60-120s）

3. **降级链路**（02）
   - 搜索模式 → Fallback: web_search → DuckDuckGo → 返回空 + 提示用户
   - 而不是整条调用失败

---

## 🔖 相关源码位置

| 文件 | 行 | 角色 |
|---|---|---|
| `internal/core/agent/agentic_fetch_tool.go` | 105, 117 | 工具入口，区分 URL 模式 vs 搜索模式 |
| `internal/core/agent/coordinator.go` | 1401-1430, 1515 | `runSubAgentWithMeta` 把 err 当 ToolResponse 返回 |
| `internal/core/agent/agent_lifecycle.go` | 115 | `context.WithCancel(ctx)` 继承父 ctx |
| `tools/plugins/netcommon/httpclient.go` | 8 | `DefaultHTTPTimeout = 30s` |
| `internal/core/agent/toolutil/retry.go` | 48 | `DefaultShouldRetry` 拒绝 context 取消 |
| `internal/core/agent/templates/agentic_fetch_prompt.md.tpl` | - | 子 Agent 的系统提示 |
| `tools/plugins/netcommon/fetch_helpers.go` | 25-47 | `FetchURLAndConvert`，URL 模式异常路径 |
