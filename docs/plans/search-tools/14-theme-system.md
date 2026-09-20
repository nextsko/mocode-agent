# 子计划 14 — Theme 系统

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：13-structured-query-parser（无关，但需排在 Renderer 之前）
> **后续被依赖**：15-tui-rendering（Renderer 注入 Theme）

---

## 目标

实现 mocode Go 版的 Theme 系统：颜色 + 边框 + 树形符号 + 状态颜色映射 + 折叠语义 + 时间格式化 + OSC 8 链接。

## 范围

### 必须实现

1. **Theme 接口**
   ```go
   type Theme interface {
       FG(color ThemeColor, text string) string   // fg(color, text)
       BG(color ThemeColor, text string) string
       StyledSymbol(tool, fallback string) string  // tool-specific symbol
       BoxRound() BoxGlyphs                        // horizontal/vertical/topLeft/...
       Tree() TreeGlyphs                           // branch/last
       Sep() SepGlyphs                             // dot
       StatusIcon(state State) string
       StatusColor(state State) ThemeColor
       BorderColor(state State) ThemeColor
   }
   ```

2. **DefaultTheme（dark/light 双模式）**
   - lipgloss AdaptiveColor 实现自动切换
   - 颜色枚举：accent/text/dim/muted/error/warning/success/toolTitle/borderMuted
   - 边框字符集：U+2500 系列（horizontal/vertical/topLeft/topRight/bottomLeft/bottomRight/teeRight/teeLeft）

3. **状态可视化**
   - State 类型：pending/running/success/warning/error
   - statusIcon 映射（pending: ◐, running: ◑, success: ✓, warning: ⚠, error: ✗）
   - 状态→border color 映射：success=dim（故意弱化）, running/pending=accent, warning=warning, error=error
   - renderStatusLine 单一入口（icon + title + description + meta）

4. **折叠/展开语义**
   - `MAX_COLLAPSED_ITEMS = 8`
   - `FormatExpandHint(expanded, hasMore)` 输出 `▾ / ▴` + 提示文案
   - `FormatMoreItems(remaining, itemType)` 输出 `… N more line/source/file`

5. **时间格式化**
   - `FormatAge(ageSeconds)` 5 桶分桶：`just now` (< 60s) / `5m ago` (< 1h) / `2h ago` (< 1d) / `2d ago` (< 7d) / `3w ago` (< 30d) / `5mo ago` (30d 近似月) / `2y ago`
   - 与 `publishedDate` 字符串的 fallback

6. **域提取**
   - `GetDomain(url)` 提取 host，去 `www.` 前缀

7. **OSC 8 链接**
   - `Hyperlink(url, text)` 输出 `\x1b]8;;{url}\x1b\\{text}\x1b]8;;\x1b\\`
   - 用于 Sources 段落标题点击

8. **宽度自适应**
   - `TruncateToWidth(s, width)` ANSI-aware 截断（推荐 `charmbracelet/x/ansi.Truncate`）
   - `VisibleWidth(s)` ANSI-aware 宽度计算
   - `LineBudget(width)` → `Math.max(24, width - 6)`
   - `TitleBudget(lineBudget, metaSuffix)` → `Math.max(12, lineBudget - metaSuffix)`

## 文件清单

```
pkg/search/ui/theme/
├── theme.go              # Theme 接口 + DefaultTheme
├── colors.go             # 颜色枚举 + AdaptiveColor 切换
├── symbols.go            # 边框 + 树形 + 分隔符
├── status.go             # State + renderStatusLine + statusIcon
├── collapse.go           # FormatExpandHint + FormatMoreItems
├── format.go             # FormatAge + GetDomain
├── hyperlink.go          # OSC 8 Hyperlink
├── width.go              # TruncateToWidth + VisibleWidth + LineBudget + TitleBudget
└── tests/
    ├── theme_test.go
    ├── format_test.go    # FormatAge 5 桶
    └── width_test.go     # ANSI 宽度边界
```

## 验收标准

- [ ] Theme 接口 5 个实现（DefaultDark / DefaultLight / NoColor / HighContrast / MockTest）
- [ ] FormatAge 5 桶分桶单测覆盖（0s/30s/2m/5h/3d/2w/5mo/2y）
- [ ] OSC 8 hyperlink 在 kitty/iTerm2 中可点击
- [ ] TruncateToWidth 处理 ANSI 颜色码不截断（vs VisibleWidth 正确计算）
- [ ] dark/light 模式在 lipgloss AdaptiveColor 下自动切换

## 工期估

1.5 个工作日

## 风险

- **lipgloss 不自动 ellipsis**：必须用 `charmbracelet/x/ansi.Truncate`
- **Terminal 兼容性**：OSC 8 链接在 Windows Terminal / 老旧 xterm 不支持
- **mattn/go-runewidth 宽度计算**：CJK 字符宽度边界

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：Theme 接口 → 5 个实现 → 各工具函数（status / collapse / format / hyperlink / width）
> 2. **至底向上搞实现**：colors → symbols → status → collapse → format → hyperlink → width → theme 主类
> 3. **向上发展作拼装**：DefaultTheme 通过 embed 各子模块拼装
> 4. **逐层验证和仿真**：colors 单测 → symbols 单测 → status 单测 → collapse 单测 → format 单测（5 桶覆盖）→ hyperlink 渲染测试 → width ANSI 边界测试
> 5. **铁律约束 LLM 超边界**：只允许创建 `pkg/search/ui/theme/` 目录下文件

## 执行模型（推荐）

- **主执行模型**：**Haiku**（标准化的 UI 工具函数，无复杂业务逻辑）
- **辅助模型**：无

## 相关笔记

- [[ui-theme-system]] — 完整 7 维度分析
- [[MASTER_PLAN]]