# 子计划 11 — Z.AI MCP Integration

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：06-provider-chain-skeleton / 08-exa-public-mcp（MCPClient 已实现）
> **后续被依赖**：无

---

## 目标

实现 Z.AI Provider：完整 MCP JSON-RPC 协议 + 三级参数重试 + SSE/JSON 双形态响应解析。

## 范围

### 必须实现

1. **MCP 三步握手**
   ```go
   // Step 1: initialize
   sendRequest(client, "initialize", map[string]any{
       "jsonrpc": "2.0",
       "id":      uuid.NewString(),
       "method":  "initialize",
       "params": map[string]any{
           "protocolVersion": "2025-03-26",
           "clientInfo":      map[string]any{"name": "mocode", "version": "1.0.0"},
       },
   })
   // Step 2: notifications/initialized (expectResponse=false)
   sendNotification(client, "notifications/initialized", nil)
   // Step 3: tools/call
   sendRequest(client, "tools/call", map[string]any{
       "name": "web_search_prime",
       "arguments": attempts[0],
   })
   ```

2. **三级参数重试**
   ```go
   var attempts = []map[string]any{
       {"query": q, "count": n},
       {"search_query": q, "count": n},
       {"search_query": q, "search_engine": "search-prime", "count": n},
   }
   for _, args := range attempts {
       resp, err := callTool(client, "web_search_prime", args)
       if !isArgumentError(err) {
           return resp, err
       }
   }
   ```

3. **SSE / JSON 双形态响应解析**
   - SSE: `data:` 行解析（`r3labs/sse/v2` 或自实现 `bufio.Scanner`）
   - 裸 JSON：检测 `Content-Type: application/json`
   - "取最后一条消息"：在 SSE 多 message 场景下保留最后一个

4. **多层 result 提取**
   - `structuredContent` → `data` → `result` → `content[].text` → `root`
   - content[].text 双重 JSON.parse（处理 `text` 字段本身是 JSON 字符串）
   - 字段名兼容：`search_result` / `results`

5. **answer 拼接**
   - 取所有 text parts 中"无法解析为 results"的拼接
   - 用 `\n\n` 分隔

6. **sessionId 管理**
   - `sync.Map[string, string]` key=endpoint, value=sessionId
   - 每次响应更新（`Mcp-Session-Id` header）
   - 跨请求回传（`Mcp-Session-Id: {value}` header）

7. **错误分类**
   - JSON-RPC `error.code/message` → `SearchProviderError`
   - `result.isError === true` → 从 `result.content[].text` 提取 message
   - 正则 `MCP error (-?\d+)` 提取 status code
   - HTTP 非 2xx → `classifyHTTPError`

## 文件清单

```
pkg/search/providers/zai/
├── zai.go                  # ZAIProvider 主体
├── handshake.go            # MCP 三步握手
├── retry.go                # 三级参数重试
├── response.go             # SSE/JSON 解析 + 5 层 fallback
├── session.go              # sessionId 管理
├── errors.go               # MCP error 提取
└── tests/
    ├── zai_test.go
    └── retry_test.go
```

## 验收标准

- [ ] 三步握手完整实现
- [ ] 三级参数重试：400 + invalid argument → 自动 fallback
- [ ] sessionId 跨请求维持
- [ ] 5 层 result 提取 fallback
- [ ] 双重 JSON.parse 场景测试
- [ ] 真实调用 Z.AI 1 次成功（用 `ZAI_API_KEY`）

## 工期估

2 个工作日（复用 03 子计划的 MCPClient，仅需补协议层）

## 风险

- **三级重试够用吗**：若 Z.AI 升级 schema（如新增 `recency_filter`），需扩展 attempts
- **服务端 SSE 多 message 进度反馈**：当前 Z.AI 单 message 单 result，若升级流式输出需改造为 progress callback
- **`withAuth` 包装层**：mocode 是否有对等抽象？需看现有认证体系

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：在 `08-exa-mcp` 的 MCPClient 基础上，叠加 Z.AI 特定协议（三级重试 + 5 层 payload fallback）
> 2. **至底向上搞实现**：`session.go`（sessionId 维护）→ `handshake.go`（三步握手）→ `response.go`（SSE/JSON 解析 + 5 层 fallback）→ `retry.go`（三级参数重试）→ `errors.go`（MCP error 提取）→ `zai.go`（Provider 主类）
> 3. **向上发展作拼装**：复用 `pkg/mcpclient/` 通用层，Z.AI 特有逻辑通过 embed + override 拼装
> 4. **逐层验证和仿真**：sessionId 跨请求维持测试 → 三步握手 mock server → 三级重试场景测试 → 5 层 fallback 单测 → 真实调用 Z.AI 1 次
> 5. **铁律约束 LLM 超边界**：只允许创建 `pkg/search/providers/zai/` 目录；禁止修改 `pkg/mcpclient/`（已在 08 中实现）

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（协议实现 + 多层 fallback 复杂）
- **辅助模型**：Haiku（生成 Z.AI mock server fixture）

## 相关笔记

- [[zai-mcp-deep-dive]] — 完整 10 维度分析
- [[08-exa-public-mcp]] — MCPClient 复用
- [[MASTER_PLAN]]