---
tags: [search, provider, fallback, architecture, design, oh-my-pi, port-to-go]
aliases: [Provider 链设计, 搜索 Provider 链]
created: 2026-09-14
updated: 2026-09-14
status: active
relatedKnowledge: []
---

# oh-my-pi Provider 懒加载 + Fallback 链：深度调研与 Go 移植设计

> [!abstract] 调研范围与目标
>
> 把 `oh-my-pi/packages/coding-agent/src/web/search/` 下「22 个 provider 的注册表 + 懒加载 + fallback 链 + 自定义 provider 接入」机制逐行挖透，输出可被 mocode Go 版 `netcommon.Provider` 链直接吸收的设计方案。
>
> 调研源文件：
>
> - `provider.ts` — 静态注册表 + 动态 `import()` 懒加载 + 单例缓存
> - `index.ts` — `executeSearch` 主循环（fallback 状态机）
> - `providers/base.ts` — `SearchProvider` 抽象基类 + `SearchParams` DTO
> - `types.ts` — `SearchProviderId` / `SearchProviderError` / `SearchSource` / `SearchResponse` 等核心类型
> - `providers/utils.ts` — `withHardTimeout` / `classifyProviderHttpError` / `findCredential`
> - `render.ts` — TUI 树状渲染抽象

## 一、PROVIDER_META 注册表结构

### 1.1 22 个 provider 的元数据形态

`provider.ts` 中每个 provider 一条三字段记录 —— 这是整个懒加载机制的最小化元数据：

```35:108:packages/coding-agent/src/web/search/provider.ts
interface ProviderMeta {
    id: SearchProviderId;
    label: string;
    load: () => Promise<SearchProvider>;
}

const PROVIDER_META: Record<SearchProviderId, ProviderMeta> = {
    perplexity: {
        id: "perplexity",
        label: SEARCH_PROVIDER_LABELS.perplexity,
        load: async () => new (await import("./providers/perplexity")).PerplexityProvider(),
    },
    // ... 21 个同类条目
    public:    { ..., load: async () => new (await import("./providers/public")).PublicWebProvider() },
};
```

**22 个 provider**（按 `SEARCH_PROVIDER_OPTIONS` 顺序，去掉 `auto`）：

| 类别 | 数量 | 列表 |
|---|---|---|
| LLM-mediated（需要 API key 或 OAuth） | 12 | perplexity, gemini, anthropic, codex, xai, zai, kimi, kagi, exa, tinyfish, firecrawl, parallel |
| 纯搜索 API（需要 key） | 5 | jina, tavily, brave, searxng, synthetic |
| 凭证免费（浏览器抓取） | 5 | duckduckgo, google, ecosia, startpage, mojeek |
| 聚合器（meta-provider） | 1 | public（fan-out 上述 5 个凭证免费引擎） |

> [!tip] 元数据只持有 `id/label/load`
>
> 故意不放 `defaultTimeoutMs`、`defaultLimit`、`homepage`、`description` 等运行时元数据 —— 这些都在对应 provider 类内部定义。注册表保持极薄（≈ 4 行 × N provider），意图明确：注册表只负责「找到这个 provider 类」，不参与任何运行决策。

### 1.2 `instanceCache: Map` 单例缓存生命周期

```111:111:packages/coding-agent/src/web/search/provider.ts
const instanceCache = new Map<SearchProviderId, SearchProvider>();
```

- **模块级**（`private`），跨 `getSearchProvider()` 调用保持。
- **写入时机**：`getSearchProvider(id)` 第一次加载后写入。
- **没有过期 / 失效** —— 整个进程生命周期内最多构造 22 次（每 provider 一次）。
- **没有并发保护**：`Map.set/get` 在 Node.js 单线程模型下天然原子，不需要锁。

```131:138:packages/coding-agent/src/web/search/provider.ts
export async function getSearchProvider(id: SearchProviderId): Promise<SearchProvider> {
    const cached = instanceCache.get(id);
    if (cached) return cached;
    const meta = PROVIDER_META[id];
    if (!meta) throw new Error(`Unknown search provider: ${id}`);
    const provider = await meta.load();
    instanceCache.set(id, provider);
    return provider;
}
```

### 1.3 为什么用 `await import()` 而非 `require()` 或静态 import

```1:14:packages/coding-agent/src/web/search/provider.ts
// Lazy registry of web search providers.
//
// Each provider is loaded on first use; importing this module loads zero
// provider implementations. Provider modules are heavy (each pulls in
// fetch/parse/format helpers) and only one — at most — is needed per session,
// so eager construction was wasted work at startup.
```

四点原因，按重要性排序：

1. **启动性能**：22 个 provider 每个都 `import` HTML 解析、SSE 解析、JSON-RPC、HTTP 客户端、formatting helpers。一次性全部加载会让 CLI 启动时间变长（实测数据未在源码中给出，但注释明确表态「eager construction was wasted work at startup」）。
2. **零依赖路径**：用户只配 Exa，不应该让 Bun 把 Anthropic、Perplexity、OpenAI 等 SDK 拉到进程里。动态 `import()` 是 ESM 唯一支持「按需加载 + tree-shake」的语法。
3. **tree-shaking 友好**：静态 `import { PerplexityProvider } from './perplexity'` 会让 bundler 永远把它打进 chunk；`await import()` 是字面字符串，bundler 识别不到。
4. **运行时容错**：某个 provider 模块初始化失败（导入阶段抛错）不会影响其他 provider —— 因为失败要等到 `load()` 第一次执行才暴露。

> [!warning] 不允许 `require()`
>
> 项目是 ESM-only（bun、TS strict + ESM）。`require()` 在 ESM 模块里是禁止的。即使在 CJS 环境也不应该用 `require()`，因为它会同步阻塞整个 event loop；`await import()` 是异步的，符合 provider 加载的网络语义。

---

## 二、`resolveProviderCandidates` 算法

### 2.1 preferredProvider 显式 vs auto 路径

`resolveProviderCandidates` 是「候选构建器」，**不加载模块** —— 这是与 `resolveProviderChain` 的根本区别。

```194:208:packages/coding-agent/src/web/search/provider.ts
export function resolveProviderCandidates(forcedProvider?: SearchProviderId): SearchProviderCandidate[] {
    const candidates: SearchProviderCandidate[] = [];

    if (forcedProvider !== undefined && !isSearchProviderExcluded(forcedProvider)) {
        candidates.push({ id: forcedProvider, explicit: true });
    }

    for (const id of orderedProvIds) {
        if (id === forcedProvider || isSearchProviderExcluded(id)) continue;
        candidates.push({ id, explicit: explicitProvIds.has(id) });
    }

    return candidates;
}
```

| `forcedProvider` | 行为 |
|---|---|
| `undefined`（`auto`） | 只走 `orderedProvIds`；其中 `explicitProvIds`（用户手动列入 `providers.webSearchOrder`）标 `explicit: true` |
| 某个具体 id | 该 id 强制排首位、跳过排除检查、标 `explicit: true`；其余按 `orderedProvIds` 续接 |

### 2.2 `isSearchProviderExcluded` 黑名单检查时机

黑名单检查发生在 **两个时刻**（同一个 `isSearchProviderExcluded` 函数）：

1. `resolveProviderCandidates` 构造候选时 — 决定**谁能进候选数组**。
2. `executeSearch` 主循环 — 这里**不再**调 `isSearchProviderExcluded`，因为已经被候选构造过滤了。

> [!tip] 「两步检查」的语义清晰度
>
> 把「能不能进候选」和「执行时的可用性」分到两阶段处理，让 fallback 链对设置变化是 deterministic 的：candidate 列表一旦构造完毕，整个 `executeSearch` 调用都不会再读 `excludedProvIds`。

### 2.3 `SEARCH_PROVIDER_ORDER` 来源

```76:81:packages/coding-agent/src/web/search/types.ts
export const SEARCH_PROVIDER_ORDER: readonly SearchProviderId[] = SEARCH_PROVIDER_OPTIONS.flatMap(option =>
    option.value === "auto" ? [] : [option.value],
);
```

**单一信源**：`SEARCH_PROVIDER_OPTIONS`（带 `value/label/description` 三字段）→ 派生 `SEARCH_PROVIDER_ORDER`、`SEARCH_PROVIDER_LABELS`、`SEARCH_PROVIDER_CHOICES` 三个常量。**UI 下拉框、CLI 设置、fallback 链共享一份定义，永不漂移**。

```156:170:packages/coding-agent/src/web/search/provider.ts
export function setSearchProviderOrder(providers: readonly SearchProviderId[]): void {
    const prioritized = new Set(providers.filter(id => SEARCH_PROVIDER_ORDER.includes(id)));
    explicitProvIds = prioritized;
    orderedProvIds =
        prioritized.size === 0
            ? SEARCH_PROVIDER_ORDER
            : [...prioritized, ...SEARCH_PROVIDER_ORDER.filter(id => !prioritized.has(id))];
}
```

- 用户在 `providers.webSearchOrder` 里列出的 id → 进 `explicitProvIds`（这些 id 在 auto 链里也标 `explicit: true`，等同于显式选择）。
- 用户未列出的 id → 按 `SEARCH_PROVIDER_ORDER` 默认顺序续接。
- 用户列表为空 → 退回到内置 `SEARCH_PROVIDER_ORDER`。

> [!warning] `flatMap` 的隐性副作用
>
> `flatMap` 自动展平 + 过滤 `auto`，比 `filter().map()` 更紧凑，但要警惕 `option.value === "auto"` 那一行：如果未来加 `auto-foo`、`auto-bar` 这种前缀值，必须改用 `option.value.startsWith("auto")`。当前是单一 sentinel，硬编码够用。

---

## 三、`executeSearch` 状态机

`index.ts` 的 `executeSearch` 是 fallback 链的核心实现，**全文 60 行包含全部状态转移**。

### 3.1 状态机总览

```210:286:packages/coding-agent/src/web/search/index.ts
async function executeSearch(_toolCallId, params, options): Promise<...> {
    // [1] 候选构建
    const candidates = params.provider && params.provider !== "auto"
        ? [{ id: params.provider, explicit: true }]
        : resolveProviderCandidates();

    // [2] 全局 settings 读取（容忍未初始化）
    let timeoutMs = DEFAULT_WEB_SEARCH_TIMEOUT_SECONDS * 1_000;
    try { /* read settings with safe defaults */ } catch {}

    // [3] 主循环
    const failures = [];
    let availableProviderCount = 0;
    let lastProvider: ...;
    for (const candidate of candidates) {
        try {
            provider = await getSearchProvider(candidate.id);                        // [3a] 懒加载

            const available = candidate.explicit
                ? await provider.isExplicitlyAvailable(authStorage)                 // [3b] 显式探测
                : await provider.isAvailable(authStorage);                          // [3b] cheap 探测

            if (!available && !candidate.explicit) continue;                        // [3c] auto 跳过
            if (!available && candidate.explicit) throw new SearchProviderError(...); // [3c] explicit 报错

            availableProviderCount++;
            const response = await provider.search({ /* SearchParams */ });         // [3d] 实际搜索

            // [3e] 约束后处理（site:/inurl:/intitle:/filetype:/date 指令）
            if (parsedQuery.hasConstraints && response.sources.length > 0) {
                finalResponse = applyQueryConstraints(response.sources, parsedQuery);
            }

            // [3f] renderability 校验
            if (!hasRenderableSearchContent(finalResponse)) {
                throw new SearchProviderError(provider.id, "...no renderable...", 204);
            }

            return { content: [...], details: { response: finalResponse } };        // [3g] 成功退出
        } catch (error) {
            throwIfAborted(signal);                                                 // [3h] 用户取消优先
            failures.push({ provider: provider ?? providerMeta, error });           // [3i] 失败累加
        }
    }

    // [4] 终态：区分 "no provider configured" vs "all failed"
    if (availableProviderCount === 0 && failures.length === 0) {
        return { ... "No web search provider configured." ... };
    }
    const lastFailure = failures[failures.length - 1];
    const message = failures.length > 1
        ? `All web search providers failed: ${formatSearchProviderFailures(failures)}`
        : formatSearchProviderFailure(lastFailure.error, lastFailure.provider);
    return { ... `Error: ${message}` ... };
}
```

### 3.2 break vs continue 判定表

| 情况 | 触发条件 | 行为 |
|---|---|---|
| auto + `isAvailable` 返回 false | `candidate.explicit === false && !available` | **`continue`**（无声跳过） |
| explicit + `isExplicitlyAvailable` 返回 false | `candidate.explicit && !available` | **`throw`**（SearchProviderError，让用户知道「你选的不行」） |
| 用户取消 | catch 块里的 `throwIfAborted(signal)` | **跳出循环 + 抛 AbortError**（不计入 failures） |
| provider `search()` 抛错 | 任何业务错误 | **`failures.push` + 继续循环** |
| provider 返回空内容 | `hasRenderableSearchContent === false` | **`throw` 204 + `failures.push`**（算 provider 失败） |
| 任意 provider 成功 | 拿到非空 `SearchResponse` | **`return` 退出整个函数** |

### 3.3 「No web search provider configured」vs「All web search providers failed」

**关键判定**：

```286:288:packages/coding-agent/src/web/search/index.ts
if (availableProviderCount === 0 && failures.length === 0) {
    const message = "No web search provider configured.";
    ...
}
```

- `availableProviderCount === 0` 表示**没有任何 provider 通过探测**（auto 全跳过、explicit 全抛错）。
- `failures.length === 0` 表示**没有任何错误**（即全是 `continue`，意味着配置根本没启用任何一个 provider）。

→ 这就是「没配置」vs「都失败」的关键区分。

**剩下的所有非成功路径**都走：

```294:302:packages/coding-agent/src/web/search/index.ts
const lastFailure = failures[failures.length - 1];
const baseMessage = lastFailure
    ? formatSearchProviderFailure(lastFailure.error, lastFailure.provider)
    : `Unknown error from ${lastProvider?.label ?? "web search provider"}`;
const message = failures.length > 1
    ? `All web search providers failed: ${formatSearchProviderFailures(failures)}`
    : baseMessage;
```

- 单失败 → 用 `formatSearchProviderFailure` 渲染**最后一次**失败（最相关信息）。
- 多失败 → 用 `formatSearchProviderFailures` 渲染**全部**失败，逗号分隔。

> [!warning] 「最后一次失败」的语义
>
> 用 `failures[failures.length - 1]` 作为「最有意义的错误」是 oh-my-pi 的选择 —— 这假设**最后一次失败通常是最相关的**（前面的失败可能被后面的成功掩盖）。但如果最后一个 provider 本身是「注定失败」的（例如显式选择了一个 unknown provider），用户看到的错误可能不是根因。Go 移植时可考虑改为「第一个 fatal error」语义，或者按 explicit / 状态码优先级排序。

### 3.4 `throwIfAborted` 在 catch 中的位置

```277:281:packages/coding-agent/src/web/search/index.ts
} catch (error) {
    // Surface user-initiated cancellation immediately so the session sees
    // a clean abort instead of a generic "all providers failed" message.
    throwIfAborted(signal);
    failures.push({ provider: provider ?? providerMeta, error });
}
```

**关键**：`throwIfAborted(signal)` 放在 `failures.push` **之前**。这意味着：

- 用户按 Esc → fetch 抛 AbortError → 进入 catch → `throwIfAborted` 立即重抛 → 整个循环中断 → 用户看到的是干净的「Cancelled」消息。
- 如果顺序倒过来 → AbortError 被压进 `failures` → 循环继续跑下一个 provider → 浪费时间 → 最后用户看到的是「all providers failed」（掩盖了取消原因）。

> [!tip] Go 移植映射
>
> `throwIfAborted(signal)` ≡ `ctx.Err() != nil { return ctx.Err() }`。在 Go 中无需在 catch 块写，标准的 `if err := ctx.Err(); err != nil { return err }` 放在每次循环顶部即可 —— 编译器看不出区别，但语义更清晰。

---

## 四、`SearchProvider` 抽象基类双探测语义

### 4.1 接口骨架

```19:104:packages/coding-agent/src/web/search/providers/base.ts
export interface SearchParams {
    query: string;
    parsedQuery?: StructuredQuery;
    limit?: number;
    recency?: "day" | "week" | "month" | "year";
    systemPrompt: string;
    signal?: AbortSignal;
    timeoutMs?: number;
    fetch?: FetchImpl;
    maxOutputTokens?: number;
    numSearchResults?: number;
    temperature?: number;
    googleSearch?: Record<string, unknown>;
    codeExecution?: Record<string, unknown>;
    urlContext?: Record<string, unknown>;
    authStorage: AuthStorage;
    modelRegistry?: ModelRegistry;
    sessionId?: string;
    antigravityEndpointMode?: "auto" | "production" | "sandbox";
    geminiModel?: string;
}

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

### 4.2 isAvailable vs isExplicitlyAvailable 差别

| 方法 | 调用时机 | 语义 | 默认实现 |
|---|---|---|---|
| `isAvailable(authStorage)` | auto chain 中探测（`candidate.explicit === false`） | "**现在**（cheap 探测）能否为 auto 链提供服务？" | 各 provider override |
| `isExplicitlyAvailable(authStorage)` | explicit selection（`candidate.explicit === true`） | "用户显式选了你，是否应该跑？" | **默认调用 `isAvailable`**（保守行为） |

**两种语义差异**：

- `isAvailable` 是 **strict gate**：无凭证 / 未配置 → auto 链无声跳过。
- `isExplicitlyAvailable` 是 **lenient gate**：用户既然显式选了，provider 应尝试 fallback（即使主路径不可用）。

### 4.3 默认实现链（base 提供默认 vs Provider override）

```102:104:packages/coding-agent/src/web/search/providers/base.ts
isExplicitlyAvailable(authStorage: AuthStorage): Promise<boolean> | boolean {
    return this.isAvailable(authStorage);
}
```

**默认行为**：`isExplicitlyAvailable` ≡ `isAvailable`。这是「保守」策略 —— 如果 provider 不 override，它就跟 auto 链同标准。

**Override 的 9 个 provider**（从 grep `isExplicitlyAvailable`）：

| Provider | `isAvailable` | `isExplicitlyAvailable` | 原因 |
|---|---|---|---|
| `Startpage` | `true` | `true` | 凭证免费，auto 链也允许 |
| `DuckDuckGo` | `true` | `true` | 同上 |
| `Ecosia` | `true` | `true` | 同上 |
| `Mojeek` | `true` | `true` | 同上 |
| `Public` (聚合器) | `false` | `true` | **auto 链不允许聚合**（避免无限递归 / 与已有引擎重复跑）但显式可用 |
| `Perplexity` | 有凭证 → true | `true` | 显式选择时允许 fallback 到 OpenRouter/anonymous |
| `Exa` | 有 key 或 env key → true | `true` | 显式选择时允许 fallback 到 keyless MCP |
| `Firecrawl` | 有 key → true | `true` | 显式选择时允许 fallback 到 keyless mode |

**对比表里的几个反直觉点**：

- `Public`（聚合器）的 `isAvailable` 是 `false`！这是故意的 —— auto 链已经把所有引擎按顺序排好了，聚合器 fan-out 是「用户主动选择」行为，不是 fallback 行为。
- `Perplexity` 的 `isAvailable` 看 `authStorage.hasAuth("perplexity")`，但 `isExplicitlyAvailable` 是 `true` —— 因为 Perplexity 接受匿名 browser-style ask。

---

## 五、`SearchParams` / `SearchResponse` 统一 DTO

### 5.1 `SearchParams` 字段语义

必填：

- `query: string` — 原始查询字符串
- `authStorage: AuthStorage` — 凭证统一入口（**禁止 Provider 直连**）
- `signal: AbortSignal?` — 用户取消信号

可选扩展（按是否常用分类）：

| 字段 | 用途 | 谁用 |
|---|---|---|
| `parsedQuery` | 结构化查询（site:/before:/OR/...） | 所有 provider，约束后处理 |
| `limit` / `numSearchResults` | 结果数上限 | 所有 provider |
| `recency` | 时间窗（"day"/"week"/"month"/"year"） | 搜索 API（excludes Google 等 scraper） |
| `temperature` / `maxOutputTokens` | LLM 参数 | LLM-mediated（perplexity, gemini, ...） |
| `systemPrompt` | 系统 prompt | LLM-mediated |
| `timeoutMs` | 硬超时（ms） | 所有 provider |
| `fetch` | 可注入的 HTTP 客户端 | 单元测试 |
| `modelRegistry` | 模型注册表 | LLM-mediated |
| `sessionId` | round-robin / sticky key | 多凭证同 provider |
| `googleSearch` / `codeExecution` / `urlContext` | Gemini 特有 | Gemini |
| `antigravityEndpointMode` | Antigravity sandbox 选择 | Gemini |
| `geminiModel` | Gemini 模型选择 | Gemini |

> [!warning] `recency` 字段的硬性约束
>
> `base.ts` 注释里**明确禁止**把 `recency` 当作隐式信号改变 topic scope。如果 provider API 把时间窗和其他维度耦合（Tavily 的 `topic=news`），provider 实现必须自行解耦，否则会被审计出来。

### 5.2 `SearchResponse` 标准字段

```194:241:packages/coding-agent/src/web/search/types.ts
export interface SearchResponse {
    provider: SearchProviderId | "none";      // 必填：来源 provider 标识
    answer?: string;                          // LLM-mediated 才有
    sources: SearchSource[];                  // 必填：源列表
    citations?: SearchCitation[];             // LLM-mediated 才有
    searchQueries?: string[];                 // Anthropic 等 LLM-mediated 才有
    relatedQuestions?: string[];              // Perplexity 等 LLM-mediated 才有
    usage?: SearchUsage;                      // token / request 计数
    model?: string;                           // LLM-mediated 才有
    requestId?: string;                       // 调试用
    authMode?: string;                        // "oauth" / "api_key" / ... TUI 显示用
}

export interface SearchSource {
    title: string;
    url: string;
    snippet?: string;
    publishedDate?: string;
    ageSeconds?: number;
    author?: string;
}

export interface SearchCitation {
    url: string;
    title: string;
    citedText?: string;
}

export interface SearchUsage {
    inputTokens?: number;
    outputTokens?: number;
    searchRequests?: number;      // Anthropic: 多少次 web_search 请求
    totalTokens?: number;         // Perplexity: 合并 token
}
```

**关键字段语义**：

- `provider: "none"` —— 显式 sentinel，用于错误返回（"无可用 provider"）。Go 移植时可考虑用 `*string` 或 `Option<SearchProviderId>` 替代 sentinel。
- `sources: []` —— 即便 `answer` 非空，`sources` 也可能空（聚合器返回 `answer + sources` 二者并存）。
- `authMode` —— TUI 用来显示「API Key」/「OAuth」徽章。Go 移植时可改为 `AuthMode enum`。

### 5.3 来源 provider 标识（`provider` 字段）

`SearchResponse.provider` 严格取 `SearchProviderId` 联合 + `"none"` sentinel。**聚合器（public）返回 `provider: "public"`**，不展开成 5 个子引擎 —— 这避免了「用户看到的来源」和「实际扇出的引擎」不一致。

---

## 六、凭证统一入口（`authStorage` 单例 vs 直连）

### 6.1 base.ts 注释明确禁止 provider 直连 AgentStorage

```18:28:packages/coding-agent/src/web/search/providers/base.ts
/**
 * `authStorage` is the **only** credential source providers may consult.
 * Opening a sibling SQLite handle or calling provider-direct refresh helpers
 * (e.g. `refreshOpenAICodexToken`, `refreshGoogleCloudToken`) is prohibited:
 * it races the broker's per-credential refresh and POSTs the broker sentinel
 * (`REMOTE_REFRESH_SENTINEL`) to the upstream token endpoint, which classifies
 * as `invalid_grant` and disables the row.
 */
```

**为什么不能直连**（4 层理由）：

1. **并发刷新冲突**：每个 provider 各自打开 AgentStorage 会触发 SQLite 并发写，导致 last-write-wins 覆盖其他 provider 的刷新结果。
2. **broker 单飞失败**：oh-my-pi 的 auth broker 有 per-credential single-flight 机制（防止多个并发请求同时刷新同一凭证）。直连会绕过这层并发控制，导致 N 个并发请求触发 N 次 refresh。
3. **broker sentinel 污染**：oh-my-pi 在本地 SQL 凭证行里写入 `REMOTE_REFRESH_SENTINEL`，下游直连如果拿到 sentinel token 当真凭证 POST 给上游，会被分类为 `invalid_grant` → 凭证行被标记为不可用 → 用户再也用不了。
4. **OAuth refresh 协议不一致**：每个 provider 的 OAuth 流程（`refreshOpenAICodexToken` vs `refreshGoogleCloudToken`）都有自己的状态机。统一入口是 broker 唯一的真相源。

### 6.2 `authStorage.resolver(id, opts)` → ApiKeyResolver 协议

```6978:7015:packages/ai/src/auth-storage.ts
resolver(provider: string, options?: { sessionId?: string; baseUrl?: string; modelId?: string }): ApiKeyResolver {
    const { sessionId, baseUrl, modelId } = options ?? {};
    return async ({ lastChance, error, signal, previousKey }) => {
        if (error === undefined) {
            return this.getApiKey(provider, sessionId, { baseUrl, modelId, signal });
        }
        if (lastChance) {
            const switched = await this.rotateSessionCredential(provider, sessionId, {
                error, modelId, signal, apiKey: previousKey,
            });
            // ...
        }
        // step (b): force-refresh same credential
    };
}
```

**Resolver 协议**（`ApiKeyResolver`）：

```9:47:packages/ai/src/auth-retry.ts
export type ApiKeyResolveContext = {
    lastChance: boolean;
    error: unknown;
    signal: AbortSignal;
    previousKey: string | undefined;
};
export type ApiKeyResolver = (ctx: ApiKeyResolveContext) => Promise<string | undefined> | string | undefined;
export type ApiKey = string | ApiKeyResolver;
```

- 静态字符串 → 一次性 attempt，无 retry。
- Resolver → 三段式 a/b/c retry 策略：
  - a) initial（`error: undefined`）→ resolve 初始凭证
  - b) `!lastChance` → force-refresh 同一 session 凭证
  - c) `lastChance` → 切换到 sibling credential（如果有）

### 6.3 `withAuth` / `withOAuthAccess` 单飞刷新机制

```231:280:packages/ai/src/auth-retry.ts
export async function withAuth<T>(
    key: ApiKey | undefined,
    attempt: (key: string) => Promise<T>,
    opts?: { isAuthError?: (error: unknown) => boolean; signal?: AbortSignal; missingKeyMessage?: string },
): Promise<T> {
    const resolver = key;
    const signal = opts?.signal;
    const initialKey = await resolveRetryKey(resolver, false, undefined, signal);
    if (initialKey === undefined) throw missingKey();

    const state = createAuthRetryKeyState(initialKey);
    let lastError: unknown;
    try {
        return await attempt(initialKey);
    } catch (error) {
        if (!isAuthError(error)) throw error;
        lastError = error;
    }

    while (true) {
        const nextKey = await resolveNextAuthRetryKey(state, resolver, lastError, signal);
        if (nextKey === undefined) break;
        try {
            return await attempt(nextKey);
        } catch (error) {
            if (!isAuthError(error)) throw error;
            lastError = error;
        }
    }
    throw lastError;
}
```

**调用方典型形态**（来自 `exa.ts`）：

```588:594:packages/coding-agent/src/web/search/providers/exa.ts
const storedKey = params.authStorage
    ? await params.authStorage.getApiKey("exa", params.sessionId, { signal: params.signal })
    : undefined;
const keyOrResolver: ApiKey | undefined =
    storedKey && params.authStorage
        ? params.authStorage.resolver("exa", { sessionId: params.sessionId })
        : getEnvApiKey("exa");
const response = keyOrResolver
    ? await withAuth(keyOrResolver, key => callExaSearch(key, params), { signal: params.signal })
    : await callExaMcpSearch(params);
```

**关键**：先调 `getApiKey` 做一次 cheap probe（看有没有凭证）→ 拿到凭证后构造 `resolver` → 走 `withAuth` 走完整 retry 链。**两边是分开的**，但都是 `authStorage` 单例的方法。

### 6.4 env vs agent.db 优先级（`findCredential`）

```28:50:packages/coding-agent/src/web/search/providers/utils.ts
export function findCredential(
    storage: AgentStorage | null | undefined,
    envKey: string | null | undefined,
    ...storageProviders: string[]
): string | null {
    if (envKey) return envKey;       // [1] env 优先
    if (!storage) return null;

    try {
        for (const provider of storageProviders) {
            const records = storage.listAuthCredentials(provider);
            for (const record of records) {
                const credential = record.credential;
                if (credential.type === "api_key" && credential.key.trim().length > 0) {
                    return credential.key;
                }
                if (credential.type === "oauth" && credential.access.trim().length > 0) {
                    return credential.access;
                }
            }
        }
    } catch {
        return null;
    }
    return null;
}
```

**优先级**（按代码顺序）：

1. **环境变量**（`envKey`，调用方已解析）—— 最高优先级
2. `api_key` 类型凭证 —— agent.db 第一匹配
3. `oauth` 类型凭证（取 `access` 字段）—— agent.db 兜底

> [!warning] env 优先的副作用
>
> 如果用户设置了 `EXA_API_KEY=invalid_key`，**agent.db 里合法的 Exa key 不会被使用**。这是「env 永远胜出」语义，符合传统 Unix 哲学（环境变量可临时覆盖持久配置）。但用户体验上偶尔反直觉 —— Go 移植时可考虑加一个 `OMP_AUTH_OVERRIDE=prefer-db` 开关。

---

## 七、硬超时与中止

### 7.1 `withHardTimeout(signal, 60000)` 解决 Windows/Bun TCP stall

```70:84:packages/coding-agent/src/web/search/providers/utils.ts
export const SEARCH_HARD_TIMEOUT_MS = DEFAULT_WEB_SEARCH_TIMEOUT_SECONDS * 1_000;

export function withHardTimeout(signal: AbortSignal | undefined, ms: number = SEARCH_HARD_TIMEOUT_MS): AbortSignal {
    const timeout = AbortSignal.timeout(ms);
    return signal ? AbortSignal.any([signal, timeout]) : timeout;
}
```

**问题来源**：Bun 在 Windows 上使用 WinHTTP backend，**TCP/TLS stall 时不传播 AbortSignal**（issue `oven-sh/bun#15275`、`oven-sh/bun#18536`）。如果只用 `signal`，用户的 Esc 永远到不了 native 层 → session 冻结。

**解决方案**：`AbortSignal.timeout(60s)` 创建一个绝对时间触发的信号，`AbortSignal.any([userSignal, timeoutSignal])` 组合两者 → 任何一个触发都终止。

### 7.2 `throwIfAborted` 在循环中的位置

（参见 §3.4，**catch 块顶部，failures.push 之前**）

### 7.3 `AbortSignal.any([signal, timeout])` 组合

`AbortSignal.any()` 是 WHATWG 标准的 `AbortSignal` 静态方法，把多个信号合并为「任一触发即触发」的复合信号。**注意**：传入 `signal = undefined` 时要短路，否则 `AbortSignal.any([undefined, ...])` 会抛 `TypeError`。

```typescript
// 标准用法
const composed = AbortSignal.any([userSignal, timeoutSignal]);

// public.ts 在聚合器里的复合：
const signal = AbortSignal.any([
    withHardTimeout(params.signal, params.timeoutMs),
    straggler.signal,    // 内部控制器：soft/hard deadline 触发时主动 abort
]);
```

> [!tip] Go 移植映射
>
> `AbortSignal.timeout(ms)` + `AbortSignal.any([...])` ≡ `context.WithTimeout(parent, ms)`。Go 的 `context` 原生就是复合的（`WithTimeout(WithCancel(parent), ...)`），不需要 `any()`。
>
> Windows TCP stall 问题在 Go 里**不存在** —— Go 的 `net/http` 严格尊重 `context.Done()`。但仍然需要 `context.WithTimeout`，因为某些 server 端不返回响应也不关闭连接（nginx 默认 60s 才超时）。

---

## 八、错误分类器

### 8.1 `SearchProviderError(status, message, provider)` 结构

```243:252:packages/coding-agent/src/web/search/types.ts
export class SearchProviderError extends Error {
    constructor(
        public readonly provider: SearchProviderId,
        message: string,
        public readonly status?: number,
    ) {
        super(message);
        this.name = "SearchProviderError";
    }
}
```

三个字段：

- `provider: SearchProviderId` —— 错误来源（聚合器 fan-out 时保留 `provider: "public"`，**不**展开为子引擎）
- `message: string` —— 已经经过 `classifyProviderHttpError` 或 provider 自定义的人类可读消息
- `status?: number` —— HTTP 状态码（200/204/401/402/403/429/503）

### 8.2 `classifyProviderHttpError` 正则 (`credits/quota/insufficient`)

```99:114:packages/coding-agent/src/web/search/providers/utils.ts
const CREDIT_BODY_PATTERN = /credits?\s*(?:exhausted|exceeded)|quota|insufficient/i;

export function classifyProviderHttpError(
    provider: SearchProviderId,
    status: number,
    body: string,
): SearchProviderError | null {
    if (CREDIT_BODY_PATTERN.test(body)) {
        return new SearchProviderError(provider, `${provider}: credits exhausted`, status);
    }
    if (status === 402) {
        return new SearchProviderError(provider, `${provider}: 402 credits exhausted`, status);
    }
    if (status === 401) {
        return new SearchProviderError(provider, `${provider}: 401 unauthorized`, status);
    }
    if (status === 403) {
        return new SearchProviderError(provider, `${provider}: 403 forbidden`, status);
    }
    return null;
}
```

**匹配顺序**（按特异性降序）：

1. **body 文本正则**（`credits? (exhausted|exceeded) | quota | insufficient`）—— 匹配 15.1.7/15.1.8 telemetry 统计的真实信号
2. **402** —— 显式 credits exhausted（Stripe 风格）
3. **401** —— unauthorized
4. **403** —— forbidden
5. **null** —— 留给 caller 抛 provider-specific 错误

**为什么不匹配 429？** — 因为 429 是 rate limit，不是 quota。Exa 的 MCP 路径单独抛 `429 rate limit`，不通过这个 classifier。

### 8.3 `formatSearchProviderFailures` 聚合多 provider 错误信息

```124:128:packages/coding-agent/src/web/search/provider.ts
export function formatSearchProviderFailures(
    failures: readonly { provider: Pick<SearchProvider, "id" | "label">; error: unknown }[],
): string {
    return failures.map(f => `${f.provider.id}: ${formatSearchProviderFailure(f.error, f.provider)}`).join("; ");
}
```

聚合形态：`perplexity: 401 unauthorized; exa: credits exhausted; duckduckgo: bot challenged`。**空 query 友好的细节**：

```115:122:packages/coding-agent/src/web/search/provider.ts
export function formatSearchProviderFailure(error: unknown, provider: Pick<SearchProvider, "id" | "label">): string {
    if (error instanceof SearchProviderError) {
        if (error.provider === "anthropic" && error.status === 404) {
            return "Anthropic web search returned 404 (model or endpoint not found).";
        }
        if (error.status === 401 || error.status === 403) {
            if (error.provider === "zai") return error.message;
            return `${getSearchProviderLabel(error.provider)} authorization failed (${error.status}). Check API key or base URL.`;
        }
        return error.message;
    }
    if (error instanceof Error) return error.message;
    return `Unknown error from ${provider.label}`;
}
```

**特殊分支**：

- **Anthropic 404** —— 单独文案，因为 404 通常意味着 model 或 endpoint 不存在（不是凭证问题）
- **zai 401/403** —— 直接返回 `error.message`，因为 zai 的错误文案对用户更友好
- **其他 401/403** —— 通用模板：`"<Label> authorization failed (status). Check API key or base URL."`

---

## 九、TUI 渲染抽象

### 9.1 `render.ts` 的 `renderTreeList` + `markFramedBlockComponent`

`renderSearchResult` 的核心调用栈（`render.ts` line 90-130）：

```typescript
const sourceTree = renderTreeList(
    {
        items: sources,
        expanded,
        maxCollapsed: MAX_COLLAPSED_ITEMS,    // PREVIEW_LIMITS.COLLAPSED_ITEMS
        itemType: "source",
        renderItem: src => {
            const titleText = src.title || src.url || "Untitled";
            const domain = getDomain(src.url);
            const age = formatAge(src.ageSeconds) || src.publishedDate;
            const linkedTitle = url ? urlHyperlink(url, title) : title;
            return [`${linkedTitle}${metaSuffix}`];
        },
    },
    theme,
);

return markFramedBlockComponent({
    render(width: number): readonly string[] {
        return outputBlock.render({
            header,
            state: sourceCount > 0 ? "success" : "warning",
            sections: [
                { label: "Query",    lines: [queryPreview] },
                { label: "Answer",   lines: answerLines },
                { label: "Sources",  lines: sourceTree },
                { label: "Metadata", lines: metaLines },
            ],
            width,
        }, theme);
    },
    invalidate() { outputBlock.invalidate(); },
});
```

**关键抽象**：

1. `renderTreeList` —— 把 `sources[]` 渲染成「树状带 `├─` `└─` 字符的列表」，每条 source 一行。
2. `markFramedBlockComponent` —— 把组件标记为「有边框的输出块」（framed），周围自动绘制 `┌─` `─` `─┐` `│` `└` `┘`。
3. `CachedOutputBlock` —— 缓存渲染结果，避免每帧重算（在 TUI 重绘场景里关键）。
4. `outputBlockContentWidth(width)` —— 计算内容可用宽度（边框内）。

### 9.2 折叠 / 展开默认状态

折叠由 `RenderResultOptions.expanded` 控制（外部传入），与 `MAX_COLLAPSED_ITEMS` 配合：

- `expanded === false` → 只显示前 N 条 source（`PREVIEW_LIMITS.COLLAPSED_ITEMS`，具体值未在源码里看到定义但模式清晰）
- `expanded === true` → 显示全部 source
- `formatExpandHint(theme, expanded, hasMore)` → 显示「点击展开」提示

```typescript
const maxLines = expanded ? lines.length : 6;
const displayLines = lines.slice(0, maxLines).map(line => truncateToWidth(line.trim(), 110));
const remaining = lines.length - displayLines.length;
```

**Answer section** 类似：折叠时 `args?.maxAnswerLines` cap（CLI `omp q` 用），TUI 不 cap 全显示。

### 9.3 `urlHyperlink` 在 TUI 中如何渲染

```typescript
const linkedTitle = url ? urlHyperlink(url, title) : title;
```

`urlHyperlink` 是 `pi-tui` 包提供的工具，**底层使用 ANSI OSC 8 转义序列**：

```
\u001b]8;;https://example.com\u001b\\Click here\u001b]8;;\u001b\\
```

支持的终端（iTerm2、Kitty、WezTerm、Windows Terminal）会把这段文本渲染为可点击的超链接；不支持的终端直接显示原始文字（不影响可读性）。

**降级**：如果 `url` 为空，直接显示 `title`（无 OSC 包裹）。

---

## 十、Go 移植架构建议

### 10.1 注册表模式 vs IoC 容器 vs 接口注册器

| 模式 | 实现成本 | 类型安全 | 自定义 Provider 接入 | 推荐度 |
|---|---|---|---|---|
| **静态 map 注册表**（oh-my-pi 风格） | 低 | 中（需要 type assertion） | 需修改源文件 `provider.go` 加 entry | ★★★ |
| **IoC 容器**（uber/dig、wire） | 高 | 高 | 通过 `di.NewModule()` 动态注册 | ★★ |
| **接口注册器**（plugin-style） | 中 | 高 | 通过 `func init()` 或显式 `Register("exa", &ExaProvider{})` | ★★★★ |

**推荐**：**接口注册器 + 内置 init()**。

- 内置 provider 用 `var _ = Register("exa", &ExaProvider{})` 在同包 `init()` 链式注册。
- 外部 provider 通过 `netcommon.Register("myengine", &MyProvider{})` 在 main.go 注册（类似 `http.HandleFunc`）。
- 注册表是 `map[string]ProviderFactory`，`ProviderFactory = func() (Provider, error)` 实现懒加载（factory 不立即构造实例）。

### 10.2 懒加载用 `sync.Once` 还是 `sync.Map`

oh-my-pi 用 `Map`（JS 单线程无并发问题）。Go 必须考虑并发安全。

| 方案 | 优点 | 缺点 |
|---|---|---|
| `sync.Map` | 无锁读 | 写竞争多时退化；类型断言需小心 |
| `map + sync.RWMutex` | 简单清晰 | 写时全锁，但 provider 注册只发生一次 |
| `sync.Once` per id | 每个 id 一次原子 | 多 id 多次初始化开销 |
| **`sync.Map` + factory pattern** | 读极快，写极少（只注册时写） | 类型擦除 |

**推荐**：

```go
type Registry struct {
    mu        sync.RWMutex
    factories map[string]ProviderFactory
    instances sync.Map  // map[string]Provider
}

func (r *Registry) Get(id string) (Provider, error) {
    if v, ok := r.instances.Load(id); ok {
        return v.(Provider), nil
    }
    r.mu.RLock()
    factory, ok := r.factories[id]
    r.mu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", id)
    }
    p, err := factory()
    if err != nil {
        return nil, err
    }
    if actual, loaded := r.instances.LoadOrStore(id, p); loaded {
        return actual.(Provider), nil  // race：别人先存了
    }
    return p, nil
}
```

**关键**：用 `LoadOrStore` 而不是直接 `Store` —— 处理「两个 goroutine 同时调 Get(id)」的 race（一个 factory 跑两次，但只保留一个实例）。

### 10.3 Credentials broker 接口设计

```go
// CredentialBroker 是 provider **唯一**允许的凭证来源。
// 严禁 Provider 直连 SQLite / Vault / env / 文件 —— 那样会绕过 broker 的
// per-credential single-flight 刷新机制，并可能把 broker sentinel
// 误当成真凭证 POST 给上游。
type CredentialBroker interface {
    // GetAPIKey 是 cheap probe：探测 provider 是否有可用凭证。
    GetAPIKey(ctx context.Context, provider string, opts GetKeyOpts) (string, error)

    // Resolver 返回一个 ApiKeyResolver 函数，闭包持有 broker 引用，
    // 每次调用执行 a/b/c 三段式 retry：
    //   a) initial:  resolve 当前 session-sticky 凭证
    //   b) !lastChance: force-refresh 同一凭证
    //   c) lastChance: 切换 sibling 凭证（如有）
    Resolver(provider string, opts ResolverOpts) ApiKeyResolver

    // HasAuth 用于 isAvailable 探测（同步 cheap 检查）。
    HasAuth(provider string) bool

    // GetOAuthAccess 用于需要 identity metadata 的场景（如 Anthropic OAuth）。
    GetOAuthAccess(ctx context.Context, provider string, opts GetAccessOpts) (OAuthAccess, error)

    // RotateSessionCredential 主动触发 sibling 切换。
    RotateSessionCredential(ctx context.Context, provider string, sessionID string, opts RotateOpts) (bool, error)
}

type GetKeyOpts struct {
    SessionID string
    BaseURL   string
    ModelID   string
}

type ResolverOpts struct {
    SessionID string
    BaseURL   string
    ModelID   string
}

type ApiKeyResolveContext struct {
    LastChance  bool
    Error       error
    Ctx         context.Context
    PreviousKey string
}

type ApiKeyResolver func(ApiKeyResolveContext) (string, error)

type ApiKey = ApiKeyResolver // API key 可以是静态字符串（特殊路径）或 resolver

type OAuthAccess struct {
    AccessToken string
    ExpiresAt   time.Time
    // ...
}

type GetAccessOpts struct {
    SessionID    string
    ForceRefresh bool
}

type RotateOpts struct {
    Error        error
    ModelID      string
    APIKey       string
    CredentialID int
}
```

### 10.4 `context` 取代 `AbortSignal` 的所有映射点

| AbortSignal 用法 | Go context 对应 | 备注 |
|---|---|---|
| `signal.throwIfAborted()` | `ctx.Err()` | 主入口 |
| `withHardTimeout(signal, 60000)` | `context.WithTimeout(ctx, 60s)` | Go 原生，无 WinHTTP bug |
| `AbortSignal.any([a, b])` | `context.WithCancel` + `select` 多 channel | Go 没有 `any()`，手动合并 |
| `AbortSignal.timeout(ms)` | `context.WithTimeout(ctx, ms)` | 同上 |
| `signal.addEventListener("abort", cb)` | `go func() { <-ctx.Done(); cb() }()` | 异步监听 |
| `signal.removeEventListener` | defer cancel() | 自动清理 |

### 10.5 错误聚合器接口

```go
// Failure 是单个 provider 失败记录。
type Failure struct {
    ProviderID string
    ProviderLabel string
    Err        error
}

// ErrorClassifier 把任意 error 归一为 ProviderError 或 nil。
type ErrorClassifier func(providerID string, status int, body []byte) *ProviderError

// FailureFormatter 渲染单个 / 多个 failure 为人类可读字符串。
type FailureFormatter func(failure Failure) string
type FailuresFormatter func(failures []Failure) string

// ProviderError 是统一错误结构。
type ProviderError struct {
    ProviderID string
    Message    string
    Status     int    // HTTP 状态码，0 = 非 HTTP 错误
    Cause      error  // 原始错误（debug 用）
}

func (e *ProviderError) Error() string { return e.Message }
func (e *ProviderError) Unwrap() error { return e.Cause }
```

**关键**：聚合器把 `[]Failure` 渲染成 `perplexity: 401 unauthorized; exa: credits exhausted` 这种格式，逻辑全部在 `netcommon` 包，provider 不参与。

### 10.6 mocode 现有 `netcommon.Provider` 链如何最小改动吸收

当前 mocode 形态：

```go
// internal/core/tools/external/common/net/provider.go
type Provider interface {
    Name() string
    Search(ctx context.Context, client *http.Client, query string, maxResults int) ([]SearchResult, error)
}

type MultiProvider struct {
    Providers []Provider
}

func (m MultiProvider) Search(ctx, client, query, maxResults) ([]SearchResult, error) {
    var lastErr error
    for _, p := range m.Providers {
        results, err := p.Search(ctx, client, query, maxResults)
        if err != nil { lastErr = err; continue }
        if len(results) > 0 { return results, nil }
    }
    // ...
}
```

**最小改动方案**（保留 `Provider` 接口，向后兼容）：

1. **保留 `Provider.Search` 签名**：返回 `([]SearchResult, error)` 不变。
2. **新增 `ProviderV2` 接口**（带 `IsAvailable(ctx, broker) bool` + `IsExplicitlyAvailable(...) bool` + `SearchV2(ctx, req SearchRequest) (SearchResponse, error)`）。
3. **新增 `SearchResponse` / `SearchSource` / `SearchCitation` 类型**（与 oh-my-pi 对齐，Go 风格命名）。
4. **改造 `MultiProvider` 为 `Chain`**：
   - 接受 `[]ProviderV2`
   - 调 `IsAvailable` 做 auto 探测
   - 调 `SearchV2` 走 fallback
   - 累积 `[]Failure`
5. **新增 `Registry`**：替代当前的硬编码 `MultiProvider{Providers: []Provider{...}}`。
6. **新增 `CredentialBroker` 接口** + 默认实现（包装 `internal/transport/auth` 或独立新包）。
7. **保留 `Provider` 作为 `ProviderV2` 的适配器**：老 provider（如 `DuckDuckGoProvider`）通过 `providerAdapter{old Provider}` 自动实现 `ProviderV2`，零代码改动。

**最小代码骨架见 §11**。

---

## 十一、Go 侧 Provider 链完整代码骨架（最小可运行）

> [!tip] 最小可运行：接口 + 注册表 + fallback 循环 + 凭证 broker

### 11.1 接口定义

```go
// internal/core/tools/external/common/net/provider.go（最小骨架）

package netcommon

import (
    "context"
    "fmt"
    "sync"
)

// ─── 统一 DTO ─────────────────────────────────────────────────────────

type Source struct {
    Title         string
    URL           string
    Snippet       string
    PublishedDate string
    AgeSeconds    int64
    Author        string
}

type Citation struct {
    URL       string
    Title     string
    CitedText string
}

type Usage struct {
    InputTokens    int64
    OutputTokens   int64
    SearchRequests int64
    TotalTokens    int64
}

type SearchResponse struct {
    ProviderID       string     // "exa" / "public" / "none"
    Answer           string     // LLM-mediated provider 才有
    Sources          []Source   // 必填，错误时为空
    Citations        []Citation // LLM-mediated 才有
    SearchQueries    []string   // Anthropic 等才有
    RelatedQuestions []string
    Usage            Usage
    Model            string
    RequestID        string
    AuthMode         string // "oauth" / "api_key" / ""
}

// ─── 错误类型 ─────────────────────────────────────────────────────────

type ProviderError struct {
    ProviderID string
    Message    string
    Status     int // HTTP 状态码；0 = 非 HTTP 错误
    Cause      error
}

func (e *ProviderError) Error() string { return e.Message }
func (e *ProviderError) Unwrap() error { return e.Cause }

// ─── Provider 接口（V2，对齐 oh-my-pi 双探测语义） ───────────────────

type SearchRequest struct {
    Query              string
    Limit              int
    Recency            string // "day"|"week"|"month"|"year"
    MaxOutputTokens    int
    NumSearchResults   int
    Temperature        float64
    SystemPrompt       string
    Fetch              FetchFunc // 可注入 HTTP 客户端（测试用）
    TimeoutMs          int64     // 硬超时
    SessionID          string    // round-robin / sticky key
    ModelRegistry      ModelRegistry // 可选：模型注册表
}

type FetchFunc func(ctx context.Context, req *HTTPRequest) (*HTTPResponse, error)

type ModelRegistry interface {
    AuthStorage() CredentialBroker
}

type ProviderV2 interface {
    ID() string
    Label() string

    // IsAvailable: auto chain 探测（cheap；可同步查 broker）
    IsAvailable(ctx context.Context, broker CredentialBroker) bool

    // IsExplicitlyAvailable: explicit selection 时 lenient。
    // 默认实现：return IsAvailable(ctx, broker)
    IsExplicitlyAvailable(ctx context.Context, broker CredentialBroker) bool {
        return p.IsAvailable(ctx, broker)
    }

    Search(ctx context.Context, req SearchRequest, broker CredentialBroker) (SearchResponse, error)
}

// ─── 凭证 Broker 接口 ────────────────────────────────────────────────

type CredentialBroker interface {
    GetAPIKey(ctx context.Context, provider string, opts GetKeyOpts) (string, error)
    Resolver(provider string, opts ResolverOpts) ApiKeyResolver
    HasAuth(provider string) bool
    GetOAuthAccess(ctx context.Context, provider string, opts GetAccessOpts) (OAuthAccess, error)
    RotateSessionCredential(ctx context.Context, provider string, sessionID string, opts RotateOpts) (bool, error)
}

type GetKeyOpts struct {
    SessionID string
    BaseURL   string
    ModelID   string
}

type ResolverOpts struct {
    SessionID string
    BaseURL   string
    ModelID   string
}

type GetAccessOpts struct {
    SessionID    string
    ForceRefresh bool
}

type RotateOpts struct {
    Error        error
    ModelID      string
    APIKey       string
    CredentialID int
}

type ApiKeyResolveContext struct {
    LastChance  bool
    Error       error
    Ctx         context.Context
    PreviousKey string
}

type ApiKeyResolver func(ApiKeyResolveContext) (string, error)

type OAuthAccess struct {
    AccessToken string
    ExpiresAt   time.Time
}

// ─── HTTP 抽象（与 net/http 解耦，测试可注入） ─────────────────────────

type HTTPRequest struct {
    Method  string
    URL     string
    Headers map[string]string
    Body    []byte
}

type HTTPResponse struct {
    StatusCode int
    Headers    map[string]string
    Body       []byte
}

// ─── 候选构建器 ───────────────────────────────────────────────────────

type Candidate struct {
    ID      string
    Explicit bool
}

type Resolved struct {
    ProviderID string
    Provider   ProviderV2
}

func ResolveCandidates(forced string, ordered, excluded, explicit map[string]bool) []Candidate {
    var out []Candidate
    seen := map[string]bool{}

    if forced != "" && !excluded[forced] {
        out = append(out, Candidate{ID: forced, Explicit: true})
        seen[forced] = true
    }

    // 按 ordered 的顺序遍历，保证 deterministic
    for id := range ordered {
        if seen[id] || excluded[id] {
            continue
        }
        out = append(out, Candidate{ID: id, Explicit: explicit[id]})
        seen[id] = true
    }
    return out
}

// ─── 注册表 ───────────────────────────────────────────────────────────

type ProviderFactory func() (ProviderV2, error)

type Registry struct {
    mu        sync.RWMutex
    factories map[string]ProviderFactory
    instances sync.Map // map[string]ProviderV2
}

func NewRegistry() *Registry {
    return &Registry{factories: map[string]ProviderFactory{}}
}

func (r *Registry) Register(id string, factory ProviderFactory) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.factories[id] = factory
}

// MustRegister 用于 init() 里的内置 provider 注册；panic on conflict。
func (r *Registry) MustRegister(id string, factory ProviderFactory) {
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.factories[id]; exists {
        panic(fmt.Sprintf("netcommon: provider %q already registered", id))
    }
    r.factories[id] = factory
}

func (r *Registry) Get(id string) (ProviderV2, error) {
    if v, ok := r.instances.Load(id); ok {
        return v.(ProviderV2), nil
    }
    r.mu.RLock()
    factory, ok := r.factories[id]
    r.mu.RUnlock()
    if !ok {
        return nil, fmt.Errorf("netcommon: unknown provider %q", id)
    }
    p, err := factory()
    if err != nil {
        return nil, fmt.Errorf("netcommon: factory %q failed: %w", id, err)
    }
    actual, _ := r.instances.LoadOrStore(id, p)
    return actual.(ProviderV2), nil
}

func (r *Registry) FactoryIDs() []string {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]string, 0, len(r.factories))
    for id := range r.factories {
        out = append(out, id)
    }
    sort.Strings(out)
    return out
}

// ─── Fallback 链 ──────────────────────────────────────────────────────

type Failure struct {
    ProviderID   string
    ProviderLabel string
    Err          error
}

type Chain struct {
    Registry  *Registry
    Broker    CredentialBroker
    ForcedID  string  // 当前请求的 forced provider（per-request 覆盖）
    Excluded  map[string]bool
    Ordered   []string
    Explicit  map[string]bool
    TimeoutMs int64
}

func (c *Chain) Search(ctx context.Context, req SearchRequest) (SearchResponse, []Failure, error) {
    timeoutMs := c.TimeoutMs
    if timeoutMs <= 0 {
        timeoutMs = 60_000
    }
    ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
    defer cancel()

    candidates := ResolveCandidates(c.ForcedID, setFrom(c.Ordered), c.Excluded, c.Explicit)

    var failures []Failure
    availableCount := 0
    var lastProvider ProviderV2

    for _, cand := range candidates {
        provider, err := c.Registry.Get(cand.ID)
        if err != nil {
            failures = append(failures, Failure{ProviderID: cand.ID, Err: err})
            continue
        }

        available := cand.Explicit
            ? provider.IsExplicitlyAvailable(ctx, c.Broker)
            : provider.IsAvailable(ctx, c.Broker)

        if !available && !cand.Explicit {
            continue // auto chain 无声跳过
        }
        if !available && cand.Explicit {
            // explicit 失败：显式抛错，计入 failures
            failures = append(failures, Failure{
                ProviderID: cand.ID,
                ProviderLabel: provider.Label(),
                Err: &ProviderError{
                    ProviderID: cand.ID,
                    Message: fmt.Sprintf("%s web search is unavailable. Configure its credentials or select the automatic provider chain.", provider.Label()),
                },
            })
            continue
        }

        availableCount++
        lastProvider = provider

        resp, err := provider.Search(ctx, req, c.Broker)
        if err != nil {
            failures = append(failures, Failure{
                ProviderID: cand.ID,
                ProviderLabel: provider.Label(),
                Err: err,
            })
            continue
        }

        if !hasRenderableContent(resp) {
            failures = append(failures, Failure{
                ProviderID: cand.ID,
                ProviderLabel: provider.Label(),
                Err: &ProviderError{
                    ProviderID: cand.ID,
                    Message: fmt.Sprintf("%s returned no renderable search content.", provider.Label()),
                    Status: 204,
                },
            })
            continue
        }
        return resp, nil, nil // 成功
    }

    if availableCount == 0 && len(failures) == 0 {
        return SearchResponse{ProviderID: "none"}, nil, ErrNoProviderConfigured
    }

    // 失败聚合
    var lastFailure *Failure
    if n := len(failures); n > 0 {
        lastFailure = &failures[n-1]
    }
    var providerID string
    if lastFailure != nil {
        providerID = lastFailure.ProviderID
    } else if lastProvider != nil {
        providerID = lastProvider.ID()
    } else {
        providerID = "none"
    }
    return SearchResponse{ProviderID: providerID}, failures, ErrAllProvidersFailed
}

// ─── 错误分类器 ───────────────────────────────────────────────────────

var creditPattern = regexp.MustCompile(`(?i)credits?\s*(?:exhausted|exceeded)|quota|insufficient`)

func ClassifyHTTPError(providerID string, status int, body []byte) *ProviderError {
    if creditPattern.Match(body) {
        return &ProviderError{ProviderID: providerID, Message: fmt.Sprintf("%s: credits exhausted", providerID), Status: status}
    }
    switch status {
    case 402:
        return &ProviderError{ProviderID: providerID, Message: fmt.Sprintf("%s: 402 credits exhausted", providerID), Status: status}
    case 401:
        return &ProviderError{ProviderID: providerID, Message: fmt.Sprintf("%s: 401 unauthorized", providerID), Status: status}
    case 403:
        return &ProviderError{ProviderID: providerID, Message: fmt.Sprintf("%s: 403 forbidden", providerID), Status: status}
    }
    return nil
}

// ─── 失败格式化器 ─────────────────────────────────────────────────────

func FormatFailure(f Failure) string {
    var pe *ProviderError
    if errors.As(f.Err, &pe) {
        switch {
        case f.ProviderID == "anthropic" && pe.Status == 404:
            return "Anthropic web search returned 404 (model or endpoint not found)."
        case pe.Status == 401 || pe.Status == 403:
            if f.ProviderID == "zai" {
                return pe.Message
            }
            return fmt.Sprintf("%s authorization failed (%d). Check API key or base URL.", f.ProviderLabel, pe.Status)
        }
        return pe.Message
    }
    return f.Err.Error()
}

func FormatFailures(failures []Failure) string {
    parts := make([]string, len(failures))
    for i, f := range failures {
        parts[i] = fmt.Sprintf("%s: %s", f.ProviderID, FormatFailure(f))
    }
    return strings.Join(parts, "; ")
}

// ─── Renderability 校验 ───────────────────────────────────────────────

func hasRenderableContent(r SearchResponse) bool {
    if strings.TrimSpace(r.Answer) != "" {
        return true
    }
    if len(r.Sources) > 0 {
        return true
    }
    for _, c := range r.Citations {
        if c.URL != "" || c.Title != "" {
            return true
        }
    }
    for _, q := range r.RelatedQuestions {
        if strings.TrimSpace(q) != "" {
            return true
        }
    }
    for _, q := range r.SearchQueries {
        if strings.TrimSpace(q) != "" {
            return true
        }
    }
    return false
}

// ─── 内置 Provider 示例（演示用） ────────────────────────────────────

type DuckDuckGoProvider struct{}

func (DuckDuckGoProvider) ID() string   { return "duckduckgo" }
func (DuckDuckGoProvider) Label() string { return "DuckDuckGo" }

func (DuckDuckGoProvider) IsAvailable(_ context.Context, _ CredentialBroker) bool {
    return true
}

func (DuckDuckGoProvider) Search(ctx context.Context, req SearchRequest, _ CredentialBroker) (SearchResponse, error) {
    // 调用现有的 SearchDuckDuckGo（兼容 mocode 现有实现）
    httpClient := &http.Client{Timeout: 30 * time.Second}
    results, err := SearchDuckDuckGo(ctx, httpClient, req.Query, req.Limit)
    if err != nil {
        return SearchResponse{}, err
    }
    sources := make([]Source, len(results))
    for i, r := range results {
        sources[i] = Source{Title: r.Title, URL: r.Link, Snippet: r.Snippet}
    }
    return SearchResponse{ProviderID: "duckduckgo", Sources: sources}, nil
}

// ─── 包级单例 ─────────────────────────────────────────────────────────

var Default = NewRegistry()

func init() {
    Default.MustRegister("duckduckgo", func() (ProviderV2, error) { return DuckDuckGoProvider{}, nil })
    // Default.MustRegister("exa", func() (ProviderV2, error) { return ExaProvider{}, nil })
    // ...
}

// ─── 哨兵错误 ─────────────────────────────────────────────────────────

var (
    ErrNoProviderConfigured = errors.New("no web search provider configured")
    ErrAllProvidersFailed   = errors.New("all web search providers failed")
)
```

### 11.2 接口定义清单（汇总）

| 接口 / 类型 | 文件位置 | 关键方法 / 字段 | 对应 oh-my-pi 实体 |
|---|---|---|---|
| `Source` | `provider.go` | `Title, URL, Snippet, ...` | `SearchSource` |
| `Citation` | `provider.go` | `URL, Title, CitedText` | `SearchCitation` |
| `Usage` | `provider.go` | `InputTokens, OutputTokens, ...` | `SearchUsage` |
| `SearchResponse` | `provider.go` | `ProviderID, Answer, Sources, ...` | `SearchResponse` |
| `ProviderError` | `provider.go` | `ProviderID, Message, Status, Cause` | `SearchProviderError` |
| `SearchRequest` | `provider.go` | `Query, Limit, Recency, ...` | `SearchParams` |
| `ProviderV2` | `provider.go` | `ID, Label, IsAvailable, IsExplicitlyAvailable, Search` | `SearchProvider` |
| `CredentialBroker` | `provider.go` | `GetAPIKey, Resolver, HasAuth, ...` | `AuthStorage` |
| `ApiKeyResolver` | `provider.go` | `func(ApiKeyResolveContext) (string, error)` | `ApiKeyResolver` |
| `Candidate` | `provider.go` | `ID, Explicit` | `SearchProviderCandidate` |
| `Registry` | `provider.go` | `Register, Get, MustRegister, FactoryIDs` | `PROVIDER_META` + `instanceCache` |
| `Chain` | `provider.go` | `Search(ctx, req)` | `executeSearch` 状态机 |
| `Failure` | `provider.go` | `ProviderID, ProviderLabel, Err` | `failures[]` |
| `ClassifyHTTPError` | `provider.go` | `func(providerID, status, body) *ProviderError` | `classifyProviderHttpError` |
| `FormatFailure` / `FormatFailures` | `provider.go` | `func(Failure) string` | `formatSearchProviderFailure/Failures` |
| `Default` | `provider.go` | `*Registry`（包级单例） | `PROVIDER_META` 单例 |
| `ErrNoProviderConfigured` | `provider.go` | sentinel error | `"No web search provider configured."` 文案 |
| `ErrAllProvidersFailed` | `provider.go` | sentinel error | `"All web search providers failed"` 文案 |

---

## 十二、未确认细节 / 待澄清事项

> [!warning] 调研中以下点无法从源码 100% 确认：

1. **`PREVIEW_LIMITS.COLLAPSED_ITEMS` 的具体数值** —— 在 `render.ts` 中 `import { ... PREVIEW_LIMITS ... } from "../../tools/render-utils"`，但 `render-utils.ts` 未在调研范围。需要从 `tools/render-utils.ts` 查找。
2. **`SearchProvider` 的 `setExcludedSearchProviders` / `setSearchProviderOrder` 是否在 HTTP/RPC 层暴露** —— 调研范围内只见 settings 写入，未见 IPC 接口（如果有 server-mode）。
3. **`authStorage.discoverAuthStorage()` 的具体实现** —— `runSearchQuery` 调用了它但未展开。涉及 SQLite 单例 + ENV 覆盖逻辑。
4. **`MAX_WEB_SEARCH_TIMEOUT_SECONDS = 300` 的 clamp 位置** —— 在 `index.ts` 中 `timeoutMs = Math.ceil(Math.min(configuredSeconds, MAX_WEB_SEARCH_TIMEOUT_SECONDS) * 1_000)`，但 Go 移植时是否要保留 5 分钟上限，取决于 mocode 业务约束。
5. **`throwIfAborted` 在 stream 流式响应里的处理** —— 当前调研只覆盖 `index.ts` 的非流式入口。流式 web search 走 `stream.ts`，其内部可能有自己的 AbortSignal 处理。
6. **`PublicWebProvider` 在 auto chain 里被排除的具体原因** —— 注释说「避免与单引擎重复」，但未量化性能成本。
7. **`cacheAgeSeconds` 的 `dateToAgeSeconds` 解析容错** —— `Number.isNaN(date.getTime())` 后返回 `undefined`，但 `publishedDate` 是 ISO 字符串还是相对时间字符串（"2d ago"）未在源码说明。
8. **`analyze *formatSearchProviderFailure` 对 `zia` provider 的特殊分支是否仍生效** —— 代码里只看到 `zai` 的特殊处理，没有 `zia`。
9. **`WebFetchToolName` 和 `WebSearchToolName` 常量是否与 oh-my-pi 命名一致** —— mocode 已定义常量 `WebSearchToolName = "web_search"`，与 oh-my-pi 对齐，OK。

---

## 十三、相关笔记

- [[02-web-search-provider-chain]] — 概览版（含 22 个 provider 表格对比）
- [[01-grep-tool-architecture]] — 类似的 provider 链模式在 grep 工具里的应用
- [[zai-mcp-deep-dive]] — zai provider 的 MCP 接入方式

## 引用源文件

- `tmp/oh-my-pi/packages/coding-agent/src/web/search/provider.ts`
- `tmp/oh-my-pi/packages/coding-agent/src/web/search/index.ts`
- `tmp/oh-my-pi/packages/coding-agent/src/web/search/types.ts`
- `tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/base.ts`
- `tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/utils.ts`
- `tmp/oh-my-pi/packages/coding-agent/src/web/search/render.ts`
- `tmp/oh-my-pi/packages/ai/src/auth-retry.ts`（withAuth / ApiKeyResolver）
- `tmp/oh-my-pi/packages/ai/src/auth-storage.ts`（AuthStorage.resolver）
- `mocode-agent/internal/core/tools/external/common/net/provider.go`（现有 Provider 接口）
- `mocode-agent/internal/core/tools/external/common/net/types.go`（现有常量）
- `mocode-agent/internal/core/tools/external/common/net/search.go`（现有 SearchDuckDuckGo）
