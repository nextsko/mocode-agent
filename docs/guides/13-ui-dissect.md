# UI 审查与复刻：庖丁解牛工作流

`ui_dissect` 把 mocode 的原生浏览器能力从「点选/截图」升级为「结构理解」：
它测量真实页面的每一个可见盒子，切成组件，评估审美，提取设计令牌，
并把结果沉淀成可复用的资产。

## 能力分层

| 层 | 产物 | 说明 |
|---|---|---|
| 采集 | `Raw` | 通过 CDP `Runtime.evaluate` 注入 `uidissect.CollectorScript`，拿到 DOM 路径 / 盒子 / 计算样式 / 可访问性字段 |
| 地图 | `Map` | 文档序结构树 + 组件切片 + 审美 findings + 设计令牌 |
| 审查 | findings | 对比度、字号阶梯、间距节奏、色板膨胀、触控目标、溢出裁切、alt/标签缺失、h1 数量 |
| 复刻 | HTML+CSS skeleton | 按组件子树生成语义 HTML + 变量化 CSS，作为二次创作起点 |
| 沉淀 | `.mocode/ui/` | 每次 map 存 JSON + Markdown 并追加 `index.md`；replicate 存 `components/*.html` |

## 工具用法

```text
ui_dissect { "action": "map", "url": "https://example.com/pricing" }
ui_dissect { "action": "review", "url": "https://example.com/pricing" }
ui_dissect { "action": "map" }                 # 复用 browser 工具已打开的页面
ui_dissect { "action": "replicate", "component": "c3" }
ui_dissect { "action": "list" }
```

- `map` 默认保存；`save: false` 只返回、不落盘。
- `replicate` 需要先 `map`（同一次调用会先测量，再按 `cN` 生成）。
- 测量是只读的；只有显式给 `url` 才会导航。

## 组件切片规则

1. **重复兄弟**：同父、同 tag+class 签名、尺寸相近（±30%）且 ≥3 个 → 一个 `card`/`list-item` 组件，`Repeat` 记录数量。
2. **地标**：`header/nav/main/aside/footer/section/article/form/dialog` 且宽度 ≥15% 视口 → 独立组件。
3. **大区块兜底**：depth ≤1 且面积 ≥3% 视口 → `region`。

## 审美规则（第一版）

| 类别 | 规则 | 级别 |
|---|---|---|
| typography | 文本字号种类 >8；最小字号 <12px | warn |
| spacing | 间距去重后 >10 种 | warn |
| color | 同页颜色 >20 种 | warn |
| contrast | 文本与最近祖先背景对比度 <4.5:1（WCAG AA） | error |
| touch-target | 可交互元素任一边 <32px | warn |
| overflow | `scrollWidth > clientWidth` | warn |
| a11y | img 无 alt；button/a 无可访问标签 | warn/error |
| hierarchy | 多个 h1 | info |
| layout | 所有盒子都满宽（单列） | info |

规则集中在 `internal/core/uidissect/analyze.go`，新增规则只需追加 finding。

## 与已有能力的关系

- **browser**：同一 `Manager` 会话，`browser` 负责动作（goto/click/fill），`ui_dissect` 负责理解（测量/切片/审查/复刻）。
- **agentic_fetch**：负责「读内容」；`ui_dissect` 负责「读界面」。
- **learn / memory**：复刻后的组件与设计决策可通过 learn 沉淀为 skill，通过 memory 记录取舍原因。

## 资产布局

```text
.mocode/ui/
  index.md
  20260101T120000-example-com.md
  20260101T120000-example-com.json
  components/20260101T120500-card-c3.html
```

## 后续扩展

- 组件相似度聚类（跨页面沉淀设计系统）
- 截图 + 视觉回归（对照 map 的盒子差异）
- 设计令牌对比（当前页面 vs 已沉淀的品牌令牌）
- 生成 React/Vue/Svelte 目标框架的复刻骨架
