# StructuredQuery 解析 + 引擎重建 + 宽松过滤（oh-my-pi 深度调研）

> [!info] 元信息
> **状态**：调研完成（research-only，未实现）
> **创建日期**：2026-09-14
> **调研范围**：`tmp/oh-my-pi/packages/coding-agent/src/web/search/query.ts`（1110 行 TypeScript + 完整单测）
> **关联使用方**：`web/search/index.ts`（管道入口 `parseSearchQuery → provider.search → applyQueryConstraints`）
> **mocode 用途**：为 mocode Go 版本的「Google-style 指令解析」提供参考实现

---

## 0. 概览

oh-my-pi 把「Agent 随手在 query 里塞的 Google 指令」当作一等公民，**在调用 provider 之前**先解析为结构化对象，再由 provider 各自决定如何把结构化约束映射到 API 参数或引擎查询语法；对结构化约束**无法被原生支持的维度**，管道在拿到结果后做一次**宽松 post-filter**——任何会让结果清零的维度被静默降级并记入 `dropped` notes，永远保证非空输入有非空输出。

三段式管道：

```
parseSearchQuery(raw) → StructuredQuery
        ↓
   provider.search(parsedQuery)  ← 每个 provider 自己把约束映射到 API
        ↓
applyQueryConstraints(response.sources, parsedQuery)  ← 宽松后过滤
        ↓
{ sources, dropped[] }  →  LLM 看见：sources + "Note: no results matched `site:...`; relaxed"
```

---

## 1. StructuredQuery 数据结构

### 1.1 字段表

| 字段 | 类型 | 来源指令 | 规范化 |
|---|---|---|---|
| `raw` | string | — | 原文 verbatim |
| `text` | string | 自由文本 | 移除指令、保留引号/`-`/OR，**可能为空** |
| `terms[]` | `QueryTerm[]` | 自由文本 | 顺序保留，支持 `phrase`/`negated`/`group` |
| `sites` | `string[]` | `site:`/`domain:`/`host:` | 小写 + 剥 scheme + 剥 `*.` + 剥尾 `/.` |
| `excludedSites` | `string[]` | `-site:` 等 | 同上 |
| `inUrl` | `string[]` | `inurl:`/`url:`/`allinurl:`（后续项） | 原样（未小写化） |
| `excludedInUrl` | `string[]` | `-inurl:` | 同上 |
| `inTitle` | `string[]` | `intitle:`/`title:`/`allintitle:` | 原样 |
| `excludedInTitle` | `string[]` | `-intitle:` | 原样 |
| `inText` | `string[]` | `intext:`/`inbody:`/`inanchor:`/`allintext:` | 原样，**只用于 query 重建，不参与 post-filter**（snippet 不全） |
| `excludedInText` | `string[]` | `-intext:` 等 | 同上 |
| `filetypes` | `string[]` | `filetype:`/`ext:` | 小写 + 剥前导 `.` |
| `excludedFiletypes` | `string[]` | `-filetype:` | 同上 |
| `after` | `string?` | `after:`/`since:` | ISO `YYYY-MM-DD` |
| `before` | `string?` | `before:`/`until:` | ISO `YYYY-MM-DD` |
| `lang` | `string?` | `lang:`/`language:` | 小写，如 `en-us` |
| `hasDirectives` | bool | 任意指令或 boolean 算子 | `true` 一旦见到 `site:`/`OR`/`-` 等 |
| `hasConstraints` | bool | 任意可后过滤维度 | `sites / inUrl / inTitle / filetypes / date` 任一非空 |

> [!tip] 设计观察
> - `text` 可能为空（指令-only query），但 `raw` 永远有原文 → `formatQuery` 的「never-empty 降级」有保底
> - `inText` 不进 `hasConstraints`——snippet 不全，无法精确 post-filter；只用于 query 重建
> - 所有 list 默认空数组（而非 `null`）——调用方零 nil 检查

### 1.2 QueryTerm 子结构

```typescript
interface QueryTerm {
  text: string;          // 去掉引号/操作符
  phrase?: boolean;      // "exact" 或 +verbatim
  negated?: boolean;     // -foo / NOT foo
  group?: number;        // OR 组 id，同组是 alternatives
}
```

OR 组有两条**隐式不变式**：
1. 同一 `group` id 的 term 在 `terms[]` 中是**连续段**（`renderTerms` 借此一段一段展开）
2. group id 单调递增（`++groupSeq`），从不复用 → 跨 query 实例不会撞 id

---

## 2. tokenize 算法

```typescript
function tokenize(raw: string): RawToken[] {
  // 三种 token：
  //  1) 空格分隔的裸词（含 intitle:"a b" 这种带冒号-引号的）
  //  2) "..." 或 \u201c...\u201d 引号包裹的整段
  //  3) 后续 splitParens 把前导 ( / 尾随 ) 拆成独立 token
}
```

### 2.1 Unicode 空白

```typescript
const WHITESPACE = /\s/;   // 匹配 \u00A0 (NBSP), \u2003, \u3000 ...
```

Agent 复制粘贴常常带入 NBSP/全角空格，`/\s/` 比 `" "` 鲁棒得多。

### 2.2 中英文双引号

```typescript
function isQuote(ch: string): boolean {
  return ch === '"' || ch === "\u201c" || ch === "\u201d";
}
```

`"a b"`、`"a b"`（U+201C/U+201D，Word/Notion 复制来源）一视同仁。引号内的内容**直接作为 term 文本**（不再分词），但 `buf.trim().length > 0` 才入列——避免空引号污染。

### 2.3 嵌套 `intitle:"a b"` 引号吞入

```typescript
// Bare word; a quote directly after `name:` swallows the quoted span
// into the same token (`intitle:"budget tips"`).
while (i < n && !WHITESPACE.test(raw[i])) {
  if (isQuote(c) && buf.endsWith(":")) {
    let j = i + 1;
    while (j < n && !isQuote(raw[j])) { buf += raw[j]; j++; }
    quotedValue = true;
    i = j + 1;
    continue;
  }
  if (isQuote(c)) break; // foo"bar — stop word, let quote start phrase
  buf += c; i++;
}
```

判定逻辑：**当前一个裸词以 `:` 结尾时，下一个引号是「吞入式」，把整个 `"a b"` 视为裸词值的一部分**；否则引号是「短语起始」，终止当前裸词。

### 2.4 圆括号分离（leading/trailing 配对）

```typescript
function splitParens(tokens: RawToken[]): RawToken[] {
  // 拆前导 ( 到独立 token；尾随 ) 只在「不闭合内部 opener」时拆
  // 目的：让 (react OR vue) 正确分组，但 site:wikipedia.org/Foo_(bar) 保持完整
}
```

关键：尾随 `)` 只有在 body 中存在未闭合的 `(` 时**才不拆**——保证 `Rust_(programming_language)` 这种 URL 段不被误拆。

```typescript
let trailing = 0;
while (text.endsWith(")")) {
  const body = text.slice(0, -1);
  let depth = 0;
  for (const c of body) {
    if (c === "(") depth++;
    else if (c === ")") depth--;
  }
  if (depth > 0) break;   // 内部还有未闭合的 ( → 这是 URL 的一部分
  text = body; trailing++;
}
```

---

## 3. DIRECTIVE_PATTERN 与 DIRECTIVE_FIELDS 映射

### 3.1 识别模式

```typescript
const DIRECTIVE_PATTERN = /^([+-]?)([a-z][a-z-]*):(.*)$/i;
```

三段：可选 `+`/`-` 前缀 + 字母开头的指令名 + `:` + 剩余值。**`[a-z-]*` 允许 `all-intitle` 这种 kebab 形式**（虽然代码里只用全字母别名，但留了扩展空间）。

### 3.2 别名表

```typescript
const DIRECTIVE_FIELDS: Record<string, Field> = {
  // 一对多别名 → 同一字段
  site:     "site",     domain:   "site",   host:    "site",
  inurl:    "inUrl",    url:      "inUrl",
  intitle:  "inTitle",  title:    "inTitle",
  intext:   "inText",   inbody:   "inText", inanchor: "inText",
  filetype: "filetype", ext:      "filetype",
  before:   "before",   until:    "before",          // before/until 同义
  after:    "after",    since:    "after",           // after/since 同义
  lang:     "lang",     language: "lang",
};
```

| 别名簇 | 字段 | 备注 |
|---|---|---|
| `site`/`domain`/`host` | `sites` | 全部共用 `normalizeSite` |
| `inurl`/`url` | `inUrl` | |
| `intitle`/`title` | `inTitle` | |
| `intext`/`inbody`/`inanchor` | `inText` | post-filter 不可用 |
| `filetype`/`ext` | `filetype` | |
| `before`/`until` | `before` | exclusive upper bound |
| `after`/`since` | `after` | inclusive lower bound |
| `lang`/`language` | `lang` | |

> [!tip] 为什么 `before` 是 exclusive 而 `after` 是 inclusive？
> 直觉上「before:2024-01-01」是「早于这一天（不含）」——和 `Range.Before(t)` 一致；`after:2024-01-01` 是「晚于等于这一天」——和 `Range.After(t)` 一致。代码用 `time < afterMs` vs `time >= beforeMs` 验证。

### 3.3 ALL_MODES 捕获

```typescript
const ALL_MODES: Record<string, AllMode> = {
  allintitle: "inTitle",
  allinurl:   "inUrl",
  allintext:  "inText",
};
```

`allintitle:budget planning tips` 等价于 `intitle:budget intitle:planning intitle:tips`——但**在 parseSearchQuery 主循环里以「状态机」实现**：见 §7。

---

## 4. ALL_MODES 状态机（与 §3.2 配合）

```typescript
let allMode: AllMode | undefined;

if (match && allMatch) {
  allMode = allMatch;          // 进入 all* 模式
  q.hasDirectives = true;
  const inline = match[3].trim();
  if (inline) pushConstraint(allMatch, inline, match[1] === "-");
  // 后续所有 term 走 pushTerm()，而 pushTerm 看到 allMode 会转调 pushConstraint
}
```

```typescript
const pushTerm = (text: string, phrase: boolean): void => {
  const negated = negateNext;
  negateNext = false;
  if (allMode && !negated)  { pushConstraint(allMode, text, false); return; }
  if (allMode && negated)   { pushConstraint(allMode, text, true);  return; }
  // ... 否则走 OR-group 逻辑
};
```

例：`allintitle: budget planning tips`
1. `allintitle:` → `allMode = "inTitle"`，inline 无值
2. `budget` → `pushConstraint("inTitle", "budget", false)`
3. `planning` → `pushConstraint("inTitle", "planning", false)`
4. `tips` → `pushConstraint("inTitle", "tips", false)`

结果：`q.inTitle = ["budget", "planning", "tips"]`，`q.text = ""`。

---

## 5. parseDateValue 多种格式

```typescript
export function parseDateValue(value: string): string | undefined {
  const t = value.trim();
  // 形 1: YYYY[-/. MM[-/. DD]]?
  let m = /^(\d{4})(?:[-/.](\d{1,2})(?:[-/.](\d{1,2}))?)?$/.exec(t);
  if (m) return isoDate(Number(m[1]),
                       m[2] ? Number(m[2]) : 1,
                       m[3] ? Number(m[3]) : 1);
  // 形 2: MM[-/.]DD[-/.]YYYY  (day-first 推断)
  m = /^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$/.exec(t);
  if (m) {
    let month = Number(m[1]), day = Number(m[2]);
    if (month > 12 && day <= 12) [month, day] = [day, month];
    return isoDate(Number(m[3]), month, day);
  }
  return undefined;
}
```

### 5.1 支持的输入

| 输入 | 解析结果 | 备注 |
|---|---|---|
| `2024` | `2024-01-01` | bare year → 当年第 1 天 |
| `2024-05` | `2024-05-01` | bare month → 当月第 1 天 |
| `2024-05-15` | `2024-05-15` | ISO |
| `2024/05/15` | `2024-05-15` | `/` 分隔 |
| `2024.05.15` | `2024-05-15` | `.` 分隔 |
| `5/15/2024` | `2024-05-15` | MM/DD 推断 |
| `15/5/2024` | `2024-05-15` | day-first 推断（首段 > 12） |
| `2024-13-01` | `undefined` | 范围校验失败 |
| `soon` | `undefined` | → 降级为 term（见 §7） |

### 5.2 校验

```typescript
function isoDate(year: number, month: number, day: number): string | undefined {
  if (year < 1000 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31) return undefined;
  return `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}
```

只做范围检查，不做「2 月没有 30 日」这种细校验——代价低，价值也低。

---

## 6. normalizeSite

```typescript
function normalizeSite(value: string): string {
  let site = value.trim().toLowerCase();
  site = site.replace(/^[a-z][a-z0-9+.-]*:\/\//, "");  // 剥 scheme（含 data:、file:）
  if (site.startsWith("*.")) site = site.slice(2);       // 剥 *. 通配
  site = site.replace(/[/.]+$/, "");                      // 剥尾 / 或 .
  return site;
}
```

| 输入 | 输出 |
|---|---|
| `HTTPS://Docs.K8s.IO/` | `docs.k8s.io` |
| `*.reddit.com` | `reddit.com` |
| `github.com/anthropics.` | `github.com/anthropics` |
| `https://github.com/anthropics` | `github.com/anthropics` |

注意**保留 `github.com/anthropics` 这种 path**——见 §9.4 `matchesSite` 知道怎么处理 path 前缀。

---

## 7. parseSearchQuery 主循环（状态机）

```typescript
let negateNext = false;       // 累积的 - / NOT
let orPending = false;        // 上一项是 OR 算子
let lastWasTerm = false;      // 上一项是 term（用于 OR 成组）
let groupSeq = 0;             // OR 组 id 分配
let allMode: AllMode | undefined;  // allin* 捕获状态
```

### 7.1 状态转移表

| 当前 token | 状态变化 | 输出 |
|---|---|---|
| `"phrase"` | `negateNext=false`, `orPending=false`, `lastWasTerm=true` | term (phrase) |
| `(` / `)` | — | 丢弃（分组用 splitParens 处理过了） |
| `OR` / `\|` / `\|\|` | `orPending=true`, `hasDirectives=true` | 算子 |
| `AND` / `&&` | `hasDirectives=true` | 算子（默认值） |
| `NOT` / `!` | `negateNext=true`, `hasDirectives=true` | 前缀 |
| `-` | 若 `tokens[idx+1]?.quoted` 则 `negateNext=true` | 单独 `-` |
| `+` | — | 单独 `+` |
| `name:value`（已知字段） | 解析、push 约束 | site/date/lang/inX/filetype |
| `allin*:` | `allMode=inTitle/inUrl/inText` | 后续 term 改走 pushConstraint |
| `-foo` | `negateNext=true` + 把 `foo` 重新当裸词处理 | term (negated) |
| `+foo` | term (phrase)（legacy 强制精确） | term (phrase) |
| 其他裸词 | pushTerm | term |

### 7.2 OR 组 id 分配

```typescript
if (orPending && lastWasTerm) {
  const prev = q.terms[q.terms.length - 1];
  if (prev) {
    prev.group ??= ++groupSeq;     // 关键 ??=：只对首个成员分配 id
    term.group = prev.group;
  }
}
```

`??=` 是关键：**只为 OR 组的第一个成员分配 id**，后续成员继承。所以 `react OR vue OR svelte` 三个 term 都共享同一 group id（首个的）。

### 7.3 未知指令降级为 term

```typescript
if (match && allMatch) { ... }
if (match && field) {
  let value = match[3].trim();
  if (!value) { /* adopt next token */ }
  if (!value) { q.hasDirectives = true; continue; }   // 空指令名仍算 directive
  // ... date 解析失败时：
  case "before": case "after": {
    const iso = parseDateValue(value);
    if (!iso) { pushTerm(tok.text, false); continue; }  // 降级为 term
  }
}
```

**宽松哲学**：错误值（`before:soon`）不丢，降级为 term 让用户原文能搜到。

### 7.4 lenient：未知 name:value 保留为原文

`name:` 必须既不在 `DIRECTIVE_FIELDS` 也不在 `ALL_MODES`，否则**不会进任何 pushConstraint**——直接 fallthrough 到末尾的「普通裸词」分支，原文进 terms。

测试用例：
```
parseSearchQuery("error TS2345: https://example.com/a?b=c C:\\Users\\me")
→ terms = ["error", "TS2345:", "https://example.com/a?b=c", "C:\\Users\\me"]
```

这是**对 Agent 生成 query 最关键的设计**——LLM 经常塞错误码、URL、路径，**任何冒号都吃的话整个 query 就废了**。

### 7.5 完整的 §7 逻辑骨架（精简版）

```
for tok in tokens:
  if tok.quoted:           pushTerm(tok.text, phrase)
  if tok in { "(", ")" }:  continue
  if tok in {OR | | ||}:   orPending=true
  if tok in {AND &&}:      hasDirectives=true
  if tok in {NOT !}:       negateNext=true
  if tok == "-":           if next.quoted: negateNext=true; continue
  if tok == "+":           continue  (legacy +verbatim handled at bare word)
  match = DIRECTIVE_PATTERN.exec(tok.text)
  if match && allMatch:    allMode=allMatch, pushConstraint if inline
  if match && field:
    value = match[3].trim()
    if !value: try adopt next token
    if !value: hasDirectives=true; continue
    switch field:
      before/after: parseDateValue → ISO or fallthrough to term
      lang:         q.lang=value.toLowerCase()
      default:      pushConstraint
    continue
  // 裸词
  if tok starts with "-": negateNext=true, recurse on remaining
    if remaining is directive (except date/lang): pushConstraint(negated)
    else: pushTerm(remaining)
  if tok starts with "+": pushTerm(remaining, phrase)
  pushTerm(tok.text, false)
```

---

## 8. formatQuery 引擎重建

### 8.1 QuerySyntax 能力声明

```typescript
export interface QuerySyntax {
  phrases?:   boolean;   // emit "quoted phrases"
  negation?:  boolean;   // emit -term
  or?:        boolean;   // emit OR
  site?:      boolean;
  inUrl?:     boolean;
  inTitle?:   boolean;
  inText?:    boolean;
  filetype?:  boolean;
  dateRange?: boolean;
}
```

零值（`{}`）= 自然语言 API，**只输出裸关键词**。

### 8.2 GOOGLE_QUERY_SYNTAX 全语法

```typescript
export const GOOGLE_QUERY_SYNTAX: QuerySyntax = {
  phrases: true, negation: true, or: true,
  site: true, inUrl: true, inTitle: true, inText: true,
  filetype: true, dateRange: true,
};
```

适用于：Google / Startpage / Ecosia / Brave / Kagi / Mojeek / SearXNG。

### 8.3 多 site / 多 filetype 的 OR 合并

```typescript
if (syntax.site) {
  if (q.sites.length > 1 && syntax.or)
    parts.push(`(${q.sites.map(s => `site:${s}`).join(" OR ")})`);
  else parts.push(...q.sites.map(s => `site:${s}`));
  // excludedSites 永远平铺（AND 语义）
}
```

多个 `site:` 用 OR 合并，**符合 Google「`site:a OR site:b`」**；而 `-site:` 永远是 AND（不能被「-site:a OR -site:b」理解）。

### 8.4 「Never returns empty」降级链

```typescript
export function formatQuery(q: StructuredQuery, syntax: QuerySyntax = {}): string {
  // 1) 主组装
  let result = parts.join(" ").trim();
  if (!result) {
    // 2) 指令-only 且引擎没指令语法 → 把约束值当关键词
    const fallback = [...q.sites, ...q.inTitle, ...q.inUrl, ...q.inText, ...q.filetypes];
    result = fallback.join(" ").trim();
  }
  return result || q.raw.trim();   // 3) 终极保底：原文
}
```

降级链（以优先级排序）：

```
1. formatQuery 主组装（自由文本 + 引擎支持的指令）
   ↓ 空
2. 指令值当关键词（site 名、filetype、inText 子串）
   ↓ 空
3. raw 原文
   ↓ 空
4. 返回空字符串（query 真为空）
```

测试验证：
```
parseSearchQuery("site:kubernetes.io filetype:yaml")
formatQuery(q, {})           → "kubernetes.io yaml"   ← 走第 2 步
formatQuery(q, {})           // raw = "site:kubernetes.io filetype:yaml"
                              // 但 fallback 命中 site + filetype 值
```

### 8.5 附：formatScraperQuery（hostile 算子降级）

scraper 引擎（DDG/Google/Startpage）有两个零匹配陷阱：
- `site:x.com/path` → 路径会让爬虫匹配 0 条
- `inurl:w` → DDG 整个忽略

`formatScraperQuery` **在格式化前**把这些值降级为裸 term：

```typescript
const demoted = [...parsed.sites.filter(site => site.includes("/")), ...parsed.inUrl];
const downgraded = { ...parsed,
  sites: parsed.sites.filter(site => !site.includes("/")),  // 留 bare domain
  inUrl: [],
  terms: [...parsed.terms, ...demoted.map(text => ({ text }))],
};
return formatQuery(downgraded, syntax);
```

注意**只降级正形式**，不动 `-site:` / `-inurl:`——降级它们会把「排除」反转成「搜索」。

---

## 9. applyQueryConstraints 宽松过滤

### 9.1 per-source 严格 vs 整批 宽松

| 函数 | 行为 | 用法 |
|---|---|---|
| `matchesQueryConstraints(source, q)` | 所有维度必须通过；date 缺失放过 | 单元测试 / 自定义流 |
| `applyQueryConstraints(sources[], q)` | 按维度顺序应用，**每步检查零匹配 → 跳过并报告** | **管道默认** |

```typescript
export function applyQueryConstraints(sources, q): ConstraintFilterResult {
  let current = [...sources];
  const dropped: string[] = [];
  if (current.length === 0) return { sources: current, dropped };
  for (const dim of constraintDimensions(q)) {
    const kept = current.filter(dim.pred);
    if (kept.length > 0) current = kept;   // 关键：> 0 才应用
    else dropped.push(dim.label);          // 0 → 跳过，记录
  }
  return { sources: current, dropped };
}
```

返回 `dropped[]` 喂给 LLM：`"Note: no results matched \`site:nonexistent.example\`; the constraint was relaxed"`。

### 9.2 constraintDimensions 构建

每个有内容的维度生成一个 `{ label, pred }`：

```typescript
{
  sites:          label=`site:a OR site:b`, pred=src => sites.some(s => matchesSite(src.url, s))
  excludedSites:  label=`-site:a -site:b`,  pred=src => !excludedSites.some(...)
  inUrl:          label=`inurl:a inurl:b`,   pred=src => inUrl.every(v => url.includes(v))  // all-must
  excludedInUrl:  label=`-inurl:a`,          pred=src => !excludedInUrl.some(...)
  inTitle:        label=`intitle:a`,         pred=src => inTitle.every(v => title.includes(v))
  excludedInTitle, filetypes, excludedFiletypes: 类比
  date range:     pred=src => { t=sourceTime(src); if !t return true; ... }
}
```

> [!note] all-must vs any-of 不对称
> - `inUrl`/`inTitle` 是 **all-must**（每个子串都出现）
> - `sites`/`filetypes` 是 **any-of**（任一匹配即可）
>
> 这是 Google 原生语义：`inurl:a inurl:b` 要求两段都出现，`site:a OR site:b` 是任一。

### 9.3 sourceTime 解析（三路径）

```typescript
function sourceTime(source: SearchSource): number | undefined {
  // 路径 1: ageSeconds（已预算好的秒数）
  if (typeof source.ageSeconds === "number" && Number.isFinite(source.ageSeconds)) {
    return Date.now() - source.ageSeconds * 1000;
  }
  if (!source.publishedDate) return undefined;
  // 路径 2: 相对时间 "2d ago" / "3 hours ago"
  const rel = RELATIVE_AGE_PATTERN.exec(source.publishedDate.trim());
  if (rel) {
    const seconds = Number(rel[1]) * (RELATIVE_UNIT_SECONDS[rel[2].toLowerCase()] ?? 0);
    return seconds > 0 ? Date.now() - seconds * 1000 : undefined;
  }
  // 路径 3: ISO / 任何 Date.parse 接受的格式
  const parsed = Date.parse(source.publishedDate);
  return Number.isNaN(parsed) ? undefined : parsed;
}
```

支持的相对时间单位：

| 单位 | 秒数 | 别名 |
|---|---|---|
| minute | 60 | `m` / `min` / `minute` |
| hour | 3600 | `h` / `hr` / `hour` |
| day | 86_400 | `d` / `day` |
| week | 604_800 | `w` / `week` |
| month | 2_592_000 | `mo` / `month`（30 天近似） |
| year | 31_536_000 | `y` / `yr` / `year`（365 天近似） |

> **缺日期处理哲学**：没有 date 就放过——「无法证明违规」≠「违规」。这一行是关键：
> ```typescript
> if (time === undefined) return true;  // undated → cannot prove violation
> ```

### 9.4 matchesSite 详解

```typescript
export function matchesSite(url: string, site: string): boolean {
  const parsed = hostAndPath(url);    // { host, path } 或 undefined
  if (!parsed) return false;
  const slash = site.indexOf("/");
  const siteHost = slash === -1 ? site : site.slice(0, slash);
  const sitePath = slash === -1 ? ""   : site.slice(slash);
  // host 等价或子域匹配
  if (parsed.host !== siteHost && !parsed.host.endsWith(`.${siteHost}`)) return false;
  // path 前缀
  if (sitePath && !parsed.path.toLowerCase().startsWith(sitePath.toLowerCase())) return false;
  return true;
}
```

| 规则 | 例子 |
|---|---|
| **完全匹配** | `site=k8s.io` ↔ `k8s.io/docs` ✓ |
| **子域匹配** | `site=k8s.io` ↔ `docs.k8s.io/setup` ✓（`docs.k8s.io` endsWith `.k8s.io`） |
| **非子域反例** | `site=k8s.io` ↔ `notk8s.io/` ✗（endsWith `.k8s.io` 但 `notk8s.io` 不以 `.` 开头被拒绝） |
| **path 前缀** | `site=github.com/anthropics` ↔ `github.com/anthropics/sdk` ✓ |
| **path 失配** | `site=github.com/anthropics` ↔ `github.com/other/sdk` ✗ |

**反例关键**：`notk8s.io` endsWith `.k8s.io` 是 `true`（`.k8s.io` 是 `notk8s.io` 的子串），但前面 `!==` 短路保证了**严格检查**：只有 `host == siteHost` 或 `host endsWith .siteHost`（注意是 `.siteHost` 带前导点）才能通过。

### 9.5 matchesFiletype

```typescript
function matchesFiletype(url: string, ext: string): boolean {
  const parsed = hostAndPath(url);
  if (!parsed) return false;
  return parsed.path.toLowerCase().endsWith(`.${ext}`);
}
```

极简：`https://x.com/foo.pdf` ↔ `ext=pdf` ✓（`/foo.pdf` endsWith `.pdf`）。

---

## 10. 设计原则（7 条必写）

> [!abstract] 7 条核心设计原则
>
> 1. **三段式管道**：parse → provider 映射 → post-filter
>    解析产出 `StructuredQuery`，provider 自己决定怎么把约束映射到原生 API（Perplexity `search_domain_filter`、Exa `include_domains`、Tavily `include_domains` 等），管道层只做兜底 post-filter。**约束知识集中在一处**，provider 各自轻装。
>
> 2. **「Never returns empty」原则**
>    `formatQuery` 三级降级（主组装 → 约束值当关键词 → raw 原文）；`applyQueryConstraints` 维度零匹配跳过而非淘汰；`parseSearchQuery` 错误值降级为 term。**任何一步都不能让 LLM 收到空 result**——空结果对 Agent 来说是死局。
>
> 3. **引擎能力声明（QuerySyntax capability struct）**
>    Provider 不必硬编码「我会哪些指令」，而是声明 `QuerySyntax`，`formatQuery` 据此裁剪输出。LLM-mediated 引擎（`{}`）只发关键词，Google-syntax 引擎发全指令——**同一份 StructuredQuery 服务所有 provider**。
>
> 4. **「Lenient and Report」零匹配降级**
>    任何 post-filter 维度若会让结果清空，**跳过 + 记入 dropped[]**，绝不让「mis-scoped 指令」变「dead search」。dropped 透传给 LLM 作为 `Note:` 前缀，**让模型知道有约束被放弃**，而不是被隐瞒。
>
> 5. **宽松解析（unknown stays verbatim）**
>    `name:` 必须既在 `DIRECTIVE_FIELDS` 又在 `ALL_MODES` 才算指令；**未知的 `name:value`（错误码、URL、文件路径）原样进 terms**。这是对 Agent 生成 query 最关键的鲁棒性——LLM 经常产出 `TS2345:`、`C:\path`、`SHA256:` 这类含冒号的文本。
>
> 6. **结构化优先 + 反向重建（structured-first, format-on-demand）**
>    解析阶段把所有指令拆成 typed 字段（site/inUrl/...），但**始终同时保留 `raw` 原文**作为「不可解析时的最后保底」。重建时按引擎能力选择「emit operator」还是「emit as keyword」——同一份解析可服务 22 个 provider。
>
> 7. **不变式：groups 连续 + host 反向匹配带前导点**
>    OR 组在 `terms[]` 中必须连续（renderTerms 借此段合并），分配用 `prev.group ??= ++groupSeq`；`matchesSite` 检查子域必须 `endsWith(.siteHost)` 带前导点，避免 `notk8s.io` 命中 `k8s.io`。**这些不变式若被破坏，宽松过滤就退化出 bug**。

---

## 11. Go 移植设计

### 11.1 数据结构（pkg/search/query/types.go）

```go
package query

import "time"

// QueryTerm 自由文本 token（Phrase/Negated/Group）
type QueryTerm struct {
    Text    string `json:"text"`
    Phrase  bool   `json:"phrase,omitempty"`
    Negated bool   `json:"negated,omitempty"`
    Group   *int   `json:"group,omitempty"` // 用指针区分「未分组」与「group=0」
}

// StructuredQuery 解析结果
type StructuredQuery struct {
    Raw               string      `json:"raw"`
    Text              string      `json:"text"`
    Terms             []QueryTerm `json:"terms"`
    Sites             []string    `json:"sites,omitempty"`
    ExcludedSites     []string    `json:"excludedSites,omitempty"`
    InURL             []string    `json:"inUrl,omitempty"`
    ExcludedInURL     []string    `json:"excludedInUrl,omitempty"`
    InTitle           []string    `json:"inTitle,omitempty"`
    ExcludedInTitle   []string    `json:"excludedInTitle,omitempty"`
    InText            []string    `json:"inText,omitempty"`
    ExcludedInText    []string    `json:"excludedInText,omitempty"`
    Filetypes         []string    `json:"filetypes,omitempty"`
    ExcludedFiletypes []string    `json:"excludedFiletypes,omitempty"`
    After             *time.Time  `json:"after,omitempty"`   // exclusive-after 用 After.Add(24h)
    Before            *time.Time  `json:"before,omitempty"`  // 用 ISO date string 也可
    AfterISO          string      `json:"afterIso,omitempty"`
    BeforeISO         string      `json:"beforeIso,omitempty"`
    Lang              string      `json:"lang,omitempty"`
    HasDirectives     bool        `json:"hasDirectives"`
    HasConstraints    bool        `json:"hasConstraints"`
}

// QuerySyntax 引擎能力声明
type QuerySyntax struct {
    Phrases   bool `json:"phrases,omitempty"`
    Negation  bool `json:"negation,omitempty"`
    OR        bool `json:"or,omitempty"`
    Site      bool `json:"site,omitempty"`
    InURL     bool `json:"inUrl,omitempty"`
    InTitle   bool `json:"inTitle,omitempty"`
    InText    bool `json:"inText,omitempty"`
    Filetype  bool `json:"filetype,omitempty"`
    DateRange bool `json:"dateRange,omitempty"`
}

// GoogleSyntax 全语法能力
var GoogleSyntax = QuerySyntax{
    Phrases: true, Negation: true, OR: true,
    Site: true, InURL: true, InTitle: true, InText: true,
    Filetype: true, DateRange: true,
}

// ConstraintFilterResult 宽松过滤结果
type ConstraintFilterResult struct {
    Sources []SearchSource `json:"sources"`
    Dropped []string       `json:"dropped"`
}
```

### 11.2 正则模式表

```go
// pkg/search/query/patterns.go
package query

import "regexp"

// 指令识别：可选 +/- 前缀 + 字母开头 kebab 名字 + : + 值
var directivePattern = regexp.MustCompile(`^([+-]?)([a-z][a-z-]*):(.*)$`)

// 智能引号（中英文双引号）
var quotePattern = regexp.MustCompile(`["\u201c\u201d]`)

// 日期形 1：YYYY[-/. MM[-/. DD]]?
var dateISO = regexp.MustCompile(`^(\d{4})(?:[-/.](\d{1,2})(?:[-/.](\d{1,2}))?)?$`)

// 日期形 2：MM[-/.]DD[-/.]YYYY
var dateUS = regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$`)

// 相对时间 "2d ago" / "3 hours ago"
var relativeAge = regexp.MustCompile(`(?i)^(\d+)\s*(minute|min|hour|hr|day|week|month|mo|year|yr|[mhdwy])s?\s+ago$`)

// site 归一化：scheme / 通配 / 尾分隔
var siteScheme    = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://`)
var siteWildcard  = regexp.MustCompile(`^\*\.'`)
var siteTrailing  = regexp.MustCompile(`[/.]+$`)

// 词法边界：whitespace（含 NBSP/全角空格）
// Go 用 unicode.IsSpace 而非 \s，天然支持 NBSP（U+00A0）/ 全角空格（U+3000）
// 因此 Go 的 strings.FieldsFunc 即可

// 指令名映射
var directiveFields = map[string]string{
    "site":     "site", "domain": "site", "host": "site",
    "inurl":    "inUrl", "url": "inUrl",
    "intitle":  "inTitle", "title": "inTitle",
    "intext":   "inText", "inbody": "inText", "inanchor": "inText",
    "filetype": "filetype", "ext": "filetype",
    "before":   "before", "until": "before",
    "after":    "after", "since": "after",
    "lang":     "lang", "language": "lang",
}

var allModes = map[string]string{
    "allintitle": "inTitle",
    "allinurl":   "inUrl",
    "allintext":  "inText",
}
```

### 11.3 DirectedParser 状态机接口

```go
// pkg/search/query/parser.go
package query

type Parser struct {
    // 状态字段
    negateNext  bool
    orPending   bool
    lastWasTerm bool
    groupSeq    int
    allMode     string  // "inTitle" | "inUrl" | "inText" | ""
}

// Parse 主入口：raw → StructuredQuery
func Parse(raw string) StructuredQuery { ... }

// 内部 helpers
func (p *Parser) tokenize(raw string) []rawToken
func (p *Parser) splitParens(tokens []rawToken) []rawToken
func (p *Parser) pushConstraint(q *StructuredQuery, field, value string, negated bool)
func (p *Parser) pushTerm(q *StructuredQuery, text string, phrase bool)

// 时间解析
func ParseDateValue(value string) (time.Time, bool)   // 返回 (date, ok)

// site 归一化
func NormalizeSite(value string) string

// 宽松过滤
func ApplyQueryConstraints(sources []SearchSource, q StructuredQuery) ConstraintFilterResult
func MatchesQueryConstraints(source SearchSource, q StructuredQuery) bool
func MatchesSite(url, site string) bool
func MatchesFiletype(url, ext string) bool

// 引擎重建
func FormatQuery(q StructuredQuery, syntax QuerySyntax) string
```

### 11.4 时间解析的容错策略

| 输入 | Go 行为 | 备注 |
|---|---|---|
| `2024` | 2024-01-01 UTC | bare year |
| `2024-05` | 2024-05-01 UTC | bare month |
| `2024-05-15` | `time.Parse("2006-01-02", ...)` | ISO |
| `5/15/2024` | MM/DD 推断 | day-first 当 `month > 12 && day <= 12` |
| `soon` | `(zero, false)` | 返回 ok=false → 降级为 term |
| `2024-13-01` | 范围校验失败 | `month < 1 \|\| month > 12` |

**容错关键**：解析失败返回 `(time.Time{}, false)`，调用方在 `Parse()` 主循环里走 `pushTerm` 降级。

```go
func ParseDateValue(value string) (time.Time, bool) {
    t := strings.TrimSpace(value)
    if m := dateISO.FindStringSubmatch(t); m != nil {
        y, _ := strconv.Atoi(m[1])
        mo := 1
        if m[2] != "" { mo, _ = strconv.Atoi(m[2]) }
        d := 1
        if m[3] != "" { d, _ = strconv.Atoi(m[3]) }
        if y < 1000 || y > 9999 || mo < 1 || mo > 12 || d < 1 || d > 31 { return time.Time{}, false }
        return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC), true
    }
    if m := dateUS.FindStringSubmatch(t); m != nil {
        mo, _ := strconv.Atoi(m[1])
        d, _ := strconv.Atoi(m[2])
        y, _ := strconv.Atoi(m[3])
        if mo > 12 && d <= 12 { mo, d = d, mo }
        if y < 1000 || y > 9999 || mo < 1 || mo > 12 || d < 1 || d > 31 { return time.Time{}, false }
        return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC), true
    }
    return time.Time{}, false
}
```

### 11.5 测试用例覆盖矩阵

```go
// pkg/search/query/parser_test.go
func TestParseSearchQuery(t *testing.T) {
    cases := []struct {
        name   string
        input  string
        check  func(t *testing.T, q StructuredQuery)
    }{
        // -- 基础 --
        {"plain", "rust async runtime", func(t *testing.T, q StructuredQuery) {
            assert.False(t, q.HasDirectives)
            assert.False(t, q.HasConstraints)
            assert.Equal(t, []string{"rust","async","runtime"}, termsToTexts(q.Terms))
        }},
        // -- 鲁棒性：未知 name:value 保留 --
        {"unknown colon", "error TS2345: https://example.com/a?b=c C:\\Users\\me", func(t *testing.T, q StructuredQuery) {
            assert.False(t, q.HasConstraints)
            assert.Equal(t, []string{"error","TS2345:","https://example.com/a?b=c","C:\\Users\\me"}, termsToTexts(q.Terms))
        }},
        // -- site 别名 + 归一化 --
        {"site aliases", "kubernetes site:HTTPS://Docs.K8s.IO/ domain:cncf.io -site:*.reddit.com host:github.com/kubernetes", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"docs.k8s.io","cncf.io","github.com/kubernetes"}, q.Sites)
            assert.Equal(t, []string{"reddit.com"}, q.ExcludedSites)
        }},
        // -- 空格分隔值 --
        {"adopt next", "site: arxiv.org transformer", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"arxiv.org"}, q.Sites)
            assert.Equal(t, "transformer", q.Text)
        }},
        {"reserved", "site: OR site:example.com", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"example.com"}, q.Sites)
        }},
        // -- inX 变体 --
        {"inX variants", `inurl:docs intitle:"getting started" -intitle:deprecated inbody:websocket handshake`, func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"docs"}, q.InURL)
            assert.Equal(t, []string{"getting started"}, q.InTitle)
            assert.Equal(t, []string{"deprecated"}, q.ExcludedInTitle)
            assert.Equal(t, []string{"websocket"}, q.InText)
        }},
        // -- allintitle 状态机 --
        {"allin*", "allintitle: budget planning tips", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"budget","planning","tips"}, q.InTitle)
            assert.Equal(t, "", q.Text)
        }},
        // -- filetype 归一化 --
        {"filetype norm", "filetype:PDF ext:.xlsx -filetype:doc", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"pdf","xlsx"}, q.Filetypes)
            assert.Equal(t, []string{"doc"}, q.ExcludedFiletypes)
        }},
        // -- date 多种格式 --
        {"date forms", "after:2024 before:2025-06-15", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, "2024-01-01", q.AfterISO)
            assert.Equal(t, "2025-06-15", q.BeforeISO)
        }},
        {"date us", "after:6/15/2024", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, "2024-06-15", q.AfterISO)
        }},
        {"date day-first", "after:15/6/2024", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, "2024-06-15", q.AfterISO)
        }},
        {"date fallback", "before:soon", func(t *testing.T, q StructuredQuery) {
            assert.Nil(t, q.Before)
            assert.Contains(t, termsToTexts(q.Terms), "before:soon")
        }},
        // -- 引号（含智能引号） --
        {"smart quotes", `"exact phrase" \u201csmart quoted\u201d -"not this"`, func(t *testing.T, q StructuredQuery) {
            // phrase=true, phrase=true, phrase=true & negated=true
        }},
        // -- OR 组 --
        {"OR group", "(react OR vue OR svelte) AND hooks", func(t *testing.T, q StructuredQuery) {
            // react, vue, svelte 共享同一 group，hooks 无 group
        }},
        // -- 嵌套 URL 圆括号 --
        {"wiki paren", "site:en.wikipedia.org/wiki/Rust_(programming_language) borrow", func(t *testing.T, q StructuredQuery) {
            assert.Equal(t, []string{"en.wikipedia.org/wiki/rust_(programming_language)"}, q.Sites)
        }},
        // -- 复杂 Google 指令样例（综合）--
        {"complex Google", `release notes site:github.com -site:gist.github.com filetype:md after:2024-05-01 intitle:"v2"`, func(t *testing.T, q StructuredQuery) {
            // 5 个维度同时存在
            assert.Equal(t, []string{"github.com"}, q.Sites)
            assert.Equal(t, []string{"gist.github.com"}, q.ExcludedSites)
            assert.Equal(t, []string{"md"}, q.Filetypes)
            assert.Equal(t, "v2", q.InTitle[0])
        }},
    }
    for _, c := range cases { t.Run(c.name, func(t *testing.T) { c.check(t, Parse(c.input)) }) }
}
```

### 11.6 formatQuery 与 ApplyQueryConstraints 测试

```go
func TestFormatQuery_GoogleSyntax(t *testing.T) {
    q := Parse(`release notes site:github.com -site:gist.github.com filetype:md after:2024-05-01 intitle:"v2"`)
    got := FormatQuery(q, GoogleSyntax)
    want := `release notes site:github.com -site:gist.github.com intitle:v2 filetype:md after:2024-05-01`
    assert.Equal(t, want, got)
}

func TestFormatQuery_OrSites(t *testing.T) {
    q := Parse("cve site:nvd.nist.gov site:mitre.org")
    assert.Equal(t, "cve (site:nvd.nist.gov OR site:mitre.org)", FormatQuery(q, GoogleSyntax))
}

func TestFormatQuery_PlainKeywords(t *testing.T) {
    q := Parse(`(react OR vue) "state management" -redux site:dev.to filetype:pdf`)
    // no syntax → 全部降级为裸词：react vue state management
    assert.Equal(t, "react vue state management", FormatQuery(q, QuerySyntax{}))
}

func TestFormatQuery_DirectiveOnlyFallback(t *testing.T) {
    q := Parse("site:kubernetes.io filetype:yaml")
    // {} → 走第 2 步：约束值当关键词
    assert.Equal(t, "kubernetes.io yaml", FormatQuery(q, QuerySyntax{}))
}

func TestApplyQueryConstraints_RelaxeAndReport(t *testing.T) {
    sources := []SearchSource{
        {URL: "https://kubernetes.io/docs.pdf"},
        {URL: "https://blog.example.com/k8s"},
    }
    q := Parse("install site:nonexistent.example")
    res := ApplyQueryConstraints(sources, q)
    assert.Len(t, res.Sources, 2)             // 全保留
    assert.Equal(t, []string{"site:nonexistent.example"}, res.Dropped)
}

func TestApplyQueryConstraints_DateBoundsUndated(t *testing.T) {
    q := Parse("k8s after:2024-01-01")
    sources := []SearchSource{
        {URL: "https://a.com/1", PublishedDate: "2023-01-01"},  // 已知违规
        {URL: "https://a.com/2"},                              // undated → 放过
    }
    res := ApplyQueryConstraints(sources, q)
    assert.Equal(t, []string{"https://a.com/2"}, sourceURLs(res.Sources))
}

func TestApplyQueryConstraints_RelativeDate(t *testing.T) {
    q := Parse("news before:2020")
    sources := []SearchSource{
        {URL: "https://a.com/1", PublishedDate: "2 days ago"},  // 已知违规
        {URL: "https://a.com/2"},                              // undated → 放过
    }
    res := ApplyQueryConstraints(sources, q)
    assert.Equal(t, []string{"https://a.com/2"}, sourceURLs(res.Sources))
}

func TestMatchesSite(t *testing.T) {
    assert.True(t,  MatchesSite("https://docs.k8s.io/setup", "k8s.io"))
    assert.True(t,  MatchesSite("https://k8s.io/", "k8s.io"))
    assert.False(t, MatchesSite("https://notk8s.io/", "k8s.io"))
    assert.True(t,  MatchesSite("https://github.com/anthropics/sdk", "github.com/anthropics"))
    assert.False(t, MatchesSite("https://github.com/other/sdk", "github.com/anthropics"))
    assert.False(t, MatchesSite("not a url", "k8s.io"))
}
```

---

## 12. 完整 Go 解析器代码骨架

> [!warning] 这是参考骨架，**未在 mocode 中实际实现**（调研报告，按用户要求不写代码）
> 约 400 行 Go，保留全部 oh-my-pi 设计要点，**省略**实现细节（错误返回、context 支持、metrics）。

```go
// pkg/search/query/types.go
package query

import "time"

type QueryTerm struct {
    Text    string
    Phrase  bool
    Negated bool
    Group   *int
}

type StructuredQuery struct {
    Raw               string
    Text              string
    Terms             []QueryTerm
    Sites             []string
    ExcludedSites     []string
    InURL             []string
    ExcludedInURL     []string
    InTitle           []string
    ExcludedInTitle   []string
    InText            []string
    ExcludedInText    []string
    Filetypes         []string
    ExcludedFiletypes []string
    After             *time.Time
    Before            *time.Time
    Lang              string
    HasDirectives     bool
    HasConstraints    bool
}

type QuerySyntax struct {
    Phrases, Negation, OR, Site, InURL, InTitle, InText, Filetype, DateRange bool
}

var GoogleSyntax = QuerySyntax{
    Phrases: true, Negation: true, OR: true,
    Site: true, InURL: true, InTitle: true, InText: true,
    Filetype: true, DateRange: true,
}

type SearchSource struct {
    URL           string
    Title         string
    Snippet       string
    PublishedDate string
    AgeSeconds    *float64
}

type ConstraintFilterResult struct {
    Sources []SearchSource
    Dropped []string
}
```

```go
// pkg/search/query/patterns.go
package query

import (
    "regexp"
    "strings"
    "unicode"
)

var (
    directivePattern = regexp.MustCompile(`^([+-]?)([a-z][a-z-]*):(.*)$`)
    dateISO          = regexp.MustCompile(`^(\d{4})(?:[-/.](\d{1,2})(?:[-/.](\d{1,2}))?)?$`)
    dateUS           = regexp.MustCompile(`^(\d{1,2})[-/.](\d{1,2})[-/.](\d{4})$`)
    relativeAge      = regexp.MustCompile(`(?i)^(\d+)\s*(minute|min|hour|hr|day|week|month|mo|year|yr|[mhdwy])s?\s+ago$`)
    siteScheme       = regexp.MustCompile(`^[a-z][a-z0-9+.-]*://`)
)

var directiveFields = map[string]string{
    "site": "site", "domain": "site", "host": "site",
    "inurl": "inUrl", "url": "inUrl",
    "intitle": "inTitle", "title": "inTitle",
    "intext": "inText", "inbody": "inText", "inanchor": "inText",
    "filetype": "filetype", "ext": "filetype",
    "before": "before", "until": "before",
    "after": "after", "since": "after",
    "lang": "lang", "language": "lang",
}

var allModes = map[string]string{
    "allintitle": "inTitle",
    "allinurl":   "inUrl",
    "allintext":  "inText",
}

func isQuote(r rune) bool { return r == '"' || r == '\u201c' || r == '\u201d' }
func isReservedToken(text string) bool {
    switch text {
    case "(", ")", "OR", "AND", "NOT", "|", "||", "&&", "!":
        return true
    }
    m := directivePattern.FindStringSubmatch(text)
    if m == nil { return false }
    _, ok1 := directiveFields[strings.ToLower(m[2])]
    _, ok2 := allModes[strings.ToLower(m[2])]
    return ok1 || ok2
}
```

```go
// pkg/search/query/parser.go
package query

import (
    "strconv"
    "strings"
    "time"
    "unicode"
)

type rawToken struct {
    text         string
    quoted       bool
    quotedValue  bool
}

func tokenize(raw string) []rawToken {
    var tokens []rawToken
    i, n := 0, len([]rune(raw))
    runes := []rune(raw)
    for i < n {
        ch := runes[i]
        if unicode.IsSpace(ch) { i++; continue }
        if isQuote(ch) {
            j := i + 1
            var buf strings.Builder
            for j < n && !isQuote(runes[j]) { buf.WriteRune(runes[j]); j++ }
            if strings.TrimSpace(buf.String()) != "" {
                tokens = append(tokens, rawToken{text: strings.TrimSpace(buf.String()), quoted: true})
            }
            i = j + 1; continue
        }
        var buf strings.Builder
        quotedValue := false
        for i < n && !unicode.IsSpace(runes[i]) {
            c := runes[i]
            if isQuote(c) && strings.HasSuffix(buf.String(), ":") {
                j := i + 1
                for j < n && !isQuote(runes[j]) { buf.WriteRune(runes[j]); j++ }
                quotedValue = true; i = j + 1; continue
            }
            if isQuote(c) { break }
            buf.WriteRune(c); i++
        }
        if buf.Len() > 0 { tokens = append(tokens, rawToken{text: buf.String(), quotedValue: quotedValue}) }
    }
    return splitParens(tokens)
}

func splitParens(tokens []rawToken) []rawToken {
    var out []rawToken
    for _, tok := range tokens {
        if tok.quoted || tok.quotedValue { out = append(out, tok); continue }
        text := tok.text
        for strings.HasPrefix(text, "(") {
            out = append(out, rawToken{text: "("})
            text = text[1:]
        }
        trailing := 0
        for strings.HasSuffix(text, ")") {
            body := text[:len(text)-1]
            depth := 0
            for _, c := range body {
                if c == '(' { depth++ } else if c == ')' { depth-- }
            }
            if depth > 0 { break }
            text = body; trailing++
        }
        if text != "" { out = append(out, rawToken{text: text}) }
        for k := 0; k < trailing; k++ { out = append(out, rawToken{text: ")"}) }
    }
    return out
}

func NormalizeSite(value string) string {
    site := strings.ToLower(strings.TrimSpace(value))
    site = siteScheme.ReplaceAllString(site, "")
    if strings.HasPrefix(site, "*.") { site = site[2:] }
    site = strings.TrimRight(site, "/.")
    return site
}

func ParseDateValue(value string) (time.Time, bool) {
    t := strings.TrimSpace(value)
    if m := dateISO.FindStringSubmatch(t); m != nil {
        y, _ := strconv.Atoi(m[1])
        mo := 1
        if m[2] != "" { mo, _ = strconv.Atoi(m[2]) }
        d := 1
        if m[3] != "" { d, _ = strconv.Atoi(m[3]) }
        if y < 1000 || y > 9999 || mo < 1 || mo > 12 || d < 1 || d > 31 { return time.Time{}, false }
        return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC), true
    }
    if m := dateUS.FindStringSubmatch(t); m != nil {
        mo, _ := strconv.Atoi(m[1]); d, _ := strconv.Atoi(m[2]); y, _ := strconv.Atoi(m[3])
        if mo > 12 && d <= 12 { mo, d = d, mo }
        if y < 1000 || y > 9999 || mo < 1 || mo > 12 || d < 1 || d > 31 { return time.Time{}, false }
        return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC), true
    }
    return time.Time{}, false
}

func Parse(raw string) StructuredQuery {
    q := StructuredQuery{Raw: raw}
    tokens := tokenize(raw)
    var negateNext, orPending, lastWasTerm bool
    groupSeq := 0
    var allMode string

    pushConstraint := func(field, value string, negated bool) {
        q.HasDirectives = true; orPending = false; lastWasTerm = false
        v := strings.TrimSpace(value)
        if v == "" { return }
        switch field {
        case "site":
            s := NormalizeSite(v)
            if s != "" {
                if negated { q.ExcludedSites = append(q.ExcludedSites, s) } else { q.Sites = append(q.Sites, s) }
            }
        case "inUrl":
            if negated { q.ExcludedInURL = append(q.ExcludedInURL, v) } else { q.InURL = append(q.InURL, v) }
        case "inTitle":
            if negated { q.ExcludedInTitle = append(q.ExcludedInTitle, v) } else { q.InTitle = append(q.InTitle, v) }
        case "inText":
            if negated { q.ExcludedInText = append(q.ExcludedInText, v) } else { q.InText = append(q.InText, v) }
        case "filetype":
            ext := strings.TrimLeft(strings.ToLower(v), ".")
            if ext != "" {
                if negated { q.ExcludedFiletypes = append(q.ExcludedFiletypes, ext) } else { q.Filetypes = append(q.Filetypes, ext) }
            }
        }
    }

    pushTerm := func(text string, phrase bool) {
        negated := negateNext; negateNext = false
        if allMode != "" {
            pushConstraint(allMode, text, negated); return
        }
        term := QueryTerm{Text: text, Phrase: phrase, Negated: negated}
        if orPending && lastWasTerm {
            if n := len(q.Terms); n > 0 {
                if q.Terms[n-1].Group == nil { groupSeq++; q.Terms[n-1].Group = &groupSeq }
                term.Group = q.Terms[n-1].Group
            }
        }
        orPending = false; lastWasTerm = true
        q.Terms = append(q.Terms, term)
    }

    for i := 0; i < len(tokens); i++ {
        tok := tokens[i]
        if tok.quoted { pushTerm(tok.text, true); continue }
        switch tok.text {
        case "(", ")": continue
        case "OR", "|", "||": orPending = true; q.HasDirectives = true; continue
        case "AND", "&&": q.HasDirectives = true; continue
        case "NOT", "!": negateNext = true; q.HasDirectives = true; continue
        case "-":
            if i+1 < len(tokens) && tokens[i+1].quoted { negateNext = true }
            continue
        case "+": continue
        }
        m := directivePattern.FindStringSubmatch(tok.text)
        if m == nil {
            // 裸词
            text := tok.text
            if strings.HasPrefix(text, "-") && len(text) > 1 {
                negateNext = true; q.HasDirectives = true
                text = strings.TrimLeft(text, "-")
                if text == "" { continue }
                if nm := directivePattern.FindStringSubmatch(text); nm != nil {
                    if f, ok := directiveFields[strings.ToLower(nm[2])]; ok && f != "before" && f != "after" && f != "lang" {
                        negateNext = false
                        pushConstraint(f, nm[3], true); continue
                    }
                }
                pushTerm(text, false); continue
            }
            if strings.HasPrefix(text, "+") && len(text) > 1 {
                pushTerm(text[1:], true); q.HasDirectives = true; continue
            }
            pushTerm(text, false); continue
        }
        name := strings.ToLower(m[2])
        allMatch := allModes[name]
        field, isField := directiveFields[name]
        if allMatch != "" {
            allMode = allMatch; q.HasDirectives = true
            if inline := strings.TrimSpace(m[3]); inline != "" { pushConstraint(allMatch, inline, m[1] == "-") }
            orPending = false; lastWasTerm = false; continue
        }
        if isField {
            value := strings.TrimSpace(m[3])
            if value == "" && i+1 < len(tokens) {
                if nxt := tokens[i+1]; nxt.quoted || !isReservedToken(nxt.text) {
                    value = strings.TrimSpace(nxt.text); i++
                }
            }
            if value == "" { q.HasDirectives = true; continue }
            negated := m[1] == "-" || negateNext; negateNext = false
            switch field {
            case "before", "after":
                d, ok := ParseDateValue(value)
                if !ok { pushTerm(tok.text, false); continue }
                if field == "before" { q.Before = &d } else { q.After = &d }
                q.HasDirectives = true; orPending = false; lastWasTerm = false
            case "lang":
                q.Lang = strings.ToLower(value); q.HasDirectives = true; orPending = false; lastWasTerm = false
            default:
                pushConstraint(field, value, negated)
            }
            continue
        }
        // 未知指令：作为裸词
        pushTerm(tok.text, false)
    }
    q.Text = renderTerms(q.Terms, QuerySyntax{Phrases: true, Negation: true, OR: true})
    q.HasConstraints = len(q.Sites) > 0 || len(q.ExcludedSites) > 0 ||
        len(q.InURL) > 0 || len(q.ExcludedInURL) > 0 ||
        len(q.InTitle) > 0 || len(q.ExcludedInTitle) > 0 ||
        len(q.Filetypes) > 0 || len(q.ExcludedFiletypes) > 0 ||
        q.Before != nil || q.After != nil
    return q
}

func renderTerms(terms []QueryTerm, syn QuerySyntax) string {
    var parts []string
    for i := 0; i < len(terms); i++ {
        t := terms[i]
        if t.Group != nil && syn.OR {
            var members []string
            j := i
            for ; j < len(terms) && terms[j].Group != nil && *terms[j].Group == *t.Group; j++ {
                if r := renderTerm(terms[j], syn); r != "" { members = append(members, r) }
            }
            i = j - 1
            switch len(members) {
            case 0: // skip
            case 1: parts = append(parts, members[0])
            default: parts = append(parts, "("+strings.Join(members, " OR ")+")")
            }
            continue
        }
        if r := renderTerm(t, syn); r != "" { parts = append(parts, r) }
    }
    return strings.Join(parts, " ")
}

func renderTerm(t QueryTerm, syn QuerySyntax) string {
    if t.Negated && !syn.Negation { return "" }
    body := t.Text
    if t.Phrase && syn.Phrases { body = `"` + body + `"` }
    if t.Negated { return "-" + body }
    return body
}
```

```go
// pkg/search/query/format.go
package query

import "strings"

func quoteValue(v string) string {
    if strings.ContainsAny(v, " \t\n") { return `"` + v + `"` }
    return v
}

func FormatQuery(q StructuredQuery, syn QuerySyntax) string {
    var parts []string
    if t := renderTerms(q.Terms, syn); t != "" { parts = append(parts, t) }
    if syn.Site {
        if len(q.Sites) > 1 && syn.OR {
            var ss []string
            for _, s := range q.Sites { ss = append(ss, "site:"+s) }
            parts = append(parts, "("+strings.Join(ss, " OR ")+")")
        } else {
            for _, s := range q.Sites { parts = append(parts, "site:"+s) }
        }
        for _, s := range q.ExcludedSites { parts = append(parts, "-site:"+s) }
    }
    if syn.InURL {
        for _, v := range q.InURL { parts = append(parts, "inurl:"+quoteValue(v)) }
        for _, v := range q.ExcludedInURL { parts = append(parts, "-inurl:"+quoteValue(v)) }
    }
    if syn.InTitle {
        for _, v := range q.InTitle { parts = append(parts, "intitle:"+quoteValue(v)) }
        for _, v := range q.ExcludedInTitle { parts = append(parts, "-intitle:"+quoteValue(v)) }
    }
    if syn.InText {
        for _, v := range q.InText { parts = append(parts, "intext:"+quoteValue(v)) }
        for _, v := range q.ExcludedInText { parts = append(parts, "-intext:"+quoteValue(v)) }
    }
    if syn.Filetype {
        if len(q.Filetypes) > 1 && syn.OR {
            var ss []string
            for _, f := range q.Filetypes { ss = append(ss, "filetype:"+f) }
            parts = append(parts, "("+strings.Join(ss, " OR ")+")")
        } else {
            for _, f := range q.Filetypes { parts = append(parts, "filetype:"+f) }
        }
        for _, f := range q.ExcludedFiletypes { parts = append(parts, "-filetype:"+f) }
    }
    if syn.DateRange {
        if q.After != nil { parts = append(parts, "after:"+q.After.Format("2006-01-02")) }
        if q.Before != nil { parts = append(parts, "before:"+q.Before.Format("2006-01-02")) }
    }
    result := strings.TrimSpace(strings.Join(parts, " "))
    if result != "" { return result }
    var fallback []string
    fallback = append(fallback, q.Sites...)
    fallback = append(fallback, q.InTitle...)
    fallback = append(fallback, q.InURL...)
    fallback = append(fallback, q.InText...)
    fallback = append(fallback, q.Filetypes...)
    if fb := strings.TrimSpace(strings.Join(fallback, " ")); fb != "" { return fb }
    return strings.TrimSpace(q.Raw)
}
```

```go
// pkg/search/query/filter.go
package query

import (
    "net/url"
    "regexp"
    "strconv"
    "strings"
    "time"
)

func hostAndPath(rawURL string) (string, string, bool) {
    u, err := url.Parse(rawURL)
    if err != nil || u.Host == "" { return "", "", false }
    return strings.ToLower(u.Host), u.Path, true
}

func MatchesSite(rawURL, site string) bool {
    host, path, ok := hostAndPath(rawURL)
    if !ok { return false }
    if i := strings.Index(site, "/"); i >= 0 {
        siteHost, sitePath := site[:i], strings.ToLower(site[i:])
        if host != siteHost && !strings.HasSuffix(host, "."+siteHost) { return false }
        if sitePath != "" && !strings.HasPrefix(strings.ToLower(path), sitePath) { return false }
        return true
    }
    return host == site || strings.HasSuffix(host, "."+site)
}

func matchesFiletype(rawURL, ext string) bool {
    _, path, ok := hostAndPath(rawURL)
    if !ok { return false }
    return strings.HasSuffix(strings.ToLower(path), "."+strings.ToLower(ext))
}

var relUnitSec = map[string]float64{
    "m": 60, "min": 60, "minute": 60,
    "h": 3600, "hr": 3600, "hour": 3600,
    "d": 86400, "day": 86400,
    "w": 604800, "week": 604800,
    "mo": 2592000, "month": 2592000,
    "y": 31536000, "yr": 31536000, "year": 31536000,
}

func sourceTime(s SearchSource) *time.Time {
    if s.AgeSeconds != nil && !isInf(*s.AgeSeconds) { t := time.Now().Add(-time.Duration(*s.AgeSeconds*float64(time.Second))); return &t }
    if s.PublishedDate == "" { return nil }
    if m := relativeAge.FindStringSubmatch(strings.TrimSpace(s.PublishedDate)); m != nil {
        n, _ := strconv.Atoi(m[1])
        if sec := n * int(relUnitSec[strings.ToLower(m[2])]); sec > 0 { t := time.Now().Add(-time.Duration(sec) * time.Second); return &t }
    }
    if t, err := time.Parse(time.RFC3339, s.PublishedDate); err == nil { return &t }
    return nil
}

func isInf(f float64) bool { return f != f || f > 1e308 || f < -1e308 }

type constraintDim struct {
    label string
    pred  func(SearchSource) bool
}

func constraintDimensions(q StructuredQuery) []constraintDim {
    var dims []constraintDim
    lc := func(s string) string { return strings.ToLower(s) }
    if len(q.Sites) > 0 {
        var lab []string
        for _, s := range q.Sites { lab = append(lab, "site:"+s) }
        sites := q.Sites
        dims = append(dims, constraintDim{
            label: strings.Join(lab, " OR "),
            pred: func(src SearchSource) bool {
                for _, s := range sites { if MatchesSite(src.URL, s) { return true } }
                return false
            },
        })
    }
    if len(q.ExcludedSites) > 0 {
        var lab []string
        for _, s := range q.ExcludedSites { lab = append(lab, "-site:"+s) }
        exc := q.ExcludedSites
        dims = append(dims, constraintDim{
            label: strings.Join(lab, " "),
            pred: func(src SearchSource) bool {
                for _, s := range exc { if MatchesSite(src.URL, s) { return false } }
                return true
            },
        })
    }
    if len(q.InURL) > 0 {
        var lab []string
        for _, v := range q.InURL { lab = append(lab, "inurl:"+v) }
        inUrl := q.InURL
        dims = append(dims, constraintDim{
            label: strings.Join(lab, " "),
            pred: func(src SearchSource) bool {
                u := lc(src.URL)
                for _, v := range inUrl { if !strings.Contains(u, lc(v)) { return false } }
                return true
            },
        })
    }
    if len(q.InTitle) > 0 {
        var lab []string
        for _, v := range q.InTitle { lab = append(lab, "intitle:"+v) }
        inTitle := q.InTitle
        dims = append(dims, constraintDim{
            label: strings.Join(lab, " "),
            pred: func(src SearchSource) bool {
                ti := lc(src.Title)
                for _, v := range inTitle { if !strings.Contains(ti, lc(v)) { return false } }
                return true
            },
        })
    }
    if len(q.Filetypes) > 0 {
        var lab []string
        for _, f := range q.Filetypes { lab = append(lab, "filetype:"+f) }
        fts := q.Filetypes
        dims = append(dims, constraintDim{
            label: strings.Join(lab, " OR "),
            pred: func(src SearchSource) bool {
                for _, f := range fts { if matchesFiletype(src.URL, f) { return true } }
                return false
            },
        })
    }
    if q.After != nil || q.Before != nil {
        var lab []string
        if q.After != nil { lab = append(lab, "after:"+q.After.Format("2006-01-02")) }
        if q.Before != nil { lab = append(lab, "before:"+q.Before.Format("2006-01-02")) }
        after, before := q.After, q.Before
        dims = append(dims, constraintDim{
            label: strings.Join(lab, " "),
            pred: func(src SearchSource) bool {
                t := sourceTime(src)
                if t == nil { return true } // undated → cannot prove violation
                if after != nil && t.Before(*after) { return false }
                if before != nil && !t.Before(*before) { return false }
                return true
            },
        })
    }
    return dims
}

func MatchesQueryConstraints(src SearchSource, q StructuredQuery) bool {
    for _, d := range constraintDimensions(q) { if !d.pred(src) { return false } }
    return true
}

func ApplyQueryConstraints(sources []SearchSource, q StructuredQuery) ConstraintFilterResult {
    current := append([]SearchSource(nil), sources...)
    var dropped []string
    if len(current) == 0 { return ConstraintFilterResult{Sources: current, Dropped: dropped} }
    for _, d := range constraintDimensions(q) {
        kept := current[:0]
        for _, s := range current { if d.pred(s) { kept = append(kept, s) } }
        if len(kept) > 0 { current = kept } else { dropped = append(dropped, d.label) }
    }
    return ConstraintFilterResult{Sources: current, Dropped: dropped}
}
```

---

## 13. 相关笔记

- [[02-web-search-provider-chain]] — 整体搜索架构
- [[provider-chain-design]] — Provider 懒加载 / Fallback 链设计
- [[MASTER_PLAN]] — search-tools 顶层规划
- `tmp/oh-my-pi/packages/coding-agent/src/web/search/query.ts` — 调研源（1110 行 TypeScript + 完整单测）
- `tmp/oh-my-pi/packages/coding-agent/test/web/search/query.test.ts` — 17 个测试用例覆盖全部 9 维度

## 14. 不确定 / 需进一步确认的细节

1. **`intitle:"a b"` 引号吞入的边界**：当前实现只在 `buf.endsWith(":")` 时吞——若用户在 directive 后多打空格 `intitle: "a b"`，会走「`intitle:` + 裸词 `"a b"`」路径，依赖 §7 的「adopt next token」机制。两种路径在测试里都覆盖，但**合并语义是否始终一致**需在 Go 移植时逐用例对照。
2. **`isReservedToken` 的精确范围**：当前 reserved = `(`/`)`/`OR`/`AND`/`NOT`/`|`/`||`/`&&`/`!` + 已知 directive。**未来若加新指令名，必须同步这里**，否则 `site: OR new-directive` 会把 `new-directive` 当 site 值。
3. **date `before:` 是 exclusive 上界（`time >= beforeMs` 视为违规）**——这与 Go `time.Before` 自然契合（`!t.Before(*before)` 意味着 `t >= before`）。但文档里没明确说"exclusive"——若用户/下游期望 `before:2024` 包含 2024 全年，需要调整。
4. **inText 不参与 post-filter 的判定**：注释说"snippets are partial"——`SearchSource.Snippet` 字段确实只有 200-300 字符。**如果未来有 provider 返回 full body text，可重新启用 inText 后过滤**，只需在 `constraintDimensions` 里加一段。
5. **Go `time.RFC3339` 解析限制**：原版 `Date.parse` 接受更多格式（locale-specific），Go 仅 `RFC3339` 严格。若需兼容 "May 15, 2024" 等格式，Go 端需额外实现。
6. **`formatScraperQuery` 单独成函数**——是否在 mocode Go 版需要这一层，取决于目标引擎集合。若只用 MCP 类 provider（Exa/z.ai），可省略。
7. **OR 组 id 跨 query 实例不唯一**：用 `groupSeq` 自增，**不同 query 解析可能产生相同 id 值**——但每份 `StructuredQuery` 独立使用，不会跨实例比较。**若未来要把多个 query 合并到同一 `terms[]`**，需重设 group id。
