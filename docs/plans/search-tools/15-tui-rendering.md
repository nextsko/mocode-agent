# 子计划 15 — TUI 渲染（OutputBlock + TreeList + Markdown + WebSearch Renderer）

> [!info] 元信息
> **状态**：pending
> **创建日期**：2026-09-14
> **所属计划**：[[MASTER_PLAN]] Web Search Provider 链吸收
> **依赖**：13-structured-query-parser / 14-theme-system
> **后续被依赖**：无

---

## 目标

实现 mocode Go 版的 Web Search TUI 渲染：OutputBlock 带边框容器 + TreeList 树形折叠 + Markdown 答案渲染 + ToolRenderer 入口。

## 范围

### 必须实现

1. **OutputBlock 容器**
   ```go
   type OutputBlockOptions struct {
       Header     string
       HeaderMeta string
       State      State
       Sections   []Section  // {Label string, Lines []string, Separator bool}
       Width      int
       ApplyBG    bool
   }
   type OutputBlock interface {
       Render(opts OutputBlockOptions) []string  // 输出 N 行带边框的字符串
   }
   ```
   - Border 颜色映射：success=dim（视觉弱化），running/pending=accent，warning=warning，error=error
   - Header bar + Section bar（teeRight/teeLeft 字符）
   - CachedOutputBlock 缓存键（width + sections hash）
   - 自适应宽度（wrapTextWithAnsi(contentWidth)）

2. **TreeList 树形列表**
   - tree 结构（branch/last/dim/dot）
   - MAX_COLLAPSED_ITEMS 默认 8
   - 折叠/展开语义（head/tail 双截断）
   - renderItem 单行返回（稳定行数）
   - title + URL 链接化（OSC 8）

3. **Markdown 答案渲染**
   - glamour 渲染
   - compact 模式 maxAnswerLines cap
   - 「No answer text returned」降级

4. **Metadata 段落**
   - Provider 标识（model @ label + auth mode OAuth/API key）
   - Usage 指标（in/out/total/search requests）
   - sep.dot 分隔符

5. **Sources 段落**
   - title (link) + (domain) · age
   - TruncateToWidth 预算计算（lineBudget - metaSuffix）
   - 「No sources returned」降级

6. **WebSearchRenderer 入口**
   ```go
   type WebSearchRenderer interface {
       RenderCall(args map[string]any) Component
       RenderResult(result SearchResult, options RenderOptions) Component
       MergeCallAndResult() bool
   }
   ```
   - `webSearchToolRenderer` 三件套
   - 错误降级路径（framed error panel vs simple text fallback）

## 文件清单

```
pkg/search/ui/
├── output_block.go       # OutputBlock + CachedOutputBlock + framedBlock
├── tree_list.go          # TreeList + head/tail 双截断
├── markdown.go           # glamour 渲染 + compact mode cap
├── status_line.go        # renderStatusLine
├── web_search_renderer.go  # WebSearchRenderer 入口
└── tests/
    ├── output_block_test.go
    ├── tree_list_test.go
    └── renderer_test.go
```

## 验收标准

- [ ] OutputBlock 5 态 border color 单测覆盖
- [ ] CachedOutputBlock 缓存命中率验证
- [ ] TreeList 折叠展开（head/tail 双截断）
- [ ] WebSearchRenderer 真实 SearchResponse 渲染（金标准）
- [ ] 错误降级：搜索失败 → framed error panel（保持布局一致）
- [ ] 宽度自适应：80/120/200 列布局正确

## 工期估

2.5 个工作日

## 风险

- **Bubble Tea Model 协议**：tea.Msg / tea.Cmd 需要与现有 mocode TUI 对接
- **glamour 主题切换**：dark/light 必须随 Theme 自动切换
- **mattn/go-runewidth**：ANSI 宽度计算的边界（emoji、CJK、组合字符）
- **mergeCallAndResult**：与 mocode 现有 ToolExecutionContainer 的对接

## 设计原则（铁律）

> [!warning] 本计划必须严格遵循
> 1. **置顶向下作规划**：OutputBlock → TreeList → Markdown → StatusLine → Renderer 主类
> 2. **至底向上搞实现**：`output_block.go`（最底层容器）→ `tree_list.go` → `markdown.go` → `status_line.go` → `web_search_renderer.go`（拼装上层）
> 3. **向上发展作拼装**：`WebSearchRenderer` 通过组合 OutputBlock + TreeList + Markdown + StatusLine 拼装；Theme 通过依赖注入传入
> 4. **逐层验证和仿真**：OutputBlock mock sections 单测 → TreeList 折叠单测 → Markdown 渲染快照 → Renderer 端到端快照测试（golden file）
> 5. **铁律约束 LLM 超边界**：只允许创建 `pkg/search/ui/` 目录下文件；禁止修改 `pkg/search/query/` 与 `pkg/search/ui/theme/`

## 执行模型（推荐）

- **主执行模型**：**Sonnet**（多组件拼装 + 视觉一致性是难点）
- **辅助模型**：Haiku（生成 golden file 快照 fixture）

## 相关笔记

- [[ui-tui-rendering]] — 完整 8 维度分析
- [[ui-theme-system]] — Theme 系统依赖
- [[ui-structured-query]] — Query 解析依赖
- [[MASTER_PLAN]]