 # Mocode 内循环闭环进化系统设计

 ## 目标

 让 agent 在运行中持续从自己的行为产物里学习:错误到纠正(反例环)、效果到优化(正例环)、经验到固化(Patch 环)。三个环共用同一套观测数据,注入出口已就绪,缺的是生产端和部分信号采集。

 ## 现状(三 explorer 审计结论)

 **一通一断两条线**:
 - ErrorLearner(工具错误到3次到纠正注入)是唯一真正接通的闭环,但只吃 OnToolResult.IsError 一种信号。
 - Patch 环库和注入出口(injectEvolutionContext)齐全,但 CreatePatch/ApplyAll 全仓库零调用,生产者从未实现。
 - 观测层"建好管道没接水龙头":sessionlog 8 类方法 + event.go 3 个空函数,实际只写 session_start/session_end。
 - errcoll(shell 错误)只落盘不回读,和 ErrorLearner 并行割裂。
 - evaluation 完全游离在运行时之外(只在离线 CLI 用)。

 ## 已完成:观测层接通(第一颗螺丝)

 | 信号 | 采集点 | 落盘类别 | 状态 |
 |------|--------|----------|------|
 | 工具调用 | OnToolCall | toolcall | 已接 |
 | 工具错误 | OnToolResult(IsError) | bug | 已接 |
 | 步骤 token 用量 | OnStepFinish | info(step_usage) | 已接 |
 | 推理轨迹 | OnReasoningEnd | thinks | 已接 |

 实现:toolutil.SessionLoggerSink 接口 + SessionLoggerContextKey,coordinator 在 Run 时注入 sessionLogSink(适配 sessionlog.Logger)到 ctx,agent_lifecycle 的回调通过 toolutil.GetSessionLogger(ctx) 取出落盘。零侵入 sessionAgent 字段,生命周期由 coordinator 管理。

 ## 三环设计骨架

 ```
 反例环(已通):   工具错误 -> ErrorLearner.Record -> 3次 -> PrepareStep 注入纠正
 错误进化环(待建): errcoll + provider错误 + 循环检测 -> 统一采集 -> 喂 ErrorLearner
 效果进化环(待建): session trace -> evaluation 在线打分 -> Patch(rule/skill/memory)
 Patch 环(待建):  Patch 生产者(读 sessionlog/分数 -> CreatePatch) -> injectEvolutionContext 注入
 ```

 注入出口都已存在:PrepareStep(纠正)、injectEvolutionContext(Patch)。缺的是生产端。

 ## 下一步(按杠杆排序)

 ### 1. 错误进化环:统一错误源
 把 errcoll(.mocode/errors/*.jsonl)和 provider 错误(401/超限/循环检测)接入 ErrorLearner 或新建通用 learner。现在 ErrorLearner 只吃工具结果错误,provider/hook 失败信号全丢。

 ### 2. 效果进化环:evaluation 在线化
 给 sessionAgent.Run 加 turn 结束后的轻量打分钩子(复用 evaluation 包的 criterion/llmjudge),让每次 turn 的质量分进入数据流。这是把"效果到进化"接上的现成抓手,evaluation 包已建好但孤立。

 ### 3. Patch 生产者:闭环核心
 实现"进化 agent":定时/触发式扫描 sessionlog(现在有料了)和评分数据,用 LLM 提炼出 rule/skill/memory patch,调 CreatePatch 落到 .mocode/patches/。读侧 injectEvolutionContext 自动注入。需定义 Patch 收敛机制(何时 MarkApplied),否则会撑爆 context。

 ## 参考
 - ErrorLearner:internal/evolution/error_learner.go(Record, CorrectionContext, PromoteThreshold=3)
 - Patch 线:internal/evolution/evolution.go(PatchStore, BuildContext, ApplyAll)
 - 注入出口:internal/agent/coordinator.go(injectEvolutionContext)
 - 观测层(新):internal/agent/toolutil/context.go(SessionLoggerSink)、internal/agent/agent_lifecycle.go(OnToolCall/OnToolResult/OnStepFinish/OnReasoningEnd)

## 已完成:Patch 生产者(闭环核心)

`internal/evolution/producer.go` 是之前完全缺失的写侧:读 sessionlog 的 bug 日志,聚类重复工具错误,达到阈值(默认 3 次)后产出一个 `KindRule` patch 到 `.mocode/patches/`。下次 agent 运行时,读侧 `injectEvolutionContext` 自动把它注入 system prompt。

```
sessionlog bug.md → Producer.Produce() → CreatePatch(KindRule) → injectEvolutionContext 注入
```

- 阈值可配(`--min-repeats`),重跑幂等(按 title 去重,不重复产)
- 5 个测试全绿:创建 patch、重跑去重、阈值以下不产、空目录安全、JSON block 解析
- CLI:`mocode evolve` 扫描全部 session bug 日志并产 patch

至此**错误反例环结构上完全闭合**:工具报错 → sessionlog 落盘 → evolve 聚类产 patch → 注入纠正。这是原来"一通一断"里断掉的那一半,现在通了。

## 已完成:Patch 收敛机制(防退化)

之前 `injectEvolutionContext` 每次注入所有未应用 patch 且从不 `MarkApplied`,patch 会无限累积进 system prompt 撑爆 context。

现在:`Patch` 加 `InjectCount`/`MaxInjects` 字段,`BuildContext` 每次注入递增计数,达 `MaxInjects` 自动 `Applied` 退出未应用集。Producer 默认 `MaxInjects=10`(重复曝光 10 次后视为已学,自动退出)。`MaxInjects=0` 表示永不自动收敛(需手动 apply)。

测试覆盖:自动收敛(2 次后退出)、MaxInjects=0 永不收敛、规则加载。3 个新测试全绿。

## 已完成:错误源统一(错误进化环)

之前 errcoll(`.mocode/errors/*.jsonl`)只落盘不回读,和 sessionlog 是两条并行割裂的错误源。现在 Producer 同时扫描两处:

- sessionlog 的 `bug.md`(工具错误,由观测层 OnToolResult 落盘)
- errcoll 的 `errors-YYYYMMDD.jsonl`(shell/工具错误,由 errcoll.Collector 落盘)

按 (tool, error fragment) 聚类,跨两个源合并计数,达阈值即产 patch。这打通了 errcoll 的回读路径——它不再是单向漏斗。

测试覆盖:errcoll 单独产 patch、sessionlog+errcoll 合并计数达阈值。2 个新测试全绿。

## 已完成:效果进化环(evaluation 在线化)

之前 evaluation 包完全游离在运行时之外(只在 `mocode eval` 离线用)。现在 `coordinator.Run` 在每次成功 turn 后异步 `go c.scoreTurn()`:

- 用 small model 作 LLM judge(`llmjudge.Judge`),按通用 helpfulness rubric 打分
- 分数落到 sessionlog 的 `turn_score` 记录
- 完全 best-effort、非阻塞:失败静默,不影响返回结果

这把"效果→进化"的正例环接上了:高质量 turn 是正信号,低质量 turn 进 sessionlog 可被 producer/分析工具消费。同一套判分能力离线(evaluation)和在线(scoreTurn)复用。

## 闭环总览(三环全闭合)

```
反例环(已通):    工具错误 → ErrorLearner → 3次 → PrepareStep 纠正注入
错误进化环(已通): sessionlog bug + errcoll → Producer 聚类 → KindRule patch → 注入(MaxInjects 收敛)
效果环(已通):     成功 turn → scoreTurn(LLM judge) → turn_score 落 sessionlog
Patch 环(已通):   Producer.Produce(读 bug/errors) → CreatePatch → injectEvolutionContext(BuildContext 注入,MaxInjects 自动 Applied)
```

触发:`mocode evolve` 扫描错误源产 patch;`scoreTurn` 在每个成功 turn 后自动跑。

## 已完成:正反馈环消费者(turn_score → quality patch)

之前 turn_score 只是单向写入 sessionlog,无消费者。现在 Producer 读 info.md 的 turn_score 记录,当平均分持续低于阈值(<0.5,且样本数 >=3)时,产出一个 quality-improvement rule patch,提醒 agent 提高响应质量。正反馈环(效果→进化)现在端到端闭合:scoreTurn 写分 → Producer 消费低分 → quality patch → 注入。

顺带修了一个隐藏 bug:emitRulePatch/produceQualityPatch 之前返回 `patch.ID`,但 CreatePatch 按值传递不会回填 ID,导致返回空串。改用 `filepath.Base(patchDir)`。新增 6 个测试覆盖。
