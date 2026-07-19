# agentic_fetch 降级策略设计

> 记录时间：2026-07-20  
> 状态：草案（待与 agent/transport team 对齐）

---

## 🎯 目标

让 `agentic_fetch` 在端点不可用时**仍有可用响应**，而不是整条链路硬失败 `context canceled`。

具体来说：
- URL 模式：404/5xx/超时 → 返回"无法访问" + 给出线索（是否网络层/服务层/已下架）
- 搜索模式：Provider 全挂 → 兜底返回空结果 + 清楚的错误描述

---

## 📐 总体策略：四层降级

```
L1: 命中缓存/快照          → 直接返回 0 网络调用
        │ miss
        ▼
L2: 主 Provider 链         → web_search 主链路
        │ 全失败
        ▼
L3: 兜底 Provider (DuckDuckGo / Instant Answer)
        │ 全失败
        ▼
L4: 软错误返回 ToolResponse → 不再爆炸 context.Canceled
```

要点：
- L1-L3 任一层成功就返回结果，不要继续 fallback
- L4 是"工程保底"——确保工具永不抛 context.Canceled
- 每次降级记录 `tool_metadata`：`{tried: [...], source: "duckduckgo_fallback", elapsed_ms: 1234}`
- 上层 Coordinator / SessionAgent 可以基于 metadata 决定是否继续分析

---

## 🔧 落地步骤

### 阶段 1：工具内部降级

修改 `internal/core/agent/agentic_fetch_tool.go`：

1. URL 模式（`agentic_fetch_tool.go:105`）
   - 当前：`return fantasy.NewTextErrorResponse("Failed to fetch URL: ...")`
   - 改造：把 `*http.Response` 也带到 output，error 时附加 `url_status: 404`、`url: "..."`，便于模型理解
2. 搜索模式（`agentic_fetch_tool.go:117`）
   - 改造：不再 fallback 走 `web_search` 工具链，而是调用 `netcommon.Provider` 接口的 `DefaultSearchProvider()` 直接做搜索
   - 网络失败时使用 `MultiProvider` 的 fallback 链

### 阶段 2：子 Agent context 隔离

见 `03-context-isolation-plan.md`：
- 子 Agent 不继承父 deadline
- 子 Agent 自己的 timeout（60-120s）
- 子 Agent 错误不直接报 `context.Canceled`，而是转换为 ToolResponse 带显式 success=false

### 阶段 3：Provider 链可注入

`netcommon/provider.go:198` 的 `DefaultSearchProvider` 当前已经是 `MultiProvider{DuckDuckGoProvider{}, DuckDuckGoInstantAnswerProvider{}}`。

进一步：
- 暴露 `WithProviderChain(custom ...Provider)` 选项给工具构造
- 在 `mcp/init.go`、admin HTTP 后台支持运行时切换 Provider
- 配置文件 `mocode.json` 增加 `web_search.providers` 字段

### 阶段 4：可观测性

每次失败/降级通过 `app.ErrorCollectorService()` 写入：
```
{
  "tool": "agentic_fetch",
  "mode": "url|search",
  "url": "...",                  // URL 模式
  "query": "...",                // 搜索模式
  "status": 404,                 // URL 模式
  "provider": "duckduckgo",      // 搜索模式
  "elapsed_ms": 12345,
  "context_canceled": true,
  "stack": "..."
}
```

UI 层在 `internal/ui/chat/agent.go` 把这些事件渲染成"🛟 网关已降级到 X"的明显提示，用户立刻知道搜索走了降级路径。

---

## 🚦 预期行为对比

### 现在（硬失败）

```
agentic_fetch("https://github.com/z-ai-community/z-ai-mcp-server")
→ Agent: "Failed to fetch URL: request failed with status code: 404"
agentic_fetch("Find Z.ai MCP config")
→ Agent 重试 search 模式
→ Stream 调用被 RequestTimeout cancel
→ ToolResult: "error generating response: context canceled"
→ 整个 ToolCall 标红失败，session 状态被打断
```

### 改造后（软降级）

```
agentic_fetch("https://github.com/z-ai-community/z-ai-mcp-server")
→ Agent: "Failed to fetch URL: 404\n\nThis repo may be private, renamed,
          or moved. Try checking the org profile at /z-ai-community"
agentic_fetch("Find Z.ai MCP config")
→ Provider 主链失败
→ 兜底 DuckDuckGo 仍可用 → 拿到一些结果
→ ToolResult 包含 source: "duckduckgo_fallback" + 实际搜索结果
```

最坏情况：
```
agentic_fetch("Find Z.ai MCP config")
→ Provider 全挂
→ ToolResult: success=false, text="All search providers (DuckDuckGo,
          Instant Answer) timed out. The user's network may be blocking
          these services."
→ 用户看到清晰的提示，可选择手动配置或换网络
→ 不会因为 ctx.Canceled 把 session 流程打断
```

---

## ⚖️ 不在范围内

- 引入第三方收费搜索 API（Bing/Google CSE）— 留给后续 PR
- 自建 elasticsearch/meilisearch 索引 — 留给后续 PR
- 端点 health check 主动缓存 — 留给 L2 改造

---

## 📋 验收标准

- [ ] L1 缓存：相同 URL 5 分钟内复用（待 L1 实现）
- [ ] L2 主链：30s 内返回，否则切 L3
- [ ] L3 兜底：5s 内返回（少量结果也行）
- [ ] L4 兜底：永不抛 context.Canceled
- [ ] 可观测性：错误/降级全部入 ErrorCollector
