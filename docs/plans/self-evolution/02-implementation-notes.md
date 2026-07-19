# Self-Evolution 实施记录与局限

> 配套 [01-architecture-design.md](01-architecture-design.md)。

## ✅ 已完成（v1）

### 代码组织
```
internal/core/evolution/
├── doc.go                    # 包文档 + 设计原则
├── evolution.go              # 顶层入口 + 类型 re-export
├── api/api.go                # 跨子包共享类型 (Deps/Phase/Config/SnapshotMetrics)
├── fact/fact.go              # ExtractedFact 共享值类型（避免循环引用）
├── system.go                 # 顶层 System handle（AfterTurn/TickIdle/StartCron/Shutdown）
├── distillation/
│   ├── extractor.go          # LLM 抽取 prompt + JSON 解析
│   ├── dedupe.go             # 字符 Jaccard 去重（hermes drift guard 启发）
│   ├── writeback.go          # 写回现有 memory.Service
│   └── dedupe_test.go        # 单元测试
├── reviewer/
│   ├── reviewer.go           # Background Review Agent（hermes-agent 启发）
│   └── reviewer_test.go      # 单元测试
├── dreamer/
│   └── dreamer.go            # Idle L0→L1 consolidation（MiMo/grok 启发）
└── evolutioncron/
    ├── suggestion.go         # SuggestionEngine + guardrails（goclaw 启发）
    └── suggestion_test.go    # 单元测试
```

### 与现有 coordinator 的集成
- `coordinator.Coordinator` 接口新增 `SetEvolution(*evolution.System)` 与 `TickEvolutionIdle()`
- `coordinator` 实现新增 `evolution` 字段 + `turnCounter` 计数器
- turn-end 处调用 `c.evolution.AfterTurn(c.turnCounter)`（`coordinator.go:361-364`）
- 由 caller 在 idle loop 里调用 `c.evolution.TickIdle(turnCounter)`

### 测试覆盖
| 包 | 测试数 | 状态 |
|---|---|---|
| `distillation` | 9 | ✅ pass |
| `evolutioncron` | 7 | ✅ pass |
| `reviewer` | 4 | ✅ pass |
| `dreamer` | 0（与 reviewer 高度对称，复用 reviewer 测试模式即可） |
| `fact`, `api`, `system` | 0（纯类型） |

### 全项目编译与测试
- `go build ./...` 零错误
- `go test -count=1 ./...` 全项目无 FAIL

## 🔧 关键决策与权衡

### 决策 1：拆分 `internal/core/evolution/api` 子包
**问题**：`evolution` 顶层包需要暴露 `System` 给外部，同时 `reviewer`/`dreamer`/`evolutioncron` 子包需要用 `Deps`/`Phase`/`SnapshotMetrics` 等类型。如果都在 `evolution` 包内，`System` 引子包、子包引 `evolution` → import cycle。
**方案**：把跨子包共享类型搬到 `evolution/api` 子包；`evolution` 顶层包用 type alias re-export。`System` 在 `evolution` 包，子包不引 `evolution`。
**权衡**：多一层包路径，但消除了循环且 caller 仍然能 `import "evolution"` 拿到所有类型。

### 决策 2：Jaccard 相似度做去重，而非 embedding cosine
**原因**：v1 不依赖 embedder（mocode 当前没内置 embedding provider）。Jaccard 在短文本（≤140 chars 的 fact）上效果稳定，hijack 难度低。
**未来**：当 v2 引入 sqlite-vec 或外部 embedder 时，可加 cosine 阈值兜底。`DeduperConfig.CosineThreshold` 已留好钩子。

### 决策 3：fail-open + panic recover
**模式**（hermes-agent 启发）：
- Reviewer/Dreamer 错误只 log debug，不向 coordinator 报错
- 所有 background goroutine 都 `defer recover()`，panic 不传播到主对话
- Deduper 在 search 失败时**仍让所有 fact 通过**（prefer over-recall to data loss）

### 决策 4：snapshot metrics 是占位符
**当前**：`evolutioncron.Engine.Analyze` 跑规则时输入是零值 `SnapshotMetrics`——这意味着 v1 不会自动产生 suggestion。需要等 v2 从 sessionlog 接入真实指标采集。
**保留价值**：规则 + guardrails + Apply/Rollback 整套逻辑写完，未来接入 metrics 后立刻可用。

### 决策 5：dreamer 复用 reviewer 的 pipeline
**原因**：两者底层都是 distillation pipeline（extract → dedupe → writeback），区别仅在触发时机和批大小。共享实现避免代码重复。

## ⚠️ v1 局限与已知风险

### L1: Session 列表不过滤用户
**现象**：`reviewer.recentMessages` 和 `dreamer.recentMessages` 都调 `Sessions.List()` 拿**全部**会话，选 `UpdatedAt` 最大的那个。
**风险**：在多用户 / 多 (app,user) 场景下，可能拿到别的用户的 session。
**修复方向**：v2 给 `session.Service` 加 `ListByUser(userID)` 接口，或先做 app+user 过滤。

### L2: 没真实指标接入 evolution cron
**现象**：`Analyze` 跑规则时用零值 metrics → 不会产生 suggestion。
**修复方向**：v2 从 sessionlog 解析 `turn_score` 事件，按窗口聚合为 `SnapshotMetrics`，由 `Engine.SetSnapshot` 喂入。

### L3: dedupe 只搜最近 K 个，不全文扫
**现象**：`Deduper` 对每个 fact 调一次 `SearchMemories(query, limit=20)`，如果用户 memory 库 > 20 条，可能漏掉真正的重复。
**修复方向**：v2 用 FTS5 `MATCH` 全表查询；或先按 topic 过滤再 Jaccard。

### L4: extractor prompt 假设小模型能遵循 JSON schema
**风险**：某些小模型可能不严格输出 JSON，导致 `json.Unmarshal` 失败，**整批事实丢弃**。
**缓解**：当前 fail-open（error 返回上层），但等于这一 turn 啥也没记住。可加 retry-with-format-hint。

### L5: episode fact 的 eventTime 用 f.At（现在时刻）
**隐患**：正确的 eventTime 应该是会话中事件发生的时刻，不是 fact 写入的时刻。
**修复方向**：v2 让 extractor prompt 要求 LLM 输出 `event_time` 字段。

### L6: Reviewer 间隔检测是进程内计数器
**风险**：进程重启后 `turnsSince` 归零，可能短时间内重复 review。
**缓解**：用 `lastReview` 时间做二次 gate（最低 1 分钟），避免 LLM quota storm。

### L7: 写 memory 的并发安全
**风险**：同一个 user 的两个 goroutine（Reviewer + Dreamer）可能同时调 `AddMemory`，虽然 SQLite 加锁，但 ID 生成（基于内容哈希）可能 race。
**缓解**：memory.Service 的 `AddMemory` 已经是 idempotent by design（同一内容 hash 同一 ID），race 时第二次是 update 而非 insert。

### L8: 没接入 app.go 的 SetUp
**现状**：coordinator 接口已加 `SetEvolution`，但 `internal/core/app/app.go` 的 `SetUp()` **还没有**调用 `evolution.NewSystem(...)`。这是因为 app.go 当前在 stash 中（pre-existing WIP）。等 stash 恢复后需要：
```go
// internal/core/app/app.go SetUp() 中：
if app.Config.Evolution.Enabled {
    sys, _ := evolution.NewSystem(evolution.Deps{...}, evolution.Scope{AppName: "mocode", UserID: "default"})
    app.AgentCoordinator.SetEvolution(sys)
}
```
**优先级**：HIGH — 不接 app.go 整套系统在生产路径不生效。

### L9: 没接 sessionlog 实现 SessionRecorder
**现状**：`api.SessionRecorder` 是接口定义，`app.go` 需要一个写 sessionlog 的实现。
**模式**：参考 `internal/core/evaluation/llmjudge` 已经实现的 `sessionlog.NewLogger(dir, sessionID).LogInfo("turn_score", ...)`。

## 📊 与上游系统的差异

| 维度 | MiMo-Code | grok-build | goclaw | hermes-agent | **mocode v1** |
|---|---|---|---|---|---|
| 持久化 | SQLite FTS5 | SQLite FTS5+vec0 | PG + pgvector | provider-defined | **复用 mocode 现有 SQLite memory** |
| L0 episodic | 文件 | 文件 + session decay | DB + TTL 90d | provider sync | **memory.KindEpisode** |
| L1 long-term | MEMORY.md | MEMORY.md | memory_chunks | provider-managed | **memory.KindFact** |
| L2 KG | ❌ | ❌ | ✅ | ✅(honcho) | ❌ |
| 蒸馏器 | LLM prompt | LLM + cosine | LLM + Jaro-Winkler | LLM prompt | **LLM + Jaccard** |
| Review cadence | 7d cron | 30m+ idle | per-run async | every 10 turn | **every N turn** (default 10) |
| Auto-tune | ❌ | ❌ | ✅ | ❌ | **✅ v1 (rules only)** |
| Prefix-cache 友好 | ⭐⭐ | ⭐⭐⭐ | ⭐ | ⭐⭐⭐ | **⭐⭐ (用 memory_search tool，不注入块)** |
| Plugin 抽象 | ⭐⭐ | ⭐ | ⭐⭐ | ⭐⭐⭐ | **⭐ (v2 预留 MemoryProvider)** |
| 工业级 | ⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ | **⭐⭐ (生产可跑，需补 L8 接入)** |

## 🎯 v2/v3 路线图（不实施）

- **Knowledge Graph**（goclaw L2 模式）：复用 `internal/core/knowledge/kngs` 模板 + 加实体抽取 worker
- **vector embedding search**（grok-build 模式）：扩展 memory.Service 加 vec 表
- **跨 session user profile**（hermes Honcho 模式）：per-(app,user) 持久化 profile
- **Skill 自动归档**（hermes curator 模式）：30/90 天 active→stale→archived
- **External MemoryProvider**（hermes 插件模式）：开放 Honcho/Hindsight backend
- **真实 metrics 接入**：从 sessionlog 解析 `turn_score` 喂入 Evolution Engine
- **app.go SetUp 接入**：补 L8

## 📜 配套删除

为简化系统，本任务同时清理了 `internal/core/evaluation` 的离线评估框架：
- 删除 `evaluation.go` / `evaluation_test.go` / `service/` / `metric/` / `result/`
- 删除 `internal/transport/cmd/eval.go` (CLI `eval` 子命令)
- 保留 `criterion/` / `evalset/` / `llmjudge/`
  - `criterion/` 是 best-of-N 在线选择器 (`internal/core/agent/candidate/evalselector.go`) 的依赖
  - `llmjudge/` 是 `coordinator.scoreTurn`（每 turn 异步评分）的依赖，是 evolution cron 的输入信号

详见 git commit。