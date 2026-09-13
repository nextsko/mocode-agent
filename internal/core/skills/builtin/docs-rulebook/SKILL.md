---
name: docs-rulebook
description: >-
  Use when the user asks "where should docs/plans live", "how to write a
  plan", "how to organize docs", "how to archive docs", "where does
  MASTER_PLAN go", or when creating/moving/archiving documents or designing
  a docs directory structure in any project. Not tied to a language or
  editor. If the target repository already has its own docs convention,
  follow the repository first.
---

# docs-rulebook：通用项目文档与计划目录规范

组织文档、写计划、归档文档时按本规范执行。核心一句话：**活跃的进活动区，完成的进归档区，临时笔记进草稿区，三区不混。**

本规范提炼自 andy-code 的 `docs/` 实践，作为通用默认方案。**目标仓库若已有自己的文档约定，先遵循仓库现状**，只在新建或重构时引入本方案。

## 生命周期三区（默认命名）

| 区 | 默认目录 | 定位 | 纪律 |
|----|----------|------|------|
| 活动区 | `docs/in/` | 活跃计划 + 活跃支撑笔记 | 只放进行中的东西 |
| 归档区 | `docs/plan/`、`docs/archive/` | 已完成/历史计划 | 只作历史参考；任务未明确要求不得照它执行 |
| 草稿区 | `docs/experiment/`、`docs/dev/` | 临时/探索笔记 | 简短、实用、可随时丢弃，别写长篇 |

## 新建主题目录：`<docs-root>/<topic>/`（默认放活动区）

- topic 用短 kebab-case；版本化主题加 `-v1` 后缀（如 `request-context-bloat-v1`）；纯审计/调研类可不带版本号（如 `context-system-audit`）。
- 主题根**必放 `README.md`**。
- 支撑笔记放主题根下：`NN-标题.md` 编号，标题用团队惯用语言。**一个文档一个主题**，范围跑偏就拆分；记录结论/约束/阻塞/下一步，不写长叙事。
- 多阶段大主题在主题根放 `MASTER_PLAN.md`，含量化验收目标与实施进度。
- 辅助产物按类型分目录：`artifacts/`（按日期归档证据，如 `2026-08-05/`）、`specs/`、`interfaces/`、`logs/`、`scripts/`。

## 计划文件

- 位置：`<docs-root>/<topic>/plans/NN-short-purpose-plan.md`
- 命名：两位数序号 + 短连字符用途 + `-plan` 后缀；插入序号可用 `01b` 形式。
- 计划文件头部固定写元信息块：**状态、创建日期、范围/目标、依赖（前置计划）、证据（数据/报告引用）、触达模块**。

```
<docs-root>/<topic>/
├─ README.md
├─ MASTER_PLAN.md
├─ 01-现状与目标.md                 # 支撑笔记
├─ plans/
│  ├─ 01-authorization-bound-plan.md
│  ├─ 01b-authorization-cache-plan.md
│  └─ 02-migration-steps-plan.md
├─ specs/
├─ artifacts/2026-08-05/
└─ logs/
```

## 完成即归档

- 计划完成后**从活动区移入归档区**（`docs/plan/` 或等价物），或进版本忽略的存储。
- **禁止**把 finished plans 和 active docs 混放在活动区。

## 变更纪律

- 影响架构/边界的变更，**同一个变更里**同步更新相关活跃计划文档 + 对应测试/护栏，缺一不可。
- **Truth 顺序（别靠记忆）**：项目结构清单（`go.mod` / `Cargo.toml` / `package.json` 等）→ 活动区计划 → 测试与护栏 → 公开 API/文档。README 与代码冲突时以这几处为准。

## 随团队约定的部分

- **文档语言**：跟随团队惯用（本工作区中文或中英双语；代码注释一般为英文）。
- **目录命名**：本规范是默认值；**仓库已有惯例就沿用仓库的**，保持一致比强行统一更重要。

## 与 mocode 的衔接

- mocode 的 **plan 模式**（`/plan`）遵循本规范组织计划与文档；本仓库自身沿用的是
  `docs/plans/<topic>/README.md` + `docs/plans/README.md` 索引（即「仓库已有惯例优先」的
  一个实例），而非默认的 `docs/in/`。
- 具体计划产物的模板与元信息块见 plan 模式提示词；本 skill 负责**目录与生命周期纪律**。
