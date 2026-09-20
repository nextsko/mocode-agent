# oh-my-pi TUI 主题系统 / 折叠展开 / 状态可视化 调研报告

> [!abstract] 概述
> 本报告深度调研 oh-my-pi 项目的三个 UI 横切关注点：**Theme 系统**、`border + state → 颜色/图标` 映射、**折叠/展开语义**（含时间格式、域提取、宽度自适应）。所有发现均直接对应到 oh-my-pi 的 6 个源文件（`theme.ts`、`output-block.ts`、`render-utils.ts`、`web/search/render.ts`、`tui/types.ts`、`status-line.ts`、`format.ts`）。报告末尾给出 **Go 移植骨架代码**（lipgloss + glamour + bubbles），可直接作为 mocode-agent 的 UI 子系统参考实现。

---

## 1. Theme 类型结构

### 1.1 `Theme` 接口核心方法（theme-class.ts）

`Theme` 是一个 class，构造时把所有 fg/bg 颜色预解析成 ANSI 转义码字符串（`#fgColors: Record<ThemeColor, string>`），调用 `fg(color, text)` 时直接拼接：

```typescript
// theme-class.ts (节选)
fg(color: ThemeColor, text: string): string {
  const ansi = this.#fgColors[color];
  if (!ansi) throw new Error(`Unknown theme color: ${color}`);
  return `${ansi}${text}\x1b[39m`; // Reset only foreground color
}

bg(color: ThemeBg, text: string): string {
  const ansi = this.#bgColors[color];
  if (!ansi) throw new Error(`Unknown theme background color: ${background}`);
  return `${ansi}${text}\x1b[49m`; // Reset only background color
}
```

**关键 API**：
- `fg(color, text)` — 两参数 API，输出 `ANSI(text)`，自动 `ESC[39m` 复位前景
- `fgResolved(color, text)` — 处理 foreground reset 嵌套（`fgResolved` 在 text 内的 `\x1b[39m` 后重新应用 ansi）
- `bgFill(color, text)` — bg 的 `bgFill` 版本，复位嵌套 `ESC[0m` / `ESC[49m`
- `fgOnBg(fg, bg, text)` — 已知背景时挑对比色
- `bold / italic / underline / strikethrough / inverse` — 委托给 chalk
- `getFgAnsi(color)` / `getBgAnsi(bg)` — 返回原始 ANSI（用于 syntax highlight 配色）

### 1.2 符号分类（SymbolMap）

Theme 构造时从 `SYMBOL_PRESETS[symbolPreset]` 加载符号表，提供类别访问器：

```typescript
get boxRound() {
  return {
    topLeft:    this.#symbols["boxRound.topLeft"],
    topRight:   this.#symbols["boxRound.topRight"],
    bottomLeft: this.#symbols["boxRound.bottomLeft"],
    bottomRight:this.#symbols["boxRound.bottomRight"],
    horizontal: this.#symbols["boxRound.horizontal"],
    vertical:   this.#symbols["boxRound.vertical"],
    // 注意：rounded box 的 junctions 复用 boxSharp.tee*
    cross:      this.#symbols["boxSharp.cross"],
    teeDown:    this.#symbols["boxSharp.teeDown"],
    teeUp:      this.#symbols["boxSharp.teeUp"],
    teeRight:   this.#symbols["boxSharp.teeRight"],
    teeLeft:    this.#symbols["boxSharp.teeLeft"],
  };
}

get tree() {
  return {
    branch:    this.#symbols["tree.branch"],     // ├─
    last:      this.#symbols["tree.last"],       // └─
    vertical:  this.#symbols["tree.vertical"],   // │
    horizontal:this.#symbols["tree.horizontal"], // ─
    hook:      this.#symbols["tree.hook"],       // └
  };
}

get sep() {
  return { dot: " · ", slash: " / ", pipe: " │ ", powerline: "▕", ... };
}

get icon() {
  return { model: "⬢", plan: "🗺", folder: "📁", agent: "👥", ... };
}
```

### 1.3 styledSymbol — 工具专属符号

```typescript
styledSymbol(key: SymbolKey, color: ThemeColor): string {
  return this.fg(color, this.#symbols[key]);
}
```

**用法**：`theme.styledSymbol("status.success", "success")` → `✔` 染色后输出。

### 1.4 颜色枚举（schema.ts）

`ThemeColor` 是 70+ 命名 token 的 union type，分组：

| 分组 | Token |
|---|---|
| 强调/边框 | `accent` `border` `borderAccent` `borderMuted` |
| 状态色 | `success` `error` `warning` `muted` `dim` `text` |
| Markdown | `mdHeading` `mdLink` `mdLinkUrl` `mdCode` `mdCodeBlock` `mdCodeBlockBorder` `mdQuote` `mdQuoteBorder` `mdHr` `mdListBullet` |
| Diff | `toolDiffAdded` `toolDiffRemoved` `toolDiffContext` |
| 工具输出 | `toolTitle` `toolOutput` |
| Syntax | `syntaxComment` `syntaxKeyword` `syntaxFunction` `syntaxVariable` `syntaxString` `syntaxNumber` `syntaxType` `syntaxOperator` `syntaxPunctuation` |
| Thinking | `thinkingOff` `thinkingMinimal` `thinkingLow` `thinkingMedium` `thinkingHigh` `thinkingXhigh` `thinkingMax` |
| Bash/Python | `bashMode` `pythonMode` |
| StatusLine | `statusLineSep` `statusLineModel` `statusLinePath` `statusLineGitClean` `statusLineGitDirty` `statusLineContext` `statusLineSpend` `statusLineStaged` `statusLineDirty` `statusLineUntracked` `statusLineOutput` `statusLineCost` `statusLineSubagents` |

`ThemeBg` 是另一组（`toolPendingBg` `toolSuccessBg` `toolErrorBg` `userMessageBg` `userMessageText` `customMessageBg` `customMessageText` `customMessageLabel` `selectedBg` `thinkingText` `statusLineBg` 等）。

### 1.5 符号预设

```typescript
export type SymbolPreset = "unicode" | "nerd" | "ascii";
export const SYMBOL_PRESETS: Record<SymbolPreset, SymbolMap> = { unicode, nerd, ascii };
```

ASCII 预设下大量 icon 变成 `[A]` `[M]` 等方括号缩写，保证无 Unicode 支持终端可用。

### 1.6 主题加载与切换（theme.ts）

```typescript
function detectTerminalBackground(): "dark" | "light" {
  // Tier 1: OSC 11 luminance（最权威）
  // Tier 2: COLORFGBG 环境变量
  // Tier 3: macOS 原生 appearance（仅 Zellij + darwin）
  return "dark"; // fallback
}

function getDefaultTheme(): string {
  return detectTerminalBackground() === "light" ? "light" : "dark";
}
```

- 全局单例：`export var theme: Theme`（module-level 变量）
- `initThemeSync()` 在第一帧渲染前同步初始化（避免闪屏）
- `setTheme(name)` / `previewTheme(name)`：异步加载 + 通知 watcher
- `getThemeEpoch()`：单调递增计数器，让 cached renderer 在主题切换后失效
- `notifyThemeChange({ephemeral})`：preview 模式仅触发重绘（不替换 native scrollback）
- `startThemeWatcher()`：仅对自定义主题启用 `fs.watch`，文件改动 100ms 防抖后重载

---

## 2. 状态可视化

### 2.1 State 类型（tui/types.ts）

```typescript
export type State = "pending" | "running" | "success" | "error" | "warning";
```

### 2.2 State → Border 颜色映射（output-block.ts）

```typescript
const borderColor: ThemeColor =
  options.borderColor ??
  (state === "error"    ? "error"
   : state === "warning" ? "warning"
   : state === "running" || state === "pending" ? "accent"
   : "dim"); // success 用 dim（灰色边框，弱化视觉）
```

**State → 背景色映射**（utils.ts）：

```typescript
export function getStateBgColor(state: State): ThemeBg {
  if (state === "success") return "toolSuccessBg";
  if (state === "error")   return "toolErrorBg";
  return "toolPendingBg"; // pending/running/warning
}
```

### 2.3 背景填充稳定（output-block.ts）

嵌套 SGR reset 会"打断"外部背景。`renderOutputBlock` 的 `bgFn` 用正则把 reset 重新拼回：

```typescript
const bgFn = (text: string) => {
  const stabilized = text
    .replace(/\x1b\[(?:0)?m/g, m => `${m}${bgAnsi}`)
    .replace(/\x1b\[49m/g, m => `${m}${bgAnsi}`);
  return `${bgAnsi}${stabilized}\x1b[49m`;
};
```

### 2.4 renderStatusLine 单一入口（status-line.ts）

所有工具的 header 都走这一个函数：

```typescript
export interface StatusLineOptions {
  icon?: ToolUIStatus;
  iconOverride?: string;    // 工具专属符号（"tool.webSearch"）
  spinnerFrame?: number;    // running 动画帧索引
  title: string;
  titleColor?: ThemeColor;  // 默认 "accent"
  description?: string;
  badge?: { label: string; color: ThemeColor };
  meta?: string[];
}

export function renderStatusLine(options: StatusLineOptions, theme: Theme): string {
  const icon = options.iconOverride ??
    (options.icon ? formatStatusIcon(options.icon, theme, options.spinnerFrame) : "");
  const title = theme.fg(options.titleColor ?? "accent", flattenForHeader(options.title));
  let line = icon ? `${icon} ${title}` : title;
  if (options.description) {
    line += `: ${theme.fg("muted", flattenForHeader(options.description))}`;
  }
  if (options.badge) {
    line += ` ${theme.fg(options.badge.color,
      `${theme.format.bracketLeft}${flattenForHeader(options.badge.label)}${theme.format.bracketRight}`)}`;
  }
  if (options.meta?.length) {
    line += ` ${theme.fg("dim", options.meta.map(flattenForHeader).join(theme.sep.dot))}`;
  }
  return line;
}
```

**关键点**：
- `flattenForHeader()` 把 `\r\n?|\n` 折叠成空格（防止 header 内嵌换行破坏 bordered block）
- icon 与 title 之间有空格，title 与 description 用 `: ` 分隔，meta 用 ` · `（`theme.sep.dot`）分隔

### 2.5 statusIcon 映射（render-utils.ts）

```typescript
export type ToolUIStatus = "success" | "done" | "error" | "warning"
                          | "info" | "pending" | "running" | "aborted";

export function formatStatusIcon(status: ToolUIStatus, theme: Theme, spinnerFrame?: number): string {
  switch (status) {
    case "success":  return theme.styledSymbol("status.success", "success");  // ✔
    case "done":     return theme.styledSymbol("status.done",    "success");  // •
    case "error":    return theme.styledSymbol("status.error",   "error");    // ✘
    case "warning":  return theme.styledSymbol("status.warning", "warning");  // ⚠
    case "info":     return theme.styledSymbol("status.info",    "accent");   // ⓘ
    case "pending":  return theme.styledSymbol("status.pending", "muted");    // ⏳
    case "running":
      if (spinnerFrame !== undefined) {
        const frames = theme.spinnerFrames; // 8-frame 旋转
        return frames[spinnerFrame % frames.length];
      }
      return theme.styledSymbol("status.running", "accent");
    case "aborted":  return theme.styledSymbol("status.aborted", "error");    // ⏹
  }
}
```

**三态映射一致**：`state → {color, icon, border}` 三者共用同一组 token（success/warning/error/running/pending），确保视觉一致。

### 2.6 meta 用 formatCount 格式化

`formatCount(label, n)` 来自 `@oh-my-pi/pi-utils/format.ts`：

```typescript
export function formatCount(label: string, count: number): string {
  return `${count} ${pluralize(label, count)}`;
}

export function pluralize(label: string, count: number): string {
  if (count === 1) return label;
  if (/(?:ch|sh|s|x|z)$/i.test(label)) return `${label}es`;  // box → boxes
  if (/[^aeiou]y$/i.test(label))     return `${label.slice(0, -1)}ies`; // entry → entries
  return `${label}s`;
}
```

web/search/render.ts 中用法：`meta: [formatCount("source", sourceCount)]` → `"3 sources"` / `"1 source"`。

---

## 3. 折叠/展开语义

### 3.1 全局开关：RenderResultOptions.expanded

```typescript
// extensibility/custom-tools/types.ts
export interface RenderResultOptions {
  expanded: boolean;     // 全局折叠/展开开关（ctrl+o 触发）
  isPartial: boolean;    // 流式渲染中
  spinnerFrame?: number; // running 动画帧索引
  argsComplete?: boolean;
  executionStarted?: boolean;
}
```

**注意**：`expanded` 由父组件持有（`getExpanded: () => boolean`），render 函数每次重渲时读最新值，而非闭包快照。这点对流式渲染非常关键。

### 3.2 MAX_COLLAPSED_ITEMS = 8

```typescript
// tools/render-utils.ts
export const PREVIEW_LIMITS = {
  COLLAPSED_LINES: 3,
  EXPANDED_LINES:  12,
  COLLAPSED_ITEMS: 8,
  OUTPUT_COLLAPSED: 3,
  OUTPUT_EXPANDED:  10,
  COMPUTER_CODE_COLLAPSED: 10,
  DIFF_COLLAPSED_HUNKS: 8,
  DIFF_COLLAPSED_LINES: 40,
} as const;
```

```typescript
// web/search/render.ts
const MAX_COLLAPSED_ITEMS = PREVIEW_LIMITS.COLLAPSED_ITEMS; // 8
```

### 3.3 formatExpandHint（render-utils.ts）

```typescript
const EXPAND_ACTION = "app.tools.expand";
const DEFAULT_EXPAND_KEY: KeyId = "ctrl+o";

export function expandKeyHint(): string {
  const keys = getKeybindings().getKeys(EXPAND_ACTION);
  return formatKeyHints(keys.length > 0 ? keys : [DEFAULT_EXPAND_KEY]);
}

export function formatExpandHint(theme: Theme, expanded?: boolean, hasMore?: boolean): string {
  if (expanded) return "";
  if (hasMore === false) return "";
  return theme.fg("dim", wrapBrackets(`${expandKeyHint()}: Expand`, theme));
}
```

输出样式：`[ctrl+o: Expand]`（使用 `theme.format.bracketLeft`/`bracketRight`）。

### 3.4 formatMoreItems（render-utils.ts）

```typescript
export function formatMoreItems(remaining: number, itemType: string): string {
  return `… ${remaining} more ${pluralize(itemType, remaining)}`;
}
```

输出：`… 5 more sources` / `… 1 more line` / `… 12 more warnings`。

### 3.5 树形列表 + 双截断模式（tree-list.ts）

`renderTreeList` 是通用折叠组件，**支持 head-truncate 和 tail-truncate**：

```typescript
export interface TreeListOptions<T> {
  items: T[];
  expanded?: boolean;
  maxCollapsed?: number;        // 最多显示 N 项
  maxCollapsedLines?: number;   // 总行数预算（用于流式滚动）
  itemType?: string;            // pluralize 类型
  truncateFrom?: "start" | "end"; // 默认 "end"
  trailingSummary?: string;     // 调用方自带摘要
  renderItem: (item, ctx: TreeContext) => string | string[];
}
```

```typescript
// 截断策略
if (truncateFrom === "start") {
  // 显示尾部 N 项（流式渲染常用，新事件在底部）
  const startCandidateIdx = Math.max(0, items.length - maxItems);
  for (let i = startCandidateIdx; i < items.length; i++) candidateIndices.push(i);
} else {
  // 默认 head 截断
  for (let i = 0; i < maxItems; i++) candidateIndices.push(i);
}
```

### 3.6 宽度自适应预算（web/search/render.ts）

```typescript
// 一行 source：title 链接 + meta 后缀（domain · age）
const lineBudget  = Math.max(24, width - 6);              // 减边框 + tree branch
const titleBudget = Math.max(12, lineBudget - Bun.stringWidth(metaSuffix));
const title       = theme.fg("accent", truncateToWidth(titleText, titleBudget));
```

---

## 4. 时间格式化

### 4.1 formatAge（utils/format.ts）

```typescript
export function formatAge(ageSeconds: number | null | undefined): string {
  if (!ageSeconds) return "";
  const mins   = Math.floor(ageSeconds / 60);
  const hours  = Math.floor(mins / 60);
  const days   = Math.floor(hours / 24);
  const weeks  = Math.floor(days / 7);
  const months = Math.floor(days / 30);

  if (months > 0) return `${months}mo ago`;
  if (weeks  > 0) return `${weeks}w ago`;
  if (days   > 0) return `${days}d ago`;
  if (hours  > 0) return `${hours}h ago`;
  if (mins   > 0) return `${mins}m ago`;
  return "just now";
}
```

**分桶策略**：mo（>30d） → w（>7d） → d（>24h） → h（>60min） → m（>60s） → `just now`。

### 4.2 fallback 到 publishedDate 字符串

`web/search/render.ts`：

```typescript
const age =
  formatAge(src.ageSeconds) ||
  (typeof src.publishedDate === "string" ? src.publishedDate : "");
```

**顺序**：先尝试 `ageSeconds` → 失败则用原始 ISO 日期字符串 → 都没有则空串。

### 4.3 formatDuration / formatNumber / formatBytes

```typescript
export function formatDuration(ms: number): string {
  // "123ms", "1.5s", "30m15s", "2h30m", "3d2h"
  if (ms < 1000) return `${Math.floor(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 3_600_000) { /* m + s */ }
  if (ms < 86_400_000) { /* h + m */ }
  /* d + h */
}

export function formatNumber(n: number): string {
  // "999", "1K", "1.5K", "25K", "1M", "25M", "1.5B"
  if (n < 1_000) return n.toString();
  if (n < 10_000) return `${trim1(n / 1_000)}K`;
  /* ... */
}
```

---

## 5. 域提取

### 5.1 getDomain（tools/render-utils.ts）

```typescript
export function getDomain(url: string): string {
  try {
    const u = new URL(url);
    return u.hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}
```

**特性**：
- 使用原生 `URL` 解析（不依赖正则）
- 去 `www.` 前缀（`www.example.com` → `example.com`）
- 解析失败回退到原字符串

### 5.2 sources 列表中的 `(domain)` 后缀（web/search/render.ts）

```typescript
const domain = url ? getDomain(url) : "";
const age    = formatAge(src.ageSeconds) || ...;
const metaParts: string[] = [];
if (domain) metaParts.push(theme.fg("dim", `(${domain})`));  // dim 灰色
if (age)    metaParts.push(theme.fg("muted", age));          // muted 较暗
const metaSep      = theme.fg("dim", theme.sep.dot);
const metaSuffix   = metaParts.length > 0 ? ` ${metaParts.join(metaSep)}` : "";
```

**视觉差异**：`(domain)` 用 `dim`（深灰），`age` 用 `muted`（中灰），让 domain 更显眼，age 退化。

---

## 6. 宽度自适应

### 6.1 ANSI-aware 截断（tui/utils.ts）

```typescript
export function truncateToWidth(
  text: string,
  maxWidth: number,
  ellipsisKind?: Ellipsis | null | "",
  pad?: boolean | null,
): string {
  // Fast path: 纯 ASCII 直接跳过 native 调用
  if (!pad && text.length * 3 <= maxWidth) return text;
  return nativeTruncateToWidth(text, maxWidth,
    (typeof ellipsisKind === "string" ? Ellipsis.Omit : ellipsisKind) ?? Ellipsis.Unicode,
    pad ?? false, DEFAULT_TAB_WIDTH);
}

export function visibleWidth(str: string): string {
  // 跳过 ANSI 转义序列，按 East Asian Width 计算显示宽度
  // 处理 tab、零宽字符、CJK、组合字符、emoji ZWJ 序列
}
```

**为什么 ANSI-aware**：`text.length` 返回 UTF-16 code unit 数，但显示宽度由 East Asian Width 决定（CJK = 2 cells，emoji = 2 cells，零宽 = 0）。如果直接 `text.length`，截断会把 CJK 截成半字符导致乱码。

### 6.2 wrapTextWithAnsi — 行内软折行

```typescript
export function wrapTextWithAnsi(text: string, width: number): string[] {
  return nativeWrapTextWithAnsi(text, width, DEFAULT_TAB_WIDTH);
}
```

用于 `output-block.ts` 把超长 inner 内容折成多行。

### 6.3 lineBudget / titleBudget 计算（web/search/render.ts）

```typescript
const lineBudget  = Math.max(24, width - 6);   // 减 boxRound.vertical (2) + tree.branch (3) + padding
const titleBudget = Math.max(12, lineBudget - Bun.stringWidth(metaSuffix));
const title       = theme.fg("accent", truncateToWidth(titleText, titleBudget));
```

**为什么 `Math.max(24, ...)` 和 `Math.max(12, ...)`**：下限保证即使 `width` 极窄（窄终端被挤窄），title 至少有 12 cells 可用，避免负数或截到 0。

### 6.4 truncateMiddleToWidth — 保留头尾

```typescript
function truncateMiddleToWidth(text: string, maxWidth: number): string {
  const width = visibleWidth(text);
  if (width <= maxWidth) return text;
  if (maxWidth <= 1) return maxWidth === 1 ? "…" : "";
  const tailWidth = Math.ceil((maxWidth - 1) / 2);
  const headWidth = maxWidth - 1 - tailWidth;
  return `${sliceByColumn(text, 0, headWidth, true)}…${sliceByColumn(text, width - tailWidth, tailWidth, true)}`;
}
```

用于 model badge：`claude-3-opus-20240229` → `claud…0229`。

---

## 7. Bubble Tea 等价设计

### 7.1 映射表

| oh-my-pi | Bubble Tea / Go | 说明 |
|---|---|---|
| `Theme.fg(color, text)` | `lipgloss.AdaptiveColor` + `lipgloss.NewStyle().Foreground(c)` | 颜色 token |
| `Theme.styledSymbol(key, color)` | `lipgloss.NewStyle().Foreground(c).Render(symbol)` | 染色符号 |
| `Markdown` 组件 | `glamour` 渲染器 | Markdown 渲染 |
| `RenderResultOptions` | `tea.Msg` 协议 | 展开状态 |
| `render(width)` | `tea.Model.View() tea.Cmd` | 重渲入口 |
| `Component.invalidate()` | `tea.Msg` 触发 `Update` | 缓存失效 |
| `theme.styledSymbol` spinner | `bubbles/spinner` | running 动画 |
| `renderTreeList` 折叠 | `bubbles/viewport` + KeyMsg（ctrl+o） | 滚动/展开 |
| OSC 8 hyperlink | 直接输出 `\x1b]8;;URL\x1b\\TEXT\x1b]8;;\x1b\\` | 终端原生支持 |
| `outputBlock` | `lipgloss.Border(RoundedBorder())` | 边框 |
| `Hasher` 缓存键 | `xxhash.Sum64String` | 缓存键 |

### 7.2 库选择

```go
import (
    "github.com/charmbracelet/lipgloss"   // 颜色/边框/布局
    "github.com/charmbracelet/glamour"     // Markdown
    "github.com/charmbracelet/bubbles/viewport" // 折叠滚动
    "github.com/charmbracelet/bubbles/spinner"  // running 动画
    "github.com/cespare/xxhash/v2"          // 缓存键
)
```

### 7.3 关键映射原则

- **lipgloss.AdaptiveColor**：定义 `{Light, Dark} string` 二元组，由 lipgloss 根据终端背景自动选择
- **ANSI-aware**：lipgloss 内置 `Width()` 处理 ANSI 转义序列；`truncate` 用 `lipgloss.Width` 而非 `len()`
- **tea.Msg**：定义 `ExpandMsg{Expanded bool} SpinnerFrameMsg{Frame int} WidthMsg{W int}` 三类消息
- **theme watcher**：用 `tea.Tick(100ms, ...)` 防抖 + `tea.Msg` 通知重渲
- **OSC 8**：直接拼接 `\x1b]8;id=<hash>;url\x1b\\text\x1b]8;;\x1b\\`，无需额外库

---

## 8. 关键设计原则（8 条）

1. **ANSI-aware 宽度计算是 TUI 必备**：`visibleWidth` 必须跳过 `\x1b[...m` 转义序列、按 East Asian Width 计数（CJK = 2，emoji = 2）。直接用 `len()` 会把 CJK 截成半字符。

2. **state → color/icon/border 三映射一致**：`{success, warning, error, running, pending}` 五个 state 必须共用同一组 token，border、icon、background 都从同一份映射走，避免 `error` icon 配 `success` border 的视觉错位。

3. **折叠默认前 N + 「… N more」**：列表渲染默认显示前 8 项（`PREVIEW_LIMITS.COLLAPSED_ITEMS = 8`），超出用 `formatMoreItems(remaining, "source")` → `… 5 more sources`。`ctrl+o`（`EXPAND_ACTION`）触发全展开。

4. **Time 人性化分桶**：年龄按 `mo > w > d > h > m > just now` 分桶，超过 30 天用月份，不到 1 分钟用 `"just now"`。fallback 到原始 `publishedDate` 字符串。

5. **formatCount 用 'source' 单复数**：`formatCount("source", 3)` → `"3 sources"`，`formatCount("source", 1)` → `"1 source"`。`pluralize` 内置英语复数规则（`-s / -es / -y → -ies`），不是简单 `+s`。

6. **URL 渲染双重：文字 + hyperlink OSC 8**：`theme.fg("accent", title)` 是显示文字，`urlHyperlink(url, title)` 用 OSC 8（`\x1b]8;;URL\x1b\\TEXT\x1b]8;;\x1b\\`）包裹，支持 iTerm2/WezTerm 等现代终端的 `cmd+click`。设置 `tui.hyperlinks = "auto"` 时自动检测；`off` 时纯文字。

7. **status line 单一入口**：所有工具 header 都走 `renderStatusLine(options, theme)`，由 `icon/description/badge/meta` 四字段拼装。`flattenForHeader` 把换行符折叠成空格，防止 header 内嵌换行破坏 bordered block。

8. **Theme AdaptiveColor 双模式（dark/light）**：`ThemeColor` 70+ token 中同一 token（如 `accent`）在 `dark.json` 和 `light.json` 中取不同色值。运行时 `getDefaultTheme()` 用 OSC 11 query 自动选择；macOS Zellij 降级到 host appearance。

---

## 9. Go 移植骨架代码

```go
// internal/ui/theme.go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ============================================================================
// Color Tokens (mirrors oh-my-pi ThemeColor enum)
// ============================================================================

type ColorToken string

const (
	ColAccent     ColorToken = "accent"
	ColBorder     ColorToken = "border"
	ColBorderAcc  ColorToken = "borderAccent"
	ColBorderMuted ColorToken = "borderMuted"
	ColSuccess    ColorToken = "success"
	ColError      ColorToken = "error"
	ColWarning    ColorToken = "warning"
	ColMuted      ColorToken = "muted"
	ColDim        ColorToken = "dim"
	ColText       ColorToken = "text"
	ColToolTitle  ColorToken = "toolTitle"
	ColToolOutput ColorToken = "toolOutput"
	ColMdHeading  ColorToken = "mdHeading"
	ColMdLink     ColorToken = "mdLink"
	ColMdCode     ColorToken = "mdCode"
	ColDiffAdded  ColorToken = "toolDiffAdded"
	ColDiffRemoved ColorToken = "toolDiffRemoved"
	ColUserText   ColorToken = "userMessageText"
	ColToolSuccessBg ColorToken = "toolSuccessBg"
	ColToolErrorBg   ColorToken = "toolErrorBg"
	ColToolPendingBg ColorToken = "toolPendingBg"
)

type SymbolPreset string

const (
	PresetUnicode SymbolPreset = "unicode"
	PresetASCII   SymbolPreset = "ascii"
)

// ============================================================================
// Theme struct: mirrors oh-my-pi Theme class
// ============================================================================

// Colors is the resolved palette (fg tokens + bg tokens).
type Colors struct {
	Fg map[ColorToken]lipgloss.AdaptiveColor
	Bg map[ColorToken]lipgloss.AdaptiveColor
}

// Symbols is the resolved symbol table by category.
type Symbols struct {
	Status struct {
		Success, Error, Warning, Info, Pending,
		Running, Disabled, Enabled, Aborted, Done, Shadowed string
	}
	BoxRound struct {
		TopLeft, TopRight, BottomLeft, BottomRight,
		Horizontal, Vertical,
		TeeRight, TeeLeft, TeeUp, TeeDown, Cross string
	}
	Tree struct {
		Branch, Last, Vertical, Horizontal, Hook string
	}
	Sep struct {
		Dot, Slash, Pipe, Space string
	}
	Icon struct {
		Model, Plan, Folder, File, Agent, Branch, Search, Warning string
	}
	Format struct {
		Bullet, Dash, BracketLeft, BracketRight string
	}
	Tool map[string]string // tool.* 专属符号
}

// Theme is the Go equivalent of oh-my-pi's Theme class.
type Theme struct {
	Name    string
	Preset  SymbolPreset
	IsLight bool
	colors  Colors
	symbols Symbols
}

// ============================================================================
// fg / bg (ANSI application)
// ============================================================================

// fg applies a foreground color token to text. Equivalent to Theme.fg().
func (t *Theme) fg(color ColorToken, text string) string {
	ac, ok := t.colors.Fg[color]
	if !ok {
		return text
	}
	return lipgloss.NewStyle().Foreground(ac).Render(text)
}

// bg applies a background color token to text. Equivalent to Theme.bg().
func (t *Theme) bg(color ColorToken, text string) string {
	ac, ok := t.colors.Bg[color]
	if !ok {
		return text
	}
	return lipgloss.NewStyle().Background(ac).Render(text)
}

// bold/italic/underline — thin wrappers
func (t *Theme) Bold(s string) string   { return lipgloss.NewStyle().Bold(true).Render(s) }
func (t *Theme) Italic(s string) string { return lipgloss.NewStyle().Italic(true).Render(s) }
func (t *Theme) Underline(s string) string {
	return lipgloss.NewStyle().Underline(true).Render(s)
}

// styledSymbol renders a tool-specific or category symbol with a color.
func (t *Theme) StyledSymbol(key, color ColorToken) string {
	glyph := t.symbolByKey(key)
	if glyph == "" {
		return ""
	}
	return t.fg(color, glyph)
}

func (t *Theme) symbolByKey(key ColorToken) string {
	switch key {
	case ColAccent:
		return "■" // status.enabled
	}
	return ""
}

// ============================================================================
// State → Color/Border/Icon mapping (mirrors output-block.ts + status-line.ts)
// ============================================================================

type State string

const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateSuccess State = "success"
	StateError   State = "error"
	StateWarning State = "warning"
)

// BorderColor maps state → border color (from output-block.ts logic).
func (t *Theme) BorderColor(state State) ColorToken {
	switch state {
	case StateError:
		return ColError
	case StateWarning:
		return ColWarning
	case StateRunning, StatePending:
		return ColAccent
	default:
		return ColDim // success uses dim (gray border)
	}
}

// BgColor maps state → background color (from utils.ts getStateBgColor).
func (t *Theme) BgColor(state State) ColorToken {
	switch state {
	case StateSuccess:
		return ColToolSuccessBg
	case StateError:
		return ColToolErrorBg
	default:
		return ColToolPendingBg
	}
}

// StatusIcon maps state → status icon glyph (from formatStatusIcon).
func (t *Theme) StatusIcon(state State, spinnerFrame int) string {
	switch state {
	case StateSuccess:
		return t.StyledSymbol(ColSuccess, ColSuccess)
	case StateError:
		return t.StyledSymbol(ColError, ColError)
	case StateWarning:
		return t.StyledSymbol(ColWarning, ColWarning)
	case StatePending:
		return t.StyledSymbol(ColMuted, ColMuted)
	case StateRunning:
		// 8-frame spinner
		frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
		return frames[spinnerFrame%len(frames)]
	}
	return ""
}

// ============================================================================
// renderStatusLine (single-entry header renderer)
// ============================================================================

type StatusLineOptions struct {
	Icon           State
	IconOverride   string // pre-styled glyph (e.g. magnifier)
	SpinnerFrame   int
	Title          string
	TitleColor     ColorToken // default ColAccent
	Description    string
	Badge          *struct{ Label string; Color ColorToken }
	Meta           []string
}

// RenderStatusLine is the single-entry header renderer.
func (t *Theme) RenderStatusLine(opts StatusLineOptions) string {
	icon := opts.IconOverride
	if icon == "" && opts.Icon != "" {
		icon = t.StatusIcon(opts.Icon, opts.SpinnerFrame)
	}
	titleCol := opts.TitleColor
	if titleCol == "" {
		titleCol = ColAccent
	}
	title := t.fg(titleCol, flatten(opts.Title))
	line := title
	if icon != "" {
		line = icon + " " + title
	}
	if opts.Description != "" {
		line += ": " + t.fg(ColMuted, flatten(opts.Description))
	}
	if opts.Badge != nil {
		line += " " + t.fg(opts.Badge.Color,
			t.symbols.Format.BracketLeft+flatten(opts.Badge.Label)+t.symbols.Format.BracketRight)
	}
	if len(opts.Meta) > 0 {
		filtered := make([]string, 0, len(opts.Meta))
		for _, m := range opts.Meta {
			m = flatten(m)
			if strings.TrimSpace(m) != "" {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) > 0 {
			line += " " + t.fg(ColDim, strings.Join(filtered, t.symbols.Sep.Dot))
		}
	}
	return line
}

// flatten collapses CR/LF into spaces (mirrors flattenForHeader).
func flatten(s string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
}

// ============================================================================
// Collapse / Expand (mirrors formatExpandHint + formatMoreItems)
// ============================================================================

const (
	DefaultCollapsedItems = 8
	ExpandKey             = "ctrl+o"
)

// ExpandHint renders the "[ctrl+o: Expand]" suffix. Empty when expanded or no more.
func (t *Theme) ExpandHint(expanded, hasMore bool) string {
	if expanded || !hasMore {
		return ""
	}
	bracket := t.symbols.Format.BracketLeft + ExpandKey + ": Expand" + t.symbols.Format.BracketRight
	return t.fg(ColDim, bracket)
}

// MoreItems renders "… N more <type>" line.
func (t *Theme) MoreItems(remaining int, itemType string) string {
	return fmt.Sprintf("… %d more %s", remaining, Pluralize(itemType, remaining))
}

// Pluralize mirrors pi-utils pluralize (English only).
func Pluralize(label string, count int) string {
	if count == 1 {
		return label
	}
	last := label[len(label)-1]
	switch {
	case last == 's' || last == 'x' || last == 'z' ||
		(len(label) >= 2 && label[len(label)-2:] == "ch") ||
		(len(label) >= 2 && label[len(label)-2:] == "sh"):
		return label + "es"
	case last == 'y' && len(label) >= 2 && !isVowel(label[len(label)-2]):
		return label[:len(label)-1] + "ies"
	}
	return label + "s"
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}

// ============================================================================
// FormatAge (mirrors utils/format.ts formatAge)
// ============================================================================

// FormatAge returns human age bucket ("just now" / "5m ago" / "2d ago" / "3w ago").
func FormatAge(ageSeconds int64) string {
	if ageSeconds <= 0 {
		return ""
	}
	mins := ageSeconds / 60
	hours := mins / 60
	days := hours / 24
	weeks := days / 7
	months := days / 30

	switch {
	case months > 0:
		return fmt.Sprintf("%dmo ago", months)
	case weeks > 0:
		return fmt.Sprintf("%dw ago", weeks)
	case days > 0:
		return fmt.Sprintf("%dd ago", days)
	case hours > 0:
		return fmt.Sprintf("%dh ago", hours)
	case mins > 0:
		return fmt.Sprintf("%dm ago", mins)
	}
	return "just now"
}

// FormatAgeOrDate falls back to a date string when ageSeconds is missing.
func FormatAgeOrDate(ageSeconds int64, publishedDate string) string {
	if s := FormatAge(ageSeconds); s != "" {
		return s
	}
	return publishedDate
}

// FormatDuration mirrors formatDuration (compact ms/s/m/h/d).
func FormatDuration(d time.Duration) string {
	ms := d.Milliseconds()
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	case ms < 3_600_000:
		m := ms / 60_000
		s := (ms % 60_000) / 1000
		if s > 0 {
			return fmt.Sprintf("%dm%ds", m, s)
		}
		return fmt.Sprintf("%dm", m)
	case ms < 86_400_000:
		h := ms / 3_600_000
		m := (ms % 3_600_000) / 60_000
		if m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		days := ms / 86_400_000
		h := (ms % 86_400_000) / 3_600_000
		if h > 0 {
			return fmt.Sprintf("%dd%dh", days, h)
		}
		return fmt.Sprintf("%dd", days)
	}
}

// FormatBytes mirrors formatBytes (B/KB/MB/GB).
func FormatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%dB", n)
	case n < mb:
		return fmt.Sprintf("%.1fKB", float64(n)/kb)
	case n < gb:
		return fmt.Sprintf("%.1fMB", float64(n)/mb)
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/gb)
	}
}

// FormatCount mirrors formatCount("source", N) → "N sources".
func FormatCount(label string, count int) string {
	return fmt.Sprintf("%d %s", count, Pluralize(label, count))
}

// ============================================================================
// GetDomain (mirrors render-utils.ts getDomain)
// ============================================================================

import "net/url"

// GetDomain extracts hostname with www. stripped. Falls back to input on parse failure.
func GetDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := u.Hostname()
	return strings.TrimPrefix(host, "www.")
}

// ============================================================================
// Width-aware truncation (mirrors truncateToWidth + visibleWidth)
// ============================================================================

// TruncateToWidth is the Go equivalent of pi-tui's truncateToWidth.
// Uses lipgloss.Width which is ANSI-aware and East Asian Width aware.
func TruncateToWidth(s string, maxWidth int, ellipsis ...string) string {
	ell := "…"
	if len(ellipsis) > 0 {
		ell = ellipsis[0]
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
	// Note: lipgloss.MaxWidth doesn't add ellipsis; for true truncation,
	// call VisibleWidth + manual slice or use ansi.Truncate from
	// github.com/charmbracelet/x/ansi (true ANSI-aware).
	// For brevity here, prefer x/ansi.Truncate in production code.
}

// VisibleWidth is the Go equivalent of pi-tui's visibleWidth.
func VisibleWidth(s string) int {
	return lipgloss.Width(s)
}

// ============================================================================
// OSC 8 hyperlink (mirrors urlHyperlink)
// ============================================================================

import "hash/fnv"

// Hyperlink wraps text in an OSC 8 hyperlink when enabled.
// Returns plain text when disabled or text already contains OSC 8.
func Hyperlink(uri, displayText string, enabled bool) string {
	if !enabled || strings.Contains(uri, "\x00") || strings.Contains(displayText, "\x1b]8;") {
		return displayText
	}
	id := buildLinkID(uri)
	return fmt.Sprintf("\x1b]8;id=%s;%s\x1b\\%s\x1b]8;;\x1b\\", id, uri, displayText)
}

// URLHyperlink wraps text in OSC 8 pointing to http(s) URL.
func URLHyperlink(rawURL, displayText string, enabled bool) string {
	normalized := rawURL
	if strings.HasPrefix(strings.ToLower(rawURL), "www.") {
		normalized = "https://" + rawURL
	}
	u, err := url.Parse(normalized)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return displayText
	}
	return Hyperlink(u.String(), displayText, enabled)
}

func buildLinkID(uri string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(uri))
	return fmt.Sprintf("%08x", h.Sum32())
}

// ============================================================================
// Default Theme (dark)
// ============================================================================

// NewDefaultTheme returns the built-in dark theme.
// For light theme, swap AdaptiveColor.Light values.
func NewDefaultTheme() *Theme {
	t := &Theme{
		Name:   "dark",
		Preset: PresetUnicode,
	}

	// Foreground palette (dark mode)
	t.colors.Fg = map[ColorToken]lipgloss.AdaptiveColor{
		ColAccent:        {Light: "#5f5faf", Dark: "#7d7dff"},
		ColBorder:        {Light: "#999999", Dark: "#3c3c3c"},
		ColBorderAcc:     {Light: "#5f5faf", Dark: "#7d7dff"},
		ColBorderMuted:   {Light: "#cccccc", Dark: "#1e1e1e"},
		ColSuccess:       {Light: "#005f00", Dark: "#5fd75f"},
		ColError:         {Light: "#af0000", Dark: "#ff5f5f"},
		ColWarning:       {Light: "#875f00", Dark: "#d78700"},
		ColMuted:         {Light: "#7f7f7f", Dark: "#808080"},
		ColDim:           {Light: "#a0a0a0", Dark: "#5c5c5c"},
		ColText:          {Light: "#1a1a1a", Dark: "#e5e5e7"},
		ColToolTitle:     {Light: "#5f5faf", Dark: "#9f9fff"},
		ColToolOutput:    {Light: "#3a3a3a", Dark: "#cccccc"},
		ColMdHeading:     {Light: "#5f5faf", Dark: "#9f9fff"},
		ColMdLink:        {Light: "#5f5faf", Dark: "#7d7dff"},
		ColMdCode:        {Light: "#875f00", Dark: "#d78700"},
		ColDiffAdded:     {Light: "#005f00", Dark: "#5fd75f"},
		ColDiffRemoved:   {Light: "#af0000", Dark: "#ff5f5f"},
	}

	// Background palette
	t.colors.Bg = map[ColorToken]lipgloss.AdaptiveColor{
		ColToolSuccessBg:  {Light: "#e6f5e6", Dark: "#0f3a0f"},
		ColToolErrorBg:    {Light: "#f5e6e6", Dark: "#3a0f0f"},
		ColToolPendingBg:  {Light: "#f0f0f0", Dark: "#1a1a1a"},
		ColUserText:       {Light: "#1a1a1a", Dark: "#e5e5e7"},
	}

	// Symbols (unicode preset)
	t.symbols.Status.Success = "✔"
	t.symbols.Status.Error = "✘"
	t.symbols.Status.Warning = "⚠"
	t.symbols.Status.Info = "ⓘ"
	t.symbols.Status.Pending = "⏳"
	t.symbols.Status.Running = "⟳"
	t.symbols.Status.Disabled = "⦸"
	t.symbols.Status.Enabled = "●"
	t.symbols.Status.Aborted = "⏹"
	t.symbols.Status.Done = "•"
	t.symbols.Status.Shadowed = "○"

	t.symbols.BoxRound.TopLeft = "╭"
	t.symbols.BoxRound.TopRight = "╮"
	t.symbols.BoxRound.BottomLeft = "╰"
	t.symbols.BoxRound.BottomRight = "╯"
	t.symbols.BoxRound.Horizontal = "─"
	t.symbols.BoxRound.Vertical = "│"
	t.symbols.BoxRound.TeeRight = "├"
	t.symbols.BoxRound.TeeLeft = "┤"
	t.symbols.BoxRound.TeeUp = "┴"
	t.symbols.BoxRound.TeeDown = "┬"
	t.symbols.BoxRound.Cross = "┼"

	t.symbols.Tree.Branch = "├─"
	t.symbols.Tree.Last = "└─"
	t.symbols.Tree.Vertical = "│"
	t.symbols.Tree.Horizontal = "─"
	t.symbols.Tree.Hook = "└"

	t.symbols.Sep.Dot = " · "
	t.symbols.Sep.Slash = " / "
	t.symbols.Sep.Pipe = " │ "
	t.symbols.Sep.Space = " "

	t.symbols.Icon.Model = "⬢"
	t.symbols.Icon.Plan = "🗺"
	t.symbols.Icon.Folder = "📁"
	t.symbols.Icon.File = "📄"
	t.symbols.Icon.Agent = "👥"
	t.symbols.Icon.Branch = "⑂"
	t.symbols.Icon.Search = "🔍"
	t.symbols.Icon.Warning = "⚠"

	t.symbols.Format.Bullet = "•"
	t.symbols.Format.Dash = "—"
	t.symbols.Format.BracketLeft = "⟦"
	t.symbols.Format.BracketRight = "⟧"

	t.symbols.Tool = map[string]string{
		"tool.write":     "✎",
		"tool.edit":      "✎",
		"tool.bash":      "❯",
		"tool.webSearch": "⌕",
		"tool.read":      "📄",
		"tool.glob":      "🔍",
		"tool.grep":      "🔍",
	}

	t.IsLight = false
	return t
}

// ============================================================================
// Auto-detect dark/light (mirrors theme.ts detectTerminalBackground)
// ============================================================================

import "os"

// DetectBackground returns "dark" or "light" based on terminal signals.
// In production: use OSC 11 query (most accurate). Here we read COLORFGBG fallback.
func DetectBackground() string {
	if cfb := os.Getenv("COLORFGBG"); cfb != "" {
		parts := strings.Split(cfb, ";")
		if len(parts) >= 2 {
			var bg int
			fmt.Sscanf(parts[1], "%d", &bg)
			if bg >= 0 && bg < 8 {
				return "dark"
			}
			return "light"
		}
	}
	return "dark" // safe default
}

// ============================================================================
// Box / State rendering (mirrors output-block.ts)
// ============================================================================

// BorderedBox returns a lipgloss-styled rounded box with state-colored border.
// Equivalent to renderOutputBlock minimal path.
func (t *Theme) BorderedBox(content string, state State, width int) string {
	bc := t.BorderColor(state)
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.colors.Fg[bc]).
		Width(width - 2) // -2 for border glyphs
	if bgCol, ok := t.colors.Bg[t.BgColor(state)]; ok {
		borderStyle = borderStyle.Background(bgCol)
	}
	return borderStyle.Render(content)
}

// ============================================================================
// Width budget helpers (mirrors web/search/render.ts lineBudget/titleBudget)
// ============================================================================

// LineBudget returns the safe display width inside a box (subtracts borders + padding).
func LineBudget(boxWidth int) int {
	return max(24, boxWidth-6) // 2 vertical borders + tree branch + padding
}

// TitleBudget reserves space for meta suffix in a one-line item.
func TitleBudget(lineWidth int, metaSuffix string) int {
	return max(12, lineWidth-lipgloss.Width(metaSuffix))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

> [!tip] 集成建议
> - `lipgloss.Width` 已 ANSI-aware，可替代 `visibleWidth`。对超长文本截断用 `github.com/charmbracelet/x/ansi.Truncate`（更精确的 CJK + ZWJ 处理）。
> - 主题切换走 `tea.Msg{Type: "themeChanged"}` + `tea.Tick(100ms, ...)` 防抖（`ThemeWatcher` 类比）。
> - 缓存键用 `xxhash.Sum64String`（`github.com/cespare/xxhash/v2`），`RenderCache` 同结构（`{key, lines}`）。
> - State → Color/Border/Icon 三映射集中在 `BorderColor / BgColor / StatusIcon` 三个方法，确保视觉一致。
> - `RenderStatusLine` 是唯一 header 入口，所有工具都调用它。

> [!warning] 注意事项
> - **OSC 8 hyperlink** 在 lipgloss 中默认开启，但 iTerm2/WezTerm/Kitty 才支持。设置 `tui.hyperlinks = "auto"` 时检测 `TERMINAL_PROGRAM`。
> - **macOS Zellij** 需要降级到 host appearance 检测（`osascript -e "tell application \"System Events\" to get appearance of \"System Settings\""`）。
> - **light theme 的 AdaptiveColor**：在 light theme 下 `lipgloss` 会自动取 `Light` 字段，但部分终端（Windows Terminal 旧版）只识别 `Dark`。可加 `TERM=*-256color` 探测。
> - **truncateToWidth** 实现：lipgloss 的 `MaxWidth()` 不会添加 ellipsis，生产代码推荐用 `github.com/charmbracelet/x/ansi.Truncate(s, maxWidth, "…")`。

## 相关笔记

- [[../knowledge/patterns/01-bubble-tea-tui-pattern]]
- [[../knowledge/research/01-oh-my-pi-调研]]
- [[../knowledge/projects/oh-my-pi/01-theme-system]]
