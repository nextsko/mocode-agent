---
name: requesting-code-review
description: >-
  Use when 完成任务、实现主要功能或合并到 main 之前，需要派一个独立的代码评审子 Agent 核验工作是否
  满足需求时。评审者只拿到精心构造的上下文（描述、需求、BASE/HEAD SHA），绝不继承当前会话历史。
  核心原则：早评审、勤评审。
---

# 发起代码评审（Requesting Code Review）

## 概述

派一个代码评审子 Agent，在问题级联放大之前抓住它们。评审者拿到的是**精心构造的上下文**——绝不带上当前会话的历史。这样评审者聚焦于工作产物而非你的思考过程，同时保留你自己的上下文以继续工作。

核心原则：**早评审、勤评审**。

本技能负责「怎么发起评审」；评审维度与批准标准见 `code-review-and-quality`，收到反馈后如何处理见 `receiving-code-review`。若评审暴露的是原因不明的故障，转 `investigate-first` / `systematic-debugging`。

## 何时发起评审

**强制：**

- subagent-driven development 中每个任务完成后
- 完成主要 feature 后
- 合并到 main 之前

**可选但有价值：**

- 卡住时（换个新视角）
- 重构之前（建立基线）
- 修完复杂 bug 之后

## 如何发起

**1. 获取 git SHA：**

```bash
BASE_SHA=$(git rev-parse HEAD~1)   # 或 origin/main
HEAD_SHA=$(git rev-parse HEAD)
```

**2. 派 `general-purpose` 子 Agent**，填入下方评审模板。

**占位符：**

- `{DESCRIPTION}` — 你构建了什么，简短总结
- `{PLAN_OR_REQUIREMENTS}` — 它应该做什么（计划路径 / 任务文本 / 需求）
- `{BASE_SHA}` — 起始提交
- `{HEAD_SHA}` — 结束提交

**3. 处理反馈：**

- Critical 问题立即修
- Important 问题在继续之前修
- Minor 问题记下待后续
- 评审者有错时，带技术理由顶回去

## 评审者 prompt 模板（精简）

```
你是一位资深代码评审者，具备软件架构、设计模式与最佳实践专长。你的职责是
对照计划/需求评审已完成的工作，在问题级联前识别它们。

## 实现了什么
{DESCRIPTION}

## 需求 / 计划
{PLAN_OR_REQUIREMENTS}

## 待评审的 Git 范围
Base: {BASE_SHA}   Head: {HEAD_SHA}
    git diff --stat {BASE_SHA}..{HEAD_SHA}
    git diff {BASE_SHA}..{HEAD_SHA}

## 只读评审
评审对当前 checkout 只读：不得改动工作树、index、HEAD 或分支状态。用
`git show` / `git diff` / `git log` 察看历史。若需要其它版本的可用副本，用
`git worktree add /tmp/review-<SHA> <SHA>` 检出到独立临时目录，绝不移动本 checkout 的 HEAD。

## 检查什么
- Plan alignment：实现是否匹配计划/需求？偏离是合理改进还是问题？
- Code quality：关注点分离、错误处理、类型安全、DRY（不过早抽象）、边界情况。
- Architecture：设计是否合理、可扩展性/性能、安全、与周边代码集成。
- Testing：测试验证真实行为而非 mock？边界覆盖？必要处有集成测试？全部通过？
- Production readiness：schema 变更的迁移策略、向后兼容、文档、明显 bug。

## 校准
按实际严重级分类，不是什么都 Critical。列问题前先如实肯定做得好的地方。
发现重大偏离要具体标出；若问题出在计划本身而非实现，也直说。

## 输出格式
### Strengths
[具体好在哪]
### Issues
#### Critical (Must Fix)   [bug、安全、数据丢失、功能损坏]
#### Important (Should Fix) [架构问题、缺功能、错误处理差、测试缺口]
#### Minor (Nice to Have)   [风格、优化机会、文档润色]
每条问题给：File:line、错在哪、为什么重要、怎么修（不明显时）。
### Recommendations
### Assessment
Ready to merge? [Yes | No | With fixes]
Reasoning: [1-2 句技术判断]

## 铁律
DO：按实际严重级分类；具体到 file:line；解释 WHY；肯定优点；给出明确结论。
DON'T：没检查就说「looks good」；把 nitpick 标成 Critical；评论没读过的代码；
      模糊反馈（「改进错误处理」）；回避明确结论。
```

评审者返回：Strengths、Issues（Critical / Important / Minor）、Recommendations、Assessment。

## 与工作流的集成

- **Subagent-Driven Development**：每个任务后评审，在问题复利前抓住，修完再进下一任务。
- **Executing Plans**：每个任务或自然检查点后评审，拿到反馈、应用、继续。
- **Ad-Hoc 开发**：合并前评审；卡住时评审。

## 红旗

**Never：**

- 因为「很简单」而跳过评审
- 无视 Critical 问题
- 带着未修的 Important 问题继续
- 与有效的技术反馈争论

**评审者有错时：**

- 带技术理由顶回去
- 用代码/测试证明它能工作
- 请求澄清
