# 当前状态与集成计划

## 一、现状速览

`internal/core/evolution/` 下的子模块已基本写完：

- `fact`：事实类型定义
- `reviewer`：每 N turn 后台 review
- `dreamer`：idle 时 dream
- `distillation`：Extractor → Deduper → Writeback 流水线
- `evolutioncron`：auto-tune + guardrails
- `api`：跨包类型与接口
- `system.go`：顶层 System 句柄

**但关键问题：这些代码目前没有被 coordinator 调用。**

## 二、已确认的问题

### L1：Reviewer / Dreamer 未接线
`internal/core/agent/coordinator.go` 的 `coordinator` struct 和 `NewCoordinator` 里：
- 没有 `reviewer.Reviewer` 字段
- 没有 `dreamer.Dreamer` 字段
- 没有 `evolutioncron.Engine` 字段
- `Run()` 末尾没有 `AfterTurn`
- 没有 `TickIdle` 调用点
- Shutdown 没有 `Shutdown`

结果是：reviewer/dreamer 的写记忆链路虽然代码完整，但**永远不会被执行**。

### L2：EvolutionCron 未接线
`evolutioncron` 在 `internal/core/evolution/evolutioncron/suggestion.go` 中实现，但：
- 没有在任何地方 `NewEngine`
- 没有 `Run(ctx)` 或 `StartCron(ctx)`
- 默认 emitter 只打 debug 日志

结果是：auto-tune 永远不会跑，suggestion 只存在于内存中，不持久化。

### L3：SessionRecorder 未实现
`api.SessionRecorder` 当前只有 `nopRecorder`，没有接线到 sessionlog。所以即使 review/dream 跑了，也没有审计日志。

### L4：无 UI 可见入口
- TUI 没有 evolution 状态面板
- `transport/cmd` 没有 `/evolution` slash 命令
- 用户无法查看 review/dream/cron 的产出

### L5：memory 路径已存在但无进化内容
本地 `%LOCALAPPDATA%\mocode\projects\<项目哈希>\memory\entries.jsonl` 已存在，但内容只有用户主动记忆，没有 evolution 产物。

## 三、下一步计划

### 阶段 A：最小可运行集成（让代码跑起来）
目标：让 reviewer/dreamer 真正被调用并写 memory。

1. **扩展 `coordinator` struct**
   - 加字段：`reviewer *reviewer.Reviewer`、`dreamer *dreamer.Dreamer`
   - 加字段：`evolutionEnabled bool`

2. **扩展 `NewCoordinator`**
   - 读取配置项（`ReviewEnabled`、`DreamEnabled`、`AutoTuneEnabled`）
   - 如启用，初始化 `reviewer.New` 和 `dreamer.New`
   - 传 `memory`、`SmallLanguageModel`、`SessionRecorder`、`app/user scope`

3. **接入 turn-end 钩子**
   - 在 `Run()` 返回前调用 `c.reviewer.AfterTurn(turnNum)`
   - 只在 `evolutionEnabled && reviewer != nil` 时执行

4. **接入 idle 钩子**
   - 在 coordinator 主循环或 `Run()` 中加 `c.dreamer.TickIdle(turnCount)`
   - 只在 idle 超过阈值时执行

5. **Shutdown 优雅退出**
   - 调用 `c.reviewer.Shutdown(ctx)`
   - 调用 `c.dreamer.Shutdown(ctx)`

6. **实现 `SessionRecorder`**
   - 将 `api.SessionRecorder` 接线到 sessionlog service
   - 让 `RecordEvolutionEvent` 真正落盘

### 阶段 B：Cron 接线
目标：让 auto-tune 真正跑起来。

1. 在 `NewCoordinator` 中初始化 `evolutioncron.NewEngine`
2. 读取 snapshot metrics（sessionlog + memory stats）
3. 启动 `go c.cron.Run(ctx)` 或显式 `StartCron(ctx)`
4. Shutdown 时 cancel

### 阶段 C：UI 可见性（可选但重要）
目标：让用户能看到进化系统的产出。

1. **Slash 命令**
   - `/evolution`：查看最近 suggestion / review / dream 状态
   - `/evolution history`：查看历史记录
   - `/evolution apply <id>` / `/evolution rollback <id>`：管理 suggestion

2. **TUI 状态提示**
   - 右下角或状态栏显示 evolution 状态（idle / reviewing / dreaming）
   - 新记忆产生时的轻量提示

## 四、风险评估

| 风险 | 影响 | 缓解 |
|---|---|---|
| 每 turn 调 LLM 成本 | 高 | `ReviewInterval` 默认 10 turn，时间门控 1m |
| memory 并发写冲突 | 中 | `memory.Service` 已提供 `entries.jsonl.lock` 文件锁 |
| Shutdown 时 goroutine 泄漏 | 低 | `Shutdown` + `errgroup` / `sync.WaitGroup` |
| 用户隐私（自动写记忆） | 高 | 默认 `Enabled = false`，需用户显式开启 |
| 配置项爆炸 | 中 | 集中在 `evolution.Config`，避免散落 |

## 五、实施顺序建议

```
Phase A（最小集成）
  -> Phase B（Cron）
    -> Phase C（UI）
```

每个阶段独立可测试，不阻塞主流程。

## 六、待确认问题

1. **配置来源**：是否加在 `mocode.json` 的顶层 `evolution` 节点，还是复用现有 agent 配置？
2. **Scope 粒度**：每 `(app, user)` 一个 System，还是每个 session 一个？
3. **Reviewer 模型**：复用 `SmallLanguageModel`，还是单独指定模型？
4. **SessionRecorder 实现**：复用 `internal/store` 的 JSONL writer，还是直接调用 sessionlog？
