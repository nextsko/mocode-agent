---
name: css-layout-and-box-model
description: >-
  Use when 需要精确实现或排查布局与尺寸：盒模型（content/padding/border/margin）、盒尺寸计算、
  包含块、常规流、Flex/Grid 对齐、定位与层叠、溢出与滚动、逻辑属性、响应式与容器查询，
  以及「为什么这个元素撑不开 / 溢出 / 居中不了 / 高度塌陷」等疑难。
---

# CSS 布局与盒子模型

一句话：把「尺寸从哪来、位置由谁定」讲清楚——多数布局 bug 都源于对**盒子尺寸**与**包含块/常规流**的误解。

## 1. 盒子模型

```
┌─ margin ────────────────────────────┐
│ ┌─ border ────────────────────────┐ │
│ │ ┌─ padding ───────────────────┐ │ │
│ │ │        content              │ │ │
│ │ └─────────────────────────────┘ │ │
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

- **content-box**（默认）：`width` = 内容宽，实际占位 = `width + padding + border`。
- **border-box**：`width` 含 padding+border（Tailwind / 现代 reset 默认）。
- 用 `box-sizing: border-box` 全局统一，避免「加了 padding 就溢出」。
- `height` 默认由内容撑开；`min-height: 100%` / `dvh` 处理满屏。

## 2. 尺寸来源

- **内在尺寸**：内容决定（文本、图片）。
- **外在尺寸**：显式 `width/height`、`flex-basis`、Grid 轨道。
- 关键函数：`min()/max()/clamp()`、`fit-content`、`min-content`、`max-content`。
- `aspect-ratio` 固定比例；图片用 `object-fit`。

## 3. 包含块与常规流（Normal Flow）

- **包含块**决定百分比尺寸与绝对定位的参照：
  - 静态/相对定位元素 → 最近**块容器**的 content box。
  - `position: absolute` 元素 → 最近**非 static** 祖先的 padding box。
  - `position: fixed` → 视口（除非有 transform/filter 祖先）。
- 块级垂直排列；行内沿文本书写方向排列。`display` 改变参与方式。
- **BFC**：`overflow: hidden/auto`、`display: flow-root`、`float`、flex/grid 项会创建——用于清除浮动、阻止 margin 塌陷。

## 4. 外边距塌陷（margin collapse）

- 相邻块级元素的**垂直** margin 会合并取较大者；父子间也可能塌陷。
- 修法：父加 `overflow: hidden`/`display: flow-root`/padding/border，或改用 **flex/grid + gap**。

## 5. Flex（一维）

- 容器：`display:flex` + `flex-direction` + `flex-wrap` + `justify-content`（主轴）+ `align-items`（交叉轴）+ `align-content`（多行）+ `gap`。
- 项目：`flex-grow/shrink/basis`；简写 `flex: 1`（= `1 1 0%`）。
- 常见坑：`min-width: auto` 使 flex 项不收缩 → 设 `min-width: 0`（或 `min-height: 0`）才能截断/滚动。
- 居中：`display:flex; place-items:center`（或 `justify-content`+`align-items`）。

## 6. Grid（二维）

- `grid-template-columns: repeat(auto-fit, minmax(240px, 1fr))` 做**自适应卡片**。
- `gap` 行列间距；`grid-template-areas` 表达版式；`grid-auto-flow`。
- 子项用 `grid-column/row: span N`；`place-self` 单独对齐。
- 命名线与 `subgrid`（继承父轨道）。

## 7. 定位与层叠

- `relative`（占位、作绝对定位参照）/ `absolute`（脱离流）/ `fixed`（视口）/ `sticky`（滚动阈值吸附）。
- `inset` 简写、`translate` 优于 `left/top` 做动画。
- **层叠上下文**由 `position+z-index`、`transform`、`opacity<1`、`filter`、`isolation` 等创建；`z-index` 只在同一上下文内比较。

## 8. 溢出与滚动

- `overflow: auto/hidden/scroll`；`overscroll-behavior` 防穿透。
- 滚动容器要 `min-height: 0`（flex 子项）才能滚动。
- `scroll-margin`/`scroll-snap` 处理锚点与轮播。
- 自定义滚动条 `::-webkit-scrollbar` / `scrollbar-width`。

## 9. 逻辑属性（国际化）

- 用 `margin-inline` / `padding-block` / `inset-inline-start` 替代物理方向，天然支持 RTL。
- Tailwind v4 的 `ps-*`/`pe-*`/`ms-*`/`me-*` 即逻辑间距。

## 10. 响应式

- 移动优先：`@media (min-width: …)`；断点由设计定（常见 640/768/1024/1280）。
- **容器查询** `@container`：组件按自身宽度自适应，优于全局视口断点。
- 弹性优先用 `clamp()`、`minmax()`、`auto-fit`，少写死断点。

## 排错清单

| 症状 | 常见根因 |
|------|----------|
| 元素撑不宽/不收缩 | flex 项 `min-width: auto` → 设 `min-width: 0` |
| 加了 padding 就溢出 | 未用 `border-box` |
| 子元素撑不开/高度塌陷 | 父无确定高度 / 浮动未清 / margin 塌陷 |
| 居中不了 | 用了 `margin:auto` 但父无固定宽 / 主轴交叉轴搞混 |
| 滚动条不出现 | 缺 `min-height:0` 或 `overflow` 设错 |
| `z-index` 不生效 | 不在同一层叠上下文 / 未定位 |
| sticky 失效 | 父级 `overflow: hidden` 或未设坐标阈值 |

## 相关技能

- `screenshot-to-ui`：从截图推导盒子模型与布局的方法学。
- `design-tokens`：间距 / 尺寸 token。
- `web-design-guidelines`：可用性与 a11y。
