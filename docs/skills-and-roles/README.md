# 助手角色 & 内置 Skill 总览

> mocode 内置 **10 个专家角色**（模式）与 **97 个内置 skill** 的可浏览索引。
> 权威来源：角色 = `internal/core/config/templates/modes/*.md`；skill = `internal/core/skills/builtin/<name>/SKILL.md`（front matter `name` 必须等于目录名）。
> 首次构建/重启后生效。

---

## 一、专家角色（Modes）

用模式选择器 / `Ctrl+G` 切换；每个角色声明其主导 skill、工作流与约束。

| 角色 | id | 定位 | 主导 skill |
|------|----|------|-----------|
| Architect | `architect` | 深模块设计、领域建模与架构演进 | codebase-design, domain-modeling, improve-codebase-architecture, safe-refactor |
| Frontend | `frontend` | React/Next 性能、组合式组件与可用性 | vercel-react-best-practices, vercel-composition-patterns, design-taste-frontend, web-design-guidelines, screenshot-to-ui, css-layout-and-box-model, design-tokens |
| Backend | `backend` | 异步并发、序列化、错误处理与可观测 | rust-backend, runtime, streaming, observability, security-and-hardening |
| Designer | `designer` | 体验与视觉品味（反模板化） | design-taste-frontend, web-design-guidelines, screenshot-to-ui, css-layout-and-box-model, design-tokens |
| QA | `qa` | 五轴评审、证据优先、完成即可验证 | code-review-and-quality, verification-before-completion, incremental-implementation, investigate-first |
| AI Engineer | `ai-engineer` | 模型集成、流式、多智能体编排与可观测 | rig-core-llm-integration, multi-agent-orchestration, streaming, observability |
| Mobile | `mobile` | Tauri/跨端 WebView + Rust 后端 + 原生打包 | tauri, rust-backend, rust-android-apk, ui-replication |
| DevOps | `devops` | 可观测 / 精简交付 / 安全加固 / 发布 | observability, lean-build, security-and-hardening, shipping-gitea-prs, triage |
| Researcher | `researcher` | 项目拆解 / 逆向文档 / 多主题并行调研 | project-teardown, reverse-engineering-docs, fromsko-research, investigate-first, handoff |
| Writer | `writer` | 计划·规范·报告与文档体系 | docs-rulebook, writing-plans, report-writer, common-rulebook, to-spec |

---

## 二、内置 Skill（97）

### 元技能 · 发现与自省
- `using-agent-skills` — 发现并调用 skill 的元技能
- `find-skills` — 按需求发现/安装 skill
- `using-superpowers` — 探索扩展能力（superpowers/codex/gemini/copilot）
- `writing-skills` — 编写/校验 skill（front matter、触发描述、压力测试）
- `dot-skill` — 把同事/关系/名人蒸馏成可复用 Skill 的元引擎
- `deep-roles` — 专家角色激活索引（按路由表读人设原文）

### 规范与理念
- `docs-rulebook` — 文档与计划目录规范（三区生命周期、主题目录、`plans/NN-*-plan.md`）
- `common-rulebook` — 跨栈/跨组织规范萃取方法论（可平移实践 vs 口味决策）
- `ooml-rulebook` — 实例层家法（OOMOL 库：架构、API/SDK、错误、测试、发布）
- `karpathy-guidelines` — LLM 编码行为铁律（反过度设计、显式假设、可验证成功标准）
- `lean-build` — 最小完整端到端交付，acceptance 通过即停

### 计划 · 协作 · 编排
- `planning-with-files` — 跨会话计划/进度/发现三文件跟踪
- `writing-plans` — 把 spec 写成可执行计划（对齐 docs-rulebook）
- `executing-plans` — 按计划分步执行 + 评审检查点
- `spec-driven-development` — 先写 spec 再写码
- `to-spec` — 把对话/理解收敛成可发布 spec
- `brainstorming` — 创作前的需求与设计探索
- `subagent-driven-development` — 每任务派实现子代理 + 质量门
- `sub-team-dev` — 总控+子代理的团队化开发（文件约定协作）
- `dispatching-parallel-agents` — 并行派发无依赖子任务
- `multi-agent-orchestration` — 多专家分阶段端到端编排
- `handoff` — 会话压缩成交接文档

### 评审 · 质量 · 验收
- `code-review-and-quality` — 五轴评审 + 严重级 + 变更规模
- `requesting-code-review` — 派独立评审子代理核验
- `receiving-code-review` — 严谨对待评审反馈
- `multi-review` — 多专家并行审查 + 两轮制
- `verification-before-completion` — 有证据再声明完成
- `verify-and-stop` — 只验证不扩 scope，证明完成即停
- `triage` — issue/PR 状态机与分流（2 category + 5 state）
- `surgical-patch` — 收窄到最窄责任层的外科式修补
- `bug-lesson` — 修 bug 闭环 + 回归测试 + 踩坑沉淀
- `test-driven-development` — 红-绿-重构

### 调试 · 调查 · 重构
- `systematic-debugging` — 提修复前先系统定位
- `investigate-first` — 无可信机制解释全部证据前不动手
- `incremental-implementation` — thin vertical slice 增量推进
- `safe-refactor` — 行为保持的重构

### 架构 · 领域 · 设计系统
- `codebase-design` — 深模块词汇表与接口深化
- `domain-modeling` — 术语表 + ADR
- `improve-codebase-architecture` — 架构热点探查与改进报告
- `design-md` — 用 `DESIGN.md` 落地一致设计语言
- `design-taste-frontend` — Brief → 三拨盘 → 设计系统（反模板化）

### 前端 · UI 库
- `vercel-react-best-practices` — React/Next 70 条最佳实践
- `vercel-composition-patterns` — 组合式组件设计
- `web-design-guidelines` — UI 可用性审查清单
- `tailwindcss` — Tailwind v4 用法与陷阱
- `shadcn` — shadcn/ui registry/CLI/preset 与规则
- `zustand` — 轻量状态管理
- `zod` — 运行时校验与类型推导
- `ui-replication` — 设计稿复刻与视觉偏差修复
- `playwright-cli` — 浏览器自动化与 E2E

### UI 设计方法学 · 视觉产出
- `screenshot-to-ui` — 截图/设计稿 → UI 复刻方法学（盒子模型/布局/间距/字号/色彩/组件/状态/验收）
- `css-layout-and-box-model` — 盒子模型与布局系统（Flex/Grid/定位/层叠/溢出/逻辑属性/排错）
- `design-tokens` — 设计变量体系（颜色/间距/字号/圆角/阴影/动效/主题）
- `ecommerce-image-studio` — 产品图 → 电商图组（六图组/平台方向/合规）
- `interactive-h5-app` — 静态原型 → 零依赖可交互 H5
- `red-team-animation-verification` — 动画逐帧验证 + red team 攻击矩阵

### AI 对话 UI · LLM 集成
- `assistant-ui` — AI 聊天界面原语与运行时选型
- `primitives` — assistant-ui composable primitives 规则
- `runtime` — assistant-ui runtime 系统
- `streaming` — assistant-stream 流式后端与 wire protocol
- `cloud` — assistant-ui Cloud 持久化与授权
- `observability` — tracing/telemetry 接入（Langfuse/LangSmith/Helicone）
- `rig-core-llm-integration` — rig-core 流式 LLM 集成（Rust/Tauri）

### 后端 · 运行时 · 构建
- `rust-backend` — Rust 后端（tokio/reqwest/serde/错误/tracing）
- `rust-android-apk` — Rust → Android APK（Tauri v2）
- `tauri` — Tauri 桌面/移动（IPC/权限/插件/打包）
- `bun` — Bun 运行时/包管理/打包/测试
- `rsbuild` — Rsbuild/Rspack 前端构建
- `go-tools-mcp-master` — Go MCP Server 开发与安全清单

### Shell · Windows 环境
- `powershell-on-windows` — bash 工具里调用 PowerShell 的坑与模式
- `nu-shell-helper` — Nushell 答疑与命令
- `pi-windows-setup` — pi on Windows 配置排查
- `windows-junction-link` — Windows 目录 Junction
- `jq` — JSON 查询/构造

### 写作 · 文档 · 可视化产出
- `report-writer` — 结构化报告（md/html/docx/pdf）
- `docx` — Word 文档处理
- `pptx` — PowerPoint 处理
- `kami-pdf-workflow` — Kami 设计系统 → 印刷级 PDF
- `stunning-html-slides` — 三轨 HTML 幻灯片
- `excalidraw-diagram` — Excalidraw 语义图表 + Playwright 自检
- `file-organizer` — 目录整理/去重/报告

### 研究 · 调研 · 抓取 · 复盘
- `project-teardown` — 全方位拆解项目 → 结构化文档
- `reverse-engineering-docs` — 逆向成「删码可复刻」文档
- `fromsko-research` — 多主题/仓库并行调研 + 溯源
- `fromsko-knowledge` — 知识萃取与归档入口
- `public-social-research` — 公开社交媒体调研（三档深度）
- `firecrawl` — 网页搜索/抓取/爬取/变更监控
- `session-viz` — 会话结构可视化
- `exp-recorder` — 任务经验固化到 `.exps/`

### Git · 交付 · 安全
- `shipping-gitea-prs` — Gitea/Forgejo 交付与 PR
- `github-issue-fix-pr` — issue → 合规 PR 六阶段
- `resolving-merge-conflicts` — 理解双方意图后解冲突
- `finishing-a-development-branch` — 分支收尾（merge/PR/保留/丢弃）
- `using-git-worktrees` — 隔离工作区
- `security-and-hardening` — 边界校验、会话、第三方集成的加固

### Mocode 自身
- `mocode-config` — mocode 配置（provider/model/MCP/LSP/权限/模式）
- `mocode-hooks` — mocode hooks（PreToolUse 拦截与护栏）

---

## 三、新增 skill / 角色

- **新增 skill**：建 `internal/core/skills/builtin/<name>/SKILL.md`，front matter `name` 必须等于目录名（kebab-case，**禁止下划线**），`description` 以 `Use when ...` 开头且 ≤1024 字符（**多行用 `>-` 折叠块，避免含 `:` 的 plain scalar 解析失败**）。方法见 `writing-skills`。
- **新增角色**：建 `internal/core/config/templates/modes/<id>.md`（front matter `id/name/description` + 可选 `sub_agents/tools`），在正文声明主导 skill、工作流与约束。
- 两者均通过 `//go:embed` 打包，**需重新构建并重启**生效；`internal/core/skills` 的测试会校验所有内置 skill 合法。
