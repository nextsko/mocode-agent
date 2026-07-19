# mocode 自进化系统 — 架构设计

> 设计目标：让 mocode agent **越用越懂用户**，自我持续积累经验并不断更新提升。
> 来源：基于对 MiMo-Code / grok-build / goclaw / hermes-agent 4 大上游项目的代码学习（详见 `00-upstream-survey.md`）。
> 范围：v1 实现范围 = "核心闭环（精简版）"：L0 episodic + L1 semantic fact + Background Review Agent + Evolution Cron。

---

## 🎯 设计原则

1. **不引入新的持久化抽象** — 复用 mocode 现有的 `internal/core/knowledge/memory.Service`（带 SQLite + 6 个 tools）和 session/message store。
2. **可插拔** — 借鉴 hermes-agent 的 MemoryProvider 抽象，但 v1 只实现 built-in 实现。
3. **prefix-cache 友好** — 借鉴 grok-build 的 `<memory-context>` 块模式 + hermes-agent 的 frozen snapshot。
4. **失败隔离** — 借鉴 hermes-agent 的 fail-open pattern，单个组件失败不影响主对话。
5. **可观测** — 借鉴 goclaw 的 `PruneStats` 模式，把可观测性嵌入函数签名。
6. **可回滚** — 借鉴 goclaw 的 Evolution guardrails 模式，自动调参必须带 rollback 机制。
7. **pluggable 回调注入** — 借鉴 goclaw 的 `Stage interface + PipelineDeps` 模式，所有外部依赖（LLM、memory、session）通过依赖注入传入。

---

## 🏗️ 整体架构

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                       mocode 现有架构 (不动)                                  │
│                                                                              │
│  internal/core/agent/coordinator.go — SessionAgent 入口                      │
│                              ↓                                              │
│  internal/core/agent/candidate/ — best-of-N 候选选择                          │
│  internal/core/evaluation/criterion + llmjudge — 打分原语（极简化后保留）    │
│  internal/core/knowledge/memory/ — 6 个 memory tools + Service 接口           │
│  internal/core/knowledge/kngs/ — 模板 + 知识图谱 store（v1 只用其模板）     │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ↓
┌──────────────────────────────────────────────────────────────────────────────┐
│                       自进化系统 v1（本任务实现）                              │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                       🎭 Reviewer Agent（后台）                       │   │
│  │  触发：每 N turn（默认 10）                                          │   │
│  │  行为：fork agent，调 memory_add/update 写偏好/规则/事实              │   │
│  │  启发：hermes-agent background_review                                │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                  ↓                                           │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                       🌙 Dreamer（idle cron）                        │   │
│  │  触发：会话 idle > 30min                                               │   │
│  │  行为：扫描最近 K turn，调 LLM 抽取 → 去重 → 写 L1 long-term fact    │   │
│  │  启发：MiMo-Code /dream + grok-build /flush                          │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                  ↓                                           │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                       📈 Evolution Cron（每 6h）                     │   │
│  │  行为：SuggestionEngine 跑规则 → EvolutionSuggestion                  │   │
│  │       → admin PATCH 审批 → 自动 apply + guardrails                   │   │
│  │  启发：goclaw evolution_cron                                          │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
                                  ↓
┌──────────────────────────────────────────────────────────────────────────────┐
│                       Distillation pipeline                                  │
│                                                                              │
│  1. Extractor（LLM，distillation/extractor.go）                              │
│     - 输入：最近 K turn 的 message trace                                      │
│     - prompt：抽取"持久的用户偏好 / 项目规则 / 验证过的事实"                  │
│     - 输出：[]ExtractedFact{Content, Topics, Kind, Confidence}                │
│                                                                              │
│  2. Dedupe（drift guard，distillation/dedupe.go）                             │
│     - 输入：新 fact + memory.Service.SearchMemories                           │
│     - 行为：Jaccard 相似度 ≥0.85 或 emb cosine ≥0.92 → skip                   │
│     - 启发：hermes-agent _detect_external_drift + grok-build is_content_free  │
│                                                                              │
│  3. Writeback（distillation/writeback.go）                                    │
│     - 调用 memory.AddMemory(...) 写入 L1                                     │
│     - 字符预算约束：MEMORY.md-equivalent 的 snapshot 不超 2200 chars           │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 📁 目录结构（新增文件）

```
internal/core/evolution/
├── README.md                       # 本目录的设计理念
├── evolution.go                    # 总入口（启动 wiring）
├── option.go                       # Option 模式
├── deps.go                         # Deps 注入结构体（所有外部依赖）
├── distillation/
│   ├── extractor.go                # LLM 抽取 prompt（hermes/MiMo 启发）
│   ├── extractor_test.go
│   ├── dedupe.go                   # 相似度去重（hermes-agent drift guard 启发）
│   ├── dedupe_test.go
│   └── types.go                    # ExtractedFact / FactKind / Confidence 阈值
├── reviewer/
│   ├── reviewer.go                 # Background Review Agent（hermes-agent 启发）
│   ├── reviewer_test.go
│   └── prompts.go                  # review prompt + extraction prompt
├── dreamer/
│   ├── dreamer.go                  # L0→L1 dreaming（MiMo-Code /dream 启发）
│   ├── dreamer_test.go
│   └── gate.go                     # debounce + sessions threshold（goclaw 启发）
├── evolution/
│   ├── suggestion.go               # SuggestionEngine（goclaw 启发）
│   ├── suggestion_test.go
│   ├── guardrails.go               # MaxDeltaPerCycle / MinDataPoints / Rollback
│   ├── guardrails_test.go
│   └── metrics.go                  # retrieval / tool failure 计数
├── pipeline/
│   ├── hook.go                     # Stage interface + Deps 注入
│   ├── after_turn.go               # turn-end hook（reviewer trigger）
│   └── idle.go                     # idle hook（dreamer trigger）
└── cmd/
    ├── evolution_cron.go           # 每 6h 跑 evolution
    └── dream_cron.go               # idle 5min 跑 dream
```

---

## 🔑 关键设计决策

### 决策 1：复用 mocode 现有 `memory.Service`，不新建 store

**为什么**：mocode 已经有：
- `memory.Service` 接口（AddMemory/UpdateMemory/DeleteMemory/ClearMemories/ReadMemories/SearchMemories）
- 6 个 fantasy.AgentTool：`memory_add` / `memory_update` / `memory_delete` / `memory_clear` / `memory_search` / `memory_load`
- `internal/domain/memory` 已经有 `KindFact` / `KindEpisode` 区分事实 vs 情节

**怎么做**：把 L0 episodic 当 `KindEpisode` 写，L1 semantic fact 当 `KindFact` 写。所有 extraction 结果存到现有 memory store。

**风险**：现有 memory 库没有"重要性 / confidence / participants / location"的高级字段，但这是 v2 范围。v1 用 `[Topics]` 字段打标签即可。

---

### 决策 2：Reviewer 用 Background Agent 模式（不阻塞主对话）

**启发**：hermes-agent 的 `background_review.py` 模式。

```
turn N 完成
  ↓
turnFinalizer.Execute() (集成在 coordinator 末尾)
  ↓
if should_review_memory(turns_since_last_review >= 10) {
    go coordinator.SpawnReviewAgent(userID, lastMessages)
    // 立即 return，不阻塞主对话
}
  ↓
review agent 独立运行：
   1. 调 LLM 抽取偏好/规则/事实
   2. dedupe vs existing memory
   3. 调 memory_add 写入 L1
   4. 写 session log 记录 provenance
  ↓
下一 turn 可读 new L1（snapshot 缓存，下次 reload 才注入 prompt）
```

**为什么不用 cron 而用 turn-end trigger**：cron 与 turn 错位时无法精确知道"上一段会话在哪儿结束"。turn-end trigger 是事件驱动，lazy fresh。

---

### 决策 3：Dreamer 走 idle cron（不每 turn 跑）

**启发**：goclaw consolidation pipeline + MiMo-Code `/dream` 默认 7 天 + grok-build 默认 7 天 half-life。

```
turn N 完成后 → emitTurnEndEvent(sessionID)
  ↓
dreamGate(sessionID) {
    if busy → skip
    if last_dream < 30min 前 → skip
    if accumulated_turns < 20 → skip
    return open
}
  ↓
if open → spawn dreamer goroutine
  ↓
dreamer.scanRecentTurns(sessionID, K=50)
  ↓
for each turn:
    LLM 抽取 ExtractedFact
    dedupe vs memory
    memory_add(KindFact)
  ↓
sessionLog.LogInfo("dream_completed", ...)
```

**为什么是 idle 不是 cron**：cron 跨进程协调复杂，进程重启会丢失 dream gate 状态。idle hook 在 coordinator 进程内自己 tick 即可。

---

### 决策 4：Evolution Cron 提供"建议"而非"自动改"

**启发**：goclaw `EvolutionGuardrails` 的 "SuggestSkillAdd / SuggestToolOrder / SuggestThreshold"，但 v1 不改工具权重，只调整 retrieval 相关参数。

**apply 类型**：

| 调整项 | 触发 | 上限 | 回滚 |
|---|---|---|---|
| `recall_threshold` | retrieval usage < 20% | ±0.1/cycle, baseline ±[0.3, 0.95] | 24h 后 usage 下降 > 20% → 还原 |
| `memory_injection_top_k` | recall 命中率 < 30% | ±2/cycle, [3, 15] | 同上 |

**为什么 v1 不改 prompt / tool**：改 prompt 是高风险（可能让模型输出格式破坏），改工具权重需要 provider 扩展点。v1 聚焦"可量化"参数。

---

### 决策 5：Drift Guard 在写入前 dedupe，不删已存在

**启发**：hermes-agent `_detect_external_drift` + grok-build `is_content_free`。

```
writeback.Write(extractedFact):
    existing, _ := memory.SearchMemories(userID, extractedFact.Content)
    for each existing:
        if Jaccard(extractedFact, existing) >= 0.85 → skip
        if cosineSim(embed(extractedFact), embed(existing)) >= 0.92 → skip
    memory.AddMemory(extractedFact)  // 只追加，不删
```

**为什么不删**：删除是高风险（删除错的 fact 会让 agent "忘记"用户事实）。v1 只追加，long-term fact 自然积累。`memory_clear` 由用户主动调用。

---

### 决策 6：可观测性内置（不另起 metrics 服务）

**模式**：goclaw `PruneStats` 模式 — 把 stats 嵌入函数返回值。

```go
type DreamResult struct {
    Scanned      int
    Extracted    int
    Deduped      int
    Written      int
    Skipped      int
    Duration     time.Duration
    Errors       []error
}

func (d *Dreamer) Run(ctx context.Context) (DreamResult, error) { ... }
```

调用方：
- coordinator 把 stats 写 session log
- evolution cron 把 stats 写 metrics table（也复用 SQLite）

---

### 决策 7：失败隔离（fail-open + 单 component 错误不影响主对话）

**启发**：hermes-agent `_prefetch_provider` 8s timeout + try/except 每个 provider。

```go
// reviewer.go
func (r *Reviewer) AfterTurn(ctx context.Context, turnNum int) {
    if r.shouldReview(turnNum) {
        go func() {
            defer func() {
                if rec := recover(); rec != nil {
                    slog.Error("reviewer panic", "err", rec)
                }
            }()
            if err := r.runReview(context.Background()); err != nil {
                slog.Debug("reviewer failed", "err", err)
            }
        }()
    }
}
```

**关键**：
- reviewer 错误只 log debug，不返回给主对话。
- panic 永远 recover，不让 reviewer crash coordinator。
- 写 memory 失败不重试（retry 会重复消耗 quota）。
- dreamer 失败同 reviewer。

---

## 🔌 与现有 mocode 模块的集成点

### 集成点 A：coordinator.AfterTurnHook

**位置**：`internal/core/agent/coordinator.go` 的 turn end 路径

**接入**：
```go
// coordinator.go 中 turn 完成 callback 末尾追加:
if c.evolutionReviewer != nil {
    c.evolutionReviewer.AfterTurn(ctx, turnCounter)
}
```

**为什么这样集成**：
- 不破坏现有 coordinator 流程。
- evolution 是 opt-in（`Option` 注入）。
- 测试时可注入 fake reviewer。

---

### 集成点 B：idle main loop poll

**位置**：`internal/core/agent/coordinator.go` 的 main loop

**接入**：
```go
// coordinator.go 的 Run 主循环中:
for {
    select {
    case msg := <-incoming:
        handle(msg)
    case <-ticker.C:
        if c.dreamer != nil {
            c.dreamer.Tick(ctx)  // 检查 gate，必要时 spawn dreamer
        }
    }
}
```

---

### 集成点 C：cmd 启动 wiring

**位置**：`internal/transport/cmd/` 下

**接入**：
```go
// cmd.go 的 SetUp() 中:
evolutionReviewer := evolution.NewReviewer(evolution.ReviewerDeps{
    Memory:    app.Memory,
    Sessions:  app.Sessions,
    Provider:  app.Provider,
    SmallLLM:  app.SmallLLM,
    Interval:  cfg.Evolution.ReviewInterval,
})
app.AgentCoordinator.SetEvolutionReviewer(evolutionReviewer)
```

---

## ⚙️ 配置项（在 `internal/core/config` 添加）

```go
type EvolutionConfig struct {
    // 关闭总开关
    Enabled bool `yaml:"enabled"`

    // Background Review Agent
    ReviewEnabled    bool          `yaml:"review_enabled"`
    ReviewInterval   int           `yaml:"review_interval"`     // default 10 turns
    ReviewTimeout    time.Duration `yaml:"review_timeout"`      // default 60s
    ReviewModel      string        `yaml:"review_model"`        // empty → use small model

    // Dreamer (idle L0→L1)
    DreamEnabled     bool          `yaml:"dream_enabled"`
    DreamMinInterval time.Duration `yaml:"dream_min_interval"`  // default 30m
    DreamMinTurns    int           `yaml:"dream_min_turns"`     // default 20
    DreamBatchSize   int           `yaml:"dream_batch_size"`    // default 50
    DreamTimeout     time.Duration `yaml:"dream_timeout"`       // default 5m

    // Evolution cron
    AutoTuneEnabled     bool          `yaml:"autotune_enabled"`
    AutoTuneInterval    time.Duration `yaml:"autotune_interval"`     // default 6h
    AutoTuneMinDataPts  int           `yaml:"autotune_min_data"`     // default 100
    AutoTuneMaxDelta    float64       `yaml:"autotune_max_delta"`    // default 0.1

    // Dedup
    DedupJaccardThreshold  float64 `yaml:"dedup_jaccard"`    // default 0.85
    DedupCosineThreshold   float64 `yaml:"dedup_cosine"`     // default 0.92 (when embed enabled)
}
```

**默认行为**：`Enabled=false` — 自进化 v1 是 opt-in，用户显式开启才跑。开启后所有子项走各自 default。

---

## 📊 数据流图

### 写入

```
session turn 完成
    ↓
coordinator.AfterTurn(turnNum, sessionID)
    ↓
Reviewer.AfterTurn(ctx, turnNum)
    ↓
if turnNum % 10 == 0:
    go Reviewer.Review(ctx, sessionID, lastMessages)
        ↓
        Extractor.Extract(ctx, prompt, messages) → []ExtractedFact
        ↓
        Dedupe.Dedupe(ctx, facts, memory.SearchMemories) → []SurvivingFact
        ↓
        Writeback.Write(ctx, surviving) → memory.AddMemory(...)
        ↓
        sessionLog.LogInfo("review_completed", stats)
```

### 召回

```
用户输入 turn
    ↓
coordinator.Run(prompt) (现有逻辑)
    ↓
SessionAgent 在 memory 包注入时查询:
    memory.SearchMemories(userID, query) → top 5 by score
    ↓
拼到 system prompt 末尾（prefix-cache 友好）
```

### 调整

```
每 6h: Evolution Cron
    ↓
SuggestionEngine.Analyze()
    ↓
跑规则:
    - if retrieval_usage_rate < 0.2 over 100+ queries
        → SuggestLowerRecallThreshold(current=0.3, delta=0.1, new=0.2)
    - if recall_hit_rate < 0.3 over 100+ queries
        → SuggestHigherTopK(current=5, delta=2, new=7)
    ↓
写 EvolutionSuggestion table
    ↓
admin PATCH /api/evolution/suggestions/:id {action: "approve"}
    ↓
apply guardrails (MinDataPts >= 100, MaxDelta 0.1)
    ↓
update app config
    ↓
24h 后 usage 监控：
    if new usage < baseline * 0.8:
        RollbackSuggestion
```

---

## 🧪 测试策略

每个组件必须有：

| 组件 | unit test | integration test | 行为保证 |
|---|---|---|---|
| extractor | mock LLM 返回固定 JSON | end-to-end 提一条期望 | 输出 JSON 解析正确 |
| dedupe | 准备 2 相似 + 1 不相似 input | e2e 重复跑 3 次 | 重复跳过，新事实写入 |
| reviewer | mock memory + mock LLM | 跑 10 turn 后断言 memory 增加 | fail-open 验证 |
| dreamer | mock 20 turn messages | 跑 dreamer 断言 fact 写入 | idle gate 跳过验证 |
| guardrails | 准备 baseline + new 异常 | 模拟 rollback 触发 | rollback 完整性 |
| suggestion | 跑 rules 在固定 metrics | 验证 Suggestion 表写入 | 规则触发正确性 |

---

## 🛡️ 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| Background Review 写入大量无意义 fact | memory 库被污染 | dedupe + confidence threshold (≥0.5) 才写 |
| Dream LLM 抽错事实 | 用户被错误画像 | 每次写都标 `[unverified]`，定期 review 可手动 remove |
| Evolution 阈值调坏 | recall 命中率下降 | guardrails rollback + MaxDataPts ≥100 保护 |
| 后台 LLM 调用打满 quota | 用户付费用 LLM 资源浪费 | ReviewerMaxTokens + DreamMaxTokens，disable by config |
| 并发 writer 冲突 memory.Service | SQLite write lock | memory.Service.AddMemory 已经是幂等，dedupe 保证 |
| prefix-cache busted 让注入 prompt 变 | 浪费 token | `<memory-context>` 块 ID 一致 + 内容只在变化时改 |

---

## 📌 后续 v2/v3 规划（不实现）

- **Knowledge Graph（v2）**：复用 `internal/core/knowledge/kngs` 模板 + 加 LLM 实体抽取 worker（学 goclaw L2）
- **vector embedding search（v2）**：扩展 memory.Service 加 vec 表（学 grok-build）
- **跨 session user profile（v3）**：hermes-agent Honcho 模式，per-workspace / global 策略
- **Skill 自动归档（v3）**：hermes-agent curator，30/90 天 active→stale→archived
- **External MemoryProvider（v3）**：hermes-agent plugin 体系，引入 Honcho / Hindsight
- **Evaluator 全量回放（v3 重建）**：goclaw 8-stage + cron consolidation 模式
