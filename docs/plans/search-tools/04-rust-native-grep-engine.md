# 深度研究报告：Rust 原生 Grep 引擎（`grep.rs`）

## 1. Rust 搜索引擎架构

### 1.1 模块结构总览

```
pi-natives (cdylib)
├── lib.rs                  # N-API 模块入口，运行时初始化
├── grep.rs                 # Grep 引擎核心（~3247 行）
├── task.rs                 # 阻塞任务调度 + CancelToken
├── glob_util.rs            # Glob 编译与 fast-path
├── iofs.rs                 # Walker 错误映射 + 扫描缓存失效
├── utils.rs                # 工具函数（clamp_u32 等）
└── pi-walker (crate)       # 目录遍历、gitignore、hidden 过滤
    ├── lib.rs              # WalkRequest / FileCandidate / WalkOrder
    └── cache.rs            # DashMap 目录扫描缓存
```

**外部核心依赖：**

- `grep-matcher`：`Matcher` trait（`find_at` / `is_match`）
- `grep-regex`：基于 `regex` crate 的 `RegexMatcher`（RE2 风格，零宽断言受限）
- `grep-pcre2`：`PcreMatcher`（支持 lookaround / backreference）
- `grep-searcher`：`Searcher` + `Sink` trait（行级迭代，上下文提取）
- `globset`：glob 模式编译
- `parking_lot`：`Mutex`（替代 std::sync::Mutex，减少争用）
- `smallvec`：`SmallVec<[ContextLine; 8]>`（栈内联，减少堆分配）
- `napi` / `napi-derive`：N-API 绑定宏

### 1.2 模块依赖图

```
[TypeScript / Node.js]
        │  N-API 调用
        ▼
+--------------------+
| grep::grep()       | ← GrepOptions<'env>  #napi(object)
| grep::search()     | ← SearchOptions       #napi(object)
+--------+-----------+
         │ 调用 task::blocking()
         ▼
+--------------------+
| task::Blocking<T>  |  libuv 线程池调度
+--------+-----------+
         │  compute() 内调用
         ▼
+--------------------+
| grep_sync()        |  GrepConfig → 正则编译 → 文件扫描
|   ├── build_matcher()       ← grep-regex / grep-pcre2
|   ├── build_searcher()      ← grep-searcher
|   ├── search_sync()         ← 内存搜索
|   └── run_streaming_grep()  ← pi-walker 目录遍历
+--------+-----------+
         │  Sink 回调
         ▼
+--------------------+
| MatchCollector     |  实现 grep_searcher::Sink
|   ├── matched()    |  收集匹配行、上下文、limit 检查
|   └── context()    |  Before / After 上下文分流
+--------------------+
         │  转换
         ▼
+--------------------+
| GrepResult / Match |  #napi(object) → JS 对象
+--------------------+
```

### 1.3 主要 Struct / Enum 定义

#### N-API 导出类型

```rust
// 输出模式：字符串枚举，JS 侧使用 "content" / "count" / "filesWithMatches"
#[napi(string_enum)]
pub enum GrepOutputMode { Content, Count, FilesWithMatches }

// JS 入参：内存搜索选项
#[napi(object)]
pub struct SearchOptions {
    pub pattern: String,
    pub ignore_case: Option<bool>,
    pub multiline: Option<bool>,
    pub max_count: Option<u32>,
    pub offset: Option<u32>,
    pub context_before: Option<u32>,
    pub context_after: Option<u32>,
    pub context: Option<u32>,
    pub max_columns: Option<u32>,
    pub mode: Option<GrepOutputMode>,
}

// JS 入参：文件系统搜索选项
#[napi(object)]
pub struct GrepOptions<'env> {
    pub pattern: String,
    pub path: String,
    pub glob: Option<String>,
    pub r#type: Option<String>,       // 文件类型过滤
    pub ignore_case: Option<bool>,
    pub multiline: Option<bool>,
    pub hidden: Option<bool>,         // 默认 true
    pub gitignore: Option<bool>,      // 默认 true
    pub max_count: Option<u32>,
    pub offset: Option<u32>,
    pub context_before: Option<u32>,
    pub context_after: Option<u32>,
    pub context: Option<u32>,
    pub max_columns: Option<u32>,
    pub mode: Option<GrepOutputMode>,
    pub max_count_per_file: Option<u32>, // 防止热文件饿死
    pub signal: Option<Unknown<'env>>,    // AbortSignal
    pub timeout_ms: Option<u32>,          // 超时
}

// 返回类型
#[napi(object)] pub struct Match { line_number, line, context_before, context_after, truncated }
#[napi(object)] pub struct GrepMatch { path, line_number, line, ..., match_count }
#[napi(object)] pub struct SearchResult { matches, match_count, limit_reached, error }
#[napi(object)] pub struct GrepResult { matches, total_matches, files_with_matches, files_searched, limit_reached, skipped_oversized }
#[napi(object)] pub struct ContextLine { line_number, line }
```

#### 内部核心类型

```rust
// 搜索参数（内部统一表示）
#[derive(Clone, Copy, PartialEq, Eq)]
struct SearchParams {
    context_before: u32, context_after: u32,
    max_columns: Option<u32>, mode: OutputMode,
    max_count: Option<u64>, max_count_per_file: Option<u64>,
    offset: u64, multiline: bool,
}

// 编译后的 matcher 双后端封装
enum CompiledMatcher {
    Rust(RegexMatcher),   // grep-regex
    Pcre(PcreMatcher),    // grep-pcre2
}

// Sink 实现：匹配收集器
struct MatchCollector {
    matches: Vec<CollectedMatch>,
    match_count: u64, collected_count: u64,
    max_count: Option<u64>, offset: u64,
    limit_reached: bool, max_columns: Option<usize>,
    collect_matches: bool,
    context_before: SmallVec<[ContextLine; 8]>,
}

// 文件类型过滤器
enum TypeFilter {
    Known { exts: &'static [&'static str], names: &'static [&'static str] },
    Custom(String),
}
```

### 1.4 Trait 设计

**`grep_searcher::Sink`** 是核心 trait，`MatchCollector` 实现它：

```rust
impl Sink for MatchCollector {
    type Error = io::Error;

    fn matched(&mut self, _searcher: &Searcher, mat: &SinkMatch<'_>)
        -> Result<bool, Self::Error>
    {
        // 1. 增加 match_count
        // 2. 检查 limit_reached → 返回 false 停止搜索
        // 3. 检查 offset → 跳过前 N 条
        // 4. collect_matches=true 时：记录行号、行内容、上下文
        // 5. 检查 max_count → 设置 limit_reached
    }

    fn context(&mut self, _searcher: &Searcher, ctx: &SinkContext<'_>)
        -> Result<bool, Self::Error>
    {
        // Before → 存入 context_before 缓冲区
        // After → 挂到上一个 match 的 context_after
    }
}
```

**`grep_matcher::Matcher`** trait 被 `CompiledMatcher` 实现，实现双引擎透明切换：

```rust
impl Matcher for CompiledMatcher {
    type Captures = NoCaptures;  // 不需要捕获组
    type Error = CompiledMatcherError;

    fn find_at(&self, haystack: &[u8], at: usize)
        -> Result<Option<Match>, Self::Error>
    {
        match self { Self::Rust(m) => ..., Self::Pcre(m) => ... }
    }
}
```

---

## 2. 正则编译

### 2.1 RE2 风格正则（grep-regex）

```rust
fn build_regex_matcher(pattern: &str, ignore_case: bool, multiline: bool)
    -> Result<RegexMatcher, grep_regex::Error>
{
    let build = |line_terminated| {
        let mut builder = RegexMatcherBuilder::new();
        builder.case_insensitive(ignore_case).multi_line(multiline);
        if line_terminated { builder.line_terminator(Some(b'\n')); }
        builder.build(pattern)
    };
    // 先尝试行终止模式（性能更好），失败则回退
    if !multiline && build(true).is_ok() { return Ok(matcher); }
    build(false)
}
```

`RegexMatcherBuilder` 底层基于 `regex` crate（RE2 风格），特点：
- 保证线性时间复杂度
- 不支持 lookaround / backreference
- 支持 `multi_line` 模式（`.` 匹配 `\n`）

### 2.2 PCRE2 回退（grep-pcre2）

```rust
fn build_pcre_matcher(pattern: &str, ignore_case: bool, multiline: bool)
    -> Result<PcreMatcher, grep_pcre2::Error>
{
    let mut builder = PcreMatcherBuilder::new();
    builder.caseless(ignore_case).multi_line(multiline)
        .utf(true).ucp(true).jit_if_available(true);
    builder.build(pattern)
}
```

支持：lookaround、backreference、Unicode 属性。JIT 编译加速（`jit_if_available`）。

### 2.3 build_matcher() — 四级错误恢复策略

```rust
fn build_matcher(pattern: &str, ignore_case: bool, multiline: bool) -> Result<CompiledMatcher> {
    let sanitized = sanitize_braces(pattern);       // 步骤 1: 花括号清理
    let err = match build_regex_matcher(sanitized, ...) {
        Ok(m) => return Ok(CompiledMatcher::Rust(m)),
        Err(e) => e,
    };
    // 步骤 2: PCRE2 回退
    if build_pcre_matcher(sanitized, ...).is_ok() { ... }
    // 步骤 3: 转义未转义括号（针对 unclosed/unopened group 错误）
    if message.contains("unclosed group") || message.contains("unopened group") {
        let escaped = escape_unescaped_parentheses(sanitized);
        if escaped != sanitized {
            // 再次尝试 Rust 和 PCRE2
        }
    }
    // 步骤 4: 最终回退为字面量搜索
    build_regex_matcher(&regex::escape(pattern), ...)
}
```

### 2.4 花括号自动转义（sanitize_braces）

```rust
fn sanitize_braces(pattern: &str) -> Cow<'_, str> {
    // 遍历每个字节：
    // 1. \{...} 属性转义（\p{Greek}、\x{41}）→ 原样保留
    // 2. {N}、{N,}、{N,M} 合法量词 → 原样保留
    // 3. 孤立的 { 或 } → 转义为 \{ / \}
}
```

**设计意图**：用户输入 `${platform}` 或 `a{b}` 时，正则引擎报 "malformed repetition"，自动转义后作为字面量搜索，避免 confusing error messages。

### 2.5 括号自动转义（escape_unescaped_parentheses）

```rust
fn escape_unescaped_parentheses(pattern: &str) -> Cow<'_, str> {
    // 在 regex parse 报 "unclosed/unopened group" 后触发
    // 将所有未转义的 ( ) 转义为 \( \)
    // 保留原有的合法正则其余部分
}
```

**典型场景**：`fetchAnthropicProvider(` — 末尾 `(` 被解析器误认为捕获组开头，转义后变为字面量搜索。

---

## 3. 文件扫描管线

### 3.1 pi-walker 集成

```rust
fn build_grep_walk_request(...) -> Result<pi_walker::WalkRequest> {
    pi_walker::WalkRequest::new(search_path)
        .hidden(include_hidden)           // hidden 文件处理
        .gitignore(use_gitignore)         // .gitignore 过滤
        .skip_git(true)                   // 跳过 .git 目录
        .skip_node_modules(skip_node_modules)
        .follow_links(FollowLinks::Never)
        .detail(WalkDetail::Minimal)      // 默认最小元数据
        .size_hints(SizeHintPolicy::WhenCheap) // 便宜时获取大小提示
        .order(order)                     // Path | Unordered
        .depth(1, usize::MAX)
        .directory_errors(DirectoryErrorMode::SkipSkippable)
        .cache(false)                     // grep 不使用 walker 缓存
        .filter(glob_filter)              // glob 预过滤
}
```

### 3.2 gitignore 处理

- `pi-walker` 内部集成 `ignore` crate
- `WalkRequest::gitignore(true)` 激活 `.gitignore` 解析
- 默认跳过 `.git/` 目录（`skip_git(true)`）
- 可通过 `hidden(false)` 跳过 hidden 文件

### 3.3 Hidden 文件处理

```rust
let include_hidden = options.hidden.unwrap_or(true);
// .hidden 默认 true，与 JS 侧行为一致
// pi-walker 在遍历时过滤以 `.` 开头的条目
```

### 3.4 文件大小提示（SizeHint）

```rust
.size_hints(pi_walker::SizeHintPolicy::WhenCheap)
// 在遍历时尝试获取文件大小（stat 开销可控时）
// 用于 pass 1 前预分拣超大文件
```

---

## 4. 匹配策略

### 4.1 单文件 vs 目录扫描

```rust
if metadata.is_file() {
    // === 单文件路径 ===
    let bytes = read_file_bytes(&search_path, &mut buffer);
    let search = run_search(matcher, bytes, params);
    // 直接返回 GrepResult
} else {
    // === 目录路径 ===
    let results = run_streaming_grep(...);
}
```

单文件路径：
1. 读取文件（支持 oversized prefix fallback）
2. `run_search()` 直接搜索
3. 按 mode 组装结果

### 4.2 Context Lines 计算

```rust
fn resolve_context(context, context_before, context_after) -> (u32, u32) {
    if context_before.is_some() || context_after.is_some() {
        (context_before.unwrap_or(0), context_after.unwrap_or(0))
    } else {
        let value = context.unwrap_or(0);
        (value, value)  // legacy: 对称上下文
    }
}
```

`Sink::context()` 回调：
- `SinkContextKind::Before` → 存入 `MatchCollector.context_before`
- `SinkContextKind::After` → 挂到 `last_match.context_after`

### 4.3 多行模式

```rust
build_searcher(...)
    .multi_line(multiline)
    .before_context(context_before)
    .after_context(context_after)
```

多行模式下：
- `.` 匹配 `\n`
- `^` / `$` 匹配行首/行尾
- 上下文行跨越多行匹配边界

### 4.4 三种 Output Mode

| Mode | 行为 | 上下文收集 |
|------|------|-----------|
| `Content` | 返回匹配行 + 上下文 | ✅ |
| `Count` | 每文件返回总匹配数 | ❌ |
| `FilesWithMatches` | 只返回匹配文件名 | ❌（用 `is_match` 短路） |

### 4.5 max_count_per_file 防饿死

```rust
fn per_file_params(params: SearchParams) -> SearchParams {
    let file_limit = match params.mode {
        OutputMode::Content => {
            let global = params.max_count.map(|max| max.saturating_add(params.offset));
            match (global, params.max_count_per_file) {
                (Some(global), Some(per_file)) => Some(global.min(per_file)),
                (global, per_file) => global.or(per_file),
            }
        },
        OutputMode::Count => None,
        OutputMode::FilesWithMatches => Some(1),
    };
    SearchParams { max_count: file_limit, offset: 0, ..params }
}
```

---

## 5. 性能与安全

### 5.1 两阶段搜索（Pass Architecture）

```
┌─────────────────────────────────────────────────────────┐
│  Pass 1: 正常文件                                    │
│  ├── ReadPolicy::Full  — 完整读取到内存               │
│  ├── 超大文件（有 size hint）→ 直接 defer              │
│  └── 运行时超大文件 → defer 到 Pass 2                 │
├─────────────────────────────────────────────────────────┤
│  Pass 2: 超大文件前缀搜索                              │
│  ├── ReadPolicy::Prefix — 只读前 4MB                  │
│  ├── 仅当内容模式预算未满足时执行                      │
│  └── 结果追加在正常结果之后                            │
└─────────────────────────────────────────────────────────┘
```

```rust
fn process_candidates<M: Matcher + Sync>(candidates, ...) {
    let (normal, oversized_hinted) = candidates
        .into_iter()
        .partition(|file| file_size_hint(file.size)
            .map_or(true, |size| size <= MAX_FILE_BYTES));
    
    // Pass 1: normal files
    let results = run_pass(&normal, ..., ReadPolicy::Full, ...)?;
    
    // Pass 2: deferred oversized
    let deferred = std::mem::take(&mut *state.deferred.lock());
    if !limit_satisfied {
        let oversized = run_pass(&deferred, ..., ReadPolicy::Prefix, ...)?;
        results.extend(oversized);
    }
}
```

### 5.2 文件大小限制

```rust
const MAX_FILE_BYTES: u64 = 4 * 1024 * 1024;  // 4MB
const FILE_CLASSIFICATION_READ_BYTES: u64 = MAX_FILE_BYTES + 1;

fn read_file_prefix(path: &Path, buffer: &mut Vec<u8>) -> io::Result<ReadFile> {
    let window = len.min(MAX_FILE_BYTES);
    read_owned_prefix(file, window, window, buffer)?;
    Ok(ReadFile::Read)  // 只读前缀，剩余丢弃
}
```

### 5.3 线程安全

```rust
struct PassState {
    results: Mutex<Vec<FileSearchResult>>,   // parking_lot::Mutex
    deferred: Mutex<Vec<FileCandidate>>,
    files_searched: AtomicU64,
    skipped_oversized: AtomicU64,
    emitted: AtomicU64,                      // 已发出匹配数，用于早停
}

thread_local! {
    static PARALLEL_GREP_SEARCHER: RefCell<Option<(SearchParams, SearchWorker)>>
        = const { RefCell::new(None) };
}
```

- `PassState` 使用 `parking_lot::Mutex` + `AtomicU64` 实现并发安全
- 线程局部缓存避免重复构造 `Searcher` + buffer
- `rayon` 并行执行候选文件（通过 `pi_walker::execute_candidates_init`）

### 5.4 超时与取消

```rust
let ct = task::CancelToken::new(timeout_ms, signal);
task::blocking("grep", ct, move |ct| grep_sync(config, on_match.as_ref(), ct))
```

- `CancelToken` 封装 `timeout_ms`（chrono 超时）和 `AbortSignal`（JS 侧取消）
- 定期 `ct.heartbeat()` 检查（目录遍历回调 + 文件处理循环）
- 超时前取消 → 返回 `Error::from_reason("Timeout")`

### 5.5 内存限制

- `MAX_FILE_BYTES = 4MB` 硬上限
- `SmallVec<[ContextLine; 8]>` — 栈内联 8 个上下文行，减少堆分配
- `ReadPolicy::Prefix` — 超大文件只映射前 4MB，避免 mmap 整文件

### 5.6 三种搜索执行策略

```rust
fn run_streaming_grep(...) -> Result<...> {
    let stop_after_matches = streaming_stop_after(params);
    match stop_after_matches {
        None => run_parallel_streaming_grep(...),  // 无限制 → 并行流式
        Some(stop) if stop <= 64 || workers <= 1 => 
            run_sequential_grep(..., Some(stop)),  // 小预算或单核 → 顺序
        Some(stop) => 
            run_windowed_streaming_grep(..., stop), // 大预算 → 窗口化流式
    }
}
```

- **无限制**：`run_parallel_streaming_grep` — Unordered walk + rayon 并行
- **小预算（≤64）**：`run_sequential_grep` — Path order + 单线程
- **大预算**：`run_windowed_streaming_grep` — 512 文件窗口，按路径排序

---

## 6. FFI 边界

### 6.1 N-API 暴露

```rust
#[napi]
pub fn search(content: Either<JsString, Uint8Array>, options: SearchOptions) -> SearchResult {
    // 零拷贝：JsString::into_utf8() / Uint8Array::as_ref()
    search_sync(utf8.as_slice(), options)
}

#[napi]
pub fn grep(
    options: GrepOptions<'_>,
    #[napi(ts_arg_type = "((error: Error | null, match: GrepMatch) => void) | undefined | null")]
    on_match: Option<ThreadsafeFunction<GrepMatch>>,
) -> task::Promise<GrepResult> {
    // 1. 解构 GrepOptions → GrepConfig
    // 2. 创建 CancelToken
    // 3. task::blocking() 调度到 libuv 线程池
}
```

### 6.2 ThreadsafeFunction 回调

```rust
// 结果聚合后，批量调用 JS 回调
if let Some(callback) = on_match {
    for grep_match in &matches {
        callback.call(Ok(grep_match.clone()), ThreadsafeFunctionCallMode::NonBlocking);
    }
}
```

- 回调在 libuv 线程池线程执行，不阻塞主线程
- `NonBlocking` 模式避免回调阻塞搜索

### 6.3 取消传播

```rust
// JS AbortSignal → Rust CancelToken
let ct = CancelToken::new(timeout_ms, signal);
// signal.on_abort(move || abort_token.abort(AbortReason::Signal))
```

### 6.4 类型安全

- `GrepOptions<'env>` — lifetime 绑定 napi `Env`
- `Either<JsString, Uint8Array>` — 支持字符串或 Buffer 零拷贝输入
- `Option<ThreadsafeFunction<GrepMatch>>` — 可选回调

---

## 7. 缓存机制

### 7.1 pi-walker 扫描缓存（directory-scan cache）

```rust
// cache.rs 核心结构
struct CacheKey { root: PathBuf, options: WalkOptions }
struct CacheEntry { created_at: Instant, entries: Vec<CollectedEntry> }

static SCAN_CACHE: LazyLock<DashMap<CacheKey, CacheEntry>> = LazyLock::new(DashMap::new);
```

**配置项（环境变量）：**

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `FS_SCAN_CACHE_TTL_MS` | 1000 | 缓存条目 TTL |
| `FS_SCAN_EMPTY_RECHECK_MS` | 200 | 空结果快速重查间隔 |
| `FS_SCAN_CACHE_MAX_ENTRIES` | 16 | 最大缓存条目数 |
| `PI_WALK_WORKERS` | 4 | 遍历并行度 |

### 7.2 grep 的缓存策略

```rust
// grep 遍历显式关闭 walker 缓存
fn build_grep_walk_request(...) -> Result<WalkRequest> {
    WalkRequest::new(search_path)
        ...
        .cache(false)  // ← grep 不使用缓存
        ...
}
```

**原因**：grep 结果高度依赖 pattern，而目录结构缓存只加速文件列表获取。grep 的"缓存"体现在：
1. `PARALLEL_GREP_SEARCHER` thread_local 缓存 — 避免重复构造 Searcher
2. `file_size_hint` 预分拣 — 利用 walker 的 size hint 提前 defer 超大文件

### 7.3 缓存失效

```rust
// iofs.rs
#[napi]
pub fn invalidate_fs_scan_cache(path: Option<String>) {
    match path {
        Some(path) => pi_walker::invalidate_path_string(&path),
        None => pi_walker::invalidate_all(),
    }
}
```

文件变更后由 JS 侧主动失效缓存。

---

## 8. 搜索流程状态机

```
[grep() N-API 入口]
        │
        ▼
[解构 GrepOptions → GrepConfig]
        │
        ▼
[创建 CancelToken（timeout_ms + AbortSignal）]
        │
        ▼
[task::blocking("grep", ct, ...)]
        │
        ▼
┌───────────────────────────────────────────────────────────┐
│ grep_sync(config, on_match, ct)                            │
└───────────────────────────────────────────────────────────┘
        │
        ▼
[build_matcher(pattern, ignore_case, multiline)]
        │
        ├─► 1. sanitize_braces() → 清理花括号
        ├─► 2. build_regex_matcher() → Rust RE2
        ├─► 3. build_pcre_matcher() → PCRE2 回退
        ├─► 4. escape_unescaped_parentheses() → 括号修复
        └─► 5. regex::escape() → 最终字面量回退
        │
        ▼
[resolve_search_path() → 绝对路径]
        │
        ▼
[metadata 检查]
        │
        ├── is_file() ─────────────────────────┐
        │   ├── type_filter 检查              │
        │   ├── read_file_bytes()             │
        │   │   ├── Read → 搜索              │
        │   │   ├── Oversized → prefix 搜索  │
        │   │   └── Skipped → 空结果         │
        │   ├── run_search()                  │
        │   └── 按 mode 组装 GrepResult       │
        │                                      │
        └── is_dir() ─────────────────────────┤
            ├── build_grep_walk_request()     │
            │   ├── hidden / gitignore        │
            │   ├── glob / type_filter        │
            │   └── skip_node_modules         │
            ├── collect_grep_candidates()     │
            │   └── pi_walker 遍历           │
            ├── run_streaming_grep()          │
            │   ├── 无限制 → 并行流式         │
            │   ├── 小预算 → 顺序流式         │
            │   └── 大预算 → 窗口化流式       │
            │       ├── Pass 1: 正常文件      │
            │       └── Pass 2: 超大文件前缀 │
            ├── aggregate_parallel_results()  │
            │   ├── Content mode → push_content_matches
            │   ├── Count mode → push_count_match
            │   └── FilesWithMatches → push_file_match
            └── 触发 on_match 回调            │
                                                ▼
                                    [GrepResult 返回 JS]
```

---

## 9. 对 Go 项目的借鉴点

### 9.1 核心设计模式

| 借鉴点 | Rust 实现 | Go 映射建议 |
|--------|-----------|-------------|
| **双引擎正则回退** | `CompiledMatcher::Rust/Pcre` — RE2 → PCRE2 → 字面量 | `regexp` + `pcre2` 库三级回退，花括号/括号自动修复 |
| **Sink 模式匹配** | `grep_searcher::Sink` trait — 流式匹配回调 | Go 的 `io.Writer` 或 `chan Match` 模式 |
| **两阶段搜索** | Pass 1 正常文件 + Pass 2 超大文件前缀 | 同上，避免大文件阻塞小结果 |
| **线程局部缓存** | `thread_local! PARALLEL_GREP_SEARCHER` | Go: `sync.Pool` 复用 `Searcher` + buffer |
| **三种执行策略** | 并行流式 / 顺序流式 / 窗口化流式 | Go: 根据 `max_count` 阈值选择 `goroutine` 数 |
| **TypeFilter 枚举** | `Known` 内置类型 + `Custom` 用户类型 | Go: 预定义 map + 动态扩展 |
| **SmallVec 优化** | `SmallVec<[ContextLine; 8]>` 栈内联 | Go: 固定大小数组 + `copy` 避免切片分配 |

### 9.2 具体可借鉴的代码结构

#### Go 版 CompiledMatcher 双引擎

```go
type CompiledMatcher struct {
    rust *regexp.Regexp
    pcre *pcre2.Regexp
    mode MatcherMode
}

func BuildMatcher(pattern string, ignoreCase, multiline bool) (*CompiledMatcher, error) {
    // 1. sanitizeBraces(pattern)
    // 2. regexp.Compile() → RE2
    // 3. pcre2.Compile() → 回退
    // 4. escapeParentheses() → 二次修复
    // 5. regexp.QuoteMeta() → 最终字面量
}
```

#### Go 版 Sink 接口

```go
type Sink interface {
    Matched(line []byte, lineNum uint64) bool  // 返回 false 停止
    Context(kind ContextKind, line []byte, lineNum uint64)
}
```

#### Go 版两阶段搜索

```go
func processCandidates(candidates []FileCandidate, matcher Matcher, ...) {
    // Pass 1: normal files (size <= 4MB)
    // Pass 2: deferred oversized (prefix 4MB)
}
```

### 9.3 性能优化策略

1. **文件大小预分拣**：遍历时获取 `size hint`，将超大文件直接 defer
2. **匹配预算早停**：`stop_after_matches` 在遍历阶段就检查，避免不必要的搜索
3. **上下文行栈内联**：`SmallVec` 避免 80% 的堆分配（大多数匹配无上下文）
4. **并行度自适应**：`pi_walker::should_parallelize(len)` 根据候选数决定是否并行
5. **Glob fast-path**：常见模式（`*.ts`、`**/*.rs`）用预分类而非全 GlobSet 匹配

### 9.4 错误恢复哲学

**原则**：宁可降级为字面量搜索，也不向用户暴露正则错误。

```
用户输入: "fetchProvider("
    ↓ RE2 报错: "unclosed group"
    ↓ 自动转义括号: "fetchProvider\("
    ↓ 成功作为字面量搜索
```

这种设计对 IDE/Agent 场景至关重要——用户可能输入任意文本片段，不应因特殊字符报错。

### 9.5 Go 项目优先实现清单

| 优先级 | 功能 | 复杂度 |
|--------|------|--------|
| P0 | 内存搜索（`search()`）— RE2 正则 + 上下文 + limit/offset | 低 |
| P0 | 文件大小限制（4MB）+ 超大文件前缀搜索 | 低 |
| P1 | 目录遍历 + gitignore/hidden 过滤 | 中 |
| P1 | 三种输出模式（content/count/filesWithMatches） | 中 |
| P1 | 多行正则支持 | 中 |
| P2 | 并行搜索（goroutine pool）+ 线程局部 Searcher 缓存 | 中 |
| P2 | 超时/取消传播（context.Context） | 低 |
| P3 | PCRE2 回退（Go 可用 `github.com/glenn-brown/golang-pcre2`） | 高 |
| P3 | 窗口化流式搜索（大预算场景） | 高 |
