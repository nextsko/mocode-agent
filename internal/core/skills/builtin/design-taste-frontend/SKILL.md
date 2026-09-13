---
name: design-taste-frontend
description: >-
  Use when 构建或重设计 landing page、作品集、营销站点等前端界面，先做 brief 推断与三档设计拨盘（variance/motion/density），再选设计系统或审美方向，规避模板化 AI 痕迹并执行上线前检查。不适用于 dashboard、数据表、多步表单等产品型 UI。
---

# 前端设计品味（Anti-Slop）

面向 **landing page / 作品集 / 重设计**。每条规则都是**情境化**的：先读 brief，再只取适用的部分，不要机械套用。

> 本技能只管视觉方向与前端实现；设计文档与计划按 `docs-rulebook` 组织，代码评审按 `code-review-and-quality`，涉及表单 / 鉴权 / 用户输入时按 `security-and-hardening`。

## 0. Brief 推断（改代码前先做）

先读这些信号：页面类型（landing / portfolio / redesign / editorial）、用户用的氛围词（minimalist / Linear-style / brutalist / premium consumer / Awwwards）、参考链接与截图、受众（B2B 采购 vs 设计敏感消费者 vs 招聘者）、已有品牌资产、隐性约束（无障碍 / 公共部门 / 合规优先）。

**输出一句 Design Read**，例如：
> Reading this as: B2B SaaS landing for technical buyers, with a Linear-style minimalist language, leaning toward Tailwind utilities + Geist + restrained motion.

若 brief 确实模糊且无法推断，**只问一个问题**，不要多问；能推断就直接声明并开工。不要默认使用 AI 紫渐变、居中 hero 配暗色 mesh、三张等宽 feature 卡、通用玻璃拟态、Inter + slate-900。

## 1. 三个拨盘（全局配置）

| 拨盘 | 范围 | 基线 |
|---|---|---|
| `DESIGN_VARIANCE` | 1 完美对称 → 10 艺术混乱 | 8 |
| `MOTION_INTENSITY` | 1 静态 → 10 电影级 / 物理 | 6 |
| `VISUAL_DENSITY` | 1 画廊留白 → 10 座舱密数据 | 4 |

推断表（节选）：

| 信号 | VARIANCE | MOTION | DENSITY |
|---|---|---|---|
| minimalist / clean / calm / editorial | 5-6 | 3-4 | 2-3 |
| premium consumer / Apple-y / luxury | 7-8 | 5-7 | 3-4 |
| playful / Awwwards / agency / experimental | 9-10 | 8-10 | 3-4 |
| trust-first / public-sector / 合规优先 | 3-4 | 2-3 | 4-5 |
| redesign - preserve | 沿用现状 | +1 | 沿用现状 |
| redesign - overhaul | +2 | +2 | 沿用现状 |

拨盘是全局变量，全文只用这三个名字，不要自造别名。`MOTION_INTENSITY > 3` 时必须支持 `prefers-reduced-motion`。

## 2. Brief → 设计系统映射

**有官方系统就用官方包，不要手搓它的 CSS**；一个项目只用一个系统。常见映射：

| Brief 氛围 | 选用 |
|---|---|
| Microsoft / 企业 SaaS / dashboard | `@fluentui/react-components` |
| Google / Material 风格产品 | `@material/web` + Material 3 tokens |
| IBM 风格 B2B / 分析 | `@carbon/react` + `@carbon/styles` |
| GitHub 风格 devtool | `@primer/css` 或 `@primer/react-brand` |
| 英国公共服务 | `govuk-frontend` |
| 美国政府 / trust-first | `uswds` |
| 现代可访问 React 基础 | `@radix-ui/themes` |
| 自有组件的现代 SaaS | shadcn/ui（必须定制，禁止出厂默认态） |
| 独立 / 小团队现代 SaaS | Tailwind v4 utilities + `dark:` |

**审美 ≠ 系统**：玻璃拟态、Bento、Brutalism、社论、暗黑科技、Aurora、动态字体等没有官方包，用原生 CSS + Tailwind + 组件库诚实实现，并在注释里标明哪些是借用灵感。Apple Liquid Glass 没有官方 Web CSS 包，只能标注为近似实现（`backdrop-filter` + 分层边框 + 高光）。

## 3. 默认技术栈与约定

- **框架**：React / Next.js，默认 Server Components。全局状态仅限 Client Component；动效、滚动监听、指针物理必须是顶层带 `'use client'` 的隔离叶子。
- **样式**：Tailwind v4 默认（v4 用 `@tailwindcss/postcss`，不要在 postcss 里用 `tailwindcss` 插件）。
- **动效**：Motion，`import { motion } from "motion/react"`。
- **字体**：用 `next/font` 或自托管 `@font-face` + `font-display: swap`，生产环境禁止 `<link>` 引 Google Fonts。
- **状态**：连续值（鼠标、滚动进度）用 `useMotionValue` / `useScroll`，**绝不**用 `useState`。
- **图标**：优先 Phosphor / HugeIcons / Radix / Tabler，一个项目一个家族，全局统一 `strokeWidth`；禁止手绘 SVG；Lucide 仅在用户明确要求时。
- **Emoji**：默认不用，除非 brief 明确要 playful / 社交风。
- **响应式**：断点 `sm 640 / md 768 / lg 1024 / xl 1280 / 2xl 1536`；容器 `max-w-7xl` 或 `max-w-[1400px] mx-auto`。
- **视口稳定**：全高 hero 用 `min-h-[100dvh]`，禁止 `h-screen`；栅格布局用 CSS Grid，禁止 flex 百分比数学。
- **依赖核查**：导入任何三方库前先查 `package.json`，缺失就先给出安装命令。

## 4. 纠偏默认（LLM 常见套路）

- **排版**：显示标题默认 `text-4xl md:text-6xl tracking-tighter leading-none`；正文 `text-base text-gray-600 leading-relaxed max-w-[65ch]`。默认**不用 Inter**（改用 Geist / Outfit / Satoshi / Cabinet Grotesk 等）；衬线体默认强烈不推荐，`Fraunces` 与 `Instrument_Serif` 直接禁用。斜体单词含 `y g j p q` 时行高至少 `leading-[1.1]` 并预留 `pb-1`，防止降部被裁。
- **配色**：最多 1 个强调色，饱和度默认 < 80%；禁用 AI 紫 / 外发光 / 随机霓虹渐变。一旦选定强调色就**全页锁定**。premium-consumer brief 禁用默认的米色 + 黄铜 / 陶土 / 赭石 + 浓咖啡色组合，轮换使用冷奢、森林、黑棕、钴蓝 + 奶油、陶土 + 石板灰等家族。
- **布局**：`DESIGN_VARIANCE > 4` 时避免居中 hero，改用分屏、左右非对称、不对称留白或滚动固定结构；禁止三张等宽 feature 卡。圆角系统全页锁定（全直角 / 全 12-16px / 全 pill，或成文规则下混用）。
- **材质**：只有需要表达层级时才用卡片，否则用 `border-t` / `divide-y` / 留白分组；阴影带背景色调，避免纯黑投影。
- **交互态**：必须实现完整的 loading（骨架屏，不用圆形 spinner）、empty、error 状态；`:active` 用 `-translate-y-[1px]` 或 `scale-[0.98]` 提供触感。上线前检查按钮文字与背景对比度（正文 4.5:1，大字号 3:1）。

## 5. 禁止的 AI 痕迹（硬性）

- **Em-dash（—）完全禁用**，en-dash 作分隔符同样禁用；只能用普通连字符 `-`。标题、eyebrow、按钮、正文、引文、attribution、caption、alt 文本一律适用。
- Hero：禁止版本标签（`V0.6` / `BETA` / `EARLY ACCESS`，除非就是发布预告）、禁止 `Brand · No. 01` 类微标签。
- 禁止章节编号 eyebrow（`00 / INDEX`、`01 / 4` 分页）、禁止滚动提示（`Scroll`、`↓ scroll`）、禁止装饰性彩色状态点（零容忍，仅真实语义状态可少量使用）。
- 装饰性排版：`·` 每行最多 1 个；禁止 `<br>` + 斜体标题、旋转竖排文字、纯装饰十字 / 发丝网格线、hero 底部装饰文字条（`BRAND. MOTION. SPATIAL.`）。
- 禁止 div 手搓的假产品截图（最强 AI 痕迹），用真实 / 生成 / 组件预览或干脆不放；禁止假版本页脚。
- 文案：禁止 `Quietly in use at` / `From the field` / 天气 locale 条 / `Stage 1 / Step 1` 通用步骤标签 / eyebrow 下的微 meta 句；禁止 `Acme`、`Nexus` 类品牌名与 `Elevate`、`Seamless` 类空话；数字要么真实要么明确标注为 mock。
- 列表：长列表（> 5 项）换用 2 列分组、卡片网格、tab / accordion、scroll-snap 或 marquee，禁止每行都加 `border-t` + `border-b`；规范表用 2 列卡片网格或分组块。
- 图片上禁止叠加 pill / 标签，禁止装饰性 photo-credit，禁止 div 假预览，禁止坏掉的 Unsplash 链接（用 `https://picsum.photos/seed/{slug}/{w}/{h}`）。
- 引文 ≤ 3 行，attribution 为姓名 + 角色（可选公司），使用弯引号。
- 每页最多 1 个 marquee；每页只锁一种主题（light / dark / auto），章节不得中途反色。

## 6. 动效纪律

只动画 `transform` 与 `opacity`，禁止 `top/left/width/height`；`will-change` 少用。**禁止** `window.addEventListener("scroll")`、基于 `window.scrollY` 的 React state 进度、触碰 React state 的 rAF 循环。改用 Motion `useScroll()`、GSAP ScrollTrigger、IntersectionObserver 或 CSS scroll-driven animations。

简单进场用 Motion `whileInView`（轻量）；GSAP 只留给真正的 pin / scrub：

```tsx
"use client";
// ScrollTrigger: start: "top top", pin: true, scrub: 1
ScrollTrigger.create({ trigger: card, start: "top top", pin: true, pinSpacing: false });
```

粘滞堆叠与横向平移都必须 `start: "top top"`。每个动画都要能用一句话说明动机（层级 / 叙事 / 反馈 / 状态转换），否则删掉。GSAP / Three.js 与 Motion 不可混用在同一组件树。

## 7. 性能与可访问性护栏

- `MOTION_INTENSITY > 3` 的动效必须遵守 `prefers-reduced-motion`，在 Motion 用 `useReducedMotion()`，在 CSS 用媒体查询降级为静态。
- 消费级页面默认双模式（light + dark），保持层级与品牌色一致，避免 `#000000` / `#ffffff` 纯色。
- Core Web Vitals：LCP < 2.5s（hero 图 `priority` / 预加载）、INP < 200ms、CLS < 0.1（预留尺寸）。
- 噪点 / 颗粒滤镜只加在 `fixed pointer-events-none` 伪元素上，不要加在滚动容器。
- z-index 只用于系统性层级（导航、模态、遮罩），不要到处 `z-50`。

## 8. 重设计协议

1. 先判定模式：greenfield / preserve（保品牌渐进）/ overhaul（保内容与 IA，视觉重来）。含糊只问一次。
2. **先审计**：品牌 token、信息架构、内容块、要保留的交互、要淘汰的 AI 痕迹、现有三拨盘读数、SEO 基线（#1 风险）。
3. 保留规则：不改 IA / slug / 锚点 / 导航标签，保留文案语气与无障碍成果，不重命名分析事件依赖的元素。
4. 现代化杠杆（按序，够用即停）：排版刷新 → 间距节奏 → 配色校准 → 动效层 → hero / 关键区块重组 → 整块替换。
5. 未经批准不得改动：URL / 路由、主导航标签、表单字段名与顺序、logo / wordmark、法律与 cookie 文案。

## 9. 上线前检查（关键项）

- [ ] 已声明 Design Read，三拨盘有依据而非常用基线
- [ ] 设计系统唯一，或审美方向已诚实标注
- [ ] 全页零 `—` / `–`（作分隔符）
- [ ] 主题、强调色、圆角系统三项各自全页锁定
- [ ] 每个 CTA 文字对背景对比度达标，桌面端 CTA 不换行
- [ ] hero ≤ 2 行标题、副文案 ≤ 20 词、CTA 不滚动可见、`pt` 不超过 `pt-24`
- [ ] eyebrow 数量 ≤ ceil(章节数 / 3)，无左大标题 + 右小解释的分裂头部
- [ ] 无 3 段以上相同图文分栏、无重复意图 CTA、logo 墙只放 logo
- [ ] 真实图片 / 允许的图标库，无 div 假截图、无手绘 SVG
- [ ] 动效有动机、marquee ≤ 1、滚动监听未用 `addEventListener`
- [ ] reduced-motion、暗色模式、移动端折叠（`w-full px-4`）均已处理
- [ ] `min-h-[100dvh]`、`useEffect` 有清理、empty / loading / error 齐备
- [ ] Core Web Vitals 可信达标，文案自审无语法 / 幻觉句

任何一项无法诚实勾选，就不算完成。
