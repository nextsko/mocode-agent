# 03 · 状态数据流转 + 配色 + 布局方法论

> 核心问题：TUI 程序里有多少状态？它们怎么流转？颜色如何决定？矩形如何计算？

---

## 一、状态分类（State Taxonomy）

mocode/crush 的状态可分 5 层，自下而上：

```
┌─────────────────────────────────────┐
│ Layer 5: 全局 UI 状态                │  uiOnboarding / uiInitialize / uiLanding / uiChat
├─────────────────────────────────────┤
│ Layer 4: 聚焦状态                    │  uiFocusNone / uiFocusEditor / uiFocusMain
├─────────────────────────────────────┤
│ Layer 3: 业务状态                    │  sessions, providers, mcp servers, skills, todos
├─────────────────────────────────────┤
│ Layer 2: 视图状态                    │  scroll offset, follow, expanded/collapsed, compact
├─────────────────────────────────────┤
│ Layer 1: 渲染状态                    │  render cache (width-keyed), anim frame
└─────────────────────────────────────┘
```

**关键原则**：状态越上层，变化越慢；越下层，变化越频繁。**每层状态的责任清晰分离**，避免跨层耦合。

### 实际代码映射

```go
// Layer 5: 全局 UI 状态 (ui.go)
type uiState uint8
const (
    uiOnboarding uiState = iota  // 未配置任何 provider
    uiInitialize                  // 当前目录需要初始化
    uiLanding                     // 配置好但还没开始对话
    uiChat                        // 对话中
)

// Layer 4: 聚焦 (ui.go)
type uiFocusState uint8
const (
    uiFocusNone uiFocusState = iota
    uiFocusEditor
    uiFocusMain
)

// Layer 3: 业务状态（来自 Workspace）
type UI struct {
    com      *common.Common  // ← Workspace 在这
    session  *session.Session
}

// Layer 2: 视图状态
type UI struct {
    isCompact       bool
    forceCompactMode bool
    detailsOpen     bool
    follow          bool  // chat 滚动跟踪
    pillsExpanded   bool
    completionsOpen bool
}

// Layer 1: 渲染状态
type cachedMessageItem struct {
    cache   map[int]string  // width → rendered string
    width   int
    srcHash uint64
}
```

---

## 二、状态流转图

### 主流程

```
                       ┌──────────────┐
                       │   Program    │
                       │  tea.NewProgram(u)  │
                       └──────┬───────┘
                              │ Init()
                              ▼
                       ┌──────────────┐
                       │ uiOnboarding │ (state=0, 启动时若未配置)
                       └──────┬───────┘
                              │ 配置完成（ActionSelectModel）
                              ▼
                       ┌──────────────┐
                       │ uiInitialize │ (项目未初始化)
                       └──────┬───────┘
                              │ 初始化完成
                              ▼
                       ┌──────────────┐
                       │  uiLanding   │ (空状态)
                       └──────┬───────┘
                              │ 发送第一条消息
                              ▼
                       ┌──────────────┐
                       │    uiChat    │ (主对话态)
                       └──────┬───────┘
                              │ session 重置
                              ▼
                       ┌──────────────┐
                       │  uiLanding   │ ← 循环
                       └──────────────┘
```

### 消息路由（`Update`）

```go
func (u *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // ① 全局快捷键（始终优先）
    if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
        if u.busy { u.cancelAgent(); return u, nil }
    }

    // ② Dialog 路由（栈顶优先）
    if u.dialog.HasDialogs() {
        if action := u.dialog.Update(msg); action != nil {
            u.handleDialogAction(action)
        }
        return u, nil
    }

    // ③ pubsub 事件
    switch msg := msg.(type) {
    case pubsub.Event[notify.Notification]:
        u.handleAgentNotification(msg.Payload)
    case pubsub.Event[message.Message]:
        // Created → appendSessionMessage
        // Updated → updateSessionMessage
        // Deleted → chat.RemoveMessage
    case tea.WindowSizeMsg:
        u.width, u.height = msg.Width, msg.Height
        return u, nil
    }

    // ④ 按状态分发
    switch u.state {
    case uiOnboarding: return u.updateOnboarding(msg)
    case uiInitialize: return u.updateInitialize(msg)
    case uiLanding:    return u.updateLanding(msg)
    case uiChat:       return u.updateChat(msg)
    }
    return u, nil
}
```

### 消息同步机制（pubsub）

```go
// app.Subscribe 协程把事件灌入 tea.Program
func (app *App) Subscribe(program *tea.Program) {
    events := app.events.Subscribe(tuiCtx)
    for {
        select {
        case ev, ok := <-events:
            if !ok { return }
            program.Send(ev.Payload)  // ← 关键：直接 Send 到 Program
        }
    }
}
```

事件类型枚举：
- `pubsub.Event[message.Message]` — 消息增删改
- `pubsub.Event[notify.Notification]` — agent 通知（thinking/tool/finished）
- `pubsub.Event[permission.PermissionRequest]` — 权限请求
- `pubsub.Event[mcp.Event]` — MCP 状态变化
- `pubsub.Event[skills.Event]` — 技能加载

### Agent 通知 → 渲染 流转

```
backend agent                          UI
─────────────                          ──
tool call finished
  ↓
app.events.Publish(notify.TypeAgentToolExecuting, ...)
  ↓
app.Subscribe() goroutine
  ↓
program.Send(notify.Notification)
  ↓
ui.Update(case pubsub.Event[notify.Notification])
  ↓
ui.handleAgentNotification(n)
  ├─ m.agentStatus = "executing bash..."
  ├─ m.updateAgentRuntime(...) // 更新 runtime map
  └─ side panel / pill 重渲染
```

---

## 三、配色方法论

### 3.1 色彩角色模型

mocode 使用 **charmtone** palette 的语义化角色：

```go
type quickStyleOpts struct {
    // 基础色
    primary        color.Color  // 主色（按钮、激活态、关键路径）
    secondary      color.Color  // 次色（图标、徽章）
    accent         color.Color  // 强调（链接、提示）

    // 文本
    fgBase         color.Color  // 正文
    fgMuted        color.Color  // 次要文字
    fgSubtle       color.Color  // 极淡文字（提示、占位）
    fgOnPrimary    color.Color  // 主色背景上的文字

    // 背景
    bgBase         color.Color  // 主背景
    bgSubtle       color.Color  // 二级背景（卡片、面板）
    bgElevated     color.Color  // 悬浮层（菜单、tooltip）

    // 状态
    success        color.Color  // ✓ 完成
    warning        color.Color  // ⚠ 注意
    warningSubtle  color.Color
    danger         color.Color  // ✗ 失败
    destructive    color.Color  // 删除

    // 边框
    border         color.Color
    borderActive   color.Color
    borderSubtle   color.Color
    separator      color.Color

    // 特殊
    spinner        color.Color
    working        color.Color
    error          color.Color
}
```

### 3.2 三套主题（预设）

```go
func ThemeForProvider(providerID string) Styles {
    switch providerID {
    case "hyper":  return HypermocodeObsidiana()  // 主 Charple, 次 Dolly, 强调 Bok
    case "evo":    return EvoCrimson()             // 暗红 / 火焰
    default:       return FromskoPantera()         // 主 Bok 绿, 次 Citron, 强调 Blush
    }
}
```

切换主题时只需要：
```go
applyTheme(styles.FromskoPantera())  // 替换 *Styles
chat.InvalidateRenderCaches()         // 清缓存
refreshStyles()                       // 推到子组件
```

### 3.3 渐变 stop 设计

| 用途 | FromColor | ToColor | 应用 |
|------|-----------|---------|------|
| Logo | secondary | primary | 顶栏 oMo code |
| Dialog title | primary | secondary | 标题后的 `╱╱╱` |
| Pills queue | primary | secondary | 队列指示器 `▶▶▶` |
| Spinner (working) | primary | secondary | 动态动画 |
| Sessions 删除 | destructive | primary | 危险态 |
| Sessions 重命名 | warningSubtle | accent | 警告态 |

### 3.4 语义命名优先于字面颜色

```go
// ❌ 错：字面颜色
header.Foreground = lipgloss.Color("#bd93f9")

// ✅ 对：语义角色
header.Foreground = primary
// 主题切换时自动跟随
```

---

## 四、布局方法论

### 4.1 矩形优先原则

**核心思想**：所有布局用 `image.Rectangle` 表示，绝不写绝对坐标。

```go
type uiLayout struct {
    area            image.Rectangle  // 整个屏幕
    header          image.Rectangle  // 顶栏
    main            image.Rectangle  // 主聊天区
    sidebar         image.Rectangle  // 侧栏
    editor          image.Rectangle  // 输入框
    pills           image.Rectangle  // 队列指示
    status          image.Rectangle  // 状态栏
    sessionDetails  image.Rectangle  // compact 模式下的会话详情
}
```

### 4.2 布局计算（`ui_layout.go`）

```go
func (m *UI) generateLayout(w, h int) uiLayout {
    const (
        sidebarWidth        = 30
        headerHeight        = 1
        helpHeight          = 1
        leftPadding         = 1
    )
    m.isCompact = m.forceCompactMode || w < compactModeWidthBreakpoint || h < compactModeHeightBreakpoint

    var l uiLayout
    l.area = image.Rect(0, 0, w, h)

    // 1. 顶栏（1 行）
    l.header = image.Rect(0, 0, w, headerHeight)

    // 2. 状态栏（最后 1 行）
    l.status = image.Rect(0, h-1, w, h)

    // 3. 编辑器（倒数第 4 行起，到状态栏上方）
    editorH := m.textarea.Height() + 3
    l.editor = image.Rect(0, h-1-editorH, w, h-1)

    // 4. 主聊天区
    bodyTop := headerHeight
    bodyBottom := h - 1 - editorH
    l.main = image.Rect(0, bodyTop, w, bodyBottom)

    // 5. 侧栏（仅非 compact）
    if !m.isCompact {
        l.sidebar = image.Rect(w-sidebarWidth-leftPadding, bodyTop, w-leftPadding, bodyBottom)
        l.main.Max.X -= sidebarWidth + leftPadding  // 主区右边界收缩
        l.main.Max.X -= 1                          // 内 padding
    }

    // 6. Pills（仅在有队列时）
    if m.pillsView != "" {
        pillsH := /* measure height of pills view */
        l.pills = image.Rect(l.main.Min.X, l.main.Max.Y-pillsH, l.main.Max.X, l.main.Max.Y)
        l.main.Max.Y -= pillsH
    }

    return l
}
```

### 4.3 Ultraviolet layout 库

**更复杂的分屏**：使用 `github.com/charmbracelet/ultraviolet/layout`：

```go
import uvlayout "github.com/charmbracelet/ultraviolet/layout"

func buildHeaderAndBody(w, h int) (header, body image.Rectangle) {
    header = uvlayout.Vertical(
        uvlayout.Len(1),          // 顶栏 1 行
        uvlayout.Fill(),          // 主体填满
        uvlayout.Len(1),          // 状态栏 1 行
    ).Split(0, 0, w, h)

    body = /* ... */
    return
}
```

DSL 风格声明式布局：
- `Len(n)` — 固定 n 像素
- `Fill()` — 占据剩余空间
- `Ratio(p, total)` — 按比例
- `Split().Assign(...)` — 显式分配

### 4.4 Compact 模式策略

```go
func (u *UI) View() tea.View {
    if u.isCompact {
        // compact：单列 + session details overlay
        return u.viewCompact()
    }
    // 正常：header + (main | sidebar) + editor + status
    return u.viewNormal()
}
```

compact 模式策略：
- **隐藏侧栏**（节省 30 列）
- **会话详情弹层**替代侧栏（按 `?` 或 hover 打开）
- **增大主区**获得更多聊天空间
- **阈值**：`width < 120` 或 `height < 30`

### 4.5 响应式断点

| 断点 | width | height | 行为 |
|------|-------|--------|------|
| 全功能 | ≥120 | ≥30 | header + main + sidebar(30) + editor + status |
| 紧凑 | <120 或 <30 | 同上 | 单列 + details overlay |
| 超紧凑 | <80 | 任意 | 隐藏 logo |

### 4.6 布局重算时机

```go
func (m *UI) View() tea.View {
    layout := m.generateLayout(m.width, m.height)

    // 关键：layout 变了才推到子组件
    if m.layout != layout {
        m.layout = layout
        m.updateSize()  // 把矩形赋给各子组件
    }
    // ...
}

func (m *UI) updateSize() {
    m.chat.SetSize(m.layout.main.Dx(), m.layout.main.Dy())
    m.textarea.MaxHeight = TextareaMaxHeight
    m.textarea.SetWidth(m.layout.editor.Dx())
    // 侧栏
    // status
    // pills
}
```

**限制每 resize 最多 2 遍**（textarea 高度变化触发重算时）。

---

## 五、绘制顺序（z-order）

```go
func (m *UI) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    layout := m.generateLayout(area.Dx(), area.Dy())
    if m.layout != layout { m.layout = layout; m.updateSize() }
    screen.Clear(scr)

    // 1. 底色（背景）
    // （由 canvas.Clear 或 styles.Background 自动覆盖）

    // 2. 内容层（从下到上）
    m.drawHeader(scr, layout.header)
    if !m.isCompact { m.drawSidebar(scr, layout.sidebar) }
    m.chat.Draw(scr, layout.main)

    // 3. 中间层
    if layout.pills.Dy() > 0 && m.pillsView != "" {
        drawPills(scr, layout.pills, m.pillsView)
    }

    // 4. 输入层
    uv.NewStyledString(m.editorView()).Draw(scr, layout.editor)

    // 5. 状态 / 帮助层
    m.status.Draw(scr, layout.status)

    // 6. Toast（浮层，覆盖 status）
    if m.toast.IsVisible() {
        m.toast.Draw(scr, scr.Bounds())
    }

    // 7. 浮层（completions / attach）
    if m.completionsOpen { m.completions.Draw(scr, ...) }

    // 8. Panel 叠加（agent 多任务时）
    if m.com.Panels != nil && m.com.Panels.IsVisible() {
        // chat 区缩 3/5，panel 占底部 2/5
        chatArea := upper; panelRect := lower
        m.com.Panels.Draw(scr, panelRect)
        m.chat.Draw(scr, chatArea)
    }

    // 9. Dialog（最后画，永远最上层）
    if m.dialog.HasDialogs() {
        return m.dialog.Draw(scr, scr.Bounds())
    }

    // 10. Debug 指示（MOCODE_UI_DEBUG=true 时画随机色方块）
    // 11. Cursor 定位
    return m.computeCursor()
}
```

**规则**：**后画的覆盖先画的**；dialog 永远最后；cursor 只在最后一个组件返回。

---

## 六、状态/数据/渲染三者的关系

```
        ┌─────────────────────────────┐
        │       State (struct fields) │  ← single source of truth
        └──────────────┬──────────────┘
                       │ (via pubsub or Update)
        ┌──────────────▼──────────────┐
        │   View state (render cache) │  ← derived, can be invalidated
        └──────────────┬──────────────┘
                       │ (Read by Render)
        ┌──────────────▼──────────────┐
        │     Render (string/uv)     │  ← derived from state
        └─────────────────────────────┘
```

**单方向**：state → render，**不**反向。Render 不能修改 state；修改必须通过 message + Update。

---

## 七、配色 vs 状态的对应

| 状态 | 配色变化 |
|------|----------|
| 正常 | 默认 fgBase 文字 + bgBase 背景 |
| Hover | 浅色 fgMuted → fgBase |
| Focused | borderActive 高亮边框 + bgSubtle 背景 |
| Disabled | fgSubtle + 删除线 |
| Error | danger 文字 + bgSubtle |
| Pending | working 渐变动画 |
| Selected | primary 文字 + bgElevated 背景 |
| Destructive (删除) | destructive 文字 + destructive 边框 |

**统一规则**：状态决定颜色，颜色**只**通过 `*styles.Styles` 查询；**不**在 Render 里硬编码。

---

## 八、设计方法论 5 条

1. **数据驱动渲染**：组件只接收 string/data，不持有业务逻辑。
2. **样式集中化**：所有颜色/边框/padding 在 `styles.go`，运行时只查表。
3. **布局声明化**：用矩形 + 声明式库，不写绝对坐标。
4. **绘制顺序固定**：背景 → 内容 → 中间层 → 浮层 → Dialog。
5. **缓存策略分层**：消息级 cache（width-keyed）、section 级 cache（per-section）、流式 cache（stable-prefix）。