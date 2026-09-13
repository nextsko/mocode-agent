---
name: ui-replication
description: >-
  Use when 用户给出设计稿/截图/现有网页要求「复刻这个界面/1:1 还原/fork 这个 UI/做成一样」，
  或已实现界面需要「修 UI/对齐设计稿/修样式偏差/视觉不对」。覆盖两条工作流：从零复刻（截图→
  结构映射→实现）与偏差修复，并用 playwright-cli 截图比对做视觉验收。
---

# UI 复刻与偏差修复

两件套工作流：**fork**（从设计稿复刻新界面）与 **fix**（修复已实现界面的偏差/a11y 问题）。核心原则：先产出映射表与布局规格并确认，再写代码；**a11y 优先于 100% 像素还原**。

## 路由表

| 用户意图 | 工作流 |
|---|---|
| 从零复刻（截图/Figma → 结构映射 → 实现） | A. 复刻（fork） |
| 修复已实现 UI 的偏差 / 优化视觉细节 | B. 偏差修复（fix） |

## 技术栈约束

- 包管理只用 `bun`（命令见 `bun` 技能）；样式 Tailwind CSS v4；组件库 shadcn/ui。
- **禁止硬编码颜色/间距**，一律用 Design Token；Token 通过 `@theme` 落在 `globals.css`。
- 确认 Tailwind `content` / `@source` 覆盖所有组件路径，避免生产样式被 purge。

## 工作流 A：从零复刻

**Phase 1 · 设计稿分析**
1. 先产出**页面→组件映射表**与布局规格（栅格、断点、间距、字号、层级），请用户确认后再写代码。
2. 提取 Design Token：颜色、间距、圆角、字体、阴影等设计变量。

**Phase 2 · 项目初始化**
```bash
bunx shadcn@latest init     # 未初始化时执行
bun audit                   # 初始化后做依赖安全校验
```
- Token 用 `@theme` 写入 `globals.css`；按原子设计分层（atoms → molecules → organisms → pages）。

**Phase 3 · 组件复刻**
- 严格按映射表逐组件实现；每完成一个组件立即做 a11y 快速校验（focus 可见、label、键盘可达）。

**Phase 4 · 验收与修复**
- 用 `playwright-cli` 截图与设计稿并排比对，产出偏差清单。
- `bun run typecheck` 零错误、lint 零 warning；可选 build 后检查 CSS 体积。

## 工作流 B：偏差修复

1. **情报收集**：定位目标组件/页面（list/read），确定问题范围。
2. **根因分析**：归类为布局 / 配色 / 交互态 / a11y / Token 未统一 / 体积与 purge。
3. **最小改动修复**：不破坏既有功能，新增样式必须走 Design Token。
4. **校验**：typecheck + lint 零 warning；axe 零 violation、Lighthouse a11y ≥90；截图复查。

| 偏差类型 | 动作 |
|---|---|
| 布局偏差 | 对齐栅格、spacing、断点 |
| 配色偏差 | 硬编码 → Design Token，校验对比度 |
| 交互态缺失 | 补 hover/focus/active/loading/error/disabled |
| a11y 不合规 | 键盘、ARIA、对比度、焦点可见（对照 `web-design-guidelines`） |
| Token 未统一 | 抽取硬编码 → Token，更新 `@theme` |
| 体积 / purge | 排查动态类、`content`/`@source` 路径、safelist，验证生产体积 |

## 视觉验收（playwright-cli）

```bash
playwright-cli open <url>
playwright-cli resize 1440 900
playwright-cli screenshot --filename=current.png
playwright-cli snapshot --boxes          # 拿到元素 ref 与 bounding box
playwright-cli eval "getComputedStyle(document.querySelector('h1')).fontSize"
playwright-cli close
```

- 用固定视口截图（桌面/移动各一次），与设计稿并排对比，禁止仅凭肉眼感觉。
- 用 `--boxes` / `eval` 取实际尺寸、间距、颜色，与映射表逐项核对（命令细节见 `playwright-cli`）。

## 完成检查

- [ ] 映射表 + 布局规格已产出并经确认
- [ ] 无硬编码颜色/间距，全部 Design Token
- [ ] 关键交互态（hover/focus/active/disabled）齐备
- [ ] 键盘可达、焦点可见、对比度达标
- [ ] `bun run typecheck` 零错误、lint 零 warning
- [ ] axe 零 violation、Lighthouse a11y ≥90
- [ ] 截图与设计稿比对通过，偏差清单清零

## 相关技能

- `web-design-guidelines`：无障碍、焦点、表单、动效等审查规则。
- `vercel-react-best-practices`：React 组件性能与重渲染优化。
- `playwright-cli`：截图、快照、元素属性与视觉比对命令。
- `bun`：包管理与脚本执行。
- `screenshot-to-ui`：从截图推导盒子模型 / 布局 / 间距 / 字号 / 色彩的方法学。
- `css-layout-and-box-model`：盒子模型与布局系统详解与排错。
- `design-tokens`：把量到的值固化为 token 体系。
