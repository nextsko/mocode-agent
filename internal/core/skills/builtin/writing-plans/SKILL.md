---
name: writing-plans
description: >-
  Use when you have a spec or requirements for a multi-step task and need an
  implementation plan before touching code — deciding where the plan lives,
  mapping files, decomposing tasks into bite-sized TDD steps, and handing off
  to execution.
---

# Writing Plans：写实现计划

把 spec 变成"零上下文工程师也能照做"的实现计划：每个任务触碰哪些文件、写什么代码、怎么测、怎么提交。假设对方是熟练工程师，但完全不熟悉本代码库、工具链与领域。

**开工宣告**："I'm using the writing-plans skill to create the implementation plan."

**保存位置：先遵循仓库现有文档约定。** 本仓库及多数项目采用 `docs-rulebook` 的三区生命周期 `docs/in/`（活动）、`docs/plan/`（归档）、`docs/experiment/`（草稿）+ 主题目录 `<topic>/` + `plans/NN-*-plan.md` + `MASTER_PLAN.md`，完成即归档。**目标仓库若已有自己的文档约定，一律先遵循仓库现状**（本仓库自身即 `docs/plans/<topic>/README.md` + `docs/plans/README.md` 索引的一个实例）。放哪、怎么命名、何时归档以 `docs-rulebook` 为准；需要跨会话跟踪时配 `planning-with-files`。

## 范围检查

若 spec 覆盖多个相互独立的子系统，先拆成多个子项目 spec / 多个计划，每个计划独立产出可运行、可测试的软件。不要在一个计划里塞两个子系统。

## 文件结构（先于任务定义）

在拆任务前，先列出将创建/修改的文件及其单一职责：

- 边界清晰、接口明确；一个文件一个职责。
- 能同时装进上下文的代码才推理得准，文件越小越可靠。
- 一起改的文件放一起；按职责拆分，而不是按技术分层。
- 已有代码库遵循既有模式；不要擅自重构整个项目，但若你正要改的文件已臃肿，把拆分写进计划是合理的。

## 任务粒度与切分

- 任务是"自带测试循环、值得独立评审"的最小单元。
- 把搭建、配置、脚手架、文档步骤折叠进需要它们的那一个任务；只在"审阅者可以否决 A 而通过 B"的地方切分。
- 每个任务以**独立可验证的交付物**结束。
- 每一步是一个动作（2–5 分钟）：写失败测试 → 跑它确认失败 → 写最小实现 → 跑它确认通过 → 提交。

## 计划头部（每个计划必写）

```markdown
# [Feature Name] Implementation Plan

> **For agentic workers:** 逐任务执行本计划；步骤用 checkbox（`- [ ]`）跟踪。

**Goal:** [一句话说明要构建什么]
**Architecture:** [2–3 句说明做法]
**Tech Stack:** [关键技术/库]

## Global Constraints

[spec 中项目级要求：版本下限、依赖限制、命名/文案规则、平台要求。
逐条一行，数值原样抄自 spec。每个任务隐含包含本节。]
```

## 任务结构

````markdown
### Task N: [Component Name]

**Files:**
- Create: `exact/path/to/file.go`
- Modify: `exact/path/to/existing.go:123-145`
- Test: `exact/path/to/test.go`

**Interfaces:**
- Consumes: [本任务用到前序任务的精确签名]
- Produces: [后续任务依赖的精确函数名、参数与返回类型]

- [ ] **Step 1: 写失败测试**
```go
func TestSpecificBehavior(t *testing.T) { ... }
```
- [ ] **Step 2: 跑测试确认失败**
  Run: `go test ./path/... -run TestSpecificBehavior -v` / Expected: FAIL
- [ ] **Step 3: 写最小实现**
```go
func SpecificBehavior(...) { ... }
```
- [ ] **Step 4: 跑测试确认通过** / Expected: PASS
- [ ] **Step 5: 提交**
```bash
git add path/... && git commit -m "feat: add specific behavior"
```
````

## 禁止占位符

每一步都必须含工程师真正需要的内容。以下都是**计划失败**，绝不允许：

- "TBD" / "TODO" / "稍后实现" / "补充细节"
- "加上适当的错误处理" / "加校验" / "处理边界情况"
- "为上述写测试"（却不给测试代码）
- "同 Task N"（必须重复代码，读者可能跳序阅读）
- 只描述"做什么"却不展示"怎么做"（代码步骤必须给代码块）
- 引用任何任务都未定义的类型/函数/方法

## 自审（写完计划后自查）

1. **Spec 覆盖**：逐节核对 spec，每个需求都能指到任务；有缺口就补任务。
2. **占位符扫描**：按上面的清单搜一遍，发现即改。
3. **类型一致性**：后序任务用到的类型、方法签名、属性名与前面定义一致（`clearLayers()` 与 `clearFullLayers()` 是 bug）。

发现问题就地修，不必重新评审。

## 执行交接

计划保存后给出两个选项：

1. **Subagent-Driven（推荐）**：每个任务派一个全新子代理，任务间做两阶段评审（先 spec 合规、再代码质量），迭代快。
2. **Inline Execution**：在当前会话逐任务执行，按检查点批量推进。

选定后分别走子代理驱动或逐任务执行的流程；执行期用 `planning-with-files` 维护进度文件。

## 记住

- 永远给出精确文件路径。
- 每个改代码的步骤都写出完整代码。
- 命令与期望输出都写全。
- DRY、YAGNI、TDD、频繁提交。
- 计划位置与生命周期遵循 `docs-rulebook`，且**仓库现有约定优先**。
