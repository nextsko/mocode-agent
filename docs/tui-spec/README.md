# TUI 设计规范总目录

> 基于 **mocode**（`github.com/package-register/mocode`）和 **crush**（`github.com/charmbracelet/crush`）两个 Charm 生态 TUI 项目的源码提炼而成。
> 两者底层均为 **Bubble Tea v2 + Ultraviolet + Lip Gloss v2 + Glamour v2**。

---

## 目录结构

```
docs/tui-spec/
├── README.md                            # 本文件：总目录
├── 01-coding-standards.md               # 编码规范 + 最小 MVP 路线
├── 02-design-techniques.md              # 6 大优秀设计技巧拆解（mocode 原生）
├── 03-state-and-layout.md               # 状态数据流转 + 配色 + 布局方法论
├── 04-component-apis.md                 # 组件 API 调用清单 + 优秀设计洞察
├── 05-extending-and-testing.md          # 组件扩展/删除/测试方法论
├── 06-html-to-tui-prototyping.md        # HTML→TUI 原型建模方法
├── 07-claude-code-patterns.md           # Claude Code subagent/team/AskUserQuestion 吸收
├── 08-team-mode-architecture.md         # Team 模式架构深度解析
├── 09-hermes-and-claude-code-evolution.md  # Hermes 自进化 + CC 动态工作流 + Go 生态
└── 10-tty-input-and-bg-tasks.md        # TTY 输入处理 + 后台任务观测追踪
```

---

## 阅读顺序建议

| 阶段 | 文档 | 用途 |
|------|------|------|
| **入门** | `01` | 快速建立编码心智模型和 MVP 落地路径 |
| **仿写** | `02` | 拆解 mocode 的视觉亮点，复刻到自己的 TUI |
| **架构** | `03` + `04` | 理解状态/布局/组件的设计哲学与 API 选型 |
| **生产** | `05` | 添加/删除/测试组件的标准 SOP |
| **创新** | `06` | 用 HTML 加速原型设计并迁移到 TUI |

---

## 核心原则（One-liner）

> **UI 是唯一的 Bubble Tea model；所有子组件都是命令式结构体；矩形合成优先于字符串拼接；语义化样式字段；任何 IO/耗时都走 `tea.Cmd`。**

——`internal/ui/AGENTS.md`

---

## 文件大小预估

| 文档 | 字数估计 | 阅读时长 |
|------|----------|----------|
| 01 | ~6k | 20 min |
| 02 | ~12k | 40 min |
| 03 | ~9k | 30 min |
| 04 | ~8k | 25 min |
| 05 | ~7k | 25 min |
| 06 | ~5k | 15 min |
| 07 | ~13k | 45 min |
| 08 | ~14k | 50 min |
| 09 | ~15k | 55 min |
| 10 | ~13k | 45 min |
| **合计** | **~102k** | **~5.7 h** |