---
name: subagent-driven-development
description: >-
  Use when executing an implementation plan whose tasks are mostly independent,
  in the current session, by dispatching a fresh implementer subagent per task
  with a spec + quality review gate after each and a broad whole-branch review
  at the end. Not for work that needs a parallel session (use executing-plans),
  tightly coupled tasks with no clean interfaces, or when the user wants to
  approve each step.
---

# Subagent-Driven Development：同会话逐任务派发子代理

## 核心模型

你是**控制器（controller）**：持有计划、跨任务接口与验收标准，**不亲自做实现**。

每个任务派一个**全新**的 implementer 子代理（上下文隔离），任务完成后立刻派 task reviewer 同时给出两个判决 —— **spec 合规**与**代码质量**；全部任务结束后再做一次**全分支终审**。

**核心公式：每任务新子代理 + 任务评审（spec + quality）+ 全分支终审 = 高质量、快迭代。**

为什么用子代理：子代理**不继承**你的会话历史，由你精确构造它所需的全部上下文；它专注于单一任务，也为你保留协调所需的上下文。

## 何时用 / 何时不用

**用**：已有实施计划；任务大体独立；留在当前会话执行。

**不用**：

- 需要并行会话执行 → 用 `executing-plans`。
- 任务紧耦合、接口不清晰 → 先拆 spec 或内联做。
- 用户要逐步确认、或全权推进的分阶段目标 → 内联做或 `multi-agent-orchestration`。

**与 `executing-plans` 的区别**：同会话（无上下文切换）、每任务新子代理（无上下文污染）、每任务评审 + 终审、任务间无人类介入（更快迭代）。

## 流程

1. **读计划一次**：记下上下文、Global Constraints 与全局约束，为所有任务建 todo。
2. **Pre-flight 计划审查**：派 Task 1 前扫一遍计划，找（a）任务之间或与 Global Constraints 矛盾之处；（b）计划明确要求、但评审规则视为缺陷的东西（无断言测试、逻辑块逐字重复）。**一次性批量**把发现连同对应计划原文交给用户裁决，不要发现一个打断一次；扫描干净则直接开工。
3. **逐任务循环**（见下）。
4. 全部完成后派**全分支终审**，走收尾流程。

### 逐任务循环

1. **派 implementer**：给它任务简报（task brief）+ 报告文件路径 + 一句话场景定位 + 它无法从简报得知的接口/决策 + 你对歧义的裁决。**不在派发词里粘贴历史任务摘要**。
2. **处理 implementer 状态**。
3. **生成 diff 文件**：记录派发前的 BASE 提交，用 review package 生成 diff 文件（`git log --oneline` + `git diff --stat` + `git diff -U10 BASE..HEAD` 重定向到一个唯一命名文件），把打印出的路径交给 reviewer。**BASE 绝不能用 `HEAD~1`**，否则会悄悄丢掉多提交任务里除最后一次外的所有提交。
4. **派 task reviewer**：给它三样路径（简报、报告、diff 文件）+ 绑定本任务的 global constraints（逐字复制）。
5. **有 Critical/Important → 派修复子代理 → 重新评审**，循环到批准；Minor 记入进度账本，交给终审统一分诊。
6. **标记完成**：更新 todo 与进度账本（含提交范围）。

## 处理 implementer 状态

| 状态 | 动作 |
|------|------|
| `DONE` | 生成 diff，派 task reviewer。 |
| `DONE_WITH_CONCERNS` | 先读疑虑。涉及正确性或范围 → 处理后再评审；仅观察（如"这文件在变大"）→ 记录后继续。 |
| `NEEDS_CONTEXT` | 补齐缺失上下文后重派。 |
| `BLOCKED` | 判断：上下文不足 → 补；推理不足 → 换更强模型；任务太大 → 拆；计划本身错 → 升级给人类。 |

**绝不**忽略升级，也**绝不**不改变任何东西就逼同一模型重试 —— implementer 说卡住了，就一定有东西要改。

reviewer 报出的 **⚠️ 无法从 diff 验证**项（需求落在未改动代码或跨任务）不阻塞其余评审，但你必须亲自逐条解决后才能标任务完成 —— 你持有 reviewer 没有的计划与跨任务上下文。若确认是真缺口，按 spec 评审失败处理，退回 implementer 并重评。

## 模型选择

用能胜任该角色的**最弱**模型以省成本、提速度：

| 任务性质 | 模型档位 |
|----------|----------|
| 机械实现（1-2 文件、spec 完整） | 最快最便宜 |
| 集成/判断（多文件协调、模式匹配、调试） | 标准 |
| 架构/设计、**全分支终审** | 最强 |
| 评审 | 按 diff 的规模、复杂度、风险同比缩放 |

- **派发时永远显式指定模型**：省略会继承你的会话模型（常是最强最贵），悄然抵消本节效果。
- **轮数比 token 单价更要紧**：最便宜的模型常在多步任务上多花 2-3 倍轮数，总体更贵。评审者与"凭散文描述实现"的 implementer 以中档模型为下限；计划文本已含完整要写代码时，实现只是誊写 + 测试 → 用最便宜档。

## 构造 reviewer 派发词

Per-task 评审是**任务级门禁**，全分支广度评审只在最后做一次。填模板时：

- 不加无谓的开放式指令（"检查所有用法""有用的话跑 race 测试"）—— 除非有具体的、任务相关的理由。
- 不让 reviewer 重跑 implementer 已在同一份代码上跑过的测试；implementer 的报告就是测试证据。
- **不预判发现**：绝不指示 reviewer 忽略或不要标记某个问题。派发词里出现"不要标记""别把 X 当缺陷""最多 Minor""计划选择了"—— 停，你在预判，通常是为了省掉一轮评审。计划的示例代码是起点，不是其弱点获选的证据。
- global-constraints 块是 reviewer 的注意力透镜：从计划的 Global Constraints 或 spec **逐字**复制绑定要求（精确值、精确格式、组件间声明的关系）。模板已含流程规则（YAGNI、测试卫生、评审方法），该块只放本项目 spec 的要求。
- **计划强制的发现**（或任何与计划文本冲突的发现）是人类的决定：并列展示发现与计划原文，问哪个为准。不要因计划强制就驳回，也不要未经询问就派修复。
- 每个**修复派发**都带 implementer 契约：修复代理须重跑覆盖其改动的测试并报告结果；派发词点名覆盖测试文件（一行修复不需要整套）。
- 终审若返回发现，**派一个**修复子代理带上**完整发现列表** —— 不要一个发现派一个（各自重建上下文并重跑套件，成本极高）。

## 文件交接

粘进派发词的一切、以及子代理打印回来的一切，都会滞留并反复被你的上下文重读。**用文件交接产物**：

- **任务简报**：从计划提取任务全文到唯一命名文件；派发词只含（1）任务在项目中的位置一句话；（2）简报路径，注明"先读它 —— 这是你的需求，精确值逐字使用"；（3）简报无法得知的、来自前序任务的接口与决策；（4）你对简报歧义的裁决；（5）报告文件路径与报告契约。精确值只出现在简报里。
- **报告文件**：报告命名与简报配套（`task-N-brief.md` → `task-N-report.md`）。implementer 把完整报告写进去，只回传状态、提交、一行测试摘要、疑虑。
- **评审输入**：task reviewer 拿到三个路径（简报、报告、diff 文件）+ 绑定约束。
- **修复**：修复报告附到同一报告文件；重评读更新后的文件。

## 进度持久化

会话记忆**熬不过压缩**。真实事故中，控制器丢失进度后重派了整批已完成任务 —— 这是观察到的最贵失败。

- 开工先查账本（如 `.superpowers/sdd/progress.md`）；已标完成的任务**不重派**，从第一个未完成处续跑。
- 任务评审清干净后，立刻追加一行：`Task N: complete (commits <base7>..<head7>, review clean)`。
- 账本是恢复地图：它记的提交在 git 里真实存在，即使你的上下文已不记得。压缩后**信任账本与 `git log`，不信任记忆**。

## 红旗（Never）

- 未经用户明确同意就在 main/master 上开始实现。
- 跳过 task review，或接受缺少任一判决（spec 合规 **和** 质量都必须有）的报告。
- 带着未修问题进入下一任务（评审有 Critical/Important = 未完成）。
- **并行**派多个实现子代理（会冲突）—— 需要并行调查独立故障用 `dispatching-parallel-agents`。
- 让子代理读整个计划文件（应给它任务简报）。
- 省略场景定位；或忽略子代理提问（先答完再让它动手）。
- 在 spec 合规上接受"差不多"；跳过评审循环（发现问题 = 修复 = 重评）。
- 用 implementer 自审替代真正的 review（两者都要）。
- 不生成 diff 文件就差派 task reviewer。
- 重派账本已标完成的任务。

## 集成

- **`code-review-and-quality`**：评审维度、严重级与批准标准；全分支终审复用其清单。
- **`dispatching-parallel-agents`**：并行派发机制；本技能刻意**串行**实现任务。
- **`multi-agent-orchestration`**：全权、分阶段、命名专家角色的替代流程。
- **`test-driven-development`**：implementer 按 TDD 逐任务实现。
- **`writing-plans`** / **`executing-plans`** / **`finishing-a-development-branch`**：计划产出、并行会话执行、分支收尾。
- **`docs-rulebook`**：账本与简报等产物的目录与生命周期规范。
