# Self-Evolution Plans

> This directory holds design and reference material for mocode's self-evolution subsystem. The implementation lives at `internal/core/evolution/`.

## 📑 索引

| 文件 | 内容 |
|---|---|
| [`00-upstream-survey.md`](00-upstream-survey.md) | 对 MiMo-Code / grok-build / goclaw / hermes-agent 4 个上游项目的"自进化"机制的代码级学习报告。**先读这份**。 |
| [`01-architecture-design.md`](01-architecture-design.md) | mocode 自进化系统的总体架构设计：3 层记忆 + 2 个 cron + 1 个 online judge 的闭环。 |
| [`02-implementation-notes.md`](02-implementation-notes.md) | 实施记录、决策、局限、与上游系统的差异。 |

## 🚀 快速上手

```go
import "github.com/nextsko/mocode-agent/internal/core/evolution"

sys, err := evolution.NewSystem(evolution.Deps{
    Memory:     app.Memory,
    Sessions:   app.Sessions,
    Messages:   app.Messages,
    SmallModel: smallLLM,
    Recorder:   app.SessionRecorder,
    Config: evolution.Config{
        Enabled:            true,
        ReviewEnabled:      true,
        ReviewInterval:     10,                // review every 10 turns
        DreamEnabled:       true,
        DreamMinInterval:   30 * time.Minute,  // dream when idle > 30m
        AutoTuneEnabled:    true,
        AutoTuneInterval:   6 * time.Hour,
    },
}, evolution.Scope{AppName: "mocode", UserID: userID})

// In coordinator's turn-end hook:
app.AgentCoordinator.SetEvolution(sys)
sys.AfterTurn(turnNum)        // called automatically by coordinator

// In coordinator's idle main loop:
sys.TickIdle(turnCount)       // called periodically

// At shutdown:
sys.Shutdown(ctx)
```

## ⚙️ 默认行为

v1 的所有自进化功能都是 **opt-in**：默认 `Config.Enabled = false`。开启后子项仍可独立控制。开启后：

- **Reviewer**：每 10 个 turn 后台提取用户偏好/项目规则/事实
- **Dreamer**：会话 idle 超过 30 分钟（且 ≥ 20 turn）时做一次大整理
- **Evolution Cron**：每 6 小时跑 auto-tune，按 guardrails 自动调参并支持回滚

## 🎯 借鉴自上游的关键设计

| 来源 | 启发 → 我们的实现 |
|---|---|
| hermes-agent `background_review.py` | Reviewer（每 N turn 后台 fork agent 提取偏好） |
| MiMo-Code `/dream` + `/distill` | Distillation 流水线（Extractor → Deduper → Writeback） |
| MiMo-Code external-content FTS5 + triggers | 复用 mocode 现有 SQLite + 6 个 memory tools |
| grok-build 三层 source (global/workspace/session) | Scope 分层（每 (app,user) 一份 Evolution System） |
| grok-build `<memory-context>` block | 接 mocode 现有 memory_search tool，由模型自己召回 |
| goclaw 8-stage pipeline + event bus | turn-end hook + idle loop hook，无独立 event bus |
| goclaw evolution_cron + guardrails | EvolutionCron + MinDataPts/MaxDelta/Rollback |
| hermes-agent `_detect_external_drift` | Deduper 用 Jaccard ≥ 0.85 阈值做去重 |

详见 `00-upstream-survey.md` 与 `01-architecture-design.md`。