---
name: tailwindcss
description: >-
  Use when 用 Tailwind CSS v4 以 utility class 构建界面：响应式布局、间距、排版、配色、暗色模式、group/peer、任意值、动画，或在 CSS 中用 @theme 定义设计 token；也适用于从 v3 迁移、配置构建插件，以及理解为何某些动态 class 不生效。
---

# Tailwind CSS v4 — Utility-First

用组合式 utility class 直接构建界面，不写自定义 CSS；按需 tree-shake，产物通常 < 10KB gzip。

## 安装与接入（v4）

```bash
npm install tailwindcss @tailwindcss/vite
```

```ts
// vite.config.ts
import tailwindcss from "@tailwindcss/vite";
export default { plugins: [tailwindcss()] };
```

```css
/* 全局 CSS：一行引入 */
@import "tailwindcss";
```

> v4 不再需要 `tailwind.config.js`，也**不要**在 PostCSS 里挂 `tailwindcss` 插件（用 `@tailwindcss/postcss`）。配置改为在 CSS 中写。

## 布局与响应式

```tsx
<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6 p-4">
  {products.map((p) => (
    <div key={p.id} className="group rounded-2xl border border-gray-100 shadow-sm transition-all duration-300 hover:shadow-lg">
      <div className="aspect-[4/3] overflow-hidden">
        <img src={p.image} alt={p.name}
             className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105" />
      </div>
      <div className="p-4">
        <h3 className="truncate font-semibold">{p.name}</h3>
        <p className="line-clamp-2 text-sm text-gray-500">{p.description}</p>
      </div>
    </div>
  ))}
</div>
```

- 断点 mobile-first：`sm:640` `md:768` `lg:1024` `xl:1280` `2xl:1536`；容器常用 `mx-auto max-w-7xl`。
- `group-hover:` 由父级触发、`peer-*:` 由兄弟触发；`group` / `peer` 标记父 / 兄元素。
- 宽高相等用 `size-*`；一行显示省略用 `truncate`，多行用 `line-clamp-N`。

## 设计 token：@theme

v4 用 CSS 的 `@theme` 定义 token，Tailwind 直接读取并生成对应 utility（无需 JS 配置）：

```css
@import "tailwindcss";

@theme {
  --color-brand-500: #3b82f6;
  --color-brand-600: #2563eb;
  --font-family-sans: "Inter", system-ui, sans-serif;
  --radius-lg: 0.75rem;
  --radius-xl: 1rem;
}
```

定义 `--color-brand-500` 后即可用 `bg-brand-500` / `text-brand-500`。自定义 keyframes 也放这里（见下）。

## 暗色模式

```tsx
<div className="bg-white text-gray-900 dark:bg-gray-800 dark:text-white">...</div>
```

- 通过 `dark:` 前缀给变体；class 策略下由根元素 `.dark` 切换。
- 与 shadcn/ui 搭配时优先用语义 token（`bg-background`、`text-muted-foreground`），由它统一处理明暗，**不要手写 `dark:` 颜色覆盖**（见 `shadcn`）。

## 任意值与主题

```tsx
<div className="w-[137px] text-[#1a2b3c] grid-cols-[1fr_2fr]" />
```

一次性尺寸 / 颜色用方括号；可复用的颜色、半径、字体一律进 `@theme`，不要到处散落 magic value。

## 动画

内置 4 个：

```tsx
<div className="animate-spin" />    {/* loading */}
<div className="animate-ping" />    {/* 通知点 */}
<div className="animate-pulse" />   {/* 骨架 */}
<div className="animate-bounce" />  {/* 滚动提示 */}
```

`tailwindcss-animate`（shadcn overlay 动效的来源）：

```tsx
<div className="animate-in fade-in slide-in-from-bottom-4 duration-500" />
<div className="animate-out fade-out slide-out-to-top-4 duration-300" />
{items.map((it, i) => (
  <div key={it.id}
       className="animate-in fade-in slide-in-from-bottom-2 duration-300 fill-mode-backwards"
       style={{ animationDelay: `${i * 100}ms` }}>{it.name}</div>
))}
```

`fill-mode-backwards` 防止延迟期间闪现终态。自定义动画（v4）：

```css
@theme { --animate-shimmer: shimmer 2s infinite linear; }
@keyframes shimmer { 0% { background-position: -200% 0; } 100% { background-position: 200% 0; } }
```

```tsx
<div className="h-4 w-48 rounded bg-gradient-to-r from-gray-200 via-gray-100 to-gray-200 bg-[length:200%_100%] animate-shimmer" />
```

## cn() 条件类名

```ts
import { clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export const cn = (...inputs: ClassValue[]) => twMerge(clsx(inputs));
```

```tsx
<div className={cn("flex items-center rounded-md", isActive && "bg-primary text-primary-foreground")} />
```

`twMerge` 解决同类 utility 冲突（后写的生效），避免字符串拼接导致样式打架。

## 陷阱

- **动态拼接的 class 不会被扫描**：`` `text-${color}-500` `` 不生成样式；用完整字符串、映射表或 CSS 变量。
- v4 移除 JS 配置：`theme.extend` 等迁移到 `@theme`；第三方插件确认支持 v4。
- 不要用 `space-y-*`（shadcn 规则），用 `flex flex-col gap-*` / `grid gap-*`。
- `@apply` 尽量少用，会绕过 utility 的按需与可读性；优先抽成组件。
- 动效只动画 `transform` / `opacity`；配合 `motion-reduce:animate-none` 尊重 `prefers-reduced-motion`。
- 重复颜色 / 圆角用 token，别散落 `#hex`，否则主题无法统一替换。

相关技能：组件与语义 token 见 `shadcn`；视觉方向与反 AI 套路见 `design-taste-frontend`、`web-design-guidelines`；React 渲染性能见 `vercel-react-best-practices`。
