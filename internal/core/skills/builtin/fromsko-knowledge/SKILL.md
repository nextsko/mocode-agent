---
name: fromsko-knowledge
description: >-
  Use when 需要把经验 / 结论「记下来、沉淀、归档、萃取、写笔记」，或做「压缩记忆 /
  会话快照 / 重建索引 / SOP 萃取」，以及完成调试、选型、架构决策后的收尾归档时。
  知识库一切读写都从本入口（根目录 `/data/fromsko/kng/`）按路由表进；`bug-lesson`
  的 bug 笔记、`fromsko-research` 的调研结论都经此入库。
---

# fromsko-knowledge：知识萃取与管理总入口

## 概述

知识库根：`/data/fromsko/kng/`。总索引 `INDEX.md`，明细 `knowledge/INDEX.md`（两者自动生成）。沉淀纪律见 kng 根目录的 `AGENTS.md`。

本技能是**索引层**：先按下方路由表**读 command 原文再执行，不要凭记忆复述流程**。

## 路由表

| 用户意图 | 先读原文 |
|---|---|
| 把当前对话的隐性经验结构化归档（首席知识工程师流程） | `/data/fromsko/kng/commands/knowledge-extract.md` |
| 会话结束，压缩记忆 / 产出会话快照 | `/data/fromsko/kng/commands/memory-compress.md` |
| 重建或更新知识库索引 | `/data/fromsko/kng/commands/index-knowledge.md`（Linux 直接 `python3 /data/fromsko/kng/scripts/index-knowledge.py`，幂等覆盖） |
| 从工程文档 / SOP 材料萃取标准流程 | `/data/fromsko/kng/commands/require-extract.md` |

## 执行约定

- **写入位置**：`/data/fromsko/kng/knowledge/` 对应子目录——
  `project-map` / `bugs` / `bug-recipes` / `patterns` / `research` / `specs` / `references` / `snapshots`。
- **命名**：`编号-主题-技术-动作.md`，编号从现有文件续编，不重号。
- **模板**：遵循 `/data/fromsko/kng/templates/`——
  `knowledge-unit.md`（知识单元）、`pattern-template.md`（模式，含 Tradeoffs 与 When Not To Use）、
  `bug-recipe-template.md`（完整 bug 配方，含 Regression Guard）、`snapshot.md`（快照）。
- **索引新鲜度**：新增 / 删除 knowledge 文件后**必须重跑索引脚本**，保持 INDEX 新鲜。
- **时间戳**：Linux/macOS 用 `date -u +%Y-%m-%dT%H:%M:%SZ`；Windows 参考
  `/data/fromsko/kng/scripts/get-timestamp.ps1`；**禁止凭空推断日期**。
- **不必记录**：简单读文件、问候、trivial 命令。

## 判断流程

1. 判断意图属于路由表哪一行；命中则**打开原文**，按原文步骤执行（本 skill 不复制流程细节）。
2. 属于「收尾归档」（调试 / 选型 / 架构决策完成后）但拿不准归哪个 command 时，先走 `knowledge-extract.md`。
3. 写入后按其模板产出，并重跑索引；只需跨会话留存当前状态时改走 `memory-compress.md`。

## 入库协作

- `bug-lesson` 产出的 bug 笔记与完整配方，经本入口按 `bug-recipe-template.md` 入库到 `bugs/` 与 `bug-recipes/`。
- `fromsko-research` 的调研结论与 Adopt/Reject 决策，入库到 `research/`。
- rulebook 族的萃取与重蒸馏产物，按知识单元或模式模板入库后重建索引。
- 入库条目应能被按**技术关键词**检索到——`bug-lesson` 修前检索依赖于此。

## Checklist

- [ ] 已按路由表读 command 原文，而非凭记忆复述
- [ ] 写入正确的 knowledge 子目录，编号续编不重号
- [ ] 套用对应模板（模式含 Tradeoffs / When Not To Use；bug 配方含 Regression Guard）
- [ ] 时间戳来自命令 / 脚本，未凭空推断
- [ ] 新增或删除后已重跑索引脚本
- [ ] 未把 trivial 操作记入知识库

## 相关技能

- `bug-lesson`：修 bug 前后检索 bugs / bug-recipes 并回流笔记。
- `fromsko-research`：调研结论的产生与归档。
- `common-rulebook` / `docs-rulebook`：规范萃取与文档目录纪律。
- `handoff`：会话压缩 / 交接时的记忆快照。
