# 16 · learn 工具设计 —— 把「做完一件事」沉淀成 skill

> 状态：**已实现**（工具 + 注册 + 测试 + 文档）
> 上游依据：`docs/architecture/00-hermes-blueprint.md` §4 B09/B10、§7 P5、§10-4/§10-5；
> `docs/design/09-hermes-and-claude-code-evolution.md` §1（hermes Track A）、§4.1、§9.5

---

## 1. 目标与非目标

### 目标

给模型一个**可以主动调用**的工具，把刚完成的任务里那部分「不显然、会复发、复现成本高」的知识，
写成一份 `SKILL.md`，并在后续不断打磨同一份 skill。

### 非目标（本轮明确不做，避免范围蔓延）

| 不做 | 理由 | 归属 |
|---|---|---|
| 每 N turn 自动 fork review | 蓝图 §10-5 定为「双轨」，本轮只交付**手动路径**；自动触发器需要 turn 计数器 + 后台 review agent，是独立 PR | B06/B10，P5 第二轨 |
| Skill Curator（active→stale→archived 生命周期） | 依赖 provenance 已铺好才谈得上；本轮只**埋下 provenance** | `docs/design/09` §4.1 |
| `skills.shared_dirs` 配置项 | 蓝图 §10-4 的落点。本轮复用已有的 `options.skills_paths[0]`，**零新增配置面**即可满足「落可配置共享目录」 | B10 |
| 写 `MEMORY.md` / 记忆库 | 那是 B06；`learn` 只写 skill | B06 |

**判断依据**：蓝图 §10 末行写着「以上为当前决定；若后续推翻，先改本表再动代码」，
且 §9 Boundaries 把「改本蓝图 / 改端口契约」列为 *Ask first*。
所以本轮**不改蓝图、不新增端口、不改配置 schema**，只填 B09 表格里那一格明确标注的缺口
（`docs/architecture/00-hermes-blueprint.md` §4 B09 最后一列：**learn 工具**）。

## 2. 触发与形态：为什么是「一个工具 + action」

hermes Track A 的核心洞察（`docs/design/09` §1.3 原文）是：

> 「**核心洞察**：不需要复杂的反思机制，只需要**计数器 + 阈值 + 后台 fork**。」

本轮把手动那一轨做成一个工具，而不是三个工具（`learn_create` / `learn_refine` / `learn_search`），
因为三者的**前置判断是同一个**——「这件事值不值得沉淀、已有 skill 里有没有能改的」。
拆成三个工具会让模型先猜该调哪个；合成一个带 `action` 枚举的工具，可以先 `list` 再决定。

```
learn(action=create|refine|list)
```

- `list` —— 先侦察：哪些 skill 可改、哪些是内置（不可改，但名字要避开）、写到哪里。
- `create` —— 新写一份。**拒绝**重名（见 §4）。
- `refine` —— 就地把已有 skill 改写一遍，保留 provenance，旧版本自动备份。

## 3. 写到哪：复用已有的读取路径，零新增配置

写入目标 = **`options.skills_paths[0]`**，即优先级最高的用户 skill 目录。

选它的理由是**读写同源**：`discoverSkills`（`internal/core/agent/coordinator/coordinator_skills.go:33`）
本来就按 `Options.SkillsPaths` 顺序扫描，所以

1. 写进去的 skill **下一个 session 自动被发现**，不需要任何新配置；
2. 想改成项目级或共享目录，用户只需要配 `options.skills_paths`——**已有旋钮**，不需要 `skills.shared_dirs`；
3. 路径展开（`infra.Long` + `$VAR` 解析）与 `discoverSkills` **逐行一致**，保证写目标和读目标永远是同一个目录
   （`internal/core/tools/registry.go` 的 `learnSkillsRoot`）。

> 默认值形态：未配置时 `SkillsPaths[0]` 落到 `~/.config/mocode/skills`（或在设置了 `MOCODE_SKILLS_DIR` 时落到它），
> 与 hermes 写 `~/.hermes/skills/`、蓝图 §10-4 写 `~/.agent/skills` 的语义一致。

## 4. 三条硬规则

### 4.1 不许遮蔽（no shadowing）

`skills.Deduplicate` 的语义是「**后者胜**，用户 skill 覆盖同名内置 skill」
（`internal/core/skills/skills.go:321` 注释）。这条语义对 `learn` 是危险的：

- `create` 一个叫 `jq` 的 skill → 会**静默顶掉**内置的 `jq` skill；
- 在 `SkillsPaths[0]` 建同名 → 因为同目录内它排在**最前**，反而会被后面的同名 skill 覆盖。

所以 `create` 明确**拒绝**：名字命中内置 → 报错并说明原因；名字已存在（无论在 `Known` 还是本次 session 已落盘）→
报错并指向 `refine`。`refine` 则反过来：**改在实际生效的那个文件里**（`SkillFilePath` 所在目录），
而不是在 root 里造一份影子副本。

### 4.2 不许洗白 provenance（no provenance laundering）

`docs/design/09` §9.5 把 hermes 的成功归因为：

> 「> Hermes Agent 的成功不是因为 GEPA，而是因为 **Source Provenance + Curator + Backup + Pinned** 这套**信任系统**。」

`learn` 是这套系统的**前置条件**：它负责把「这份 skill 是谁写的」写进 frontmatter。
用的是**已有的** `metadata` map，**不动 skill 格式**：

```yaml
metadata:
  origin: learn          # 只有 learn 工具 create 出来的才盖这个章
  revision: "2"
  created_at: 2026-02-03T04:05:06Z
  updated_at: 2026-02-03T05:05:06Z
  reason: ...
```

关键约束：**`refine` 绝不把 `origin` 改成 `learn`**。
一份人写的 skill 被 `learn` 改进后，依然**不带** `origin`，因此未来的自动 curation 仍然会跳过它
（`TestLearn_Refine_DoesNotLaunderHandWrittenProvenance` 守住这条）。

### 4.3 写入必须可撤销、可审计

- **备份**：每次覆盖前，旧内容复制到 `<root>/.backups/<name>/<UTC 时间戳>.md`。
  备份**故意不叫 `SKILL.md`**——扫描器只认这个精确文件名（`skills.go:219`），
  所以备份永远不会被当成 live skill 加载（`TestWriteFile_BackupIsNotDiscovered` 守住）。
  同一秒内二次覆盖会加序号，不会互相踩（`TestWriteFile_SecondBackupSameSecondDoesNotClobber`）。
- **权限**：走仓库既有的按次审批机制（`permission.Service.Request`），
  与 `write` 工具同构，payload 带 `old_content`/`new_content` 以便 UI 出 diff。
  `Action` 用 `create`/`refine`，所以 `allowed_tools: ["learn:refine"]` 这类细粒度白名单天然可用。
- **原子写**：先写同目录临时文件再 `rename`，中途失败不会留下半个 skill。
- **落盘前往返校验**：`Render` 出来的字节必须先能被 `ParseContent` 读回，否则拒绝写入。

## 5. 校验：把仓库的「skill 房规」变成工具约束

`docs/skills-and-roles/README.md` §新增 skill 定的规矩是：

> 「front matter `name` 必须等于目录名（kebab-case，**禁止下划线**），`description` 以 `Use when ...` 开头且 ≤1024 字符」

`learn` 把这些变成**硬校验**（错了就返回可自我修正的软错误）：

| 规则 | 实现位置 |
|---|---|
| `name` kebab-case、≤64、字母数字+单连字符 | 复用 `skills.Validate()` 的名字规则 |
| `name` 必须等于目录名 | `skills.WriteFileTo` 写入前用目标目录二次 `Validate()` |
| `description` 必须以 `Use when` 开头、≤1024 | `validateDescription` |
| `instructions` 非空 | `resolveWriteRequest` |

> 为什么强制 `Use when`：`writing-skills` 内置 skill 第 58 行的 SDO 规则写着
> 「一旦描述里写了流程，agent 会照描述抄近路而不读正文」。
> 工具描述 `learn.md` 里把这条写清楚了，所以模型第一次就会写对。

## 6. 交付物

| 文件 | 作用 |
|---|---|
| `internal/core/skills/write.go` | `Render` / `WriteFile` / `WriteFileTo` + provenance 常量。**格式知识归 skills 包**，它仍是 SKILL.md 的唯一权威 |
| `internal/core/tools/internalx/agent/learn.go` | 工具本体：action 分发、三条硬规则、权限门 |
| `internal/core/tools/internalx/agent/learn.md` | 工具描述（`FirstLineDescription` 取首行）——同时是模型的房规手册 |
| `internal/core/tools/registry.go` | `learnPlugin` + `learnSkillsRoot` |
| `internal/core/config/config.go` | `allToolNames()` 加 `learn`（不加会被 `AllowedTools` 过滤掉） |
| `internal/core/tools/registry_test.go` / `tools.go` | 名单交叉校验 + façade 重导出 |

配套测试：`internal/core/skills/write_test.go`、`internal/core/tools/internalx/agent/learn_test.go`。

## 7. 核验清单

```bash
go build ./...                                   # ✅
go run ./scripts/layercheck                      # ✅ no upward dependency violations
go test ./internal/...                           # ✅ 全量通过
go test ./internal/core/tools/internalx/agent/ -run TestLearn -v
go test ./internal/core/skills/ -run 'TestWriteFile|TestRender' -v
```

手工验收（P5 第一条路径）：

1. 在新 session 里让模型干完一件有沉淀价值的事，再让它调 `learn(action=list)` —— 应列出可改 skill 与写入目录。
2. `learn(action=create, name=demo-skill, description="Use when ...", instructions="...")` —— 应弹权限框；
   批准后 `~/.config/mocode/skills/demo-skill/SKILL.md` 出现，frontmatter 带 `origin: learn`。
3. **重开 session**，`mocode_info` 的 `[skills]` 段应能看到 `demo-skill`（本轮不做热重载，见 §8）。
4. 再 `learn(action=refine, name=demo-skill, ...)` —— `revision` 变 2，`.backups/` 里出现旧版本。

## 8. 已知边界

- **不做热重载**：`Known` 在 session 启动时构建（`discoverSkills`），
  所以本次 session 内新建的 skill 不会出现在 `<available_skills>` 里。工具响应里明说了这一点。
- **`refine` 的落点**：优先改写 `Known` 里那份实际生效的文件；
  若该目录不可写（只读共享目录、`~/.claude/skills` 等），会返回文件系统错误原文，不做静默降级。
- **未验证**：`CategoryMemory` 此前是死枚举（无任何 plugin 使用），本轮用它承载 `learn`；
  已确认没有任何过滤逻辑按该 category 过滤（全仓仅 `registry.go:59` 一处定义）。
- **`config.allToolNames()` 与 registry 本来就有漂移**（例如 `session_search` 在 registry 有、config 没有；
  `memory_*` 在 config 有、全仓无实现）。`TestAllToolNames_MatchesConfigList` 只比对
  `knownAllToolNames`，抓不到这层漂移。本轮的 `learn` 两边都加了，并在 `doc.go` 的
  「Adding a new tool」清单里把这一点写成显式警告。
