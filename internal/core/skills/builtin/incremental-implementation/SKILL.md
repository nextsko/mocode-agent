---
name: incremental-implementation
description: >-
  Use when 要实现任何涉及多个文件的 feature 或变更、从任务拆解开始构建新功能、
  重构既有代码，或当你准备一次性写大量代码、任务大到无法一步落地时。以 thin
  vertical slice 推进：实现一小块、测试、验证、提交，再扩展，让每个增量都保持
  系统可工作、可测试。单一文件/单一函数且范围已最小的改动不适用本技能。
---

# 增量实现（Incremental Implementation）

## 概述

以 thin vertical slice 构建：实现一小块，测试，验证，再扩展。不要一次性实现整个 feature。每个增量都应让系统保持可工作、可测试。这是让大型 feature 可控的执行纪律。

## 何时使用

- 实现任何多文件变更
- 从任务拆解开始构建新 feature
- 重构既有代码
- 任何时候你想在测试前写超过 ~100 行代码

**不适用**：单文件、单函数且范围已经最小的变更。

## 增量循环

```
Implement ──→ Test ──→ Verify ──→ Commit ──→ Next slice
     ▲                                          │
     └──────────────────────────────────────────┘
```

每个 slice：

1. **Implement**：实现最小的完整功能块
2. **Test**：运行测试套件（无测试则先写一个）
3. **Verify**：确认 slice 按预期工作——测试通过、build 成功、必要时手动检查
4. **Commit**：用描述性 message 保存进度（原子提交见 `git-workflow-and-versioning`）
5. **Next slice**：向前推进，不要推倒重来

## 切片策略

### Vertical Slices（首选）

构建一条完整穿透各层的最小路径：

```
Slice 1: Create a task (DB + API + basic UI) → 用户能创建任务
Slice 2: List tasks (query + API + UI)       → 用户能看到任务
Slice 3: Edit a task (update + API + UI)
Slice 4: Delete a task (delete + API + UI + confirmation) → CRUD 完整
```

每个 slice 交付端到端可用的功能。

### Contract-First Slicing

前后端需要并行开发时：先定契约（types、interfaces、OpenAPI），再各自按契约实现，最后集成并做端到端测试。

### Risk-First Slicing

先啃风险最高、最不确定的部分（如先证明 WebSocket 连接可用），再做构建在其上的功能。Slice 1 失败时，你会在投入后续工作之前就发现。

## 实现规则

### Rule 0：Simplicity First

写代码前先问："能工作的最简单方案是什么？"写完后自检：

- 能用更少行数完成吗？
- 这些抽象配得上其复杂度吗？
- 资深工程师会不会说"你为什么不直接……"？
- 我是在为假想的未来需求，还是当前任务写代码？

```
SIMPLICITY CHECK:
✗ 为一个通知搭 Generic EventBus + middleware pipeline   ✓ 一次简单函数调用
✗ 为两个相似组件上 Abstract Factory                     ✓ 两个直接组件 + 共享工具
✗ 为三个表单做 config-driven form builder               ✓ 三个表单组件
```

三行相似的代码胜过过早的抽象。先实现朴素、明显正确的版本；正确性由测试证明后再优化。

### Rule 0.5：Scope Discipline

只碰任务要求的东西。**不要**：

- "顺手"清理改动附近的代码
- 重构你并未修改的文件里的 import
- 删除你并不完全理解的注释
- 因为"看起来有用"就加 spec 里没有的 feature
- 现代化你只是阅读的文件里的语法

发现范围外值得改进的点，记下来而不是动手：

发现范围外值得改进的点，记下来而不是动手，例如："src/utils/format.ts 有未使用的 import，auth middleware 的错误信息可以更好——需要我为这些建任务吗？"

### Rule 1：One Thing at a Time

每个增量只改一件逻辑上的事。不要混关注点。

- **坏**：一个 commit 同时加新组件、重构旧组件、改 build 配置。
- **好**：三个独立 commit，各改一件事。

### Rule 2：Keep It Compilable

每个增量后项目必须能 build，既有测试必须通过。不要在 slice 之间把代码库留在损坏状态。

### Rule 3：Feature Flags for Incomplete Features

功能没准备好但对用户可见时，用 feature flag 隔离，以便小步合并到主分支：

```typescript
const ENABLE_TASK_SHARING = process.env.FEATURE_TASK_SHARING === 'true';
if (ENABLE_TASK_SHARING) { /* 新的分享 UI */ }
```

### Rule 4：Safe Defaults

新代码默认采取安全、保守的行为（例如能力默认关闭、opt-in）。

### Rule 5：Rollback-Friendly

每个增量都应可独立 revert：

- 增量式变更（新文件、新函数）最容易回退
- 对既有代码的修改要小而聚焦
- DB migration 要有对应的 rollback migration
- 不要在同一 commit 里删一个东西又替换它——拆开

## 与 Agent 协作

指挥 agent 增量实现时，明确划出 scope 边界：

指挥 agent 时明确 scope，例如："实现计划里的 Task 3，先只做 DB schema 变更和 API endpoint，不要碰 UI；实现后跑 `npm test` 和 `npm run build` 确认没有破坏。"

每个增量都要说清楚什么在范围内、什么不在。

## 增量 Checklist

每个增量后确认：

- [ ] 变更只做一件事，并完整地做完
- [ ] 既有测试全部通过
- [ ] build 成功
- [ ] type checking 通过（如 `tsc --noEmit`）
- [ ] lint 通过
- [ ] 新功能按预期工作
- [ ] 已用描述性 message 提交

**注意**：只在可能影响它的改动之后重跑验证命令；一次成功运行后，代码没变就不要重复跑，那不会带来新信息。

## 常见合理化

| 合理化 | 现实 |
|--------|------|
| "最后一起测" | Bug 会复利：Slice 1 的 bug 会让 2–5 全错。每个 slice 都要测。 |
| "一次做完更快" | 直到出错——你无法在 500 行改动里定位是哪一行，就不快了。 |
| "这些改动太小，不值得单独提交" | 小 commit 是免费的；大 commit 藏 bug 且难以回退。 |
| "以后再加 feature flag" | 功能没完成就不该对用户可见，现在就加。 |
| "这个重构很小，顺手带上" | 重构与 feature 混在一起，评审和调试都更难。分开。 |
| "再跑一次 build 确认一下" | 代码没变时重复跑命令零信息；有后续编辑后再跑。 |

## 红旗

- 写了超过 100 行代码还没跑测试
- 单个增量里混入多个不相关变更
- "我顺便快速加上这个"式的 scope 扩张
- 为了赶速度跳过 test/verify 步骤
- 增量之间 build 或测试是坏的
- 大量未提交改动持续堆积
- 在第三个用例出现前就构建抽象
- "既然来了"就碰任务范围外的文件
- 为一次性操作新建 utility 文件
- 没有任何代码变动却连续重跑同一条 build/test 命令

## 完成后的验证

- [ ] 每个增量都单独测试并提交
- [ ] 全量测试套件通过
- [ ] build 干净
- [ ] feature 端到端按 spec 工作
- [ ] 没有未提交改动残留

## 相关技能

- `test-driven-development`：每个 slice 的测试先行、red-green 循环。
- `verification-before-completion`：提交或声明完成前先给证据。
- `systematic-debugging`：slice 之间出现失败时的根因调查。
- `code-review-and-quality`：slice 合并前的评审与质量门。
