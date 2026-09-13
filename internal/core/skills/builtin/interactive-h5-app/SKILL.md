---
name: interactive-h5-app
description: >-
  Use when 要把静态 / 高保真 HTML 原型推进成零依赖、浏览器直接打开即可玩的移动端 H5 单页
  应用，且要求「每个组件都真实可交互、不要虚拟摆设」。涵盖三文件拆分、单一 state 与
  localStorage 持久化、分区渲染、事件委托、弹层与 toast、种子数据与无头冒烟测试的完整工作流。
---

# Interactive H5 App — 静态原型 → 全交互应用

场景：用户给一个静态/高保真 HTML 原型，要求「全面推进、每个组件都真实可交互」，交付**零依赖、浏览器直接打开可玩**的移动端 H5 / 单页应用。

相关：`design-tokens`（CSS 变量 + `[data-theme]` 双主题）、`css-layout-and-box-model`（flex 等高、行内元素换行坑）、`playwright-cli`（真实浏览器验收）、`screenshot-to-ui`（原型还原）。

## 文件结构

拆三文件，拒绝单文件巨石：

- `index.html` — 静态骨架：状态栏、视图容器、标签栏、FAB、`#overlay-root`、`#toast`。
- `styles.css` — 设计令牌（CSS 变量 + `[data-theme]` 双主题）+ 组件样式。
- `app.js` — 全部逻辑：`'use strict'` + ES5 风格 `var`，兼容性最好。

## 核心架构（app.js）

1. **单一 state + 持久化**
   `state = Object.assign(defaults, loadSaved() || seedState())`。
   `persist()` 只存**数据子集**（如 tasks/theme/notify/profile）；视图、筛选、搜索为瞬态不存。
   `localStorage` 全部 `try/catch`（`file://` 下可能被禁用）。

2. **数据模型解耦**
   实体（`tasks`）与行为记录（`completions`）分开；统计页完全由行为记录驱动，勾选/取消时同步增删记录。这样 KPI、图表、连续天数都从**同一事实源**计算。

3. **分区渲染**
   `renderHome / renderTasks / renderStats / renderMe / renderOverlay` 只写各自的动态子容器（`innerHTML` / `textContent`）。
   **静态输入框（如搜索框）绝不重建**——否则每次重渲染都丢焦点。

4. **事件委托**
   `document` 上单个 `click` 监听，`e.target.closest('[data-action]')` 分发到 `handleAction`。
   键盘 `Enter`/`Space` 对非 `BUTTON`/`INPUT` 的 `[data-action]` 元素手动 `click()`；`Escape` 关闭弹层。

5. **弹层（overlay）**
   `state.overlay` 描述 `{kind:'sheet'|'modal', mode, id}`，`renderOverlay` 统一渲染。
   `backdrop` 单独绑 `click`，且仅在 `e.target === backdrop` 时关闭。
   打开后用 `requestAnimationFrame` 聚焦第一个 input。

6. **toast**
   直接 DOM 操作 + `setTimeout`，**不进 state**，避免把瞬态 UI 卷进重渲染复杂度。

## 种子数据

- 用 `new Date()` **运行时计算**（今天、本周一、本周），**禁止硬编码日期/时间戳**。
- 让统计页开箱即有数据：为本周已过的几天生成完成记录。

## 验证（不要省）

1. `node --check app.js` 语法校验。
2. **无头冒烟测试**：在系统 `temp` 目录（不进项目目录）写临时测试文件，用 `Proxy` 做最小 DOM 桩：
   - `querySelector` 返回桩、`querySelectorAll` 返回 `[]`；
   - `localStorage` / `requestAnimationFrame` / `setInterval` 打桩；
   - `eval(appSource + 测试代码)` 在同一作用域访问内部函数；断言种子数据、勾选增减统计、增删实体、各 `render*` 不抛错。
3. 需要真实渲染/交互时用 `playwright-cli` 打开页面做端到端点击验收。

## 易踩的坑

- **按钮内的行内元素（span）不换行**：`.title / .meta / .name` 等要显式 `display:block`；flex 子项会自动块化，不用再设。
- **flex 等高列 + 内部百分比高度**：父容器用 `align-items:stretch`（**不是** `flex-end`），柱体用 `flex:1; min-height:0`，**不要** `height:100%`，否则溢出。
- **CSS 自定义属性平滑过渡**：需 `@property --p { syntax: '<number>'; ... }` 注册，否则数值动不起来。
- **键盘可达**：chart/列表等可点区域给 `role="button" tabindex="0"`。
- **重渲染丢状态**：把输入框、滚动位置等留在静态骨架里，只刷新数据容器。

## 反模式

- 单文件巨石；用 JS 全量 `innerHTML` 重建整页（丢焦点、丢状态、事件绑定重复）。
- 硬编码日期/时间；把瞬态 UI（toast、弹层）塞进持久化 state。
- 只做「视觉可交互」不写冒烟测试；改完不跑 `node --check`。
