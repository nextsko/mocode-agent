---
status: active
created: 2026-09-14
updated: 2026-09-14
project: mocode
area: search-tools
topic: search-provider-absorption
goal: 将 oh-my-pi 的 Web Search Provider 链（22 provider + 懒加载 + Fallback + UI 渲染 + Query 解析）按免费优先策略吸收到 mocode（Go）
---

# Web Search Provider 链吸收 — MASTER_PLAN

> [!abstract] 概述
> **目标**：把 oh-my-pi 的 Web Search Provider 架构设计思想（含懒加载、双探测、fallback 链、聚合 fan-out、MCP JSON-RPC、TUI 渲染、StructuredQuery 解析、主题系统）按阶段吸收到 mocode（Go）。
>
> **设计原则**（铁律）：
> - **置顶向下作规划**：从顶层 MASTER 计划逐层拆解到原子任务，每层有明确接口契约
> - **至底向上搞实现**：先实现底层（types、parser、credentials），再实现上层（provider、chain、ui）
> - **向上发展作拼装**：底层通过组合/继承拼装出上层能力（接口 → 注册表 → 链）
> - **逐层验证和仿真**：每层完成先单测 → mock 仿真 → 真实调用三阶段
> - **铁律约束 LLM 超边界**：每个执行计划严格限定 prompt 与文件边界，LLM 不得越界修改未授权文件
>
> **调研来源**：`tmp/oh-my-pi/packages/coding-agent/src/web/search/`（已确认仓库存在，27 文件 + 27 providers）

---

## 1. 背景与动机

mocode 现有的 `netcommon.Provider` 链（GitHub / GitLab / Gerrit）已经验证可行，但 Web Search 是更高频、多形态、需要降级的场景：
- 22 个 provider 中既有付费（Perplexity/Anthropic/Codex/Gemini/xAI）也有免费（Exa Public MCP/Google/DuckDuckGo/Public 聚合）
- 需要按 provider 可用性动态切换（懒加载）
- 需要处理 SSE / JSON-RPC / HTML 爬虫 / OAuth 指纹 / 限流重试等异构协议
- UI 部分需要支持 TUI 树形布局、Markdown 答案、Sources 折叠、Provider 状态可视化

## 2. 调研成果（已沉淀至 docs/plans/search-tools/）

| 报告 | 大小 | 覆盖 |
|---|---|---|
| `02-web-search-provider-chain.md` | 26 KB | 架构总览、22 provider 分类、Registry/lazy-load/fallback 设计 |
| `free-providers-research.md` | 47 KB | 5 免费 provider + utils 工具（DDG/Google/Exa 双路径/Public 聚合/utils） |
| `zai-mcp-deep-dive.md` | 38 KB | Z.AI 完整 MCP JSON-RPC 协议实现（10 维度 + Go 骨架） |
| `provider-chain-design.md` | 68 KB | Provider 懒加载 + Fallback 链 + 自定义扩展机制（10 维度 + Go 接口骨架） |
| `ui-tui-rendering.md` | 50 KB | TUI 渲染架构（OutputBlock + TreeList + Markdown） |
| `ui-structured-query.md` | 66 KB | StructuredQuery 解析 + 宽松过滤 + 引擎查询格式化 |
| `ui-theme-system.md` | 38 KB | Theme 系统 + 折叠/展开语义 + 状态可视化 |

**关键发现**：
1. **Exa Public MCP 是「免费王者」**：无 API key 也能调，仅用 `?exaApiKey=` 走 MCP JSON-RPC
2. **MCP JSON-RPC 是统一抽象层**：zai + exa 都用同一协议，mocode 可一次实现、终身复用
3. **双探测语义是关键**：`isAvailable`（cheap，auto chain 用）与 `isExplicitlyAvailable`（明确选择时允许无凭证 fallback）
4. **Public 聚合是兜底方案**：5 引擎 consensus 排序，比单爬虫稳定性高 5 倍
5. **TUI 设计三件套**：OutputBlock（带边框容器）+ TreeList（折叠树形）+ Markdown（答案渲染）
6. **StructuredQuery 三段式**：结构化解析 → 引擎查询重建 → 宽松过滤（never-return-empty）
7. **Theme 三大原则**：ANSI-aware 宽度计算、state→color/icon/border 三映射一致、time 人性化分桶

## 3. 吸收阶段（子计划）

### Phase 1 — Provider 链骨架（基础，必须先做）

| 子计划 | 目标 | 工期估 | 优先级 | 执行模型 |
|---|---|---|---|---|
| `plans/06-provider-chain-skeleton.md` | `ProviderV2` 接口 + `Registry` 懒加载 + `Chain` fallback + `CredentialBroker` + 错误聚合器 | 3 天 | P0 | **sonnet** |
| `plans/07-credential-broker-design.md` | env vs authStorage 优先级 + 单飞刷新 + force-refresh | 1 天 | P0 | **sonnet** |

### Phase 2 — 免费 Provider（最高 ROI）

| 子计划 | 目标 | 工期估 | 优先级 | 执行模型 |
|---|---|---|---|---|
| `plans/08-exa-public-mcp.md` | Exa MCP JSON-RPC 无 key fallback，含 SSE 复用 | 1.5 天 | P1 | **sonnet** |
| `plans/09-duckduckgo-scraper.md` | DDG POST form + vqd 提取 + goquery HTML 解析 | 2 天 | P2 | **haiku** |
| `plans/10-public-aggregate.md` | 5 引擎 fan-out + soft/hard deadline + consensus 排序 | 2 天 | P2 | **sonnet** |

### Phase 3 — MCP 抽象层（一次实现，多 Provider 复用）

| 子计划 | 目标 | 工期估 | 优先级 | 执行模型 |
|---|---|---|---|---|
| `plans/11-zai-mcp-integration.md` | Z.AI MCP JSON-RPC 完整实现，复用 Exa MCP client | 2 天 | P1 | **sonnet** |

### Phase 4 — UI 子系统（TUI 渲染 + Query 解析 + 主题）

| 子计划 | 目标 | 工期估 | 优先级 | 执行模型 |
|---|---|---|---|---|
| `plans/13-structured-query-parser.md` | StructuredQuery 解析 + formatQuery + applyQueryConstraints（Google-style 指令） | 2 天 | P1 | **sonnet** |
| `plans/14-theme-system.md` | Theme 接口 + DefaultTheme + 状态颜色 + 折叠语义 + OSC 8 链接 | 1.5 天 | P1 | **haiku** |
| `plans/15-tui-rendering.md` | OutputBlock + TreeList + Markdown + WebSearch Renderer 入口 | 2.5 天 | P1 | **sonnet** |

### Phase 5 — 增强与运维

| 子计划 | 目标 | 工期估 | 优先级 | 执行模型 |
|---|---|---|---|---|
| `plans/12-search-testing-observability.md` | e2e 测试 + provider 性能 metrics + 错误分类告警 | 2 天 | P3 | **haiku** |

**总工期估**：18 个工作日（Phase 1-5 含 UI）

## 4. 并行约定

- **Phase 1 内**：`06-skeleton` 必须先完成（基础），`07-credential-broker` 可与 `06` 并行（接口已锁定）
- **Phase 2 内**：`08-exa-mcp` 可与 `09-duckduckgo` 并行（独立 provider）
- **Phase 2→3**：`10-public-aggregate` 依赖 `01` 完成（MCP SSE 复用），可在 Phase 2 内并行 `08/09` 后做
- **Phase 3**：`11-zai-mcp` 必须等 Phase 1 + Phase 2 中 Exa MCP 完成
- **Phase 4**：`13-parser` 可与 `14-theme` 并行；`15-renderer` 依赖前两者
- **Phase 4 可与 Phase 3 重叠**：UI 子系统不依赖具体 Provider 实现，只依赖 SearchResponse DTO

每组最多 3 个子任务并行（避免 review/合并冲突）。

## 5. 验收门禁

每个子计划完成的硬性条件：
1. ✅ 实现代码编译通过（`go build ./...`）
2. ✅ 单测覆盖率 ≥ 70%
3. ✅ e2e 测试：真实调用 provider 至少 1 次（CI 用 mock，生产用真实）
4. ✅ 文档：在 `mocode-agent/docs/architecture/` 或 `docs/guides/` 写使用示例
5. ✅ 知识沉淀：在 `.cursor/knowledge/patterns/` 或 `research/` 写经验总结

MASTER_PLAN 完成标准：所有 10 个子计划 ✅ + 整体 e2e（mocode 启动后能调用 Web Search 工具 + TUI 渲染）+ 至少 3 个免费 provider 实际跑通。

## 6. 铁律：LLM 执行边界约束

> [!warning] 铁律
> 每个子计划执行时，**必须**在 prompt 中包含以下硬约束：
> 1. **只读白名单**：LLM 只能读 `pkg/search/`、`pkg/mcpclient/` 已有文件
> 2. **只写白名单**：LLM 只能创建/修改 plan 中列出的文件路径
> 3. **禁止越界**：不得修改 `internal/auth/`、`internal/netcommon/` 等已有模块
> 4. **禁止依赖未实现**：不得 import Phase 后续阶段的代码
> 5. **必须验证**：每次完成必须跑 `go build ./...` 和 `go test ./...`

## 7. 不在本 MASTER_PLAN 范围

- 付费 provider（Perplexity/Anthropic/Codex/Gemini/xAI）— 详见独立付费 provider 调研报告 `.cursor/knowledge/research/01-web-search-provider-deep-dive.md`
- Provider 内 NLP 增强（query 改写、相关性 rerank）— 属于上层 search service 范畴
- 浏览器渲染（headless puppeteer）— mocode 已有 rod 集成，按需引入

## 8. 进度追踪

| 阶段 | 状态 | 完成日期 | 备注 |
|---|---|---|---|
| 调研 | ✅ done | 2026-09-14 | 7 份报告已沉淀（含 UI） |
| Phase 1 skeleton | 🔴 pending | — | 接口骨架 |
| Phase 1 credential broker | 🔴 pending | — | 凭证 broker |
| Phase 2 exa mcp | 🔴 pending | — | 免费王者 |
| Phase 2 ddg | 🔴 pending | — | HTML 爬虫 |
| Phase 2 public aggregate | 🔴 pending | — | 聚合兜底 |
| Phase 3 zai mcp | 🔴 pending | — | MCP 复用 |
| Phase 4 query parser | 🔴 pending | — | UI 子系统前置 |
| Phase 4 theme | 🔴 pending | — | UI 子系统前置 |
| Phase 4 tui renderer | 🔴 pending | — | UI 完整 |
| Phase 5 testing | 🔴 pending | — | 验收门禁 |
| MASTER 完成 | 🔴 pending | — | 整体验收 |

## 9. 相关笔记

- [[02-web-search-provider-chain]] — 架构总览
- [[free-providers-research]] — 免费 provider 深度调研
- [[zai-mcp-deep-dive]] — Z.AI MCP JSON-RPC 深度
- [[provider-chain-design]] — Provider 懒加载 + Fallback 设计
- [[ui-tui-rendering]] — TUI 渲染架构
- [[ui-structured-query]] — StructuredQuery 解析
- [[ui-theme-system]] — Theme 系统
- [[../.cursor/knowledge/research/01-web-search-provider-deep-dive\|01-web-search-provider-deep-dive]] — 付费 provider 深度调研

## 10. 同主题其他子计划

- [[06-provider-chain-skeleton]] — Phase 1 骨架
- [[07-credential-broker-design]] — Phase 1 凭证 broker
- [[08-exa-public-mcp]] — Phase 2 Exa 免费王者
- [[09-duckduckgo-scraper]] — Phase 2 DDG 爬虫
- [[10-public-aggregate]] — Phase 2 聚合兜底
- [[11-zai-mcp-integration]] — Phase 3 Z.AI MCP
- [[12-search-testing-observability]] — Phase 5 测试与可观测性
- [[13-structured-query-parser]] — Phase 4 Query 解析
- [[14-theme-system]] — Phase 4 主题
- [[15-tui-rendering]] — Phase 4 TUI 渲染