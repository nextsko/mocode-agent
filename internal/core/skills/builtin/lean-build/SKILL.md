---
name: lean-build
description: >-
  Use when 实现新行为、产品切片或系统集成，且存在明显的过度构建（overbuilding）风险时：
  需求边界模糊、容易顺手加 mode/provider/config/扩展点/打磨，或需要把 feature 落成
  横跨各责任层的最小完整端到端路径。以 repository 复用、严格 scope 与显式 acceptance
  为纪律，acceptance 通过即停。适用于 feature 开发、slice 落地、与既有系统集成。
---

# Lean Build（精简构建）

## 概述

Native Core 的 **architecture-first simplicity** 是强制前提。把 feature 转成**完整但窄**的 outcome，并让它与系统架构契合。

这个 skill 的产出不是「最小的文件 diff」，而是**横跨各责任层的最小完整端到端路径**。

核心一句话：**只构建 acceptance 需要的东西，复用最合适的 seam，在 acceptance 通过时停止。**

## 何时使用

- 实现新行为、产品切片（product slice）、与既有系统的集成
- 过度构建风险高：容易顺手加模式、provider、配置、扩展点、打磨
- 需要从 request + repository 同时推导可观察 acceptance 与明确 non-goals

**不适用**：范围明确的 bug 修复（见 `surgical-patch`）；纯验证任务（见 `verify-and-stop`）。

## 铁律

```
DONE = END-TO-END PATH MEETS OBSERVABLE ACCEPTANCE
不把工作硬塞进单文件 / 单层直接表达式 / local patch
不在 acceptance 之外增加 surface、依赖、配置、打磨
acceptance 通过即停，不做 scope 外收尾
```

## 工作流程

### 1. 推导 acceptance 与 non-goals

从 request **和** repository 同时出发，写下来：

- **可观察 acceptance（observable acceptance）**：什么外部可观察的行为 / 输出 / 副作用证明完成？必须可测试、可复现。
- **显式 non-goals**：本次明确不做什么。写下来，作为 scope 防线。
- 两者冲突或含糊时先澄清，不要用「顺手实现」填补空白。

### 2. 从入口追踪到责任层

- 从 entry point 出发，穿过真正拥有 invariants 的层。
- 识别哪个 module / adapter 拥有该行为的不变量（用 `codebase-design` 的 module / interface / seam 词汇）。
- **不要**把工作硬塞进一个文件、一层直接表达式或一个 local patch——那会破坏 ownership 与 locality。

### 3. 交付完整窄路径

- 在负责任的各层上交付**一致的端到端路径**，而不是一个局部补丁。
- **复用最合适的 seam**；优先让行为落在它本就该归属的 module。
- 当打补丁会重复行为、削弱 ownership 或掩盖 root cause 时，选择重构，并按 `safe-refactor` 的边界纪律执行。

### 4. 克制构建范围

除非 acceptance 需要，否则**省略**以下内容：

- 额外的 mode / provider / 可配置项
- 扩展点（extension points）与插件机制
- 打磨（polish）、美化、非必需的 UX 细节
- 未被验收要求的抽象层

新增 **surface、dependency、service、config 或 migration** 只在以下情形才允许：

- 生命周期设计（lifecycle design）需要，或
- acceptance 明确需要。

一旦新增，必须说明其**实质性 tradeoff**，不要默默引入。

### 5. 保持可运行与安全

- 每一步都保持 work runnable：代码可 build、测试可跑、系统不处于损坏状态。
- 保留 Native Core 的安全保证（不为了走捷径削弱既有 invariant / 安全边界）。
- 增量落地方式参照 `incremental-implementation`。

## 停止条件

- **Exercise the path**：实际走一遍端到端路径，而不是只看代码。
- **Run focused proof**：跑能证明 acceptance 的聚焦验证（测试 / 命令）。
- **Stop when acceptance passes**：一旦 acceptance 通过，立即停止。
- 汇报只包含：**material omissions（实质性缺口）** 与 **trigger（触发本次构建的条件）**，不堆砌过程叙述。

## Checklist

- [ ] acceptance 可观察、可测试；non-goals 已显式写下
- [ ] 已从 entry point 追踪到拥有 invariant 的责任层
- [ ] 交付的是跨层端到端路径，而非单文件/单层补丁
- [ ] 复用了合适的 seam；重构只在打补丁会损伤 ownership 时发生
- [ ] 未引入 acceptance 之外的 mode/provider/config/扩展点/打磨
- [ ] 新增 surface/dependency/config/migration 均有生命周期或 acceptance 依据，并已说明 tradeoff
- [ ] 路径实际走通，focused proof 通过
- [ ] 工作始终 runnable，Core safety 未被削弱

## 常见合理化

| 合理化 | 现实 |
|--------|------|
| 「顺手加个 config 更灵活」 | acceptance 不需要的灵活性就是过度构建，是负债。 |
| 「一个文件就能改完，快」 | 把工作塞进不拥有 invariant 的层，破坏 ownership，制造隐性耦合。 |
| 「先加上，以后可能用」 | YAGNI。生命周期设计与 acceptance 才是新增的唯一理由。 |
| 「顺便把这块清理了」 | 清理与交付混在一起，评审与验证都失效。 |
| 「acceptance 差不多满足了」 | 「差不多」不是可观察的通过；跑完整 proof 再声明（见 `verification-before-completion`）。 |

红旗：未写 non-goals 就开写；为「将来」加扩展点；跨层路径被压成单文件补丁；引入依赖却不说明 tradeoff；acceptance 未走通就宣布完成。

## 相关技能

- `surgical-patch`：范围明确的 bug / 小行为改动的窄层修复。
- `incremental-implementation`：把这条端到端路径切成可工作、可测试的 slice。
- `codebase-design`：判断 seam 位置与模块 ownership 的词汇与原则。
- `safe-refactor`：当 lean build 决定重构时，用验证夹住结构编辑。
- `test-driven-development`：为 acceptance 写失败测试并驱动实现。
- `verification-before-completion`：声明 acceptance 通过前先给新鲜证据。
- `code-review-and-quality`：合并前按五轴评审。
- `docs-rulebook`：若涉及计划/文档产出，遵循目录与生命周期纪律。
