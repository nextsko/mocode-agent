# 00 · Hermes 积木架构蓝图（母版）

> 本文档是整个 mocode-agent 演进的**唯一规划真相源**：先设计、后实现。
> 后续全面采用 **subagent + workspace(worktree) 并行推进**，本文档即各 workstream 的任务书来源。
> 修改本文档 = 修改计划；实现漂移必须回写此处（spec 是活文档）。
>
> 相关：`docs/design/09-hermes-and-claude-code-evolution.md`（自进化调研）、
> `internal/core/tools/doc.go`（工具树现状）、`docs/dev-notes/structure-governance-baseline.md`（分层基线）。

---

## 1. 目标（Objective）

把 mocode-agent 从「单 agent + 工具集」演进为**积木式组合系统**：每个能力是一块自封装积木（块），通过显式端口（port）组合，受约束（constraint）治理——任何积木可被替换、升级、并行开发，而不牵动全局。

成功判据：
1. 任意一个积木可以在独立 worktree 中由 subagent 开发，合并时**零文件冲突**（除注册点/组装点）。
2. 新增一个外部系统接入（如新 connector）只需新增目录 + 注册，不改其他积木。
3. `layercheck` 对每个积木的输入依赖有强制约束，违规即构建失败。

## 2. 假设清单（ASSUMPTIONS）

1. 仓库继续单 Go module；积木 = `internal/` 下顶层包，不做多 module。
2. 三作用域会话中**云端作用域只有接口设计，不实现**（无云服务可用）；先落项目级+全局级。
3. Dream Evo / moa / team / tree 是**产品模式**，骑在架构之上，不占顶层目录。
4. subagent 开发遵循 AGENTS.md 现有规范（devship.sh 闭环、layercheck、提交格式）。
5. 存量 UI(transport/ui) 不重构入积木，作为「平台层」消费积木。
→ 如有偏差，先改本文档再动代码。

## 3. 设计原则（不变式）

| 原则 | 来源 | 落法 |
|---|---|---|
| 目录即结构 | NextJS | 目录=能力，文件=路由；禁止空壳目录（要么有实现要么删） |
| 块-端口-约束 | SysML | 每块显式声明依赖端口（接口），layercheck 做约束验证器 |
| 组合优于扩展 | 积木 | 能力=块的组合；新能力=新块或换块，不改既有块内部 |
| 单一真相源 | 版本教训 | 版本=git tag；代理=nethttp 工厂；计划=本文档 |
| 生命周期显式 | 泄漏教训 | 每块必须回答：谁创建、谁关闭、失败时谁兜底（Startable/Close） |

## 4. 积木全景（16 块 · 三层）

### 第一梯队：地基（其他块踩在上面）

| # | 积木 | 职责 | 现状 | 缺口 |
|---|------|------|------|------|
| B01 | **model** 模型层 | provider 路由/降级、usage 落账（token 计量，只记不阻断）、流式中断恢复 | provider 构建✅；**usage 字段不存在** | usage 落账+累计+查询（无熔断，见 §10-2） |
| B02 | **session** 会话 | 三作用域(项目`.mocode/`/全局`~/.mocode/`/云端)、生命周期(create/resume/**fork**/merge/archive)、加工链(原始→摘要→经验包蒸馏) | 项目级✅、snapshot/export✅、摘要队列✅ | 全局/云端作用域、fork/merge、蒸馏链 |
| B03 | **secrets** 密钥 | keyring 后端、`$MOCODE_KEYRING` 解析、明文 config 降级为 fallback | ❌ 明文 config.json | 全部 |
| B04 | **obs** 可观测 | span 化审计（tool_call 父子树）、回放（"agent 为什么这么做"）、成本仪表 | sessionlog 事件流✅(种子)、errcoll✅ | span 树、回放、仪表 |
| B05 | **store** 存储抽象 | 会话/记忆/审计的统一存储接口（本地文件后端首发） | store 层存在但被隐形 | 显式 port 化 |

### 第二梯队：能力（用户可感知）

| # | 积木 | 职责 | 现状 | 缺口 |
|---|------|------|------|------|
| B06 | **memory** 记忆 | md 记忆、经验包、上下文时间线角色记忆 | `domain/memory` **空壳**（4 文件全是 types） | 全部（吃 B02 的加工链输出） |
| B07 | **context** 上下文 | 角色动态化、fork/tree 回滚 | session_snapshot 只算 checkpoint | fork/tree、角色注入 |
| B08 | **sandbox** 沙箱 | 工具执行隔离、agent 托管 | shellruntime+后台任务✅（0.8 已修可观测/挂死） | 隔离边界、托管 |
| B09 | **tools** 工具 | 外部(net/ssh/gitea/mcp/connector)/内部(lsp/agent 工具)/核心(fs/bash/net) | ✅ 0.8 已三层化；nethttp 端口✅ | connector 统一端口、learn 工具 |
| B10 | **skills** 技能 | 内置/外部共享目录(可配置多目录，默认 `~/.agent/skills`，不签名)/learn 进化链(手动 `/learn` + 每 N turn 自动 fork review) | core/skills✅ + Tracker✅ | 共享目录配置化、learn 链（吃 B06/B02） |
| B11 | **hooks** 钩子 | 生命周期事件总线(agent/tool/event) → 脚本目录分发 | config/load_hooks.go 仅配置解析 | 事件总线、分发器 |
| B12 | **cron** 定时 | 后台定时任务、日常脚本策略 | ❌（复用 BackgroundShellManager 地基） | 全部 |
| B13 | **permission** 权限 | 工具调用守门 | ✅ 就位 | 仅接 B11 事件 |

### 第三梯队：通道与外围

| # | 积木 | 职责 | 现状 | 缺口 |
|---|------|------|------|------|
| B14 | **notify** 通知 | 事件→路由→通道；路由规则引擎为占位（见 §10-3） | messenger 接口+WeChat 雏形✅ | `Router` 接口占位 + 通道适配 |
| B15 | **update** 更新 | 版本检查、自更新 | ❌ npm 分发就位但无检查调用 | 全部 |
| B16 | **hitl** 人机交互 | TUI/权限对话框/通知=第六生命周期 | 分散在 ui/ | 归位为事件消费方 |

**产品模式（不占顶层目录，骑在 B02/B06/B07 上）**：Dream Evo 🧬、moa/team/tree 多 agent 编排、transfer_to_agent。

## 5. 目标拓扑

```
internal/
├── hermes/                 # 地基层
│   ├── model/              # B01
│   ├── session/            # B02 (吸收 domain/session 的服务面)
│   ├── secrets/            # B03
│   ├── obs/                # B04 (sessionlog span 化迁入)
│   └── store/              # B05
├── core/
│   ├── tools/              # B09 保持 0.8 形状: net/ssh/gitea/mcp/lsp/nethttp/
│   ├── skills/             # B10
│   ├── permission/         # B13
│   └── sandbox/            # B08 (shellruntime 并入)
├── runtime/
│   ├── hooks/              # B11 事件总线
│   ├── cron/               # B12
│   └── notify/             # B14 (domain/messenger 迁入)
├── agent/                  # 瘦身: coordinator + 模式插件(moa/tree/...)
└── platform/               # app/config/transport/ui/store 消费面(不重构)
```

## 6. 块间端口契约（subagent 并行的关键）

每个契约 = 一个小接口文件 + 单测。**先立契约，后填实现**——这是并行的前提。

```go
// hermes/model: 出账
type UsageSink interface { Record(scope string, u Usage) }        // B01 出
// hermes/session: 消费 B01, 被 B06/B07 消费
type Timeline interface { Fork(id string) (Timeline, error) ... } // B02 出
// hermes/store: 被所有持久化块消费
type KV interface { Get/Put/Delete/Scan(scope Scope, key string) } // B05 出
// runtime/hooks: B11 出, B13/B14/B16 消费
type Bus interface { Emit(e Event); On(kind, Handler) }
// tools: connector 端口(B09 补)
type Connector interface { Tools() []AgentTool; Close() error }
```

规则：块 A 只准 import 块 B 的 `port.go`（接口），不准 import 实现——layercheck 逐步加禁令。

## 7. 依赖 DAG 与路线图

```
B05 store ─→ B02 session ─→ B06 memory ─→ B10 skills(learn)
                │    └────→ B07 context ─→ (Dream Evo 模式)
                └──→ B04 obs ←─ B01 model ─→ (moa/cron 预算熔断)
B03 secrets ─→ (云端会话-仅接口)
B11 hooks ─→ B12 cron ─→ B14 notify;  B08 sandbox 独立;  B15 update 独立
```

| Phase | 内容 | 验收标准（可测） |
|---|---|---|
| **P1 契约周** | 全部端口接口+单测落地（6 个 port.go）；workstream 划分冻结 | `layercheck` 含端口规则；空接口编译过 |
| **P2 地基** | B01 usage 落账（token 只记不阻断）；B05 store port+文件后端+`Indexer` 接口；B04 span 化 | 单轮对话后 session 文件含 usage JSON 且可查询；Indexer 替换实现不改调用方（里氏验证） |
| **P3 会话** | B02 全局作用域+fork；B03 keyring | fork 后两会话独立演化；`$MOCODE_KEYRING` 解析出 key |
| **P4 能力** | B11 事件总线→B12 cron→B14 通知链（路由占位：cron 产出直通默认通道） | cron 任务产出经默认通道发出；Router 接口可替换实现 |
| **P5 进化** | B06 记忆实现+B10 learn 双轨触发(手动+每 N turn)；B07 tree 回滚 | learn 生成 skill 落可配置共享目录；回滚到任意节点 |
| **P6 产品模式** | moa/team/tree、Dream Evo（独立 PR，不阻塞架构） | 模式插件注册即用 |

## 8. Subagent + Workstream 执行策略

**Workstream = 冲突面为零的块组**（同一 workstream 内串行，不同 workstream 并行）：

| WS | 块 | 主战场目录 | 冲突点(唯一) |
|---|---|---|---|
| WS-1 | B01+B04 | `hermes/model/`, `hermes/obs/` | agent 循环埋点(2 文件) |
| WS-2 | B05+B02+B03 | `hermes/{store,session,secrets}/` | session 服务迁移 |
| WS-3 | B11+B12+B14 | `runtime/{hooks,cron,notify}/` | app 装配点(1 文件) |
| WS-4 | B09 connector+B08 | `core/tools/`, `core/sandbox/` | registry 注册块 |
| WS-5 | B06+B10+B07 | `hermes/memory/`, `core/skills/` | P3 完成后启动 |

操作规程：
1. 每 WS 一个 worktree：`git worktree add ../mocode-ws<N> -b ws/<name>`，根目录 `.worktrees/` 已 ignore。
2. 每个 subagent 任务书 = 本文档对应块行 + 端口契约 + AGENTS.md；完成定义 = 验收标准 + `devship.sh` 绿 + layercheck 绿。
3. 合并顺序按 DAG：P1 契约先合 → WS-1..4 可并行合（rebase 各自冲突点，均 ≤2 文件）→ WS-5。
4. 冲突点文件改动 ≤10 行的，由合并者手工 rebase；超出的回炉该 WS。

## 9. 验证与守门（Commands）

```bash
task dev:ship                     # 测试→构建→原子替换→版本核验（唯一合法发布路径）
go run ./scripts/layercheck       # 分层+端口约束（每 P 扩规则）
go test -race ./internal/...      # 全量回归
```

**Boundaries**：Always=改前读文件、改后跑 devship/layercheck、验收标准先行；Ask first=新顶层目录、改端口契约、改本蓝图；Never=绕过 nethttp 自建 transport、空壳目录进主干、手工 cp 二进制。

## 10. 已决事项（已拍板）

| # | 问题 | 决定 | 落点 |
|---|------|------|------|
| 1 | 全局会话索引形态 | **纯文件 + 接口式实现（里氏替换）**：索引器是 `SessionIndexer` 接口，文件扫描为首发实现，未来 SQLite/FST 可无侵入替换 | B05 store port 定义 `Indexer` 接口；P2 验收含「替换实现不改调用方」测试 |
| 2 | 预算单位与熔断 | **token 计量，无熔断**：只落账不阻断（usage 落账→累计→仪表盘），不做硬预算拦截；未来需要时作为可选插件再加 | B01 去掉「预算熔断」，改为 usage 记录+查询；验收改为「单轮对话后 session 文件含 usage JSON 且可查询」 |
| 3 | 通知路由规则 | **占位符**：B14 仅定义 `Router` 接口与空实现，规则引擎延后 | B14 降级为「接口占位」，移出 P4 关键路径（cron→notify 直通版本号） |
| 4 | `~/.agent/` 签名校验 | **不做签名，但可配置化**：共识目录是行业规范；目录列表可配置（支持多目录），为将来按目录分级信任留口 | B10 目录列表进 config（`skills.shared_dirs` 数组，默认 `["~/.agent/skills"]`），不引入签名机制 |
| 5 | learn 链触发时机 | **双轨：手动 `/learn` + 每 N turn 自动 fork review**（参考 Hermes Track A） | B10/B06：手动命令 + 自动触发器（N 默认 10，config 可调/可关）；P5 验收含两条路径 |

→ 以上为当前决定；若后续推翻，先改本表再动代码。
