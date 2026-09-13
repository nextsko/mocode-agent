---
name: screenshot-to-ui
description: >-
  Use when 拿到一张图片 / 设计稿 / 截图，要求「照它做出界面 / 1:1 复刻 / 还原这个页面 /
  做成一样」。给出从像素到实现的完整方法学：如何测量并推导盒子模型、布局系统、间距节奏、
  字体层级、色彩系统，如何做组件识别与状态补全，以及如何映射到 Tailwind/token 并做像素级验收。
---

# 截图 → UI 复刻方法学

一句话：**先测量、再建模、后实现、终验收**；绝不凭肉眼感觉直接写 CSS。

配套：`ui-replication`（fork/fix 两工作流 + playwright-cli 验收）、`css-layout-and-box-model`（布局详解）、`design-tokens`（token 体系）、`web-design-guidelines`（a11y）。

## 0. 准备

- 取**原图**（最高分辨率）；记录画布尺寸，判断是 @1x 还是 @2x。
- 建立**测量参照**：找一个已知尺寸反推比例（头像 40/48px、按钮高 36/40/44px、行高、状态栏）。
- 从像素换算到 CSS 像素：`css_px = 图上像素 ÷ 缩放倍率`。

## 1. 图层拆解（从上到下）

1. **画布**：宽度、背景色、是否有外边框 / 圆角 / 阴影。
2. **栅格与安全区**：内容最大宽度（1200 / 1440）、左右 gutter、列数与 `gap`（常见 12 列）。
3. **区块（section）**：导航 / 主视觉 / 内容区 / 侧栏 / 页脚——标出各自高度或占比。

## 2. 盒子模型测量（对每个元素）

推导 CSS 盒子 `margin / border / padding / content`：

- **外沿到外沿**的距离 = 相邻 margin 之和；**内沿到内容** = padding。
- 边框：1px hairline 还是声明式描边；**圆角半径**用圆角弧与边长比估算，归一到 4/6/8/12/16/full。
- 校验阶段用 `playwright-cli snapshot --boxes` / `eval getComputedStyle(...)` 取真实值。

## 3. 布局系统判定

- **维度**：一维（行或列）→ Flex；二维（行 × 列对齐）→ Grid。
- **主/交叉轴对齐**：`justify-content` / `align-items` / `place-*`。
- 元素间距一律用 **`gap`**，不要散写 margin。
- **定位**：`sticky`（吸顶导航）/`fixed`/`absolute`（浮层、徽标）。
- **溢出与滚动**：`overflow` 归属哪个容器。
- **层叠上下文**：`z-index` 分层（base < dropdown < modal < toast）。

## 4. 间距节奏（spacing scale）

- 用 **4/8pt 网格**：所有间距归一到 4 的倍数（4, 8, 12, 16, 24, 32, 40, 48, 64…）。
- 量到的值取**最近档位**（如 18 → 16 或 20），并让整图**节奏统一**。
- 定义为 **spacing token**，禁止硬编码。

## 5. 字体层级（typography）

- 逐级量 **font-size**，验证是否成**模数比例**（如 1.25 / 1.333）。
- **line-height** = 行距 ÷ 字号（正文 1.4–1.6，标题 1.1–1.3）；可用两行基线间距反推。
- **字重**、**字距**（大标题常为负 tracking）、大小写与对齐方式。
- 响应式字号用 `clamp()` / token 表达。

## 6. 色彩系统

- 抽取主色 + 中性色阶：`bg / surface / border / muted / fg / primary / danger`。
- 取色取**纯色区中心**，避免抗锯齿边缘偏差。
- 校验**对比度**（正文 ≥ 4.5:1，大字 ≥ 3:1），不达标就调整。
- **语义化命名**（`--color-fg`, `--color-primary`…），预留暗色主题。

## 7. 组件识别与变体

- 归并同类：按钮 / 输入 / 卡片 / 标签 / 导航项 → 建**组件清单 + 变体**（size × variant × state）。
- 按原子设计分层：atoms → molecules → organisms → pages。

## 8. 状态补全（图里没有但必须有）

- **交互态**：hover / focus-visible / active / disabled / loading。
- **数据态**：empty / error / skeleton。
- **响应式**：移动 / 平板 / 桌面断点下的重排（栅格坍缩、导航收起）。

## 9. 映射为代码

- Tailwind v4 + `@theme` token；颜色 / 间距 / 字号 / 圆角 / 阴影**全部走 token**。
- 顺序：先搭**布局骨架**（容器 / 栅格 / 区块）→ 再填组件 → 最后调状态与细节。

## 10. 验收（像素级）

- 固定视口截图与设计稿**并排比对**；用 `--boxes` / `eval` 逐项核对尺寸、间距、颜色、字号。
- 偏差清单**逐项清零**；同时保证 a11y（键盘可达、焦点可见、对比度达标）。

## 反模式（禁止）

- 凭肉眼直接写 CSS；硬编码 magic number；用 `margin` 拼 `gap`；忽略状态与响应式；只对单张图负责而不建 token / 组件体系；无条件 1:1 牺牲 a11y。
