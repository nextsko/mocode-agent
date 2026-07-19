# AST Grep 深度研究

> 基于 `oh-my-pi` 项目中 `packages/coding-agent/src/tools/ast-grep.ts` 及底层 `crates/pi-natives/src/ast.rs`、`crates/pi-ast/src/` 的源码分析。

---

## 1. AST 搜索基础

### 1.1 核心架构

```text
用户输入 pattern
    │
    ▼
[TypeScript AgentTool] ── N-API ──> [Rust pi-natives::ast_grep]
    │
    │  resolve_language(path)       语言推断（extension → SupportLang）
    │  compile_pattern(pattern, lang) PatternBuilder + StrDoc
    │
    ▼
tree-sitter parser
    │
    │  language.ast_grep(source)
    │  StrDoc::try_new(src, lang)
    │
    ▼
AST 根节点
    │
    │  root.find_all(pattern)
    │  root.dfs().any(|node| node.is_error())
    │
    ▼
Match<StrDoc> ── range() / start_pos() / end_pos() / text() / get_env()
```

### 1.2 语言支持层

- **tree-sitter 作为唯一解析后端**：`pi-ast/src/language/parsers.rs` 为每种语言导出 `TSLanguage`，全部来自 `tree-sitter-<lang>` crate。
- **SupportLang 枚举**：48 种语言，通过 `extensions()` 映射文件后缀，通过 `from_path()` 自动推断。
- **Language trait 实现**：
  - `impl_lang!`：语法本身接受 `$` 作为标识符的语言（JS/TS/Bash/Java 等），无需转义。
  - `impl_lang_expando!`：语法不允许 `$` 作为标识符的语言（C/C++/Python/Rust 等），使用特殊占位符（`µ`、`𐀀`）在 `pre_process_pattern` 阶段转义 metavariable。
- **HTML 特殊处理**：支持 `<script>`/`<style>` 注入解析（`injectable_languages` + `extract_injections`）。

### 1.3 AST 构建细节

- `StrDoc::try_new(src, language)`：将源码包装为 tree-sitter 可查询文档。
- `language.ast_grep(source)`：创建 `Doc<StrDoc<L>>`，内部调用 `tree_sitter::Parser::parse`。
- 错误节点检测：`ast.root().dfs().any(|node| node.is_error())`，仅报告不中断搜索。

---

## 2. 查询语言 / DSL

### 2.1 Pattern 语法（ast-grep 风格）

| 语法 | 语义 | 示例 |
|------|------|------|
| `$NAME` | 捕获一个节点，绑定到 `NAME` | `console.log($MSG)` |
| `$$$NAME` | 捕获零个或多个兄弟节点 | `import { $$$IMPORTS } from "react"` |
| `$_` | 匹配任意单个节点，不绑定 | `logger.$_($$$ARGS)` |
| `$$$` | 捕获零个或多个不绑定节点 | `const $NAME = ($$$ARGS) => $BODY` |
| `kind` | 按节点类型匹配 | `if_statement`, `call_expression` |
| `field` | 按字段名匹配 | `left: $A, right: $B` |
| `selector` | contextual pattern 的父级选择器 | `Pattern::contextual(pattern, selector, lang)` |
| `strictness` | 匹配严格度 | `cst / smart / ast / relaxed / signature / template` |

### 2.2 匹配严格度

```rust
// ast.rs: AstMatchStrictness
Cst      // 具体语法树级别
Smart    // 平衡默认（代码默认）
Ast      // 纯 AST 级别
Relaxed  // 更宽松
Signature // 结构签名
Template  // 模板风格
```

### 2.3 模式编译流程

```rust
// pi-ast/src/ops.rs
fn compile_pattern(pattern, selector, strictness, lang) -> Result<Pattern> {
    let selector = selector.map(str::trim).filter(|s| !s.is_empty());
    let compiled = if let Some(selector) = selector {
        Pattern::contextual(pattern, selector, lang)?       // 带父级选择器
    } else {
        Pattern::try_new(pattern, lang)?                    // 独立 pattern
    };
    // MultipleNode fallback: 自动包装（JSON 特有）
    compiled.strictness = strictness;
    Ok(compiled)
}
```

- **MultipleNode fallback**：当 pattern 片段解析为多个根节点时（如 `"key": $V`），自动包装为 `{ "key": $V }` 并选择 `pair` 节点（仅 JSON）。
- **Rust 特殊处理**：自动尝试 `fn __rwp_wrapper() {{ {pattern}; }}` 上下文包装，使表达式语句可被匹配。

### 2.4 模型提示词约束（`prompts/tools/ast-grep.md`）

```markdown
- 每次调用只匹配一种语言
- 使用 `$$$NAME`，禁止 `$$NAME`
- Metavariable 名称必须全大写
- Pattern 必须解析为单个 AST 节点
- C++ 表达式语句需 trailing `;`
- TS 可容忍 annotations
```

---

## 3. 工具实现

### 3.1 参数 Schema

```typescript
// ast-grep.ts
const astGrepSchema = type({
    pat:    type("string").describe("ast pattern"),
    "path?": type("string").describe(
        'file, directory, glob, or internal URL; semicolon-delimited list ("src; tests")'
    ),
    "skip?": type("number").describe("matches to skip"),
});
```

### 3.2 AgentTool 注册

```typescript
export class AstGrepTool implements AgentTool<typeof astGrepSchema, AstGrepToolDetails> {
    readonly name = "ast_grep";
    readonly approval = "read";
    readonly label = "AST Grep";
    readonly summary = "Search code with AST patterns (structural grep)";
    readonly strict = true;
    readonly loadMode = "discoverable";
    readonly examples = [
        { caption: "Search TypeScript files under src", call: { pat: "console.log($$$)", path: "src/**/*.ts" } },
        // ...
    ];
}
```

### 3.3 执行流程

```typescript
async execute(_toolCallId, params, signal, _onUpdate, _context) {
    return untilAborted(signal, async () => {
        const pattern = params.pat.trim();
        const skip = Math.floor(params.skip ?? 0);
        const scopedPaths = toPathList(params.path);
        const scope = await resolveToolSearchScope({ ... });
        const { searchPath, multiTargets, globFilter } = scope;

        const DEFAULT_AST_LIMIT = 50;
        const result = multiTargets
            ? await runMultiTargetAstGrep(multiTargets, {
                  patterns: [pattern],
                  commonBasePath: searchPath,
                  skip, limit: DEFAULT_AST_LIMIT, signal,
              })
            : await astGrep({
                  patterns: [pattern],
                  path: searchPath,
                  glob: globFilter,
                  offset: skip,
                  includeMeta: true,
                  signal,
              });

        // 格式化输出...
        return toolResult(details).text(outputLines.join("\n")).done();
    });
}
```

### 3.4 多目标合并逻辑

```typescript
async function runMultiTargetAstGrep(targets, options) {
    const retainedMatches: AstFindMatch[] = [];
    const retainedCapacity = options.skip + options.limit + 1;

    for (const target of targets) {
        const targetResult = await astGrep({
            patterns: options.patterns,
            path: target.basePath,
            glob: target.glob,
            offset: 0,
            limit: retainedCapacity,
            includeMeta: true,
            signal: options.signal,
        });
        // 去重 + rebase 路径
        for (const match of targetResult.matches) {
            const absolute = path.resolve(target.basePath, match.path);
            const rebased = path.relative(options.commonBasePath, absolute).replace(/\\/g, "/");
            retainAstFindMatch(retainedMatches, retainedCapacity, { ...match, path: rebased });
        }
    }
    retainedMatches.sort(compareAstFindMatch);
    const visible = retainedMatches.slice(options.skip);
    return { matches: visible.slice(0, options.limit), ... };
}
```

### 3.5 Bounded Retention 策略

```typescript
function retainAstFindMatch(matches, capacity, candidate) {
    if (matches.length < capacity) { matches.push(candidate); return; }
    let worstIndex = 0;
    for (let index = 1; index < matches.length; index++) {
        if (compareAstFindMatch(matches[index]!, matches[worstIndex]!) > 0) worstIndex = index;
    }
    if (compareAstFindMatch(candidate, matches[worstIndex]!) < 0) matches[worstIndex] = candidate;
}
```

- **避免全量内存累积**：只保留 `skip + limit + 1` 个最差候选项中的最佳者。
- Rust 侧同步实现：`BinaryHeap<RetainedAstFindMatch>` + `should_retain_match`。

---

## 4. 与 grep 的协同

### 4.1 对比表格

| 维度 | `ast_grep` | `grep` |
|------|-----------|--------|
| **后端** | tree-sitter + ast-grep-core | ripgrep (grep-searcher) + 可选 PCRE2 |
| **查询类型** | 结构模式（AST pattern） | 正则表达式（RE2/PCRE2） |
| **匹配粒度** | AST 节点（多行代码块） | 单行/多行文本匹配 |
| **语言感知** | 48 种 tree-sitter 语言 | 文本搜索，无语法感知 |
| **元变量捕获** | 支持（`$NAME` → matched text） | 不支持 |
| **严格度** | 6 级（cst/smart/ast/relaxed/signature/template） | N/A |
| **大文件处理** | 4MB 前缀窗口搜索 | 4MB 前缀窗口搜索；oversized deferred pass |
| **archive/SSH** | 仅本地文件系统 | 支持 archive 成员、SSH 远程文件、virtual resources |
| **搜索范围** | 单 path/glob | 单 path/glob + 行范围选择器 `:N-M` |
| **并行化** | 单文件顺序遍历（worker thread） | 并行 walker + streaming windowed grep |
| **超时** | `timeout_ms` + cancel token | `timeoutMs` + cancel token |
| **上下文行** | N/A（返回匹配节点全文） | `contextBefore` / `contextAfter` |
| **输出格式** | `*LINE|content` + `meta:` 行 | `*LINE|content` + context lines |
| **主场景** | 函数调用、声明、语法结构搜索 | 文本片段、字符串、符号搜索 |

### 4.2 互补关系

```text
模型需要查找代码结构：
1. 先 grep（正则）→ 快速定位可能包含目标的行
2. 再 ast_grep → 验证语法结构是否匹配
3. 或直接用 ast_grep → 当语法形状比文本更重要时
```

- **grep 优势**：跨 archive/SSH、支持行范围选择器、并行 streaming、超大文件分片、PCRE2 lookaround。
- **ast_grep 优势**：无视注释/字符串格式变化、匹配语法语义、支持 metavariable 约束（`$A == $A` 要求两侧相同）。

### 4.3 结果合并策略

当前 `oh-my-pi` 中两者**独立工具**，不存在自动合并。模型需分别调用，各自通过 `fileList` + `matchCount` 聚合结果。

---

## 5. 性能优化

### 5.1 AST 解析缓存

- **Rust 侧无显式 AST 缓存**：每次 `ast_grep` 调用都对每个文件重新解析。
- **文件枚举缓存**：`pi_walker::WalkRequest::cache(true)` 在 AST 搜索中启用文件列表缓存。
- **大文件两阶段**：
  - Pass 1：正常文件全读搜索。
  - Pass 2：oversized 文件只搜索前 4MB 前缀（`ReadPolicy::Prefix`）。
  - 预算满足时跳过 Pass 2。

### 5.2 增量/流式搜索

```rust
// pi-natives/src/grep.rs
const ORDERED_STREAMING_STOP_MAX_COUNT: u64 = 64;
const GREP_STREAM_WINDOW: usize = 512;

// 小预算 → 顺序 streaming，匹配后立即停止 walk
// 大预算 → 并行 walker + 全局 offset/limit
// 中等预算 → windowed streaming（512 文件窗口）
```

- ast_grep 无并行文件遍历（单 worker thread blocking task），依赖 tree-sitter 单文件解析速度。

### 5.3 内存控制

| 机制 | 位置 | 说明 |
|------|------|------|
| `BinaryHeap` bounded retention | ast.rs | 仅保留 `skip+limit+1` 个最差候选项 |
| `retained_find_capacity` | ast.rs | `offset + limit + 1` |
| `MAX_FILE_BYTES = 4MB` | grep.rs | 超过时只读前缀 |
| `MatchCollector` 按需收集 | grep.rs | `collect_matches = false` 时只计数不存储 |
| `SmallVec<[ContextLine; 8]>` | grep.rs | 上下文行用栈上数组 |

### 5.4 取消与超时

```rust
let ct = task::CancelToken::new(timeout_ms, signal);
task::blocking("ast_grep", ct, move |ct| { /* ... */ });
```

- `CancelToken::heartbeat()` 在每次文件/模式编译后检查取消状态。
- TypeScript 侧通过 `untilAborted(signal, ...)` 包装，AbortSignal 透传 Rust。

---

## 6. 对模型友好的输出

### 6.1 Match 行格式化

```typescript
// match-line-format.ts
export function formatMatchLine(lineNumber, line, isMatch, options) {
    const marker = isMatch ? "*" : " ";
    if (options.useHashLines) {
        return `${marker}${lineNumber}:${line}`;   // hashline 模式
    }
    return `${marker}${lineNumber}|${line}`;        // 传统模式
}
```

### 6.2 Meta 变量输出

```typescript
if (match.metaVariables && Object.keys(match.metaVariables).length > 0) {
    const serializedMeta = Object.entries(match.metaVariables)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => `${key}=${value}`)
        .join(", ");
    modelOut.push(`  meta: ${serializedMeta}`);
    displayOut.push(`  meta: ${serializedMeta}`);
}
```

### 6.3 Hashtag 锚点（可编辑引用）

```typescript
if (hashContext?.tag) {
    outputLines.push(formatHashlineHeader(relativePath, hashContext.tag));
}
// → "src/foo.ts#<snapshot-tag>"
```

- 模型在后续 `edit` 工具中可直接引用该锚点。
- 文件快照通过 `recordFileSnapshot` 生成内容哈希标签，文件变更时自动失效。

### 6.4 树形折叠渲染

```typescript
const matchGroups = groupLineIndicesByBlank(allLines)
    .filter(indices => !first.startsWith("Result limit reached"))
    .map(indices => indices.map(index => styledLines[index]!));
```

- TUI 侧通过 `renderTreeList` 折叠/展开文件组。
- 折叠模式仅显示 `*` 标记的匹配行，隐藏上下文。

---

## 7. 局限性

### 7.1 不支持的语言

```rust
// ast.rs tests
assert!(resolve_supported_lang("brainfuck").is_err());
```

- `SupportLang` 仅覆盖 48 种语言，未支持的语言搜索会直接报错。
- 无通用 text-to-AST fallback。

### 7.2 解析失败策略

- **非致命**：`parse error (syntax tree contains error nodes)` 仅记录到 `parseErrors`，不中断整个搜索。
- **模型提示**：`No matches found. Parse issues mean the query may be mis-scoped; narrow path before concluding absence.`

### 7.3 大文件限制

- 超过 4MB 的文件只搜索前 4MB 前缀（与 grep 一致）。
- 无增量解析（incremental parse）或内存映射（mmap）持久化。

### 7.4 特定语法陷阱

```markdown
# prompts/tools/ast-grep.md
- C++ expression-statement calls need trailing `;`
- TS: tolerate annotations
- Declaration forms are distinct
```

- 模型需理解声明形式差异（`function foo` vs `const foo = () => {}` vs method `foo()`）。
- JSON 仅支持特定 wrapper fallback（`{...}` + `pair` selector）。

### 7.5 功能缺口 vs grep

| 缺口 | 说明 |
|------|------|
| 无 archive/SSH 搜索 | ast_grep 仅限本地文件系统 |
| 无行范围选择器 `:N-M` | ast_grep 不支持 |
| 无虚拟资源搜索 | ast_grep 无 virtual/remote resource 能力 |
| 无 context lines | ast_grep 返回匹配节点全文，无前后文 |
| 无 PCRE2 lookaround | 依赖 tree-sitter 语法，非正则引擎 |

---

## 8. 对项目的借鉴点

### 8.1 核心架构

1. **分层分离**：
   - TS 层：AgentTool schema、路径解析、TUI 渲染。
   - N-API 层：Rust native 导出，cancel token + blocking task。
   - 核心层：`ast-grep-core` + `tree-sitter`，与平台无关。

2. **语言抽象**：
   - `SupportLang` 枚举 + `impl_lang_expando!` macro 模式适合扩展新语言。
   - `pre_process_pattern` 处理不同语法对 `$` 的接受度。

### 8.2 可复用的设计模式

| 模式 | 实现 | 项目应用 |
|------|------|---------|
| Bounded retention | `BinaryHeap` + `should_retain_match` | 大规模搜索结果分页 |
| Multi-target merge | `runMultiTargetAstGrep` | 多路径/glob 搜索聚合 |
| Parse error capping | `capParseErrors` | 避免模型上下文爆炸 |
| Hashtag anchoring | `recordFileSnapshot` | 可编辑引用锚点 |
| Grouped file output | `formatGroupedFiles` | 目录级搜索渲染 |
| Streaming windowed grep | `GREP_STREAM_WINDOW = 512` | 超大仓库快速返回 |

### 8.3 DSL 设计建议

- 支持 **strictness 参数**暴露给模型（默认 smart，高级场景允许 ast/cst）。
- 保留 **selector 语法**用于 contextual pattern（如匹配 `expression_statement` 内的内容）。
- **Auto-wrap fallback**（JSON `{...}`）应文档化，避免模型对 "MultipleNode" 错误困惑。

### 8.4 与现有 grep 工具的整合

- 当前 `grep` 与 `ast_grep` 独立，模型需分别调用。
- 可考虑 **混合策略**：先用 `grep` 粗筛候选文件，再 `ast_grep` 精确匹配（类似 `runMultiTargetAstGrep` 的两阶段思路）。
- 共享基础设施：`resolveToolSearchScope`、`formatResultPath`、`createFileRecorder` 已抽象，可直接复用。

---

## 9. 附录：关键代码路径

| 文件 | 职责 |
|------|------|
| `crates/pi-ast/src/language/parsers.rs` | 48 种 tree-sitter parser 导出 |
| `crates/pi-ast/src/language/mod.rs` | `SupportLang` 枚举、别名映射、扩展推断 |
| `crates/pi-ast/src/ops.rs` | `compile_pattern`、`collect_matches`、`rewrite_source` |
| `crates/pi-ast/src/summary.rs` | 基于 BFS unfold 的 AST 摘要（非搜索，相关） |
| `crates/pi-natives/src/ast.rs` | N-API `ast_grep` / `ast_match` / `ast_edit` 实现 |
| `crates/pi-natives/src/grep.rs` | N-API `grep` / `search` 实现，ripgrep 后端 |
| `packages/coding-agent/src/tools/ast-grep.ts` | TypeScript AgentTool 包装、TUI 渲染 |
| `packages/coding-agent/src/tools/grep.ts` | TypeScript GrepTool 包装、virtual resource 搜索 |
| `packages/coding-agent/src/prompts/tools/ast-grep.md` | 模型提示词、DSL 约束说明 |
