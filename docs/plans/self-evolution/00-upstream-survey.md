# tmp/ 上游 Agent 项目「自进化」调研报告

> 调研时间：2026-06
> 目的：扫描 `tmp/` 下所有 Agent 项目，找出真正实现了「让 Agent 越用越懂用户 / 自我积累经验 / 持续更新提升」的代码，提炼可借鉴的设计模式。

---

## 📌 调研范围

| 项目 | 语言 | 备注 |
|---|---|---|
| `MiMo-Code` | TypeScript | OpenCode fork，自进化最完整 |
| `grok-build` | Rust | x.ai 旗下，跨 session memory + hybrid search |
| `goclaw` | Go | 类 OpenClaw 架构，最工业级 |
| `hermes-agent` | Python | nousresearch，MemoryProvider 插件体系最优雅 |

其他（`NemoClaw` / `awesome-hermes-agent` / `grok-build/pager` / `picoclaw` / `opencode` / `oh-my-pi` / `kimi-code` / `anthropic` / `fantasy` / `MiMo/libs/playwright-cli`）经筛选无自进化逻辑，仅作为参考实现。

---

## 1. MiMo-Code 自进化机制（**最完整**）

### 1.1 五大子系统（一句话定义）
1. **/dream** — 扫描近期会话轨迹，把验证过的工程知识写到 `MEMORY.md`，淘汰过时/重复条目。
2. **/distill** — 识别 ≥2 次出现的手动工作流，包装成最小形式（skill / subagent / command）。
3. **Checkpoint writer** — Token 阈值跨越后异步写 11 段固定结构的 checkpoint.md。
4. **Goal-driven loop** — 独立 judge 模型判断 `/goal <stop condition>` 是否达成。
5. **SQLite FTS5 MEMORY.md 体系** — external-content FTS5 + triggers，自动同步全文索引。

### 1.2 关键代码路径速查

| 关注点 | 文件 |
|---|---|
| `/dream` prompt（5 phase） | `packages/opencode/src/agent/prompt/dream.txt` |
| `/distill` prompt（6 phase） | `packages/opencode/src/agent/prompt/distill.txt` |
| Auto Dream 触发判定 | `packages/opencode/src/session/auto-dream.ts` |
| 触发入口（runLoop step 1） | `packages/opencode/src/session/prompt.ts:2919` |
| Memory 服务 | `packages/opencode/src/memory/service.ts` |
| Memory reconcile | `packages/opencode/src/memory/reconcile.ts` |
| Memory 路径解析 | `packages/opencode/src/memory/paths.ts` |
| Memory FTS query | `packages/opencode/src/memory/fts-query.ts` |
| FTS5 schema + 触发器迁移 | `packages/opencode/migration/20260515010000_memory_fts/` + `20260521010000_*` + `20260521020000_*` |
| History FTS5（轨迹级） | `packages/opencode/src/history/{service,writer,extract}.ts` |
| Goal-driven loop | `packages/opencode/src/session/goal.ts` |
| Cron scheduler | `packages/opencode/src/cron/scheduler.ts` |
| Cron-bridge per-session | `packages/opencode/src/session/cron-bridge.ts` |
| Checkpoint 主体（1631 行） | `packages/opencode/src/session/checkpoint.ts` |
| Section-aware budgeted read | `packages/opencode/src/session/budgeted-read.ts` |
| Task registry | `packages/opencode/src/task/registry.ts` |
| Skill search reminder | `packages/opencode/src/session/skill-search-reminder.ts` |

### 1.3 核心模型
- **两层 scope**：`global` / `projects` / `sessions` / `cc`（Claude Code 兼容）。
- **路径布局**：`<data>/memory/{global|projects|<pid>|sessions/<sid>}/<key>.md`
- **项目 ID**：`sha256(absRepoPath).slice(0,12)` — 不暴露绝对路径。
- **fingerprint 增量索引**：`"${size}-${mtimeMs}"` — 不读全文算 hash。

### 1.4 知识写入路径
- Checkpoint writer 子 agent（自动，token 阈值触发）
- `/dream` 与 Auto Dream（默认 7 天间隔）
- `/distill` 与 Auto Distill（默认 30 天间隔）
- `task` tool + TaskRegistry（任务级 progress.md）
- `memory` tool（agent 主动调用）

### 1.5 知识召回路径
- 系统级 reminder（AGENTS.md / CLAUDE.md / skill search）
- Memory.search (FTS5 bm25 + 相对 score floor top*0.15)
- history_fts.around(message_id) — verbatim 字段（连接串/端口/token）精确保留
- Section-aware budgeted read — 超出 token 预算时按段+索引行保留

### 1.6 知识淘汰路径
- reconcile lazy 扫盘删 dead rows（FTS 触发器同步 memory_fts_idx）
- Dream Phase 5 prune：MEMORY.md ≤200 行 / ≤10KB
- Compaction (PruneStage) 70%/100% soft trim
- Task cleanup（7 天后 archived）
- Loop aged_out

### 1.7 限制
- Auto Dream/Distill 只在 top-level session 第一 step 检查，子 session 不触发
- Loop keepalive budget=1 偏激进（PR#1479 修复部分）
- Dream 输出格式硬限制（MEMORY.md ≤200 行/10KB 是 prompt 指南而非硬限制）
- FTS query 在 memory/ 和 history/ 独立演进（OR-join vs AND-join）

### 1.8 可借鉴设计模式（**我们一定要学的**）
1. **External-content FTS5 + 触发器同步** — 主表存全文 + FTS5 virtual 表 + `'delete'` magic command
2. **`buildFtsQuery` 用 phrase-quote 包每个 token** — 安全转义 FTS5 special chars
3. **OR-join + 相对 score floor** — 召回率高，抗 BM25 分数塌缩
4. **Lazy reconcile on search** — 按需保证 search 看到磁盘当前状态
5. **独立 judge 模型 + temperature=0 + verbatim transcript** — `goal.ts:evaluate` 范式
6. **Keepalive algorithm with strike counter + budget** — 只 strike overdue 且模型未 re-arm 的 loop
7. **Checkpoint 11 段 + per-section token budget** — 固定模板 + 预算分配
8. **Rebuild-time microcompact** — boundary 之后对可重生成 tool_result 做 placeholder，保留 tool_use
9. **Task Registry** — 任务有父子树、状态机、owner、idempotent start、terminal 拒绝复活
10. **subscribe-before-seed race fix** — 先订阅再读 seed，避免 busy→idle 事件丢失
11. **shell 注入防护** — `assertSafeComponent()` 拒绝 `..` 和 `/` 前缀

---

## 2. grok-build 自进化机制

### 2.1 三大子系统
1. **cross-session memory**：`~/.grok/memory/` 三层目录（global / workspace / session）
2. **/dream + /flush + /memory** — 三个 slash 命令对应三种写入模式
3. **SQLite FTS5 + sqlite-vec hybrid search** — 关键词 + 向量 KNN 融合

### 2.2 关键代码路径
| 子系统 | 文件 |
|---|---|
| Schema（meta/chunks/chunks_fts/chunks_vec） | `crates/codegen/xai-grok-memory/src/schema.rs` |
| Storage（文件 I/O + 路径解析） | `crates/codegen/xai-grok-memory/src/storage.rs` |
| Chunker（Markdown 感知分块 + ancestor header） | `crates/codegen/xai-grok-memory/src/chunker.rs` |
| Index (SQLite + FTS5 + vec) | `crates/codegen/xai-grok-memory/src/index.rs` |
| Search 编排（hybrid pipeline） | `crates/codegen/xai-grok-memory/src/search.rs` |
| Watcher（lock-free `ArcSwap`） | `crates/codegen/xai-grok-memory/src/watcher.rs` |
| Backend 编排 | `crates/codegen/xai-grok-memory/src/backend.rs` |
| Embedding Provider | `crates/codegen/xai-grok-memory/src/embedding.rs` |
| Dream lock | `crates/codegen/xai-grok-memory/src/dream_lock.rs` |
| Dream | `crates/codegen/xai-grok-memory/src/dream.rs` |
| Query expansion | `crates/codegen/xai-grok-memory/src/query_expansion.rs` |
| MMR 多样性重排 | `crates/codegen/xai-grok-memory/src/mmr.rs` |
| Tool 实现 | `crates/codegen/xai-grok-tools/src/implementations/memory/` |
| 编排层 | `crates/codegen/xai-grok-shell/src/session/{memory_state,memory,slash_commands,acp_session_impl/{turn,memory_dream}.rs}` |
| 注入块（`<memory-context>`） | `crates/codegen/xai-grok-shell/src/session/helpers/memory_context.rs` |
| 压缩触发器 | `crates/codegen/xai-grok-shell/src/session/compaction.rs::maybe_pre_compaction_flush` |

### 2.3 核心模型
- **三层 source**：`global` / `workspace` / `session` — 评分权重、时间衰减、内容过滤都基于它
- **生命周期**：`<data>/memory/{global|workspace/MEMORY.md|workspace/sessions/<sid>.md}`
- **`search_source` 遥测标签**：`"tool"` / `"injection"` / `"compaction_recovery"` — 区分三种召回路径
- **`<memory-context>` prompt-cache 友好注入** — 复用已注入块避免 bust KV cache

### 2.4 知识写入路径
1. `/flush` (user slash) → `sessions/YYYY-MM-DD-flush-<sid8>.md`
2. idle flush (timer) → `sessions/YYYY-MM-DD-interval-<sid8>.md`
3. pre-compaction flush → `sessions/YYYY-MM-DD-pre_compaction-<sid8>.md`
4. `/dream` (slash or auto) → workspace MEMORY.md（**覆盖式**）
5. session-end hook → `sessions/YYYY-MM-DD-<slug>-<sid8>.md`（无 LLM，纯 metadata）

### 2.5 知识召回路径
- **首轮注入**：`first_turn_memory_reminder` — greeting fallback → "project conventions preferences architecture"
- **Tool 调用**：`memory_search` / `memory_get` (`score / source / path (lines) / staleness / 片段`)
- **压缩后注入**：`compaction_recovery` — 复用 tool 配置
- **3 阶段 hybrid pipeline**：sync FTS → async embed → sync merge (BM25 normalize [0,1] + vec 1-dist/2 绝对归一 + max(hybrid, fts) 保护纯 FTS)

### 2.6 知识淘汰路径
- **Time decay**：仅 session source，`e^(-ln2/half_life × age_days)`，默认 7 天
- **Dream 合并**：processed sessions 物理删除 + 索引 delete_path（workspace MEMORY.md 覆盖式重写）
- **GC 孤儿目录**：`tmp*` 空立即删；非空 tmp 7 天后；其他无 sessions 且 >30 天
- **Stale reindex claim**：reindex_claim 60s 过期可抢
- **Stale dream lock**：`.dream-lock` 3600s 或 PID 不存活可抢
- **Dimension mismatch DROP**：embed 模型升级后整个 chunks_vec 重建

### 2.7 限制
- Watcher 依赖 notify crate（Linux inotify max_user_instances 耗尽 risk）
- 单 session 内 actor (LocalSet) 天然串行，多 agent 并发依赖 60s reindex claim
- Dream 的 race condition 已知（`best-effort coordination, not mutual exclusion`）
- Recency guard 5 分钟：跳过 mtime < 5min 的文件（防并发 session 写入冲突），可能导致重复 dream
- Chunk ID = `{path}:{i}` — path 重命名留 zombie chunks（除非 watcher 感知 Remove）
- 三种 search 的 min_score 不统一：tool 0.35 / injection 0.0 / compaction 0.35

### 2.8 可借鉴设计模式
1. **`from_session_params` 工厂方法** + `search_source: &'static str` 类型系统强制标注 caller 角色（**防止 per-site drift**）
2. **`&MemoryIndex` 不跨 `.await` 借用**（`!Send + !Sync` 拆分 sync-callable + async-only + sync-callable 三段）
3. **Lock-free watcher via `ArcSwap`**（notify 线程 `rcu()` 锁自由，search 路径 swap 原子指针）
4. **三层 source 分类**贯穿所有评分策略（decay/source_weight/content_free/staleness）
5. **Prompt cache preservation via `<memory-context>` 持久注入块**
6. **绝对归一向量分数**（`1 - dist/2.0` 防高维 concentration of measure）
7. **Unclamped rank + clamped display**（access boost 用未截断 raw_score 排，display 用 [0,1] 截断）
8. **Endpoint-scoped credentials（fail-closed URL match）**（BYOK 凭证绑定 URL）
9. **append-friendly daily logs**（用 `<!-- flush HH:MM:SS UTC -->` 内嵌版本号）
10. **builder + with_* + 工厂方法**（链式改 vs 从头构造）
11. **Pinned 依赖 + manual ABI transmute 注释**（`// SAFETY: ...` 编译报错 vs 静默 UB）
12. **Recency guard 保护并发写入**（批量删除前先按 mtime 分类）
13. **Cycle-counter 取代绝对时间戳**（`last_flush_compaction` 进程重启 idempotent）

---

## 3. goclaw 自进化机制（**最工业级**）

### 3.1 三条事件驱动流水线
1. **Request Pipeline（同步）** — 8-stage pipeline 每个 turn 跑一次（context → history → prompt → think → act → observe → memory → summarize）
2. **Consolidation Pipeline（事件驱动异步）** — session→episodic→entity.upserted→dreaming
3. **Evolution Cron Pipeline（定时）** — 每 6h SuggestionEngine；周日 3AM EvaluateApplied → Rollback quality drop

### 3.2 关键子系统（按架构分层）

#### Agent Pipeline (核心调度引擎)
- `internal/pipeline/{pipeline.go, stage.go, deps.go, run_state.go, message_buffer.go}`
- 入口：`internal/agent/loop_run.go` → `runViaPipeline` → `pipeline.NewDefaultPipeline(deps).Run(ctx, state)`
- `Stage` interface + `StageWithResult`（区分 `Continue` / `BreakLoop` / `AbortRun`）
- `PipelineDeps` 是 **30+ function 字段** callback 结构体（DI 注入）
- 8 个 stage：context / prune / memory_flush / think / tool / observe / checkpoint / finalize
- **always-on execution**：v3 pipeline 强制路径，consolidation listener 永久订阅，cron ticker 24/7 跑

#### Memory System (3-Tier)
- **L0 Episodic**：`episodic_summaries` + `episodic_search_index`，TTL 90 天，每 6h PruneExpired
- **L1 Long-term**：`memory_chunks` (FTS) + `memory/YYYY-MM-DD.md`
- **L2 Knowledge Graph**：`kg_entities` / `kg_relations` / `kg_dedup_candidates`，10 类节点 + 21 类边

#### Consolidation Pipeline
- `consolidation/{workers.go, episodic_worker.go, semantic_worker.go, dedup_worker.go, dreaming_worker.go}`
- `Register(deps)` 启动时一并注入，返回 cleanup
- 4 个 worker：`episodicWorker` (session→episodic) → `semanticWorker` (episodic→KG) → `dedupWorker` (entity merge) → `dreamingWorker` (L0→L1 promotion)

#### Evolution System
- `internal/agent/suggestion_engine.go` + `suggestion_rules.go`
- 3 rules：`LowRetrievalUsageRule` / `ToolFailureRule` / `RepeatedToolRule`
- `internal/agent/evolution_guardrails.go`：`MaxDeltaPerCycle=0.1` / `MinDataPoints=100` / `RollbackOnDrop=20%`
- `cmd/gateway_evolution_cron.go`：每 6h 分析；周日 3AM 评估回滚（PG advisory lock 0x65766F6C = "evol"）

### 3.3 知识写入路径

1. **L1 Long-term（per-session 事件触发）** — 70% budget 超 → soft trim; 100% budget 超 → MemoryFlushStage → LLM 写 `memory/YYYY-MM-DD.md`
2. **L0 Episodic（每 run 结束事件触发）** — FinalizeStage → emit `domainBus.EventSessionCompleted` → episodicWorker
3. **L2 KG Entities（L0 间接驱动）** — EventEpisodicCreated → semanticWorker → Extractor.Extract (LLM) → IngestExtraction
4. **L2 KG Dedup** — EventEntityUpserted → dedupWorker (HNSW KNN + Jaro-Winkler name match)
5. **Dreaming（L0→L1 晋级）** — debounce per (agent,user) 10min → threshold 5 条 → ComputeRecallScore 加权排序 → LLM 合成 → 写 `memory/_system/dreaming/YYYYMMDD-consolidated.md`
6. **Evolution（行为自调优）** — 6h cron → SuggestionEngine.Analyze → EvolutionSuggestion → admin PATCH 审批 → 写 SKILL.md / 改 threshold

### 3.4 知识召回路径
- **AutoInject L0**：ContextStage 每次 turn 调 → recentContext (最近 2 user 轮, runes=300) → buildRecallQuery → hybrid search → "## Memory Context" section
- **MemorySearchTool**：hybrid FTS + vec + EpisodicStore depth merge + `recordEpisodicRecall` (5s 超时 background)
- **KnowledgeGraphSearchTool**：3 tier fallback（CTE 多跳 → 1-hop relation → search）
- **`scopeClause` 租户隔离**强制

### 3.5 知识淘汰/压缩路径
- **70% budget → soft trim**：head+tail + `[Tool result trimmed: kept first N and last N of M chars]`
- **100% budget → MemoryFlush L1 写盘** + CompactMessages (LLM 摘要前 70% + 后 30% 完整)
- **6h 后台 PruneExpired**：episodic 90 天 TTL
- **Dreaming 晋级**：MarkPromoted but 仍保留，避免重复
- **Evolution Rollback**：周日 3AM 拉 retrieval metrics，对比 baseline，drop > 20% → RollbackSuggestion
- **KG Supersede**：事务内 UPDATE old SET valid_until=NOW + INSERT new SET valid_from=NOW

### 3.6 限制
1. **EmbeddingProvider 强耦合 pgvector** — SQLite edition 自动 FTS-only
2. **Embedding 后台是裸 goroutine** — 重启后丢失
3. **dedupThreshold 硬编码** — 0.98 自动 / 0.90 候选，per-agent 配置在 `DedupAfterExtraction` 路径不生效
4. **L0 auto-inject 没有 L1 fallback** — LLM 主动 `memory_search` 才看 L1
5. **TTL 写死 90 天** — `MemoryConfig.EpisodicTTLDays` 字段存在但未读取
6. **Dreaming debounce 进程内 sync.Map** — 多实例不协调
7. **Rollback 兜底缺口**：`EvaluateApplied` 对 `SuggestSkillAdd` / `SuggestToolOrder` 直接 continue，无自动回滚
8. **`MaxDeltaPerCycle=0.1` 永远从 0.3 起步累加** — A/B 信息丢失
9. **Consolidation 失败重试 3 次** — dedup 不幂等
10. **8-stage 描述 vs 实际 stage 错位** — README "8 个名字" 不等于 8 个 stage class
11. **`MemoryFlushCompactionCount` 去重只在 session 一致时** — 多并发 run 漏写
12. **没有 emergency-all-mem-flush** — OOM/disk full 无 graceful flush

### 3.7 可借鉴设计模式
1. **Stage interface + DI callback 注入** — 全 30+ function-field injection
2. **BuildRecentContext + buildRecallQuery** — rune-safe 防 CJK 切断
3. **Knuth 风格 FTS+Vector hybrid scoring** — 不分场景就 RAG 是低效的
4. **Two-phase pruning** — 70% soft-trim / 100% LLM compaction
5. **`Try-Then-Compile`-Shape Pipeline Safety** — `tryEmergencyCompaction` 紧急压缩
6. **`set -e` for Stages** — Stage 返回 `error` 立即 fail，结果用 optional `StageWithResult`
7. **`SourceID` 幂等性** — `"{sessionKey}:{compactionCount}"` session+计数
8. **EventBus 解耦 worker / 多订阅 fan-out**
9. **Permission-aware auto-merge + Jaro-Winkler name match** — 同人名不合并
10. **`PruneStats` callback 模式** — 把可观测性嵌入函数签名
11. **`FinalizeCtx = context.WithoutCancel(ctx)`** — 即使上游 cancel 也要落盘
12. **`MarkCacheTouched AFTER mutate`** — 只看真变没变
13. **`math.Log1p / log decay` 唤醒 scoring** — `0.30·freq + 0.35·rel + 0.20·recency + 0.15·freshness`
14. **Plugin-style tool wiring** — `SetMemoryStore/SetEpisodicStore/SetKGStore` 一个一个 set
15. **命名以"子系统"为粒度** — `cmd/lifecycle_shell_deny_groups.go`

---

## 4. hermes-agent 自进化机制（**plugin 抽象最优雅**）

### 4.1 三层两条并行管道
- **System Prompt（frozen）** — 启动一次性装入 MEMORY.md/USER.md snapshot
- **Turn-time injection (per turn)** — 用户消息侧车注入 `<memory-context>`
- **Write-side 3 路并行**：main 模型主动调 memory tool / sync_turn 后台写 / Review Agent
- **Recall-side 2 层**：builtin frozen snapshot / external prefetch + queue_prefetch

### 4.2 关键子系统

#### `MemoryProvider` 抽象基类（agent/memory_provider.py）
```python
class MemoryProvider(ABC):
    def name(self) -> str
    def initialize(self, session_id, **kwargs)
    def system_prompt_block(self) -> str  # OPT
    def prefetch(self, query, *, session_id="") -> str  # OPT sync (8s timeout)
    def queue_prefetch(self, query, *, session_id="") -> None  # OPT async
    def sync_turn(self, user, asst, *, session_id="", messages=None)  # OPT async
    def get_tool_schemas() -> List[Dict]
    def handle_tool_call(tool_name, args, **kwargs) -> str
    def shutdown()
    # Lifecycle hooks (all OPT)
    on_turn_start / on_session_end / on_session_switch / on_pre_compress / on_memory_write / on_delegation
```

#### `MemoryManager` 编排器（agent/memory_manager.py）
- **单 worker 序列化**：`DaemonThreadPoolExecutor(max_workers=1)` — FIFO 写顺序
- **失败隔离**：每个 provider try/except，单失败不影响其他
- **8s 外部 prefetch 超时** — 失败 = 该 turn 无 external recall，但主对话继续（fail-open）
- **5s shutdown drain** — daemon + bounded drain
- **3 类 durability** (`write` / `prefetch` / `boundary`) 分别统计放弃数

#### 内置 `MemoryStore`（tools/memory_tool.py）
- **frozen snapshot** 模式 — `load_from_disk()` 一次性 capture 整 session 不动
- **字符预算**：`memory_char_limit=2200` / `user_char_limit=1375`
- **all-or-nothing abort**：超额 → final-state 校验失败
- **`_consolidation_failure`** 计数 ≥3 → 强制 turn terminal（防 fragility loop）
- **`_detect_external_drift`** 检测非 tool 形状 → `.bak.<ts>` + 拒绝 mutation
- **Threat pattern 双层扫描**：load-time sanitization + runtime re-sanitization

#### 三方 memory 后端（8 个 bundled plugins）
- `honcho` / `hindsight` / `holographic` / `openviking` / `mem0` / `supermemory` / `retaindb` / `byterover`
- 每个 plugin 的 `__init__.py` 实现 `register(ctx)`
- `ProviderField` dataclass declarative config schema

#### Background Review Agent
- **触发**：每 10 turn（`memory.nudge_interval` 配置）
- **完全后台** — 主对话已发回用户响应后才 fork
- **fork AIAgent**：`skip_memory=True` / `_memory_nudge_interval=0` / `_persist_disabled=True`
- **共享 `_memory_store`** — 写出去的 MEMORY.md/USER.md 与主 agent 同份
- **pin `tools[]`** byte-identical 命中 provider prompt cache prefix
- **Prompt 模板** — "Review the conversation above and consider saving..."

#### Mirror 机制
- 每次 `memory` tool 成功 → `notify_memory_tool_write` → 每个 non-builtin provider `on_memory_write`
- Honcho `add + target=user` → `create_conclusion()`
- Holographic `add` → `add_fact(category="user_pref")`

### 4.3 知识召回路径
- **启动期 frozen snapshot** — `load_from_disk()` 一性 capture，整 session 不变
- **每 turn 同步召回** — `_prefetch_provider` 起 daemon thread，join ≤8s
- **每 turn 异步预热** — `queue_prefetch_all` → 下一 turn `prefetch()` 消费
- **工具化召回** — `provider.handle_tool_call`
- **Honcho 多 pass dialectic**：
  - Pass 0：cold/warm base prompt
  - Pass 1：self-audit "what gaps remain?"
  - Pass 2：reconciliation "do these assessments cohere?"
  - `_signal_sufficient` 检查 (`##` / `•` / `^[*-]` / `^\d+\.`) 早退

### 4.4 知识淘汰/压缩路径
- **字符预算淘汰** — 2200/1375 硬天花板，`_consolidation_failure` 强制模型在同 turn remove+add
- **外部漂移检测** — `.bak.<ts>` 备份 + 拒绝 mutation（issue #26045）
- **Threat pattern 阻断** — `scope="strict"` load+write 双层
- **压缩期 `on_pre_compress`** — 让 provider 在压缩前抢救
- **Session 切换** — `commit_session_boundary_async` FIFO 序列化 end → switch
- **Skill Curator** — pure 函数 `apply_automatic_transitions` 按 last_activity_at 转移 active → stale → archived（30/90 天）

### 4.5 限制
- MEMORY.md/USER.md 硬上限 2200/1375 chars，靠 `_consolidation_failure` 强制
- frozen snapshot 同一 session 内新增/替换对模型不可见
- 单 external provider 强制（无法同时跑 Honcho+Hindsight）
- Prefetch 时延：Honcho .chat() 1-3s 起步 + cadence; Hindsight cloud 120s
- 后台 Review 每 10 turn fork AIAgent，多花一次 LLM 调用
- session_strategy 默认 per-session 让 Honcho 不累积
- 跨 provider 无一致性约束 — 三套 schema 互不相通，没有 fan-in reconcile
- MEMORY.md 内容安全：snapshot 阻断 strict scope，但 live state 保留原文
- 多用户冲突：HERMES_HOME 做 profile 隔离，但 `memories/` 共享，多用户场景需 external provider

### 4.6 可借鉴设计模式（**我们一定要学的**）
1. **Frozen Snapshot + Sidecar** — prefix-cache 友好 + 持久化字节一致
2. **单 worker 序列化后台写入** — `max_workers=1` 跨 provider / 跨 turn FIFO
3. **Daemon thread pool + bounded shutdown drain** — wedged thread 不阻塞进程退出
4. **Fail-open + 隔离 provider 失败** — 每个 provider try/except，不能让一个错误传播
5. **Background Review Agent（fork AIAgent 做维护）** — 不阻塞主对话
6. **Drift Guard** — `_detect_external_drift` round-trip 自检 + `.bak` 备份
7. **Atomic temp + rename + flock** — `mkstemp + fsync + atomic_replace`
8. **Threat-pattern 双层扫描（load + write）** — 双层防御
9. **Session Lifecycle Hooks 显式化** — 6 个 hook 覆盖所有 session 边界翻转点
10. **Declarative Config Schema + 通用渲染** — `ProviderField` dataclass
11. **流水线式 Layered Prefetch** — cheap base + expensive supplement 各层独立 cadence
12. **Per-pass reasoning level** — multi-pass 早 pass 轻、晚 pass 重、与 query 长度联动

---

## 📊 4 大项目对比矩阵

| 维度 | MiMo-Code | grok-build | goclaw | hermes-agent |
|---|---|---|---|---|
| 持久化 | SQLite FTS5 | SQLite FTS5+vec0 | PG + pgvector | 各 provider 自选 |
| 知识层级 | 2 层（session/project） | 3 层（global/workspace/session） | 3 层（L0/L1/L2 KG） | provider-defined |
| KG | 无 | 无 | 有（10 节点 21 边） | Honcho/Hindsight 提供 |
| 蒸馏器 | /dream + /distill | /flush + /dream | LLM 摘要 + L0→L1 | sync_turn 后台 |
| 知识写入触发 | 阈值 + cron | 阈值 + cron | 阈值 + cron | 同步 turn |
| 召回方式 | FTS5 only | hybrid FTS+vec | hybrid FTS+vec + KG | provider prefetch |
| KV cache 友好 | ⭐⭐ | ⭐⭐⭐（注入块复用） | ⭐ | ⭐⭐⭐（frozen snapshot） |
| Plugin 抽象 | ⭐⭐（skill） | ⭐（无） | ⭐⭐（tool stage） | ⭐⭐⭐（MemoryProvider） |
| 工业级（高可用） | ⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ |
| 自动参数调优 | 无 | 无 | ⭐⭐⭐（Evolution cron） | 无 |
| 代码复杂度 | 高 | 中 | 高 | 中 |
| 我们要学的精华 | Section-aware budget + 独立 judge + checkpoint 11 段 | hybrid search + 三层 source + ArcSwap watcher | Stage interface + EventBus + Evolution guardrails | MemoryProvider 抽象 + Background Review + Drift Guard |

---

## 🎯 我们要实现的 mocode 自进化系统的核心选择

| 来源 | 启发 → 我们的实现 |
|---|---|
| MiMo-Code `/dream` + `/distill` | **Distinguisher**：每 N turn 后台 fork agent 审会话，提取偏好/规则 |
| MiMo-Code FTS5 external-content | **Indexer**：复用 mocode 现有 SQLite + FTS5 |
| grok-build 三层 source | **Scope 分层**：global (user profile) / project (workspace facts) / session (turn log) |
| grok-build `<memory-context>` block | **Injector**：每 turn 注入到 prompt，prefix-cache 友好 |
| grok-build hybrid (FTS+vec) | **Recall**：mocode Phase 1 只用 FTS5，vec 留待 v2 |
| goclaw 8-stage pipeline | **Pipeline hook**：在 coordinator 的合适阶段插入 evolution 写入 |
| goclaw Consolidation Pipeline | **Event-driven**：用 mocode 现成 session/message store 提取事件 |
| goclaw Evolution guardrails | **Auto-tune**：根据 turn score 自动调 tool threshold，带 rollback |
| hermes-agent MemoryProvider 抽象 | **Layered**：先 built-in MEMORY（用现有 memory.Service），留 external 扩展点 |
| hermes-agent Background Review | **Reviewer**：每 N turn 后台 fork，而非每 turn（控制 LLM 成本） |
| hermes-agent Drift Guard | **Dedupe**：写入前用 embedding/keyword 检查重复 + similarity ≥0.92 跳过 |
| hermes-agent `_consolidation_failure` | **Bound**：MEMORY.md 字符预算，超过则强制 turn 终态 |

---

## 📂 最终代码骨架（详见 architecture 文档）

```
internal/core/evolution/               # 自进化系统主体
├── reviewer.go                         # Background Review Agent (hermes-agent 启发)
├── dreamer.go                          # L0→L1 dreaming (MiMo-Code 启发)
├── evolution.go                        # 自动参数调优 (goclaw 启发)
├── distillation/                       # LLM 抽取 + 去重
│   ├── extractor.go                    # 偏好/规则抽取 prompt
│   └── dedupe.go                       # 相似度去重 (Drift Guard)
├── pipeline/                           # 8-stage 风格的 hook 接入
│   ├── after_turn_hook.go              # turn 完成后触发 review
│   └── idle_hook.go                    # idle 触发 dream
├── scorer/                             # turn 评分（已有 scoreTurn 的扩展）
│   └── metrics.go                      # retrieval / tool failure 计数
└── cmd/                                # evolution cron (every 6h)
    └── evolution.go                    # SuggestionEngine + guardrails
```
