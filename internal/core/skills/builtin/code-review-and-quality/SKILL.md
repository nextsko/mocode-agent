---
name: code-review-and-quality
description: >-
  Use when 需要评审代码变更、在合并前把质量关，或需要从 correctness、readability、
  architecture、security、performance 五个维度评估代码时。适用于 PR 评审、功能
  实现完成后、重构既有代码，以及任何 bug fix 之后（修复与回归测试一并评审）。
  即使代码由你自己、另一个 agent 或人类编写，也应执行同一套评审与批准标准。
---

# 代码评审与质量（Code Review & Quality）

## 概述

每次变更在合并前都必须评审，没有例外。评审覆盖五个维度：**correctness、readability、architecture、security、performance**。

**批准标准**：只要变更明确改善整体 code health，就批准——即使不完美。不要因为"不是你写的方式"而 block；只要它改善代码库并符合项目约定，就放行。

## 何时使用

- 合并任何 PR / change 之前
- 功能实现完成后
- 评审其他 agent 或模型产出的代码
- 重构既有代码
- 任何 bug fix 之后（同时评审修复与 regression test）

## 五轴评审

### 1. Correctness（正确性）

- 是否匹配 spec / 任务要求？
- 边界情况是否处理（null、empty、boundary）？
- error path 是否处理，而不是只走 happy path？
- 测试是否通过？测试是否真的在测对的东西？
- 有无 off-by-one、race condition、state 不一致？

### 2. Readability & Simplicity（可读性与简洁）

- 命名是否清晰、符合项目约定（拒绝无上下文的 `temp`、`data`、`result`）？
- 控制流是否直白（避免嵌套三元、深层回调）？
- 是否能用更少行数完成？（100 行能做的事写成 1000 行即为失败）
- 抽象是否配得上其复杂度？（第三次使用场景出现前不要泛化）
- 是否有 dead code 残留：no-op 变量、兼容 shim、`// removed` 注释？

### 3. Architecture（架构）

- 是沿用既有模式，还是引入新模式？新模式是否已论证？
- module 边界是否清晰？是否有应共享的重复代码？
- 依赖方向是否正确（无循环依赖）？
- 抽象层次是否合适（不过度设计、不过度耦合）？

### 4. Security（安全）

细节见 `security-and-hardening`。

- 用户输入是否在边界验证、清洗？
- secret 是否远离代码、日志、版本控制？
- 需要的 authentication / authorization 是否检查？
- SQL 是否参数化（禁止字符串拼接）？
- 输出是否编码以防 XSS？
- 外部数据（API、日志、用户内容、配置文件）是否按不可信处理，并在边界验证后再进入逻辑或渲染？

### 5. Performance（性能）

细节见下方性能检查项。

- 是否有 N+1 query？
- 是否有无界循环或无约束的数据拉取？
- 是否该异步的同步操作？
- list endpoint 是否缺 pagination？
- hot path 是否创建大对象？

## 变更规模

小而聚焦的变更更易评审、更快合并、更安全发布：

```
~100 行变更   → 好，一次评审可完成
~300 行变更   → 可接受（前提是单一逻辑变更）
~1000 行变更  → 太大，先拆分
```

**"一个变更"的定义**：单一自包含的修改，只解决一件事，包含相关测试，且提交后系统仍可用——是 feature 的一部分，而不是整个 feature。

拆分策略：

| 策略 | 做法 | 适用 |
|------|------|------|
| Stack | 先提交小变更，下一个基于它开始 | 顺序依赖 |
| By file group | 需要不同 reviewer 的文件分组提交 | 横切关注点 |
| Horizontal | 先建共享代码/stub，再接消费者 | 分层架构 |
| Vertical | 拆成更小的全栈 feature 切片 | feature 开发 |

**大变更可接受的情形**：完整文件删除、自动化重构——reviewer 只需验证意图而非每一行。

**重构与 feature work 分开提交**：一个变更同时重构旧代码并新增行为，就是两个变更。小清理（变量重命名）可由 reviewer 酌情并入。

## 变更描述

- **首行**：简短、祈使、自足（"Delete the FizzBuzz RPC"），让搜索历史的人不读 diff 也能理解。
- **正文**：改了什么、为什么；补充代码看不到的上下文/决策；链接 bug 号、benchmark、设计文档；必要时坦承方案不足。
- **反模式**："Fix bug"、"Fix build"、"Add patch"、"Moving code from A to B"、"Phase 1"。

## 评审流程

1. **理解上下文**：这个变更想达成什么？实现哪个 spec / 任务？预期行为变化是什么？
2. **先读测试**：测试揭示意图与覆盖。是否覆盖行为而非实现细节？边界是否覆盖？名字是否描述性？代码改动后测试能否抓住 regression？
3. **读实现**：逐个文件按五轴走查。
4. **分类发现**：给每条评论标注严重级（见下）。
5. **验证验证**：作者跑了哪些测试？build 是否通过？是否手动验证？UI 变更是否有截图？是否有 before/after 对比？

## 严重级标签

| 前缀 | 含义 | 作者动作 |
|------|------|----------|
| （无前缀） | Required change | 合并前必须处理 |
| **Critical:** | Blocks merge | 安全漏洞、数据丢失、功能损坏 |
| **Nit:** | Minor, optional | 可忽略——格式、风格偏好 |
| **Optional:** / **Consider:** | Suggestion | 值得考虑但非必需 |
| **FYI** | Informational only | 无需动作，仅提供上下文 |

标注严重级可避免作者把可选建议当成强制要求而浪费时间。

## 多模型评审

Model A 写代码 → Model B 评审 correctness 与 architecture → A 处理反馈 → Human 最终裁决。不同模型盲区不同，交叉评审能抓到单模型遗漏的问题。

## Dead Code 清理

重构或实现后检查孤立代码：列出已不可达/未使用的内容，**删除前先问**："这些已不再使用的元素可以移除吗：[列表]？" 不要留下 dead code 让后来者困惑，但不确定的也不要默默删除。

## 诚实与分歧

- **不要 rubber-stamp**：没有评审证据的 "LGTM" 毫无价值。
- **不要软化真问题**：明明是会上生产的事故，却写成 "might be a minor concern" 是不诚实。
- **能量化就量化**："这个 N+1 query 会让列表每项多 ~50ms" 优于 "这可能有点慢"。
- **对明显有问题的方案顶回去**：sycophancy 是评审的失败模式；直接指出并给替代方案。
- **接受合理驳回**：作者掌握完整上下文且不同意时，尊重其判断。评论代码，不评论人。
- **拒绝 "I'll clean it up later"**：推迟的清理通常不会发生；除非真正紧急，要求提交前清理。

分歧裁决优先级：技术事实与数据 > style guide > 软件设计原则 > 代码库一致性。

## 依赖纪律

新增依赖前依次确认：现有技术栈能否解决（通常可以）→ 依赖体积 → 是否仍维护 → 有无已知漏洞（`npm audit`）→ license 兼容性。**规则**：优先标准库与既有工具；每个依赖都是负债。

## 评审清单

```markdown
## Review: [PR/Change title]
### Context
- [ ] 我理解这个变更做什么、为什么
### Correctness
- [ ] 匹配 spec；边界与 error path 已处理；测试充分覆盖
### Readability
- [ ] 命名清晰一致；逻辑直白；无多余复杂度
### Architecture
- [ ] 沿用既有模式；无不必要耦合；抽象层次合适
### Security
- [ ] 无 secret 入库；边界验证输入；无注入；auth 到位；外部数据按不可信处理
### Performance
- [ ] 无 N+1；无无界操作；list endpoint 有 pagination
### Verification
- [ ] 测试通过；build 成功；必要时已手动验证
### Verdict
- [ ] Approve — 可合并
- [ ] Request changes — 问题必须处理
```

## 常见合理化与红旗

| 合理化 | 现实 |
|--------|------|
| "能跑就行" | 不可读、不安全、架构错误的代码会复利式累积债务。 |
| "我写的，我知道它对" | 作者对自己的假设是盲的，任何变更都受益于另一双眼睛。 |
| "以后再清理" | 以后不会来。评审就是质量门，提交前清理。 |
| "AI 生成的代码大概没问题" | AI 代码需要更多而非更少审查：它自信且看似合理，即使错了。 |
| "测试过了，所以没问题" | 测试必要但不充分，抓不住架构、安全、可读性问题。 |

红旗：未经评审就合并；只检查测试是否通过；"LGTM" 无评审证据；安全敏感变更未做安全评审；大到"没法好好评审"的 PR；bug fix 不带 regression test；评论不带严重级；接受"以后再修"。

## 评审完成后的验证

- [ ] 所有 Critical 问题已解决
- [ ] 所有 Important 问题已解决，或带理由显式延期
- [ ] 测试通过、build 成功
- [ ] verification story 有记录（改了什么、如何验证）

## 相关技能

- `receiving-code-review`：收到评审反馈后如何技术性核验、何时顶回。
- `verification-before-completion`：完成声明前必须提供新鲜证据。
- `systematic-debugging`：bug / 测试失败的根因调查。
- `test-driven-development`：为修复与回归写失败测试。
- `security-and-hardening`：安全加固；性能审查见本文件的性能轴。
