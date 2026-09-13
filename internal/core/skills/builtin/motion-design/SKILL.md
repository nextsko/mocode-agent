---
name: motion-design
description: >-
  Use when 设计或实现界面动效与微交互：选择时长/缓动 token（fast 120 / base 200 / slow 320ms、ease-out / spring）、
  判断何时该用动效（反馈、过渡、引导注意力、表达层级与空间关系）与何时不该用、
  设计 enter/exit、列表 stagger、共享元素/布局位移、skeleton、optimistic update 等模式，
  遵守只动 transform/opacity 的性能纪律、prefers-reduced-motion 降级与焦点管理，
  并用逐帧录制核对验收（见 red-team-animation-verification）。
---

# 动效与微交互设计

一句话：**动效是信息的载体，不是装饰**——每次动画都要回答「它帮用户理解了什么」，答不出就删掉。

配套：`design-tokens`（时长/缓动 token）、`red-team-animation-verification`（逐帧验收）、`web-design-guidelines`（reduced-motion 与焦点）、`vercel-react-best-practices`（框架内动画）、`tailwindcss`（utility 动画）、`screenshot-to-ui`（交互态补全）。

## 1. 核心原则

- **服务于理解**：反馈操作已发生、说明元素从哪来到哪去、引导注意力、表达层级与空间关系。
- **克制**：默认无动画，只在有明确目的处加；好 UI 动效「几乎察觉不到但少不得」。
- **可打断**：用户输入应立即中断动画，不能「等动画播完」。
- **一致**：同类动作同时长同缓动；时长与缓动来自 token，不临时拍脑袋。

## 2. 时长与缓动 token

| token | 值 | 用途 |
|-------|-----|------|
| `--motion-fast` | 120ms | 微交互：hover、按钮按下、toggle、颜色变化 |
| `--motion-base` | 200ms | 常规：弹出、tooltip、下拉、卡片进入 |
| `--motion-slow` | 320ms | 大范围/复杂：模态、页面级过渡、抽屉 |

```css
:root {
  --motion-fast: 120ms;
  --motion-base: 200ms;
  --motion-slow: 320ms;
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);   /* 进入：快起慢收 */
  --ease-in-out: cubic-bezier(0.4, 0, 0.2, 1); /* 位移/形变 */
}
```

- **方向感**：进入用 ease-out（快、自信）；退出稍快（约 0.75×时长）+ ease-in；位移/双向用 ease-in-out。
- **spring**：需要物理感/可打断时用弹簧（Framer Motion `spring`、原生 `linear()` 逼近）；日常 UI 优先 ease-out——弹簧调不好比不用更糟。
- **超出三档就要重新审视**：复杂编排也应落在 120/200/320 与少量派生值上。
- 时长随**距离/元素大小**微调（大面板 vs 小 chip 同 200ms 观感不同），但幅度小。
- 所有值进 `design-tokens`；组件引用 `var(--motion-base)`，禁止硬编码 `0.3s`。

## 3. 何时用 / 何时不用

**该用**：

- **反馈**：按下/成功/失败/加载（按钮状态、toast）。
- **过渡**：元素出现/消失、视图切换、布局变化（避免「闪一下」）。
- **引导注意力**：新功能提示、错误定位、内容更新高亮。
- **表达空间/层级**：模态从触发处放大（说明来源）、抽屉从边缘推入、共享元素过渡（说明「是同一个东西」）。

**不该用**：

- **纯装饰**：无信息量的持续漂浮、闪烁、粒子。
- **阻塞**：动画播完才能操作；用动画假装加载进度。
- **高频触发**：滚动/输入每次都触发重度动画（卡顿且干扰）。
- **数据已足够清晰**时再加动画（重复强调）。
- **reduced-motion 用户**：必须降级（见 §6）。
- 判断口径：**说不出它传达了哪条信息 → 不加。**

## 4. 常见模式

| 模式 | 做法 | 要点 |
|------|------|------|
| enter/exit | opacity + 轻微 translate/scale；退出更快 | 退出用更短时长，避免「残影感」 |
| 列表 stagger | 子项按 index 延迟 20–50ms 入场 | 总时长封顶（如 ≤400ms），长列表只 stagger 首屏可见项 |
| 共享元素/布局位移 | 元素在两位置间连续移动（FLIP / layout animation） | 用 `transform` 位移，给出明确「来源-去向」 |
| skeleton | 形状占位 + 微光替代 spinner | 形状接近真实内容；reduced-motion 下停止微光 |
| optimistic update | 立即反馈结果，失败再回滚并提示 | 回滚也要有过渡，别让用户感到「跳回去了」 |
| 展开/折叠 | 高度 + opacity 过渡（或 `grid-template-rows: 0fr→1fr`） | `height:auto` 不可动画，用 grid 技巧或测量 |
| 数字/进度 | 数值缓动、进度条平滑推进 | 数字用 `tabular-nums` 防抖动；避免每 tick 重排 |

- 优先用**框架的布局动画能力**（Framer Motion `layout`、`View Transitions`）而非手写。
- 同一时刻**不要叠加过多动作**：主次分明，保持单一视觉焦点。

## 5. 性能

- **只动 `transform` 与 `opacity`**：可被合成器单独处理，不触发布局/重绘。
- **禁止**动画 `width/height/top/left/margin/padding/border` 等触发布局的属性；形变用 `transform: scale/translate/rotate`。
- `will-change` **慎用**：动画开始前加、结束后移除；滥用会常驻合成层、吃显存。
- JS 逐帧驱动用 `requestAnimationFrame`（不用 `setTimeout`）；读写分离，避免 layout thrashing。
- 大范围滤镜/模糊/阴影动画代价高，改用预渲染或静态图；长列表只对**可见区域**做动画（虚拟化，见 `web-design-guidelines`）。

## 6. 可访问性

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.001ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.001ms !important;
  }
}
```

- **必须尊重 `prefers-reduced-motion`**：把「位移/缩放/视差/自动播放」降级为淡入或直接切换，**不是**简单删掉后什么都不剩。
- reduced-motion 下仍要保留**状态变化反馈**（如用静态高亮替代弹跳）。
- 自动播放/循环动效提供**暂停/停止**；>5s 的自动动效必须有控件。
- **焦点管理**：动画期间不抢焦点；模态/抽屉关闭后焦点回归触发元素；用 `inert` 隔离背景。
- 避免前庭不适（大位移、缩放、视差）——这类最该在 reduced-motion 下降级。

## 7. 反模式

- 滥用弹跳/回弹（bounce/elastic）：几乎永远不合时宜，拉低专业感。
- 阻塞型动画 >300ms 挡住主操作；过长的入场。
- 纯装饰的持续动效（漂浮、闪烁、无意义粒子）。
- `transition: all`：意外动画了不想动的属性（应显式列出）。
- 用 `left/top/width/height` 做动画 → 每帧布局，掉帧。
- 无 `prefers-reduced-motion` 处理；或用 `visibility:hidden` 直接抹掉反馈。
- 动画期间抢焦点、模态关闭后焦点丢失。
- 时长/缓动硬编码、各处不一致；用 `setTimeout` 而非 `rAF` 驱动。

## 8. 验收（必须看运动本身）

- **不要用「文件存在 / 体积合理 / DOM 有 class」证明动画在跑**——那是假代理。
- 用 `playwright-cli` 录制视频，`ffmpeg` 在已知时间点抽帧，计算相邻帧像素差：全部为 0 = 静止 = FAIL；差值序列形状符合预期 = PASS。
- 逐帧核对：时长是否落在 token 档位、缓动方向是否正确（进入快起）、退出是否更短、是否只动 transform/opacity、reduced-motion 下是否正确降级。
- 完整协议与基线见 `red-team-animation-verification`。

## 相关技能

- `design-tokens`：时长/缓动 token 的唯一来源。
- `red-team-animation-verification`：逐帧差分动画验收。
- `web-design-guidelines`：reduced-motion、焦点、性能审查。
- `vercel-react-best-practices`：React 内动效与渲染开销。
- `tailwindcss` / `shadcn`：`transition-*` / `animate-*` 与组件动效落地。
- `screenshot-to-ui`：从设计稿补全交互态。
