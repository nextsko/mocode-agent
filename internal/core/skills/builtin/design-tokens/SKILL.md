---
name: design-tokens
description: >-
  Use when 需要建立或统一设计系统变量：颜色、间距、字号、行高、圆角、阴影、动效的 token 体系，
  主题（亮/暗/多品牌）切换，或把硬编码样式收敛为 token。也用于截图复刻时把量到的值固化为变量。
---

# Design Tokens（设计变量体系）

一句话：**先有 token，再写组件**——token 是设计与代码之间唯一的契约，杜绝硬编码 magic number。

## 1. Token 分层

| 层 | 例 | 说明 |
|----|----|------|
| 原始（primitive） | `--blue-500`, `--space-4`, `--font-14` | 与语义无关的原子值 |
| 语义（semantic） | `--color-bg`, `--color-fg`, `--color-primary`, `--color-danger` | 引用原始层，按**用途**命名 |
| 组件（component） | `--button-bg`, `--card-radius` | 引用语义层，仅组件内部用 |

规则：组件只准引用**语义/组件**层；换主题只改语义层映射。

## 2. 各类 token 的尺度

- **间距 / 尺寸**：4/8pt 网格（4,8,12,16,24,32,40,48,64…）。
- **字号**：模数比例（1.125/1.25/1.333），配**行高** token（正文 1.5，标题 1.2）。
- **圆角**：`sm 4 / md 8 / lg 12 / xl 16 / full 9999`。
- **阴影**：分 elevation 档（xs/sm/md/lg），每档 `x y blur spread color`。
- **颜色**：主色 + 中性色阶（bg/surface/border/muted/fg）+ 状态色（success/warn/danger/info）。
- **动效**：时长（fast 120 / base 200 / slow 320ms）+ 缓动（ease-out / spring）。
- **断点**：640 / 768 / 1024 / 1280（由设计定，勿随意加）。

## 3. 命名约定

- 语义优先，避免外观命名（用 `--color-danger` 而非 `--color-red`）。
- 层级用 `-` 连接（`--color-surface-raised`）；变体后缀 `-hover/-active/-disabled`。
- 同一含义在**设计工具与代码**用**同名**，减少翻译损耗。

## 4. 落地（Tailwind v4 / CSS 变量）

```css
/* globals.css */
@theme {
  --color-bg: oklch(100% 0 0);
  --color-fg: oklch(20% 0.02 250);
  --color-primary: oklch(62% 0.19 255);
  --space-4: 1rem;
  --radius-lg: 0.75rem;
  --shadow-md: 0 4px 12px rgb(0 0 0 / 0.08);
  --text-base: 1rem;
  --leading-base: 1.5;
}
```

- 组件里写 `bg-bg text-fg rounded-lg gap-4`，不写 `bg-white text-[#1a1a1a] gap-[17px]`。
- 暗色/多主题：在 `:root`/`.dark`/`[data-theme=…]` 覆盖**语义层**，不动组件。

## 5. 收敛硬编码（改造既有项目）

1. 扫描硬编码值：颜色（`#…`/`rgb()`）、间距（`px`）、字号、圆角、阴影。
2. 按频率聚类 → 归一到最近档位 → 命名成语义 token。
3. 批量替换；对无法归一的**例外**加注释说明。
4. 加 lint/审查护栏：禁止新增硬编码（可用 stylelint / 自定义检查）。

## 6. 验收

- [ ] 颜色/间距/字号/圆角/阴影**无硬编码**，全走 token
- [ ] 换主题只改语义层，组件零改动
- [ ] 对比度达标（正文 ≥4.5:1）
- [ ] token 命名语义化、与设计工具一致
- [ ] 暗色/多品牌有覆盖且无遗漏

## 相关技能

- `screenshot-to-ui`：把截图量到的值固化为 token。
- `css-layout-and-box-model`：间距 / 尺寸的布局基础。
- `tailwindcss` / `shadcn`：token 的落地方式。
- `web-design-guidelines`：对比度与可用性。
