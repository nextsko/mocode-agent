---
title: Z.AI MCP JSON-RPC Provider 深度调研
project: mocode-agent
tags: [mcp, json-rpc, zai, web-search, port-guide]
status: reference
created: 2026-09-14
updated: 2026-09-14
relatedPlans: []
---

# Z.AI MCP JSON-RPC Provider 深度调研

> [!abstract] 概述
> 本报告对 oh-my-pi 仓库 `packages/coding-agent/src/web/search/providers/zai.ts` 的 Z.AI `webSearchPrime` MCP 客户端实现做逐行拆解，按 10 个维度组织。末尾给出 Go 端可吸收的 MCP JSON-RPC over Streamable HTTP 客户端最小骨架与依赖清单，供 mocode 的 search-tools 模块直接复用。

源文件：`tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/zai.ts`（约 360 行）
辅助文件：`tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/utils.ts`（含 `withHardTimeout` 与 `classifyProviderHttpError`）
测试基线：`tmp/oh-my-pi/packages/coding-agent/test/web/search/zai.test.ts`

## 1. 完整 MCP 协议握手

### 1.1 入口常量

```ts
const ZAI_MCP_URL = "https://api.z.ai/api/mcp/web_search_prime/mcp";
const ZAI_TOOL_NAME = "web_search_prime";
const ZAI_MCP_PROTOCOL_VERSION = "2025-03-26";
const ZAI_MCP_CLIENT_INFO = { name: "omp-coding-agent", version: "1.0.0" };
```

- 协议版本固定为 `2025-03-26`（MCP Streamable HTTP 当前主流版本）。
- 客户端标识写死为 `omp-coding-agent/1.0.0`，Go 移植时建议改为 `mocode/<version>`。

### 1.2 `initialize` 请求体

`callZaiTool` 第一步：

```ts
await postZaiMcp(apiKey, "initialize", {
    protocolVersion: ZAI_MCP_PROTOCOL_VERSION,
    capabilities: {},
    clientInfo: ZAI_MCP_CLIENT_INFO,
}, undefined, signal, fetchImpl, true, timeoutMs);
```

- `expectResponse=true` → `postZaiMcp` 会注入 `id: crypto.randomUUID()`。
- 入参的 `sessionId` 为 `undefined`（首次握手无 session）。
- 返回体通过 `readJsonRpcPayload` 校验，但 `result` 内容被忽略，仅作为「服务端 OK」的探针。

### 1.3 `notifications/initialized` 的 `expectResponse=false` 处理

```ts
await postZaiMcp(apiKey, "notifications/initialized", {}, initialized.sessionId, signal, fetchImpl, false, timeoutMs);
```

- `expectResponse=false` → **不**在 body 中加 `id`（JSON-RPC notification 规范）。
- `postZaiMcp` 对此分支直接 `await response.body?.cancel()` 并返回 `{ sessionId }`，不解析 payload。
- 注意：服务端响应是 `202 Accepted`（见测试 mock），任何 2xx 都视为成功。

### 1.4 `tools/call` 调用 `web_search_prime`

```ts
await postZaiMcp(apiKey, "tools/call", {
    name: ZAI_TOOL_NAME,
    arguments: args,   // 见 §2 重试策略
}, initialized.sessionId, signal, fetchImpl, true, timeoutMs);
```

- 第三步起携带 `sessionId`。
- 服务端返回 `result.content[]`（标准 MCP tool result schema）。

### 1.5 `Mcp-Session-Id` Header 的客户端状态维护

```ts
const nextSessionId = response.headers.get("Mcp-Session-Id") ?? sessionId;
```

- 关键点：**优先用响应 header 中的新 sessionId**，仅在 header 缺失时回退到入参；保证服务端能轮换/刷新会话。
- `initialized.sessionId` 在后续两次请求中作为 `Mcp-Session-Id` header 回传。
- 注意：sessionId 不持久化到磁盘——它是单次 search 调用生命周期内的临时状态。

## 2. 参数多形态重试机制（核心）

### 2.1 三组 attempts

```ts
const attempts: Record<string, unknown>[] = [
    { query: params.query, count },
    { search_query: params.query, count },
    { search_query: params.query, search_engine: "search-prime", count },
];
```

按顺序：

1. `{ query, count }` — 旧字段名（兼容历史 schema）。
2. `{ search_query, count }` — 中间字段名。
3. `{ search_query, search_engine: "search-prime", count }` — 当前官方 schema（明确指定引擎）。

### 2.2 重试条件判定

```ts
if (!(error instanceof SearchProviderError)) throw error;
const message = error.message.toLowerCase();
const looksLikeArgError =
    error.status === 400 ||
    message.includes("invalid") ||
    message.includes("argument") ||
    message.includes("search_query") ||
    message.includes("query");
if (!looksLikeArgError) throw error;
```

**触发重试的条件**（任一满足）：

- HTTP `400` 状态码（直传 `SearchProviderError.status`）
- 错误消息（小写）含 `invalid`
- 含 `argument`
- 含 `search_query`
- 含 `query`

**不重试的场景**：

- `SearchProviderError` 以外的异常（网络层异常等）
- 401 / 402 / 403 / 429（已在 `classifyProviderHttpError` 中转译，状态码非 400）
- 任何不含上述关键字的错误消息

> [!tip] 设计意图
> Z.AI 后端在不同时期支持不同字段名；客户端不知道哪个版本对应哪个 schema，所以用 **「失败即降级」** 的方式依次尝试，401/429 等真错误立即抛出而不浪费 1–2 次重试预算。

## 3. SSE/JSON 双形态响应解析

`parseZaiMcpResponse` 是关键函数：

```ts
function parseZaiMcpResponse(rawText: string): unknown {
    const parsedMessages: unknown[] = [];
    for (const line of rawText.split("\n")) {
        const trimmed = line.trim();
        if (!trimmed.startsWith("data:")) continue;
        const data = trimmed.slice(5).trim();
        if (!data) continue;
        try {
            parsedMessages.push(JSON.parse(data));
        } catch {
            // ignore non-JSON data events
        }
    }
    if (parsedMessages.length === 0) {
        try {
            parsedMessages.push(JSON.parse(rawText));
        } catch {
            throw new SearchProviderError("zai", "Failed to parse Z.AI MCP response", 500);
        }
    }
    return parsedMessages[parsedMessages.length - 1];
}
```

### 3.1 SSE 处理流程

1. 按 `\n` 分行，逐行 `trim`。
2. 跳过不以 `data:` 开头的行（event:、id:、空行、注释行）。
3. 去掉 `data:` 前缀后再 `trim`（去前导空格）。
4. 空 data 跳过。
5. 尝试 `JSON.parse`，失败则静默忽略（避免 heartbeat、注释行污染）。
6. **取最后一条消息**（`parsedMessages[length-1]`）。

### 3.2 裸 JSON 识别

当没有任何 SSE `data:` 行被识别时（例如服务端直接返回 `application/json`），回退：

```ts
parsedMessages.push(JSON.parse(rawText));
```

失败则抛 `SearchProviderError("zai", "Failed to parse Z.AI MCP response", 500)`。

### 3.3 「取最后一条」的语义原因

- MCP Streamable HTTP 在单次 POST 响应中可能流式发送多条 JSON-RPC message（progress notification、partial result、最终 result）。
- 客户端关心最终 result；progress/partial 通常对调用方无意义。
- 注意：当前实现 **只保留最后一条**，丢弃中间的 progress——若有需要做进度回调，应改造为返回 `parsedMessages[]` 而非单值。

## 4. 多层嵌套 result 提取

### 4.1 `parseSearchPayload` 的候选递归搜索

```ts
const candidates: unknown[] = [rawResult];
if (isRecord(rawResult)) {
    if (rawResult.structuredContent) candidates.push(rawResult.structuredContent);
    if (rawResult.data)            candidates.push(rawResult.data);
    if (rawResult.result)          candidates.push(rawResult.result);
    ...
}
```

**递归搜索的顶层 key**（按尝试顺序）：

| 优先级 | key | 含义 |
|---|---|---|
| 1 | `rawResult` 本身 | MCP `result` 直传对象 |
| 2 | `structuredContent` | MCP 2025-06-18+ structured output |
| 3 | `data` | 部分后端用此包裹 |
| 4 | `result` | 套娃嵌套（Z.AI 偶尔出现） |

### 4.2 Content blocks 中 `text` 字段的 JSON.parse 双重编码

```ts
const content = rawResult.content;
if (Array.isArray(content)) {
    for (const part of content) {
        const text = isRecord(part) ? asString(part.text) : null;
        if (!text) continue;
        try {
            let parsed: unknown = JSON.parse(text);
            if (typeof parsed === "string") {
                try { parsed = JSON.parse(parsed); } catch { /* answer text */ }
            }
            candidates.push(parsed);
            if (getSearchResults(parsed).length === 0) textParts.push(text);
        } catch {
            textParts.push(text);
        }
    }
}
```

**关键点**：

- MCP `content[]` 块的 `text` 字段是「字符串形式承载 JSON」的标准做法。
- Z.AI 进一步把 JSON **再序列化为字符串**（双重编码），所以要二次 `JSON.parse`。
- **降级策略**：二次 parse 失败时，把原文作为 `textParts` 推入（避免丢失数据）。
- 若某 text parse 后**能**解析出 results → 推 `candidates`；**不能** → 推 `textParts`（最终拼成 `answer`）。

### 4.3 字段名兼容：`search_result` vs `results`

```ts
function getSearchResults(value: unknown): ZaiSearchResult[] {
    if (Array.isArray(value)) return value as ZaiSearchResult[];
    if (!isRecord(value)) return [];
    if (Array.isArray(value.search_result)) return value.search_result;
    if (Array.isArray(value.results))       return value.results;
    return [];
}
```

**判定优先级**：

1. 直接是数组（裸数组响应）。
2. 含 `search_result` 数组字段（Z.AI 主字段名）。
3. 含 `results` 数组字段（OpenAI/Anthropic 风格兼容）。
4. 都不是 → 返回 `[]`。

## 5. answer 字段组装

### 5.1 哪些 text parts 拼接成 answer

- 来自 `parseSearchPayload` 的 `textParts: string[]`。
- 组成：`parseSearchPayload` 遍历 `rawResult.content[]`，**对每个 text part**：
  - 若能 `JSON.parse` 且解析出 results → 不进 answer。
  - 否则进 answer。

### 5.2 为什么「拼接」而非「取第一条」

- Z.AI 的 `webSearchPrime` 经常返回「results + 解释性 prose 段落」的多 part content。
- 每段 prose 可能包含不同角度的总结，只取第一条会丢失信息。
- 拼接分隔符：`"\n\n"`（双换行），保证 markdown 渲染时分段清晰。

### 5.3 返回位置

最终拼好的字符串在 `searchZai` 中作为 `SearchResponse.answer` 返回：

```ts
return {
    provider: "zai",
    answer: payload.answer,    // undefined 当且仅当 textParts 为空
    sources,
    requestId: payload.requestId,
};
```

## 6. 错误分类

### 6.1 JSON-RPC `error.code/message` → `SearchProviderError`

```ts
if (payload.error) {
    const status = typeof payload.error.code === "number" ? payload.error.code : 400;
    throw new SearchProviderError(
        "zai",
        `Z.AI MCP error (${payload.error.code}): ${payload.error.message ?? "Unknown error"}`,
        status,
    );
}
```

- 把 JSON-RPC `error.code` 映射到 `SearchProviderError.status`（兜底 400）。

### 6.2 `result.isError === true` 时从 content[].text 提取错误

```ts
if (resultRecord?.isError === true) {
    const content = Array.isArray(resultRecord.content) ? resultRecord.content : [];
    const errorText = content
        .map(item => isRecord(item) ? asString(item.text) : null)
        .filter((text): text is string => text != null)
        .join("\n").trim();
    const statusMatch = errorText.match(/MCP error\s*(-?\d+)/i);
    const statusCode = statusMatch ? Math.abs(Number.parseInt(statusMatch[1], 10)) : 400;
    throw new SearchProviderError("zai", errorText || "Z.AI MCP tool call failed", statusCode);
}
```

- MCP 协议规定 tool 调用失败时仍返回 200 + `result.isError: true` + 含错误信息的 content。
- 正则 `/MCP error\s*(-?\d+)/i` 从错误文本里抽 status code（Z.AI 用此格式回传）。
- 抽不到时兜底 400（与重试条件保持一致）。

### 6.3 HTTP 非 2xx → `classifyProviderHttpError` 接入

```ts
if (!response.ok) {
    const errorText = await response.text();
    const classified = classifyProviderHttpError("zai", response.status, errorText);
    if (classified) throw classified;
    throw new SearchProviderError("zai", `Z.AI MCP error (${response.status}): ${errorText}`, response.status);
}
```

`classifyProviderHttpError` 规则（utils.ts）：

| 触发条件 | 输出消息 | 状态码 |
|---|---|---|
| body 含 `credits? exhausted/exceeded`、`quota`、`insufficient`（正则） | `${provider}: credits exhausted` | response.status |
| HTTP 402 | `${provider}: 402 credits exhausted` | 402 |
| HTTP 401 | `${provider}: 401 unauthorized` | 401 |
| HTTP 403 | `${provider}: 403 forbidden` | 403 |
| 都不匹配 | （fallback）`Z.AI MCP error (${status}): ${errorText}` | response.status |

> [!warning] 401/403/402/429 都不会触发 §2 重试，因为：
> - `SearchProviderError.status` 不等于 400
> - 错误 message 不含 `invalid/argument/search_query/query`
> - 直接抛出不重试

## 7. sessionId 状态管理

### 7.1 维护位置

完全在 `searchZai` → `callZaiSearch` → `callZaiTool` 的调用栈中**以局部变量**形式存在：

```ts
const initialized = await postZaiMcp(...);    // 首次握手，返回 sessionId
await postZaiMcp(..., initialized.sessionId, ...);  // notification
const toolCall = await postZaiMcp(..., initialized.sessionId, ...);  // tool call
```

### 7.2 何时更新

```ts
const nextSessionId = response.headers.get("Mcp-Session-Id") ?? sessionId;
```

- 每次响应都**读**一次 header，存在则覆盖入参。
- initialize 之后从 initialize 响应取；之后两次请求都用 initialize 拿到的 id（因为后续响应通常不带新 header）。

### 7.3 跨请求回传 header 格式

```ts
headers["Mcp-Session-Id"] = sessionId;
```

- HTTP header 名严格为 `Mcp-Session-Id`（中划线连字符）。
- 若 `sessionId` 为 undefined 则**不**写入 header（首次 handshake）。

> [!tip] Go 移植建议
> 用 `sync.Map` 跨 goroutine 维护 session 池；或更简单：在 `ZaiClient` 结构体上挂 `mu sync.Mutex` + `sessionID string`，每次 `initialize` 后更新字段。**避免**用 context.Value 传可变状态。

## 8. 超时与取消

### 8.1 AbortSignal 接入点

```ts
const response = await fetchImpl(ZAI_MCP_URL, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
    signal: withHardTimeout(signal, timeoutMs),
});
```

- `signal` 是上层调用方传入的取消信号。
- `withHardTimeout` 把它和硬超时合并，确保 `fetch` 一定在 `timeoutMs` 之内完成。

### 8.2 与 `withHardTimeout` 的组合

`utils.ts` 实现：

```ts
export function withHardTimeout(signal: AbortSignal | undefined, ms: number = SEARCH_HARD_TIMEOUT_MS): AbortSignal {
    const timeout = AbortSignal.timeout(ms);
    return signal ? AbortSignal.any([signal, timeout]) : timeout;
}
```

- `AbortSignal.any([s1, s2])` 任何一个触发即取消。
- 默认 60s，可被 `timeoutMs` 覆盖（最大 300s，由 dispatcher 强制）。
- **为什么必须**：Bun WinHTTP 在 Windows 上对 stalled TCP/TLS 连接的 AbortSignal 不可靠（issue oven-sh/bun#15275、#18536），硬超时是兜底。
- 注：**不影响** Go——Go 的 `net/http.Client.Do` 自带 `context.Context` 取消，硬超时可用 `context.WithTimeout` 直接做。

## 9. 字段映射完整对照表

`toSources` 函数：

```ts
function toSources(results: ZaiSearchResult[]): SearchSource[] {
    for (const result of results) {
        const url = asString(result.link) ?? asString(result.url);
        if (!url) continue;
        const publishedDate = asString(result.publish_date) ?? asString(result.publishedDate);
        sources.push({
            title: asString(result.title) ?? url,
            url,
            snippet: asString(result.content) ?? undefined,
            publishedDate: publishedDate ?? undefined,
            ageSeconds: dateToAgeSeconds(publishedDate),
            author: asString(result.media) ?? undefined,
        });
    }
}
```

| Z.AI 字段（候选） | TS `SearchSource` 字段 | 处理逻辑 |
|---|---|---|
| `title` | `title` | 透传；缺失时 fallback 到 `url` |
| `content` | `snippet` | 透传 `result.content`（注意：与 `link/url` 字段混用） |
| `link` / `url` | `url` | 优先 `link`，回退 `url`；缺失则**跳过该条** |
| `publish_date` / `publishedDate` | `publishedDate` | 优先 `publish_date`，回退 `publishedDate` |
| （基于 `publishedDate`） | `ageSeconds` | `dateToAgeSeconds()` 计算 |
| `media` | `author` | 透传（语义上 Z.AI 用 `media` 表达来源媒体/作者） |
| （顶层）`request_id` / `requestId` / `id` | `requestId` | 同 §4 字段名兼容逻辑 |

> [!note] 注意
> `snippet` 没有字段名兼容（只读 `content`），但 Z.AI 实际响应里 snippet 字段名是 `content`——这点容易踩坑。

## 10. Go 移植难度评级

### 10.1 JSON-RPC 2.0 over SSE 客户端需要哪些库

| 需求 | 推荐 Go 库 | 备注 |
|---|---|---|
| HTTP 客户端 | `net/http` 标准库 | 已支持 `context.Context` 取消 |
| SSE 流解析 | `github.com/r3labs/sse/v2` 或手写 `bufio.Scanner` | 单条 message 解析很简单 |
| JSON-RPC envelope | 手写 struct（极简）：`{jsonrpc,id,method,params,result,error}` | 不需要重型库 |
| UUID v4（生成 `id`） | `github.com/google/uuid` | 与 `crypto.randomUUID()` 对齐 |
| MCP 协议版本常量 | 直接常量 `"2025-03-26"` | |

**难度评级**：⭐⭐（中等偏低）。Z.AI 实现用 Bun/TS 也就 360 行，Go 移植 1 个文件 ~400 行可搞定。

### 10.2 sessionId 状态如何用 sync.Map 或 context 传

**方案 A：`ZaiClient` 结构体持有 sessionId**（推荐，单调用）

```go
type ZaiClient struct {
    mu        sync.Mutex
    sessionID string
    apiKey    string
    httpClient *http.Client
}

func (c *ZaiClient) ensureSession(ctx context.Context) (string, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.sessionID != "" { return c.sessionID, nil }
    // ... POST initialize ...
    c.sessionID = nextSessionID
    return c.sessionID, nil
}
```

**方案 B：`sync.Map` 池**（适合多 session 场景）

```go
var sessionPool sync.Map  // key: apiKeyHash, value: *sessionEntry
type sessionEntry struct {
    id  string
    exp time.Time
}
```

**注意**：

- **不要**用 `context.Context` 存可变状态——context 应是不可变的；sessionId 中途更新会破坏 Go 的 context 语义。
- 若必须传 ctx，只能传 immutable snapshot（用 `sync.Once` + `atomic.Value`）。

### 10.3 三级参数重试的代码骨架示例

```go
type argShape map[string]any

func (c *ZaiClient) callWebSearchPrime(ctx context.Context, query string, count int) (json.RawMessage, error) {
    attempts := []argShape{
        {"query": query, "count": count},
        {"search_query": query, "count": count},
        {"search_query": query, "search_engine": "search-prime", "count": count},
    }

    var lastErr error
    for i, args := range attempts {
        result, err := c.callTool(ctx, "web_search_prime", args)
        if err == nil {
            return result, nil
        }
        lastErr = err

        // 最后一次直接抛
        if i == len(attempts)-1 {
            break
        }

        // 重试判定：必须是参数错误
        if !isArgError(err) {
            return nil, err
        }
    }
    return nil, lastErr
}

func isArgError(err error) bool {
    var spe *SearchProviderError
    if !errors.As(err, &spe) {
        return false
    }
    if spe.Status == 400 {
        return true
    }
    msg := strings.ToLower(spe.Message)
    return strings.Contains(msg, "invalid") ||
        strings.Contains(msg, "argument") ||
        strings.Contains(msg, "search_query") ||
        strings.Contains(msg, "query")
}
```

---

## 附录 A：Go MCP JSON-RPC 客户端完整代码骨架（最小可运行示例）

> [!example] 文件结构
> 建议放在 `mocode-agent/internal/search/zai/`，单文件实现：

```go
// Package zai implements the Z.AI webSearchPrime MCP client.
package zai

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "regexp"
    "strings"
    "sync"
    "time"

    "github.com/google/uuid"
)

const (
    ZaiMCPURL            = "https://api.z.ai/api/mcp/web_search_prime/mcp"
    ZaiToolName          = "web_search_prime"
    ZaiProtocolVersion   = "2025-03-26"
    ZaiClientInfoName    = "mocode"
    DefaultNumResults    = 10
    DefaultTimeoutSecs   = 60
    MaxTimeoutSecs       = 300
)

// ---------- JSON-RPC 2.0 envelope ----------

type jsonRPCRequest struct {
    JSONRPC string         `json:"jsonrpc"`
    ID      string         `json:"id,omitempty"`
    Method  string         `json:"method"`
    Params  map[string]any `json:"params,omitempty"`
}

type jsonRPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

type jsonRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      string          `json:"id,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *jsonRPCError   `json:"error,omitempty"`
}

// ---------- 错误类型 ----------

type SearchProviderError struct {
    Provider string
    Message  string
    Status   int
}

func (e *SearchProviderError) Error() string { return e.Message }

// ---------- 客户端 ----------

type Client struct {
    apiKey     string
    httpClient *http.Client
    mu         sync.Mutex
    sessionID  string
}

func NewClient(apiKey string, httpClient *http.Client) *Client {
    if httpClient == nil {
        httpClient = &http.Client{Timeout: 70 * time.Second}
    }
    return &Client{apiKey: apiKey, httpClient: httpClient}
}

// ---------- 公共 API ----------

func (c *Client) Search(ctx context.Context, query string, numResults int) (*SearchResponse, error) {
    if numResults <= 0 {
        numResults = DefaultNumResults
    }
    raw, err := c.callWebSearchPrime(ctx, query, numResults)
    if err != nil {
        return nil, err
    }
    return parseSearchPayload(raw)
}

// ---------- 三级重试 ----------

func (c *Client) callWebSearchPrime(ctx context.Context, query string, count int) (json.RawMessage, error) {
    attempts := []map[string]any{
        {"query": query, "count": count},
        {"search_query": query, "count": count},
        {"search_query": query, "search_engine": "search-prime", "count": count},
    }
    var lastErr error
    for i, args := range attempts {
        res, err := c.callToolWithRetry(ctx, "web_search_prime", args)
        if err == nil {
            return res, nil
        }
        lastErr = err
        if i == len(attempts)-1 {
            break
        }
        if !isArgError(err) {
            return nil, err
        }
    }
    return nil, lastErr
}

func isArgError(err error) bool {
    var spe *SearchProviderError
    if !errors.As(err, &spe) {
        return false
    }
    if spe.Status == 400 {
        return true
    }
    msg := strings.ToLower(spe.Message)
    for _, k := range []string{"invalid", "argument", "search_query", "query"} {
        if strings.Contains(msg, k) {
            return true
        }
    }
    return false
}

// ---------- MCP 握手 + tools/call ----------

func (c *Client) callToolWithRetry(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
    if err := c.ensureSession(ctx); err != nil {
        return nil, err
    }
    raw, _, err := c.postMCP(ctx, "tools/call", map[string]any{
        "name":      name,
        "arguments": args,
    }, true)
    if err != nil {
        return nil, err
    }
    return c.extractToolResult(raw)
}

func (c *Client) ensureSession(ctx context.Context) error {
    c.mu.Lock()
    if c.sessionID != "" {
        c.mu.Unlock()
        return nil
    }
    c.mu.Unlock()

    raw, sid, err := c.postMCP(ctx, "initialize", map[string]any{
        "protocolVersion": ZaiProtocolVersion,
        "capabilities":    map[string]any{},
        "clientInfo": map[string]any{
            "name":    ZaiClientInfoName,
            "version": "1.0.0",
        },
    }, true)
    if err != nil {
        return err
    }
    // result 内容可丢弃，仅看 err
    _ = raw

    if _, _, err := c.postMCP(ctx, "notifications/initialized", map[string]any{}, true, sid); err != nil {
        // 通知失败一般不致命，但保守起见记录
        return err
    }

    c.mu.Lock()
    c.sessionID = sid
    c.mu.Unlock()
    return nil
}

// ---------- 底层 POST + SSE/JSON 解析 ----------

// postMCP 发送 JSON-RPC 请求；expectResponse=false 时不加 id、只取 sessionId。
func (c *Client) postMCP(
    ctx context.Context, method string, params map[string]any,
    expectResponse bool,
) (json.RawMessage, string, error) {
    return c.postMCPWithSession(ctx, method, params, expectResponse, "")
}

// postMCPWithSession 显式传 sessionId（内部用）。
func (c *Client) postMCPWithSession(
    ctx context.Context, method string, params map[string]any,
    expectResponse bool, sessionID string,
) (json.RawMessage, string, error) {
    req := jsonRPCRequest{
        JSONRPC: "2.0",
        Method:  method,
        Params:  params,
    }
    if expectResponse {
        req.ID = uuid.NewString()
    }

    body, _ := json.Marshal(req)

    timeoutSecs := DefaultTimeoutSecs
    if dl, ok := ctx.Deadline(); ok {
        if sec := time.Until(dl).Seconds(); sec > 0 && sec < float64(MaxTimeoutSecs) {
            timeoutSecs = int(sec) + 1
        }
    }
    ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
    defer cancel()

    httpReq, err := http.NewRequestWithContext(ctx, "POST", ZaiMCPURL, bytes.NewReader(body))
    if err != nil {
        return nil, "", err
    }
    httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("Accept", "application/json, text/event-stream")
    if sessionID != "" {
        httpReq.Header.Set("Mcp-Session-Id", sessionID)
    }

    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, "", &SearchProviderError{Provider: "zai", Message: err.Error(), Status: 0}
    }
    defer resp.Body.Close()

    nextSessionID := resp.Header.Get("Mcp-Session-Id")
    if nextSessionID == "" {
        nextSessionID = sessionID
    }

    if !expectResponse {
        _, _ = io.Copy(io.Discard, resp.Body)
        return nil, nextSessionID, nil
    }

    if !is2xx(resp.StatusCode) {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return nil, nextSessionID, classifyHTTPError(resp.StatusCode, string(bodyBytes))
    }

    raw, err := readAndParseMCPResponse(resp.Body, resp.Header.Get("Content-Type"))
    if err != nil {
        return nil, nextSessionID, err
    }
    return raw, nextSessionID, nil
}

func is2xx(s int) bool { return s >= 200 && s < 300 }

// readAndParseMCPResponse 支持 SSE 与裸 JSON 两种形态。
func readAndParseMCPResponse(body io.Reader, contentType string) (json.RawMessage, error) {
    all, err := io.ReadAll(body)
    if err != nil {
        return nil, &SearchProviderError{Provider: "zai", Message: err.Error(), Status: 500}
    }
    text := string(all)

    // 优先尝试 SSE 解析
    if strings.HasPrefix(contentType, "text/event-stream") ||
        strings.Contains(text, "data:") {
        msgs := parseSSE(text)
        if len(msgs) > 0 {
            return msgs[len(msgs)-1], nil
        }
    }
    // 兜底：裸 JSON
    var raw json.RawMessage
    if err := json.Unmarshal([]byte(text), &raw); err != nil {
        return nil, &SearchProviderError{Provider: "zai", Message: "Failed to parse Z.AI MCP response", Status: 500}
    }
    return raw, nil
}

// parseSSE 按行解析 SSE data:，跳过非 data 行与解析失败行。
func parseSSE(text string) []json.RawMessage {
    var out []json.RawMessage
    scanner := bufio.NewScanner(strings.NewReader(text))
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if !strings.HasPrefix(line, "data:") {
            continue
        }
        data := strings.TrimSpace(line[len("data:"):])
        if data == "" {
            continue
        }
        var raw json.RawMessage
        if err := json.Unmarshal([]byte(data), &raw); err == nil {
            out = append(out, raw)
        }
    }
    return out
}

// ---------- result 提取与错误分类 ----------

func (c *Client) extractToolResult(raw json.RawMessage) (json.RawMessage, error) {
    var resp jsonRPCResponse
    if err := json.Unmarshal(raw, &resp); err != nil {
        return nil, &SearchProviderError{Provider: "zai", Message: "invalid JSON-RPC envelope", Status: 500}
    }
    if resp.Error != nil {
        return nil, &SearchProviderError{
            Provider: "zai",
            Message:  fmt.Sprintf("Z.AI MCP error (%d): %s", resp.Error.Code, resp.Error.Message),
            Status:   resp.Error.Code,
        }
    }
    // 检查 MCP tool isError
    var resultObj map[string]any
    if err := json.Unmarshal(resp.Result, &resultObj); err == nil {
        if b, ok := resultObj["isError"].(bool); ok && b {
            txt := concatContentText(resultObj["content"])
            return nil, &SearchProviderError{
                Provider: "zai",
                Message:  txt,
                Status:   extractMCPStatus(txt, 400),
            }
        }
    }
    return resp.Result, nil
}

func extractMCPStatus(msg string, def int) int {
    re := regexp.MustCompile(`(?i)MCP error\s*(-?\d+)`)
    m := re.FindStringSubmatch(msg)
    if len(m) < 2 {
        return def
    }
    var n int
    fmt.Sscanf(m[1], "%d", &n)
    if n < 0 {
        n = -n
    }
    return n
}

func concatContentText(content any) string {
    arr, ok := content.([]any)
    if !ok {
        return ""
    }
    var parts []string
    for _, item := range arr {
        m, ok := item.(map[string]any)
        if !ok {
            continue
        }
        if s, ok := m["text"].(string); ok {
            parts = append(parts, s)
        }
    }
    return strings.TrimSpace(strings.Join(parts, "\n"))
}

// ---------- HTTP 错误分类 ----------

var creditPattern = regexp.MustCompile(`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

func classifyHTTPError(status int, body string) error {
    if creditPattern.MatchString(body) {
        return &SearchProviderError{Provider: "zai", Message: fmt.Sprintf("zai: credits exhausted"), Status: status}
    }
    switch status {
    case 402:
        return &SearchProviderError{Provider: "zai", Message: "zai: 402 credits exhausted", Status: 402}
    case 401:
        return &SearchProviderError{Provider: "zai", Message: "zai: 401 unauthorized", Status: 401}
    case 403:
        return &SearchProviderError{Provider: "zai", Message: "zai: 403 forbidden", Status: 403}
    }
    return &SearchProviderError{
        Provider: "zai",
        Message:  fmt.Sprintf("Z.AI MCP error (%d): %s", status, body),
        Status:   status,
    }
}

// ---------- result payload 解析（多层嵌套 + 双重编码） ----------

type SearchSource struct {
    Title         string  `json:"title"`
    URL           string  `json:"url"`
    Snippet       string  `json:"snippet,omitempty"`
    PublishedDate string  `json:"publishedDate,omitempty"`
    AgeSeconds    *int64  `json:"ageSeconds,omitempty"`
    Author        string  `json:"author,omitempty"`
}

type SearchResponse struct {
    Provider  string         `json:"provider"`
    Answer    string         `json:"answer,omitempty"`
    Sources   []SearchSource `json:"sources"`
    RequestID string         `json:"requestId,omitempty"`
}

type zaiResult struct {
    Title         string `json:"title,omitempty"`
    Content       string `json:"content,omitempty"`
    Link          string `json:"link,omitempty"`
    URL           string `json:"url,omitempty"`
    Media         string `json:"media,omitempty"`
    PublishDate   string `json:"publish_date,omitempty"`
    PublishedDate string `json:"publishedDate,omitempty"`
}

type zaiResp struct {
    ID          string       `json:"id,omitempty"`
    RequestID   string       `json:"request_id,omitempty"`
    RequestID2  string       `json:"requestId,omitempty"`
    SearchResult []zaiResult `json:"search_result,omitempty"`
    Results     []zaiResult  `json:"results,omitempty"`
}

func parseSearchPayload(raw json.RawMessage) (*SearchResponse, error) {
    // 候选集合
    var rawObj map[string]any
    _ = json.Unmarshal(raw, &rawObj)

    candidates := []any{rawObj}
    var textParts []string
    if rawObj != nil {
        for _, k := range []string{"structuredContent", "data", "result"} {
            if v, ok := rawObj[k]; ok {
                candidates = append(candidates, v)
            }
        }
        if content, ok := rawObj["content"].([]any); ok {
            for _, part := range content {
                pm, ok := part.(map[string]any)
                if !ok {
                    continue
                }
                text, _ := pm["text"].(string)
                if text == "" {
                    continue
                }
                var parsed any
                if err := json.Unmarshal([]byte(text), &parsed); err == nil {
                    if s, ok := parsed.(string); ok {
                        if err2 := json.Unmarshal([]byte(s), &parsed); err2 != nil {
                            parsed = s // 当作 answer 文本
                        }
                    }
                    candidates = append(candidates, parsed)
                    if len(getSearchResults(parsed)) == 0 {
                        textParts = append(textParts, text)
                    }
                } else {
                    textParts = append(textParts, text)
                }
            }
        }
    }

    for _, cand := range candidates {
        rs := getSearchResults(cand)
        if len(rs) > 0 {
            resp := &SearchResponse{
                Provider: "zai",
                Sources:  toSources(rs),
            }
            if len(textParts) > 0 {
                resp.Answer = strings.Join(textParts, "\n\n")
            }
            // 取 requestId
            if m, ok := cand.(map[string]any); ok {
                for _, k := range []string{"request_id", "requestId", "id"} {
                    if s, ok := m[k].(string); ok && s != "" {
                        resp.RequestID = s
                        break
                    }
                }
            }
            return resp, nil
        }
    }

    resp := &SearchResponse{Provider: "zai", Sources: []SearchSource{}}
    if len(textParts) > 0 {
        resp.Answer = strings.Join(textParts, "\n\n")
    }
    return resp, nil
}

func getSearchResults(v any) []zaiResult {
    if arr, ok := v.([]any); ok {
        out := make([]zaiResult, 0, len(arr))
        for _, item := range arr {
            b, _ := json.Marshal(item)
            var r zaiResult
            _ = json.Unmarshal(b, &r)
            out = append(out, r)
        }
        return out
    }
    m, ok := v.(map[string]any)
    if !ok {
        return nil
    }
    if arr, ok := m["search_result"].([]any); ok {
        return toResultSlice(arr)
    }
    if arr, ok := m["results"].([]any); ok {
        return toResultSlice(arr)
    }
    return nil
}

func toResultSlice(arr []any) []zaiResult {
    out := make([]zaiResult, 0, len(arr))
    for _, item := range arr {
        b, _ := json.Marshal(item)
        var r zaiResult
        _ = json.Unmarshal(b, &r)
        out = append(out, r)
    }
    return out
}

func toSources(rs []zaiResult) []SearchSource {
    out := make([]SearchSource, 0, len(rs))
    for _, r := range rs {
        url := firstNonEmpty(r.Link, r.URL)
        if url == "" {
            continue
        }
        published := firstNonEmpty(r.PublishDate, r.PublishedDate)
        out = append(out, SearchSource{
            Title:         firstNonEmpty(r.Title, url),
            URL:           url,
            Snippet:       r.Content,
            PublishedDate: published,
            Author:        r.Media,
        })
    }
    return out
}

func firstNonEmpty(ss ...string) string {
    for _, s := range ss {
        if s != "" {
            return s
        }
    }
    return ""
}
```

> [!warning] 与 TS 实现的差异
> 1. Go 版 `parseSearchPayload` 把 raw result 重新 unmarshal 到 `map[string]any`，会有性能开销——若对延迟敏感，可改成直接 struct decode。
> 2. `getSearchResults` 中通过 `json.Marshal` 中转是兼容 `[]any` → `[]zaiResult` 的最简洁写法；如果上游 schema 稳定，可直接 struct unmarshal。

## 附录 B：依赖清单

```go
// go.mod
module github.com/mocode/mocode-agent

go 1.22

require (
    github.com/google/uuid v1.6.0 // 仅生成 JSON-RPC id
)

// 测试依赖
require (
    github.com/stretchr/testify v1.9.0
)
```

**额外说明**：

- **不需要**重型 MCP 客户端 SDK（如 `github.com/mark3labs/mcp-go`）——Z.AI 实现只用 JSON-RPC 2.0 + Streamable HTTP 一条 path，SDK 反而增加 surface。
- **不需要** SSE 解析库——`bufio.Scanner` 几行就能搞定（参考 `parseSSE`）。
- **不需要** `golang.org/x/sync`——本场景不涉及 fan-out。

## 相关笔记

- [[search-tools-overview]]（待补：search-tools 模块总览）
- [[mcp-json-rpc-spec]]（待补：MCP 2025-03-26 协议规范要点）