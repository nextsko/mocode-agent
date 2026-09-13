---
name: design-md
description: >-
  Use when 想让 AI 在生成 UI 时保持一致的设计语言，需要在项目里落地或维护一份 DESIGN.md。用纯文本描述配色、字体、组件、间距与设计哲学，让任何 AI coding agent 都能据此产出像素级一致的界面。适用于用 AI 搭 UI、跨组件保持设计一致、复刻既有网站外观等场景。
---

# DESIGN.md：给 AI 的一份设计系统

`DESIGN.md` 是纯文本设计系统文档，放在项目根目录，AI agent 读它来生成一致的 UI。类比：`AGENTS.md` 告诉 agent **怎么构建**，`DESIGN.md` 告诉 agent **东西应该长什么样**。它对标 Google Stitch 引入的格式，也是 LLM 最容易读的 markdown。

> 视觉方向与"反 AI 味"的品味判断交给 `design-taste-frontend`；产出后的无障碍与交互审查交给 `web-design-guidelines`；组件实现规范交给 `vercel-react-best-practices`。

## 1. 使用方式

1. 把 `DESIGN.md` 放到项目根目录（与 `AGENTS.md` / `CLAUDE.md` 同级）。
2. 告诉 agent：`Build a landing page following DESIGN.md`。
3. agent 读取配色、字体、组件、布局规则后生成一致 UI。

一个项目**只放一份** DESIGN.md，它定义整个视觉语言；随设计演进更新（版本化 markdown）。

## 2. 标准结构（每份都按此骨架）

```markdown
# DESIGN.md

## Visual Theme & Atmosphere
氛围、密度、设计哲学（如 "dark cinematic"、"clean editorial"）

## Color Palette & Roles
| Name | Hex | Role |
|------|-----|------|
| Primary | #6366f1 | Interactive elements, CTAs |
| Background | #0a0a0b | Page background |
| Surface | #18181b | Cards, elevated content |
| Text Primary | #fafafa | Main content |
| Accent | #f97316 | Highlights, alerts |

## Typography Rules
字体族、h1-h6/body/caption 的字号层级、字重、行高

## Component Stylings
按钮(primary/secondary/ghost)、卡片、输入框、导航 —— 含 hover/active 状态

## Layout Principles
间距刻度(4px base)、max-width、网格列数、留白哲学

## Depth & Elevation
阴影系统、边框处理、surface 层级

## Do's and Don'ts
设计护栏：该做什么、避免什么

## Responsive Behavior
断点、触摸目标、移动端适配
```

## 3. 从零创建：抓取目标站点

1. **Colors**：浏览器 DevTools → Computed styles，收集全部用色。
2. **Typography**：CSS 里的 font-family，从标题提取字号刻度。
3. **Spacing**：量 padding/margin，找出基准单位（通常 4px 或 8px）。
4. **Components**：截图按钮、卡片、输入框的**所有状态**。
5. **Layout**：记录 max-width、网格列数、响应式断点。

按 §2 结构写进 markdown。

## 4. 可选：现成 DESIGN.md 库

`awesome-design-md` 集合（https://github.com/VoltAgent/awesome-design-md）有 58+ 份从真实网站提取的现成文件：

- **AI**：Claude、Cohere、ElevenLabs、Mistral、Ollama、Replicate、RunwayML
- **Dev Tools**：GitHub、Vercel、Supabase、Railway、Linear、Cursor
- **SaaS**：Stripe、Notion、Figma、Slack、Cal.com
- **Marketing**：Tailwind、shadcn/ui、Framer

```bash
curl -O https://raw.githubusercontent.com/VoltAgent/awesome-design-md/main/design-md/linear/DESIGN.md
```

然后：`Build a landing page for a project management tool following DESIGN.md. Include: hero with gradient, feature grid, pricing table, CTA section.`

## 5. 使用示例

**示例 1 · SaaS landing**：放入 Linear 的 DESIGN.md，要求 hero + feature grid + pricing + CTA，agent 会用 Linear 的精确配色、字体、间距与组件样式产出。

**示例 2 · 组件库一致性**：`Create a notification dropdown component following DESIGN.md.` —— agent 自动采用正确的 surface 色、圆角、阴影级别与字体，无需手工纠正。

## 6. 指南

- 放项目根目录，与 `AGENTS.md` / `CLAUDE.md` 并列。
- 一份项目一份 DESIGN.md，覆盖全部视觉语言。
- 设计演进时同步更新（它是受版本控制的 markdown）。
- 深/浅色主题：在 palette 段里**同时**列出两套色值。
- 先用一个简单组件验证 agent 确实读懂了，再规模化。
- 与 `design-taste-frontend` 搭配使用以获得额外的设计质量规则。
