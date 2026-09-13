---
name: responsive-design
description: >-
  Use when 界面需要在多种屏幕与容器尺寸下都成立：选择并论证断点（640/768/1024/1280，由设计而非默认决定）、
  移动优先与 min-width、流式布局替代固定宽度、用 clamp()/minmax()/auto-fit 表达弹性、
  用 @container 让组件按自身宽度自适应、设计常见重排模式（栅格坍缩、导航收起为抽屉、表格转卡片、侧栏堆叠）、
  响应式图片与字体、触控目标 ≥44px、安全区 env(safe-area-inset-*)、dvh 与横竖屏，
  并用 playwright-cli 多尺寸截图比对验收。
---

# 响应式设计细则

一句话：**响应式不是「为每台设备写一版」，而是让同一份布局在任意宽度下都自然成立**——断点由内容决定，不由设备型号决定。

配套：`css-layout-and-box-model`（Flex/Grid/包含块）、`design-tokens`（断点/间距 token）、`tailwindcss`（`sm: md: lg:` 落地）、`screenshot-to-ui`（多视口状态补全）、`web-design-guidelines`（a11y/安全区）、`playwright-cli`（多尺寸验收）。

## 0. 核心心智模型

- **内容驱动，而非设备驱动**：宽度连续、设备名只是离散的营销分类；断点应落在「布局开始难看」的那一刻。
- **弹性优先，断点兜底**：能用 `clamp()`/`minmax()`/`auto-fit`/`%`/`fr` 表达的就别写死断点；断点只用于**无法连续表达**的结构切换。
- **组件自治 + 先窄后宽**：能按自身宽度自适应的组件用 `@container`，不关心全局视口；基础样式写移动端，用 `min-width` 逐级增强。

## 1. 断点选择与理由

档位及其**存在理由**（不是「大家都这么写」）：

| 断点 | 触发场景 | 典型重排 |
|------|----------|----------|
| 640px (sm) | 单列 → 双列卡片；手机横屏/大屏手机 | 栅格 `1 → 2` 列 |
| 768px (md) | 平板竖屏；导航由抽屉变为可见 | 侧栏出现 / 导航展开 |
| 1024px (lg) | 平板横屏 / 小笔电；复杂多列 | `2 → 3+` 列、媒体并排 |
| 1280px (xl) | 桌面；内容最大宽度生效 | 容器居中、留白增大 |

- **由设计定而非默认**：断点写进 `design-tokens`，团队统一；不要私自加 900/1100 等魔数。
- **数量最少化**：每加一个断点都增加维护成本与「只在某宽度才坏」的 bug 面。
- 用 `min-width` 语义命名；必须用 `max-width` 时成对断点错开 0.02px 防边界重叠。

## 2. 移动优先与 min-width

```css
.card { display: grid; grid-template-columns: 1fr; gap: 1rem; }   /* 基础样式 = 最窄 */
@media (min-width: 640px)  { .card { grid-template-columns: repeat(2, 1fr); } }
@media (min-width: 1024px) { .card { grid-template-columns: repeat(3, 1fr); } }
```

- 基础样式无媒体查询也能用，降级场景仍可读。
- 用 `min-width` 而非 `max-width`：样式**单调增强**，避免覆盖优先级连锁，易推理。
- Tailwind：基础类 = 移动，`sm: md: lg: xl:` 即 `min-width` 覆盖。

## 3. 流式布局 vs 固定宽度

- **反模式**：容器写死 `width: 1200px` → 窄屏横向滚动、宽屏大片空白。
- **正解**：内容容器 `width: min(100% - 2rem, 1200px); margin-inline: auto;`；网格 `repeat(auto-fit, minmax(240px, 1fr))` 连续自适应；媒体 `max-width: 100%; height: auto;`。
- **例外与区分**：可滚动代码块、画布/编辑器、宽数据表（外层套 `overflow-x: auto`）才用固定宽度；`max-width` 是上限、`width` 是强制，绝大多数用前者。

## 4. 弹性表达：clamp / minmax / auto-fit

```css
h1 { font-size: clamp(1.5rem, 1.2rem + 2vw, 3rem); }                        /* 流体字号 */
.cards  { grid-template-columns: repeat(auto-fit, minmax(min(240px, 100%), 1fr)); }
.layout { grid-template-columns: minmax(220px, 280px) 1fr; }                /* 侧栏区间 */
section { padding-inline: clamp(1rem, 4vw, 4rem); }
```

- `clamp(min, preferred, max)` 钳住上下限，中间随 `vw`/`%` 流动；`minmax(min, max)` 给 Grid 轨道一个可伸缩区间。
- `auto-fit` vs `auto-fill`：前者合并空轨道让卡片拉伸填满，后者保留空列。**卡片流用 `auto-fit`**。
- `minmax` 内套 `min(...)`：防超窄屏上 240px 比容器还宽而溢出。

## 5. 容器查询（组件自治，优先于视口断点）

视口断点的问题：同一卡片在主区是宽的、在侧栏是窄的，但视口一样宽——`@media` 无法区分。

```css
.card-host { container-type: inline-size; }        /* 宿主声明，不是组件自己 */
@container (min-width: 480px) { .card { grid-template-columns: 120px 1fr; } }
```

- `@container` 按**最近容器**宽度匹配；用 `container-name` 消除歧义。
- 组件库/设计系统**一律用容器查询**：组件在任何上下文都正确，不依赖页面布局。
- `container-type: size` 需两轴尺寸，会丢失内在高度，慎用。

## 6. 常见重排模式（reflow patterns）

| 模式 | 窄屏 → 宽屏 | 关键实现 |
|------|-------------|----------|
| 栅格坍缩 | 1 列 → 2–4 列 | `repeat(auto-fit, minmax(...))` 或断点切换 |
| 导航收起 | 汉堡抽屉 → 横排菜单 | 抽屉用 `<dialog>`/portal，`overscroll-behavior: contain` |
| 表格转卡片 | 每行一张卡片 → 真表格 | 窄屏隐藏 `<thead>`，`td` 借 `data-label` 显示字段名 |
| 侧栏堆叠 | 主内容在上、侧栏在下 → 并排 | `grid-template-columns` 切 `1fr` ↔ `280px 1fr` |
| 分栏反转 / 筛选 | 图在上、筛选为 sheet → 图在侧、顶栏筛选 | Grid + `order`；同一数据源两套壳 |

重排时**保内容与 DOM 语义**：两种形态应是同一份可访问结构（`<nav>` + `<ul>`），不是复制两份；抽屉/模态需**焦点管理**（见 `web-design-guidelines`）。

## 7. 响应式图片

```html
<img src="hero-800.jpg" width="800" height="600" loading="lazy" decoding="async" alt="…"
     srcset="hero-400.jpg 400w, hero-800.jpg 800w, hero-1600.jpg 1600w"
     sizes="(min-width: 1024px) 50vw, 100vw">
```

- `srcset` + `sizes` 让浏览器**按需选择**尺寸/密度，避免给手机下发桌面大图。
- `width`/`height` 或 `aspect-ratio` **必须给**，否则布局位移（CLS）。
- 首屏关键图 `fetchpriority="high"`（框架 `priority`），其余 `loading="lazy"`。
- `<picture>` + `<source media>` 用于**艺术指导**（构图裁切不同）；WebP/AVIF 用 `<source type>` 回退；满出血媒体用 `aspect-ratio` + `object-fit: cover`。

## 8. 响应式字体与排版

- 单行长度控制在 45–75 字符：`max-width: 65ch`。
- 用 `clamp()` 做流体字号，避免每断点单设 `font-size`；行高随字号调整（大标题收紧到 1.1–1.2）。
- `text-wrap: balance`（标题）/`pretty`（正文）减少孤行；`overflow-wrap: anywhere` 处理长 URL/长单词。

## 9. 触控、安全区与视口单位

- **触控目标 ≥ 44×44px**（含 padding），相邻目标留间距；`touch-action: manipulation` 去双击缩放延迟。
- **安全区**：全出血布局用 `env(safe-area-inset-*)`，如 `padding-bottom: calc(1rem + env(safe-area-inset-bottom));`，配合 `viewport-fit=cover`。
- **视口高度**：地址栏收放会让 `100vh` 抖动 → 用 `100dvh`/`100svh`/`100lvh`；底部固定栏注意软键盘（`visualViewport`）。

## 10. 方向与横竖屏

- 优先用**连续布局**适配方向变化，而非 `@media (orientation: landscape)` 硬切（平板横屏可能仍窄高）。
- 必须独立处理时才用 `orientation`；横屏垂直空间稀缺，首屏要能滚动可达，别用巨大 hero 撑满。
- 组合查询区分输入方式：`(min-width: …) and (pointer: coarse)` → 增大目标、去掉对 hover 的依赖。

## 11. 多视口测试方法（不能只看一个宽度）

1. 定代表性视口：320（极窄）、390（iPhone）、768（平板竖）、1024、1280、1440（桌面）。
2. 用 `playwright-cli` 逐尺寸截图与断言：`npx playwright codegen --viewport-size=390,844` 生成移动用例，`playwright-cli screenshot --filename=home-390.png` 在设定视口截图。
3. 每尺寸检查：**无横向滚动**（`scrollWidth <= clientWidth`）、无重叠/截断、触控目标达标、抽屉/折叠可开合。
4. 与设计稿**并排比对**（见 `screenshot-to-ui`）；把**断点前后 ±1px**（361/767/1023）也纳入，边界 bug 常在此暴露，并用**真实设备/模拟器**验证 `dvh`、安全区、软键盘（桌面缩放不能替代）。

## 反模式

- 只对**单一断点**写死布局，其余宽度靠运气。
- 按**设备名**（iPhone/iPad）而非**内容**设断点，或用 UA 检测代替 CSS。
- 直接拿 `vw` 设字号/间距而不 `clamp()` → 极端宽度下过大或过小。
- 组件用 `@media` 却放在可变宽容器里（应改 `@container`）。
- 组件用 `@media` 却放在可变宽容器里（应改 `@container`）；缺 `max-width` 导致横向滚动；图片无尺寸导致 CLS。
- 只测一个桌面宽度就宣称「响应式完成」。

## 验收清单

- [ ] 断点来自 `design-tokens`，数量最少且有理由
- [ ] 基础样式 = 移动端，`min-width` 单调增强
- [ ] 弹性（`min()`/`clamp()`/`auto-fit`）优先，固定宽度仅在有理由处
- [ ] 组件级自适应用 `@container`
- [ ] 代表性视口下**均无横向滚动、无重叠**
- [ ] 图片有 `srcset/sizes`、显式尺寸、合理 `loading`
- [ ] 触控目标 ≥44px；安全区与 `dvh` 已处理；横竖屏仍成立
- [ ] 键盘可达、焦点可见（见 `web-design-guidelines`）

## 相关技能

- `css-layout-and-box-model`：Flex/Grid、包含块、溢出的底层机制。
- `design-tokens`：断点、间距、字号 token 的唯一来源。
- `tailwindcss` / `shadcn`：断点与容器查询的类名落地。
- `screenshot-to-ui`：从设计稿补全各视口状态。
- `web-design-guidelines`：触控、安全区、a11y 审查。
- `playwright-cli`：多尺寸截图与断言。
