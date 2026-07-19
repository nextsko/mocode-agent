# Mocode Documentation

> 终端 AI 编程助手 Mocode 的项目文档中心。

---

## 目录

### 设计文档

| # | 文档 | 主题 |
|---|------|------|
| 01 | [design/01-coding-standards.md](design/01-coding-standards.md) | TUI 编码规范 + 最小 MVP 路线 |
| 02 | [design/02-design-techniques.md](design/02-design-techniques.md) | 6 大优秀设计技巧拆解 |
| 03 | [design/03-state-and-layout.md](design/03-state-and-layout.md) | 状态数据流转 + 配色 + 布局方法论 |
| 04 | [design/04-component-apis.md](design/04-component-apis.md) | 组件 API 调用清单 + 优秀设计洞察 |
| 05 | [design/05-extending-and-testing.md](design/05-extending-and-testing.md) | 组件扩展 / 删除 / 测试方法论 |
| 06 | [design/06-html-to-tui-prototyping.md](design/06-html-to-tui-prototyping.md) | HTML → TUI 原型建模方法 |
| 07 | [design/07-claude-code-patterns.md](design/07-claude-code-patterns.md) | Claude Code 优秀设计模式吸收 |
| 08 | [design/08-team-mode-architecture.md](design/08-team-mode-architecture.md) | Team 模式架构深度解析 |
| 09 | [design/09-hermes-and-claude-code-evolution.md](design/09-hermes-and-claude-code-evolution.md) | Hermes 自进化 + CC 动态工作流 + Go 生态 |
| 10 | [design/10-tty-input-and-bg-tasks.md](design/10-tty-input-and-bg-tasks.md) | TTY 输入处理 + 后台任务观测追踪 |
| 11 | [design/11-roundtable-team-mode-design.md](design/11-roundtable-team-mode-design.md) | Roundtable Team Mode Design Spec |
| 12 | [design/12-agent-architecture-refactor-design.md](design/12-agent-architecture-refactor-design.md) | Agent Architecture Refactor Design |
| 13 | [design/13-roundtable-phase1-core-engine.md](design/13-roundtable-phase1-core-engine.md) | Roundtable Phase 1: Core Domain Engine |
| 14 | [design/14-agent-architecture-refactor-plan.md](design/14-agent-architecture-refactor-plan.md) | Agent Architecture Refactor 实施计划 |
| 15 | [design/15-file-mention-menu-redesign.md](design/15-file-mention-menu-redesign.md) | FileMentionMenu Redesign Design |

### 架构文档

| # | 文档 | 主题 |
|---|------|------|
| 01 | [architecture/01-control-plane.md](architecture/01-control-plane.md) | HTTP 控制平面（Admin）|
| 02 | [architecture/02-workspace-types.md](architecture/02-workspace-types.md) | Workspace 类型与数据流 |
| 03 | [architecture/03-slash-command-arch.md](architecture/03-slash-command-arch.md) | Slash Command 系统架构（两套命令系统）|
| 04 | [architecture/04-tool-system-comparison.md](architecture/04-tool-system-comparison.md) | Mocode vs trpc-agent-go 工具系统对比 |

### 开发指南

| # | 文档 | 主题 |
|---|------|------|
| 01 | [guides/01-go-verification-pipeline.md](guides/01-go-verification-pipeline.md) | Go 5 步验证流水线 + 6 个行为测试 |
| 02 | [guides/02-structure-governance-baseline.md](guides/02-structure-governance-baseline.md) | 项目结构治理基线（多轮 Review）|
| 03 | [guides/03-dependency-upgrade-procedure.md](guides/03-dependency-upgrade-procedure.md) | 依赖升级 SOP（含 dependabot 解读）|
| 04 | [guides/04-gfw-workarounds.md](guides/04-gfw-workarounds.md) | GFW 环境下的网络工具笔记 |
| 05 | [guides/05-roundtable-team-mode.md](guides/05-roundtable-team-mode.md) | Roundtable Team Mode 设计 |
| 06 | [guides/06-agent-cooperation.md](guides/06-agent-cooperation.md) | 子代理协作实战笔记 |
| 07 | [guides/07-evolution-loop-plan.md](guides/07-evolution-loop-plan.md) | 内循环闭环进化系统设计 |
| 08 | [guides/08-evaluation-plan.md](guides/08-evaluation-plan.md) | Evaluation 体系落地计划 |
| 09 | [guides/09-ci-fix-log.md](guides/09-ci-fix-log.md) | CI/CD 修复历史记录 |
