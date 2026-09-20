# 子计划 13 — StructuredQuery 解析器

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：无（UI 子系统基础）
> **后续被依赖**：15-tui-rendering（Renderer 调用 Parser）/ 各 Provider 的 query 预处理

---

## 目标

实现 mocode Go 版的 StructuredQuery 解析器：Google-style 指令（site/inurl/intitle/filetype/before/after/lang/OR/-）的解析 + 引擎查询重建 + 宽松过滤（never-return-empty）。

## 范围

### 必须实现

1. **StructuredQuery 数据结构**
   ```go
   type StructuredQuery struct {
       Raw                  string
       Text                 string
       Terms                []QueryTerm
       Sites                []string
       ExcludedSites        []string
       InURL                []string
       ExcludedInURL        []string
       InTitle              []string
       ExcludedInTitle      []string
       InText               []string
       ExcludedInText       []string
       Filetypes            []string
       ExcludedFiletypes    []string
       After                *string  // ISO YYYY-MM-DD
       Before               *string
       Lang                 *string
       HasDirectives        bool
       HasConstraints       bool
   }
   ```

2. **tokenize 算法**
   - Unicode 空白识别（NBSP）
   - 引号配对（中英文双引号 `"` `\u201c` `\u201d`）
   - `intitle:"a b"` 内联引号吞入
   - 圆括号分离（leading/trailing）

3. **DIRECTIVE_FIELDS 映射**
   - site/domain/host → sites
   - inurl/url → inUrl
   - intitle/title → inTitle
   - intext/inbody/inanchor → inText
   - filetype/ext → filetypes
   - before/until → before
   - after/since → after
   - lang/language → lang

4. **ALL_MODES 捕获**
   - allintitle / allinurl / allintext
   - inline value + 后续所有 term 归入对应字段

5. **parseDateValue 多格式**
   - `YYYY` / `YYYY-MM` / `YYYY-MM-DD`（分隔符 `-` `/` `.`）
   - `MM/DD/YYYY`（day-first 推断）
   - bare year/month → first day

6. **normalizeSite**
   - lowercase + strip scheme + strip `*.` + strip trailing `/` 或 `.`

7. **parseSearchQuery 主循环**
   - 状态机：negateNext / orPending / lastWasTerm / allMode / groupSeq
   - 未知 directive 降级为 term（不丢）
   - lenient：`name:value` 无法解析则保留原文

8. **formatQuery 引擎重建**
   - `QuerySyntax{phrases, negation, or, site, inUrl, inTitle, inText, filetype, dateRange}` 能力声明
   - `GOOGLE_QUERY_SYNTAX`（全语法）+ `LLM_QUERY_SYNTAX`（极简）
   - **Never returns empty** 降级链：formatQuery → constraint values → raw

9. **applyQueryConstraints 宽松过滤**
   - per-source `matchesQueryConstraints` 严格模式
   - 整批 `applyQueryConstraints` 宽松模式（dimension 零匹配 → drop & report）
   - `sourceTime` 解析（ageSeconds / ISO / 相对 "2d ago"）
   - `matchesSite`（host 等价 + 子域匹配 + path 前缀）
   - `matchesFiletype`（URL pathname 末尾 `.ext`）

## 文件清单

```
pkg/search/query/
├── types.go            # StructuredQuery / QueryTerm / QuerySyntax
├── patterns.go         # DIRECTIVE_PATTERN / ALL_MODES / DIRECTIVE_FIELDS / 日期正则
├── parser.go           # tokenize + parseSearchQuery + parseDateValue + normalizeSite
├── format.go           # formatQuery + GOOGLE_QUERY_SYNTAX + formatScraperQuery
├── filter.go           # applyQueryConstraints + matchesQueryConstraints + matchesSite + matchesFiletype
└── tests/
    ├── parser_test.go  # 17 个 TS test 用例的 Go 1:1
    ├── format_test.go
    └── filter_test.go
```

## 验收标准

- [ ] 17 个 TS test 用例 1:1 移植并通过
- [ ] 「never returns empty」3 级降级验证
- [ ] OR 组 id 跨实例唯一性
- [ ] date exclusive 语义（`before:2024-01-01` 不含当天）
- [ ] 子域匹配（`site:anthropic.com` 命中 `www.anthropic.com`）
- [ ] 路径前缀（`site:github.com/anthropics` 命中 `/anthropics/*`）

## 工期估

2 个工作日

## 风险

- **Go 正则与 TS 正则差异**：尤其是 Unicode 边界（`\s` 包含 NBSP）
- **测试用例覆盖矩阵**：17 个测试需逐个迁移，含边界 case
- **宽松过滤与严格过滤的边界**：业务侧需确认何时用哪种

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：types → patterns → parser → format → filter 五层依赖
> 2. **至底向上搞实现**：types.go → patterns.go → parser.go（tokenize + parse）→ format.go（formatQuery + formatScraperQuery）→ filter.go（applyQueryConstraints + matchesQueryConstraints）
> 3. **向上发展作拼装**：parser 产出 StructuredQuery；format/filter 消费 StructuredQuery；彼此解耦
> 4. **逐层验证和仿真**：types 单测 → patterns 单测 → parser 17 case 1:1 迁移 → format 单测 → filter 单测
> 5. **铁律约束 LLM 超边界**：只允许创建 `pkg/search/query/` 目录下文件；禁止触碰 `pkg/search/providers/`

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（状态机 + 多格式日期解析 + 边界 case 多）
- **辅助模型**：Haiku（生成测试 fixture 数据 — 17 组 Google 复杂指令样例）

## 相关笔记

- [[ui-structured-query]] — 完整 9 维度分析
- [[MASTER_PLAN]]