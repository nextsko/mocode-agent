# 04 · 组件 API 调用清单 + 优秀设计洞察

> 核心问题：每个组件用了哪些 API？这些 API 有什么"用得好的"小技巧？哪些设计点值得借鉴？

---

## 一、API 调用全景图

```
┌─────────────────────────────────────────────────────────────┐
│ Bubble Tea v2 (tea)                                         │
│   tea.NewProgram, tea.Model (Init/Update/View),             │
│   tea.Cmd, tea.Msg, tea.Batch, tea.Tick, tea.Sequence       │
│   tea.WindowSizeMsg, tea.KeyMsg, tea.MouseMsg                │
│   tea.MouseModeAllMotion, tea.MouseModeCellMotion            │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ Ultraviolet (uv)                                            │
│   uv.NewScreenBuffer(w, h), uv.Screen                        │
│   uv.Rectangle (= image.Rectangle)                           │
│   uv.NewStyledString(s).Draw(scr, rect)                      │
│   screen.Clear(scr), screen.Fill(scr, char, style)           │
│   layout.Vertical/Horizontal, layout.Len/Fill/Ratio/Split    │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ Lip Gloss v2                                                │
│   lipgloss.NewStyle(), lipgloss.Style (链式 .Foreground...) │
│   lipgloss.Render(s), lipgloss.Width(s), lipgloss.Height(s)  │
│   lipgloss.JoinVertical/Horizontal                           │
│   lipgloss.Blend1D(n, c1, c2) ← 渐变 ramp                   │
│   lipgloss.Place(w, h, hPos, vPos, content)                  │
│   lipgloss/tree.Root(s).Child(c).Enumerator(fn)              │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ Glamour v2                                                  │
│   glamour.NewTermRenderer(opts...)                           │
│   renderer.Render(markdown) → ANSI                          │
│   WithStyles(ansiConfig), WithWordWrap(w)                    │
│   WithChromaFormatter(name) ← 配合 xchroma 注册自定义        │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ Bubbles v2                                                  │
│   bubbles/textarea, bubbles/textinput, bubbles/filepicker    │
│   bubbles/help, bubbles/key, bubbles/spinner                │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ x/ansi (字符串处理，必须用，不能 []byte 操作)                  │
│   ansi.StringWidth(s), ansi.Strip(s), ansi.Truncate(s, n, …) │
│   ansi.Cut(s, i, j)                                          │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ charmtone (语义调色板)                                       │
│   Bok, Citron, Blush, Dolly, Charple, …                      │
│   Ash, Squid, Smoke, Oyster (fg)                             │
│   Pepper, BBQ, Charcoal, Iron (bg)                           │
└─────────────────────────────────────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────────┐
│ x/chroma (语法高亮)                                          │
│   xchroma.Formatter(name, nil) ← 注册自定义 formatter       │
│   glamour 内部调用                                           │
└─────────────────────────────────────────────────────────────┘
```

---

## 二、各组件的 API 用法详解

### 2.1 `model/ui.go`（核心）

#### WindowSize 处理

```go
case tea.WindowSizeMsg:
    u.width, u.height = msg.Width, msg.Height
    u.textarea.SetWidth(msg.Width)  // 立即同步
    return u, nil
```

**技巧**：resize 时**只更新尺寸，不重算布局**——布局在下次 `View()` 时算。

#### Pub/Sub 订阅

```go
case pubsub.Event[message.Message]:
    switch msg.Type {
    case pubsub.CreatedEvent:
        u.appendSessionMessage(msg.Payload)
    case pubsub.UpdatedEvent:
        u.updateSessionMessage(msg.Payload)
    case pubsub.DeletedEvent:
        u.chat.RemoveMessage(msg.Payload.ID)
    }
```

**技巧**：用**泛型** `pubsub.Event[T]` 携带类型安全的数据。

#### Focus 切换

```go
case tea.MouseClickMsg:
    return u, u.handleClickFocus(msg)

func (u *UI) handleClickFocus(msg tea.MouseClickMsg) tea.Cmd {
    if isInRect(msg, u.layout.editor) {
        u.focus = uiFocusEditor
        u.textarea.Focus()
    } else if isInRect(msg, u.layout.main) {
        u.focus = uiFocusMain
        u.textarea.Blur()
    }
    return nil
}
```

**技巧**：鼠标点哪个矩形区域就 focus 哪个区域；矩形复用，无坐标硬编码。

#### Send Message（关键路径）

```go
func (u *UI) send() tea.Cmd {
    value := u.textarea.Value()
    if strings.TrimSpace(value) == "" { return nil }

    // 1. 持久化到 history（异步）
    cmd := u.sendMessage(value)

    // 2. 立即清空 textarea + 重置 history 索引
    u.textarea.Reset()
    u.historyReset()

    // 3. 滚动到底部
    u.chat.ScrollToBottomAndAnimate()

    // 4. 重置 attachments
    u.attachments.Reset()

    return cmd
}
```

**技巧**：用户感知延迟优先——先清空 UI，再异步发送。

---

### 2.2 `chat/assistant.go`（AssistantMessageItem）

#### Anim 配置

```go
a.anim = anim.New(anim.Settings{
    ID:          a.ID(),
    Size:        15,
    GradColorA:  sty.WorkingGradFromColor,
    GradColorB:  sty.WorkingGradToColor,
    LabelColor:  sty.WorkingLabelColor,
    CycleColors: true,  // ← 3 stop 渐变，A→B→A 滚动
})
```

#### 思考块折叠

```go
func (a *AssistantMessageItem) renderThinking(width int) string {
    src := a.message.ReasoningContent().Thinking
    if !a.thinkingExpanded && strings.Count(src, "\n") > 10 {
        // 折叠：只显示最后 10 行
        lines := strings.Split(src, "\n")
        src = strings.Join(lines[len(lines)-10:], "\n")
        src += "\n" + sty.Messages.ThinkingTruncationHint.Render("(Ctrl+T to expand)")
    }
    return common.QuietMarkdownRenderer(a.sty, width).Render(src)
}
```

#### Per-section Cache

```go
type assistantSection struct {
    width  int
    srcHash uint64  // FNV-64 hash of source
    extra  uint64  // 额外状态（viewMode 等折叠）
    out    string
    aux    any  // 自定义辅助数据
    valid  bool
}

func (s *assistantSection) hit(width int, srcHash, extra uint64) bool {
    return s.valid && s.width == width && s.srcHash == srcHash && s.extra == extra
}
```

**技巧**：每个 section（thinking/content/error）独立缓存；流式 content 更新不 invalidate thinking。

#### Hash 防碰撞（重要）

```go
func fnvFields(fields ...[]byte) uint64 {
    h := fnv.New64a()
    var lenBuf [8]byte
    for _, f := range fields {
        binary.LittleEndian.PutUint64(lenBuf[:], uint64(len(f)))
        h.Write(lenBuf[:])
        h.Write(f)
    }
    return h.Sum64()
}
```

**技巧**：长度前缀防止 `["ab", "c"]` 和 `["a", "bc"]` 碰撞。

---

### 2.3 `chat/tools.go`（ToolMessageItem）

#### Tool 派发

```go
func NewToolMessageItem(com *common.Common, msg *message.Message, call message.ToolCall) ToolMessageItem {
    base := &baseToolMessageItem{
        com: com, msg: msg, toolCall: call,
        anim: anim.New(anim.Settings{
            ID: msg.ID + ":" + call.ID,
            Size: 10,
            GradColorA: com.Styles.WorkingGradFromColor,
            GradColorB: com.Styles.WorkingGradToColor,
            LabelColor: com.Styles.WorkingLabelColor,
        }),
    }

    switch call.Name {
    case "bash", "job_output", "job_kill":
        return &BashToolMessageItem{baseToolMessageItem: base, ...}
    case "view", "write", "edit", "multi_edit", "download":
        return &FileToolMessageItem{baseToolMessageItem: base, ...}
    // ... 30+ tool types
    default:
        return &GenericToolMessageItem{baseToolMessageItem: base}
    }
}
```

**技巧**：工厂函数 + 嵌入共享 base + switch 分派；新增 tool 只需加 case。

#### Status 颜色映射

```go
func (b *baseToolMessageItem) toolIcon(sty *styles.Styles) string {
    switch b.status {
    case ToolAwaitingPermission: return sty.Tool.IconPending.Render(QuestionMarkIcon)
    case ToolRunning:            return sty.Tool.IconPending.Render(ArrowIcon)
    case ToolSuccess:            return sty.Tool.IconSuccess.Render(CheckIcon)
    case ToolError:              return sty.Tool.IconError.Render(CrossIcon)
    case ToolCanceled:           return sty.Tool.IconCancelled.Render(CircleIcon)
    }
}
```

---

### 2.4 `chat/agent.go`（AgentToolMessageItem）

#### Tree + Custom Enumerator

```go
import "charm.land/lipgloss/v2/tree"

childTools := tree.Root(header)
for _, nested := range r.agent.nestedTools {
    childTools.Child(nested.Render(width))
}
result := childTools.Enumerator(roundedEnumerator(2, 8)).String()
```

```go
func roundedEnumerator(lPadding, width int) tree.Enumerator {
    if width == 0 { width = 2 }
    if lPadding == 0 { lPadding = 1 }
    return func(children tree.Children, index int) string {
        line := strings.Repeat("─", width)
        padding := strings.Repeat(" ", lPadding)
        if children.Length()-1 == index {
            return padding + "╰" + line
        }
        return padding + "├" + line
    }
}
```

#### Panel 注入（全局变量模式）

```go
// chat/tools.go
var toolPanelView *panel.View
var agentPanelResolver func(...) []panel.AgentPanelData

func SetToolPanelView(pv *panel.View) { toolPanelView = pv }
func SetAgentPanelResolver(resolver ...) { agentPanelResolver = resolver }
```

```go
// ui.go 启动时
chat.SetToolPanelView(com.Panels)
chat.SetAgentPanelResolver(u.agentTaskPanelsForRender)
```

**设计模式**：`chat` 包不直接 `import` `panel`，通过全局 setter 注入，避免循环依赖。

---

### 2.5 `dialog/dialog.go`

#### Dialog 接口

```go
type Dialog interface {
    ID() string
    HandleMsg(msg tea.Msg) Action  // Action is `any`，返回 typed Action
    Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor
}

type Overlay struct {
    dialogs []Dialog
}

func (o *Overlay) Update(msg tea.Msg) Action {
    if len(o.dialogs) == 0 { return nil }
    return o.dialogs[len(o.dialogs)-1].HandleMsg(msg)  // 只有栈顶收消息
}

func (o *Overlay) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    var cursor *tea.Cursor
    for _, d := range o.dialogs {
        cursor = d.Draw(scr, area)
    }
    return cursor
}
```

**技巧**：
- **Action 是 `any`**：每个 dialog 自定义 Action 类型，UI 端 switch 处理。
- **只有栈顶收消息**：避免子 dialog 干扰父 dialog。
- **所有 dialog 都画**：背景虚化由 dialog 自己处理。

---

### 2.6 `dialog/commands.go`

#### FilterableList 模式

```go
list := list.NewFilterable([]list.Item{...}, 30, sty)
list.SetFilter(textinput.Value())  // 实时过滤
selected := list.SelectedItem()
```

#### Command Type 切换

```go
type CommandType int
const (
    SystemCommands CommandType = iota
    UserCommands
    MCPPrompts
)

// 切换时调用 setCommandItems(type) 重 bake
```

**技巧**：三种来源统一抽象为 `CommandItem` + `Action`。

---

### 2.7 `panel/panel.go`

#### 树形结构

```go
type Panel struct {
    ID        string
    Title     string
    Content   string
    Direction Direction  // Vertical / Horizontal
    Children  []*Panel
    Sizes     []float64
    Border    lipgloss.Style
    Active    bool
}

func (p *Panel) IsLeaf() bool { return len(p.Children) == 0 }

func (p *Panel) RenderAt(scr uv.Screen, area image.Rectangle) {
    if p.IsLeaf() {
        p.renderLeaf(scr, area)
    } else {
        p.renderSplit(scr, area)
    }
}
```

**技巧**：递归 + 双模式（leaf/split），支持任意嵌套。

---

### 2.8 `list/list.go`

#### Lazy Rendering

```go
func (l *List) Render() string {
    // 只渲染可见的 items
    start, end := l.visibleRange()
    var b strings.Builder
    for i := start; i < end; i++ {
        b.WriteString(l.items[i].Render(l.width))
        b.WriteString("\n")
    }
    return b.String()
}
```

#### Offset Tracking

```go
type List struct {
    offsetIdx int  // 第一个可见 item 索引
    offsetLine int // 第一个可见 item 的行偏移
}

func (l *List) ScrollBy(delta int) {
    // 同时调整 idx 和 line，处理跨 item 滚动
    for delta != 0 {
        if delta > 0 { /* 上滚 */ }
        else { /* 下滚 */ }
    }
}
```

**技巧**：滚动以"行"为单位（不是 item），支持 item 高于视口的情况。

---

### 2.9 `completions/completions.go`

#### Tiered Matching

```go
const (
    tierExactName    = 0  // 精确匹配文件名
    tierPrefixName   = 1  // 前缀匹配
    tierPathSegment  = 2  // 路径段匹配
    tierFallback     = 3  // 模糊匹配
)

func (c *Completions) filter(input string) []CompletionItem {
    var items []CompletionItem
    for _, item := range c.allItems {
        if tier := item.tier(input); tier < tierFallback {
            items = append(items, CompletionItem{Item: item, Tier: tier})
        }
    }
    sort.Slice(items, func(i, j int) bool { return items[i].Tier < items[j].Tier })
    return items
}
```

**技巧**：分级匹配+排序，优先精确匹配。

---

### 2.10 `common/markdown.go`

#### Width-Keyed Cache

```go
var (
    mdCacheMu    sync.Mutex
    mdCache      = map[int]*glamour.TermRenderer{}
    quietMDCache = map[int]*glamour.TermRenderer{}
)

func MarkdownRenderer(sty *styles.Styles, width int) *glamour.TermRenderer {
    mdCacheMu.Lock()
    defer mdCacheMu.Unlock()

    if r, ok := mdCache[width]; ok { return r }

    r, _ := glamour.NewTermRenderer(
        glamour.WithStyles(*sty.Markdown),
        glamour.WithWordWrap(width),
        glamour.WithChromaFormatter("mocode"),
    )
    mdCache[width] = r
    return r
}

func InvalidateMarkdownRendererCache() {
    mdCacheMu.Lock()
    defer mdCacheMu.Unlock()
    mdCache = map[int]*glamour.TermRenderer{}
    quietMDCache = map[int]*glamour.TermRenderer{}
}
```

**技巧**：renderer 缓存按 width 复用；切主题时调 `Invalidate` 清空。

#### 自定义 Chroma Formatter

```go
func init() {
    xchroma.RegisterFormatter("mocode", xchroma.Formatter(zero, nil))
}
```

**技巧**：注册自定义 syntax highlighting formatter，glamour 内部调用。

---

## 三、6 个"用得好"的 API 技巧

### 技巧 A · 渲染器工厂 + 全局缓存

```go
var rendererCache = sync.Map{}  // width → renderer

func GetRenderer(width int) *Renderer {
    if v, ok := rendererCache.Load(width); ok { return v.(*Renderer) }
    r := newRenderer(width)
    rendererCache.Store(width, r)
    return r
}
```

**适用**：昂贵的 renderer（glamour、chroma）按 width 缓存。

### 技巧 B · 渲染宽度变化时只清缓存不重建

```go
func (s *Styles) SetWidth(w int) {
    if s.width == w { return }
    s.width = w
    s.cache = nil  // 失效，下次渲染重建
}
```

**技巧**：渲染时**先查缓存再算**，不在 setter 里清掉子组件缓存。

### 技巧 C · 注入式全局变量（避免循环依赖）

```go
// chat 包不 import panel，但需要使用
var toolPanelView *panel.View  // 由 ui 包启动时注入
func SetToolPanelView(pv *panel.View) { toolPanelView = pv }
```

**适用**：双向依赖但不想反向 import 的场景。

### 技巧 D · 接口返回 `any`，调用方 switch

```go
type Dialog interface {
    HandleMsg(msg tea.Msg) any  // Action
}

// 调用方
switch action := result.(type) {
case dialog.ActionSelectModel:    handleModelSelect(action)
case dialog.ActionCustomCommand:  handleCustomCommand(action)
case dialog.ActionClose:          m.dialog.Close(d.ID())
}
```

**技巧**：避免定义 30 个不同的返回值类型；调用方显式 switch 强制处理所有 case。

### 技巧 E · 自定义渲染样式 + 边距计算

```go
style := lipgloss.NewStyle().
    Padding(1, 2).
    Margin(0, 1).
    Border(lipgloss.RoundedBorder())

// 计算实际可用区域
innerW := area.Dx() - style.GetHorizontalFrameSize()
innerH := area.Dy() - style.GetVerticalFrameSize()

// 渲染后用 Place 居中
content := lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, content)
result := style.Render(content)
```

**技巧**：用 `GetHorizontalFrameSize/GetVerticalFrameSize` 算 padding+border 占用的像素。

### 技巧 F · 自定义 IME 感知光标

```go
func InputCursor(t *styles.Styles, cur *tea.Cursor) *tea.Cursor {
    return &tea.Cursor{
        X:    cur.X,
        Y:    cur.Y + borderTop,
        Style: t.Dialog.InputCursor,
    }
}
```

**技巧**：dialog 内输入框需要手动调光标位置；mocode 提供 `InputCursor` 助手。

---

## 四、4 个"洞察"（来自 mocode vs crush 对比）

### 洞察 1 · mocode 用 `time.Time` 作 ID；crush 用 string ID

```go
// mocode: anim.New 用毫秒时间戳当 ID
anim.New(anim.Settings{
    ID: fmt.Sprintf("%d", time.Now().UnixMilli()),
    // ...
})

// crush: 整个 message 的稳定 ID
anim.New(anim.Settings{
    ID: assistantMessageItem.ID(),
    // ...
})
```

**洞察**：stable ID 在重渲染时不会变化（动画持续），timestamp ID 会重复创建（动画闪烁）。

### 洞察 2 · mocode 全局注入；crush 通过 common 注入

```go
// mocode：全局包变量
var toolPanelView *panel.View
func SetToolPanelView(pv *panel.View) { toolPanelView = pv }

// crush：通过 *common.Common 字段传递
type Common struct {
    Panels *panel.View
}
// 调用：t.Common.Panels.Draw(...)
```

**洞察**：全局变量更简洁但**不可重入**（多个 UI 实例会冲突）；Common 注入更"显式"。

### 洞察 3 · mocode Panel 用纯 string Content；crush Panel 持有 child components

```go
// mocode：panel.Content 是 string
type Panel struct {
    Content string
    Children []*Panel
}

// crush：panel 可以持有 sub-model
type Panel struct {
    Content tea.Model
    // ...
}
```

**洞察**：纯 string 简单但无法内部交互；tea.Model 灵活但复杂。

### 洞察 4 · mocode 鼠标事件主动过滤；crush 让 Bubble Tea 处理

```go
// mocode：15ms 节流
func MouseEventFilter(...) tea.Cmd {
    // 节流 wheel/motion 事件
}

// crush：直接转发
// 全靠终端自己限流
```

**洞察**：mocode 的过滤防止 trackpad 事件洪泛；crush 依赖终端。

---

## 五、设计点速查（10 条）

| 设计点 | 出现位置 | 借鉴价值 |
|--------|----------|----------|
| width-keyed 渲染缓存 | `cachedMessageItem` | ★★★★★ |
| per-section 独立缓存 | `assistantSection` | ★★★★★ |
| FNV + 长度前缀防碰撞 | `fnvFields` | ★★★★ |
| 圆角 tree enumerator | `roundedEnumerator` | ★★★★ |
| ForegroundGrad via uniseg | `grad.go` | ★★★★ |
| 全局 setter 注入避免循环依赖 | `toolPanelView` | ★★★ |
| Dialog Overlay 栈（只顶收消息） | `dialog.Overlay` | ★★★★★ |
| Tiered search (分级匹配) | `completions` | ★★★★ |
| 渲染前 log cache hit/miss | `MOCODE_RENDER_CACHE_DEBUG` | ★★★ |
| Anim 双模式（静态+流光） | `anim.go` | ★★★★ |