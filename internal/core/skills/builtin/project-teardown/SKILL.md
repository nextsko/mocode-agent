---
name: project-teardown
description: >-
  Use when 用户要求「拆解 / 分析 / 搞懂 / 学习 / 深入理解 / teardown」一个代码项目，需要产出多份带
  meta YAML 检索头的结构化 Markdown 文档时。覆盖技术架构、设计理念、核心亮点、同类对比、可复用资产、
  变更历史、issue 与依赖追踪。只读项目源码，产物统一落入 docs/。
---

# 项目拆解（Project Teardown）

## 概述

对一个代码项目做全方位拆解，产出多份带 meta YAML 检索头的结构化 Markdown 文档。面向学习、审核、技术选型评估、二次开发准备等「深入理解一个项目」的场景。

核心纪律：**只读不写**——不修改项目源码，所有产物落在项目的 `docs/` 目录。

## 何时使用

- 用户说「拆解 / 分析 / 搞懂 / 深入理解 / teardown / 全方位分析」一个项目。
- 需要为陌生项目建立可检索、可交叉引用的文档资产。

文档目录与生命周期纪律参照 `docs-rulebook`；调查阶段「证据不足不下手」的纪律参照 `investigate-first`。

## 工作流

### 第 1 步：快速侦察（Reconnaissance）

先做轻量快照，判断项目性质，不做深度分析：

- `ls -la` 项目根，看结构雏形。
- 是否 git 仓库：`git rev-parse --is-inside-work-tree`；若不是，记录「本地快照非 git 仓库」。
- 读核心文档：`README.md`、`README_zh.md`、`CONTRIBUTING.md`。
- 读构建/配置文件：`go.mod` / `package.json` / `Cargo.toml` / `pyproject.toml` / `pom.xml`，记录技术栈、模块名、版本要求。
- 读入口文件：`main.go` / `main.py` / `index.ts` 等。
- 读 `Dockerfile` / `docker-compose.yml`（如有）；有 `.github/` 则记录 CI/CD。
- 快速浏览顶级目录：`cmd/`、`internal/`、`src/`、`lib/`、`web/`。

产出：项目性质判断 + 技术栈摘要 + 关键文件列表。

### 第 2 步：规划文档体系（Document Planning）

按第 1 步的规模与性质选择要产出的文档。命名用小写 `NN-xxx.md`，顶部带 `---` YAML meta：

| 文档 | 内容 | 何时包含 |
|------|------|----------|
| `00-project-overview.md` | 项目定位、事实速查表、能力地图 | 总是 |
| `01-tech-architecture.md` | 后端分层、路由、数据流、DB、部署 | 代码项目 |
| `02-design-philosophy.md` | 设计理念、取舍、权衡、限制 | 总是 |
| `03-core-highlights.md` | 核心亮点逐条深挖（含代码位置） | 总是 |
| `04-comparison.md` | 与同类产品对比 + 选型建议 | 有竞品时 |
| `05-reusable-assets.md` | 可复用架构/设计/代码/思想 | 总是 |
| `06-changelog-milestones.md` | 变更历史、版本、贡献节奏 | 有上游 repo 或 git 历史 |
| `07-issues-tracking.md` | issue 追踪（开放/已关闭按主题归类） | 有上游 repo |
| `08-dependency-tracking.md` | 依赖追踪（版本/最新/用途/供应链风险） | 有依赖管理文件 |
| `09-index.md` | 总索引 + 检索入口 | 总是 |

每篇顶部 YAML 固定字段：

```yaml
---
title: "..."
slug: "NN-xxx"
category: overview | architecture | design | feature | comparison | reuse | changelog | issues | dependency | index
project: "..."
upstream: "..."   # 有则写 GitHub URL，没有则省略
tags: [...]
analyzed_at: <当前日期>
doc_version: "1.0"
sources: [文件/API 来源]
related: [关联文档]
---
```

### 第 3 步：并行深度侦察（Parallel Research Agents）

按项目规模**并发射出 3–4 个子代理**，每个专注一个维度。每个代理必须有清晰输出契约（结构化报告），含真实文件路径与代码片段：

**代理 A — 技术架构**（Explore，`max_turns=40`）：遍历主要源码目录，逐包汇报职责、关键文件、核心类型/函数、包间调用；HTTP 路由/中间件/数据层/业务层；从入口到输出的完整数据流。

**代理 B — 设计理念与核心特性**（Explore，`max_turns=30`）：从 README + 源码提炼设计哲学（stated purpose、values、audience）；为每个核心功能定位实现代码 + 短片段 + 原理；列出工程权衡与限制。

**代理 C — 上游变更与 issue 追踪**（general-purpose，`max_turns=25`）：仅在项目有已知上游 repo 时发射。用 `gh` CLI 优先拉取仓库概览、releases/tags、open/closed issues 归类、近期 commit 主题；本地有 git 历史先 `git log` / `git tag`。

**代理 D — 上游依赖追踪**（Explore，`max_turns=25`）：读依赖清单；对每个直接依赖用 Grep 确认导入/使用目的，经 `proxy.golang.org` / `registry.npmjs.org` / `crates.io` 查最新版本；追踪 replace/fork 的原因；识别供应链风险。

派发规则：

- 各维度相互独立、不写同一批文件，**放在同一条消息里全部发出**。
- `max_turns` 随规模调整（小项目 15–20，中 25–30，大 35–40）。
- 代理 prompt **完全自包含**（代理看不到当前会话）：项目路径、技术栈、要查什么、报告格式。
- 明确告知代理「只读 + 返回结构化报告，不执行任何修改」。
- 项目很大（500+ 文件 / 10 万行）时，把架构探查拆成后端、前端两个代理。

### 第 4 步：综合落盘（Synthesis）

1. 按规划的文档体系逐一写 `<project>/docs/NN-xxx.md`（`docs/` 不存在则创建）。
2. 每篇以 `---` YAML meta 头开头，通过 `related:` 做交叉引用。
3. 不同代理结论矛盾时，**以「在本地源码中确认的」为准**。
4. 不在本仓库内的实现（外部依赖、fork 仓库）显式标注「此实现不在本仓库」。

### 第 5 步：交付（Deliver）

1. 按编号展示全部文档。
2. 附精简总览：文档清单 + 关键结论。
3. 如需保留过程记录，按 `docs-rulebook` 的活动区约定落盘。

## 注意事项

- **只读不写**：不修改项目源码。
- **精确引用**：代码位置用相对项目根的路径。
- **不做架空推测**：不在可见范围内的实现如实标注。
- **版本意识**：有上游 repo 时标注分析所基于的版本/commit。
- **来源透明**：每篇 doc 的 `sources` 列出全部信息来源。
- **并行优先**：相互独立的维度必须并行，不要串行。
- **YAML 质量**：`tags` 与 `category` 是未来检索的关键，务必备足。
