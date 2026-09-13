---
name: improve-codebase-architecture
description: >-
  Use when asked to review, audit, or improve a codebase's architecture, find
  refactoring opportunities, reduce coupling, or make code more testable and
  AI-navigable. Scans for deepening opportunities, presents them as a visual
  HTML report, then drills into whichever candidate the user picks.
---

# Improve Codebase Architecture（改进代码库架构）

找出架构摩擦，提出**加深机会（deepening opportunities）**：把浅模块改造成深模块。目标是可测试性与 AI 可导航性。

本 skill 由项目的领域模型**驱动**，并建立在共享设计词汇之上：

- 架构词汇（**module / interface / depth / seam / adapter / leverage / locality**）及其原则（删除测试、「接口即测试面」、「一 adapter 假设 seam，两 adapter 真实 seam」）来自 `codebase-design`。**每条建议都严格用这些词**，不要漂移到 component / service / API / boundary。
- `CONTEXT.md` 的领域语言为好 seam 提供命名；`docs/adr/` 记录本流程**不应重新翻案**的决策。

## 第 1 步：探索

**先定范围，再扫描——YAGNI。** 加深模块靠的是让未来对它的改动更容易，所以额外加权到**近期变更过**的代码。看之前先决定看哪：

- 用户已指名方向（某模块、子系统、痛点）→ 采纳，跳过下面的推断。
- 否则回看一段提交历史（`git log --oneline`），找出热点文件与反复出现的区域，让这些路径先吸引注意力。若变更分散无热点，就扩大搜索面。

先读相关区域的领域术语表（`CONTEXT.md`）与 ADR。

然后 spawn 一个子代理遍历代码库。不要套死板启发式，有机地探索并记录**在哪里感到摩擦**：

- 理解一个概念是否要在许多小模块间跳来跳去？
- 哪些模块**浅**——接口几乎和实现一样复杂？
- 哪里为了可测性抽出了纯函数，但真正的 bug 藏在**它们如何被调用**里（缺 **locality**）？
- 哪里的紧耦合模块跨 seam 泄漏？
- 哪些部分没测试，或难以通过现有接口测试？

对任何疑似浅模块应用**删除测试**：删掉它是让复杂度集中，还是只是把复杂度挪走？「集中」才是你要的信号。

## 第 2 步：以 HTML 报告呈现候选

写一个**自包含 HTML** 到操作系统临时目录，避免污染仓库。解析临时目录：优先 `$TMPDIR`，回退 `/tmp`（Windows 用 `%TEMP%`），文件名为 `<tmpdir>/architecture-review-<timestamp>.html`，每次运行生成新文件。为用户打开（Linux `xdg-open`、macOS `open`、Windows `start`），并告知绝对路径。

报告用 **Tailwind（CDN）** 布局与样式，用 **Mermaid（CDN）** 画关系呈图状的图（调用图、依赖、时序）。把 Mermaid 与手写 CSS/SVG 混用：关系是图状时用 Mermaid，想要更「编辑感」的视觉（质量图、剖面、折叠动画）时用手搭的 div/SVG。**每个候选都要有 before/after 可视化。要视觉化。**

每个候选渲染一张卡片：

| 字段 | 内容 |
|------|------|
| **Files** | 涉及哪些文件/模块（等宽字体列表） |
| **Problem** | 当前架构为何造成摩擦，一句话 |
| **Solution** | 将要改变什么，平实英文，一句话 |
| **Benefits** | 用 locality / leverage 表述，并说明测试如何改善（≤6 词的 bullet） |
| **Before / After diagram** | 并排、自绘，展示浅与深 |
| **Recommendation strength** | `Strong`（emerald）/ `Worth exploring`（amber）/ `Speculative`（slate）徽章 |
| **依赖类别标签** | `in-process` / `local-substitutable` / `ports & adapters` / `mock` |

报告结尾放 **Top recommendation** 小节：先做哪个候选、为什么。

**语言纪律：**

- **领域用 `CONTEXT.md` 词汇，架构用 `codebase-design` 词汇。** 若 `CONTEXT.md` 定义了「Order」，就说「the Order intake module」，不要写「the FooBarHandler」，也不要写「the Order service」。
- **ADR 冲突：** 候选若与现有 ADR 矛盾，仅当摩擦真实到值得重开该 ADR 时才呈现，并在卡片里明确标记（如琥珀色警告：「contradicts ADR-0007, but worth reopening because…」）。不要列出 ADR 禁止的每个理论重构。
- 样式：倾编辑感而非企业仪表盘；留白充足；克制用色（一个强调色 + 红色表泄漏 + 琥珀色表警告）；图高约 320px 以便并排；图内模块标签用 `text-xs uppercase tracking-wider`，读起来像示意图而非 UI。除 Tailwind CDN 与 Mermaid ESM 外无其他脚本。
- 措辞：不绕弯、不铺垫、不写「值得注意的是……」；能用 bullet 就别用句子。可用的短语如：「Order intake module is shallow: interface nearly matches the implementation.」「Pricing leaks across the seam.」「Two adapters justify the seam: HTTP in prod, in-memory in tests.」Wins bullet 用术语命名收益：*locality: bugs concentrate in one module*、*leverage: one interface, N call sites*，别写「easier to maintain」「cleaner code」。

**此时不要提接口方案。** 文件写完后问用户：「Which of these would you like to explore?」

## 第 3 步：Grilling 循环

用户选定候选后，与他走决策树：约束、依赖、深模块的形状、seam 背后是什么、哪些测试能存活。

决策结晶时**就地产生副作用**，用 `domain-modeling` 保持领域模型同步：

- **给深模块命名的概念不在 `CONTEXT.md`？** 把该术语加进去，文件不存在则惰性创建。
- **对话中锐化了模糊术语？** 当场更新 `CONTEXT.md`。
- **用户带着承重理由否掉候选？** 提议 ADR：「要不要记成 ADR，这样未来的架构评审不会再提它？」仅当未来探索者确实需要该理由来避免重复提议时才提；跳过临时理由（「现在不值得」）与不言自明的理由。
- **想为深模块探索替代接口？** 用 `codebase-design` 的 **Design It Twice** 并行子代理模式。

## 词汇纪律速查

**只用：** module、interface、implementation、depth、deep、shallow、seam、adapter、leverage、locality。

**绝不替换：** component / service / unit（当指 module）· API / signature（当指 interface）· boundary（当指 seam）· layer / wrapper（当你要说的是 module）。

若一句话能变成 bullet，就变 bullet；能删就删。术语不在 `codebase-design` 术语表里，先从表里找一个，再考虑造新词。

## 与其他 skill 的关系

- `codebase-design`：唯一架构词汇与原则来源；`improve-codebase-architecture` 只负责扫描、呈报、驱动决策。
- `domain-modeling`：读取并回写 `CONTEXT.md` / ADR，深模块用领域词命名。
- `test-driven-development`：推荐加深时，用「接口即测试面」写新测试、删除旧浅模块单测。
- `safe-refactor`：候选落地时逐一边界迁移、保持中间状态可构建。
- `code-review-and-quality`：改动评审时确认接口契约、错误模式与测试覆盖。
- `docs-rulebook`：报告落在系统临时目录；若用户要求留存，按其目录规范归档。
