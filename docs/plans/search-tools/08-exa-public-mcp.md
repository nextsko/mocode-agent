# 子计划 08 — Exa Public MCP Provider

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：06-provider-chain-skeleton / 07-credential-broker
> **后续被依赖**：09 / 10 / 11（MCP 复用）

---

## 目标

实现 Exa 双路径 provider：
- **A 路径（REST）**：有 API key 时走 `https://api.exa.ai/search`
- **B 路径（Public MCP）**：无 key 时走 `https://mcp.exa.ai/mcp?exaApiKey=`（JSON-RPC over SSE）

这是 mocode 第一个免费 provider，也是 MCP JSON-RPC 抽象层的第一个落地。

## 范围

### 必须实现

1. **Exa REST path**
   - Endpoint: `POST https://api.exa.ai/search`
   - Header: `x-api-key: {key}`
   - Body: `{query, numResults, type, contents: {summary: {query}}, includeDomains?, excludeDomains?, startPublishedDate?, endPublishedDate?}`
   - 响应：解析 `results[]` → `[]SearchSource`
   - `synthesizeAnswer`：取前 3 个 non-empty `summary` 拼接

2. **Exa Public MCP path（核心）**
   - Endpoint: `POST https://mcp.exa.ai/mcp?exaApiKey={key}&tools=web_search_exa`
   - 即使 `exaApiKey=` 为空也允许调用
   - JSON-RPC 2.0 over SSE：
     - `initialize` → 取 `Mcp-Session-Id`
     - `notifications/initialized`
     - `tools/call` name=`web_search_exa`
   - inline operators: `site:`/`-site:`/`after:`/`before:` 作为 query 一部分传递

3. **MCP Client 复用**
   - `pkg/mcpclient/` 通用 JSON-RPC over SSE 客户端
   - sessionId 维护（`sync.Map` 或 context value）
   - 5 层 payload fallback（`structuredContent / data / result / content[].text / root`）

4. **限流**
   - `waitForExaSearchSlot` FIFO promise queue
   - 配置项 `exa.searchDelayMs`（默认 100ms）

5. **错误处理**
   - MCP 429 → "configure an Exa API key for higher limits"
   - REST 429 → 通用 classify
   - MCP `isError: true` → 从 `result.content[].text` 提取

## 文件清单

```
pkg/search/providers/
├── exa/
│   ├── exa.go              # ExaProvider 主体
│   ├── exa_rest.go         # REST path
│   ├── exa_mcp.go          # Public MCP path
│   ├── throttle.go         # waitForExaSearchSlot
│   └── tests/
│       └── exa_test.go
pkg/mcpclient/
├── client.go               # MCPClient 主类
├── session.go              # sessionId 管理
├── sse.go                  # SSE 解析
├── jsonrpc.go              # JSON-RPC 2.0 编解码
├── types.go                # InitializeResult / ToolCallResult 等
└── tests/
    └── client_test.go
```

## 验收标准

- [ ] `ExaProvider.IsAvailable`：有 key → true；无 key → false
- [ ] `ExaProvider.IsExplicitlyAvailable`：任何时候 true（允许 keyless fallback）
- [ ] REST path 真实调用通过（用 EXA_API_KEY 测 1 次）
- [ ] MCP path 真实调用通过（无 key 测 1 次）
- [ ] sessionId 跨请求维持
- [ ] 5 层 payload fallback 单测

## 工期估

1.5 个工作日（含 MCPClient 通用层 0.5 天）

## 风险

- **Exa Public MCP 稳定性**：5 层 fallback 暗示服务端多形态切换，建议加监控告警
- **rate limit 限制**：keyless 形态 429 频率较高，需做好提示用户配置 key

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：先设计 MCPClient 通用接口（`pkg/mcpclient/`），再设计 Exa 双路径适配
> 2. **至底向上搞实现**：先实现 `mcpclient/sse.go`（SSE 流解析）→ `mcpclient/jsonrpc.go`（JSON-RPC 编解码）→ `mcpclient/session.go`（sessionId）→ `mcpclient/client.go`（主类）→ `providers/exa/exa_mcp.go`（Exa MCP 适配）→ `providers/exa/exa_rest.go`（REST 适配）→ `providers/exa/exa.go`（Provider 主类）
> 3. **向上发展作拼装**：`ExaProvider` 通过组合 `MCPClient` + `HTTPClient` 两个底层 client 拼装双路径
> 4. **逐层验证和仿真**：SSE parser 单测 → JSON-RPC 编解码单测 → MCPClient mock server 测试 → Exa REST mock 测试 → Exa MCP 真实调用
> 5. **铁律约束 LLM 超边界**：只允许创建 `pkg/mcpclient/` 与 `pkg/search/providers/exa/` 两目录下的文件

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（MCP 协议实现复杂，含 sessionId 维护与 5 层 payload fallback）
- **辅助模型**：Haiku（生成 Exa mock server fixture）

## 相关笔记

- [[free-providers-research]] §3 Exa 双路径
- [[provider-chain-design]] §11 Go 骨架
- [[MASTER_PLAN]]