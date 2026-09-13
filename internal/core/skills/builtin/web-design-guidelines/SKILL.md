---
name: web-design-guidelines
description: >-
  Use when 被要求 "review my UI"、"check accessibility"、"audit design"、"review UX" 或对照最佳实践检查站点，按 Web Interface Guidelines 从无障碍、焦点、表单、动效、排版、性能、导航状态等维度审查 UI 代码并输出 file:line 形式的精简发现。
---

# Web Interface Guidelines 审查

对指定文件做 Web 界面规范审查，输出简洁、高信噪比的 `file:line` 发现。

## 工作方式

1. 每次审查前先抓取最新规则源：
   `https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md`
2. 读取用户给出的文件或 pattern（未指定就询问）。
3. 逐条对照下方规则（以抓取到的最新版为准，本文件是稳定基线）。
4. 按 `file:line` 格式输出。

> 这是评审类技能，与 `code-review-and-quality` 的评审流程配合使用；涉及表单、鉴权、用户输入的处理时联动 `security-and-hardening`。

## Accessibility

- 纯图标按钮需要 `aria-label`；表单控件需要 `<label>` 或 `aria-label`
- 交互元素要有键盘处理（`onKeyDown` / `onKeyUp`）
- 动作用 `<button>`，导航用 `<a>` / `<Link>`，不要用可点击的 `<div>`
- 图片需要 `alt`（装饰性用 `alt=""`）；装饰图标加 `aria-hidden="true"`
- 异步更新（toast、校验）需要 `aria-live="polite"`
- 优先语义 HTML（`button` / `a` / `nav` / `header`），再考虑 ARIA
- 标题层级连续（`h1` → `h2` → `h3`），主内容提供 skip link
- 标题锚点加 `scroll-margin-top`；有意义的媒体提供 caption / transcript
- 媒体控件支持键盘；装饰性媒体对辅助技术隐藏

## Focus States

- 交互元素有可见焦点（`focus-visible:ring-*` 或等价）
- 绝不在无焦点替代的情况下 `outline-none` / `outline: none`
- 用 `:focus-visible` 而非 `:focus`（避免点击时出现焦点环）
- 复合控件用 `:focus-within` 统一焦点
- sticky header / footer / overlay 不得遮住获焦元素

## Forms

- 输入框要有 `autocomplete` 与有意义的 `name`
- 使用正确的 `type`（`email` / `tel` / `url` / `number`）与 `inputmode`
- 绝不阻止粘贴（`onPaste` + `preventDefault`）
- label 可点击（`htmlFor` 或包裹控件）
- 邮箱 / 验证码 / 用户名关闭拼写检查（`spellCheck={false}`）
- checkbox / radio：label 与控件共享单一命中区，无死区
- 请求开始前提交按钮保持可用，进行中显示 spinner
- 错误就近显示在字段旁，提交时聚焦第一个错误
- placeholder 以 `…` 结尾并给出示例格式；非鉴权字段设 `autocomplete="off"`
- 有未保存改动时离开前警告（`beforeunload` 或路由守卫）

## Animation

- 遵守 `prefers-reduced-motion`（提供减弱版或直接禁用）
- 只动画 `transform` / `opacity`
- 不要 `transition: all`，显式列出属性
- 设置正确的 `transform-origin`；SVG 变换放在包装层并设 `transform-box: fill-box; transform-origin: center`
- 动画可被用户输入打断
- 超过 5 秒的自动播放动效需提供暂停 / 停止 / 隐藏控件
- 静音装饰性循环动效在 reduced-motion 下必须停止

## Typography

- 用 `…` 而非 `...`；用弯引号 `“ ”` 而非直引号
- 不断行空格：`10 MB`、`⌘ K`、品牌名
- 加载态以 `…` 结尾：`Loading…`、`Saving…`
- 数字列 / 对比用 `font-variant-numeric: tabular-nums`
- 标题用 `text-wrap: balance` 或 `text-pretty` 防止孤行

## Content Handling

- 文本容器处理长内容：`truncate`、`line-clamp-*` 或 `break-words`
- flex 子项加 `min-w-0` 以允许截断
- 处理空状态，不要为空字符串 / 空数组渲染出坏 UI
- 预判用户内容的短、中、超长输入

## Images

- 图片需要显式 `width` 与 `height`（防止 CLS）
- 首屏以下图片 `loading="lazy"`；首屏关键图 `priority` 或 `fetchpriority="high"`

## Performance

- 大列表（> 50 项）虚拟化（`virtua`、`content-visibility: auto`）
- 渲染期不做布局读取（`getBoundingClientRect`、`offsetHeight`、`offsetWidth`、`scrollTop`）
- 批量读写 DOM，避免交错
- 优先非受控输入；受控输入每次按键的开销要低
- 为 CDN / 资源域名加 `preconnect`
- 关键字体用 `font-display: swap`
- 优先 `<video>` 而非动图 GIF，并提供静态替代
- 短的非必要循环：Safari H.264 MP4、`prefers-reduced-motion` 媒体条件、静态回退

## Navigation & State

- URL 反映状态：筛选、tab、分页、展开面板进 query params
- 链接用 `<a>` / `<Link>`（支持 Cmd/Ctrl + 点击、中键）
- 有状态 UI 可深链（用了 `useState` 就考虑用 nuqs 等做 URL 同步）
- 破坏性操作需要确认弹窗或撤销窗口，绝不立即执行

## Touch & Interaction

- `touch-action: manipulation`（消除双击缩放延迟）
- 有意识地设置 `-webkit-tap-highlight-color`
- 模态 / 抽屉 / sheet 内 `overscroll-behavior: contain`
- 拖拽时禁用文本选择，被拖元素设 `inert`
- 拖拽 / 滑动 / 捏合 / 路径手势需要点击与键盘替代（除非是核心功能）
- `autoFocus` 慎用：仅桌面端、单一首要输入；移动端避免

## Safe Areas & Layout

- 全出血布局处理刘海：`env(safe-area-inset-*)`
- 避免多余滚动条：容器 `overflow-x-hidden`，修复内容溢出
- 布局优先 flex / grid，而非 JS 测量

## Dark Mode & Theming

- 暗色主题在 `<html>` 设 `color-scheme: dark`（修正滚动条与输入控件）
- `theme-color` 与页面背景一致
- 原生 `<select>` 显式设置 `background-color` 与 `color`（Windows 暗色模式）

## Locale & i18n

- 日期时间用 `Intl.DateTimeFormat`，数字 / 货币用 `Intl.NumberFormat`，不要硬编码格式
- 语言通过 `Accept-Language` / `navigator.languages` 检测，不要用 IP
- 品牌名、代码 token、标识符用 `translate="no"` 包裹，防止自动翻译破坏

## Hydration Safety

- 带 `value` 的输入需要 `onChange`（非受控用 `defaultValue`）
- 日期 / 时间渲染防止 SSR 与客户端不一致
- `suppressHydrationWarning` 只在确有必要处使用

## Hover & Interactive States

- 按钮 / 链接需要 `hover:` 视觉反馈
- hover / active / focus 的对比度应高于静止态

## Content & Copy

- 主动语态："Install the CLI" 而非 "The CLI will be installed"
- 标题 / 按钮用 Title Case；计数用阿拉伯数字（"8 deployments"）
- 按钮文案具体："Save API Key" 而非 "Continue"
- 错误信息给出修复 / 下一步，而非只有问题
- 用第二人称，避免第一人称；空间受限时用 `&` 代替 "and"

## Anti-patterns（直接标记）

- `user-scalable=no` 或 `maximum-scale=1` 禁用缩放
- `onPaste` + `preventDefault`；`transition: all`
- `outline-none` 且无 `focus-visible` 替代
- 内联 `onClick` 做导航且不用 `<a>`；`<div>` / `<span>` 挂 click（应为 `<button>`）
- 图片无尺寸；大数组 `.map()` 不虚拟化
- 表单输入无 label；图标按钮无 `aria-label`
- 硬编码日期 / 数字格式（用 `Intl.*`）
- 无明确理由的 `autoFocus`；动图 GIF 可用压缩视频替代
- 仅手势操作且无点击 / 键盘替代

## 输出格式

按文件分组，使用 `file:line`（VS Code 可点击），发现精简，问题 + 位置即可，除非修复不直观否则不解释，不要前言。

```text
# src/Button.tsx
src/Button.tsx:42 - icon button missing aria-label
src/Button.tsx:18 - input lacks label
src/Button.tsx:55 - animation missing prefers-reduced-motion
src/Button.tsx:67 - transition: all → list properties

# src/Modal.tsx
src/Modal.tsx:12 - missing overscroll-behavior: contain
src/Modal.tsx:34 - "..." → "…"

# src/Card.tsx
✓ pass
```
