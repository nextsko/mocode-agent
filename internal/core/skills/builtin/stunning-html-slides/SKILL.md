---
name: stunning-html-slides
description: >-
  Use when 需要制作令人惊艳的 HTML 幻灯片/演示文稿，支持三种路径：模板库快选（32 套专业模板）、杂志风横向翻页（WebGL 流体背景 + 衬线排版）、现代 UI 风（毛玻璃卡片、Spotlight 聚光灯、粒子、Framer Motion）。覆盖选型、主题节奏、组件配方、导航与质检，产出单文件 HTML deck。
---

# HTML 幻灯片（Stunning Slides）

三轨系统：**A 模板库**最快、**B 杂志风**最有质感、**C 现代风**最科技。

> 视觉品味与反 AI 味约束见 `design-taste-frontend`；无障碍与交互细节审查见 `web-design-guidelines`。

## 0. 三轨速览

| 模式 | 风格 | 核心特性 | 适用 | 速度 |
|:--|:--|:--|:--|:--|
| **A 模板库** | 32 种专业设计 | 一键选模板、tone-first 匹配、`deck-stage.js` 导航 | 任何场景快速出 deck | 最快 |
| **B 杂志风** | 电子杂志 × 电子墨水 | WebGL 流体背景、衬线标题、横向翻页、Motion One | 演讲/分享/发布 | 中 |
| **C 现代风** | Glassmorphism × Spotlight | 毛玻璃、聚光灯跟随、粒子、Framer Motion | 技术演示/对比 | 中 |

## A · 模板库模式

### 快速匹配矩阵

| 想要… | 首选模板 |
|---|---|
| 专业/商务 | Blue Professional, Signal, Cartesian |
| 科技/开发 | Neo-Grid Bold, 8-Bit Orbit, Raw Grid, Studio |
| 学术/研究 | Vellum, Monochrome, Soft Editorial, Cobalt Grid |
| 创意/代理 | Creative Mode, BlockFrame, Scatterbrain |
| 时尚/奢华 | Pink Script, Coral, Editorial Tri-Tone |
| 温暖/友好 | Playful, Daisy Days, Capsule, Long Table |
| 大胆/宣言 | Bold Poster, People's Platform, Broadside |
| 复古/怀旧 | Retro Windows, Retro Zine, Sakura Chroma |
| 自然/可持续 | Grove, Mat, Pin & Paper |
| 双语 EN/CN | Broadside, Grove, Monochrome, Vellum, Signal, Studio |
| 暗色主题 | Vellum, Pink Script, 8-Bit Orbit, Studio, Signal |

### 6 步工作流

1. **问场景和调性**：① 什么场合？② 想要什么感觉？（只问这两个）
2. **读 `index.json` 匹配 3 个候选**：按 `mood` + `tone` + `best_for` 匹配，三个候选风格差异要足够大。字段：`mood` / `tone` / `formality` / `density` / `scheme` / `best_for` / `avoid_for`；`template.json` 另有 `palette`(bg/ink/paper/accent/muted) 与 `typography`(display/body/mono)。
3. **建预览**：取模板首张幻灯片替换为用户内容 → 复制 `deck-stage.js` 到预览目录 → 存为独立 HTML。
4. **展示预览让用户选**：用 `screenshots/{slug}-{N}.png` 截图，或打开 HTML。
5. **生成完整 deck**：克隆模板 → 替换内容 → 增删幻灯片 → 更新页码。
6. **交付**：打开文件 + 发送路径 + 一句话说明选了哪个模板及原因。

**保留**（设计系统）：字体、色板、网格、装饰元素、`deck-stage.js`。**替换**（用户内容）：标题、正文、数据、名称日期、图片。

`deck-stage.js`：键盘导航（←/→/PgUp/PgDn/Space/Home/End/数字键）、移动端点击区、自动缩放(1920×1080)、演讲者注释、打印 PDF、`slidechange` 事件、公开 API `.goTo/.next/.prev/.reset/.index/.length`。

### 模板路径禁忌

- 不要替换字体（typography IS the design system）；不要改色板（微调也破坏和谐）
- 不要混合不同模板的布局；不要去掉"多余"装饰（角标、纹理、SVG 是身份）
- 必须复制 `deck-stage.js`，否则无法导航；每次都打开浏览器预览

## B · 杂志风模式

单文件 HTML 横向翻页 PPT：WebGL 流体背景（仅 hero 页可见）、衬线标题（Noto Serif SC + Playfair）+ 非衬线正文（Noto Sans SC）+ 等宽元数据（IBM Plex Mono）、Lucide 线性图标、横向翻页（键盘/滚轮/触屏/底部圆点/ESC 索引）、主题平滑插值、Motion One 入场动效。

**Step 1 · 需求澄清**：受众/场景、分享时长（15 分钟 ≈ 10 页）、原始素材、有无图片、主题色、硬约束。

图片约定：位置 `项目/XXX/ppt/images/`，命名 `{页号}-{语义}.{ext}`（如 `01-cover.jpg`），规格 ≥1600px 宽且总 ≤10MB。

**Step 2 · 拷贝模板**：复制 `assets/template.html` → `index.html`；必改 `<title>` 与 `:root` 主题色。

| 主题 | `--ink` | 适合 |
|---|---|---|
| 🖋 墨水经典 | `#0a0a0b` | 通用/商业 |
| 🌊 靛蓝瓷 | `#0a1f3d` | 科技/技术 |
| 🌿 森林墨 | `#1a2e1f` | 自然/文化 |
| 🍂 牛皮纸 | `#2a1e13` | 人文/文学 |
| 🌙 沙丘 | `#1f1a14` | 艺术/设计 |

**Step 3 · 填充内容**：先 Read `template.html` 的 `<style>` 确认类存在（`h-hero / h-xl / h-sub / h-md / lead / stat-card / stat-label / stat-nb / grid-2-7-5 / grid-2-6-6 / grid-2-8-4 / grid-3-3 / pipeline / step / callout / frame-img / kicker` 等，缺则补）。

10 种布局：① 开场封面(Hero dark) ② 章节幕封 ③ 数据大字报(6×stat-card) ④ 左文右图(7:5) ⑤ 图片网格(3×2) ⑥ 流水线(5 步) ⑦ 悬念问题 ⑧ 大引用(逐行揭示) ⑨ 并列对比(Before/After) ⑩ 图文混排(8:4)。

**主题节奏（硬规则）**：每页必须带 `light` / `dark` / `hero light` / `hero dark`；连续 3 页以上同主题不允许；8 页以上必须有 ≥1 hero dark + ≥1 hero light。

```
页1 hero dark | 页2 hero light | 页3 dark | 页4 light | 页5 light | 页6 hero dark
页7 dark | 页8 light | 页9 dark | 页10 hero dark
```

**Step 4 · 检查**：大标题必须是衬线；图片网格用 `height:Nvh` 不用 `aspect-ratio`；无 emoji 用 Lucide；衬线/非衬线/等宽分工正确。**Step 5 · 预览**：浏览器打开。

## C · 现代风模式
### Glassmorphism 卡片
```css
.glass-card{backdrop-filter:blur(25px);background:rgba(30,30,30,.4);border:.5px solid rgba(255,255,255,.12);
  border-radius:20px;box-shadow:0 25px 50px -12px rgba(0,0,0,.5)}
.card-green{border-left:3px solid #00E676;box-shadow:0 0 30px rgba(0,230,118,.3)}
```
### Spotlight 商务暗色参考

依赖：Tailwind Play CDN + `motion@12` + Inter。色彩：页面底 `#020617`、卡片底 `rgba(30,41,59,.45)`、边框 `rgba(255,255,255,.06)`；文字 `#f1f5f9` / `#94a3b8` / `#64748b`；Brand `#6366f1`、Brand Light `#818cf8`、Accent `#f59e0b`。

**背景纹理层叠**（底→顶）：`body` → `.grid-bg`(64px 网格线) → `.noise::after`(feTurbulence 噪点) → `.vignette::before`(径向暗角) → `#spotlight`(650px 跟随光, brand .08) → `#spotlight-accent`(350px, accent .04)。

**Spotlight Card**（悬停时鼠标位置径向光 + 边框高亮）：

```css
.spotlight-card{position:relative;background:rgba(30,41,59,.45);border:1px solid rgba(255,255,255,.06);
  border-radius:1rem;overflow:hidden;transition:border-color .3s}
.spotlight-card::before{content:'';position:absolute;inset:0;opacity:0;pointer-events:none;z-index:0;transition:opacity .3s;
  background:radial-gradient(350px circle at var(--cx,50%) var(--cy,50%),rgba(99,102,241,.1),transparent 60%)}
.spotlight-card:hover::before{opacity:1}
.spotlight-card:hover{border-color:rgba(99,102,241,.2)}
```

```js
document.querySelectorAll('.spotlight-card').forEach(card => card.addEventListener('mousemove', e => {
  const r = card.getBoundingClientRect();
  card.style.setProperty('--cx', (e.clientX - r.left) + 'px');
  card.style.setProperty('--cy', (e.clientY - r.top) + 'px');
}));
```

**数据指标卡**：`text-4xl font-900 bg-gradient-to-br from-brand to-brand-light bg-clip-text text-transparent glow`（`.glow{text-shadow:0 0 40px rgba(99,102,241,.3)}`）。
**柱状图生长动画**：初始 `height:0` + `data-h`，进入页面后 JS 设 `style.height = dataset.h`，配 `transition-all duration-1000`。

### 幻灯片系统

`.slide{position:absolute;inset:0;opacity:0;pointer-events:none;transition:none}`；`.active{opacity:1;pointer-events:auto}`。方向性过渡状态：`enter-right`(translateX(80px)) / `exit-left`(-80px) / `enter-left`(-80px) / `exit-right`(80px)。

```js
// 位移 80px；isTransitioning 防抖；解锁延迟 600ms（略大于过渡时长）
let isTransitioning = false;
function transitionTo(nextIndex, direction) {
  if (isTransitioning || nextIndex === current || nextIndex < 0 || nextIndex >= total) return;
  isTransitioning = true;
  const next = direction === 'next';
  slides[current].className = next ? 'slide exit-left' : 'slide exit-right';
  slides[nextIndex].className = next ? 'slide enter-right' : 'slide enter-left';
  void slides[nextIndex].offsetWidth; // 强制回流
  requestAnimationFrame(() => {
    slides[current].className = 'slide'; slides[nextIndex].className = 'slide active';
    current = nextIndex; setTimeout(() => { isTransitioning = false; }, 600);
  });
}
// go(dir) → transitionTo(current+dir, dir>0?'next':'prev')；goTo(i) → transitionTo(i, i>current?'next':'prev')
```

**导航 + 进度条**：底部毛玻璃 `.nav-bar`（prev + dots + `1 / 6` + next），顶部渐变 `.progress-bar`（`linear-gradient(90deg,#6366f1,#8b5cf6,#a78bfa)`）；`.nav-dot.active{background:#818cf8;width:20px;border-radius:3px}`。

**双层聚光灯 + 自定义光标**：`#spotlight`(650px, z-50) / `#spotlight-accent`(350px, z-49)；小飞机 SVG 光标 `body{cursor:none}` + `#cursor-plane`。鼠标静止 **2.5s** 后聚光灯与光标一起淡出隐藏；页面加载时聚光灯居中显示、光标隐藏。

**Motion 入场编排**（每页复用，`Motion.stagger`）：

| 元素 | 起始 | 目标 | 时长 | 延迟 |
|---|---|---|---|---|
| 标签 label | `opacity:0, y:15` | `opacity:1, y:0` | 0.4s | 0 |
| 标题 title | `opacity:0, y:20` | `opacity:1, y:0` | 0.5s | 0.08s |
| 卡片/指标 | `opacity:0, y:25` | `opacity:1, y:0` | 0.5s | 0.15s +0.1s 递增 |
| 时间线节点 | `opacity:0, y:20` | `opacity:1, y:0` | 0.4s | 0.12s +0.08s 递增 |

缓动：`ease-out`(默认) / `ease-in` / `ease-in-out` / `circ-in` / `circ-out` / `back-in` / `back-out` / `anticipate`。

**交互清单**：键盘（→/↓/Space 下一页，←/↑ 上一页）、触摸滑动（`touchstart`/`touchend` 阈值 50px）、鼠标/卡片聚光灯（更新 `--x/--y`、`--cx/--cy`）、圆点跳转 `goTo(index)`、进度条 `style.width = ((current+1)/total*100)+'%'`。

**页面结构模板**：① 封面 ② 核心要点(3 列 spotlight-card) ③ 数据指标(4 列 + 柱状图) ④ 时间线(发光节点) ⑤ 引用页 ⑥ 致谢。

## 框架选择

- 开发者演示 → **Slidev**；标准幻灯片 → **Reveal.js**（Markdown、3D 过渡）；Prezi 式非线性 → **Impress.js**；终端 → **Asciinema**；代码演示 → **Monaco Editor**；图表 → **Mermaid.js**；代码高亮 → **Shiki**；3D → **Three.js**。

## 模式选择决策树

```
用户要做幻灯片
├── 快速出 deck、多种风格可选？ → A 模板库：读 index.json → 匹配3个 → 预览 → 选定 → 生成
├── 杂志风电子墨水美学(WebGL/衬线/横向翻页)？ → B：复制 template.html → 选主题 → 填充 → 检查
├── 现代科技感(毛玻璃/聚光灯/粒子/暗色)？ → C：用 Spotlight/Glassmorphism 组件构建
├── 交互特效(代码编辑/3D/终端/图表)？ → Slidev/Reveal.js + 对应组件
└── 要模板的设计 + 自定义交互？ → 混合：A 做视觉基础，注入 B/C 的交互组件（保留字体/色彩/装饰）
```

## 核心设计原则

1. **克制优于炫技** —— WebGL 只在 hero 页透出。
2. **结构优于装饰** —— 大字号 + 字体对比 + 网格留白。
3. **字体分工** —— 衬线=标题，非衬线=正文，等宽=元数据。
4. **图片只裁底部** —— 网格用固定高度，绝不用 `aspect-ratio`。
5. **节奏靠 hero 页** —— hero 与 non-hero 交替。

## 交付前质检

```
□ 模板字体未被替换（Google Fonts import 完整）      □ 色板 CSS 变量未被修改
□ 装饰元素（角标/纹理/SVG）保留完整               □ deck-stage.js 已复制到输出目录
□ 页码已更新（增删幻灯片后）                      □ 新增页符合模板设计系统
□ 键盘导航正常（←/→/Space/Home/End）            □ 浏览器预览正常
□ 类名已在 template.html 定义                     □ 主题节奏满足硬规则
□ 无 emoji，用 Lucide 图标                        □ 图片用 height:Nvh 且只裁底部
□ 衬线/非衬线/等宽分工正确                        □ 方向性过渡与聚光灯空闲隐藏正常
□ 进度条宽度与实际页数匹配                        □ 触摸滑动阈值 50px 正常
```
