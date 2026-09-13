---
name: excalidraw-diagram
description: >-
  Use when 需要用 Excalidraw 生成可视化流程图、架构图或概念图，把工作流/系统关系画成"会论证"的视觉结构。产出 `.excalidraw` JSON，并用 Playwright 渲染成 PNG 反复自检修正，直到构图、间距、箭头都正确。也适用于把文字描述转成 fan-out、时间线、收敛等有语义的图形。
---

# Excalidraw 图表生成

生成的不是"带框的文字"，而是**视觉论证**：形状本身就承载含义。

> 图表只管视觉结构；配色与品牌一致性参考 `design-taste-frontend`，成品渲染后的可读性/无障碍审查参考 `web-design-guidelines`。

## 0. 核心判据（先自问）

- **Isomorphism Test**：把所有文字删掉，结构本身还能传达概念吗？不能就重画。
- **Education Test**：读者能从图里学到具体东西，还是只看到一堆贴了标签的方框？

## 1. 深度评估（第一步）

| 类型 | 何时用 | 做法 |
|---|---|---|
| Simple/Conceptual | 心智模型、哲学、抽象概念 | 抽象形状 + 关系，不追求细节 |
| Comprehensive/Technical | 真实系统、协议、架构、教学 | 必须有 **evidence artifacts**（真实代码/数据） |

技术图必须先做 **research**：查真实 JSON 格式、事件名、方法名、API 端点，用真实术语而非占位符。

> Bad: `"Protocol" → "Frontend"`
> Good: `"AG-UI 流式事件(RUN_STARTED, STATE_DELTA)" → "CopilotKit createA2UIMessageRenderer()"`

## 2. Evidence Artifacts（技术图必带）

| 类型 | 用途 | 渲染方式 |
|---|---|---|
| Code snippet | API / 集成细节 | 深色矩形 + 语法着色文字 |
| JSON/数据示例 | 数据格式、payload | 深色矩形 + 彩色文字 |
| Event/step 序列 | 协议、工作流、生命周期 | 时间线（line + dots + labels） |
| UI mockup | 展示实际输出 | 嵌套矩形模拟真实 UI |
| Real input | 展示"输入长什么样" | 矩形内放真实内容样例 |

原则：**展示东西实际长什么样，而不只是它叫什么。**

## 3. Multi-Zoom 架构

综合图要同时具备三层：**Level 1 概要流**（`Input → Processing → Output`）、**Level 2 分区边界**（按职责/阶段/团队分组的"房间"）、**Level 3 分区内细节**（evidence artifacts）。概要给上下文，分区做组织，细节负责教学。

## 4. Container vs Free-Floating Text

默认**自由文本**，只在有目的时才加容器。

| 用容器 | 用自由文本 |
|---|---|
| 是分区焦点 / 需要分组 | 只是标签、描述、元信息 |
| 箭头要连到它 / 形状本身有含义 | 靠近某物做说明，排版已形成层级 |

**Container test**：每个带框元素问一句"当自由文本行不行？"能行就去框。目标：**<30% 的文字元素带容器**。

## 5. 设计流程（生成 JSON 前）

1. **Assess depth**：simple 还是 comprehensive？comprehensive 先 research。
2. **Understand deeply**：每个概念**做什么**？什么关系？核心变换是什么？读者**必须看到**什么？
3. **Map concepts → patterns**：

| 概念行为 | 模式 |
|---|---|
| 一→多 | Fan-out（放射箭头） |
| 多→一 | Convergence（漏斗/汇聚） |
| 层级/嵌套 | Tree（line + 自由文本，不要盒子） |
| 步骤序列 | Timeline（line + 小圆点 + 标签） |
| 循环迭代 | Spiral/Cycle（箭头回到起点） |
| 抽象状态/上下文 | Cloud（重叠 ellipse） |
| 输入→输出变换 | Assembly line（before → process → after） |
| 对比两物 | Side-by-side |
| 阶段分隔 | Gap/Break（留白/分隔线） |

4. **Ensure variety**：多个概念时，每个主要概念用**不同的**视觉模式，禁止统一卡片网格。
5. **Sketch flow**：先在心里走一遍视线路径。
6. **Generate JSON**。
7. **Render & Validate**（强制，见 §9）。

## 6. 大图策略：分区构建

综合图**必须一段一段 build**，不要一次生成整个文件（会超输出上限且质量更差）。

- Phase 1：创建含 JSON wrapper 的 base 文件，然后**每次 edit 加一个 section**。
- 用**描述性 string ID**（`trigger_rect`、`arrow_fan_left`），跨 section 引用可读。
- **按 section 分配 seed 号段**（section 1 用 100xxx，section 2 用 200xxx）。
- 新元素要 bind 旧元素时，**同时**更新旧元素的 `boundElements`。
- Phase 2：通读全 JSON，检查跨 section 箭头两端 binding、间距平衡、ID 引用是否存在。
- Phase 3：渲染验证。

**不要**：一次生成全图、交给 coding agent 生成、写 Python 生成脚本（坐标数学引入的间接层更难 debug）。

## 7. Shape / Lint / 配色

| 概念 | 形状 |
|---|---|
| 标签、描述、分区标题 | **none**（自由文本，靠字号/字重分层） |
| 时间线标记 | 小 `ellipse`（10–20px） |
| 开始/触发/输入、结束/输出 | `ellipse` |
| 决策/条件 | `diamond` |
| 过程/动作 | `rectangle` |
| 抽象状态 | 重叠 `ellipse` |
| 层级节点 | line + text（不用盒子） |

配色（语义化，一处定义，别自造新色）：

| 语义 | Fill | Stroke |
|---|---|---|
| Primary/Neutral | `#3b82f6` | `#1e3a5f` |
| Secondary | `#60a5fa` | `#1e3a5f` |
| Start/Trigger | `#fed7aa` | `#c2410c` |
| End/Success | `#a7f3d0` | `#047857` |
| Decision | `#fef3c7` | `#b45309` |
| AI/LLM | `#ddd6fe` | `#6d28d9` |
| Warning | `#fee2e2` | `#dc2626` |
| Error | `#fecaca` | `#b91c1c` |

文字层级：Title `#1e40af` / Subtitle `#3b82f6` / Detail `#64748b` / 浅底文字 `#374151` / 深底文字 `#ffffff`。Evidence artifact 背景 `#1e293b`（代码），JSON 文字 `#22c55e`。

**观感**：`roughness: 0`（现代）/ `1`（手绘）；`strokeWidth` 1 细线、2 标准、3 强调；**所有元素 `opacity: 100`**（靠颜色/字号/线宽分层，不用透明度）；画布 `#ffffff`。

**布局**：Hero 300×150 / Primary 180×90 / Secondary 120×60 / Small 60×40；最重要的元素周围留 200px+ 空白；视线一般左→右或上→下；**有关系就必须画箭头**，位置本身不表达关系。

## 8. JSON 结构与模板

```json
{
  "type": "excalidraw",
  "version": 2,
  "source": "https://excalidraw.com",
  "elements": [],
  "appState": { "viewBackgroundColor": "#ffffff", "gridSize": 20 },
  "files": {}
}
```

- 文本：`"text"` 只放可读文字，`originalText` 相同；`fontSize: 16`，`fontFamily: 3`，`textAlign`，`verticalAlign`，`lineHeight: 1.25`。
- 箭头：`points: [[0,0],[dx,dy]]`，`startBinding`/`endBinding` 为 `{"elementId","focus":0,"gap":2}`，`endArrowhead:"arrow"`；用 3+ points 画曲线。
- 圆角矩形：`"roundness": {"type": 3}`。
- line（结构性连线）：`type:"line"` + `points`，用于时间线、树、分隔线。
- 小圆点：`ellipse` 10–20px，fill=stroke=marker 色。

## 9. Render & Validate（强制循环）

无法只看 JSON 判断成品，必须渲染看图再修：

```bash
cd internal/core/skills/builtin/excalidraw-diagram/references
uv sync && uv run playwright install chromium   # 首次
uv run python render_excalidraw.py <file.excalidraw>
```

1. Render 出 PNG → Read 该 PNG。
2. **对照原始设计**：结构是否匹配概念？每段是否用了预期模式？视线顺序对吗？层级对吗？evidence 可读吗？
3. **查缺陷**：文字溢出/被裁、元素重叠、箭头穿框、箭头落点错误、标签锚点不清、间距不均、局部过挤或过空、字太小、构图失衡。
4. **Fix**：加宽容器、调 `x/y`、给箭头加中间 waypoint 绕行、把标签靠近对象、重配视觉重量。
5. Re-render → Re-view，重复至通过，通常 **2–4 轮**。不要一轮没 bug 就收工。

**停止条件**：渲染结果符合设计、无裁切/重叠/不可读、箭头干净且连对、间距一致构图平衡。

## 10. 质量清单（交付前）

- [ ] 技术图：有 research + evidence artifacts + multi-zoom + 具体内容
- [ ] Isomorphism：结构与概念行为一致；每概念用不同模式；无统一卡片网格
- [ ] 容器比 <30%；树/时间线用 line + text；字体分层有效
- [ ] 每个关系都有连线；视线路径清晰；重要元素更大更孤立
- [ ] `text` 仅可读文字；`fontFamily: 3`；`roughness: 0`；`opacity: 100`
- [ ] 已渲染 PNG 并目视检查：无溢出、无重叠、间距均匀、箭头落点正确、渲染尺寸下可读、构图均衡
