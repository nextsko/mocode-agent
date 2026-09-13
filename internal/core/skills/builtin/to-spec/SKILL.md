---
name: to-spec
description: >-
  Use when you need to turn the current conversation and codebase understanding
  into a written spec without interviewing the user — synthesizing the problem,
  solution, user stories, implementation and testing decisions, and out-of-scope
  notes into a publishable document.
---

# To Spec：把对话沉淀为 spec

把**当前对话上下文 + 代码库理解**合成为 spec。**不要再访谈用户**，只综合你已知的信息。

这是 brainstorming / spec-driven-development 之后的"落文档"步骤：设计已经谈过，现在只需沉淀。产物落位遵循 `docs-rulebook`（或仓库现有文档约定）。若仓库使用 issue tracker，则发布为 issue 并打 `ready-for-agent` 标签；否则写入主题目录的 `specs/`。

## 流程

1. **补齐代码库理解**：若还没做，先探索仓库现状。全程使用项目领域词汇（glossary），并遵守触及区域的 ADR。

2. **勾画测试接缝（seams）**：确定将在哪些接缝上测试该功能。
   - 优先复用已有接缝，而非新增。
   - 尽量选**最高层**的接缝；确需新增时，在能成立的最高点提出。
   - 全代码库接缝越少越好，理想数量是 1。
   - 与用户确认这些接缝符合其预期。

3. **写 spec 并发布**：按下述模板成文，然后：发布到 issue tracker 并打 `ready-for-agent` 标签；或（无 tracker 时）写入 `docs-rulebook` 约定的主题目录 `specs/NN-<topic>-spec.md`。

## Spec 模板

```markdown
## Problem Statement
从用户视角描述其面临的问题。

## Solution
从用户视角描述解决方案。

## User Stories
一长串编号的用户故事，每条格式为：
1. As an <actor>, I want a <feature>, so that <benefit>

例：As a mobile bank customer, I want to see balance on my accounts,
so that I can make better informed decisions about my spending.

应极其详尽，覆盖该功能的各个方面。

## Implementation Decisions
已做出的实现决策，可含：
- 将构建/修改的模块
- 将被修改的模块接口
- 开发者的技术澄清
- 架构决策
- Schema 变更
- API 契约
- 具体交互

**不要**写具体文件路径或代码片段（很快会过时）。
例外：若原型产出的片段比文字更精确地编码了某个决策（状态机、reducer、schema、类型形状），
可就地内联到相关决策中，并简要注明来自原型；只保留决策富集部分，不要可运行 demo。

## Testing Decisions
已做出的测试决策，含：
- 何为好测试的说明（只测外部行为，不测实现细节）
- 哪些模块会被测试
- 测试的先前范例（代码库中类似测试）

## Out of Scope
明确不在本 spec 范围内的事项。

## Further Notes
其他补充说明。
```

## 纪律

- 不访谈、不追加提问；只综合已有信息（需要澄清时说明缺什么，而不是开始访谈）。
- Implementation Decisions 保持"无路径、无代码"的抽象层级，prototype 例外除外。
- 写完后的下一步是 `writing-plans` 生成实现计划，而不是直接编码。
