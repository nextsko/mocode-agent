# 09 · Hermes-Agent 自进化机制 + Claude Code 动态工作流 + Go 生态调研

> 三块内容：
> 1. **Hermes-Agent 的"grows with you"机制** —— 业内最完整的 Agent 自进化实现
> 2. **Claude Code 的动态工作流** —— Skills + Hooks + Subagents 三轴动态系统
> 3. **Go 生态调研** —— 是否有现成实现？优缺点？
>
> 目标：提炼可直接复用 / 改进到 mocode 的设计。

---

## 一、Hermes-Agent 自进化机制

### 1.1 整体设计

**两个并行轨道**：

```
Track A: In-Process Runtime Evolution（agent loop 内部）
   ↓ 每 10 个 user turn / tool iter 触发
   ↓ 自动创建 / 改进 skill
   ↓ 不需要人类审批

Track B: Offline Repository Evolution（独立仓库）
   ↓ DSPy + GEPA（ICLR 2026 Oral）
   ↓ 在 hermes-agent-self-evolution 仓库里跑
   ↓ 生成 PR → 人类 review → merge
```

**这是目前最完整的"agent 自我进化"工业实现**，远超其他所有同类项目。

### 1.2 Track A：运行时自进化（3 层架构）

```
┌────────────────────────────────────────────────────────┐
│ Layer 1: PROACTIVE MEMORY CAPTURE                      │
│   每 10 个 user prompt → 后台 review fork             │
│   调用 memory 工具 → 写 MEMORY.md / USER.md           │
└────────────────────────────────────────────────────────┘
                          ↓
┌────────────────────────────────────────────────────────┐
│ Layer 2: SKILL SELF-CREATION + IMPROVEMENT             │
│   每 10 个 tool iteration → 后台 review fork           │
│   调用 skill_manage → 创建/改进 SKILL.md              │
└────────────────────────────────────────────────────────┘
                          ↓
┌────────────────────────────────────────────────────────┐
│ Layer 3: SKILL CURATOR（长生命周期）                   │
│   `hermes curator run`                                 │
│   prune（删除） + consolidate（合并）+ archive         │
│   LLM 周期性回顾                                       │
└────────────────────────────────────────────────────────┘
```

### 1.3 关键代码：触发机制

`run_agent.py` 的 `AIAgent` 类：

```python
class AIAgent:
    _turns_since_memory: int = 0      # 每次 user turn +1
    _iters_since_skill: int = 0       # 每次 tool call +1
    _memory_nudge_interval: int = 10  # 默认阈值
    _skill_nudge_interval: int = 10

    async def run_conversation(self, user_msg):
        # pre-loop:
        self._turns_since_memory += 1
        if self._turns_since_memory >= self._memory_nudge_interval:
            await self._spawn_background_review(
                messages_snapshot=...,
                review_memory=True,
                review_skills=False,
            )
        
        # chat-completions loop:
        while ...:
            self._iters_since_skill += 1
            if self._iters_since_skill >= self._skill_nudge_interval:
                await self._spawn_background_review(
                    messages_snapshot=...,
                    review_memory=False,
                    review_skills=True,
                )
```

**核心洞察**：不需要复杂的反思机制，只需要**计数器 + 阈值 + 后台 fork**。

### 1.4 关键设计：Skill Provenance（来源追踪）

**这是 hermes-agent 最重要的设计**——区分自动创建 vs 用户创建：

```python
# tools/skill_provenance.py
_write_origin: contextvars.ContextVar[str] = contextvars.ContextVar(
    "skill_write_origin",
    default="foreground",  # 默认：用户主动
)

BACKGROUND_REVIEW = "background_review"  # 后台 review fork

# 关键：只有 BACKGROUND_REVIEW 来源的 skill 才会被 curator 处理
# 用户手动创建的 skill 永远不会被自动归档
```

**对比 mocode**：mocode 的 `/evo` 模式没有这种来源追踪，可能误删用户手动创建的 patch。

### 1.5 Skill Curator（生命周期管理）

**Skill 生命周期**：

```
active  ─(30d 未使用)─►  stale  ─(90d 未使用)─►  archived
```

```bash
hermes curator status          # 状态：last run, counts, pinned, top 5 LRU
hermes curator run             # prune（默认）
hermes curator run --consolidate  # 强制 LLM 合并
hermes curator run --background   # 后台线程
hermes curator run --dry-run      # 预览
```

**安全护栏**：

```
✅ Pinned skills 跳过自动转换（用户标记的"不要动"）
✅ Bundled built-in skills 永不修改
✅ Protected built-ins 永不归档（/plan slash 命令依赖）
✅ Hub-installed skills 永不自动 curate
✅ 用户手动创建的 skills 永不自动 curate
✅ 后台 fork 是唯一触发 mark_agent_created() 的路径
```

**自动备份**：

```bash
~/.hermes/skills/.curator_backups/<utc-iso>/skills.tar.gz
hermes curator rollback -y  # 一键回滚
```

### 1.6 Memory 系统（4 层）

| 层 | 文件 | 限制 | 加载时机 |
|----|------|------|----------|
| **MEMORY.md** | `~/.hermes/memories/MEMORY.md` | 2,200 字符 | 会话启动 frozen snapshot |
| **USER.md** | `~/.hermes/memories/USER.md` | 1,375 字符 | 同上 |
| **FTS5 Search** | SQLite `~/.hermes/state.db` | 无限制 | 工具调用时查询 |
| **8 个外部 provider** | Honcho / OpenViking / Mem0 / ... | 各异 | 异步加载 |

**关键设计点**：

- **Frozen snapshot**：会话启动时拍快照，会话中修改不影响当前 context
- **手动 compact**：写入超限时返回错误，agent 必须主动 consolidate/remove
- **§ delimiter**：memory 条目分隔符（避免误解析）
- **重复检测**：拒绝完全重复
- **安全扫描**：写入前检测 prompt injection / credential exfiltration

### 1.7 Track B：离线 GEPA 进化

**5 阶段路线图**：

| Phase | 目标 | 引擎 | 状态 |
|-------|------|------|------|
| 1 | Skill 文件（SKILL.md） | DSPy + GEPA | ✅ 已实现 |
| 2 | Tool 描述 | DSPy + GEPA | 🔲 计划 |
| 3 | System prompt 段 | DSPy + GEPA | 🔲 计划 |
| 4 | Tool 实现代码 | Darwinian Evolver | 🔲 计划 |
| 5 | 持续改进 loop | 自动化 pipeline | 🔲 计划 |

**GEPA 算法**（ICLR 2026 Oral）：
- **Reflective prompt evolution**
- 读取执行 trace
- 提出针对性 mutation
- 比传统 prompt 优化收敛更快

**关键约束**：

```
1. 完整测试套件必须 100% 通过
2. 大小限制：Skills ≤15KB, Tool descriptions ≤500 字符
3. 缓存兼容性：会话中不变
4. 语义保持：不能漂移原意
```

### 1.8 与 mocode 的现状对比

mocode **已经实现了** 90% Track A 的核心架构：

| 组件 | hermes-agent | mocode | 差异 |
|------|--------------|--------|------|
| 触发机制 | 计数器 + 阈值 | `error_learner.go` 3 次错误触发 | hermes 更通用 |
| 后台 fork | 异步 sub-agent | `mocode evolve` CLI | mocode 没有 runtime fork |
| Skill 自创建 | ✅ | ✅（`/evo` 模式） | 类似 |
| Source 追踪 | ✅ `skill_provenance.py` | ❌ 无 | **mocode 缺失** |
| 自动 curate | ✅ | ❌ | **mocode 缺失** |
| Backup/rollback | ✅ tar.gz | ❌ | **mocode 缺失** |
| Memory 4 层 | ✅ | ⚠️ 部分 | mocode 只实现了 FTS5 |
| DSPy/GEPA | ✅ | ❌ | mocode 未实现 |
| Pinned skill | ✅ | ❌ | **mocode 缺失** |

**最大缺口**：

1. **没有 source provenance** —— `/evo` 创建的 patch 无法区分是用户主动还是系统自动
2. **没有 curator** —— `/evo` 创建的 patch 永远不会自动归档/清理
3. **没有 backup** —— 出问题无法回滚

---

## 二、Claude Code 动态工作流机制

### 2.1 三大机制（核心架构）

| 机制 | 决定时机 | 触发方式 |
|------|----------|----------|
| **Skills** | 会话启动（auto-discovery by description）/ 模型中途调用 `Skill` tool | 自动 |
| **Hooks** | 每次生命周期事件 | 确定性，blocking |
| **Subagents** | 主 agent 调用 `Task` tool 时 | 模型决策 |

**三者可以组合**：Skill 可以 spawn sub-agent（`context: fork`），`SubagentStart` hook 可以 fire，PreToolUse hook 可以重写任何 tool call。

### 2.2 Skills 系统（最重要的动态机制）

**3 种调用模式**：

| 设置 | Slash menu | `Skill` tool | Auto-discovery |
|------|-----------|--------------|----------------|
| `user-invocable: true`（默认）| ✅ | ✅ | ✅ |
| `user-invocable: false` | ❌ | ✅ | ✅ |
| `disable-model-invocation: true` | ✅ | ❌ | ✅ |

**Frontmatter 完整字段**（15 个）：

```yaml
name: skill-name
description: 何时加载（关键！）
when_to_use: 替代触发描述
argument-hint: 参数提示
arguments: 参数 schema
disable-model-invocation: bool
user-invocable: bool
allowed-tools: [tool1, tool2]   # 限制可用工具
model: sonnet | opus | haiku | inherit
effort: 努力程度
context: fork                   # 关键：在 sub-agent 中运行
agent: subagent_type
hooks: {...}                    # skill-scoped hooks
paths: [glob1, glob2]           # 文件路径触发过滤
shell: bash                     # scripts 用的 shell
```

**Context Fork**（核心能力）：

```yaml
---
name: code-analysis
description: Analyze code quality
context: fork                   # ← 关键：在隔离 sub-agent 中跑
---
```

**好处**：
- Skill body 在独立 context 运行
- 主对话保持干净
- 复杂多步操作不污染主上下文

### 2.3 Hooks 系统（确定性的事件拦截）

**完整事件生命周期**：

```typescript
enum HookType {
    SessionStart       = 'SessionStart',
    UserPromptSubmit   = 'UserPromptSubmit',
    PreToolUse         = 'PreToolUse',
    PostToolUse        = 'PostToolUse',
    PreCompact         = 'PreCompact',
    SubagentStart      = 'SubagentStart',
    SubagentStop       = 'SubagentStop',
    Stop               = 'Stop',
    ErrorOccurred      = 'ErrorOccurred',
    PermissionRequest  = 'PermissionRequest',
}
```

**Hook 输出 schema**：

```typescript
// PreToolUse
return { decision: "block", reason: "..." };
// 或
return { continue: true };

// PermissionRequest
return { decision: "approve" };  // 或 "deny"
```

**4 个核心能力**：

1. **Block**：PreToolUse 拦截危险命令
2. **Inject context**：PostToolUse 追加输出到 Claude context
3. **Transform**：UserPromptSubmit 重写用户 prompt
4. **Auto-approve**：PermissionRequest 程序化批准

**Hook 作用域**：

- 全局 settings.json
- Per-skill（scoped to skill execution）
- Per-agent（scoped to subagent）
- 一次性 hook（`once: true`，运行一次后自动移除）

### 2.4 Subagents（Task tool）

**6 个权限模式**：

| Mode | 用途 |
|------|------|
| `default` | 标准权限提示 |
| `acceptEdits` | 自动接受文件编辑，提示 bash |
| `plan` | 只读探索，不修改文件 |
| `delegate` | Agent 可 spawn teammates（Team Mode） |
| `dontAsk` | 大部分操作不询问 |
| `bypassPermissions` | 全部接受（需 flag） |

**6 种内置 subagent**：

| 类型 | 用途 | 工具 |
|------|------|------|
| `general-purpose` | 研究、多步任务 | 全部 |
| `Explore` | 代码库探索 | Glob/Grep/Read + 只读 bash |
| `Plan` | 架构规划 | 全部 |
| `Bash` | shell 执行 | 只 Bash |
| `statusline-setup` | 状态栏 | 受限 |
| `claude-code-guide` | 文档查询 | WebFetch |

**关键约束**：Subagent **不能 spawn 其他 subagent**（最多 2 层嵌套）。

### 2.5 Settings 级联（4 层优先级）

```
~/.claude/settings.json              (user)
  → .claude/settings.json            (project, checked in)
    → .claude/settings.local.json    (local, gitignored)
      → managed policy settings      (admin, highest)
```

### 2.6 CLAUDE.md 加载作用域（细粒度）

| 文件 | 作用域 | 加载时机 |
|------|--------|----------|
| `~/.claude/CLAUDE.md` | 全部项目 | Always |
| `CLAUDE.local.md` | 单项目 | Always |
| `CLAUDE.md` | 单项目（checked in） | Always |
| `.claude/CLAUDE.md` | 单项目（checked in） | Always |
| `<subdir>/CLAUDE.md` | 子目录 | 触碰该子目录文件时 |
| `.claude/rules/*.md` with `paths` frontmatter | glob 匹配 | glob 命中时 |

**关键洞察**：可以根据"用户正在操作哪个文件"动态加载不同的 context。

### 2.7 mocode 现状对比

| 机制 | Claude Code | mocode | 差距 |
|------|-------------|--------|------|
| Skills（auto-discovery） | ✅ 15 字段 frontmatter | ✅ 类似但更简单 | 缺 `context: fork` |
| Hooks | ✅ 9 种事件 + 4 种 output | ✅ PreToolUse | 缺 UserPromptSubmit、PostToolUse、PermissionRequest |
| Subagents | ✅ 6 类型 + 6 权限模式 | ✅ Agent + Evo | 缺 `delegate` 模式 |
| CLAUDE.md 级联 | ✅ 6 级细粒度 | ⚠️ 部分（AGENTS.md） | 缺 `paths` glob 触发 |
| Settings cascade | ✅ 4 层 | ✅ 3 层 | 类似 |
| Compaction | ✅ `/compact` + PreCompact hook | ✅ 自动 | 类似 |
| Plan mode | ✅ | ❌ | **mocode 缺失** |

**最大缺口**：

1. **没有 Plan mode** —— Agent 直接执行，没有"先计划后批准"流程
2. **没有 `context: fork`** —— Skill 总是跑在主 context
3. **没有 PermissionRequest hook** —— 权限冒泡只能等用户手动

---

## 三、Go 生态调研

### 3.1 TL;DR

> **Go 生态里没有成熟的 self-evolving agent framework。**
> 最接近的是 **CloudWeGo Eino**（orchestration + hooks + checkpoint），但缺 skill 创建、memory 进化、self-modification。

### 3.2 候选项目详细对比

#### **CloudWeGo Eino**（最强候选）

- **仓库**：github.com/cloudwego/eino（+ eino-ext + eino-examples）
- **Owner**：ByteDance
- **Stars**：~7k
- **能力**：

| 能力 | Eino 现状 |
|------|----------|
| Memory persistence | ⚠️ 部分（Retriever 接口 + checkpoint，无 first-class memory） |
| Skill 注册/发现 | ⚠️ 部分（tool.BaseTool 注册，但**不能 runtime 创建**） |
| Hook 系统 | ✅ Callback Aspects（OnStart/OnEnd/OnError） |
| Workflow orchestration | ✅ compose.Graph / Chain / Branch / Parallel |
| Self-modification | ❌ 图拓扑 compile-time 固定 |
| Feedback loops | ⚠️ DeepAgent Replanner（plan revision，不是能力进化） |
| Interrupt / Resume | ✅ 持久执行语义 |

**核心 API 示例**：

```go
// DeepAgent + Replanner（plan revision）
deepAgent, _ := deep.New(ctx, &deep.Config{
    ChatModel: chatModel,
    SubAgents: []adk.Agent{researchAgent, codeAgent},
    ToolsConfig: adk.ToolsConfig{
        ToolsNodeConfig: compose.ToolsNodeConfig{
            Tools: []tool.BaseTool{shellTool, pythonTool, webSearchTool},
        },
    },
})

runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: deepAgent})
iter := runner.Query(ctx, "Analyze sales data...")
```

**优点**：
- 强类型 + Go 原生并发
- 真实 multi-agent 协调（Supervisor / Deep / Workflow 3 种模式）
- Callback aspect 是合法的 hook 表面
- Checkpoint / interrupt / resume 支持长时间运行
- ByteDance 生产级别

**缺点**：
- **没有持久 memory store**（自己接 Milvus / Redis / SQLite）
- **没有 runtime skill 创建**
- **没有 prompt evolution / policy learning**
- 文档部分中文，examples 有时不同步
- 不是按"agent 从自己的执行历史学习"设计的

#### **tmc/langchaingo**

- 仓库：github.com/tmc/langchaingo
- Stars：~8k
- 能力：ChatModel + Memory（Buffer/Window/Summary）+ ReAct Agent + Chain
- **缺点**：Memory 只是会话历史，不是 learned skill acquisition
- **完全没** skill registry / runtime tool creation / self-modification

#### 其他候选

| 项目 | 评估 |
|------|------|
| eino-ai/eino | CloudWeGo Eino 的旧 mirror |
| milvus-io/milvus | 向量数据库，不算 agent framework |
| sashabaranov/go-openai | OpenAI HTTP client |
| tmc/langchaingo | 见上 |
| hybridgroup/gobot | 硬件驱动框架 |
| koding/kite | pre-LLM RPC 框架 |

### 3.3 自进化系统的学术背景

虽然 Go 没有，但**学术和 Python 生态有大量参考**：

- **MemRL** —— 记忆强化学习
- **Darwin Gödel Machine** —— 达尔文式自进化
- **MemSkill** —— 记忆驱动的技能创建
- **AutoSkill** —— 自动 skill 发现
- **GEPA** —— Reflective prompt evolution（ICLR 2026 Oral）
- **AccelOpt** —— 加速优化
- **Memento** —— 经验回放
- **ACE** —— Agentic Context Engineering
- **RAGEN** —— 强化学习 agent

**全部是 Python 代码**。Go port 滞后 ~2 年。

### 3.4 要在 Go 里建一个，需要 5 层架构

```go
type MemoryStore interface {
    Store(ctx, item) error
    Recall(ctx, query, k) ([]MemoryItem, error)
    Forget(ctx, id) error
    Consolidate(ctx) error   // merge, dedupe, summarize
}

type Skill struct {
    ID          string
    Description string
    Code        string     // 可执行代码
    Triggers    []string
    Stats       SkillStats  // success/failure 跟踪
}

type SkillRegistry interface {
    Register(ctx, s) error
    Match(ctx, intent) ([]Skill, error)
    Update(ctx, id, s) error
    Retire(ctx, id) error
}

// Eino Callback Aspect 是合法 hook 系统
type Aspect interface {
    OnStart(ctx, info)
    OnEnd(ctx, info)
    OnError(ctx, info)
}

// Reflection Loop
func (a *Agent) Reflect(ctx) error {
    failures := a.memory.RecentEpisodes(50).Where(failed)
    proposal := a.llm.Generate(ctx, reflectionPrompt(failures))
    if proposal.NewSkill != nil {
        a.skills.Register(ctx, proposal.NewSkill)
    }
    if proposal.PromptPatch != "" {
        a.systemPrompt = proposal.PromptPatch
        a.persist()
    }
    a.memory.Consolidate(ctx)
    return nil
}
```

### 3.5 推荐技术栈

| 组件 | 选择 |
|------|------|
| 核心 | CloudWeGo Eino（compose + adk + Callback Aspects） |
| Memory | `modernc.org/sqlite` + `sqlite-vec`（零依赖） |
| Skill runtime | `yaegi`（解释器）或 `plugin`（原生 Go） |
| Reflection 触发 | 每 N 个 episode / 失败连击 / 用户信号 |
| Prompt evolution | systemPrompt 作为 versioned 文件，diff + merge |

---

## 四、可落地的改进清单

### 4.1 高优先级（必须有）

| 改进项 | 工作量 | 借鉴自 | 价值 |
|--------|--------|--------|------|
| **Skill Provenance 追踪** | 4h | hermes-agent `skill_provenance.py` | ★★★★★ |
| **Skill Curator（生命周期管理）** | 6h | hermes-agent curator | ★★★★ |
| **Skill Backup/Rollback** | 3h | hermes-agent tar.gz | ★★★★ |
| **Plan mode 流程** | 8h | Claude Code `permissionMode: plan` | ★★★★ |
| **`context: fork` Skill** | 6h | Claude Code context fork | ★★★★ |
| **PermissionRequest hook** | 4h | Claude Code SDK | ★★★★ |

### 4.2 中优先级（应该做）

| 改进项 | 工作量 | 借鉴自 | 价值 |
|--------|--------|--------|------|
| **`paths` glob 触发的 CLAUDE.md** | 4h | Claude Code rules | ★★★ |
| **Memory 4 层架构** | 8h | hermes-agent 4-layer memory | ★★★★ |
| **`once: true` 一次性 hook** | 2h | Claude Code hook | ★★★ |
| **Pinned skill 标记** | 1h | hermes-agent `.usage.json` | ★★★ |
| **FTS5 session 搜索工具** | 3h | hermes-agent | ★★★ |

### 4.3 低优先级（有时间做）

| 改进项 | 工作量 | 借鉴自 | 价值 |
|--------|--------|--------|------|
| **DSPy/GEPA Go 实现** | 40h+ | hermes-agent-self-evolution | ★★★★ |
| **Darwinian Evolver 包装** | 16h | hermes-agent Phase 4 | ★★ |
| **Auto-trigger cron** | 8h | hermes-agent Phase 5 | ★★★ |
| **8 个外部 memory provider** | 40h+ | hermes-agent | ★★ |

---

## 五、mocode `/evo` 模式 vs Hermes 完整自进化

### 5.1 mocode 现状（核心三环设计）

`docs/evolution-loop-plan.md` 中描述：

```
反例环 (✓):    工具错误 → ErrorLearner.Record → 3 次 → PrepareStep 注入纠正
效果进化环 (✓): errcoll + provider 错误 → ErrorLearner + session trace → eval → Patch
Patch 环 (✓):   Patch 生产者(读 sessionlog/分数 → CreatePatch) → injectEvolutionContext 注入 → MaxInjects 收敛
```

### 5.2 与 Hermes 完整对比

| 维度 | mocode `/evo` | Hermes Track A | 差距 |
|------|---------------|----------------|------|
| 触发机制 | 错误 3 次 | 计数器 + 阈值 | mocode 只针对错误 |
| 后台 fork | ❌（需 CLI 跑 `mocode evolve`） | ✅ runtime async | **大缺口** |
| Skill 自创建 | ✅ | ✅ | 类似 |
| **Source Provenance** | ❌ | ✅ | **关键缺失** |
| **Curator（生命周期）** | ❌（patch 永不清除） | ✅ active/stale/archived | **关键缺失** |
| Backup/rollback | ❌ | ✅ | **关键缺失** |
| Pinned 标记 | ❌ | ✅ | 缺失 |
| Memory 4 层 | ⚠️ 部分 | ✅ 完整 | 中度缺失 |
| **DSPy/GEPA 离线优化** | ❌ | ✅ | 远期目标 |

### 5.3 mocode 应该优先补的 5 项

```
1. Source Provenance（critical）
   - 区分 user-direct vs system-evo
   - curator 只处理 system-evo 写的
   
2. Skill Curator（critical）
   - active / stale / archived 生命周期
   - LLM periodic consolidation
   
3. Backup + Rollback（critical）
   - 每次 curator run 前 tar.gz 备份
   - 一键回滚
   
4. Runtime async fork（high）
   - 现在要 CLI 跑，应该做 in-process 后台
   - 类似 hermes 的 _spawn_background_review
   
5. FTS5 cross-session search（medium）
   - 已有 hermes_state.py 模式可借鉴
   - mocode 用 SQLite + FTS5 即可
```

---

## 六、mocode 落地 Roadmap

### 6.1 短期（1-2 周）

```go
// internal/core/evo/provenance.go
package evo

type WriteOrigin string
const (
    OriginForeground     WriteOrigin = "foreground"     // 用户主动
    OriginBackgroundReview WriteOrigin = "background_review"  // 后台 review
    OriginUser            WriteOrigin = "user"           // 用户直接调用 CLI
)

var currentOrigin = contextvar.NewContextVar[WriteOrigin](OriginForeground)
```

```go
// internal/core/evo/curator.go
package evo

type SkillLifecycle string
const (
    LifecycleActive   SkillLifecycle = "active"     // 30d 内用过
    LifecycleStale    SkillLifecycle = "stale"      // 30d 未用
    LifecycleArchived SkillLifecycle = "archived"   // 90d 未用
)

type Skill struct {
    ID          string
    Lifecycle   SkillLifecycle
    Pinned      bool
    CreatedBy   WriteOrigin
    CreatedAt   time.Time
    LastUsed    time.Time
    UseCount    int
}

func (c *Curator) Run(ctx) error {
    // 1. 备份
    backupPath := c.backup()
    
    // 2. 扫描所有 skills
    skills := c.listAll()
    
    // 3. 只处理 OriginBackgroundReview 的
    autoSkills := skills.Where(s => s.CreatedBy == OriginBackgroundReview && !s.Pinned)
    
    // 4. 转换生命周期
    for _, s := range autoSkills {
        if time.Since(s.LastUsed) > 90*24*time.Hour {
            c.archive(s)
        } else if time.Since(s.LastUsed) > 30*24*time.Hour {
            c.markStale(s)
        }
    }
    
    // 5. LLM 合并相似 skills
    if c.config.Consolidate {
        c.consolidateWithLLM(autoSkills)
    }
}
```

### 6.2 中期（1 个月）

- `runtime_async_fork.go` —— 异步后台 review
- `memory_4layer.go` —— MEMORY.md / USER.md / FTS5 / external provider
- `pinned_skill.go` —— CLI `mocode skill pin <name>`
- `backup.go` —— tar.gz 备份 + `mocode skill rollback`

### 6.3 长期（2-3 个月）

- DSPy/GEPA Go port（参考 hermes-agent-self-evolution）
- Darwinian Evolver 包装
- Auto-trigger cron
- 8 个外部 memory provider 适配器

---

## 七、与 Claude Code 的核心差异 & 借鉴优先级

### 7.1 借鉴优先级矩阵

```
                    高价值   中价值   低价值
已实现(无需借鉴)     ─       ─        ─
借鉴后立即收益      A       B        C
借鉴后长期收益      D       E        F
```

**A 区（立即收益，必借鉴）**：

1. **Source Provenance**（hermes）
2. **Plan mode 流程**（CC）
3. **`context: fork` Skill**（CC）
4. **PermissionRequest hook**（CC）

**B 区（中价值，2-3 周）**：

1. **Skill Curator**（hermes）
2. **Backup/Rollback**（hermes）
3. **Memory 4 层**（hermes）

**D 区（长期，1+ 月）**：

1. **DSPy/GEPA Go port**（hermes）
2. **`paths` glob 触发的 CLAUDE.md**（CC）
3. **Auto-trigger cron**（hermes）

### 7.2 不应直接借鉴的设计

| 设计 | 不借鉴原因 |
|------|----------|
| CC 的 settings 4 层 cascade | mocode 已经简化 |
| CC 的 9 种 Hook 事件 | mocode 当前只需要 PreToolUse；不要过度设计 |
| Hermes 的 8 个 external memory provider | 太重，先做内置 |
| Hermes 的 5 阶段 GEPA 路线 | 太长期，先做运行时 |

---

## 八、参考实现链接

| 项目 | URL |
|------|-----|
| Hermes Agent | github.com/NousResearch/hermes-agent |
| Hermes Self-Evolution | github.com/NousResearch/hermes-agent-self-evolution |
| CloudWeGo Eino | github.com/cloudwego/eino |
| Eino ADK | github.com/cloudwego/eino/tree/main/adk |
| LangChain Go | github.com/tmc/langchaingo |
| Claude Code 文档 | code.claude.com/docs/en/skills |
| Claude Code Settings | code.claude.com/docs/en/settings |
| GEPA 算法 | ICLR 2026 Oral（paper） |
| Darwin Gödel Machine | github.com/.../darwin-godel-machine |
| MemSkill | paper + code |

---

## 九、总结：mocode 的下一步行动

### 9.1 1 周内（最低成本最高收益）

```go
// 1. 加 provenance.go（4h）
// 2. 加 curator.go + 自动归档（6h）
// 3. 加 backup.go + rollback（3h）
// 4. 加 pinned skill 标记（1h）
```

### 9.2 1 个月内

```go
// 5. runtime async fork（6h）
// 6. Plan mode 流程（8h）
// 7. context: fork skill（6h）
// 8. Memory 4 层基础（8h）
```

### 9.3 3 个月内

```go
// 9. FTS5 session 搜索（4h）
// 10. paths glob CLAUDE.md（4h）
// 11. GEPA 简化版（40h+）
```

### 9.4 不应做的事

- ❌ 把 mocode 改成 Python（gepa/dspy 是 Python）
- ❌ 实现完整 Darwinian Evolver（AGPL 风险）
- ❌ 8 个 external provider（先做内置）

### 9.5 核心哲学

> Hermes Agent 的成功不是因为 GEPA，而是因为 **Source Provenance + Curator + Backup + Pinned** 这套**信任系统**。
> mocode 已经 90% 到位了，缺的恰恰是这套**信任基础设施**。
> 
> 让用户**信任**自动创建/修改的 patch —— 这才是自进化的真正挑战。