# 助手角色 & 内置 Skill 总览

> mocode 内置 **20 个专家角色**（模式）与 **99 个内置 skill** 的可浏览索引。
> **本文档由 `internal/core/skills/gen` 自动生成**，请勿手改；运行 `go run ./internal/core/skills/gen` 刷新。
> 角色 = `internal/core/config/templates/modes/*.md`；skill = `internal/core/skills/builtin/<name>/SKILL.md`（front matter `name` 必须等于目录名）。

---

## 一、专家角色（Modes）

用模式选择器 / `Ctrl+G` 切换；每个角色声明其主导 skill、工作流与约束。

| 角色 | id | 定位 | 主导 skill |
|------|----|------|-----------|
| AI Engineer | `ai-engineer` | AI 工程师 — 模型集成、流式、多智能体编排与可观测 | rig-core-llm-integration, multi-agent-orchestration, streaming, observability |
| Analyzer | `analyzer` | 本质洞察型分析助手 — 话语拆解、深度学习、多模型思维 |  |
| Architect | `architect` | 架构师 — 深模块设计、领域建模与架构演进 | codebase-design, domain-modeling, improve-codebase-architecture, safe-refactor, docs-rulebook |
| Ask | `ask` | 只读问答助手 — 代码分析、架构理解 |  |
| Backend | `backend` | 后端专家 — 异步并发、序列化、错误处理与可观测 | rust-backend, runtime, observability, security-and-hardening |
| Designer | `designer` | 设计专家 — 体验与视觉品味、可用性、反模板化 | design-taste-frontend, web-design-guidelines, screenshot-to-ui, css-layout-and-box-model, design-tokens, responsive-design, motion-design |
| DevOps | `devops` | 平台工程师 — 可观测、精简交付、安全加固与发布 | observability, lean-build, security-and-hardening, shipping-gitea-prs, triage, using-git-worktrees |
| Evolve | `evolve` | 系统进化助手 — Bug 分析、模式提取、规则更新、持续改进 |  |
| Frontend | `frontend` | 前端专家 — React/Next 性能、组合式组件与可用性 | vercel-react-best-practices, vercel-composition-patterns, design-taste-frontend, web-design-guidelines, screenshot-to-ui, css-layout-and-box-model, design-tokens, responsive-design, motion-design |
| Git | `git` | 智能 Git 助手 — 规范提交、智能分组、分支管理 |  |
| MCP | `mcp` | MCP 配置助手 — 管理 MCP 服务器、验证配置、测试连接 |  |
| Mobile | `mobile` | 移动端专家 — Tauri/跨端 WebView + Rust 后端 + 原生打包 | tauri, rust-backend, rust-android-apk, ui-replication, vercel-react-best-practices |
| Obsidian | `obsidian` | Obsidian 智能笔记助手 — 结构化工作日报、学习资源记录、项目源码分析 |  |
| Plan | `plan` | 计划与协调模式 — DAG 任务拆解、并行调度、上下文管理 |  |
| Prompt Gen | `prompt-gen` | 提示词工程专家 — 生成专属助手 System Prompt，支持调用 mocode_info 和加载技能包 |  |
| QA | `qa` | 质量官 — 五轴评审、证据优先、完成即可验证 | code-review-and-quality, verification-before-completion, test-driven-development, incremental-implementation |
| Researcher | `researcher` | 调研专家 — 项目拆解、逆向文档与多主题并行调研 | project-teardown, reverse-engineering-docs, fromsko-research, investigate-first, handoff |
| Reviewer | `reviewer` | 代码审查助手 — 检查 Bug、一致性、风险 |  |
| Searcher | `searcher` | 代码定位与 Bug 诊断专家 — 深度思考 + 高效搜索 + 子代理协作 |  |
| Writer | `writer` | 技术写作 — 计划/规范/报告与文档体系 | docs-rulebook, writing-plans, report-writer, common-rulebook, to-spec |

---

## 二、内置 Skill（99）

### 元技能 · 发现与自省
- `deep-roles` — 说「激活小安 / 领域小安」，或要求「以深度调试专家 / 深度调研专家 / 学习者身份」进入长篇多轮自主规划、系统性调试、深度调研或学习式讲解时。按路由表 读取角色人设原文，以对应职责与方法工作；调试场景可与 bug-lesson（踩坑检索）叠加。
- `dot-skill` — Unified meta-skill engine for distilling colleague, relationship, or celebrity characters into reusable Skills. | 统一的 meta-skill 引擎，把 colleague、relationship、celebrity 三类对象蒸馏成可复用 Skill。
- `find-skills` — Helps users discover and install agent skills when they ask questions like "how do I do X", "find a skill for X", "is there a skill that can...", or express interest in extending capabilities. This skill should be used when the user is looking for functionality that might exist as an installable skill.
- `using-agent-skills` — Discovers and invokes agent skills. Use when starting a session or when you need to discover which skill applies to the current task. This is the meta-skill that governs how all other skills are discovered and invoked.
- `using-superpowers` — the user mentions "superpowers", "codex", "gemini tools", "copilot", or when they want to know about extended capabilities beyond the standard toolset. Also use when exploring what special abilities or integrations are available in the current environment.
- `writing-skills` — creating a new skill, editing an existing skill, or verifying that a skill actually changes agent behavior before deploying it — including writing SKILL.md front matter, designing triggering descriptions, and pressure-testing guidance against rationalization.

### 规范与理念
- `common-rulebook` — 需要为某个组织或代码库萃取公司级编码规范、判断一条规范是"可平移的最佳 实践"还是"口味/业务决策"、评审架构决策是否违背普适工程原则、建立或刷新 org rulebook，或把规范做成 skill 时。这是跨栈跨组织的方法论层；具体参数（工具选型、 命名、色值、端点）住在实例层 skill（如 ooml-rulebook）。用法顺序：先查目标组织 有无实例层 rulebook——有则同时加载，冲突时实例参数优先；实例若违反本 skill 的 普适原则，标记为"实例层债务"并报告，而非静默服从。
- `docs-rulebook` — the user asks "where should docs/plans live", "how to write a plan", "how to organize docs", "how to archive docs", "where does MASTER_PLAN go", or when creating/moving/archiving documents or designing a docs directory structure in any project. Not tied to a language or editor. If the target repository already has its own docs convention, follow the repository first.
- `karpathy-guidelines` — 编写、评审或重构代码时，需要避免过度设计、保持改动精准、显式暴露 假设与歧义、并把任务转化为可验证的成功标准时（源自 Andrej Karpathy 对 LLM 常见编码陷阱的观察）。适用于任何语言与任何规模的实现与评审，尤其是 agent 自主编码、跨会话连续修改、以及容易被"能跑就行"诱惑的场景。核心倾向是谨慎 优于速度——琐碎任务自行裁量。
- `lean-build` — 实现新行为、产品切片或系统集成，且存在明显的过度构建（overbuilding）风险时： 需求边界模糊、容易顺手加 mode/provider/config/扩展点/打磨，或需要把 feature 落成 横跨各责任层的最小完整端到端路径。以 repository 复用、严格 scope 与显式 acceptance 为纪律，acceptance 通过即停。适用于 feature 开发、slice 落地、与既有系统集成。
- `ooml-rulebook` — 在 OOMOL（oomol-lab）代码库中写代码、新建仓库、做架构决策、设计 API/SDK/CLI、写 React/Tailwind/shadcn 或 SwiftUI 界面、处理错误与重试、组织 测试、发版、写 AGENTS.md/README，或评审某设计是否符合 OOMOL 工程观念时。 本 skill 是 common-rulebook 方法论在 oomol-lab 的实例层（参数集）：具体工具 选型、命名、色值、参数以本文件为准；方法论与跨栈普适原则见 common-rulebook， 两者冲突时本文件实例参数优先。蒸馏自 oomol-lab 64 仓库（2026-08-25 快照）。

### 计划 · 协作 · 编排
- `brainstorming` — You MUST use this before any creative work - creating features, building components, adding functionality, or modifying behavior. Explores user intent, requirements and design before implementation.
- `dispatching-parallel-agents` — facing two or more independent tasks or failures that share no state and have no sequential dependency — such as several failing test files with different root causes — and you want to delegate each to a focused subagent that runs concurrently.
- `executing-plans` — the user says "execute the plan", "implement the plan", "start implementation", "begin coding", or when a structured implementation plan exists (per the docs-rulebook layout, e.g. <docs-root>/<topic>/plans/NN-*-plan.md) and needs to be carried out with review checkpoints. Also use when the user provides a numbered task list or multi-step implementation guide that should be executed methodically.
- `handoff` — 需要把当前会话压缩成一份交接文档，让一个全新代理或下一次会话能在没有 本会话上下文的情况下接手继续。适用于上下文即将耗尽、任务跨会话续做、或需要把 工作进行移交时。产出写入操作系统临时目录（不写工作区），包含"suggested skills" 段落、按路径/URL 引用既有产物而非复制，并脱敏敏感信息。
- `multi-agent-orchestration` — Deliver an end-to-end objective by orchestrating multiple named domain-expert subagents across phased work, when the user hands you full autonomy ("just get it done", "you make all the decisions", "I only want results"). Use whenever a goal spans several distinct specialties (e.g. environment setup + implementation + QA), the user delegates all decisions to you, and the work decomposes into phases with clean interfaces between them. Distinct from subagent-driven-development (one generic implementer per planned task + review gate) and dispatching-parallel-agents (fan-out for independent debugging).
- `planning-with-files` — the user has a multi-step task that needs structured planning, progress tracking, and context management across sessions. Covers creating task plans with phases/stages, tracking progress with checkpoints, saving research findings and decisions, maintaining execution context across interruptions, and managing the plan-implement-verify lifecycle. Essential for complex tasks that span multiple conversations or require careful step-by-step execution with review checkpoints.
- `spec-driven-development` — Creates specs before coding. Use when starting a new project, feature, or significant change and no specification exists yet. Use when requirements are unclear, ambiguous, or only exist as a vague idea. For the execution and progress-tracking companion to this skill, see planning-with-files.
- `sub-team-dev` — 用"总控 + 子代理"的团队模式开发中大型功能或整个项目：总控通过文件约定 （计划 / 契约 / 简报 / 报告 / 账本）管理逐任务的子代理实现，并亲自验收。适用于 用户要求"用子代理分任务推进""团队化开发""派代理实现""分阶段实现某个项目/大功能"， 或提到 sub-team-dev、文件约定协作。不适用于单文件小改动。
- `subagent-driven-development` — executing an implementation plan whose tasks are mostly independent, in the current session, by dispatching a fresh implementer subagent per task with a spec + quality review gate after each and a broad whole-branch review at the end. Not for work that needs a parallel session (use executing-plans), tightly coupled tasks with no clean interfaces, or when the user wants to approve each step.
- `to-spec` — you need to turn the current conversation and codebase understanding into a written spec without interviewing the user — synthesizing the problem, solution, user stories, implementation and testing decisions, and out-of-scope notes into a publishable document.
- `writing-plans` — you have a spec or requirements for a multi-step task and need an implementation plan before touching code — deciding where the plan lives, mapping files, decomposing tasks into bite-sized TDD steps, and handing off to execution.

### 评审 · 质量 · 验收
- `bug-lesson` — 修任何 bug、排查报错/崩溃/异常行为时：修前先检索知识库历史踩坑，避免重复 诊断；按逻辑树/逻辑链定位根因，不修表象；每个修复必须附带能复现原故障的回归测试； 修完沉淀 bug 笔记。说「这个报错怎么回事 / 帮我修一下 / 为什么挂了 / 排查一下 / 定位一下」即触发。
- `code-review-and-quality` — 需要评审代码变更、在合并前把质量关，或需要从 correctness、readability、 architecture、security、performance 五个维度评估代码时。适用于 PR 评审、功能 实现完成后、重构既有代码，以及任何 bug fix 之后（修复与回归测试一并评审）。 即使代码由你自己、另一个 agent 或人类编写，也应执行同一套评审与批准标准。
- `multi-review` — 重大改动在合入前需要多视角把关：多专家并行审查 + 两轮制（先各自出意见， 汇总去重，再交叉复核争议点）。触发语包括"审查/评审/review 这段代码""多个角度看" "帮我把把关"。适用于重构、安全敏感、核心逻辑变更，也适用于 bug-lesson 修复的重大 bug、rulebook 评审发现的债务整改落地前。单文件小改动不必走本流程，用 code-review-and-quality 即可。
- `receiving-code-review` — receiving code review feedback, before implementing suggestions, especially if feedback seems unclear or technically questionable - requires technical rigor and verification, not performative agreement or blind implementation
- `requesting-code-review` — 完成任务、实现主要功能或合并到 main 之前，需要派一个独立的代码评审子 Agent 核验工作是否 满足需求时。评审者只拿到精心构造的上下文（描述、需求、BASE/HEAD SHA），绝不继承当前会话历史。 核心原则：早评审、勤评审。
- `surgical-patch` — 修复 bug 或做小幅行为改动，且需要收窄到拥有该错误行为的最窄责任层时： 强调 regression proof、保护周边行为与用户改动，并只运行与任务相关的测试。 适用于回归修复、小行为修正，以及打补丁可能波及无关代码的场景。
- `test-driven-development` — the user says "write tests first", "TDD", "test-driven", "red-green-refactor", or when implementing any feature where tests should be written before the implementation code. Also use when adding unit tests, integration tests, fixing a bug by first writing a failing test, or when the task requires ensuring test coverage before implementation.
- `triage` — 需要把 issue 或外部 PR 沿一组 triage 角色（2 个 category + 5 个 state） 推进状态机：分类、验证、必要时 grilling，并写出 agent-ready brief。适用于维护者 调用 `/triage`、查看待处理事项、把条目移到 ready-for-agent / ready-for-human / needs-info / wontfix，以及登记 out-of-scope 知识库。
- `verification-before-completion` — 准备声明工作已完成、已修复或已通过测试，或在 commit、创建 PR、结束任务之前。 它要求先运行验证命令并确认输出，再做出任何成功声明——evidence before assertions always。 适用于任何形式的完成/成功表述、满意表达、对工作状态的正面陈述，以及委托给 agent 后的独立核验。
- `verify-and-stop` — 只做验证/验收——证明既有工作满足 acceptance conditions 而不扩大 scope： 验证型任务、完成度检查、聚焦 gate 运行、last-mile proof。核心纪律是把 acceptance 翻译成最小充分证明集，精确区分 pass/fail/unavailable/blocked，证明完成即停。

### 调试 · 调查 · 重构
- `incremental-implementation` — 要实现任何涉及多个文件的 feature 或变更、从任务拆解开始构建新功能、 重构既有代码，或当你准备一次性写大量代码、任务大到无法一步落地时。以 thin vertical slice 推进：实现一小块、测试、验证、提交，再扩展，让每个增量都保持 系统可工作、可测试。单一文件/单一函数且范围已最小的改动不适用本技能。
- `investigate-first` — 遇到原因不明、间歇性、性能回退等模糊故障，需要在改动产品代码之前先诊断； 或调查需要按证据排序假设时。适用于 failure 根因不明、证据不足、需要先定位再修复 的场景。核心纪律：在没有一个可信机制能解释全部证据之前，不要编辑。
- `safe-refactor` — restructuring code while preserving behavior: extraction, consolidation, ownership moves, renames, or cleanup where verification must bracket the structural edits. Use before and after structural changes to keep intermediate states buildable and behavior provably unchanged.
- `systematic-debugging` — encountering any bug, test failure, or unexpected behavior, before proposing fixes

### 架构 · 领域 · 设计系统
- `codebase-design` — designing or improving a module's interface, hunting for deepening opportunities, deciding where a seam belongs, or making code more testable and AI-navigable. Also use when another skill needs the deep-module vocabulary (module, interface, depth, seam, adapter, leverage, locality).
- `design-md` — 想让 AI 在生成 UI 时保持一致的设计语言，需要在项目里落地或维护一份 DESIGN.md。用纯文本描述配色、字体、组件、间距与设计哲学，让任何 AI coding agent 都能据此产出像素级一致的界面。适用于用 AI 搭 UI、跨组件保持设计一致、复刻既有网站外观等场景。
- `design-taste-frontend` — 构建或重设计 landing page、作品集、营销站点等前端界面，先做 brief 推断与三档设计拨盘（variance/motion/density），再选设计系统或审美方向，规避模板化 AI 痕迹并执行上线前检查。不适用于 dashboard、数据表、多步表单等产品型 UI。
- `domain-modeling` — discussing a codebase's terminology, writing or editing a CONTEXT.md glossary, or recording and editing an ADR. Also use when code and stated domain rules disagree and the model needs to be clarified, sharpened, or written down.
- `improve-codebase-architecture` — asked to review, audit, or improve a codebase's architecture, find refactoring opportunities, reduce coupling, or make code more testable and AI-navigable. Scans for deepening opportunities, presents them as a visual HTML report, then drills into whichever candidate the user picks.

### 前端 · UI 库
- `playwright-cli` — automating browser interactions, running or debugging Playwright (E2E) tests, taking page screenshots, scraping web content, filling forms, clicking elements, or testing web application behavior in a headless browser. Covers Playwright CLI usage, test generation, recording, storage state (cookies/sessions), request mocking, video recording, element attributes, and spec-driven testing patterns.
- `shadcn` — 使用 shadcn/ui 或任何含 components.json 的项目添加、搜索、修复、更新 UI 组件与设计系统，或处理 registry、preset（--preset / preset code）、CLI（info/docs/search/add/diff）以及基于 shadcn 原语的 chat 界面。也适用于 "shadcn init"、"create an app with --preset"、"switch to --preset"。仅写 Tailwind 样式时见 tailwindcss。
- `tailwindcss` — 用 Tailwind CSS v4 以 utility class 构建界面：响应式布局、间距、排版、配色、暗色模式、group/peer、任意值、动画，或在 CSS 中用 @theme 定义设计 token；也适用于从 v3 迁移、配置构建插件，以及理解为何某些动态 class 不生效。
- `ui-replication` — 用户给出设计稿/截图/现有网页要求「复刻这个界面/1:1 还原/fork 这个 UI/做成一样」， 或已实现界面需要「修 UI/对齐设计稿/修样式偏差/视觉不对」。覆盖两条工作流：从零复刻（截图→ 结构映射→实现）与偏差修复，并用 playwright-cli 截图比对做视觉验收。
- `vercel-composition-patterns` — 重构布尔 prop 泛滥的组件、构建可复用组件库或设计组件 API，覆盖 compound components、 显式变体、children 组合、状态上提、context 依赖注入与 React 19 API 变更。适用于组件架构设计与评审。
- `vercel-react-best-practices` — 编写、审查或重构 React / Next.js 代码，覆盖数据获取、消除请求瀑布、包体积、Server Component 性能、客户端缓存、重渲染、渲染性能与 JavaScript 微优化。适用于组件编写、页面开发、数据获取实现与性能优化相关任务。
- `web-design-guidelines` — 被要求 "review my UI"、"check accessibility"、"audit design"、"review UX" 或对照最佳实践检查站点，按 Web Interface Guidelines 从无障碍、焦点、表单、动效、排版、性能、导航状态等维度审查 UI 代码并输出 file:line 形式的精简发现。
- `zod` — 在 TypeScript 中做运行时数据校验与类型推导：API 输入校验、表单校验、环境变量与配置文件校验，或任何数据边界（DB、URL query、webhook）。使用 z.object/parse/safeParse、z.infer、discriminatedUnion、transform/refine/coerce 与 schema 组合，避免重复定义类型和校验器。
- `zustand` — 在 React 应用里做全局或跨组件状态管理，用 Zustand 的 hook store、selector、middleware（persist/devtools/immer）、异步 action、computed 值，或在 React 组件外访问与订阅状态；也适用于用 Zustand 替代 Redux/Context 的样板代码。

### UI 设计方法学 · 视觉产出
- `css-layout-and-box-model` — 需要精确实现或排查布局与尺寸：盒模型（content/padding/border/margin）、盒尺寸计算、 包含块、常规流、Flex/Grid 对齐、定位与层叠、溢出与滚动、逻辑属性、响应式与容器查询， 以及「为什么这个元素撑不开 / 溢出 / 居中不了 / 高度塌陷」等疑难。
- `design-tokens` — 需要建立或统一设计系统变量：颜色、间距、字号、行高、圆角、阴影、动效的 token 体系， 主题（亮/暗/多品牌）切换，或把硬编码样式收敛为 token。也用于截图复刻时把量到的值固化为变量。
- `ecommerce-image-studio` — 用户想把产品照片变成可直接上架的电商图组：listing 主图、生活场景图、广告首帧、 卖点图、细节图、模特/lookbook。给出平台化创作方向、默认六图组、creative archetype、 模特图策略、提示词结构与保真/合规失败处理；本 skill 只做电商策略层，实际生图/改图交给 底层图像生成执行层。
- `interactive-h5-app` — 要把静态 / 高保真 HTML 原型推进成零依赖、浏览器直接打开即可玩的移动端 H5 单页 应用，且要求「每个组件都真实可交互、不要虚拟摆设」。涵盖三文件拆分、单一 state 与 localStorage 持久化、分区渲染、事件委托、弹层与 toast、种子数据与无头冒烟测试的完整工作流。
- `motion-design` — 设计或实现界面动效与微交互：选择时长/缓动 token（fast 120 / base 200 / slow 320ms、ease-out / spring）、 判断何时该用动效（反馈、过渡、引导注意力、表达层级与空间关系）与何时不该用、 设计 enter/exit、列表 stagger、共享元素/布局位移、skeleton、optimistic update 等模式， 遵守只动 transform/opacity 的性能纪律、prefers-reduced-motion 降级与焦点管理， 并用逐帧录制核对验收（见 red-team-animation-verification）。
- `red-team-animation-verification` — Use when「能跑」还不够，需要证明 LLM 聊天 / 交互式 UI 及其动画「打不坏、且真的在动」： 对聊天集成做对抗测试（XSS、prompt injection、超长输入、Unicode/RTL、流式卸载、 监听器泄漏），用 ffmpeg 逐帧差分验证 CSS 动画真的在播放（typewriter、spinner、 bubble 入场、hover），用视觉模型做 UI 终端验证，并守护 LOCKED testid 契约。
- `responsive-design` — 界面需要在多种屏幕与容器尺寸下都成立：选择并论证断点（640/768/1024/1280，由设计而非默认决定）、 移动优先与 min-width、流式布局替代固定宽度、用 clamp()/minmax()/auto-fit 表达弹性、 用 @container 让组件按自身宽度自适应、设计常见重排模式（栅格坍缩、导航收起为抽屉、表格转卡片、侧栏堆叠）、 响应式图片与字体、触控目标 ≥44px、安全区 env(safe-area-inset-*)、dvh 与横竖屏， 并用 playwright-cli 多尺寸截图比对验收。
- `screenshot-to-ui` — 拿到一张图片 / 设计稿 / 截图，要求「照它做出界面 / 1:1 复刻 / 还原这个页面 / 做成一样」。给出从像素到实现的完整方法学：如何测量并推导盒子模型、布局系统、间距节奏、 字体层级、色彩系统，如何做组件识别与状态补全，以及如何映射到 Tailwind/token 并做像素级验收。

### AI 对话 UI · LLM 集成
- `assistant-ui` — 用 React 构建 AI 聊天界面：从 assistant-ui 的可组合原语与运行时出发，做选包、选 runtime（useChatRuntime / useExternalStoreRuntime / useLangGraphRuntime / useLocalRuntime）、理解分层模型（RuntimeCore / Runtime / aui client / primitives）与消息模型、多线程与 branching。覆盖 @assistant-ui/react 0.15.x 搭配 AI SDK v7。
- `cloud` — 为 assistant-ui 应用接入 Cloud 持久化与授权（assistant-cloud 的 AssistantCloud + useChatRuntime 的 cloud 选项）：跨会话 / 多设备 thread & message 历史、文件上传、JWT/authToken、API key + userId/workspaceId、anonymous 模式，以及 NextAuth / Clerk / Firebase / better-auth 接入；cloud.threads.list/get/create/update/delete、messages.list/create、files.generatePresignedUploadUrl、aui/v0 消息格式、自定义适配器（CloudMessagePersistence、createFormattedPersistence、ThreadHistoryAdapter、RemoteThreadListAdapter）、自动标题、external_id/metadata 映射，环境变量 NEXT_PUBLIC_ASSISTANT_BASE_URL / ASSISTANT_API_KEY。线程列表侧边栏 UI 用 thread-list。
- `observability` — 为 assistant-ui 后端接入 tracing / telemetry / observability：把 AI SDK 路由 （streamText/generateText、toUIMessageStream、createUIMessageStreamResponse）接到 Langfuse （OpenTelemetry + LangfuseSpanProcessor + NodeSDK + experimental_telemetry + propagateAttributes + serverless forceFlush）、LangSmith（wrapAISDK(ai) + createLangSmithProviderOptions + awaitPendingTraceBatches）或 Helicone（createOpenAI baseURL https://oai.helicone.ai/v1 + Helicone-Auth 头）；以及用 @assistant-ui/react-o11y 无头原语（SpanResource、SpanPrimitive、 SpanByIndexProvider、SpanData/SpanState）绘制 trace waterfall。用于 trace 缺失/为空、edge 与 nodejs 运行时差异、serverless flush、trace 瀑布排查。
- `primitives` — 用 @assistant-ui/react 的 composable、unstyled primitives 组装或定制聊天 UI （Thread、Composer、message rendering、action bar、branch picker 等），或需要确认 part 组合、children render function、part grouping、条件渲染 AuiIf 与 RuntimeProvider 等 API 规则时。适用于从 building blocks 自建界面，而非使用预置 drop-in UI。
- `rig-core-llm-integration` — 需要把 rig-core crate（v0.40）集成进 Rust / Tauri 应用，经 OpenAI 兼容端点流式输出 LLM chat completion（opencode-go、one-api、vllm、ollama 桥、Azure OpenAI 等）。当用户提到 rig、rig-core、streaming LLM output in Rust、Tauri 流式 command、自定义 base_url provider、修复 rig 的 404、`stream_chat` / `completions_api` / import 报错时使用。提炼自一次真实跑通的 rig-core 0.40 + opencode-go + Tauri 2.x 集成。
- `runtime` — 使用 @assistant-ui/react 的 runtime 系统：创建 runtime（useLocalRuntime + ChatModelAdapter、 useExternalStoreRuntime 接 Redux/Zustand、useRemoteThreadListRuntime、useAssistantTransportRuntime）、 挂载 AssistantRuntimeProvider，或读写 thread / message / composer / attachment 状态；覆盖 useAui / useAuiState / useAuiEvent 与 v0.15 属性访问器（aui.thread、aui.message、aui.composer）， s.optional.<scope>、capabilities、adapters（attachment / speech / dictation / suggestion / feedback / history）、realtime voice 与核心类型。用于排查 provider 内 "Cannot read property of undefined"、 状态不更新等问题。多线程列表 UI 与会话切换另见 thread-list 相关约定。
- `streaming` — 构建或调试 assistant-ui 的流式后端与 wire protocol（assistant-stream 包）：用 createAssistantStreamResponse / createAssistantStreamController 自建端点，通过 appendText/appendReasoning/appendSource/appendFile/addToolCallPart/setResponse 发出分片； 在 AI SDK UI-message 格式（toUIMessageStream + createUIMessageStreamResponse）与原生 Assistant Transport 格式之间选型；用 DataStreamEncoder/Decoder、AssistantTransportEncoder/Decoder、 PlainTextEncoder、UIMessageStreamDecoder 编解码；接入 useLocalRuntime / useChatRuntime； 排查 text-delta、part-start、result 事件、text/event-stream、SSE、tool call 不渲染、部分文本不显示； 以及用 assistant-stream/resumable 实现可续传流。

### 后端 · 运行时 · 构建
- `bun` — 使用 Bun 作为一体化 JavaScript/TypeScript 运行时、包管理器、打包器或测试器，包括用 Bun.serve() 构建 HTTP/WebSocket 服务、bun install/add/remove 管理依赖、bun build 打包、 bun test 跑测试、Bun.file/Bun.write 文件 I/O、Bun.password 做认证、bun:sqlite 嵌入式存储， 以及从 Node.js 迁移到 Bun。
- `go-tools-mcp-master` — 开发或审查 Go MCP Server（基于 github.com/mark3labs/mcp-go v0.32.0），或为 go-tools 仓库 7 个 MCP 项目做技术选型：NewMCPServer 与 ServerOption、stdio/SSE/StreamableHTTP 三种 transport、工厂列表注册表、NewTool + WithXxx + Required/Enum schema、CallToolRequest 提参辅助（optInt 必须兼容 float64）、textResult/errResult 约定、--book 自描述，以及路径沙箱（AbsResolve/EvalSymlinks/isInside）、命令白名单 + 参数黑名单、破坏性命令正则、SQL 只读、危险端点 deny-list、SSRF DialContext 拦截、fail-closed、原子写入与按 rune 截断。
- `rsbuild` — 使用 Rsbuild（基于 Rspack/Rust）搭建或优化前端构建，包括配置 rsbuild.config.ts、 React/Vue/Svelte/Solid 插件、开发服务器与代理、代码分割与包体积、Module Federation、 多环境构建、按需 polyfill，以及从 webpack/Vite/CRA/Vue CLI 迁移。
- `rust-android-apk` — Build a Rust application into an Android APK and run it on an emulator or device. Use whenever the user wants Rust code to run on Android — packaging Rust as an APK, cross-compiling Rust for Android, Tauri v2 mobile/Android setup, `tauri android init` or `tauri android build` troubleshooting, Android emulator/AVD setup without Android Studio, NDK/Gradle errors during Rust-Android builds, or signing Rust-built Android APKs. Covers the full path: toolchain detection → emulator provisioning → project init → Rust core + frontend → cross-compile → packaging → signing → deploy → verify. Distilled from a verified end-to-end Tauri v2 build on Windows.
- `rust-backend` — 用 Rust 开发后端服务、API 或系统级组件：异步编程（tokio）、HTTP 客户端（reqwest）、 序列化（serde）、错误处理（anyhow/thiserror）、结构化日志（tracing）、Web 抓取 （scraper/html2md）、配置与数据库（config/sqlx），以及生产级测试与交付检查。
- `tauri` — 用 Tauri 构建轻量跨平台桌面/移动应用：系统 WebView + Rust 后端、`#[tauri::command]` IPC 与事件、capabilities 权限、官方插件（fs/dialog/shell/notification/store/updater）、 `tauri build` 打包分发与自动更新。Trigger: tauri, rust desktop, system webview, tauri commands, tauri plugins.

### Shell · Windows 环境
- `jq` — the user needs to query, filter, reshape, extract, create, or construct JSON data — including API responses, config files, log output, or any structured data — or when helping the user write or debug JSON transformations.
- `nu-shell-helper` — 回答 Nushell（nu）相关问题、编写 nu 命令/脚本/管道，或用户提到 nushell、`nu`、 `nu.exe`、nu 的表格/dataframe 操作、nu 插件命令时。也适用于工作目录中存在 `nu.exe` 或 nu 插件的场景。不用于一般 shell/bash/PowerShell 问题，除非用户明确要求 Nushell 方案。
- `pi-windows-setup` — 在 Windows 上安装、配置或排查 pi coding agent：shellPath、PowerShell 兼容别名、 MCP server、自定义模型、system prompt、packages 等。触发词包括 "setup pi on windows"、 "configure pi windows"、"pi shell not working"、"pi mcp setup"、"pi model configuration"，或任何 pi 的 Windows 配置问题。
- `powershell-on-windows` — operating on a Windows environment and needing to write or debug PowerShell commands via the bash tool. Covers the `$` variable stripping issue, script-file workaround, common pitfalls, and correct patterns for file sizing, encoding, pipeline formatting, and directory recursion.
- `windows-junction-link` — 需要在 Windows 上创建目录 Junction 链接（mklink /J），让两个目录指向同一份磁盘 数据，例如把 `~/.workbuddy/skills` 指向 `~/.agents/skills`、跨工具同步配置目录、或让某个 工具直接看到另一处已有文件而无需复制。Junction 是双向的，任一边的改动立即可见于另一边。 触发词：创建 junction 链接、windows 目录链接、mklink、目录映射、同步两个目录、junction link。

### 写作 · 文档 · 可视化产出
- `docx` — the user needs to create, edit, or process Microsoft Word (.docx) documents. Covers generating new documents from Markdown, editing text and formatting, inserting images/tables/headers/footers, managing comments and tracked changes, extracting text, converting between formats (MD to DOCX, DOCX to MD/pdf), modifying styles, replacing text, and handling document templates. Uses python-docx, pandoc, or LibreOffice depending on the task.
- `excalidraw-diagram` — 需要用 Excalidraw 生成可视化流程图、架构图或概念图，把工作流/系统关系画成"会论证"的视觉结构。产出 `.excalidraw` JSON，并用 Playwright 渲染成 PNG 反复自检修正，直到构图、间距、箭头都正确。也适用于把文字描述转成 fan-out、时间线、收敛等有语义的图形。
- `file-organizer` — the user needs to organize, sort, classify, clean up, or manage files in a directory. Covers scanning directories, categorizing files by type/date/size, moving files into structured folders, deduplication, renaming batches, cleaning temp files, and generating organization reports. Handles safety checks to avoid system directories.
- `kami-pdf-workflow` — 需要用 Kami 设计系统（tw93/Kami）生成印刷级 PDF 文档——「生成 PDF / 排版 / 一页纸 / 个人画像 / profile PDF / HTML 转 PDF / 简历 / 白皮书 / 信件 / 作品集 / 幻灯片」， 即填充 Kami HTML 模板并用 Playwright Chromium 渲染成 A4 PDF 时。
- `pptx` — the user needs to create, edit, or process Microsoft PowerPoint (.pptx) presentations. Covers generating slides from data, adding charts, tables, images, animations, transitions, speaker notes, slide layouts, master slides, converting Markdown to PPTX, merging presentations, extracting content, and batch slide creation. Uses python-pptx or LibreOffice depending on the task.
- `report-writer` — the user needs to write structured reports — data analysis reports, weekly/monthly reports, industry research reports, project status reports, academic summaries, or any formal document requiring a professional template. Covers report structure design, data-driven writing, insight extraction, and formatted output (.md, .html, .docx, .pdf). NOT for creative writing, code documentation, or simple email replies.
- `stunning-html-slides` — 需要制作令人惊艳的 HTML 幻灯片/演示文稿，支持三种路径：模板库快选（32 套专业模板）、杂志风横向翻页（WebGL 流体背景 + 衬线排版）、现代 UI 风（毛玻璃卡片、Spotlight 聚光灯、粒子、Framer Motion）。覆盖选型、主题节奏、组件配方、导航与质检，产出单文件 HTML deck。

### 研究 · 调研 · 抓取 · 复盘
- `exp-recorder` — 完成一次非平凡任务后（修 bug、依赖冲突、API 迁移、环境怪异行为、中途纠正的 错误假设、可复用的模式），需要把经验固化进项目 `.exps/` 目录、避免下一个人从零重发现时。 产出规范化的 `NNN-<title>.md` 条目并更新 `.exps/README.md` 索引，建立机构记忆。
- `firecrawl` — 用 Firecrawl CLI 搜索 / 抓取 / 交互网页，或需要整站爬取、AI 结构化抽提、页面变更监控：search、scrape、map、crawl、agent、interact、download、parse、monitor 的选型与升级路径，`--status` 自检、`-o` 落盘到 .firecrawl/、`--goal` 监控语义、JSON changeTracking 的 per-field diff、search / endpoint feedback（含退费与 opt-out 环境变量）、并发与 credit 用量。也覆盖把抓回的网页当不可信输入、防间接 prompt injection 与命令注入的要点；把 Firecrawl 集成进产品代码或做深度研究 / SEO / 线索等交付物时改用 firecrawl-build / firecrawl-workflows。不触发本地文件、git、部署、编辑任务。
- `fromsko-knowledge` — 需要把经验 / 结论「记下来、沉淀、归档、萃取、写笔记」，或做「压缩记忆 / 会话快照 / 重建索引 / SOP 萃取」，以及完成调试、选型、架构决策后的收尾归档时。 知识库一切读写都从本入口（根目录 `/data/fromsko/kng/`）按路由表进；`bug-lesson` 的 bug 笔记、`fromsko-research` 的调研结论都经此入库。
- `fromsko-research` — 需要「调研 / 选型 / 对比最佳实践 / 查资料 / 并行查」多主题资料，或「研究某个 git 仓库 / 开源项目 / 别人代码怎么写的」，或需要生成「组件树 / 依赖树 / 架构树 / 设计树」做代码溯源时。 统一用并行子 Agent 调研，产出带 Adopt/Reject 决策的结论并归档。
- `project-teardown` — 用户要求「拆解 / 分析 / 搞懂 / 学习 / 深入理解 / teardown」一个代码项目，需要产出多份带 meta YAML 检索头的结构化 Markdown 文档时。覆盖技术架构、设计理念、核心亮点、同类对比、可复用资产、 变更历史、issue 与依赖追踪。只读项目源码，产物统一落入 docs/。
- `public-social-research` — 用户要查询/研究公开社交媒体内容（TikTok、抖音、小红书、微博、YouTube、Reddit、 Twitter/X、知乎、快手、微信视频号/公众号等）——找几条帖子、做舆情简报、跨平台对比、 受众或评论洞察、达人评估、产品或投流决策。按 Quick Search / Insight Brief / Deep Research 三档控制检索深度与付费调用预算，产出带可核查来源与结论的简报或结构化报告。
- `reverse-engineering-docs` — 用户要「把项目逆向成可复刻的文档 / 生成架构文档 / 用自然语言描述整个设计 / 全面扫一下还有什么没弄的」，目标是让人只看文档就能完整复刻项目时。产物是 `docs/<project>-arch/` 下的一套自然语言文档，强调可执行命令、样式系统、权限/自动导入等易遗漏项，并做「删掉代码能否复刻」验证。
- `session-viz` — 需要可视化 mocode/andy-code 的会话结构、项目分布、消息流与事件链，或导出结构化 JSON。读取 session.db 与 projects/ 目录，用骨架解析 + 多种视图（tree/projects/flow/dump）呈现会话元数据、消息、工具产物、记忆与项目分类。只读、零副作用，支持增量扫描与损坏文件容错。

### Git · 交付 · 安全
- `finishing-a-development-branch` — 实现已完成、测试通过，需要决定如何整合这份工作（merge 回主干、开 PR、保留分支或丢弃）时。适用于开发分支收尾、worktree 清理、以及需要给出结构化选项而非开放问题的场景。核心流程：验证测试 → 探测环境 → 给出选项 → 执行选择 → 清理 workspace。
- `github-issue-fix-pr` — 用户想「修复 issue / pick an issue to fix / 帮他修 bug 并提 PR / 提交 PR」， 需要从 GitHub 开放 issue 中挑一个、定位根因、写修复与测试、本地验证、生成 changeset 并按 仓库贡献规范开出 PR 时。面向 TypeScript pnpm monorepo（kimi-code 类），六阶段顺序执行。
- `resolving-merge-conflicts` — 需要解决进行中的 git merge / rebase / cherry-pick conflict，遇到冲突标记（<<<<<<< ======= >>>>>>>）或 rebase 停在某一步时。适用于多分支并行开发后的集成分支、长生命周期分支落后于主干、以及需要把重放历史期间产生的冲突逐个理清的场景。目标是理解双方原始意图后完成合并，而不是 abort 或草率挑选一边。
- `security-and-hardening` — Hardens code against vulnerabilities. Use when handling user input, authentication, data storage, or external integrations. Use when building any feature that accepts untrusted data, manages user sessions, or interacts with third-party services.
- `shipping-gitea-prs` — Gitea/Forgejo 托管的 Git 仓库里已验证的改动准备交付，需要整理分支与远端、push 或提 PR， 或需要清理多余 worktree 且只保留唯一 origin 时。核心是先重跑权威验证命令，再做分支/远端正交化、 push、用 tea 建 PR，并删除临时登录凭证。
- `using-git-worktrees` — starting feature work that needs isolation from current workspace or before executing implementation plans - ensures an isolated workspace exists via native tools or git worktree fallback

### Mocode 自身
- `mocode-config` — the user asks about Mocode's configuration, settings, providers, models, MCP servers, LSP setup, environment variables, tools, permissions, hooks, agents, modes, or any mocode.json related questions. Also use when the user wants to change how Mocode behaves — set up a new provider/model, add an MCP server, configure LSP for a language, enable/disable tools or skills, tweak TUI appearance, configure network proxy, recording/records, attribution/trailer style, or manage agent modes. Covers everything in mocode.json from $schema to options.
- `mocode-hooks` — the user asks about Mocode hooks, PreToolUse interception, tool call guarding, hook scripts, hook configuration, or wants to set up security guardrails that intercept tool calls before or after execution. Covers the hooks section of mocode.json including matcher patterns, shell command hooks, timeout settings, and the allow/deny/halt decision model.

---

## 三、新增 skill / 角色

- **新增 skill**：建 `internal/core/skills/builtin/<name>/SKILL.md`，front matter `name` 必须等于目录名（kebab-case，**禁止下划线**），`description` 以 `Use when ...` 开头且 ≤1024 字符（**多行用 `>-` 折叠块，避免含 `:` 的 plain scalar 解析失败**）。方法见 `writing-skills`。
- **新增角色**：建 `internal/core/config/templates/modes/<id>.md`（front matter `id/name/description` + 可选 `sub_agents/tools`），在「主导技能」列表引用真实存在的 skill。
- **本文档自动生成**：改完运行 `go run ./internal/core/skills/gen` 刷新；`gen_test.go` 会在陈旧时报错。
- 两者均通过 `//go:embed` 打包，**需重新构建并重启**生效。
