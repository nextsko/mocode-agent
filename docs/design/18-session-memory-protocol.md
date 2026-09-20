# 18 · 会话记忆协议（文件为基座）

> 状态：**已实现并闭环验证**（存储 + 索引 + 萃取 + 工具 + `mocode://memory/` + prompt 注入）
> 上游：`docs/architecture/00-hermes-blueprint.md` §4 B06、§7 DAG（B05 store → B02 session → **B06 memory** → B10 skills）、§10-1、§10-5
> 相关：`docs/design/16-learn-tool.md`（skill 沉淀）、`docs/design/17-internal-uri-protocol.md`（`memory` 命名空间）

---

## 1. 起点：仓库里「记忆」的真实状态

侦察结论与文档声称**严重不符**，先把事实钉住：

| 声称 | 实际 |
|---|---|
| `docs/plans/self-evolution/01-architecture-design.md` 说已有 `memory.Service`、**6 个 fantasy.AgentTool**、`internal/core/knowledge/memory/` | **全都不存在**。该目录不存在，无 `memory.Service`，无任何 tool |
| `memory_add`/`memory_update`/`memory_delete`/`memory_clear`/`memory_search`/`memory_load` | 名字出现在 `config.allToolNames()`、10 个 mode 模板、`mocode-config` skill 表格里，**0 个实现** |
| `internal/domain/memory/` | 4 个文件**全是类型**（`Kind`/`Memory`/`Entry`/`GenerateMemoryID` + 7 个 DTO + 6 个 sentinel error），无 I/O |
| `internal/store/memory_store.go`（191 行，`<ProjectDir>/memory/entries.jsonl`） | **生产代码零调用**，唯一调用方是它自己的测试 |

所以这不是「接线」，是**从零实现**。同时 `mocode://exp/` 通道已接线但**空数据**——`exp-recorder` skill 定义了 `.exps/NNN-<title>.md` 约定，却没有任何代码去写或索引它。

**决策：不复活那 6 个幽灵工具。** 它们当年烂尾正是因为把一个概念拆成六个近乎同名的入口，逼调用方先猜该用哪个。用一个工具 + action 枚举（`learn` 与 `browser` 已验证的范式）。

## 2. 存储：文件为基座

```
<root>/index.md                              全局索引（自动维护）
<root>/global/<name>.md                      全局记忆：持久、跨项目
<root>/sessions/<session>/<name>.md          会话记忆：属于产生它的会话
<root>/.archive/<name>.md                    遗忘的条目，保留可恢复
```

`<root>` 默认 `<data dir>/memories`（Linux `~/.local/share/mocode/memories`，Windows `%LOCALAPPDATA%\mocode\memories`），由 `options.memory_dir` 覆盖。

**为什么是单一 root 而不是 `[]string` 搜索路径**（像 skills/docs/exp 那样）：记忆有代码自己拥有的**结构化布局**（`global/` + `sessions/` + `index.md`），索引依赖这个布局。多 root 会把索引撕成几份，而不是扩展它。

### 2.1 文件名 = 时间 + 会话 + slug

```
20260914T123045-1a2b3c4d-flaky-go-tests-caused-by-a-shared-temp-dir.md
└─ 时间戳 ─┘ └会话┘ └──────────── slug ────────────┘
```

时间在前，所以 `ls` 天然按时间排序、无需工具。会话前缀让**萃取后的副本仍然可追溯**到它来自哪个会话。`sessionShort` 会**净化**合成会话 ID（`<msgID>$$<callID>` 含路径不安全字符）——取第一段路径安全的字符并截断 8 位。

### 2.2 原子写 + 归档而非删除

- **temp + rename**（`writeFileAtomic`），并且 `renameOver` 在 Windows 上对 `os.ErrPermission` 指数退避重试 5 次——刚关闭的句柄可能仍被索引器持有。
- **没有锁文件**：条目是彼此独立的文件，写入按文件原子，索引由扫描派生。丢失一次索引更新会在下次 rebuild 自愈，而不是变成需要修复的损坏。
- **落盘前往返校验**：`Render` 出来的字节必须先能被 `ParseEntry` 读回。
- **`forget` 是移动到 `.archive/`**，不是删除。会被一次误操作摧毁的记忆，是 agent 不敢写的记忆。`.archive` 带点号 → 不出现在列表里，也不出现在 URI 浏览视图里。

## 3. 「全局记忆内置自动索引会话记忆的位置」

这是需求的核心。索引由 `Indexer` 维护：

```go
// 蓝图 §10-1：纯文件 + 接口式实现（里氏替换）
type Indexer interface {
    Rebuild(ctx context.Context) (*Index, error)
    Render(idx *Index) []byte
}
```

蓝图 §10-1 明确要求「文件扫描为首发实现，未来 SQLite/FST 可无侵入替换」。所以**上层只把索引当作值**，从不假设它的文件形状。

索引文档记录：

```markdown
## Global memories
| Date | Title | Kind | Tags | File |

## Session memories
Where each session's memory lives. Memory written in one session is reachable from any
other through these locations.

| Session | Entries | Latest | Location |
| `11111111` | 3 | 2026-09-14 12:30 | `sessions/11111111/` |

### Session `11111111`
Directory: `sessions/11111111/`
| Date | Title | ... |
```

**位置表是这份索引存在的理由**：没有它，一个会话写下的记忆在别的会话里就无从发现（前缀是 8 位短 ID，没人猜得到）。

**确定性**：`Render` 对同一棵树必须产出**逐字节相同**的文档——无 map 迭代序、除显式 `RebuiltAt` 行外无时间依赖。否则索引不可 diff，任何对它的测试也不证明任何事。

**自动维护**：每次 `write`/`extract`/`forget` 后调 `UpdateIndex`，所以索引是自维护的，而不是一件需要谁记得去跑的事。

**索引过期自愈**：`mocode://memory/` 的根**不是**读磁盘上的 `index.md`，而是**现场 Rebuild + Render**——磁盘上的索引一次手改就可能撒谎。手写丢进树里的文件因此无需任何工具调用就进入索引。

## 4. 萃取：会话 → 全局

`extract` 是**复制而非移动**：会话保留自己对发生过什么的记录，全局层获得值得带走的那部分。移动会让会话自己的历史变成谎话。

**幂等**：提升后的文件名锚定在**源条目**的时间上，而不是萃取时刻。所以重复萃取同一份不会铸造副本——否则 agent 每重新考虑一次，全局层就会被近似重复的条目填满。`promoted_from` 记录来源路径。

**provenance 语义**（一个刻意的选择，已写进类型文档并在测试里钉住）：萃取出来的全局条目，`session` 字段是**知识来自哪个会话**，不是**谁提升的**。记忆是关于经历的证词，后来决定保留它的那个会话不是它要证明的东西。提升者不记录——`promoted_from` 已经携带了来源路径。

**跨会话萃取**：无 `path` 时优先提升**当前会话**最近的条目；当前会话为空时回退到**任意会话**最近的条目，并在响应里点名来源会话。理由：能**看见**别的会话记忆的会话，必须也能提升它，否则跨会话流程不可用。这条是闭环测试逼出来的——见 §7。

## 5. 两层与默认值

`write` 默认 `scope=session`。**这是安全方向**：之后提升很容易，反向不行。

`kind` ∈ `note`/`lesson`/`decision`/`fact`，默认 `note`；空值默认而不是报错——调用方不该为了记一个念头而先选分类。

## 6. 读：`mocode://memory/` 与 prompt digest

| 入口 | 得到 |
|---|---|
| `view mocode://memory/` | 现场派生的索引（完整） |
| `view mocode://memory/global/<file>.md` | 单条全局记忆 |
| `view mocode://memory/sessions/<s>/<file>.md` | 单条会话记忆 |
| `memory` 工具 `list`/`search` | 结构化列举与检索 |

**prompt 注入的是有界 digest**（`memoryPromptBudget = 4000` 字节），不是完整索引：条数与分层概况、全局条目标题、**每个会话记忆的位置**，以及「要细节去读 `mocode://memory/`」。这与 skill 的分层注入是同一个取舍——记忆不能随条数增长把任务挤出上下文。digest 在截断处**明说被截断**，不留半句话。

构造 prompt 时 digest 失败只记 warning，不中断：记忆是辅助，没有记忆的 prompt 好过没有 prompt。

## 7. 闭环验证（不是空实现）

四层测试，全部在 `go test ./...` 里跑：

| 层 | 文件 | 覆盖 |
|---|---|---|
| 条目 | `memory_test.go` | 文件名 = 时间+会话+slug、合成会话 ID 净化、RelDir 分层、校验、**Render↔Parse 往返**（含 YAML 敌意标题）、无 frontmatter 的手写文件仍可读、坏日期不吞正文 |
| 存储 | `store_test.go` | 分层落盘、路径穿越拒绝、坏文件跳过不致命、列表过滤/排序、检索按字段排序、归档而非删除、索引确定性、digest 有界 |
| 萃取 | 同上 | 复制不移动、**幂等**、覆盖 title/body/kind/tags、拒绝已全局、整会话萃取 |
| 工具 + 协议 | `agent/memory_test.go` | **完整闭环**（见下） |

**`TestMemory_ClosedLoop` 的十步**（这就是「闭环」的定义）：

1. 会话一通过工具 `write` 一条记忆
2. 磁盘上确实存在，文件名 `20260914T123045-11111111-flaky-go-tests-caused-by-a-shared-temp-dir.md`
3. 索引已被刷新，且**记录了 `sessions/11111111/`**
4. **另一个会话**（共享 root、独立 store）能用 `search` 找到它、能在 `list` 里看到它的位置
5. 能通过 `mocode://memory/` 读到索引，通过 `mocode://memory/sessions/…/x.md` 读到正文（`scope: session`）
6. 会话二 `extract` 把它提升为全局
7. 读 `mocode://memory/global/…/x.md` 得到 `scope: global` + `promoted_from: <源路径>`；**会话副本仍在**
8. 再 `extract` 一次 → 「already in global memory」，全局仍只有 1 条
9. **第三个会话**的 prompt digest 里出现这条持久知识
10. `forget` → `.archive/` 里确实有那个文件

另有 `TestClosedLoop_SecondSessionSeesFirstSessionsMemory`（memory 包层，纯存储 API）和 `TestClosedLoop_HandDroppedFileIsIndexed`（手写文件无需工具调用即入索引）。

### 7.1 闭环测试抓到的真问题

**① `extract` 无法提升别的会话的记忆。** 原实现只在**当前会话**里找最近条目；会话二看得见会话一写的记忆，却因为没有精确路径而提不了。跨会话流程直接不可用。已改为回退到任意会话最近条目并点名来源（§4）。

**② `Alive()` 在 `Close()` 之后仍为真（既有竞态）。** 这是上一轮浏览器工作引入的：`cdpConn.closed` 由读循环**异步**关闭，`Close()` 返回时读循环可能还没察觉，于是刚关闭的会话报告自己活着。用 `sync.Once` 让 `Close` 或读循环**先到者**关闭它——`-=count=5` 压测通过。

**③ `Title`/`times` 对齐错误**（我的测试数据）——两次都是断言写错而非代码错，已修正，并把「萃取条目的 session 是来源会话」这条语义显式写进类型文档 + 测试。

## 8. 不做（明确范围）

- **不复活 `memory_*` 六工具**；`config.allToolNames()` 里那 6 个名字**仍然在**（改动它们要同步 10 个 mode 模板 + `readOnlyTools`，是独立的一轮清理）。本轮只加了 `memory`，两边都登记。
- **不做 FTS/向量检索**：`Search` 是标题/标签/正文的子串匹配，按**字段**排序而非打分。子串检索没有有意义的相关系数，假装有只会把标题命中埋在长正文下面。
- **不做写入权限门**：记忆写入按设计是高频的，落在 mocode 自有目录，`forget` 是归档而非删除。要关掉整个工具，把 `memory` 加进 `options.disabled_tools`。这与 `learn` 不同——`learn` 写的是 `options.skills_paths[0]`，可能是团队共享目录。
- **不做每 N turn 自动触发**（蓝图 §10-5 第二轨）：需要 turn 计数器 + 后台 review agent，是独立 PR，与 `learn` 那一轮留的待办是同一件事。
- **不做 `GenerateMemoryID` 复用**：`domain/memory` 里那个内容幂等 ID 是为 JSONL 存储设计的；文件为基座的幂等性由**文件名锚定来源**实现，语义更直接。

## 9. 核验清单

```bash
go build ./...                                            # ✅
go vet ./internal/core/...                                # ✅
go run ./scripts/layercheck                                # ✅ no upward dependency violations
go test ./...                                              # ✅ 全仓通过
go test ./internal/core/memory/ -v                         # 条目/存储/索引/萃取
go test ./internal/core/tools/internalx/agent/ -run TestMemory -v   # 工具完整闭环
go test ./internal/core/browser/ -run TestAlive -count=5    # 竞态修复压测
```

手工验收：

1. 让 agent 调 `memory action=write` → 检查 `~/.local/share/mocode/memories/sessions/<8位>/<时间>-<8位>-<slug>.md`
2. `view mocode://memory/` → 索引里出现该条目，且 **Session memories 表列出它的目录**
3. 新开一个会话 → `memory action=search` 能搜到它；prompt 的 `<memory_index>` 段里有它的位置
4. `memory action=extract` → `global/` 下出现副本，带 `promoted_from`；再跑一次不重复
5. `memory action=forget path=global/<file>.md` → `.archive/` 里能找到
