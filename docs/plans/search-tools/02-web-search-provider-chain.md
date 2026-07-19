# Web Search Provider Chain 架构深度研究

> 研究路径：`tmp/oh-my-pi/packages/coding-agent/src/web/search/`
>
> 该目录实现了一套完整的 Web Search 统一搜索工具，包含 22 个 Provider 的注册、懒加载、fallback 链、响应标准化与 TUI 渲染。

---

## 1. Provider Registry（懒加载 + 缓存 + 候选解析）

### 1.1 文件定位

`provider.ts` 是所有 Provider 的入口元数据与延迟加载门面。

### 1.2 懒加载机制

```typescript
const PROVIDER_META: Record<SearchProviderId, ProviderMeta> = {
  perplexity: { load: async () => new (await import("./providers/perplexity")).PerplexityProvider() },
  gemini:     { load: async () => new (await import("./providers/gemini")).GeminiProvider() },
  // ... 共 22 个 provider
};

const instanceCache = new Map<SearchProviderId, SearchProvider>();
```

- **零启动开销**：`provider.ts` 本身不 import 任何 provider 模块；只有 `SEARCH_PROVIDER_LABELS` 与 `SEARCH_PROVIDER_ORDER` 从 `types.ts` 引入（纯数据）。
- **动态 import()**：每个 `load()` 在第一次调用时才 `await import()`，后续 `instanceCache` 直接返回单例。
- **缓存策略**：模块级 `Map` 无过期策略；同一 Session 内只构造一次，适合 Provider 常驻（内部维持 OAuth 状态、HTTP 连接池）。

### 1.3 resolveProviderCandidates 算法

```typescript
export function resolveProviderCandidates(
  preferredProvider: SearchProviderId | "auto" = preferredProvId,
): SearchProviderCandidate[] {
  const candidates: SearchProviderCandidate[] = [];

  if (preferredProvider !== "auto" && !isSearchProviderExcluded(preferredProvider)) {
    candidates.push({ id: preferredProvider, explicit: true });
  }

  for (const id of SEARCH_PROVIDER_ORDER) {
    if (id === preferredProvider || isSearchProviderExcluded(id)) continue;
    candidates.push({ id, explicit: false });
  }

  return candidates;
}
```

- **显式优先**：若用户/配置指定了具体 provider，它排第一且标记 `explicit: true`。
- **排除集合**：`setExcludedSearchProviders()` 把 provider 加入黑名单，候选解析与执行双阶段都遵守。
- **固定总序**：`SEARCH_PROVIDER_ORDER` 来源于 `SEARCH_PROVIDER_OPTIONS` 的顺序（去掉 `auto`），保证 UI 下拉框与 fallback 链唯一信源。

---

## 2. Base Provider Interface（生命周期）

### 2.1 核心接口

`providers/base.ts` 定义了抽象基类 `SearchProvider`：

```typescript
export abstract class SearchProvider {
  abstract readonly id: SearchProviderId;
  abstract readonly label: string;
  abstract isAvailable(authStorage: AuthStorage): Promise<boolean> | boolean;
  isExplicitlyAvailable(authStorage: AuthStorage): Promise<boolean> | boolean {
    return this.isAvailable(authStorage);
  }
  abstract search(params: SearchParams): Promise<SearchResponse>;
}
```

### 2.2 isAvailable / isExplicitlyAvailable 语义

| 方法 | 调用时机 | 语义 |
|------|----------|------|
| `isAvailable` | Auto chain 探测 | "现在能否为 auto 链提供服务？" |
| `isExplicitlyAvailable` | 用户显式选择该 provider | "用户点名时是否允许继续？" |

- **Auto chain**：按 `SEARCH_PROVIDER_ORDER` 顺序探测；`isAvailable` 返回 `false` 的 provider 直接跳过。
- **Explicit selection**：调用 `isExplicitlyAvailable`；若返回 `false` 则抛 `SearchProviderError`，**不**静默降级（因为用户明确指定，应该给明确失败信息而非悄悄换 provider）。
- **Override 场景**：
  - `ExaProvider`：无 API key 时公开 MCP fallback 可用 → `isExplicitlyAvailable` 返回 `true`，`isAvailable` 返回 `false`。
  - `PublicWebProvider`：always `isAvailable=false`，`isExplicitlyAvailable=true`（只有显式选择才触发 fan-out）。

### 2.3 SearchParams 单一数据契约

```typescript
export interface SearchParams {
  query: string;
  limit?: number;
  recency?: "day" | "week" | "month" | "year";
  systemPrompt: string;
  signal?: AbortSignal;
  fetch?: FetchImpl;
  maxOutputTokens?: number;
  numSearchResults?: number;
  temperature?: number;
  googleSearch?: Record<string, unknown>;
  codeExecution?: Record<string, unknown>;
  urlContext?: Record<string, unknown>;
  authStorage: AuthStorage;          // 唯一凭证入口
  modelRegistry?: ModelRegistry;
  sessionId?: string;
  antigravityEndpointMode?: "auto" | "production" | "sandbox";
  geminiModel?: string;
}
```

> **关键约束**：Provider 禁止直接打开 `AgentStorage` 或调用 `refreshOpenAICodexToken`；必须通过 `authStorage` 解析凭证，避免绕过 broker 单飞刷新。

---

## 3. 执行流程（index.ts 的 executeSearch）

### 3.1 完整时序

```typescript
async function executeSearch(_toolCallId, params, options)
```

1. **候选构建**：依据 `params.provider` 生成候选列表
   - `explicit !== auto` → `[{id, explicit:true}]`
   - `auto` → `resolveProviderCandidates("auto")`（绕过配置的 preferred，但仍受 exclude 限制）
   - 默认 → `resolveProviderCandidates()`（使用 `preferredProvId`）

2. **公共参数读取**：
   - `settings.get("providers.antigravityEndpoint")`
   - `settings.get("providers.webSearchGeminiModel")`
   - 均包裹 try/catch，容忍 Settings 未初始化（CLI / 单测场景）。

3. **Fallback 循环**：
   ```typescript
   for (const candidate of candidates) {
     provider = await getSearchProvider(candidate.id);   // 懒加载 + 缓存
     available = candidate.explicit
       ? await provider.isExplicitlyAvailable(authStorage)
       : await provider.isAvailable(authStorage);
     if (!available && !candidate.explicit) continue;
     if (!available && candidate.explicit) throw ...;

     response = await provider.search({...params, signal, authStorage, ...});

     if (!hasRenderableSearchContent(response)) throw new SearchProviderError(provider.id, "...", 204);

     return { content: [{type:"text", text: formatForLLM(response)}], details: {response} };
   }
   ```

4. **错误聚合**：
   - 失败记录 `failures.push({provider, error})`
   - 显式选择失败直接 `break`（不继续 fallback）
   - 循环结束后：
     - `availableProviderCount === 0 && failures.length === 0` → 返回 "No web search provider configured"
     - 多 provider 失败 → `All web search providers failed: id1: msg1; id2: msg2 ...`
     - 单 provider 失败 → 仅返回该 provider 的错误信息

5. **Abort 传播**：
   - 每次 catch 先调用 `throwIfAborted(signal)`，确保用户取消立即返回，而不是被吞掉。

### 3.2 外部入口

- `WebSearchTool`（AgentTool）：绑定 session，走 `executeSearch`
- `webSearchCustomTool`（CustomTool）：绑定 TUI 渲染，走相同 `executeSearch`
- `runSearchQuery`：CLI/测试入口，自动 `discoverAuthStorage`，用完关闭

---

## 4. Provider 实现分类

### 4.1 分类总览

| 分类 | Provider | 文件 | 凭证模式 | 返回答案 | 特点 |
|------|----------|------|----------|----------|------|
| **API 型** | perplexity | perplexity.ts | env / OAuth / API key | ✅ LLM 合成 | 四模式（cookie/OAuth/api/anonymous），SSE 流解析 |
| | brave | brave.ts | env `BRAVE_API_KEY` | ❌ 原始结果 | 纯 REST，extra_snippets |
| | tavily | tavily.ts | AuthStorage / env | ✅ advanced answer | topic=general 硬编码，recency 正交 |
| | kagi | kagi.ts | AuthStorage | ✅ answer + related | 复用 `../../kagi` 共享模块 |
| | firecrawl | firecrawl.ts | AuthStorage / env | ❌ | MAX_NUM_RESULTS=100，tbs 日期过滤 |
| | jina | jina.ts | env `JINA_API_KEY` | ❌ | GET `https://s.jina.ai/{query}` |
| | tinyfish | tinyfish.ts | AuthStorage / env | ❌ | 自动翻页，MAX_PAGE=10 |
| | parallel | parallel.ts | AuthStorage / env | ❌ | 复用 `../../parallel`，PARALLEL_BETA_HEADER |
| | synthetic | synthetic.ts | AuthStorage / env | ❌ | 零数据保留 |
| | exa | exa.ts | AuthStorage / env / MCP fallback | ✅ synthesizeAnswer | 请求节流，dual path（API + MCP） |
| | xai | xai.ts | AuthStorage / env `XAI_API_KEY` | ✅ Grok 合成 | xai-oauth 优先，注解收集 citations |
| | zai | zai.ts | AuthStorage / env | ✅ answer | 远程 MCP，JSON-RPC 2.0，多参数重试 |
| **平台原生** | anthropic | anthropic.ts | env / OAuth | ✅ Claude 合成 | web_search_20250305 tool，server_tool_use |
| | gemini | gemini.ts | OAuth (google-gemini-cli / google-antigravity) / API key | ✅ Gemini 合成 | SSE 流解析，antigravity 沙箱/生产双 endpoint |
| | codex | codex.ts | OAuth openai-codex | ✅ Codex 合成 | 模型降级链（GPT-5.6→5.5→5.4...），image placeholder 过滤 |
| **爬虫型** | duckduckgo | duckduckgo.ts | 无 | ❌ | no-JS HTML，POST form，bot challenge 检测 |
| | google | google.ts | 无 | ❌ | linkedom 解析，browserFetch，JavaScript/traffic challenge |
| | startpage | startpage.ts | 无 | ❌ | Google 代理，homepage sc token，CAPTCHA 检测 |
| | ecosia | ecosia.ts | 无 | ❌ | Cloudflare 前，browserFetch + 渲染等待 |
| | mojeek | mojeek.ts | 无 | ❌ | Puppeteer（可选），ALTCHA 验证 |
| **聚合型** | public | public.ts | 无 | ❌ | fan-out 到 5 个爬虫引擎，共识排序，soft/hard deadline |
| **自托管** | searxng | searxng.ts | env / settings | ❌ | JSON API，Basic/Bearer auth，categories/language 过滤 |
| **特殊** | kimi | kimi.ts | env / AuthStorage `kimi-code` | ❌ | Kimi Code 服务，区分 moonshot 平台与 kimi-code |

> 注：原需求中提到的 `bing.ts`、`yahoo.ts` 在代码库中不存在，当前实际提供 22 个 provider。

### 4.2 技术特征细表

#### API 型 Provider

| Provider | 请求方式 | 认证 | 并发/节流 | 响应解析 | Fallback |
|----------|----------|------|-----------|----------|----------|
| perplexity | SSE / POST | Cookie / OAuth / API key | 流式合并 | SSE JSON + 多层 payload 解析 | anonymous → api → oauth |
| brave | GET | Bearer header | 无 | JSON `web.results` | 无 |
| tavily | POST JSON | Bearer | 无 | JSON | retry without recency |
| kagi | POST JSON | AuthStorage resolver | 无 | 共享 `../../kagi` | 无 |
| firecrawl | POST JSON | AuthStorage resolver | 无 | JSON `data.web` | 无 |
| jina | GET | Bearer | 无 | JSON array | 无 |
| tinyfish | GET (page) | AuthStorage resolver | 自动翻页 | JSON | 无 |
| parallel | POST JSON | AuthStorage resolver | 无 | 共享 `../../parallel` | 无 |
| synthetic | POST JSON | AuthStorage resolver | 无 | JSON | 无 |
| exa | POST JSON / MCP | AuthStorage resolver | 全局 delay slot | JSON / MCP SSE | API key → public MCP |
| xai | POST JSON | AuthStorage resolver (xai-oauth 优先) | 无 | Responses API output | 无 |
| zai | MCP JSON-RPC | AuthStorage resolver | 无 | MCP result | 多参数 arg 重试 |

#### 平台原生 Provider

| Provider | 协议 | 认证 | 流式 | 特殊能力 |
|----------|------|------|------|----------|
| anthropic | Messages API | API key / OAuth | ❌ 单次 | `web_search_20250305` tool，server_tool_use block |
| gemini | Cloud Code Assist / Developer API | OAuth / API key | ✅ SSE | google_search / code_execution / url_context grounding |
| codex | OpenAI Codex Responses | OAuth openai-codex | ✅ SSE | web_search tool，responses-lite，image placeholder 过滤，模型降级 |

#### 爬虫型 Provider

| Provider | 请求方式 | 渲染 | 挑战检测 | 递归深度 |
|----------|----------|------|----------|----------|
| duckduckgo | POST HTML | no-JS | anomaly-modal | 1 |
| google | GET HTML | headless browser fallback | unusual traffic / enablejs | 1 |
| startpage | POST HTML | homepage token | CAPTCHA / /errors/ | 2（home→search） |
| ecosia | GET HTML | browserFetch + selector ready | Ecosia Firewall | 1 |
| mojeek | GET HTML | Puppeteer + ALTCHA solve | altcha-widget | 2（可选验证页） |

#### 聚合 & 自托管

| Provider | 模式 | 子引擎 | 排序 | 超时控制 |
|----------|------|--------|------|----------|
| public | 并行 fan-out | startpage, google, duckduckgo, ecosia, mojeek | 共识数 > 最佳排名 > 插入序 | soft 5s + hard 30s |
| searxng | JSON API | 由实例配置决定 | 实例原生排序 | 无硬编码 |

---

## 5. Credential 解析（providers/utils.ts）

### 5.1 findCredential

```typescript
export function findCredential(
  storage: AgentStorage | null | undefined,
  envKey: string | null | undefined,
  ...storageProviders: string[]
): string | null {
  if (envKey) return envKey;          // 1. env 优先
  if (!storage) return null;
  for (const provider of storageProviders) {
    const records = storage.listAuthCredentials(provider);
    for (const record of records) {
      if (record.credential.type === "api_key" && record.credential.key.trim().length > 0)
        return record.credential.key;
      if (record.credential.type === "oauth" && record.credential.access.trim().length > 0)
        return record.credential.access;
    }
  }
  return null;
}
```

### 5.2 优先级规则

| 优先级 | 来源 | 典型 Provider |
|--------|------|---------------|
| 1 | 环境变量 / .env | `BRAVE_API_KEY`, `JINA_API_KEY`, `PERPLEXITY_API_KEY` |
| 2 | AgentStorage（通过 AuthStorage） | `authStorage.getApiKey("tavily", sessionId)` |
| 3 | 混合模式（env 兜底 + AuthStorage 驱动刷新） | `getEnvApiKey("brave") ?? await authStorage.getApiKey(...)` |

- **env vs agent.db 优先级**：env 优先于 agent.db。
- **刷新机制**：通过 `authStorage.resolver()` 返回 `ApiKeyResolver`，再经 `withAuth()` 包装，实现单飞刷新 + 兄弟 rotate。
- **禁止直连**：`base.ts` 注释明确禁止 Provider 打开 `AgentStorage` 或调用 refresh helper；必须走 `params.authStorage`。

### 5.3 工具函数

- `withHardTimeout(signal, ms)`：包装 AbortSignal + 60s 硬超时，防止 Windows/Bun TCP stall 冻结 session。
- `classifyProviderHttpError`：识别 credits exhausted / 401/402/403，返回 `SearchProviderError`，便于 fallback 链识别。
- `toSearchSources`：将任意源数组转换为统一 `SearchSource[]` 并计算 `ageSeconds`。

---

## 6. 输出标准化

### 6.1 SearchResponse 统一结构（types.ts）

```typescript
export interface SearchResponse {
  provider: SearchProviderId | "none";
  answer?: string;                    // LLM 合成答案
  sources: SearchSource[];            // 原始结果
  citations?: SearchCitation[];       // 带上下文引用
  searchQueries?: string[];           // 中间查询（anthropic）
  relatedQuestions?: string[];        // 相关问题
  usage?: SearchUsage;                // 令牌/请求统计
  model?: string;                     // 使用模型
  requestId?: string;                 // 调试追踪 ID
  authMode?: string;                  // "oauth" | "api_key"
}
```

### 6.2 Provider 输出映射规律

| Provider | answer | sources | citations | searchQueries | relatedQuestions | usage |
|----------|--------|---------|-----------|---------------|------------------|-------|
| perplexity | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ totalTokens |
| anthropic | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ input/output/searchRequests |
| gemini | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ input/output/total |
| codex | ✅ | ✅ (markdown extract) | ❌ | ❌ | ❌ | ✅ input/output/total |
| xai | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ input/output/total |
| exa | ✅ (synthesizeAnswer) | ✅ | ❌ | ❌ | ❌ | ❌ |
| tavily | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| zai | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| brave | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| kimi | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| 爬虫型 | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| public | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |

### 6.3 LLM 友好格式化（index.ts → formatForLLM）

```typescript
function formatForLLM(response: SearchResponse): string {
  // 1. answer 文本
  // 2. ## Sources + 数量
  // 3. [序号] title (age)\n    url\n        snippet (truncate 240)
  // 4. ## Citations (citedText)
  // 5. ## Related questions
  // 6. Search queries (最多 3 条)
}
```

输出是纯文本 Markdown 结构，供 LLM 直接消费，不包含富文本标签。

### 6.4 TUI 渲染（render.ts）

- **树形布局**：`renderTreeList` + `markFramedBlockComponent`
- **折叠/展开**：Sources 默认折叠，Answer 默认完整（CLI compact 模式可 cap）
- **状态指示**：成功 `tool.webSearch accent`，失败 `warning/error`
- **元信息**：Provider label, auth mode (OAuth/API), usage 统计
- **超链接**：`urlHyperlink` 渲染 TUI 可点击链接
- **年龄格式化**：`formatAge(src.ageSeconds)` 人性化时间

---

## 7. 架构图

### 7.1 Provider 分类架构

```
┌─────────────────────────────────────────────────────────────────┐
│                    SearchProvider (abstract base)                │
│  + id, label                                                     │
│  + isAvailable(authStorage)                                      │
│  + isExplicitlyAvailable(authStorage)                            │
│  + search(params: SearchParams): Promise<SearchResponse>         │
└────────────────────┬────────────────────────────────────────────┘
                     │ implements
     ┌───────────────┼───────────────┬─────────────┬──────────────┐
     ▼               ▼               ▼             ▼              ▼
  API Provider    Platform       Scraper      Aggregate     Self-hosted
  (12)            (3)            (6)          (1)           (1)
```

### 7.2 懒加载 + Fallback 时序图

```
User/Agent           provider.ts          provider module       index.ts
    │                     │                      │                  │
    │ getSearchProvider("perplexity")           │                  │
    │────────────────────►│                      │                  │
    │                     │ instanceCache miss   │                  │
    │                     │─────────────────────►│                  │
    │                     │   import("./providers/perplexity")      │
    │                     │                     │  new PerplexityProvider()
    │                     │◄────────────────────│                  │
    │                     │ cache instance       │                  │
    │◄────────────────────│                      │                  │
    │                     │                      │                  │
    │ executeSearch(...)                       │                  │
    │─────────────────────────────────────────►│                  │
    │                     │                      │  isAvailable()   │
    │                     │────────────────────────────────────────►│
    │                     │                      │  search()        │
    │                     │────────────────────────────────────────►│
    │                     │                      │  SearchResponse  │
    │                     │◄────────────────────────────────────────│
    │                     │                      │                  │
    │   {content, details}                      │                  │
    │◄─────────────────────────────────────────│                  │
    │                     │                      │                  │
    │  (若失败)            │                      │                  │
    │  failures.push()    │                      │                  │
    │  continue ───────────────────────────────────────────────────►│
    │                     │                      │  next candidate   │
```

### 7.3 executeSearch 状态机

```
         ┌──────────┐
         │  Start   │
         └────┬─────┘
              ▼
    ┌─────────────────┐
    │ Build Candidates │───explicit──► [explicit provider]
    └────────┬────────┘              isExplicitlyAvailable?
              │ auto / default          │
              ▼                        ├─ false ──► Error (break)
    ┌─────────────────┐              └─ true
    │ Iterate Chain   │                   ▼
    │ (resolveProvider │          ┌─────────────┐
    │  Candidates)     │          │   search()  │
    └────────┬────────┘          └──────┬──────┘
              │                         │
              │                         ▼
              │               ┌─────────────────┐
              │               │ hasRenderable?  │
              │               └────────┬────────┘
              │                        │
              │              true ──────┴───── false
              │               │                │
              │               ▼                ▼
              │         ┌─────────┐    ┌────────────┐
              │         │ Success │    │   Throw    │
              │         └────┬────┘    └─────┬──────┘
              │               │              │
              │               │   failures.push()
              │               │   throwIfAborted
              │               │   continue / break
              │               ▼              ▼
              │         ┌──────────────────────────┐
              │         │   End of candidates?     │
              │         └──────────┬───────────────┘
              │                    │
              │                    ▼
              │         ┌─────────────────────────┐
              │         │   Aggregate failures     │
              │         │   & return error message │
              │         └─────────────────────────┘
              ▼
         ┌──────────┐
         │   End    │
         └──────────┘
```

---

## 8. 对本项目的借鉴点

### 8.1 已有 netcommon.Provider 链的对比

| 维度 | oh-my-pi web search | mocode netcommon.Provider |
|------|---------------------|---------------------------|
| **注册方式** | 静态 Record + 动态 import | 反射/字符串映射？ |
| **懒加载** | ✅ 按需 import，单例缓存 | ？ |
| **候选解析** | `resolveProviderCandidates` + exclude 集合 | ？ |
| **生命周期** | `isAvailable` / `isExplicitlyAvailable` 双探测 | 可能只有 `isAvailable` |
| **凭证隔离** | `AuthStorage` 唯一入口，禁止直连 | ？ |
| **错误聚合** | `SearchProviderError` + `formatSearchProviderFailures` | ？ |
| **输出标准化** | `SearchResponse` 统一 DTO | 各 provider 自行返回 |

### 8.2 可直接借鉴的设计

1. **双探测生命周期**：
   - `isAvailable` 控制 auto 链准入（便宜、同步优先）
   - `isExplicitlyAvailable` 控制显式选择（允许无凭证 fallback 如 Exa MCP / PublicWeb）
   - mocode 可引入同样语义，区分"配置可用"与"用户强制可用"。

2. **排除集合**：
   - `setExcludedSearchProviders` 统一控制显式 + auto 链的黑名单。
   - 非常适合 mocode 的多租户/租户级屏蔽需求。

3. **凭证单一入口**：
   - `base.ts` 注释明确禁止 Provider 直连 `AgentStorage`。
   - mocode 的 `netcommon.Provider` 链可加同样注释约束，防止凭证绕过。

4. **候选解析前置**：
   - `resolveProviderCandidates` 纯同步、无副作用，可用于：
     - CLI `--list-providers`
     - UI 下拉框渲染
     - 预加载提示

5. **硬超时兜底**：
   - `withHardTimeout(signal, 60000)` 解决 Bun Windows 不传播 AbortSignal 的 bug。
   - mocode 跨平台可复用。

6. **错误分类器**：
   - `classifyProviderHttpError` 把 credits/401/402/403 映射为带 status 的 `SearchProviderError`。
   - fallback 链可据此区分"凭证问题"与"服务端问题"。

7. **PublicWeb 聚合模式**：
   - soft deadline (5s) + hard deadline (30s) + straggler abort
   - 共识排序（engines 数 > bestRank > 插入序）
   - 适合 mocode 做多数据源聚合（如多渠道消息源合并）。

### 8.3 值得注意的差异

- oh-my-pi 的 `provider.ts` 是**单模块注册表**，不依赖 IoC 容器； mocode 的 `netcommon.Provider` 若用 DI 容器，可考虑保持注册表模式但保留自动发现能力。
- oh-my-pi 的 Provider 全部放在一个目录下； mocode 可按 domain 分包（如 `providers/search/`, `providers/chat/`）。
- `SearchResponse` 的 `answer` 字段只对 LLM-mediated provider 填充； mocode 的 DTO 设计可保持同样分层（结构化结果 + 合成答案）。
