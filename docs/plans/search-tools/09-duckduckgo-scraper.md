# 子计划 09 — DuckDuckGo Scraper

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：06-provider-chain-skeleton / 07-credential-broker
> **后续被依赖**：10（聚合 fan-out）

---

## 目标

实现 DuckDuckGo HTML 爬虫 provider（无凭证），作为 Exa MCP 的次选兜底。

## 范围

### 必须实现

1. **POST form 请求**
   - Endpoint: `POST https://html.duckduckgo.com/html/`
   - Body: `q={query}`（form-encoded）
   - 关键 header：`Accept: text/html`, `User-Agent` Chrome 指纹

2. **vqd 提取（分页）**
   - 首次返回的 HTML 中提取 `vqd={value}` token
   - 后续分页：`POST ?q=...&s=30&dc=30&vqd={token}`

3. **HTML 解析**
   - 用 `github.com/PuerkitoBio/goquery`（Go 等价于 cheerio/linkedom）
   - 结果容器：`.result__a`（title）、`.result__url`（url）、`.result__snippet`（snippet）
   - 去 anomaly-modal：如果响应包含 `.anomaly-modal` class → 报错

4. **降级与重试**
   - bot challenge 检测 → 标记 provider 暂时不可用（cooldown 5 min）
   - 不做 IP 轮换，遵循 DuckDuckGo ToS

5. **字段映射**

| DDG 字段 | SearchSource 字段 |
|---|---|
| `.result__a` text | `Title` |
| `.result__a` href (redirect 解码) | `URL` |
| `.result__snippet` text | `Snippet` |

## 文件清单

```
pkg/search/providers/
├── duckduckgo/
│   ├── duckduckgo.go        # DDGProvider 主体
│   ├── vqd.go               # vqd token 提取与分页
│   ├── parser.go            # goquery 解析
│   ├── challenge.go         # anomaly-modal 检测
│   └── tests/
│       └── duckduckgo_test.go
```

## 验收标准

- [ ] 真实调用 DDG 1 次成功（mock 单测覆盖核心解析逻辑）
- [ ] vqd 分页 token 提取正确
- [ ] anomaly-modal 检测 → cooldown 5 min
- [ ] User-Agent 伪装 Chrome 指纹

## 工期估

2 个工作日

## 风险

- **DDG bot 检测**：高频访问会触发 challenge，需有 cooldown 机制
- **HTML 结构变化**：DDG 不定期改版，需在解析失败时记录原始 HTML 便于排查
- **redirect URL 解码**：DDG 的 result URL 是 `//duckduckgo.com/l/?uddg=...` 格式，需要 `URL.Query().Get("uddg")` 解码

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：先设计 cooldown 状态机（per-provider cooldown map）→ vqd 提取 → HTML 解析
> 2. **至底向上搞实现**：`vqd.go`（token 提取）→ `parser.go`（goquery 解析）→ `challenge.go`（anomaly-modal 检测）→ `duckduckgo.go`（Provider 主类）
> 3. **向上发展作拼装**：Provider 通过组合 `vqd` + `parser` + `cooldown` 模块拼装
> 4. **逐层验证和仿真**：vqd 提取 regex 单测 → parser 解析 fixture HTML 单测 → challenge 检测单测 → DDG 真实调用 1 次
> 5. **铁律约束 LLM 超边界**：只允许创建 `pkg/search/providers/duckduckgo/` 目录下文件；禁止修改 `pkg/search/query/`（Phase 4 parser 已就绪时复用）

## 执行模型（推荐）

- **主执行模型**：**Haiku**（HTML 爬虫逻辑相对简单，goquery 模式标准化）
- **辅助模型**：无

## 相关笔记

- [[free-providers-research]] §1 DuckDuckGo
- [[MASTER_PLAN]]