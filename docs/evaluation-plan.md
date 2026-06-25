 # Mocode Evaluation 体系落地计划

 给 agent 自动化打分的考试系统。参考 trpc-agent-go 的 `evaluation/` 架构，适配 mocode 的 `SessionAgent` + `charm.land/fantasy`。

 ## 目标

 每改一次 prompt / 换一个模型 / 调一次参数，都能跑同一套考题，量化对比是变好还是变坏。补上 agent 框架里"能跑但无法度量"的最后一块。

 ## 数据流

 ```
 EvalSet(用例集) -> Inference(调 SessionAgent.Run) -> Evaluate(Criterion 评分) -> EvalResult(成绩单)
 ```

 - Inference 阶段直接复用现有 `agent.SessionAgent.Run`，不引入新的 runner 抽象。
 - Evaluate 阶段拿 `*fantasy.AgentResult` 里的工具调用记录和最终回复，喂给 Criterion 打分。

 ## 设计决策

 1. **模型层不抄 trpc**:mocode 已用 `fantasy` + `catwalk` 做多 provider 抽象,只补 failover 弹性能力,不重造 model/provider。
 2. **Inference 走 SessionAgent**:trpc 用 `runner.Runner`,mocode 用 `SessionAgent.Run`。`fantasy.AgentResult` 自带工具调用与回复,正好喂给评分器。
 3. **精简掉 trace/toolmock/usersimulation**:trpc 有,但 mocode 首版不需要。首版聚焦"给定输入 -> 跑 agent -> 打分"最小闭环。
4. **Criterion 三维度**:ToolTrajectory(工具轨迹)/ FinalResponse(文本匹配)/ LLMJudge(模型打分)。

## 基于深度审计的修正(三 explorer 并行探查结论)

### 修正一:ground truth 主源用 message.Service,不只靠 fantasy.AgentResult

原计划假设评分输入是 `SessionAgent.Run` 返回的 `*fantasy.AgentResult`。实际 mocode 的 `message.Service.List(sessionID)` 才是完整 trace 主源——它按 sessionID 返回结构化 `Message.Parts`,含 ToolCall(name+input)、ToolResult(content+is_error)、ReasoningContent、Finish reason,并通过 `ParentSessionID` + `AgentToolCallID` 构成会话树,能还原单 agent 和多 agent 圆桌的全部执行过程。`fantasy.AgentResult` 只含最终态,拿不到中间工具调用链。

- 设计调整:Inference 阶段调 `SessionAgent.Run` 后,立刻 `messages.List(sessionID)` 采集完整 trace,二者都存进 `InferenceResult`。
- 评分器优先消费 `[]message.Message`,`fantasy.AgentResult` 只做兜底(取最终文本)。

### 修正二:工具 mock 走 ToolPlugin/Registry,不改 SessionAgent

原计划模糊了 mock 注入点。实际 mocode 有现成路径:`tools.NewRegistryWithPlugins(...)` 接受自定义插件集,`ToolPlugin.Build(ctx, ToolDeps)` 是统一构造点。要做确定性打桩,构造一个返回 mock `fantasy.AgentTool` 的 `ToolPlugin` 即可,无需改 `buildTools`。

- 注意:coordinator-owned 工具(agent/roundtable/agentic_fetch/transfer)绕过注册表单独构建,mock 这些要单独处理。
- `toolutil.WrapFull(inner, befores, afters)` 是工具级拦截的天然点,可经 `wrapToolsWithHooks` 同款路径注入观测/metrics。

### 修正三:成本指标直接读 Session,不重算

`Session` 结构已持久化每会话 PromptTokens/CompletionTokens/CacheReadTokens/CacheCreationTokens/Cost,`IncrementCost` 保证并发子 agent 安全回传。token 成本指标直接读 session,不必重新解析 trace。

### 修正四:LLMJudge 复用 provider 构造路径,不必拉起完整 SessionAgent

`coordinator.buildAnthropicProvider`/`buildOpenaiProvider`(coordinator.go:775 起)展示了如何从 config 构造 `fantasy.LanguageModel`。judge 可走同一套 provider 路径直接调模型,比拉起一个完整 SessionAgent 更轻。memory.Service 和 kngs 都是确定性存储(关键词+sha256),不含 LLM,不可复用——LLMJudge 是新组件。

### hermes-agent 佐证:评估体系确实缺位

深度探查确认 hermes-agent(集大成者)也无独立评估体系——只有压缩效率指标和粗粒度 tool 成败统计,没有任务完成度比对或 LLM 评判。这反向印证了给 mocode 补 evaluation 的价值:三个项目里只有 trpc-agent-go 做成了体系,mocode 抢先补上等于形成差异化优势。

## hermes-agent 的两个可同步借鉴点

探查发现两块与 evaluation 正相关的成熟设计,建议一并吸收:

1. **IterationBudget(预算驱动循环 + grace/refund)**:mocode 的评估 Inference 循环可引入显式 Budget 类型,支持 consume/refund/remaining,工具粒度区分是否计入预算——避免低成本工具白白烧掉迭代配额,也便于评估时控制单用例最大轮数。
2. **原地软归档 schema(active/compacted 双标志位)**:若 evaluation 结果存 SQLite,可借鉴此 schema:压缩不删历史,只翻标志位、同事务插新记录。对 evaluation 意味着历次评分结果可追溯、可比对,而非覆盖。

## trpc-agent-go 的杀手级组合:bestofn 复用 evaluation

探查发现 trpc-agent-go 的 `runner/bestofn` 把候选选择做成了 `CandidateSelector` 接口,而 `evaluationSelector` 直接复用整个 evaluation 子系统做 pointwise/pairwise 打分。这是 evaluation 建成后的高阶应用:同一个评分管线既能离线评测,也能在线从 N 个候选回复里选最优。mocode 建成 evaluation 后可同样把它接进 Coordinator 的 roundtable 或多路生成路径,无需重复造打分逻辑。

 ## 目录结构

 ```
 internal/evaluation/
   evalset/
     evalset.go        # EvalSet + Manager
     evalcase.go       # EvalCase + ToolCall
   criterion/
     criterion.go      # Criterion 聚合(三选一/组合)
     finalresponse.go  # 文本匹配(精确/包含/正则)
     tooltrajectory.go # 工具序列匹配
     llmjudge.go       # 复用 SessionAgent 做 judge
   metric/
     metric.go         # EvalMetric + Manager
   result/
     result.go         # EvalSetResult / CaseResult / MetricResult + Manager
   service/
     service.go        # Service 接口(Inference + Evaluate)
     local.go          # 本地实现,接 SessionAgent
   evaluation.go       # Evaluator 顶层入口
 ```

 ## 分阶段实施

 四个阶段,每阶段可独立编译验证。严格按顺序,后阶段依赖前阶段。

 ### 阶段一:数据模型层

 纯 struct + interface 定义,零外部依赖。

 - [ ] `evalset/evalset.go`:`EvalSet` + `Manager`(Get/List/AddCase/Close)
 - [ ] `evalset/evalcase.go`:`EvalCase`(ID/UserContent/ExpectedTools/ExpectedText/Rubrics/SystemPrompt)+ `ToolCall`
 - [ ] `metric/metric.go`:`EvalMetric`(Name/Threshold/Criterion)+ `Manager`
 - [ ] `result/result.go`:`EvalSetResult` / `CaseResult` / `MetricResult` / `Status` 枚举 + `Manager`

 验证:`go build ./internal/evaluation/...` 通过。

 ### 阶段二:纯文本评分(最小闭环)

 不依赖 LLM,验证评分逻辑本身。

 - [ ] `criterion/criterion.go`:`Criterion` 聚合结构(ToolTrajectory/FinalResponse/LLMJudge 三个指针字段)
 - [ ] `criterion/finalresponse.go`:实现精确匹配 / 包含匹配 / 正则匹配,输出 `MetricResult`

 验证:写单测,构造一个 `fantasy.AgentResult`(含最终文本),跑 FinalResponse criterion,断言分数正确。

 ### 阶段三:接入 SessionAgent(Inference 闭环)

 把 agent 真正接进评估流程。

 - [ ] `service/service.go`:`Service` 接口(Inference + Evaluate + Close)
 - [ ] `service/local.go`:本地实现
   - Inference:遍历 EvalSet.Cases,逐个调 `agent.SessionAgent.Run`,收集 `*fantasy.AgentResult`
   - Evaluate:对每个 Inference 结果跑配置的 metric,聚合 `EvalSetResult`

 验证:构造 1 个真实 EvalCase(带 ExpectedText),跑 `service.local` 全流程,断言产出非空 `EvalSetResult`。

 ### 阶段四:LLMJudge + 多轮汇总

 补齐模型打分与统计能力。

 - [ ] `criterion/llmjudge.go`:复用一个 `SessionAgent` 当 judge,按 rubrics 打分,返回 Score + Reason
 - [ ] `criterion/tooltrajectory.go`:工具调用序列匹配(比对 `fantasy.AgentResult` 里的 tool calls)
 - [ ] `evaluation.go`:`Evaluator` 顶层入口,支持 `numRuns` 多轮跑 + 汇总 Summary
 - [ ] `result` 加 `Summary`:多轮通过率 / 平均分

 验证:构造 2 个 EvalCase,跑 3 轮,断言 Summary 统计正确。

 ## 接口骨架(编码时直接对照)

 ```go
 // evaluation.go
 type Evaluator interface {
     Evaluate(ctx context.Context, evalSetID string, opts ...Option) (*result.EvalSetResult, error)
     Close() error
 }
 func New(a agent.SessionAgent, opts ...Option) (Evaluator, error)

 // service/service.go
 type Service interface {
     Inference(ctx context.Context, req *InferenceRequest) ([]*InferenceResult, error)
     Evaluate(ctx context.Context, req *EvaluateRequest) (*result.EvalSetResult, error)
     Close() error
 }
 type InferenceRequest struct {
     EvalSetID string
     Agent     agent.SessionAgent   // 复用 mocode 现有 agent
     CallOpts  agent.SessionAgentCall
 }
 type InferenceResult struct {
     EvalID string
     Result *fantasy.AgentResult    // SessionAgent.Run 的原始产出
     Status result.Status
     Error  string
 }

 // criterion/criterion.go
 type Criterion struct {
     ToolTrajectory *ToolTrajectory
     FinalResponse  *FinalResponse
     LLMJudge       *LLMJudge       // Runner 字段是 agent.SessionAgent
 }

 // metric/metric.go
 type EvalMetric struct {
     Name      string
     Threshold float64
     Criterion *criterion.Criterion
 }

 // result/result.go
 type Status string
 const ( StatusPassed Status = "passed"; StatusFailed = "failed"; StatusError = "error" )
 type MetricResult struct { MetricName string; Score float64; Passed bool; Reason string }
 ```

 ## 模型层弹性(可选,独立于 evaluation)

 evaluation 之外,唯一值得从 trpc 借鉴的能力:

 - [ ] `internal/agent` 加 `ModelWithFailover` 包装,给 `fantasy.LanguageModel` 套主备切换(参考 `trpc-agent-go/model/failover`)
 - 不抄 hedge(尾延迟优化,初期用不上)
 - 不抄 token tailoring(mocode 已有 `ctxcompress.Pipeline`)

 ## 参考

 - trpc-agent-go evaluation 接口:`P:\coding\trpc-agent-go\evaluation\`(evaluation.go / service/service.go / evalset/evalset.go / metric/metric.go)
- mocode 现有 agent 接口:`P:\coding\mocode\internal\agent\agent.go`(SessionAgent / SessionAgentCall / Model)
- mocode 模型依赖:`charm.land/fantasy` v0.31.0 + `charm.land/catwalk` v0.44.14

## 实施状态(已全部完成并验证)

四个阶段 + LLMJudge + CLI 集成全部落地。`go build ./...`、`go vet ./internal/evaluation/... ./internal/cmd/`、`go test ./internal/evaluation/...` 全绿。

| 阶段 | 产物 | 验证 |
|------|------|------|
| 一 数据模型 | `evalset/`、`criterion/`、`metric/`、`result/` 的 struct + Manager 接口 | go build |
| 二 评分核心 | `criterion.FinalResponse`(exact/contains/regex)、`ToolTrajectory`(exact/subset)、`Criterion.Eval`(聚合 + trace 提取) | 单测全绿 |
| 三 服务层 | `service.local`:Inference 走 `session.Create -> agent.Run -> messages.List`,Evaluate 喂 trace 给 criterion | 全流程测试 |
| 四 顶层入口 | `evaluation.Evaluator`:numRuns 多轮 + `result.Summarize` 汇总 | 多轮汇总测试 |
| LLMJudge | `llmjudge.Judge`(实现 `criterion.Judger`,复用 `fantasy.LanguageModel`) | parseScore 单测 |
| JSON 加载 | `evalset.LoadSet` + MarshalJSON/UnmarshalJSON | round-trip 单测 |
| CLI 集成 | `mocode eval <set.json>` 命令(`coordinatorAgent` 适配器接 `app.AgentCoordinator`/`Sessions`/`Messages`) | `mocode eval --help` 可用 |

实际目录结构(已实现):

```
internal/evaluation/
  evaluation.go          # Evaluator 顶层入口 + numRuns/Summary
  evalset/               # EvalSet/EvalCase + Manager + JSON
    inmemory/            # 内存 Manager(测试/本地)
  criterion/             # Criterion + FinalResponse/ToolTrajectory/Judger seam
  metric/                # EvalMetric + Manager
    inmemory/            # 内存 Manager
  result/                # EvalSetResult/CaseResult/MetricResult/Summary + Summarize
  service/               # Service 接口 + local 实现(接 SessionAgent/session/message)
  llmjudge/              # criterion.Judger 的 LLM 实现(复用 fantasy.LanguageModel)
examples/evaluation/
  sample-evalset.json    # 可直接用的示例评估集
internal/cmd/eval.go     # `mocode eval` CLI 命令
```

关键设计落地点:
- ground truth 用 `message.Service.List(sessionID)` 的完整 trace(含 ToolCall/ToolResult/Reasoning/Finish),不只靠 `fantasy.AgentResult`
- Inference 复用现有 `agent.SessionAgent.Run`,通过 `coordinatorAgent` 适配器把 `agent.Coordinator` 适配为 `SessionAgent`
- 成本/用量直接读 `session.Session`(SessionID 已留接口)
- LLMJudge 通过 `Judger` 接口注入,`llmjudge.Judge` 复用 coordinator 同款 provider 构造路径的 `fantasy.LanguageModel`

用法:`mocode eval examples/evaluation/sample-evalset.json -n 3`(3 轮,带 Summary);`--json` 输出机器可读结果。

## 模型层弹性(已完成)

`internal/model/failover/` 已实现:包装 `fantasy.LanguageModel` 的主备故障转移,策略与 trpc-agent-go 一致(在第一个非错误 chunk 之前转移)。

- `failover.New(WithPrimary(m), WithFallback(m2...))` 构造,实现完整 `fantasy.LanguageModel` 接口(Generate/Stream/GenerateObject/StreamObject/Provider/Model)
- 非流式:逐候选尝试,首个成功即返回;全失败返回聚合错误(含已试候选数与最后错误)
- 流式:peek 首个 part,setup 错误或 error-part 触发转移;一旦产出非错误 part 即提交给该候选
- 5 个测试全绿:参数校验、Generate 失败转移、全失败、主成功不触碰备、stream error-part 转移

这是除 evaluation 外,trpc-agent-go 唯一值得借鉴的能力(hedge 尾延迟优化和 token tailoring 均不抄)。未在 app 装配层默认接入——需要时在构造 `agent.Model` 时把主模型用 `failover.New` 包一层即可。

### 已接入配置(可选启用)

`config.SelectedModel` 新增 `fallback` 字段(可选),coordinator 的 `buildAgentModels` 自动用 `failover.New` 包裹主模型。不声明则行为完全不变(向后兼容)。

配置示例(`mocode.json` 的 `models.large`):

```json
{
  "models": {
    "large": {
      "model": "gpt-4o",
      "provider": "openai",
      "fallback": {
        "model": "claude-3-5-sonnet",
        "provider": "anthropic"
      }
    }
  }
}
```

链路:主模型出错 → 转到 fallback → 都失败才返回聚合错误(含已试候选数)。JSON schema(`mocode-schema.json`)已用 `task schema` 重新生成并同步。

## bestofn 候选选择(internal/candidate,已完成)

把 evaluation 系统复用为在线候选选择器——这正是 trpc-agent-go 分析里识别的"杀手级组合":同一套评分管线既能离线评测,也能在线从 N 个候选回复里选最优。

- `candidate.Run(ctx, n, gen, sel)`:并发生成 N 个候选(并发执行,失败候选不中断批次),用 Selector 选出赢家
- `candidate.EvalSelector`:实现 `Selector`,用 `criterion.Criterion.Eval` 给每个候选的 trace 打分,选最高分(平分取最早,稳定);LLMJudge 可通过 Judger 注入
- 6 个测试全绿:选最高分/全失败/部分失败容忍/确定性并发证明/必需 criterion/case 期望转发

组合点:`EvalSelector` 直接消费 `internal/evaluation/criterion` 的 `Criterion.Eval`——评估系统的判分逻辑被候选选择零拷贝复用,无重复造轮子。
