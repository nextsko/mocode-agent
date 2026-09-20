# oh-my-pi 免费 Web Search Provider 深度调研

> 研究路径：`tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/`
> 目标：让 mocode (Go) 能直接吸收免费/低成本 provider 的实现思路
> 时间戳：2026-09-14
> 调研风格：docs-rulebook 草稿区（review 后决定是否升级到 project-map）

---

## 0. 摘要

oh-my-pi 的 web search 抽象层（`base.ts:SearchProvider`）有 22 个 provider，本文只深入 5 个零凭证或低成本路径：

| Provider | 凭证需求 | 渲染策略 | 数据形态 | 移植难度 |
|---|---|---|---|---|
| DuckDuckGo | 无 | 无 JS HTML POST | 解析 `<div class="result">` 块 | 中（正则脆弱） |
| Google | 无 | fetch + headless Chrome fallback | 解析 `<h3>` + 容器 snippet | 高（headless 必备） |
| Exa | API key 可选 | REST `api.exa.ai` 或 Public MCP `mcp.exa.ai/mcp` | JSON / SSE JSON-RPC | 低（纯 JSON） |
| Public（聚合） | 无 | fan-out 5 个零凭证 engine | merge 共识排序 | 中（并发编排） |
| utils 公共 | — | `withHardTimeout` / `classifyProviderHttpError` / `toSearchSources` / `findCredential` | — | 低 |

**核心洞察**：
1. 零凭证方案都通过 **browser-profile fetch → stealth headless Chrome fallback** 双层策略，在数据中心 IP 上还能坚持一会。
2. Exa Public MCP 是**目前唯一无需任何凭证、能稳定返回高质量 JSON** 的免费路径——其他零凭证方案终究会被 Cloudflare/ALTCHA/anomaly-modal 拦截。
3. `withHardTimeout` 用 `AbortSignal.timeout + AbortSignal.any` 解决了 Bun/WinHTTP 的 TCP stall 阻塞 Esc 键的 bug——Go 用 `context.WithTimeout` 即可，但需要警惕 `net/http` 在 Windows 上的同型问题。

---

## 1. DuckDuckGo Provider（`duckduckgo.ts`，237 行）

### 1.1 认证路径

完全无凭证：

```52:58:tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/duckduckgo.ts
export class DuckDuckGoProvider extends SearchProvider {
  readonly id = "duckduckgo";
  readonly label = "DuckDuckGo";

  isAvailable(_authStorage: AuthStorage): boolean {
    return true;
  }

  override isExplicitlyAvailable(_authStorage: AuthStorage): boolean {
    return true;
  }
```

- `isAvailable = true`：自动链必选。
- 注释明确指出：Instant Answer API (`api.duckduckgo.com`) 对非 Wikipedia/Wolfram 类查询几乎返回空，所以走 HTML 爬虫。

### 1.2 请求结构

**Form POST** 到 `https://html.duckduckgo.com/html/`：

```typescript
{
  q: "...",
  kl: "us-en",  // 区域语言代码（特殊映射表）
  df: "d|w|m|y", // 可选，recency filter
  b: ""          // 模拟真实浏览器表单
}
```

- `Content-Type: application/x-www-form-urlencoded`
- 通过 `browserFetch` 注入 `Accept / Accept-Language / Sec-Ch-Ua / User-Agent` 等 desktop Chrome fingerprint headers（`browser-headers.ts` 的 `buildBrowserNavigationHeaders()`）。
- `Referer: https://html.duckduckgo.com/`
- **区域语言映射**（`localeToKl`）：60+ 条 `DDG_KL_CODES` 允许列表 + 8 个 `DDG_LOCALE_ALIASES`（`en-gb → uk-en`、`zh-tw → tw-tzh` 等），因为 DDG 的 `kl` 是 region-language 而非 BCP47 的 language-region。

### 1.3 渲染策略

**纯 no-JS HTML**（不走 headless browser）。`browserFetch` 在没有 `options.browser` 时只会跑 fetch 路径。

**Query Syntax 子集**：

```typescript
const DDG_QUERY_SYNTAX: QuerySyntax = {
  phrases: true, negation: true, or: true, site: true,
  inUrl: true, inTitle: true, inText: true, filetype: true,
  // 故意不包含 before:/after: — DDG 不解析
};
```

### 1.4 Challenge 检测

```163:165:tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/duckduckgo.ts
function isAnomalyResponse(html: string): boolean {
  return html.includes("anomaly-modal") || html.includes("anomaly.js");
}
```

- 仅依赖**正文字符串**而非状态码，因为 DDG 在 anomaly 模态上混用 200/202。
- 触发后抛 `SearchProviderError(429, "...datacenter/shared-egress IPs...")`，提示用户切换到 Brave/Tavily/Exa/Kagi。

### 1.5 响应解析

三个正则驱动：

```typescript
// 1. 拆结果块（document order）
const blockRe = /<div\b[^>]*\bclass="[^"]*\bresult\b[^"]*"[^>]*>([\s\S]*?)(?=<div\b[^>]*\bclass="[^"]*\bresult\b|...)/g;

// 2. 取标题 href + innerHTML
const titleRe = /<a\b[^>]*\bclass="[^"]*\bresult__a\b[^"]*"[^>]*\bhref="([^"]+)"[^>]*>([\s\S]*?)<\/a>/;

// 3. snippet（a|div|span）
const snippetRe = /<(?:a|div|span)\b[^>]*\bclass="[^"]*\bresult__snippet\b[^"]*"[^>]*>([\s\S]*?)<\/(?:a|div|span)>/;
```

- 块结束符是下一个 `result` 或 `nav-link`，这样天然支持分页串联。
- `decodeHtmlText`：去掉内联 `<b>` 等标签 + 解码 `&amp; &lt; &nbsp; &#39;` + 合并空白。
- `unwrapResultUrl`：处理 DDG 的 `//duckduckgo.com/l/?uddg=<encoded>` 重定向包装（`uddg=` 后是真实 URL），也兼容 protocol-relative 与直接 `http(s)://`。
- `extractPublishedDate`：仅扫描 `result__extras__url` 容器里的 `<span>`，匹配 `YYYY-MM-DD` 或 `YYYY-MM-DDTHH:MM`，避免误抓 `<span class="result__snippet">` 内的日期字面量。
- **分页**：解析隐藏表单 `<input name="s">` + `<input name="vqd">` 自动续页，直到达到 `numResults` 或分页表单消失。

### 1.6 字段映射

```typescript
sources.push({
  title: result.title,
  url: result.url,
  snippet: result.snippet,
  publishedDate: result.publishedDate,    // ISO 字符串
  ageSeconds: dateToAgeSeconds(result.publishedDate),
});
```

`SearchSource` 标准字段：title / url / snippet / publishedDate / ageSeconds / author / requestId（DDG 全无）。

### 1.7 错误处理

- `withHardTimeout` 兜底（默认 60s，参数 `params.timeoutMs` 可调）。
- `classifyProviderHttpError("duckduckgo", status, body)` 检测 credit/quota/401/403。
- 通用 `SearchProviderError(503, "DuckDuckGo HTML error (...)")` 兜底。
- **无显式重试**：失败即抛错，由外层 `resolveProviderCandidates` 链式切换下一个 provider。

### 1.8 Go 移植难度

**评级：中**。

主要障碍：
- **HTML 正则脆弱**：DDG 改版任何 class 命名都得跟着改。Go 用 `golang.org/x/net/html` + CSS-like 选择器（`github.com/PuerkitoBio/goquery`）更稳。
- **分页机制**：vqd token 必须从隐藏表单正确提取，建议用 `golang.org/x/net/html` 走 attribute 解析而不是 regex。
- **decoded URL 的 uddg 处理**：URL 解码 + URL 解析组合，注意 `&amp;` 要先反转义再解码。
- **localeToKl 表**：60+ 条静态数据，直接 copy 即可。

---

## 2. Google Provider（`google.ts`，192 行）

### 2.1 认证路径

完全无凭证：

```typescript
export class GoogleProvider extends SearchProvider {
  readonly id = "google";
  readonly label = "Google";
  isAvailable(_authStorage: AuthStorage): boolean {
    return true;
  }
}
```

### 2.2 请求结构

**GET** `https://www.google.com/search?...`：

```typescript
{
  q: "<scraper 格式化后的 query>",
  num: "10..20",
  hl: "en",
  gl: "us",
  udm: "14",          // Universal search: 14 = web only (压制 image/news/...)
  pws: "0",           // 禁用个性化 (Personalized Search off)
  tbs: "qdr:d|w|m|y"  // 可选，recency
}
```

- 没有 `browserFetch` 的 init（默认 GET），但 `referer: GOOGLE_HOME_URL` + 完整 Chrome fingerprint headers。
- `formatScraperQuery` 把 `site:` / `before:` / `after:` 等 directives 改写为 Google 兼容语法。

### 2.3 渲染策略

**fetch-first + headless-browser fallback**：

```typescript
browser: {
  homeUrl: GOOGLE_HOME_URL,                              // 先访问首页建立 cookie
  ready: { selector: "a h3", timeoutMs: 10_000 },        // 等首个结果出现
  shouldFallback: candidate => blockReason(candidate) !== undefined,
}
```

- `browserFetch`（`browser-page.ts`）先跑 fetch；若 `isSuccessful(2xx)` 且 `shouldFallback === false` 直接返回；否则 escalate 到 Puppeteer stealth browser，**先访问首页 (`homeUrl`) 再 goto 搜索 URL**，等待 `a h3` 选择器出现。

### 2.4 Challenge 检测

```typescript
function blockReason(page: LoadedHtmlPage): "javascript" | "traffic" | undefined {
  if (page.html.includes("/httpservice/retry/enablejs") && !/<h3\b/i.test(page.html)) return "javascript";
  if (
    page.status === 403 ||
    page.status === 429 ||
    page.url.includes("/sorry/") ||
    /unusual traffic|detected unusual traffic|g-recaptcha/i.test(page.html)
  ) {
    return "traffic";
  }
  return undefined;
}
```

- **JS challenge**：URL 含 `/httpservice/retry/enablejs` 且**没有真实结果**（`<h3>` 不存在）—— Google 给 JS 客户端返回 enablejs 重定向页时就是这种模式。
- **Traffic wall**：状态码 403/429、`/sorry/` 路径（Google 的 CAPTCHAs 拦截页）、body 含 `unusual traffic` / `g-recaptcha`。
- 错误信息区分：`"Google blocked the browser search with an automated-traffic challenge"` vs `"Google returned its JavaScript challenge instead of rendered search results."`

### 2.5 响应解析

用 `@oh-my-pi/pi-utils/dom` 的 `parseHTML`（CSS 选择器，类 jsdom）。

```typescript
for (const heading of document.querySelectorAll("h3")) {
  const anchor = heading.closest("a");
  const href = anchor?.getAttribute("href");
  // unwrap /url?q= 重定向
  const url = unwrapResultUrl(href);
  // 找 container 的 snippet
  const snippet = findSnippet(heading);
}
```

- `unwrapResultUrl`：处理 `google.com/url?q=<encoded>` 与 `&url=` 两种跳转格式。
- `findSnippet`：向上找 `.tF2Cxc / .MjjYud / .Gx5Zad` 容器，逐个试 6 个 snippet 选择器：`[data-sncf='1'] .VwiC3b`、`.VwiC3b`、`.IsZvec`、`.BNeawe.s3v9rd` 等，末尾去 `Read more`。
- 解析路径**只看 `<h3>`**——Google 把所有自然结果的标题放在 `<h3>` 里（People Also Ask、Knowledge Panel 不算），这样能巧妙过滤掉周边 noise。

### 2.6 字段映射

```typescript
sources.push({ title: result.title, url: result.url, snippet: result.snippet });
```

无 `publishedDate` / `author`——Google SERP 普通结果不带这些。

### 2.7 错误处理

- 优先用 `withHardTimeout` + `params.timeoutMs`。
- `signal.aborted → SearchProviderError(504)`（timeout 语义）。
- 通用 catch 抛 `SearchProviderError(503)` 兜底。
- `browserFetch` 内的 Puppeteer 路径还会 `await page.close()` 并发 `releaseBrowser()`，且 `page.close()` 有 5s 独立 deadline（`PAGE_CLOSE_TIMEOUT_MS`）避免死锁（issue #8865）。

### 2.8 Go 移植难度

**评级：高**。

主要障碍：
- **headless browser 依赖**：`chromedp` / `playwright-go` / `rod` 至少要选一个。`chromedp` 性能好但 API 偏底层；`playwright-go` 生态最熟但 binary 较大。
- **stealth patches**：Puppeteer 用 `applyStealthPatches`（藏在 `../../../tools/browser/launch`）注入 CDP 脚本绕过 webdriver 检测。Go 这边需要 `go-rod/rod` 的 `Stealth` 或自己手动注入 `navigator.webdriver = undefined` 等。
- **homeUrl → searchUrl 两步导航**：单纯 POST/GET 一次不行，必须先访问首页拿 cookie/redirect state。建议保持两步。
- **JS challenge 自动解题**：Google 现在的 enablejs 页面能靠纯浏览器渲染过，但遇到 reCAPTCHA v3 仍会卡住——Go 端需要接受"卡住就报错降级"，不要尝试 OCR。
- **class 选择器过时**：Google 经常改 class 名（`.tF2Cxc → .MjjYud → .Gx5Zad` 都见过），需要持续维护。

---

## 3. Exa Provider（`exa.ts`，485 行）

### 3.1 认证路径

**双路径自动 fallback**：

```typescript
export class ExaProvider extends SearchProvider {
  isAvailable(authStorage: AuthStorage): boolean {
    if (!this.#settingsAllowSearch()) return false;
    return !!getEnvApiKey("exa") || authStorage.hasAuth("exa");
  }
  override isExplicitlyAvailable(_authStorage: AuthStorage): boolean {
    return this.#settingsAllowSearch();
  }
}
```

- `isAvailable`：检查 `EXA_API_KEY` 环境变量 + AuthStorage 是否有 exa 凭证 → 自动链里只在有 key 时入选。
- `isExplicitlyAvailable`：只检查 settings 开关 `exa.enabled`——**显式选择时无 key 也允许走 Public MCP fallback**。
- `searchExa()` 内：
  1. 先拿 `authStorage.getApiKey("exa", sessionId)` → 有则走 REST + `withAuth`（含 force-refresh-rotate 重试）。
  2. 没有 → `getEnvApiKey("exa")` 再试一次。
  3. 都没有 → 调 `callExaMcpSearch()`（Public MCP fallback）。

### 3.2 请求结构

#### REST 路径（`callExaSearch`）

```
POST https://api.exa.ai/search
Headers:
  Content-Type: application/json
  x-api-key: <API_KEY>
Body:
{
  "query": "<natural language>",
  "numResults": 10,
  "type": "auto",       // auto | neural | fast | deep | keyword
  "contents": {
    "summary": { "query": "<原 query>" }
  },
  "includeDomains": ["..."],   // 可选
  "excludeDomains": ["..."],   // 可选
  "startPublishedDate": "...",  // ISO
  "endPublishedDate": "..."
}
```

- `type` 归一化（`normalizeSearchType`）：`"keyword" → "fast"`，其他原样。
- `directiveParams`：把 parsedQuery 的 `site:` 拆出 hostname（`site:github.com/abc` → `["github.com"]`），`after:/before:` ISO 化。

#### MCP fallback 路径（`callExaMcpSearch`）

```
POST https://mcp.exa.ai/mcp?exaApiKey=<KEY 可选>&tools=web_search_exa
Headers:
  Content-Type: application/json
  Accept: application/json, text/event-stream
  x-exa-source: oh-my-pi
Body: JSON-RPC 2.0
{
  "jsonrpc": "2.0",
  "id": "<随机 36 进制>",
  "method": "tools/call",
  "params": {
    "name": "web_search_exa",
    "arguments": { "query": "...", "numResults": 10 }
  }
}
```

- `exaApiKey` 是 query string 参数——这就是 Public MCP 无 key 也能调用的"公开额度"接入方式。
- 接受 SSE (`text/event-stream`) 或直接 JSON。

### 3.3 渲染策略

**纯 JSON over HTTPS**，无浏览器、无 JS 渲染。

### 3.4 Challenge 检测

无（JSON API）。

- 状态码 429 时区分：MCP 路径提示 `"configure an Exa API key for higher limits"`；REST 路径走 `classifyProviderHttpError` 通用映射。
- 通过 `classifyProviderHttpError("exa", status, body)` 检测 credit/quota/401/403。

### 3.5 响应解析

#### REST 路径

直接 `response.json() as ExaSearchResponse`，结构清晰：

```typescript
interface ExaSearchResponse {
  requestId?: string;
  resolvedSearchType?: string;
  results?: ExaSearchResult[];
  costDollars?: { total: number };
  searchTime?: number;
}

interface ExaSearchResult {
  title?: string | null;
  url?: string | null;
  author?: string | null;
  publishedDate?: string | null;
  text?: string | null;
  highlights?: string[] | null;
  summary?: string | null;
}
```

#### MCP fallback 路径——SSE JSON-RPC 多路复用解包

```typescript
const mcpResponse = parseSSE(await response.text()) as {...};
```

`parseSSE` 在 `mcp/json-rpc.ts`：

```typescript
export function parseSSE(text: string): unknown {
  const lines = text.split("\n");
  for (const line of lines) {
    if (line.startsWith("data: ")) {
      const data = line.slice(6).trim();
      if (data === "[DONE]") continue;
      const result = JSON.parse(data) as unknown;
      if (result) return result;
    }
  }
  return JSON.parse(text);  // fallback: 整段当 JSON
}
```

返回后还要 `normalizeExaMcpPayload()` 处理 Exa MCP 的多形态载荷：

```typescript
// 候选顺序：structuredContent → data → result → content[].text (JSON parse) → 原始 root
```

如果 MCP 返回的不是结构化 JSON 而是**文本块**（Exa 实际就是这样），`parseExaMcpTextPayload` 用正则解析 `Title: / URL: / Author: / Published Date: / Text:` 分段：

```typescript
const sections = textBlocks.join("\n\n").split(/\n{2,}(?=Title:\s*[^\n]*(?:\n(?:URL|Author|Published Date|Text):))/);
```

每段单独匹配每个字段的单行，生成 `ExaSearchResult` 列表。

### 3.6 字段映射

```typescript
sources.push({
  title: result.title ?? result.url,
  url: result.url,
  snippet: (result.summary || result.text || result.highlights?.join(" ") || undefined)?.slice(0, MAX_EXA_SNIPPET_CHARS),
  publishedDate: result.publishedDate ?? undefined,
  ageSeconds: dateToAgeSeconds(...),
  author: result.author ?? undefined,
});
```

`MAX_EXA_SNIPPET_CHARS = 500` 截断。

**额外**：`synthesizeAnswer` 把前 3 个非空 summary 拼成 markdown `**title**: summary`，挂在 `SearchResponse.answer` 上：

```typescript
const MAX_ANSWER_SUMMARIES = 3;
```

### 3.7 错误处理

- REST 路径走 `withAuth(resolver, key => callExaSearch(key, params))`，支持 force-refresh + sibling rotation。
- MCP 路径用 `withHardTimeout`。
- 429 单独提示切换到 REST key 提升额度。
- `mcpResponse.error` 直接抛 `MCP error: ...`。
- `mcpResponse.result?.isError` 时从 `content[]` 找第一条 text 块作为错误信息。

### 3.8 Go 移植难度

**评级：低**。

主要优点：
- **纯 JSON**：用 `encoding/json` 直接反序列化，几乎零成本。
- **无 HTML 解析**：没有 DOM/CSS 选择器依赖。
- **SSE 解析简单**：`strings.Split(text, "\n")` 找 `data:` 前缀行，trim 后 `json.Unmarshal`。
- **公开 MCP 无 key**：`<exabase>/mcp?exaApiKey=<KEY>`（可省）—— 零凭证可用是最大亮点。

移植注意：
- SSE fallback 解析：要先按行找 `data: `，找不到再 `json.Unmarshal(text)` 整段。
- `normalizeExaMcpPayload` 的多重回退逻辑（`structuredContent / data / result / content[].text`）需要实现，因为 Exa MCP 不同部署返回形态不一样。
- `parseExaMcpTextPayload` 的 `Title: / URL: / ...` 段分割正则要保留。

---

## 4. Public（聚合）Provider（`public.ts`，228 行）

### 4.1 认证路径

完全无凭证，但**只能显式选择**：

```typescript
export class PublicWebProvider extends SearchProvider {
  readonly id = "public";
  readonly label = "Public Web";

  isAvailable(_authStorage: AuthStorage): boolean {
    return false;       // auto chain 不入选
  }
  override isExplicitlyAvailable(_authStorage: AuthStorage): boolean {
    return true;        // 显式选择才允许 fan-out
  }
}
```

注释解释："auto chain 已经在串行走单个引擎，显式 fan-out 是用户主动选择"。

### 4.2 子引擎列表

```typescript
const PUBLIC_ENGINE_IDS = [
  "startpage", "google", "duckduckgo", "ecosia", "mojeek",
] as const satisfies readonly SearchProviderId[];
```

**优先级注释**：Google 索引引擎（startpage → google）领头，DDG/Ecosia 中间，Mojeek 独立索引兜底（基于 2026-07 测量）。

**子引擎超时配置**（每个 engine 自己的 `params.timeoutMs`）：默认 60s（`SEARCH_HARD_TIMEOUT_MS`）。

### 4.3 请求结构

**Fan-out 并发调用**所有 5 个子 provider，**共享一个 `AbortSignal`**（straggler controller + 外部 signal + hard timeout 三重组合）：

```typescript
const straggler = new AbortController();
const signal = AbortSignal.any([
  withHardTimeout(params.signal, params.timeoutMs),
  straggler.signal,
]);
```

子 provider 用同一个 SearchParams 调，**不**改写 query（query syntax 各 engine 自理：`formatScraperQuery` 在子 provider 内部用）。

### 4.4 渲染策略

N/A——纯聚合，不直接渲染。子引擎各自走 headless/browser fallback。

### 4.5 Challenge 检测

不主动检测——把子 provider 的 `SearchProviderError` 当成"该引擎失败"收集到 `failures` 数组。

### 4.6 响应解析（共识排序）

#### 软 + 硬双 deadline

```typescript
const SOFT_DEADLINE_MS = 5_000;
const HARD_DEADLINE_MS = 30_000;
```

```typescript
// 阶段 1：race(all, sleep(soft))
await Promise.race([all, Bun.sleep(softMs)]);
// 阶段 2：若 soft 到时仍无成功且还有 engine 在跑 → 等第一个成功或硬超时
if (!responses.some(response => response !== undefined) && failures.length < engineIds.length) {
  await Promise.race([all, firstSuccess.promise, Bun.sleep(Math.max(0, hardMs - softMs))]);
}
// 阶段 3：决出胜负，主动 abort 所有 straggler
straggler.abort();
```

- 软超时：5s 后只要**已有结果**就立刻返回。
- 软超时+无结果：等第一个成功 / 硬超时。
- 硬超时：30s 兜底，防止一个病理慢引擎把请求拖到 60s 上限。
- `firstSuccess.promise`：第一个成功的 provider 立即 resolve，让聚合尽快返回。

#### 共识排序（Deduplication + Merge）

```typescript
interface MergedSource {
  source: SearchSource;
  engines: number;    // 命中该 URL 的 engine 数（主要排序信号）
  bestRank: number;   // 最低 per-engine rank
  order: number;      // 首次插入序（最终 tiebreak）
}
```

dedup key：

```typescript
function dedupKey(rawUrl: string): string {
  const url = new URL(rawUrl);
  const host = url.hostname.toLowerCase().replace(/^www\./, "");
  let path = url.pathname;
  if (path.length > 1 && path.endsWith("/")) path = path.slice(0, -1);
  return `${host}${path}${url.search}`;  // query 保留，fragment 丢弃
}
```

merge 规则：

```typescript
existing.engines += 1;
if (rank < existing.bestRank) {
  existing.bestRank = rank;
  existing.source.title = source.title;   // 用最佳 rank 的标题/URL
  existing.source.url = source.url;
}
if (source.snippet && source.snippet.length > (existing.source.snippet?.length ?? 0)) {
  existing.source.snippet = source.snippet;  // 用最长的 snippet
}
existing.source.publishedDate ??= source.publishedDate;
existing.source.ageSeconds ??= source.ageSeconds;
```

最终排序：

```typescript
.sort((a, b) => b.engines - a.engines || a.bestRank - b.bestRank || a.order - b.order)
```

**降级**：若所有 engine 都失败且 merged 为空：

```typescript
throw new SearchProviderError("public", `All public engines failed: ${formatSearchProviderFailures(failures)}`, 503);
```

### 4.7 字段映射

输出 `SearchResponse { provider: "public", sources: [...] }`，没有 `answer`（聚合不合成）。

### 4.8 错误处理

- 子引擎失败（timeout / challenge）→ 收集到 `failures`，**不**中断其他引擎。
- 全失败才抛 `503`，错误信息含 `formatSearchProviderFailures(failures)`（典型例子："engine timeout"、"challenge" 等）。
- **race 编排**本身无 try-catch——靠 `AbortSignal` 传播取消。

### 4.9 Go 移植难度

**评级：中**。

主要障碍：
- **三信号合并**：Go 用 `context.WithCancel` 自己合成比较绕，建议定义一个 `AggregatedCtx` 结构（cancel func + parent ctx + timeout ctx）。
- **deadline race**：JavaScript 的 `Promise.race([all, sleep])` 在 Go 里要换成 `select { case <-done: ; case <-time.After(softMs): }`。
- **straggler abort**：`straggler.cancel()` 后所有 goroutine 的 ctx.Done() 会立刻触发，但每个子 provider 必须正确把 ctx 传给底层 fetch——Go 的 `net/http` 天然支持 `req.WithContext`。
- **结果合并的并发安全**：`merged` map 多个 goroutine 同时写必须加锁；建议每个子引擎结果先在 goroutine 内归一化成 []MergedSource，再在主 goroutine 里串行 merge。
- **排序确定性**：合并结果按 `(engines desc, bestRank asc, order asc)` 三级排序，Go 直接实现 `sort.Slice` 即可。

---

## 5. utils 公共工具（`utils.ts`，138 行）

> **重要提示**：用户清单里提到的 `readLimitedText`、`fetchWithRetry`、`readSseJson` **在当前 utils.ts 中不存在**。这些函数可能：
> - 在其他文件里（如 `browser-page.ts` 的 `browserFetch` 实现了 fetch + browser retry 的等价逻辑）；
> - 在历史版本里被重构掉；
> - 不在本次研究的 scope 里。
>
> 以下只列**实际存在于 utils.ts** 的导出函数 + 我额外发现的相邻工具。

### 5.1 `withHardTimeout(signal, ms?)` ⭐ 重要

```typescript
export const SEARCH_HARD_TIMEOUT_MS = DEFAULT_WEB_SEARCH_TIMEOUT_SECONDS * 1_000;  // 60_000

export function withHardTimeout(signal: AbortSignal | undefined, ms = SEARCH_HARD_TIMEOUT_MS): AbortSignal {
  const timeout = AbortSignal.timeout(ms);
  return signal ? AbortSignal.any([signal, timeout]) : timeout;
}
```

**实现原理**：
- 永远组合 `AbortSignal.timeout(ms)` + 调用方 signal，任一触发即 abort。
- 解决 Bun/WinHTTP 后端在 Windows 上**忽略 AbortSignal** 的 bug：TCP/TLS stall 时 `fetch()` 不响应 cancellation，导致用户 Esc 键无响应、整个 session 冻结。
- 引用 issue：`oven-sh/bun#15275`、`oven-sh/bun#18536`。
- 默认 60s 容忍 LLM-mediated 慢响应（Anthropic web_search、Perplexity、Gemini、Codex），provider-specific 可调到 5 分钟上限。

**Go 移植**：直接 `context.WithTimeout(parent, ms)` 即可。注意 Go 的 `net/http` 在 Windows 上虽然没 Bun 这个 bug，但 `http.Client.Do(req.WithContext(...))` 配合 cancel 也要验证（建议加 `httptrace`）。

### 5.2 `classifyProviderHttpError(provider, status, body)`

```typescript
const CREDIT_BODY_PATTERN = /credits?\s*(?:exhausted|exceeded)|quota|insufficient/i;

export function classifyProviderHttpError(provider, status, body): SearchProviderError | null {
  if (CREDIT_BODY_PATTERN.test(body)) return new SearchProviderError(provider, `${provider}: credits exhausted`, status);
  if (status === 402) return new SearchProviderError(provider, `${provider}: 402 credits exhausted`, status);
  if (status === 401) return new SearchProviderError(provider, `${provider}: 401 unauthorized`, status);
  if (status === 403) return new SearchProviderError(provider, `${provider}: 403 forbidden`, status);
  return null;
}
```

**判定表**：

| 条件 | 错误消息 |
|---|---|
| body 匹配 `/credits? (exhausted\|exceeded)\|quota\|insufficient/i` | `<provider>: credits exhausted` |
| 状态码 402 | `<provider>: 402 credits exhausted` |
| 状态码 401 | `<provider>: 401 unauthorized` |
| 状态码 403 | `<provider>: 403 forbidden` |
| 其他 | 返回 `null`，调用方用自己的 fallback |

**触发背景**：15.1.7/15.1.8 telemetry 显示用户常遇到 credit-exhaustion、401/402/403 被当成纯 HTTP 错误展示。归一化后让 chain advance 与 summary 都能读懂。

### 5.3 `toSearchSources(sources, numResults)`

```typescript
export function toSearchSources(sources, numResults): SearchSource[] {
  return sources.slice(0, numResults).map(source => ({
    title: source.title,
    url: source.url,
    snippet: source.snippet,
    publishedDate: source.publishedDate,
    ageSeconds: dateToAgeSeconds(source.publishedDate),
  }));
}
```

**功能**：裁剪到 numResults + 注入 `ageSeconds`（如果 publishedDate 是 ISO 字符串，则计算距今秒数）。

实际上 duckduckgo.ts、exa.ts 都自己手动构造 SearchSource 没用这个 helper——它是为新 provider 准备的归一化样板。

### 5.4 `findCredential(storage, envKey, ...storageProviders)`

```typescript
export function findCredential(
  storage: AgentStorage | null | undefined,
  envKey: string | null | undefined,
  ...storageProviders: string[]
): string | null {
  if (envKey) return envKey;             // env 优先
  if (!storage) return null;
  try {
    for (const provider of storageProviders) {
      const records = storage.listAuthCredentials(provider);
      for (const record of records) {
        const credential = record.credential;
        if (credential.type === "api_key" && credential.key.trim().length > 0) return credential.key;
        if (credential.type === "oauth" && credential.access.trim().length > 0) return credential.access;
      }
    }
  } catch {
    return null;
  }
  return null;
}
```

**优先级**：env 变量 → storage 里的 `api_key` → storage 里的 `oauth.access`。

**注意**：此函数出现在 utils.ts 但搜索目录里**没有 provider 实际调用它**——5 个目标 provider 全部走 `params.authStorage`（这是更现代的 broker 管道）。可能 findCredential 是早期 API 的残留，新代码不再用。

### 5.5 `SEARCH_HARD_TIMEOUT_MS` 常量

```typescript
export const SEARCH_HARD_TIMEOUT_MS = DEFAULT_WEB_SEARCH_TIMEOUT_SECONDS * 1_000;  // 60_000
```

仅 `browser-page.ts` 的 `SEARCH_HARD_TIMEOUT_MS = ...` 用了它作 Puppeteer 操作的兜底超时。

### 5.6 缺失的工具函数（用户清单 vs 实际）

| 用户清单 | 是否存在 | 备注 |
|---|---|---|
| `withHardTimeout` | ✅ utils.ts | 见 5.1 |
| `readLimitedText` (MAX_RESPONSE_BYTES/MAX_ERROR_BYTES) | ❌ | **不存在**；可能是其他仓库/历史版本 |
| `fetchWithRetry` | ❌ | **不存在**；`browserFetch` 提供类似语义 |
| `classifyProviderHttpError` | ✅ utils.ts | 见 5.2 |
| `toSearchSources` | ✅ utils.ts | 见 5.3 |
| `findCredential` | ✅ utils.ts | 见 5.4（但 provider 不调用） |
| `withAuth` / `withOAuthAccess` | ⚠️ 不在 utils.ts | 在 `@oh-my-pi/pi-ai/auth-retry` 包，从 `pi-ai` 导出 |
| `readSseJson`、`parseSSE` | ⚠️ 不在 utils.ts | 在 `mcp/json-rpc.ts`：`parseSSE` 只做 `data: <JSON>` 行解析，无 `readSseJson` |

### 5.7 相邻文件的关键工具

#### `browserFetch(url, options)` — `browser-page.ts`

实现 `fetch + headless Chrome fallback`，行为如下：

```typescript
1. 先用 fetchImpl + 随机化 Chrome fingerprint headers 抓 URL
2. 若 fetch 抛错且 options.browser 存在 → escalate 到 browseHtmlPage()
3. 若 2xx 且 shouldFallback(page) === false → 直接返回
4. 否则 escalate 到 browseHtmlPage()
```

`browseHtmlPage()` 内部：
- `acquireBrowser({ kind: "headless" })`（共享浏览器池，registry 在 `tools/browser/registry`）。
- 先 `page.goto(homeUrl)` 建立 cookie。
- 再 `page.goto(searchUrl)`，`waitUntil: "domcontentloaded"`。
- 等待 `ready.selector` 出现（带 `ready.timeoutMs`）。
- `shouldFallback` 判定被 block → 再试一次（`attempts`、`retryDelayMs` 可配）。
- 走 Puppeteer stealth patches（`applyStealthPatches`、`applyViewport`）。

**关键调优点**：
- 每次 Puppeteer await 都包 `untilAborted(signal, () => op())`，防止 daemon 死亡时 `applyViewport` 等调用挂死（issue #8865）。
- `page.close()` 有 5s 独立 deadline，dead CDP session 的 close 会 hang 死。

**Go 移植**：见 §6 的 priority matrix。

#### `parseSSE(text)` — `mcp/json-rpc.ts`

```typescript
export function parseSSE(text: string): unknown {
  const lines = text.split("\n");
  for (const line of lines) {
    if (line.startsWith("data: ")) {
      const data = line.slice(6).trim();
      if (data === "[DONE]") continue;
      const result = JSON.parse(data) as unknown;
      if (result) return result;
    }
  }
  return JSON.parse(text);  // fallback
}
```

只返回**第一个非空 JSON 数据行**，适合单响应 SSE（Exa MCP 当前用法）；**不**处理 streaming multi-event 流（那种需要累积 + event id 跟踪）。`callMCP()` 函数包装了 `parseSSE` 的调用，固定 60s 超时。

#### `withAuth(key, attempt, opts)` — `packages/ai/src/auth-retry.ts`

不在 utils.ts，但在 exa.ts 实际使用。签名：

```typescript
export async function withAuth<T>(
  key: ApiKey | undefined,        // 字符串或 ApiKey resolver
  attempt: (key: string) => Promise<T>,
  opts?: { isAuthError?, signal?, missingKeyMessage? },
): Promise<T>
```

**重试策略**：
1. `key` 是字符串 → 直接 attempt，不重试。
2. `key` 是 resolver → 调 `resolveRetryKey(resolver, false)` 拿 initialKey。
3. attempt 失败若是 auth retryable → 进 while 循环 `resolveNextAuthRetryKey`：
   - 401/auth → 一次 force-refresh-same，然后 sibling rotation。
   - 403/usage-limit → 直接 sibling rotation。
   - typed refresh 请求 → 仅一次 refresh-same，**不**旋转。
4. 重试上限 `AUTH_RETRY_MAX_ATTEMPTS`，非 auth 错误立即抛出。

**Go 移植建议**：直接实现一个简单的 Retryer，`maxAttempts=3`、`isAuthError=is401or403` + 简单的 `rotateAccount` 列表。Go 标准库没有等价物，需要自己写。

---

## 6. Exa Public MCP 重点细节

### 6.1 无 API key 调用方式

```typescript
const query = new URLSearchParams();
const apiKey = findApiKey();           // = $env.EXA_API_KEY
if (apiKey) query.set("exaApiKey", apiKey);
query.set("tools", "web_search_exa");   // 选 tool（不是 tool name！是 URL hint）

const url = `${EXA_MCP_URL}?${query.toString()}`;  // https://mcp.exa.ai/mcp?tools=web_search_exa
```

**关键点**：
- `exaApiKey` 是 URL query 参数，可选——不传也能用 public 配额。
- `tools` 看似指定 tool，实际更像 server 端过滤（同时返回 `tools/list` 与 `tools/call` 都按此过滤）。
- 公共 MCP 对**单 IP 有速率限制**——注释提到 "429 → configure an Exa API key for higher limits"。

### 6.2 SSE JSON-RPC 多路复用实现

请求：

```json
{
  "jsonrpc": "2.0",
  "id": "<random>",
  "method": "tools/call",
  "params": {
    "name": "web_search_exa",
    "arguments": { "query": "...", "numResults": 10 }
  }
}
```

响应（Exa MCP 实际形态）：

```
HTTP/1.1 200 OK
Content-Type: text/event-stream

data: {"jsonrpc":"2.0","id":"...","result":{"content":[{"type":"text","text":"Title: ...\n\nURL: ...\n\nAuthor: ...\n\nPublished Date: ...\n\nText: ..."}]}}
```

但 Exa MCP 实际**有 3-4 种返回形态**，所以有 `normalizeExaMcpPayload()` 的多重回退：

```typescript
function normalizeExaMcpPayload(payload: unknown): unknown {
  const candidates: unknown[] = [];
  const root = asRecord(payload);

  if (root) {
    if (root.structuredContent !== undefined) candidates.push(root.structuredContent);
    if (root.data !== undefined) candidates.push(root.data);
    if (root.result !== undefined) candidates.push(root.result);
    candidates.push(root);

    const content = root.content;
    if (Array.isArray(content)) {
      for (const item of content) {
        const part = asRecord(item);
        if (!part) continue;
        const text = part.text;
        if (typeof text !== "string" || text.trim().length === 0) continue;
        const parsed = parseJsonContent(text);
        if (parsed !== null) candidates.push(parsed);
      }
    }
  } else {
    candidates.push(payload);
  }

  for (const candidate of candidates) {
    if (isSearchResponse(candidate)) return candidate;
  }
  return payload;
}
```

候选顺序：
1. `root.structuredContent`
2. `root.data`
3. `root.result`
4. `root` 本身
5. `root.content[].text` 解析为 JSON 的部分

每个候选过 `isSearchResponse()` 类型守卫（检查 `results / statuses / costDollars / searchTime` 任一字段）。如果都不过，降级到 `parseExaMcpTextPayload()` 按文本格式解析。

### 6.3 REST vs MCP 形态差异

| 维度 | REST `api.exa.ai/search` | MCP `mcp.exa.ai/mcp` |
|---|---|---|
| 鉴权 | `x-api-key` header | `?exaApiKey=` query string（可省） |
| 查询语法 | 结构化：`includeDomains[]`、`startPublishedDate` ISO 等 | inline operators：`site:`、`-site:`、`after:`、`before:` 拼到 query 里 |
| 响应 | 标准 `ExaSearchResponse` JSON | MCP `result.content[]` 文本块，按 `Title:/URL:/...` 分段 |
| 速率 | 凭 key 配额 | 公共配额（无 key 也可） |
| 配置动态性 | 静态参数 | 工具 schema 可动态拉取（`fetchExaTools`） |

REST 的 inline operators 转换：

```typescript
function buildExaMcpArgs(params: ExaSearchParams): Record<string, unknown> {
  const queryParts = [params.query];
  for (const domain of params.include_domains ?? []) queryParts.push(`site:${domain.trim()}`);
  for (const domain of params.exclude_domains ?? []) queryParts.push(`-site:${domain.trim()}`);
  if (params.start_published_date) queryParts.push(`after:${params.start_published_date}`);
  if (params.end_published_date) queryParts.push(`before:${params.end_published_date}`);

  const args: Record<string, unknown> = { query: queryParts.join(" ") };
  if (params.num_results !== undefined) args.numResults = params.num_results;
  return args;
}
```

---

## 7. Public 聚合实现重点

### 7.1 Soft + Hard Deadline + Straggler Abort 编排

**三层退出竞争**：

```
T=0       ─────────── softMs=5s ────────────── hardMs=30s ────
          ↓ race(all, sleep(soft)) ↓
          ├─ all 完成 → 返回
          ├─ soft 到时 → 检查是否有 results
          │     ├─ 有 results → straggler.abort() → 返回
          │     └─ 无 results → race(all, firstSuccess.promise, sleep(hardMs - softMs))
          │           ├─ all 完成 → 返回
          │           ├─ firstSuccess.promise → straggler.abort() → 返回
          │           └─ hardMs 到时 → straggler.abort() → 返回
```

**为什么这么设计**（注释摘录）：
- 软超时：fast HTML engine 通常 < 5s 答完，浏览器引擎（google/ecosia/mojeek）经常超过，但聚合**只要有一个就够用**，所以软到时且有结果就立刻返回。
- 软超时+无结果：保留等待第一个成功的窗口（最多 hardMs），防止空响应——比"全超时才返回"用户体验好。
- 硬超时：30s 兜底，绝不让单 engine 把请求拖到 60s 上限。

**关键并发原语**：
```typescript
const firstSuccess = Promise.withResolvers<void>();
// 每个 engine 的 try 中：
responses[index] = await provider.search(...);
firstSuccess.resolve();  // 立刻通知聚合主循环
```

### 7.2 共识排序算法

```typescript
.sort((a, b) => b.engines - a.engines || a.bestRank - b.bestRank || a.order - b.order)
```

三级排序键：
1. **engines desc**（命中数多者优先）——主要信号
2. **bestRank asc**（最佳 rank 小者优先）——同一命中数时的质量信号
3. **order asc**（首次插入序）——确定性 tiebreak

**merge 时还做的字段维护**：
- title/url 用最佳 rank 那个 engine 的（"best wins"）
- snippet 用最长的（信息量优先）
- publishedDate / ageSeconds 用最先填入的（`??=`）

### 7.3 子引擎超时配置

每个子 engine 自己决定 `params.timeoutMs`，公共聚合**不**覆盖。默认 60s（`SEARCH_HARD_TIMEOUT_MS`）。

子 engine 列表（来自 §4.2）：

| Engine | 头部指纹 | 渲染策略 | recency | 备注 |
|---|---|---|---|---|
| startpage | randomized | GET home → 提取 `sc` token → POST form / 失败 fallback GET | d/w/m/y | homepage token 是关键 |
| google | randomized + stealth fallback | fetch → headless fallback | qdr:d/w/m/y | 双路径 |
| duckduckgo | randomized | POST form（no-JS） | d/w/m/y | 不走 headless |
| ecosia | randomized + stealth fallback | fetch → headless fallback | **忽略**（无 UI 入口） | Cloudflare fronted |
| mojeek | `randomized: false`（稳定 fingerprint） | fetch → headless（含 ALTCHA solve）| since:d/w/m/y | ALTCHA 必需 |

`public.ts` 不需要单独配置——它把 `params` 直接传给每个子 provider。

---

## 8. 移植优先级矩阵

> **难度 × 业务价值** 综合排序。业务价值 = 无凭证可用性 + 排行质量 + 稳定性。

| Provider | 难度 | 业务价值 | 推荐优先级 | 主要障碍 |
|---|---|---|---|---|
| **Exa Public MCP** | ⭐ 低 | ⭐⭐⭐ 高 | **🥇 第一优先** | 仅 SSE 解析（`parseSSE` 简单实现） |
| **Exa REST** | ⭐ 低 | ⭐⭐ 中（有 key 才用） | 🥈 第二 | 需 EXA_API_KEY 才有价值 |
| **DuckDuckGo** | ⭐⭐ 中 | ⭐⭐ 中 | 🥉 第三 | HTML 正则脆弱、分页 vqd token |
| **Public 聚合** | ⭐⭐ 中 | ⭐⭐ 中（但依赖子 engine） | 第四 | 三信号并发编排、共识排序 |
| **Google** | ⭐⭐⭐ 高 | ⭐⭐ 中（同类有 startpage） | 第五 | headless browser + stealth + class 选择器过时 |

### 业务价值详细分析

- **Exa Public MCP**：唯一**零凭证 + 高质量 + 稳定 JSON** 的免费路径——这是 mocode 当前最大的 gap。
- **DuckDuckGo**：post-form 无 JS，但数据中心 IP 上经常被 anomaly-modal 拦截，长期可用性差。
- **Google**：startpage 已经代理 Google 索引且更稳定，自己跑 Google 没太大必要除非想要不同结果集。
- **Public 聚合**：作为兜底 fan-out 价值高，但每个子 engine 都要单独移植/维护，工作量乘以 5。

### 移植顺序建议（最小可用 → 完整对标）

| 阶段 | 内容 | 估时 | 说明 |
|---|---|---|---|
| P0 | Exa Public MCP | 1 天 | 直接补齐零凭证搜索能力 |
| P1 | Exa REST | 0.5 天 | 加 `EXA_API_KEY` 支持 |
| P2 | DuckDuckGo | 1-2 天 | 给 P1 失败时降级 |
| P3 | startpage + mojeek | 2-3 天 | 给聚合铺底 |
| P4 | Public 聚合 | 1 天 | 把 P2/P3 串起来 |
| P5 | ecosia + Google（headless） | 3-5 天 | 需要 chromedp/rod |

---

## 9. Go 生态替代清单

### 9.1 HTTP 客户端

| 用途 | 推荐库 | 备注 |
|---|---|---|
| 基础 HTTP | `net/http` (stdlib) | 99% 场景够用 |
| 高级特性 | [`github.com/imroc/req`](https://github.com/imroc/req) | 类似 Python requests，自动 marshal/unmarshal |
| 重试 + backoff | [`github.com/hashicorp/go-retryablehttp`](https://github.com/hashicorp/go-retryablehttp) | oh-my-pi 没显式重试，靠 provider 链切换；Go 端若想做单 provider 内重试就用这个 |
| SSE 解析 | 自写 `bufio.Scanner` 按 `\n` 读行，匹配 `data: ` 前缀 | Exa MCP SSE 简单，自写 30 行搞定 |

### 9.2 HTML 解析

| 库 | 选择器 | 备注 |
|---|---|---|
| [`github.com/PuerkitoBio/goquery`](https://github.com/PuerkitoBio/goquery) | CSS / jQuery-like | **首选**——和 `@oh-my-pi/pi-utils/dom` 最像 |
| `golang.org/x/net/html` | 走 attribute 解析 | 备用，比 regex 稳但比 goquery 啰嗦 |
| **不要用 regex 解析 HTML** | — | DDG/Ecosia/Mojeek 的解析可以改用 goquery，更稳 |

### 9.3 Headless Browser（Google/Mojeek ALTCHA 必需）

| 库 | 优点 | 缺点 | 推荐度 |
|---|---|---|---|
| [`github.com/go-rod/rod`](https://github.com/go-rod/rod) | 内置 Stealth、自带 launcher、CDP 客户端 | 文档略少 | ⭐⭐⭐ 首选 |
| [`github.com/chromedp/chromedp`](https://github.com/chromedp/chromedp) | 老牌、性能好 | stealth 要自己注入 | ⭐⭐ 次选 |
| [`github.com/playwright-community/playwright-go`](https://github.com/playwright-community/playwright-go) | 多浏览器、API 完整 | binary 大（~100MB） | ⭐⭐ 备选（仅当需 Firefox/WebKit） |

**Stealth patch 对应**：
- `navigator.webdriver` 设为 `undefined`。
- `navigator.languages`、`navigator.plugins` 真实化。
- `WebGL vendor/renderer` 改成真实 GPU。
- rod 的 `Stealth()` plugin 一键搞定。

### 9.4 JSON-RPC / MCP

| 库 | 备注 |
|---|---|
| 自写 JSON-RPC 2.0 envelope | Exa MCP 调用很简单（`{jsonrpc, id, method, params}` ↔ `{jsonrpc, id, result\|error}`），自写 50 行即可 |
| [`github.com/golang-mcp/mcp-go`](https://github.com/golang-mcp/mcp-go) | 完整 MCP 协议库——若以后想当 MCP server 用 |

### 9.5 Context / Timeout / Concurrency

| 模式 | Go 写法 |
|---|---|
| `AbortSignal.timeout(ms)` | `ctx, cancel := context.WithTimeout(parent, ms); defer cancel()` |
| `AbortSignal.any([s1, s2])` | 自封装：`mergeCtx(s1, s2)` 返回首个 cancel 即生效的 ctx |
| `Promise.race([a, sleep])` | `select { case <-a.Done(): ; case <-time.After(d): }` |
| `Promise.withResolvers()` + `firstSuccess.resolve()` | `chan struct{}` + `close(ch)`；或 `sync.Once` + channel |
| Straggler abort | 主 ctx cancel → 所有子 goroutine 的 ctx.Done() 触发 → 用 `defer cancel()` 确保释放 |

### 9.6 完整 mocode 实现路径建议

```
internal/websearch/
├── provider.go              // SearchProvider interface
├── exa/
│   ├── exa.go              // REST 路径
│   ├── exa_mcp.go          // Public MCP + SSE
│   └── exa_mcp_test.go
├── duckduckgo/
│   ├── ddg.go              // POST form + goquery 解析
│   └── ddg_test.go
├── public/
│   ├── public.go           // fan-out 编排
│   ├── consensus.go        // dedup + merge + 排序
│   └── public_test.go
├── util/
│   ├── timeout.go          // mergeCtx + withHardTimeout
│   ├── sse.go              // parseSSE
│   └── classify.go         // classifyProviderHttpError
└── browser/
    ├── rod.go              // rod-based browserFetch
    └── stealth.go
```

---

## 10. 不确定 / 待确认

1. **`findCredential` 是否真的被任何 provider 调用**：grep 显示 5 个目标 provider 全部走 `params.authStorage`，没人用 `findCredential`。可能是历史残留或新代码未跟进。

2. **`readLimitedText` / `fetchWithRetry` / `readSseJson` / `MAX_RESPONSE_BYTES` 这些函数**：在当前 `utils.ts` 中**不存在**。可能在其他仓库（`oh-my-pi` 之外的 monorepo 包）或被重构掉了，需要看 git 历史确认。

3. **DuckDuckGo `parseContinuationForm` 的 s/vqd 命名**：命名规则来自 DDG 内部，不是公开文档。需要靠实测逆向确认。如果 DDG 改版可能失效。

4. **Exa MCP 返回形态的稳定性**：`normalizeExaMcpPayload` 有 5 层 fallback 暗示 Exa MCP 当前不稳定；最好用监控 + alert 检测形态变化。

5. **Mojeek ALTCHA 求解**：`solveCaptcha` 用 `checkbox.click()` + 等待 `<ul.results-standard li>` 出现。这是利用 ALTCHA 是 proof-of-work 的特性——求解在客户端 JS 完成。但 ALTCHA 升级到复杂 captcha 时这个方案会失效。

6. **headless browser registry**：`acquireBrowser` 走的 `tools/browser/registry` 是共享 daemon，**超出本次 scope**。Go 端移植时建议直接用 rod 的 launcher（每个进程一个 browser），放弃共享 daemon 概念。

7. **Google 的 `udm=14` 含义**：实测表明这是 "Web only" 模式（屏蔽 image/news/...）。Google 官方未公开这个值的完整语义，靠逆向。

8. **各引擎的实际可用率**：建议在 mocode 内部跑一周在线样本，统计每个引擎的 200 比例（特别是 DuckDuckGo anomaly-modal 频率）来决定 fallback 优先级。

---

## 相关笔记

- [[docs/plans/search-tools/02-web-search-provider-chain.md]] — 已有 web search chain 概览（Provider Registry / Base Interface / SearchParams 契约）
- [[tmp/oh-my-pi/packages/coding-agent/src/web/search/providers/]] — 全部 22 个 provider 源码
- [[tmp/oh-my-pi/packages/ai/src/auth-retry.ts]] — `withAuth` / `withOAuthAccess` 实现（不在 utils.ts 但被 exa.ts 用）
