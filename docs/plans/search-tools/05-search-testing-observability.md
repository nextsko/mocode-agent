# 搜索工具测试与可观测性设计深度研究

> 基于 `tmp/oh-my-pi` 代码库的搜索相关实现、测试与分析脚本。

---

## 1. 测试覆盖

### 1.1 测试金字塔

```
          ┌─────────────┐
          │  集成/E2E    │  ← 真实 provider 调用 (web-search-*.test.ts)
          ├─────────────┤
          │  单元测试    │  ← renderer / path-utils / provider 逻辑
          ├─────────────┤
          │  原生/Rust   │  ← pi-uu-grep, pi-walker 性能与并行
          └─────────────┘
```

### 1.2 TypeScript 层：grep 相关测试

`packages/coding-agent/test/tools/` 下包含 4 个 grep 相关测试文件：

| 文件 | 覆盖重点 |
|---|---|
| `grep-renderer.test.ts` | 渲染器缩进、截断状态、超展开边界、文件/行超链接 |
| `grep-path-lists.test.ts` | 分号/逗号/空格分隔路径、JSON 数组路径、缺失路径容错、hashline 快照、空格路径、引号路径、绝对路径转相对、ast_grep/ast_edit/glob 联动 |
| `multi-grep-path.test.ts` | 默认 cwd、空路径数组、跨无关文件系统树 (`/tmp` + `/var/tmp`)、共同祖先回退、`.git/config` 显式目标、重叠去重 |
| `grep-internal-urls.test.ts` | `skill://`、`artifact://`、`virtual://`、`local://`、`omp://` 解析；selector 校验；大文件分块 native RE2；上下文行去重；RE2 内联标志 |

**关键设计**：
- 使用 `createTools(createTestSession(cwd))` 构建完整 tool runtime，不做 provider mock。
- `fakeAuthStorage` + `FetchImpl` mock 用于 web search provider 单测。
- `vi.spyOn` 用于 SSH capability / file-transfer 的 mock。

### 1.3 Rust 原生测试

`crates/pi-natives/tests/` **不存在**；Rust 测试分布在：

|  crate  | 测试位置 | 内容 |
|---|---|---|
| `pi-natives` | `src/lib.rs` `#[cfg(test)]` | Rayon 线程池上限、Windows 提交压力探针 |
| `pi-walker` | `tests/perf.rs` | 忽略的确定性性能 harness（15000 文件 synthetic tree） |
| `pi-walker` | `tests/parallel.rs` | 并行/串行候选集等价、提前终止、sink error、heartbeat、follow-links、深树路径 |
| `pi-shell` | `tests/minimizer_fixtures.rs` | 命令 minimizer fixture 校验 |
| `pi-uu-grep` | 无独立测试文件 | 仅 `Cargo.toml` dev-dependencies |

**grep 原生实现** (`crates/pi-uu-grep/src/lib.rs`)：
- 基于 ripgrep 库（`grep-regex` + `grep-searcher`） + `pi-walker` 目录递归 + `globset` 过滤。
- 入口 `run()` 返回 GNU 风格退出码（0 匹配 / 1 无匹配 / 2 错误）。
- 未暴露独立 `#[test]`；其行为由 TS 层 `grep.test.ts` 间接覆盖。

### 1.4 会话级搜索行为分析脚本

`scripts/session-stats/analyze_search_relevance.py`：

- **数据源**：`~/.omp/stats.db`（由 `sync.py` 从 session JSONL 同步）。
- **核心指标**：
  - `engaged-read`：模型在搜索结果后 `read` 了结果列表中的文件，记录 deepest index。
  - `next-page`：同一 search/grep 调用再次出现且 `skip/offset > 0`。
  - `refined`：在 engage 前发出了不同的 search/grep。
  - `abandoned`：以上皆非。
- **相关性评分**：
  - `deepest_index / n_results`（coverage ratio）
  - `engaged_count`（读取次数）
  - `files_per_result`、`matches_per_file` 与 engagement 的关联。
- **输出**：`search-relevance.png`（6 面板：outcome 占比、deepest index 分布、coverage CDF、result size by outcome、engagement vs files-per-result、engagement vs matches-per-file）。

---

## 2. 可观测性设计

### 2.1 SearchResponse 结构化字段

`packages/coding-agent/src/web/search/types.ts`：

```ts
export interface SearchResponse {
  provider: SearchProviderId | "none";
  answer?: string;
  sources: SearchSource[];
  citations?: SearchCitation[];
  searchQueries?: string[];
  relatedQuestions?: string[];
  usage?: SearchUsage;          // inputTokens, outputTokens, searchRequests, totalTokens
  model?: string;
  requestId?: string;           // 调试链路 ID
  authMode?: string;            // "oauth" | "api_key"
}
```

- **provider / model / requestId**：直接暴露，用于链路追踪与成本归因。
- **usage**：各 provider 自行映射（Anthropic 的 `searchRequests`、Perplexity 的 `totalTokens`、Gemini 的 `usageMetadata`）。
- **authMode**：区分 OAuth / API Key / anonymous，便于排查鉴权失败。

### 2.2 Provider Tracing / Logging

- **无显式 tracing span**：provider 实现中未使用 OpenTelemetry 或结构化 logger。
- **可观测性退化为字段回传**：
  - `requestId`：从 HTTP header（`x-request-id`、`request-id`）或 response body 提取（Brave、Kimi、Firecrawl、Kagi、Exa）。
  - `model`：从响应 JSON 或环境变量（`GEMINI_SEARCH_MODEL`）回传。
  - `usage`：从 provider 的 usage 字段直接映射。
- **浏览器-backed provider**（Google/Ecosia/Mojeek/Startpage/Bing/Yahoo）通过 `browser-page.ts` 的 `SEARCH_HARD_TIMEOUT_MS` 控制单页超时，无额外 trace。

### 2.3 失败归一化：`formatSearchProviderFailure`

`packages/coding-agent/src/web/search/provider.ts`：

```ts
export function formatSearchProviderFailure(error, provider) {
  if (error instanceof SearchProviderError) {
    if (error.provider === "anthropic" && error.status === 404) {
      return "Anthropic web search returned 404 (model or endpoint not found).";
    }
    if (error.status === 401 || error.status === 403) {
      if (error.provider === "zai") return error.message;
      return `${label} authorization failed (${status}). Check API key or base URL.`;
    }
    return error.message;
  }
  if (error instanceof Error) return error.message;
  return `Unknown error from ${provider.label}`;
}
```

- **401/403 → 鉴权失败提示**（Z.AI 保留原始 message）。
- **Anthropic 404 → 模型/端点不存在**。
- **非 `SearchProviderError` → 降级为 `error.message` 或 `Unknown error`**。

### 2.4 会话级搜索行为分析

`analyze_search_relevance.py` 的 **lookahead 窗口**：

1. 提取 `search/grep` 结果的去重路径列表（tree 格式 `# dir` / `## └─ file` 或 flat 格式 `path:14|`）。
2. 向前扫描最多 `LOOKAHEAD = 30` 个 tool call，直到下一个 user message 或 session end。
3. 分类 outcome 并记录 deepest index。

**借鉴点**：
- 将结果文本结构化反解为可索引路径，而非仅分析文本相似度。
- 用后续 tool call 序列（read / search / grep）作为 relevance 的隐式标注，无需人工标注。

---

## 3. 集成测试模式

### 3.1 Mock Provider

- **web-search-public.test.ts**：提供 `fakeAuthStorage` + `FetchImpl`，根据 URL 返回不同 HTML body，直接调用 `searchPublicWeb()`。
- **web-search-gemini.test.ts**：capture request URL/headers/body，返回 SSE `Response`，验证请求序列化与响应解析。
- **grep-internal-urls.test.ts**：`InternalUrlRouter.instance().register()` 注册虚拟 protocol handler，`LocalProtocolHandler.setOverride()` 注入 artifacts dir。

### 3.2 Fallback 测试

| 场景 | 实现方式 |
|---|---|
| 部分引擎失败 | DDG 正常 + Google 返回 challenge → 验证 DDG 结果仍被合并 |
| soft deadline | DDG 60ms 延迟 + softMs=10 → 验证等待首个成功 |
| hard deadline | 所有引擎 hang + hardMs=40 → 验证返回空结果并 abort straggler |
| 全部失败 | DDG + Google 均返回 challenge → 验证抛 `SearchProviderError(503)` |
| 全部排除 | `setExcludedSearchProviders([...all])` → 验证抛 `400` |
| 显式 provider 不可用 | 强制不存在的 provider → 回退到 auto chain |

**Public Web 的 deadline race 实现**：

```ts
const firstSuccess = Promise.withResolvers<void>();
const all = Promise.all(engineIds.map(async (id, index) => {
  try {
    const provider = await getSearchProvider(id);
    responses[index] = await provider.search({ ...params, signal });
    firstSuccess.resolve();
  } catch (error) {
    failures.push({ provider: { id, label: id }, error });
  }
}));

await Promise.race([all, Bun.sleep(softMs)]);
if (!responses.some(Boolean) && failures.length < engineIds.length) {
  await Promise.race([all, firstSuccess.promise, Bun.sleep(hardMs - softMs)]);
}
straggler.abort();
```

### 3.3 超时与取消传播

- **JS → Native**：`grep.ts` 传入 `signal` + `timeoutMs: SEARCH_GREP_TIMEOUT_MS (30_000)`。
- **Provider → fetch**：`signal` 透传到 `fetch()`；abort 时 `throwIfAborted()` 优先抛出 cancellation，避免被吞掉。
- **Public Web**：`AbortSignal.any([withHardTimeout(params.signal), straggler.signal])` 组合取消源。

---

## 4. 性能监控

### 4.1 超时统计

| 组件 | 超时配置 |
|---|---|
| `grep.ts` 原生调用 | `SEARCH_GREP_TIMEOUT_MS = 30_000` |
| `browser-page.ts` 导航 | `SEARCH_HARD_TIMEOUT_MS`（未公开默认值） |
| Public Web soft deadline | `5_000` ms |
| Public Web hard deadline | `30_000` ms |
| Gemini retry budget | `5 * 60 * 1000` ms |
| Mojeek ALTCHA 解决 | `CAPTCHA_SOLVE_TIMEOUT_MS` |

### 4.2 Provider 成功率

- **session-stats `analyze.py`**：按 `ss_tool_calls` + `ss_tool_results` 统计 per-tool calls / results / tokens，可按 `--by h/d/w/m` 分桶。
- **search relevance**：`engaged-read` 占比、`next-page` 占比、`refined` 占比、`abandoned` 占比。
- **edit reliability**：`classify_edit_result()` 按 `success / fail:anchor-stale / fail:no-enclosing-block / ...` 分类，输出 verb/loc shape 的失败率。

### 4.3 结果相关性评分

`analyze_search_relevance.py` 的评分维度：

1. **Engagement Rate**：`engaged-read / total calls`。
2. **Coverage**：`deepest_index / n_results`（p50/p75）。
3. **Read Depth**：`deepest_index` 的 p50/p90。
4. **Shape Correlation**：
   - `files-per-result` bins（1, 2, 3-5, 6-10, 11-20, 21-50, 51+）
   - `max-matches-per-file` bins（1, 2-5, 6-20, 21-100, 100+）
   - 每个 bin 的 engaged % 与 p50 deepest。

---

## 5. 对我们项目的借鉴点

### 5.1 Session Log 设计

- **schema 先行**：`sync.py` 提前定义 `ss_tool_calls / ss_tool_results / ss_edit_calls / ss_edit_sections`，支持增量同步（byte offset + mtime）。
- **字段对齐**：`ss_tool_calls` 同时记录 `model / provider / arg_json`，`ss_tool_results` 记录 `is_error / result_text`，便于后期做 provider 成功率分析。
- **token 批量计算**：`batch_count_tokens` 用 `tiktoken.encode_ordinary_batch` 一次处理，避免逐条 FFI 开销。

### 5.2 Evaluation 系统

- **隐式标注**：不依赖人工打分，而是用后续行为（`read`、`search`、`edit`）作为 relevance signal。
- **lookahead 窗口**：固定 `LOOKAHEAD = 30` calls，避免无限扫描，同时覆盖绝大多数 follow-up。
- **多级分类**：`engaged-read > next-page > refined > abandoned` 形成漏斗，可定位"结果不相关"还是"结果未被展示"。

### 5.3 搜索工具可观测性增强建议

| 建议 | 依据 |
|---|---|
| 在 `SearchResponse` 强制回传 `requestId / model / usage / authMode` | 现状已做，但部分 provider 未填（如 DuckDuckGo、Bing） |
| 增加 provider 级 latency histogram（start→first-byte） | 当前仅靠外部脚本计时，无内置埋点 |
| 在 `ss_tool_results` 增加 `details.response.provider` 持久化 | 便于按 provider 维度聚合成功率 |
| 对 native grep 增加 `matched_files / skipped_oversized_files / timed_out` 事件 | 当前仅通过 `result_text` 文本推断 |
| 引入 structured trace span（provider.search） | 现状无 span；复杂 fallback 链难以定位慢 provider |

### 5.4 测试模式借鉴

- **provider fallback 的 deadline race**：Public Web 的 `Promise.race([all, Bun.sleep(softMs)])` + straggler abort 是并行 fan-out 的参考实现。
- **browser-backed provider 的 challenge detection**：Google/Ecosia/Mojeek 统一检测 challenge body → `SearchProviderError(429)` → 触发 fallback。
- **grep 的 virtual resource 分块搜索**：`nativeChunkedLineIndexes` 对 >4MiB 内容做 line-boundary 切分，保持 RE2 方言一致性，避免 JS `RegExp` 与 native 语义漂移。

---

## 6. 关键文件索引

```
packages/coding-agent/src/web/search/types.ts          # SearchResponse / SearchProviderError
packages/coding-agent/src/web/search/provider.ts       # 懒加载 registry / fallback 顺序 / formatSearchProviderFailure
packages/coding-agent/src/web/search/index.ts          # WebSearchTool / executeSearch fallback loop
packages/coding-agent/src/web/search/providers/public.ts # 并行 fan-out + deadline race
packages/coding-agent/src/web/search/render.ts         # TUI 渲染 / Metadata 展示
packages/coding-agent/src/tools/grep.ts                # JS grep tool / native 调用 / 虚拟资源搜索
crates/pi-uu-grep/src/lib.rs                           # 原生 ripgrep grep 实现
crates/pi-walker/tests/parallel.rs                     # 并行/串行遍历等价性
scripts/session-stats/sync.py                          # session JSONL → SQLite 增量同步
scripts/session-stats/analyze_search_relevance.py      # 搜索相关性分析 / 6 面板绘图
```
