# 01 · 编码规范 + 最小 MVP 路线

> 来源：`internal/ui/AGENTS.md`、`model/ui.go`、`chat/messages.go`、`dialog/dialog.go`、`styles/styles.go`
> 适用：所有基于 Bubble Tea v2 + Ultraviolet 的 Charm 系 TUI 项目

---

## 一、必须遵守的 12 条铁律

源自 `internal/ui/AGENTS.md` + crush 的等价文件，合并去重后：

### 1. **永远不要在 `Update` 里发送消息来驱动状态变化**

> 优先直接 mutate 子组件或 UI struct 字段；只在需要副作用时才用 `tea.Cmd`。

```go
// ❌ 错：为了更新状态发消息给自己
func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case refreshMsg:
        m.refresh()
        return m, m.refreshCmd() // 消息来回一次
    }
}

// ✅ 对：直接同步调用
func (m *UI) refresh() {
    m.chat.SetMessages(m.session.Messages)
    m.completions.SetItems(buildItems())
}
```

**设计意图**：消息机制是异步且有开销的；同帧内的状态同步应当是函数调用。

---

### 2. **永远不要在 `Update` 里做 IO 或耗时操作**

> 一律通过 `tea.Cmd` 异步执行；Cmd 返回值通过消息回调。

```go
// ✅ 正确
func (m *UI) loadSessionCmd(id string) tea.Cmd {
    return func() tea.Msg {
        sess, err := m.com.Workspace.GetSession(m.com.Ctx, id)
        if err != nil {
            return sessionErrorMsg{err: err}
        }
        return sessionLoadedMsg{session: sess}
    }
}

case sessionLoadedMsg:
    m.session = msg.session
    m.chat.SetMessages(msg.session.Messages)
```

---

### 3. **永远不要在 `tea.Cmd` 内修改 model state**

> Cmd 在闭包里可能延迟执行；state 修改必须发生在主 `Update` 里。

```go
// ❌ 错：Cmd 内修改 m.state
func (m *UI) someCmd() tea.Cmd {
    return func() tea.Msg {
        m.state = uiChat // 闭包捕获并修改，竞态
        return nil
    }
}

// ✅ 对：通过消息回到 Update
func (m *UI) someCmd() tea.Cmd {
    return func() tea.Msg { return loadedMsg{} }
}
case loadedMsg:
    m.state = uiChat
```

---

### 4. **永远不要在字节层面操作 ANSI 字符串**

> 一律使用 `charm.land/x/ansi` 包提供的高阶函数。

```go
import "github.com/charmbracelet/x/ansi"

ansi.StringWidth(s)        // 视觉宽度（含中文宽度）
ansi.Strip(s)              // 去 ANSI 转义
ansi.Cut(s, 0, n)          // 按视觉宽度截取
ansi.Truncate(s, n, "…")   // 截断带省略号
```

**为什么**：直接 `[]byte` 操作会把 `\x1b[31m` 拆成两半，导致宽度计算错误、样式错乱。

---

### 5. **保持简单，不要过度设计**

> 不要嵌套 model；不要搞多层抽象；优先组合 struct embedding 而非继承。

---

### 6. **必要时拆文件，但不要嵌套 model**

> 同一个 TUI 有且仅有一个 `tea.Model`；其他都是命令式结构体。

```go
// 正确的拆分粒度
type UI struct { /* 唯一 Bubble Tea model */ }
type Chat struct { list *list.List; /* 命令式，不是 model */ }
type Sidebar struct { /* 命令式 */ }
```

---

### 7. **结构体嵌入（Composition Over Inheritance）**

> 用 mixin 风格的嵌入复用通用能力（缓存、高亮、可聚焦）。

```go
type AssistantMessageItem struct {
    *highlightableMessageItem
    *cachedMessageItem
    *focusableMessageItem
    *message.Message
    sty *styles.Styles
    anim *anim.Anim
}

type cachedMessageItem struct {
    cache  map[int]string // width-keyed
    width  int
    srcHash uint64
}

func (c *cachedMessageItem) Get(width int) (string, bool) {
    if v, ok := c.cache[width]; ok { return v, true }
    return "", false
}
```

---

### 8. **能力通过接口组合声明（Opt-in Capabilities）**

> 不要做"万能基础结构体"；每个能力用独立接口表达，组件按需实现。

```go
type Focusable interface { SetFocused(bool) }
type Highlightable interface { SetHighlight(int, int) }
type Animatable interface { Animate(anim.StepMsg) }
type Expandable interface { SetExpanded(bool); Expanded() bool }
type Compactable interface { SetCompact(bool) }
type KeyEventHandler interface { HandleKey(tea.KeyMsg) bool }
```

**对比 Crush**：mocode 多了 `Compactable` 和 `KeyEventHandler`，更细粒度。

---

### 9. **`tea.Batch()` 一次返回多个 Cmd**

```go
return m, tea.Batch(
    m.loadPromptHistory(),
    m.loadCustomCommands(),
    m.hudTickCmd(),
)
```

---

### 10. **子组件需要 styles 时，统一通过 `*common.Common` 注入**

> 不要 `import styles` 到每个文件；统一由 `Common{ App, Styles, Panels }` 穿透。

```go
type Common struct {
    App      *app.App
    Styles   *styles.Styles
    Panels   *panel.View
}
```

**为什么**：换主题时只需 `com.Styles = NewStyles` 一次，全员同步。

---

### 11. **渲染热路径不能调用 `lipgloss.NewStyle()`**

> 所有样式在 `styles/styles.go` 启动时一次性预计算；运行时只查表。

```go
// ❌ 错：每帧创建
func (m *MyComponent) Render(width int) string {
    return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff00ff")).Render("hi")
}

// ✅ 对：预计算
type Styles struct {
    MyComp lipgloss.Style  // 启动时填好
}
```

---

### 12. **聚焦状态决定按键路由**

```go
case tea.KeyMsg:
    if m.dialog.HasDialogs() {
        return m, m.handleDialogMsg(msg) // 栈顶优先
    }
    switch m.focus {
    case uiFocusEditor: return m.updateEditor(msg)
    case uiFocusMain:   return m.updateChat(msg)
    }
```

---

## 二、最小 MVP 路线（5 步走）

> 目标：用 **1000 行 Go** 实现一个能跑、有样式、可扩展的 TUI 框架。

### Step 1：搭骨架（~150 行）

```go
package ui

import (
    tea "charm.land/bubbletea/v2"
    uv "github.com/charmbracelet/ultraviolet"
    "image"
)

type UI struct {
    com    *Common
    width  int
    height int
}

type Common struct {
    Styles *Styles
}

func (u *UI) Init() tea.Cmd { return nil }
func (u *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.WindowSizeMsg:
        u.width, u.height = msg.Width, msg.Height
    }
    return u, nil
}
func (u *UI) View() tea.View {
    canvas := uv.NewScreenBuffer(u.width, u.height)
    // 占满背景
    return tea.NewView(canvas.Render())
}
```

**验证**：能在 alt-screen 画一个空屏。

---

### Step 2：加 Styles 体系（~200 行）

```go
package styles

import "charm.land/lipgloss/v2"

type Styles struct {
    Background lipgloss.Style
    Header     lipgloss.Style
    Body       lipgloss.Style
    Footer     lipgloss.Style
    Accent     lipgloss.Color
}

func Default() *Styles {
    return &Styles{
        Background: lipgloss.NewStyle().Background(lipgloss.Color("#0e0e10")),
        Header:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff79c6")),
        Body:       lipgloss.NewStyle().Foreground(lipgloss.Color("#f8f8f2")),
        Footer:     lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4")),
        Accent:     lipgloss.Color("#bd93f9"),
    }
}
```

**原则**：所有 `lipgloss.Style()` 只在 `Default()` 中调用一次。

---

### Step 3：加 textarea + 矩形布局（~300 行）

```go
import (
    "charm.land/bubbles/v2/textarea"
    uv "github.com/charmbracelet/ultraviolet"
)

type UI struct {
    // ...
    textarea textarea.Model
    layout   struct {
        header, body, footer, editor image.Rectangle
    }
}

func (u *UI) layout(w, h int) {
    u.layout.header = image.Rect(0, 0, w, 1)
    u.layout.body   = image.Rect(0, 1, w, h-3)
    u.layout.editor = image.Rect(0, h-3, w, h-1)
    u.layout.footer = image.Rect(0, h-1, w, h)
}

func (u *UI) View() tea.View {
    canvas := uv.NewScreenBuffer(u.width, u.height)
    screen.Clear(canvas)

    uv.NewStyledString(u.com.Styles.Header.Render(" 🚀 my-tui v0.1 ")).
        Draw(canvas, u.layout.header)
    uv.NewStyledString(u.body()).
        Draw(canvas, u.layout.body)
    uv.NewStyledString(u.textarea.View()).
        Draw(canvas, u.layout.editor)
    uv.NewStyledString(u.com.Styles.Footer.Render(" esc quit • enter send ")).
        Draw(canvas, u.layout.footer)

    return tea.NewView(canvas.Render())
}
```

**验证**：能输入文字，按 enter 触发一个 fake "AI response"。

---

### Step 4：加消息系统（~250 行）

```go
type Message struct {
    Role    string // "user" | "assistant"
    Content string
}

type ChatList struct {
    msgs     []Message
    styles   *Styles
    width    int
    cache    map[int]string // width-keyed cache
}

func (c *ChatList) Render() string {
    if v, ok := c.cache[c.width]; ok { return v }
    var b strings.Builder
    for _, m := range c.msgs {
        prefix := " ❯ "
        if m.Role == "assistant" { prefix = " ✦ " }
        b.WriteString(prefix + m.Content + "\n\n")
    }
    c.cache[c.width] = b.String()
    return b.String()
}
```

**关键**：width-keyed cache，避免每帧重新组装。

---

### Step 5：加 Dialog 框架（~150 行）

```go
type Dialog interface {
    ID() string
    HandleMsg(msg tea.Msg) any
    Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor
}

type Overlay struct{ dialogs []Dialog }

func (o *Overlay) HasDialogs() bool { return len(o.dialogs) > 0 }
func (o *Overlay) OpenDialog(d Dialog) { o.dialogs = append(o.dialogs, d) }
func (o *Overlay) Close(id string) { /* pop */ }
func (o *Overlay) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    for _, d := range o.dialogs { d.Draw(scr, area) }
    return nil
}
```

**使用**：`if m.dialog.HasDialogs() { m.dialog.Draw(canvas, canvas.Bounds()) }`（必须最后画）

---

### MVP 验收清单

| 能力 | 行数 | 验收方式 |
|------|------|----------|
| 启动 + alt-screen | 30 | 运行 `go run .` 不崩溃 |
| Styles 系统 | 80 | 改色立刻生效 |
| 矩形布局 | 120 | resize 时不同区域大小正确 |
| textarea | 70 | 能多行输入 |
| 消息列表 + cache | 150 | 输入不卡 |
| Dialog 框架 | 100 | `Ctrl+P` 打开命令面板 |
| **总计** | **~550 行** | **能跑、有样式、可扩展** |

> 比直接抄 mocode 的 ~1800 行 UI 节省 ~70%。

---

## 三、命名约定

| 类型 | 约定 | 示例 |
|------|------|------|
| UI 状态枚举 | `uiXxx` 前缀 | `uiChat`、`uiFocusEditor` |
| 内部消息类型 | `xxxMsg` 后缀 | `sessionLoadedMsg`、`closeDialogMsg` |
| 异步命令方法 | `xxxCmd` 后缀 | `loadPromptHistory()` → `loadPromptHistoryCmd` |
| 渲染方法 | `Render(w)` 或 `Draw(scr, area)` | `Render(width int) string` |
| 布局字段 | `layout.xxxRectangle` | `layout.header`、`layout.editor` |
| 样式嵌套 | 分组嵌套匿名 struct | `Styles.Dialog.Title`、`Styles.Messages.Thinking` |
| 图标常量 | `<Meaning>Icon` | `CheckIcon`、`ToolPending` |

---

## 四、文件组织（mocode 标准布局）

```
internal/ui/
├── model/
│   ├── ui.go              ← 唯一 Bubble Tea model
│   ├── ui_layout.go       ← 矩形布局生成
│   ├── ui_keys.go         ← 按键映射
│   ├── ui_dialogs.go      ← 对话框路由
│   ├── header.go          ← 顶栏
│   ├── sidebar.go         ← 侧栏
│   ├── status.go          ← 状态栏
│   ├── toast.go           ← Toast
│   ├── chat.go            ← 聊天包装
│   ├── history.go         ← 提示历史
│   ├── filter.go          ← 鼠标事件节流
│   ├── pills.go           ← 队列/任务指示
│   └── onboarding.go      ← 引导页
├── chat/
│   ├── messages.go        ← MessageItem 接口 + mixins
│   ├── assistant.go       ← AssistantMessageItem
│   ├── user.go            ← UserMessageItem
│   ├── tools.go           ← ToolMessageItem + dispatch
│   ├── tools_render.go    ← 共享 tool 渲染助手
│   ├── bash.go            ← 按工具分文件
│   ├── file.go
│   ├── search.go
│   ├── fetch.go
│   ├── agent.go
│   └── ...
├── dialog/
│   ├── dialog.go          ← Dialog 接口 + Overlay
│   ├── common.go          ← RenderContext
│   ├── commands.go        ← slash 面板
│   ├── models.go          ← 模型选择
│   ├── sessions.go        ← 会话选择
│   ├── permissions.go     ← 权限确认
│   ├── api_key_input.go
│   ├── filepicker.go
│   ├── reasoning.go
│   ├── quit.go
│   ├── wechat_*.go
│   └── ...
├── list/
│   ├── list.go            ← lazy-load List
│   └── item.go            ← Item 接口
├── completions/           ← @ 自动补全
├── attachments/           ← 附件管理
├── panel/                 ← Panel 树（tmux-like）
├── common/                ← Common struct + 助手
├── styles/                ← 所有样式
│   ├── styles.go          ← 主 Styles struct
│   ├── themes.go          ← 主题分发
│   ├── grad.go            ← ForegroundGrad
│   └── quickstyle.go      ← 快速创建 Styles
├── diffview/              ← diff 渲染
├── anim/                  ← 动画（流光字符）
├── image/                 ← 终端图像协议
├── logo/                  ← Logo
└── util/                  ← 小工具
```

**规则**：超过 300 行的文件优先按"职责"拆分（如 `chat/tools.go` 拆出 `bash.go` 等）。