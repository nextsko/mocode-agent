# 17 · 内部资源协议（`mocode://`）

> 状态：**已实现**（解析器 + 注册表 + 5 个 provider + View 接入 + 测试）
> 相关：`docs/design/16-learn-tool.md`（经验沉淀写入 `mocode://exp/` 能读到的地方）
> 上游：`docs/architecture/07-native-browser-runtime.md`（browser provider 的来源）

---

## 1. 问题：一个前缀特例，不是一个协议

改造前，`mocode://` 的全貌是：

| 位置 | 内容 |
|---|---|
| `internal/core/skills/embed.go:12` | `const BuiltinPrefix = "mocode://skills/"` |
| `internal/core/tools/core/fs/view.go:81` | `if strings.HasPrefix(params.FilePath, skills.BuiltinPrefix)` |
| `internal/core/tools/core/fs/view.go:389` | `"builtin/" + strings.TrimPrefix(path, BuiltinPrefix)` |

即：**一个常量 + 一个特例分支 + 一次字符串拼接**。后果：

1. **只有 View 认识它**。`read_files` / `grep` / `glob` / `ls` 收到 `mocode://...` 会当作相对路径在磁盘上找，然后失败。
2. **无法扩展**。加第二个命名空间意味着再加一个前缀判断，并且每个消费点都要加一份。
3. **文档被迫写死**。prompt 里只能写「`mocode://` **不是** URL、网络地址或 MCP 资源」——因为确实不是，它只是个魔法字符串。

所以这不是「再加几个前缀」，而是**把魔法字符串提升为协议**：一个 `scheme://authority/path`，authority 由 provider 实现，注册表负责路由。

## 2. 设计

```
mocode://<authority>/<path>[?<query>]
```

| authority | 内容 | 实现 |
|---|---|---|
| `skills` | 内置（embed）+ 用户目录的技能 | `skills.NewURIProvider` |
| `docs` | 文档根（设计/架构/指南） | `uri.NewTreeProvider` |
| `exp` | 经验笔记库（`.exps/` 约定） | `uri.NewTreeProvider` |
| `github` | 公开 GitHub 仓库的文件与目录 | `internal/core/uri/github` |
| `browser` | 活的浏览器会话 | `internal/core/browser` |
| `memory` | 会话记忆 + 全局记忆（根路径就是索引） | `internal/core/memory` |

### 2.1 分层：内核零依赖，provider 各自归位

```
internal/core/uri/           ← 内核：解析 + 注册表 + 通用树 provider（不依赖任何子系统）
├── uri.go        URI 类型、Parse、规范化、校验
├── registry.go   Provider 接口、Registry、Content、Listing 渲染
└── tree.go       基于 fs.FS 的通用 provider（docs/exp/skills 共用）

internal/core/skills/uriprovider.go    ← skills provider（它最懂 SKILL.md）
internal/core/uri/github/github.go     ← github provider（它需要 HTTP 客户端）
internal/core/browser/uriprovider.go   ← browser provider（它需要活的 Session）
```

**内核不 import 任何子系统**：provider 由**组装根**（`internal/core/tools/registry.go` 的
`buildURIRegistry`）注册。这样「一个会话能访问哪些命名空间」集中在一处可见，而不是散落在用到它们的工具里。

### 2.2 通用树 provider：三种磁盘形资源其实是一件事

`skills` / `docs` / `exp` 都是「若干个目录，按优先级查找」。抽象成 `[]Tree{Label, fs.FS}`：

- **`fs.FS` 而不是目录路径**，所以同一个 provider 同时服务 embed 文件系统（内置技能）、磁盘目录（docs/exp）、以及将来任何能表示成 `fs.FS` 的东西。
- **顺序即覆盖语义**：skills 把内置树放前面、用户目录放后面，读行为可预测。
- `nil` 的树被静默跳过，所以调用方可以直接传「可能不存在的可选目录」而不用先判断。

### 2.3 列表即发现

**不带 path 的 URI 就是列目录**，这是 agent 的主要发现手段：

```
mocode://skills          → 已安装的技能（每个带技能自己的 description gist）
mocode://docs            → 文档根
mocode://github/o/r      → 仓库根
mocode://browser         → 该命名空间能读的资源（没会话时明确说明）
```

不需要新工具、不需要新 action：**「读一个目录」本来就是 View 的语义**。

`firstLineOf` 会**跳过前置 YAML frontmatter**——否则每个 SKILL.md 的摘要都会是 `name: jq`，
对「要不要打开它」这个决策毫无帮助。

### 2.4 严格解析：宁可报错，不可静默降级

`Parse` 对畸形引用**一律报错**，而不是降级成文件路径：

- 没有 authority、authority 带空格、缺 `//`、查询串非法 → 全部拒绝。
- **路径穿越（`..`、`.`、反斜杠分隔、控制字符）在内核集中拒绝**，不委托给 provider：
  同一条路径要在 embed 文件系统和克隆下来的仓库里解析，那里没有 OS 帮忙挡 `..`。
- 反复的前后斜杠被折叠（`mocode://docs//` = 根列表，这是显然意图），但**路径中间的**
  空段是笔误，拒绝。

为什么严格：一个静默变成相对路径的引用会以「file not found」的形式失败，
把 agent 引向排查路径 bug，而不是看见自己刚打错的字。

### 2.5 错误必须可自我修正

| 情况 | 返回 |
|---|---|
| 未知 authority | 列出所有已知命名空间 |
| 资源不存在 | 列出**查过的每一棵树**（`builtin` / `user` / 具体目录） |
| 未知 browser 资源 | 列出可用资源名 |
| 没会话却读 `mocode://browser/*` | 指明 `browser` 工具 + `action=launch` |

这些都是**软错误**（`NewTextErrorResponse`）：模型能自己改，不该让整个回合失败。

## 3. GitHub：为什么用 raw 而不是 clone

仓库里已有的 `crawler.Downhub` 用 go-git 把**整个仓库**浅克隆进内存——这对
「把这个仓库的文档都给我」是对的取舍，对「给我看这一个文件」是错的。
所以单文件走 `raw.githubusercontent.com`（无需 git、无需磁盘、无克隆预算），
**目录列表**才走 `contents` API。

- `?ref=` 默认 `HEAD`，可钉 branch/tag/sha。
- **路径按段转义**：整串转义会把 `/` 变成 `%2F`，请求一个不存在的单段路径然后 404。
- 图片扩展名返回 `KindImage`，所以仓库里的截图能直接在 View 里看。
- 未认证 API 限 60 次/小时。**403 会被翻译成限流提示并指向 `GITHUB_TOKEN`**——
  裸 403 会被读成权限问题，把 agent 引向错误的排查方向。
- token 取 `GITHUB_TOKEN` / `GH_TOKEN`，无需改配置。

## 4. 顺带修掉的真 bug

改造中发现并修复了一个**与协议无关但一直存在**的缺陷：

传给 View 的 `skillsPaths` 是**未展开**的（`~` / `$VAR`），而技能发现侧
（`coordinator_skills.go:44`、`prompt.go:166`）会先 `infra.Long` + `Resolver().ResolveValue` 展开。
于是 `isInSkillsPath` 拿到 `~/.config/mocode/skills` 去做 `Abs`+`EvalSymlinks` 会失败，
**技能目录免审批、免尺寸限制的例外静默失效**。

修复：把包含判定统一到 `skills.SkillsDirIn`（一处实现），并在组装处 `expandPaths` 展开一次。
同时用 `TestSkillsDirIn` 钉住「共享前缀的兄弟目录不算在内」（`/root-extra` 对 `/root`）这个边界。

## 5. 交付物

| 文件 | 作用 |
|---|---|
| `internal/core/uri/{uri,registry,tree}.go` | 内核 |
| `internal/core/skills/uriprovider.go` | skills 命名空间 + `SkillsDirIn` + `ExpandDirs` |
| `internal/core/uri/github/github.go` | github 命名空间 |
| `internal/core/browser/uriprovider.go` | browser 命名空间 |
| `internal/core/tools/core/fs/view.go` | 用注册表分发替换前缀特例；`readBuiltinFile` 删除 |
| `internal/core/tools/registry.go` | `buildURIRegistry` 组装根 + 路径展开 |
| `internal/core/config/config.go` | 新增 `docs_paths` / `exp_paths` |

测试：`uri_test.go`（解析/严格性）、`provider_test.go`（注册表/树/列表）、
`github_test.go`（假上游，含转义与限流）、`skills/uriprovider_test.go`（含包含判定）、
`browser/uriprovider_test.go`（含真机会话）。

## 6. 两个测试暴露的真 bug（记录在案）

**① `launch` 带 URL 会挂到 `about:blank`。**
Chrome 的命令行 URL 是**异步**生效的，而我们在读到 `DevTools listening on` 后立刻
`Target.getTargets` —— 此时导航还没提交，于是选中了启动时那个空白标签页，会话静默持有错误的页面。

修复分两步：`pickTarget` **优先选非空白页**（`isBlankURL`），并且 `launch` **不再把 URL 作为命令行参数**，
改为在 attach 之后对**我们实际挂上的那个页**执行导航。确定性，且只开一个标签页。

**② 未知 browser 资源被报成「没会话」。**
资源名是否合法与会话状态无关。先校验名字再查会话，否则一个笔误会把 agent 支去启动一个它根本不需要的浏览器。

**③ `isNotExist` 用 `==` 比哨兵值，在 Windows 上失效（用真数据烟测才暴露）。**
`os.DirFS` 会把失败包进 `*fs.PathError`，而 `fs.ErrNotExist == err` 不脱壳，于是字符串兜底也匹配不上
Windows 的 `The system cannot find the file specified`。后果有两层：

- `mocode://docs/nope.md` 返回的是**裸 OS 错误**，而不是「`nope.md`（查过 docs）」——`uriGuidance`
  因此也识别不出 `ErrNotFound`，模型拿不到任何指引；
- 更严重：**多树搜索会在第一棵树的 miss 处中止**，而不是继续找下一棵。内置技能树是 `embed.FS`
  （返回裸哨兵）所以测试恰好通过，而 `docs`/`exp` 只有 `os.DirFS`，在生产路径上就是坏的。

修复：改用 `errors.Is(err, fs.ErrNotExist)`。补了三个回归测试，其中一个专门断言 OS 文案不得泄漏。

## 7. 核验清单

```bash
go build ./...                # ✅
go vet ./internal/core/...    # ✅
go run ./scripts/layercheck   # ✅
go test ./...                 # ✅ 全仓通过
```

手工验收（需要真实 workspace）：

1. `view mocode://skills` —— 列出已安装技能，每个带 description gist（不是 `name: x`）。
2. `view mocode://docs/architecture` —— 列出架构文档目录。
3. `view mocode://github/cli/cli/README.md` —— 读到 GitHub 上的文件。
4. `view mocode://github/o/r/nope` —— 报错里含 owner/repo/ref 提示，而非裸 404。
5. `view mocode://bogus/x` —— 报错里列出全部已知命名空间。
6. `browser action=launch` → `view mocode://browser/text` —— 拿到**渲染后**的页面文本。

## 8. 明确不做

- **不引入签名/权限模型**：命名空间是只读的。写路径仍归 `write`/`edit`/`learn`，各自有审批。
- **不做 `mocode://` 的写操作**：没有 `PUT`。写文件有更好的专用工具，混进 URI 只会让审批语义含糊。
- **不做缓存**：`github` 每次重新取。加缓存要面对失效策略，而这里的调用量不值得。
- **`grep`/`glob`/`ls` 仍未接协议**：它们扫描的是目录树，对「单个资源」的语义不成立。
  真实需求是「在 GitHub 仓库里搜」，那是另一件事（`sourcegraph` 已经覆盖公开仓库检索）。
