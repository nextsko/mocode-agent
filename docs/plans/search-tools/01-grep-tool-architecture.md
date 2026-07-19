# Grep 工具架构深度研究

> 研究对象：`C:/Users/16143/Desktop/mocode/tmp/oh-my-pi/packages/coding-agent/src/tools/grep.ts`（1859 行，~71KB）

---

## 1. 文件级架构图（文字描述）

```
┌─────────────────────────────────────────────────────────────────────┐
│                         GrepTool (AgentTool)                        │
│  - 实现 AgentTool<typeof searchSchema, GrepToolDetails>             │
│  - 核心方法: execute(toolCallId, params, signal, onUpdate, context) │
└─────────────────────────────┬───────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
   ┌──────────────┐  ┌─────────────────┐  ┌──────────────┐
   │ 输入处理管道   │  │  路径解析分叉    │  │ 输出格式化器  │
   │ - pattern校验 │  │ - Archive       │  │ - match-line │
   │ - skip归一化  │  │ - Internal URL  │  │ - grouped-   │
   │ - path展开    │  │ - External URL  │  │   file-output│
   │ - delimiter   │  │ - Filesystem    │  │ - truncation │
   │   expansion   │  │ - SSH           │  │ - pagination │
   └──────────────┘  └─────────────────┘  └──────────────┘
          │                   │                   │
          └───────────────────┼───────────────────┘
                              ▼
                   ┌─────────────────────┐
                   │  pi-natives grep()   │
                   │  (Rust/ripgrep)      │
                   └─────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
   ┌──────────────┐  ┌─────────────────┐  ┌──────────────┐
   │ 结果后处理    │  │ Side Effects    │  │ TUI Renderer │
   │ - range filter│  │ - file snapshot │  │ - inline     │
   │ - dedup       │  │ - seen lines    │  │ - collapse/  │
   │ - per-file cap│  │ - file recorder │  │   expand     │
   │ - pagination  │  │                 │  │ - hyperlinks │
   └──────────────┘  └─────────────────┘  └──────────────┘
```

---

## 2. 工具定义与 Schema

### 2.1 类声明与接口

```typescript
// 行 887-906
export class GrepTool implements AgentTool<typeof searchSchema, GrepToolDetails> {
    readonly name = "grep";
    readonly approval = (args: unknown): ToolTier => {
        const a = args as { path?: string | string[]; paths?: string | string[] };
        return toPathList(a.path ?? a.paths).some(pathTargetsSsh) ? "exec" : "read";
    };
    readonly label = "Grep";
    readonly loadMode = "discoverable";
    readonly summary = "Grep file contents using ripgrep (fast regex search)";
    readonly description: string;
    readonly parameters = searchSchema;
    readonly strict = true;
```

### 2.2 参数 Schema（arktype）

```typescript
// 行 73-86
const searchPathEntry = type("string").describe(
    'file, directory, glob, internal URL, or "<file>:<lines>" selector...'
);
const searchSchema = type({
    pattern: type("string").describe("regex pattern"),
    "path?": searchPathEntry.describe("..."),
    "case?": type("boolean").describe("case-sensitive search"),
    "gitignore?": type("boolean").describe("respect gitignore"),
    "skip?": type("number").or("null").describe("..."),
});
```

**关键设计决策**：
- `strict = true`：严格校验，未知参数直接拒绝
- `approval` 动态分级：含有 `ssh://` 路径时需要 `exec` 权限，否则只需 `read`
- `loadMode = "discoverable"`：工具可被发现和自动调用

---

## 3. 输入处理管道

### 3.1 Pattern 校验

```typescript
// 行 920-929
if (!pattern.trim()) {
    throw new ToolError("Pattern must not be empty");
}
const normalizedPattern = pattern; // 保留原始空白

const normalizedSkip =
    skip === undefined || skip === null ? 0
    : Number.isFinite(skip) ? Math.floor(skip) : Number.NaN;
if (normalizedSkip < 0 || !Number.isFinite(normalizedSkip)) {
    throw new ToolError("Skip must be a non-negative number");
}
```

**要点**：Pattern 保持原样（`normalizedPattern = pattern`），不 trim，因为正则可能依赖首尾空白（如缩进锚点、尾随空格匹配）。

### 3.2 Paths 归一化与 Delimiter Expansion

```typescript
// 行 930-933
const scopedPaths = toPathList(rawPath);
const effectivePaths = scopedPaths.length > 0 ? scopedPaths : ["."];
const rawEntries = await expandDelimitedPathEntries(effectivePaths, this.session.cwd);
const pathSpecs = await parsePathSpecs(rawEntries, this.session.cwd);
```

- `toPathList`：将 `string | string[]` 统一为 `string[]`
- `expandDelimitedPathEntries`：展开分号分隔的路径列表
- `parsePathSpecs`：解析每个路径条目，分离文件名与行选择器

### 3.3 Line-Range Selector 解析

```typescript
// 行 175-199 (parsePathSpecs)
const internalSplit = splitInternalUrlSel(entry);
if (internalSplit.sel !== undefined) {
    if (!isReadSelectorGrammar(internalSplit.sel)) {
        throw new ToolError(`path entry "${entry}" has an invalid selector...`);
    }
    specs.push({ original: entry, clean: internalSplit.path, ranges: selectorLineRanges(internalSplit.sel) });
    continue;
}
const strictSplit = splitPathAndSel(entry);
const split = await splitPathAndSelPreferringLiteral(entry, cwd);
// ...
```

**选择器语法**（来自 `path-utils.ts`）：
- `N` - 单行
- `N-M` - 范围（包含）
- `N+K` - N 开始的 K 行
- `N-` - 从 N 到 EOF
- `..` 作为 `-` 的宽容别名（如 `2724..2727`）
- `:raw` / `:conflicts` - 全文模式
- 复合：`path:1-50:raw` 或 `path:raw:1-50`

### 3.4 Context Before/After

```typescript
// 行 981-982
const normalizedContextBefore = this.session.settings.get("grep.contextBefore");
const normalizedContextAfter = this.session.settings.get("grep.contextAfter");
```

上下文行数从 session 设置读取，然后传递给原生引擎。

---

## 4. 路径解析分支

### 4.1 分支总览

```
                ┌─── isInternalUrl? ──→ resolveInternalSearchInputs()
                │
rawEntries ─────┼─── parseArchivePathCandidates → resolveArchiveSearchPaths()
                │       │
                │       └─── 可 materialize → 临时文件 → searchablePaths
                │       └─── 不可 materialize → unreadable[]
                │
                └─── 普通路径 ──→ resolveToolSearchScope()
                        │
                        ├─── 单文件 ──→ exactFilePaths
                        ├─── 目录 ──→ isDirectory=true + globFilter
                        └─── glob ──→ multiTargets
```

### 4.2 Archive Members

```typescript
// 行 233-309
async function resolveArchiveSearchPaths(...) {
    // 1. parseArchivePathCandidates 检测 archive:member 模式
    // 2. openArchive 打开归档
    // 3. archive.readFile(member.subPath) 提取成员
    // 4. 校验 UTF-8（检测 null byte）
    // 5. 写入临时目录: tmpdir()/omp-search-archive-{idx}-{safeBase}
    // 6. 建立 displayMap: scratchPath → originalSelector
    // 7. 返回 cleanup hook（必须调用）
}
```

**要点**：
- 原生引擎无法直接搜索归档成员，所以先解压到临时文件
- 二进制度员（含 null byte）和非 UTF-8 成员被标记为 `unreadable`
- 搜索完成后通过 `displayMap` 将结果路径映射回原始选择器

### 4.3 Internal URLs

```typescript
// 行 762-853
async function resolveInternalSearchInputs(opts) {
    // 1. 遍历路径，InternalUrlRouter.canHandle() 判断
    // 2. 拒绝 glob 模式的 internal URL
    // 3. 第一次 resolve(pathOnly: true) - 尝试仅获取 sourcePath
    //    - 有 sourcePath → 转为物理路径，标记 immutable
    //    - 无 sourcePath → 第二次 resolve(pathOnly: false) 获取内容
    // 4. expandVirtualInternalResource:
    //    - omp:// 根路径 → 展开为所有 completions
    //    - 其他 → 单个 VirtualSearchResource
}
```

**VirtualSearchResource**：
```typescript
// 行 311-315
interface VirtualSearchResource {
    path: string;      // 显示用路径
    content: string;   // 完整内容
    ranges?: readonly LineRange[];
}
```

### 4.4 External URLs（通过 fetch.ts）

```typescript
// 行 935-945
const materializeExternalUrlForSearch = async (rawPath: string) => {
    const target = parseReadUrlTarget(rawPath);
    if (!target) return undefined;
    const materialized = await materializeReadUrlToFile(
        this.session,
        { path: target.path, raw: target.raw },
        signal,
    );
    materializedExternalPaths.set(rawPath, materialized.path);
    return { sourcePath: materialized.path, immutable: true };
};
```

外部 URL 通过 `fetch.ts` 的 `materializeReadUrlToFile` 下载到本地缓存文件。

### 4.5 Filesystem + Glob

```typescript
// 行 997-1010
const scope = await resolveToolSearchScope({
    rawPaths: searchablePaths,
    cwd: this.session.cwd,
    internalUrlAction: "search",
    trackImmutableSources: true,
    surfaceExactFilePaths: true,
    fanOutFileTargets: true,
    // ...
});
```

返回结构：
- `searchPath`：搜索的基础路径
- `isDirectory`：是否搜索目录
- `multiTargets`：展开后的多 glob 目标列表
- `exactFilePaths`：单文件列表
- `globFilter`：glob 模式
- `missingPaths`：不存在的路径

---

## 5. 与原生引擎的桥接

### 5.1 核心调用参数

```typescript
// 行 1175-1194（单 scope 调用）
result = await grep(
    {
        pattern: normalizedPattern,
        path: searchPath,
        glob: globFilter,
        ignoreCase,
        multiline: effectiveMultiline,
        hidden: true,
        gitignore: useGitignore,
        maxCount: nativeMaxCount,
        contextBefore: normalizedContextBefore,
        contextAfter: normalizedContextAfter,
        maxColumns: DEFAULT_MAX_COLUMN,
        mode: GrepOutputMode.Content,
        maxCountPerFile: nativeMaxCountPerFile,
        signal,                      // ← AbortSignal 透传
        timeoutMs: SEARCH_GREP_TIMEOUT_MS, // ← 30秒超时
    },
    undefined,  // ← logger
);
```

### 5.2 多 Target 迭代

```typescript
// 行 1127-1166
for (const target of targets) {
    const targetResult = await grep({...});
    // 去重：同一物理行可能被多个重叠 target 命中
    const matchKey = `${absolute}\0${match.lineNumber}`;
    if (seenMatchKeys.has(matchKey)) { totalMatches--; continue; }
    seenMatchKeys.add(matchKey);
    const rebased = path.relative(searchPath, absolute).replace(/\\/g, "/");
    matches.push({ ...match, path: rebased });
}
```

### 5.3 超时与 Abort

```typescript
// 行 113
const SEARCH_GREP_TIMEOUT_MS = 30_000;

// 行 1198-1206
try {
    // ... native grep call
} catch (err) {
    if (err instanceof Error && err.message.includes("Aborted: Timeout")) {
        throw new ToolError(
            `Grep timed out after ${SEARCH_GREP_TIMEOUT_MS / 1000}s; narrow paths or pattern...`
        );
    }
    throw err;
}
```

### 5.4 原生层大小限制同步

```typescript
// 行 103-109
const NATIVE_GREP_MAX_FILE_BYTES = 4 * 1024 * 1024;

// 行 1096-1102
const hasLineRangeFilters = pathSpecs.some(spec => spec.ranges);
const nativeMaxCountPerFile = hasLineRangeFilters
    ? Math.max(perFileMatchCap + 1, lineRangeFetchCap(pathSpecs, perFileMatchCap + 1))
    : perFileMatchCap + 1;
const nativeMaxCount = hasLineRangeFilters
    ? Math.ceil(INTERNAL_TOTAL_CAP / (perFileMatchCap + 1)) * nativeMaxCountPerFile
    : INTERNAL_TOTAL_CAP;
```

**Range Fetch Cap 算法**（行 385-394）：
```
对于有 range 的文件，确保能 fetch 到足够多的行使得 range filter 后仍保留 perFileKeep 个匹配。
cap = max(endLine or startLine-1+perFileKeep) over all ranges
最终 clamp 到 NATIVE_GREP_MAX_FILE_BYTES
```

### 5.5 虚拟资源搜索

```typescript
// 行 624-718
async function searchVirtualResources(...) {
    // 对于有 sourcePath 的 internal URL，先走 native grep
    // 对于纯 content 的 virtual resource：
    //   1. 写入临时文件
    //   2. 调用 native grep（小文件）或 nativeChunkedLineIndexes（大文件）
    //   3. 用 JS 重建 context 窗口和 range filter
}
```

### 5.6 超大文件分块搜索

```typescript
// 行 466-537
async function nativeChunkedLineIndexes(...) {
    // 将 >4MB 的内容按行分割为不超过 4MB 的 chunk
    // 每个 chunk 写入临时文件，调用 native grep
    // 单行超过 4MB 的 fallback 到 JS RegExp
}
```

---

## 6. 输出格式化

### 6.1 Match Line Format

```typescript
// match-line-format.ts
export function formatMatchLine(
    lineNumber: number,
    line: string,
    isMatch: boolean,
    options: { useHashLines: boolean },
): string {
    const marker = isMatch ? "*" : " ";
    if (options.useHashLines) {
        return `${marker}${lineNumber}:${line}`;   // *17:const x = 1;
    }
    return `${marker}${lineNumber}|${line}`;       // *17|const x = 1;
}
```

- **Hashline mode**：输出 `LINE:content` 格式，可被文件快照系统追踪和编辑
- **Plain mode**：输出 `LINE|content` 格式，只读展示

### 6.2 Grouped File Output

```typescript
// grouped-file-output.ts:46-86
export function formatGroupedFiles(
    files: string[],
    renderFile: (filePath: string) => GroupedFileSection,
): GroupedFilesOutput {
    // 1. buildPathTree - 将文件列表构造成目录树
    // 2. walkPathTree - 深度优先遍历
    // 3. 为每个事件生成 # 级标题
    //    - 目录: # packages/pkg/src/
    //    - 文件: ## root.ts 或 ### child.ts
    // 4. 插入空白行分隔不同目录/文件
}
```

**输出示例**：
```markdown
# packages/pkg/src/

## foo.ts
1|const x = 1;

## nested/
### bar.ts
1|const y = 2;
```

### 6.3 分页策略

```typescript
// 行 91-98
export const DEFAULT_FILE_LIMIT = 20;
export const MULTI_FILE_PER_FILE_MATCHES = 20;
export const SINGLE_FILE_PER_FILE_MATCHES = 200;
const INTERNAL_TOTAL_CAP = 2000;

// 行 1296-1317
const canPaginate = isMultiScope;
const skipFiles = canPaginate ? Math.min(normalizedSkip, totalFiles) : 0;
const windowFiles = canPaginate ? fileOrder.slice(skipFiles, skipFiles + DEFAULT_FILE_LIMIT) : fileOrder;
const fileLimitReached = canPaginate && totalFiles > skipFiles + DEFAULT_FILE_LIMIT;
// Round-robin 取匹配，避免单文件垄断
const cursors = new Array<number>(lists.length).fill(0);
while (anyAdded) {
    for (let i = 0; i < lists.length; i++) {
        if (cursors[i] < lists[i].length) {
            selectedMatches.push(lists[i][cursors[i]++]);
            anyAdded = true;
        }
    }
}
```

**策略说明**：
- 多文件 scope：每页 20 文件，每文件最多 20 match（通过 `skip` 参数翻页）
- 单文件 scope：最多 200 match（不需要分页，避免截断）
- 结果按文件轮询（round-robin）展开，保证每个文件的上下文交错显示

### 6.4 截断策略

```typescript
// 行 1501-1507
const rawOutput = outputLines.join("\n");
const truncation = truncateHead(rawOutput, { maxLines: Number.MAX_SAFE_INTEGER });
const output = truncation.content;
const truncated = Boolean(
    fileLimitReached || perFileLimitReached || result.limitReached
    || truncation.truncated || linesTruncated,
);
```

- `truncateHead`：当输出超过 TUI 显示上限时，从头部截断（保留尾部最近的匹配）
- `linesTruncated`：单行超长（超过 `DEFAULT_MAX_COLUMN`）标记
- 多重截断标志：`fileLimitReached`、`perFileLimitReached`、`limitReached`

---

## 7. Side Effects

### 7.1 File Snapshot

```typescript
// 行 1405-1415
if (baseDisplayMode.hashLines) {
    for (const relativePath of fileList) {
        if (archiveDisplaySet.has(relativePath) || virtualPathSet.has(relativePath)) continue;
        if (isImmutableSourcePath(absoluteFilePath, immutableSourcePaths)) continue;
        const tag = await recordFileSnapshot(this.session, absoluteFilePath);
        if (tag) hashContexts.set(relativePath, { tag });
    }
}
```

### 7.2 Seen Lines 记录

```typescript
// 行 1459-1462
if (hashContext?.tag) {
    const absoluteFilePath = path.resolve(this.session.cwd, relativePath);
    recordSeenLinesFromBody(this.session, absoluteFilePath, hashContext.tag, modelOut.join("\n"));
}
```

### 7.3 File Recorder

```typescript
// 行 1321
const { record: recordFile, list: fileList } = createFileRecorder();
// ...
for (const match of selectedMatches) {
    const relativePath = formatPath(match.path);
    recordFile(relativePath);  // 记录访问的文件
}
```

---

## 8. 错误处理

### 8.1 ToolError 体系

```typescript
// tool-errors.ts
export class ToolError extends Error {
    constructor(message: string, readonly context?: Record<string, unknown>) {
        super(message);
        this.name = "ToolError";
    }
    render(): string { return this.message; }
}
```

### 8.2 错误类型与用户提示

| 错误场景 | 错误消息 | 处理方式 |
|---------|---------|---------|
| 空 pattern | `"Pattern must not be empty"` | 直接 throw |
| 非法 regex | `"Invalid regex: ..."` | 包装原生错误消息 |
| 非法 selector | `"...has an invalid selector..."` | 详细说明可用语法 |
| Glob + line-range | `"Line-range selector requires a single file, not a glob"` | 直接 throw |
| 路径不存在 | `"Path not found: ..."` | 列举所有缺失路径 |
| 超时 | `"Grep timed out after 30s..."` | 建议缩小范围 |
| Archive 不可读 | `"Cannot search archive member(s)..."` | 建议用 read 工具 |
| 大文件截断 | `"Searched only the first 4MB of large files..."` | 列出具体文件 |
| 二进制/非 UTF-8 archive | `"... (binary archive entry)"` | 标记为 unreadable |
| SSH 目录 | `"search cannot recurse the directory listing..."` | 建议指定具体文件 |

### 8.3 错误转换时机

```typescript
// 行 1198-1228
} catch (err) {
    if (err instanceof Error && /^regex(?: parse)? error/i.test(err.message)) {
        throw new ToolError(err.message.replace(/^regex(?: parse)? error:?\s*/i, "Invalid regex: "));
    }
    if (err instanceof Error && err.message.includes("Aborted: Timeout")) {
        throw new ToolError(`Grep timed out after ${SEARCH_GREP_TIMEOUT_MS / 1000}s...`);
    }
    throw err;
}
```

---

## 9. 数据流时序

```
┌──────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐
│  输入     │────▶│  Schema  │────▶│ Pattern  │────▶│  Paths   │
│  params  │     │ 校验     │     │ 校验     │     │ 归一化   │
│          │     │ (arktype)│     │          │     │ expand   │
└──────────┘     └──────────┘     └──────────┘     └────┬─────┘
                                                       │
                                          ┌────────────┴────────────┐
                                          ▼                         ▼
                                   ┌──────────┐            ┌──────────┐
                                   │ Archive  │            │ Internal │
                                   │ Resolve  │            │ URL Res. │
                                   └────┬─────┘            └────┬─────┘
                                        │                       │
                                        ▼                       ▼
                                   ┌──────────┐            ┌──────────┐
                                   │ Scratch  │            │ Virtual  │
                                   │ Files    │            │ Resource │
                                   └────┬─────┘            └────┬─────┘
                                        │                       │
                                        └───────────┬───────────┘
                                                    ▼
                                            ┌──────────────┐
                                            │  Native Grep │
                                            │  (ripgrep)   │
                                            └──────┬───────┘
                                                   │
                                   ┌───────────────┼───────────────┐
                                   ▼               ▼               ▼
                            ┌──────────┐  ┌──────────┐  ┌──────────┐
                            │ exact    │  │ directory│  │ virtual  │
                            │ file     │  │ scope    │  │ resource │
                            └────┬─────┘  └────┬─────┘  └────┬─────┘
                                 │             │              │
                                 └─────────────┼──────────────┘
                                               ▼
                                        ┌──────────┐
                                        │  Merge   │
                                        │ Results  │
                                        └────┬─────┘
                                             │
                                  ┌──────────┼──────────┐
                                  ▼          ▼          ▼
                            ┌──────────┐ ┌────────┐ ┌────────┐
                            │  Range   │ │  Dedup │ │ Per-   │
                            │  Filter  │ │        │ │ file   │
                            │          │ │        │ │ cap    │
                            └────┬─────┘ └───┬────┘ └───┬────┘
                                 │           │           │
                                 └───────────┼───────────┘
                                             ▼
                                        ┌──────────┐
                                        │ Paginate │
                                        │ (skip)   │
                                        └────┬─────┘
                                             │
                                  ┌──────────┼──────────┐
                                  ▼          ▼          ▼
                            ┌──────────┐ ┌────────┐ ┌────────┐
                            │  Hash    │ │  Trunc │ │  TUI   │
                            │  Header  │ │  Head  │ │ Render │
                            └──────────┘ └────────┘ └────────┘
```

---

## 10. 设计模式总结

| 模式 | 应用位置 | 说明 |
|------|---------|------|
| **Facade** | `GrepTool.execute` | 统一入口，封装复杂的路径解析、搜索、格式化流程 |
| **Strategy** | 路径解析分支 | 不同路径类型（archive/internal URL/external/filesystem）采用不同解析策略 |
| **Template Method** | `searchVirtualResources` / `nativeChunkedLineIndexes` | 骨架固定（写临时文件→native grep→收集结果），细节参数化 |
| **Chain of Responsibility** | `parsePathSpecs` | 内部 URL → strict split → literal-prefer split 的链式处理 |
| **Decorator** | 输出格式化 | 基础匹配行 → +context → +hashline → +truncation 层层包装 |
| **Iterator** | `walkPathTree` | 目录树的迭代器遍历，惰性生成分组 |
| **Memento** | `recordFileSnapshot` | 记录文件快照（hash tag），用于后续可编辑引用 |
| **Observer** | `AgentToolUpdateCallback` | 执行过程中的状态更新回调 |

---

## 11. 对我们项目的借鉴点

### 11.1 架构层面

1. **分层清晰**：输入处理 → 路径解析 → 原生桥接 → 结果后处理 → 输出渲染，每层职责单一
2. **范围一致性**：原生层大小限制（4MB）与 JS 层限制严格同步，避免行为不一致
3. **渐进降级**：原生 grep 失败时 fallback 到 JS RegExp（超大文件分块场景）

### 11.2 工程实践

1. **Side Effect 显式化**：`recordFileSnapshot`、`recordSeenLinesFromBody`、`createFileRecorder` 都是独立模块，易于测试和替换
2. **Memory Safety**：`tempDir` 通过 `finally` + cleanup hook 保证清理，即使搜索失败也不泄漏
3. **Error Context**：`ToolError` 支持 `context` 字段，可携带结构化错误信息
4. **Abort-friendly**：全程透传 `AbortSignal`，包括底层 native 调用

### 11.3 可扩展性

1. **Internal URL 抽象**：通过 `InternalUrlRouter` 协议抽象，新增 URL scheme 无需修改 grep 核心逻辑
2. **Virtual Resource**：纯 content 的资源可通过 `VirtualSearchResource` 接口接入，适用于 artifact/skill 等
3. **Display Mode 分离**：`modelLines` 和 `displayLines` 分离，同一数据可适配不同渲染目标

### 11.4 对我们项目的直接建议

- 如果我们需要实现类似的代码搜索工具，可以复用 **path-utils** 的选择器解析逻辑
- 大文件处理建议采用 **chunked native + JS fallback** 混合策略
- 结果展示建议实现 **hashline mode** 以支持后续编辑操作
- 分页建议使用 **round-robin + per-file cap** 避免单文件垄断结果

---

## 12. 关键代码片段索引

| 片段 | 行号 | 说明 |
|------|------|------|
| `searchSchema` | 73-86 | 参数 Schema 定义 |
| `GrepTool` 类声明 | 887-906 | 工具类实现 |
| `execute` 入口 | 908-914 | 执行方法签名 |
| Pattern 校验 | 920-929 | 输入验证 |
| `parsePathSpecs` | 152-201 | 路径选择器解析 |
| `resolveArchiveSearchPaths` | 233-309 | 归档路径解析 |
| `resolveInternalSearchInputs` | 762-853 | Internal URL 解析 |
| `lineRangeFetchCap` | 385-394 | Range 感知的 fetch budget |
| `searchVirtualResources` | 624-718 | 虚拟资源搜索 |
| `nativeChunkedLineIndexes` | 466-537 | 超大文件分块搜索 |
| Native grep 调用 | 1128-1194 | 原生引擎调用点 |
| Range filter | 1231-1259 | JS 侧 range 过滤 |
| 分页逻辑 | 1296-1317 | skip/limit 实现 |
| File snapshot | 1405-1415 | 快照记录 |
| `formatGroupedFiles` | 46-86 | 目录树分组 |
| `formatMatchLine` | 9-20 | 匹配行格式化 |
| TUI renderer | 1715-1858 | 渲染器实现 |
