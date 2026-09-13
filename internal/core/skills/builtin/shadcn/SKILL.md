---
name: shadcn
description: >-
  Use when 使用 shadcn/ui 或任何含 components.json 的项目添加、搜索、修复、更新 UI 组件与设计系统，或处理 registry、preset（--preset / preset code）、CLI（info/docs/search/add/diff）以及基于 shadcn 原语的 chat 界面。也适用于 "shadcn init"、"create an app with --preset"、"switch to --preset"。仅写 Tailwind 样式时见 tailwindcss。
---

# shadcn/ui

shadcn/ui 不是运行时依赖，而是把组件**源码**通过 CLI 复制进项目、用 registry 分发的框架。

> 所有 CLI 命令使用项目 `packageManager` 对应的 runner：`npx shadcn@latest`、`pnpm dlx shadcn@latest`、`bunx --bun shadcn@latest`。下文以 `npx` 为例。

## 项目上下文

```bash
npx shadcn@latest info --json   # 配置 + 已安装组件
```

关键字段（决策前必读，禁止硬编码）：

| 字段 | 用途 |
|---|---|
| `aliases` | import 前缀（如 `@/`、`~/`） |
| `isRSC` | 为 `true` 时，含 `useState`/事件/浏览器 API 的文件需 `"use client"` |
| `tailwindVersion` | `v4` → `@theme inline`；`v3` → `tailwind.config.js` |
| `tailwindCssFile` | 全局 CSS 变量文件，只改它，不新建 |
| `base` | 原语库 `radix` 或 `base`（决定 `asChild` vs `render`） |
| `iconLibrary` | 图标包，如 `lucide` → `lucide-react`，勿假定 |
| `resolvedPaths` | 组件/utils/hooks 的落盘目录 |
| `framework` / `packageManager` / `preset` | 路由约定、安装命令、preset 状态 |

## 四大原则

1. 先用已有组件：写自定义 UI 前用 `npx shadcn@latest search` 查 registry（含社区 registry）。
2. 组合而非重造：Settings = Tabs + Card + 表单控件；Dashboard = Sidebar + Card + Chart + Table。
3. 先内置 variant 再自定义样式：`variant="outline"`、`size="sm"`。
4. 用语义色：`bg-primary`、`text-muted-foreground`，禁止 `bg-blue-500`。

## 硬性规则（Incorrect → Correct）

### 样式与 Tailwind

- `className` 只做布局（`max-w-md`、`mx-auto`），不改组件颜色/字体。改外观顺序：内置 variant → 语义 token → CSS 变量。
- 禁止 `space-x-*` / `space-y-*`，用 `flex gap-*`；竖向栈 `flex flex-col gap-*`。
- 宽高相等用 `size-*`（`size-10` 而非 `w-10 h-10`）。
- 截断用 `truncate`，不要 `overflow-hidden text-ellipsis whitespace-nowrap`。
- 禁止手写 `dark:` 颜色覆盖，语义 token 自动处理明暗。
- 条件类名用 `cn()`，不要模板字符串三元。
- 禁止给 overlay 组件（Dialog/Sheet/Popover/DropdownMenu/Tooltip/HoverCard）加 `z-*`。
- 状态色（涨跌/在线）用 `Badge` variant 或 `text-destructive`，不用 `text-emerald-600`。
- loading 文字用 `shimmer`；滚动边缘淡化用 `scroll-fade*`，不要手写 keyframes/mask。

```tsx
// ❌
<div className="space-y-4 bg-blue-500">
// ✅
<div className="flex flex-col gap-4 bg-primary text-primary-foreground">
```

### 表单与输入

- 表单用 `FieldGroup` + `Field`，绝不用 `div` + `space-y-*`；设置页用 `Field orientation="horizontal"`。
- `InputGroup` 内只能用 `InputGroupInput` / `InputGroupTextarea`，不能塞原生 `Input`。
- 输入框内按钮用 `InputGroup` + `InputGroupAddon`，不要 `relative` + 绝对定位。
- 2–7 个选项的切换用 `ToggleGroup` + `ToggleGroupItem`，不要手动循环 `Button`。
- 相关 checkbox/radio 用 `FieldSet` + `FieldLegend` 分组。
- 校验态需两处属性：`Field data-invalid` + 控件 `aria-invalid`；禁用态 `Field data-disabled` + 控件 `disabled`。

```tsx
<FieldGroup>
  <Field>
    <FieldLabel htmlFor="email">Email</FieldLabel>
    <Input id="email" type="email" />
  </Field>
</FieldGroup>
```

控件选择：文本 → `Input`；预定义下拉 → `Select`；可搜索 → `Combobox`；无 JS → `native-select`；布尔 → `Switch`（设置）/ `Checkbox`（表单）；单选 → `RadioGroup`；验证码 → `InputOTP`；多行 → `Textarea`。

### 组件结构与原语

- Item 必须在 Group 内：`SelectItem`→`SelectGroup`、`DropdownMenuItem`→`DropdownMenuGroup`、`CommandItem`→`CommandGroup`。
- 自定义 trigger：Radix 用 `asChild`，Base 用 `render={<Button />}`；Base 下渲染非 button 元素加 `nativeButton={false}`。
- `Dialog`/`Sheet`/`Drawer` 必须有 Title（`DialogTitle` 等），隐藏时 `className="sr-only"`。
- `Card` 用完整组合：`CardHeader`/`CardTitle`/`CardDescription`/`CardContent`/`CardFooter`。
- `Button` 没有 `isPending`/`isLoading`：用 `<Spinner data-icon="inline-start" />` + `disabled`。
- `TabsTrigger` 必须在 `TabsList` 内；`Avatar` 必须有 `AvatarFallback`。
- 用组件替代自定义标记：`Separator` 替 `<hr>`/`border-t`；`Skeleton` 替 `animate-pulse`；`Badge` 替手写 span；`Alert` 做 callout；`Empty` 做空状态。
- Toast 跟随 base：Base UI 用 `toast.add(...)`（`@/components/ui/toast`），Radix/Aria 用 `sonner` 的 `toast.success(...)`。

### 图标

- 用项目 `iconLibrary` 导入，勿假定 `lucide-react`。
- Button 内图标加 `data-icon="inline-start|inline-end"`，不加 `size-4` / `mr-2`。
- 图标以组件对象传递（`icon={CheckIcon}`），不要字符串 key 查表。

### Chat 与消息

- 对话用原语组合，禁止手搓 bubble `div` 或裸滚动容器。
- 滚动线程用 `MessageScroller`，嵌套顺序固定：`MessageScrollerProvider → MessageScroller → MessageScrollerViewport → MessageScrollerContent → MessageScrollerItem`。
- streaming 跟随、锚定（`scrollAnchor`）、jump-to-latest（`MessageScrollerButton`）都是内置的，不要写 `useStickToBottom` / `ResizeObserver`。
- 消息行用 `Message`（`align="end"` 为用户侧），表面用 `Bubble` + `BubbleContent`，附件用 `Attachment`（`state` 驱动上传态），系统提示/分隔用 `Marker`。
- 逃生舱：`useMessageScroller*` hooks（随 `@shadcn/react` 自动安装）。

安装：`npx shadcn@latest add message-scroller message bubble attachment marker`

## 组件选择

| 需求 | 组件 |
|---|---|
| 操作按钮 | `Button` + 合适 variant |
| 表单输入 | `Input` `Select` `Combobox` `Switch` `Checkbox` `RadioGroup` `Textarea` `InputOTP` `Slider` |
| 数据展示 | `Table` `Card` `Badge` `Avatar` |
| 导航 | `Sidebar` `NavigationMenu` `Breadcrumb` `Tabs` `Pagination` |
| 覆盖层 | `Dialog`（模态）`Sheet`（侧栏）`Drawer`（底部）`AlertDialog`（确认） |
| 反馈 | `Alert` `Progress` `Skeleton` `Spinner` + toast |
| 命令面板 | `Command` 放入 `Dialog` |
| 图表 | `Chart`（封装 Recharts） |
| 菜单/提示 | `DropdownMenu` `ContextMenu` `Tooltip` `HoverCard` `Popover` |

## 工作流

1. 取上下文：`info --json`（需要刷新时重跑）。
2. 查已装组件：先看 `components` 或列 `resolvedPaths.ui` 目录，不重复 `add`。
3. 找组件：`npx shadcn@latest search`；未安装项用 `view` 浏览。
4. 看文档与示例：`npx shadcn@latest docs <component>` 拿 URL 后抓取内容，**不要靠猜 API**。
5. 安装/更新：`add`；更新已装组件先 `--dry-run` 再 `--diff`。
6. 修第三方 registry 导入：`@bundui`/`@magicui` 等可能硬编码 `@/components/ui/...`，按项目 `aliases` 改写；图标按 `iconLibrary` 替换。
7. 审查新增文件：检查缺失子组件、导入、组合与上述规则。
8. registry 必须明确：用户没说 registry 就问，不要默认。
9. 切换 preset：先问 overwrite / partial / merge / skip。

## 更新组件

禁止手抓 GitHub 原始文件，一律走 CLI：

```bash
npx shadcn@latest add button --dry-run
npx shadcn@latest add button --diff button.tsx
```

无本地改动 → 可覆盖；有本地改动 → 读本地文件分析 diff 后保留本地改动再合并；`--overwrite` 必须用户明确同意。

## CLI 速查

```bash
npx shadcn@latest init --name my-app --preset base-nova
npx shadcn@latest apply a2r6bw --only theme,font
npx shadcn@latest preset decode a2r6bw   # 不要手工解码 preset code
npx shadcn@latest preset resolve --json
npx shadcn@latest add button card dialog
npx shadcn@latest add owner/repo/item
npx shadcn@latest search @shadcn -q "sidebar" -t ui
```

Named presets：`nova` `vega` `maia` `lyra` `mira` `luma`。Templates：`next` `vite` `start` `react-router` `astro`（支持 `--monorepo`）、`laravel`（不支持 monorepo）。

相关技能：样式细节见 `tailwindcss`；视觉方向见 `design-taste-frontend` 与 `web-design-guidelines`；组合与性能见 `vercel-composition-patterns`、`vercel-react-best-practices`。
