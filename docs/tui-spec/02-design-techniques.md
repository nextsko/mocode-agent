# 02 · 6 大优秀设计技巧拆解

> 拆解目标：
> 1. 侧边好看面板
> 2. 微信二维码渲染
> 3. `/` slash 浮动面板
> 4. agent 树状展示
> 5. agent 卡片设计
> 6. 顶栏 oMo 流光字体

每个技巧包含：**效果描述 / 核心代码 / 设计意图 / 复用模板**。

---

## 技巧 1 · 侧栏优雅面板（动态高度分配）

### 效果

```
┌─ Sessions ─────────────┐
│ fix login bug          │
│ /Users/dev/proj        │
│ Anthropic · claude-…   │
│                        │
│ Files                  │
│ + 142 - 38             │
│   internal/auth.go     │
│   internal/api.go      │
│                        │
│ LSP  ●go ●ts ●py       │
│ MCP  ●github ●docker   │
│ Skills 4 active        │
└────────────────────────┘
```

终端窄时各 section **自动压缩**，文件多就多占行、LSP/MCP/Skills 少就少占。

### 核心实现（`internal/ui/model/sidebar.go`）

```go
// 动态高度分配：files > LSPs > MCPs > skills 优先级
func getDynamicHeightLimits(remaining int, counts struct {
    Files, LSPs, MCPs, Skills int
}) (filesH, lspH, mcpH, skillH int) {
    // 基础：每个 section 至少 2 行（标题 + 1 项 + 空行）
    baseTotal := 4*2 + counts.Files + counts.LSPs + counts.MCPs + counts.Skills

    if baseTotal <= remaining {
        // 全显示后还有余量 → 按优先级补
        extra := remaining - baseTotal
        // 优先级分配
        // files 优先占满，剩余 → lsp → mcp → skills
        filesH = 2 + counts.Files
        extra -= counts.Files
        if extra < 0 { filesH += extra; extra = 0 }
        // ... 同理
    } else {
        // 空间不够 → 优先级缩
        // 每个至少 1 行
        filesH = max(1, counts.Files)
        // ...
    }
    return
}
```

### 关键代码 · 侧栏 section 渲染

```go
func drawSidebar(scr uv.Screen, area image.Rectangle) {
    sty := u.com.Styles
    cur := area.Min.Y
    filesH, lspH, mcpH, skillH := getDynamicHeightLimits(area.Dy()-10, counts)

    // 1. Title
    title := u.session.Title  // 最多 2 行
    uv.NewStyledString(sty.Sidebar.SessionTitle.Render(title)).
        Draw(scr, image.Rect(area.Min.X, cur, area.Max.X, cur+2))
    cur += 2

    // 2. CWD
    uv.NewStyledString(sty.Sidebar.WorkingDir.Render(common.PrettyPath(u.session.CWD))).
        Draw(scr, image.Rect(area.Min.X, cur, area.Max.X, cur+1))
    cur++

    // 3. Model info
    uv.NewStyledString(modelInfo(sty)).Draw(scr, ...)
    cur++

    // 4. Files section（动态高度）
    if filesH > 1 {
        uv.NewStyledString(filesInfo(sty, filesH)).Draw(scr, ...)
        cur += filesH
    }

    // 5. LSP
    uv.NewStyledString(lspInfo(sty, lspH)).Draw(scr, ...)
    cur += lspH

    // 6. MCP, Skills ...
}
```

### 设计意图

1. **优先级分配**而不是均匀分配：用户最关心文件改动数和 LSP 状态。
2. **最少 1 行**：避免某 section 完全消失带来的跳变。
3. **超出滚动**：section 内容超过分配高度时显示 `N more…`，不裁切关键信息。

### 复用模板

```go
// 通用动态侧栏布局
type SidebarSection struct {
    Title    string
    Items    []string
    MinLines int    // 默认 2
    Priority int    // 0-10，数字大优先占行
}

func allocateHeight(sections []SidebarSection, total int) []int {
    // 1. 给每个 section MinLines
    // 2. 余量按 Priority 比例分配
    // 3. 超出显示 "N more…"
}
```

---

## 技巧 2 · 微信二维码渲染（白底黑字 ASCII）

### 效果

```
╭──── WeChat Login ──────────────────╮
│                                    │
│    ██████ ▄▄▄ ▀▀ ████  ▄           │
│    █  █ █ █ █▄ █ █  ▄▄ █           │
│    ██ █  ▀█ █▄▄ ███  ▀█ █          │
│    ... (ASCII QR code) ...         │
│                                    │
│       Scan with WeChat             │
│       Status: Waiting...           │
│                                    │
╰────────────────────────────────────╯
```

### 核心实现（`internal/ui/dialog/wechat_qr.go`）

```go
const WeChatQRID = "wechat_qr"

type WeChatQR struct {
    com     *common.Common
    help    help.Model
    state   qrState  // Generating, Display, Scanned, LoggedIn, Error
    qrASCII string
    errMsg  string
    // ...
}

func (d *WeChatQR) StartLogin(ctx context.Context) tea.Cmd {
    return tea.Batch(
        func() tea.Msg {
            qr, err := wechat.GetManager().Login(ctx, ...)
            if err != nil { return WeChatQRMsg{State: Error, ErrMsg: err.Error()} }
            return WeChatQRMsg{State: Display, QRASCII: qr.ASCII}
        },
        d.PollLoginCmd(),  // 250ms 心跳
    )
}

func (d *WeChatQR) PollLoginCmd() tea.Cmd {
    return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
        return WeChatQRPollMsg{}
    })
}
```

### 二维码渲染（最关键）

```go
func renderQRPanel(qrASCII string) string {
    // wechat.GenerateQR 已产出 ASCII art（unicode 半角方块）
    // 关键：白底黑字样式
    style := lipgloss.NewStyle().
        Background(lipgloss.Color("#ffffff")).
        Foreground(lipgloss.Color("#000000")).
        Padding(1, 2)

    return style.Render(qrASCII)
}
```

### Draw 方法

```go
func (d *WeChatQR) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    var rc *dialog.RenderContext
    var content string

    switch d.state {
    case Generating:
        rc = dialog.NewRenderContext(d.com.Styles, area.Dx())
        rc.Title = "WeChat Login"
        content = d.com.Styles.Status.Spinner.Render("Generating QR code…")

    case Display:
        rc = dialog.NewRenderContext(d.com.Styles, area.Dx())
        rc.Title = "WeChat Login"
        rc.AddPart(renderQRPanel(d.qrASCII))
        rc.AddPart(d.com.Styles.Status.Info.Render("Scan with WeChat"))
        rc.AddPart(d.com.Styles.Status.Hint.Render("Status: Waiting for scan"))

    case Scanned:
        // 绿色 ✓ + "Scanned, confirm on phone"

    case LoggedIn:
        // 绿色 ✓ + "Logged in as: xxx"

    case Error:
        rc.AddPart(d.com.Styles.Status.Error.Render(d.errMsg))
        rc.AddPart(d.com.Styles.Help.Render("r retry • esc close"))
    }

    return dialog.DrawCenter(rc, scr, area)
}
```

### 设计意图

1. **白底黑字**：微信是移动端，扫码对对比度敏感。
2. **200ms 心跳**：扫码是 5-15s 动作，必须轮询状态。
3. **状态机显式化**：4 个状态对应 4 种 UI，避免"在转圈但没图"的尴尬。
4. **`r retry`**：网络失败必须提供重试入口。

### 复用模板

任何"扫码登录"对话框通用结构：
```go
type QrLoginDialog struct {
    State     LoginState  // Generating/Display/Scanned/Done/Error
    QrContent string
    PollFn    func() tea.Msg
    PollEvery time.Duration
}
```

---

## 技巧 3 · `/` slash 浮动面板（命令面板）

### 效果

```
╭─ Type to filter ────────────────────────╮
│ > sess                                   │
│   /new-session    Create new session     │
│   /list           List all sessions      │
│   /resume <id>    Resume a session       │
│   /share <id>     Share a session        │
│                                          │
│ ↑↓ navigate • enter select • esc cancel  │
╰──────────────────────────────────────────╯
```

### 核心实现（`internal/ui/dialog/commands.go`）

#### 尺寸常量

```go
const (
    commandPaletteMaxWidth  = 84
    commandPaletteMaxHeight = 24
    commandPaletteMinWidth  = 56
)

type CommandType int
const (
    SystemCommands CommandType = iota
    UserCommands
    MCPPrompts
)
```

#### 数据结构

```go
type Commands struct {
    com             *common.Common
    navStack        []*CommandItem  // 面包屑（支持嵌套菜单）
    list            *list.FilterableList
    spinner         spinner.Model
    input           textinput.Model
    help            help.Model
    customCommands  []slash.CustomCommand
    mcpPrompts      []MCPPrompt
    selected        CommandType
}

type CommandItem struct {
    Title       string
    Description string
    Action      dialog.Action  // ActionOpenDialog / ActionCustomCommand / ActionAttachSkill ...
    Key         string
    ID          string
}
```

#### 关键：宽度变化时重置 items

```go
func (c *Commands) SetSize(area image.Rectangle) {
    width := max(0, min(commandPaletteMaxWidth, area.Dx() - c.com.Styles.Dialog.View.GetHorizontalBorderSize()))
    height := max(0, min(commandPaletteMaxHeight, area.Dy() - ...))

    innerWidth := width - c.com.Styles.Dialog.View.GetHorizontalFrameSize()

    // 重新计算 input/list/help 尺寸
    c.input.SetWidth(innerWidth - c.com.Styles.Dialog.InputPrompt.GetHorizontalFrameSize() - 1)

    heightOffset := /* title + input + help + view borders */
    c.list.SetSize(innerWidth, height-heightOffset)
    c.help.SetWidth(innerWidth)

    // 关键：宽度变化时某些 item 文案依赖宽度，需要重 bake
    if area.Dx() != c.windowWidth && c.selected == SystemCommands {
        c.setCommandItems(SystemCommands)
    }
    c.windowWidth = area.Dx()
}
```

#### 类型切换（System / User / MCP）

```go
func (c *Commands) setCommandItems(t CommandType) {
    var items []list.Item
    switch t {
    case SystemCommands:
        items = c.defaultCommands()  // 28 个内置命令
    case UserCommands:
        for _, cmd := range c.customCommands {
            var action dialog.Action
            if cmd.Skill != nil {
                action = dialog.ActionAttachSkill{ID: cmd.Skill.Path, Name: cmd.Name}
            } else {
                action = dialog.ActionCustomCommand{
                    Content:    cmd.Content,
                    Arguments:  cmd.Arguments,
                    Skill:      cmd.Skill,
                }
            }
            item := list.NewCommandItem(sty, "custom_"+cmd.ID, cmd.Name, "", action)
            if cmd.Skill != nil {
                item = item.WithDescription(cmd.Skill.Description)
            }
            items = append(items, item)
        }
    case MCPPrompts:
        items = buildMCPPromptItems(c.mcpPrompts)
    }
    c.list.SetItems(items)
}
```

#### Draw

```go
func (c *Commands) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    width := max(commandPaletteMinWidth, min(commandPaletteMaxWidth, area.Dx()))
    rc := dialog.NewRenderContext(c.com.Styles, width)
    rc.Title = "Type to filter"
    rc.AddPart(c.input.View())
    rc.AddPart(c.list.View())  // FilterableList 自动渲染过滤后的项
    rc.AddPart(c.help.View())
    return dialog.DrawCenter(rc, scr, area)
}
```

### 设计意图

1. **`FilterableList`**：不是 `List`，自带输入过滤。
2. **导航栈 `navStack`**：支持子菜单（按 `→` 进入，按 `←` 返回）。
3. **三种来源**：System / User (slash config) / MCP prompts，统一抽象为 `CommandItem`。
4. **宽度依赖文案**：宽度变化触发 `setCommandItems` 重 bake，避免出现"窄屏显示长文案被截断"的尴尬。

### 复用模板

```go
type PaletteDialog[I any] struct {
    Title    string
    Items    []I
    Filter   func(I, string) bool
    Render   func(I) string
    OnSelect func(I) tea.Cmd
    Input    textinput.Model
    List     *list.FilterableList
}
```

---

## 技巧 4 · Agent 树状展示（嵌套子工具）

### 效果

```
╭─ Agent ────────────────────────────────────────╮
│ Task: Find all *.go files using sqlx            │
│   ╰── ▶ grep "sqlx" --type go                   │
│   ╰── ✓ view internal/db.go                     │
│   ╰── 1 tool calls · 3.2s                       │
╰─────────────────────────────────────────────────╯
```

或更复杂的并行任务：

```
╭─ Agent ────────────────────────╮
│ Tasks (3):                     │
│   ├─ task-1 ✓ Plan             │
│   ├─ task-2 ✓ Implement        │
│   ╰─ task-3 ▶ Running          │
╰────────────────────────────────╯
```

### 核心实现（`internal/ui/chat/agent.go`）

#### Tree 渲染

```go
import "charm.land/lipgloss/v2/tree"

func (r *AgentToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
    // 1. 解析 agent 参数
    var params agent.AgentParams
    json.Unmarshal(opts.ToolCall.Input, &params)

    // 2. 头部 + 任务标签
    header := toolHeader(sty, opts.Status, "Agent", ...)
    header += sty.Tool.AgentTaskTag.Render("Task")

    // 3. 截断的 prompt
    prompt := truncate(params.Prompt, max(0, taskTagWidth-5))

    // 4. 构建 tree
    childTools := tree.Root(header)

    // 5. 运行中：先添加状态摘要
    if !opts.HasResult() && !opts.IsCanceled() {
        var summary []string
        if len(r.agent.nestedTools) > 0 {
            summary = append(summary, fmt.Sprintf("%d tool calls", len(r.agent.nestedTools)))
        }
        if r.agent.statusSummary != "" {
            summary = append(summary, r.agent.statusSummary)
        }
        if len(summary) > 0 {
            childTools.Child(sty.Tool.StateWaiting.Render(strings.Join(summary, " · ")))
        }
    }

    // 6. 嵌套工具
    for _, nested := range r.agent.nestedTools {
        nestedView := nested.Render(cappedWidth)
        childTools.Child(nestedView)
    }

    // 7. 渲染树（自定义圆角 enumerator）
    parts := []string{
        childTools.Enumerator(roundedEnumerator(2, max(0, taskTagWidth-5))).String(),
    }

    // 8. 运行中追加动画
    if !opts.HasResult() && !opts.IsCanceled() {
        parts = append(parts, "", opts.Anim.Render())
    }

    result := lipgloss.JoinVertical(lipgloss.Left, parts...)

    // 9. 完成后追加结果内容
    if opts.HasResult() && opts.Result.Content != "" {
        body := toolOutputMarkdownContent(sty, opts.Result.Content, cappedWidth-toolBodyLeftPaddingTotal, opts.ExpandedContent)
        return joinToolParts(result, body)
    }
    return result
}
```

#### 圆角 enumerator（最关键的"漂亮"细节）

```go
func roundedEnumerator(lPadding, width int) tree.Enumerator {
    if width == 0  { width = 2 }
    if lPadding == 0 { lPadding = 1 }
    return func(children tree.Children, index int) string {
        line := strings.Repeat("─", width)
        padding := strings.Repeat(" ", lPadding)
        // 最后一个子用 ╰，其它用 ├
        if children.Length()-1 == index {
            return padding + "╰" + line
        }
        return padding + "├" + line
    }
}
```

输出：
```
   ├───────── child 1
   ├───────── child 2
   ╰───────── last child
```

### 设计意图

1. **树形而非嵌套缩进**：`tree.Root()` + `Child()` 是声明式，比手写缩进清晰。
2. **自定义 enumerator**：默认 `│` 太生硬，`╰` + `─` 更柔和。
3. **运行/完成分两种布局**：运行时显示状态摘要 + spinner；完成后只显示结果。
4. **嵌套工具强制 compact**：`AddNestedTool` 时调用 `Compactable.SetCompact(true)`。

### 复用模板

```go
// 通用嵌套树渲染
type NestedToolItem struct {
    Header   string
    Children []NestedToolItem
    Status   ToolStatus
}

func (n NestedToolItem) Render(sty *Styles, width int) string {
    root := tree.Root(n.Header)
    for _, c := range n.Children {
        root.Child(c.Render(sty, width))
    }
    return root.Enumerator(roundedEnumerator(2, 6)).String()
}
```

---

## 技巧 5 · Agent 卡片设计（Panel 树）

### 效果

并行的多个 agent 同时跑时：

```
┌─ Main ─┐┌─ task-1 ────────────┐┌─ task-2 ────────────┐
│        ││ ✓ Plan complete     ││ ▶ Implementing...   │
│ Active ││   ✓ grep auth.go    ││   ▶ edit users.go   │
│        ││   ✓ view schema.go  ││   ▶ view tests.go   │
└────────┘└─────────────────────┘└─────────────────────┘
```

类似 tmux 的横向分屏，**每个 panel 显示一个并行子任务**。

### 核心实现（`internal/ui/panel/`）

#### Panel 节点

```go
type Direction int
const (
    Vertical   Direction = iota  // 左右分屏
    Horizontal                   // 上下分屏
)

type Panel struct {
    ID        string
    Title     string
    Content   string
    Direction Direction
    Children  []*Panel
    Sizes     []float64  // 0-1 比例（须和为 1）
    Border    lipgloss.Style
    TitleStyle lipgloss.Style
    Active    bool       // 当前聚焦
    mu        sync.RWMutex
}

func NewSplit(id string, dir Direction, children ...*Panel) *Panel {
    sizes := make([]float64, len(children))
    for i := range sizes { sizes[i] = 1.0 / float64(len(children)) }
    return &Panel{ID: id, Direction: dir, Children: children, Sizes: sizes}
}
```

#### 渲染

```go
func (p *Panel) RenderAt(scr uv.Screen, area image.Rectangle) {
    if p.IsLeaf() {
        p.renderLeaf(scr, area)
    } else {
        p.renderSplit(scr, area)
    }
}

func (p *Panel) renderLeaf(scr uv.Screen, area image.Rectangle) {
    content := p.GetContent()
    borderStyle := p.Border
    if p.Active { borderStyle = borderStyle.Foreground(lipgloss.Color("205")) }  // 高亮 active

    innerW := area.Dx() - 4  // 左右边框各 1 + 内 padding 2
    innerH := area.Dy() - 2  // 上下边框

    // 标题行
    var b strings.Builder
    if title := p.TitleStyle.Render(" "+p.Title+" "); p.Title != "" && innerW > len(title)+2 {
        b.WriteString("┌" + title + strings.Repeat("─", innerW-len(title)) + "┐\n")
    } else {
        b.WriteString("┌" + strings.Repeat("─", innerW) + "┐\n")
    }

    // 内容（截断/pad 到 innerW x innerH）
    lines := strings.Split(content, "\n")
    for i := 0; i < innerH; i++ {
        line := ""
        if i < len(lines) { line = lines[i] }
        if len(line) > innerW {
            line = line[:innerW]
        } else {
            line += strings.Repeat(" ", innerW-len(line))
        }
        b.WriteString("│" + line + "│\n")
    }
    b.WriteString("└" + strings.Repeat("─", innerW) + "┘")

    uv.NewStyledString(borderStyle.Render(b.String())).Draw(scr, area)
}

func (p *Panel) renderSplit(scr uv.Screen, area image.Rectangle) {
    var offset int
    for i, child := range p.Children {
        var childArea image.Rectangle
        switch p.Direction {
        case Vertical:
            childW := int(float64(area.Dx()) * p.Sizes[i])
            if i == len(p.Children)-1 { childW = area.Dx() - offset }  // 余数给最后一个
            childArea = image.Rect(area.Min.X+offset, area.Min.Y, area.Min.X+offset+childW, area.Max.Y)
            offset += childW
        case Horizontal:
            // 类似
        }
        child.RenderAt(scr, childArea)
    }
}
```

#### 与 Agent 集成（注入 View）

```go
// chat/tools.go 中定义注入点
var toolPanelView *panel.View
var agentPanelResolver func(parentID string, params agent.AgentParams, tools []ToolMessageItem) []panel.AgentPanelData

func SetToolPanelView(pv *panel.View) { toolPanelView = pv }
func SetAgentPanelResolver(resolver ...) { agentPanelResolver = resolver }

// 在 ui.go 启动时注入
chat.SetToolPanelView(com.Panels)
chat.SetAgentPanelResolver(ui.agentTaskPanelsForRender)
```

### 设计意图

1. **Panel 是树结构**（不是列表），支持任意嵌套的横向/纵向分屏。
2. **Content 是 string 而非 component**：每个 panel 只是"容器"，内容由调用方生成。
3. **Active 状态高亮**：聚焦的 panel 边框换色（lipgloss.Color("205")）。
4. **优先级分配**：`Sizes[i] * area` 给每个子，余数给最后一个避免丢像素。

### 复用模板

```go
// 任何需要"分屏展示多任务"的场景都适用
type PanelGrid struct {
    Root    *Panel
    Active  string  // panel ID
    Visible bool
}

func (g *PanelGrid) Show() / Hide() / SetActive(id)
func (g *PanelGrid) UpdateContent(id, content string)
```

---

## 技巧 6 · 顶栏 oMo 流光字体（前景渐变 + Logo）

### 效果

```
🚀 ✦ oMo code │ cwd: ~/proj │ go:ok │ ctx ████░░ 40% │ 3m 42s │ ctrl+d close
```

`oMo code` 文字本身**左→右前景色渐变**（primary → secondary），配合 charm icon 形成"霓虹"感。

### 核心实现

#### `grad.go` · 前景渐变

```go
package styles

import (
    "charm.land/lipgloss/v2"
    "github.com/rivo/uniseg"
)

func ForegroundGrad(base lipgloss.Style, input string, bold bool, c1, c2 color.Color) []string {
    var clusters []string
    gr := uniseg.NewGraphemes(input)
    for gr.Next() {
        clusters = append(clusters, string(gr.Runes()))
    }
    ramp := lipgloss.Blend1D(len(clusters), c1, c2)  // 关键 API：返回每段的颜色
    for i, c := range ramp {
        style := base.Foreground(c)
        if bold { style = style.Bold(true) }
        clusters[i] = style.Render(clusters[i])
    }
    return clusters
}

func ApplyForegroundGrad(...) string {
    return strings.Join(ForegroundGrad(...), "")
}

func ApplyBoldForegroundGrad(...) string {
    return strings.Join(ForegroundGrad(..., true), "")
}
```

#### `quickstyle.go` · 注入渐变色

```go
// 在主题里定义渐变 stop
WorkingGradFromColor = primary       // 起始色
WorkingGradToColor   = secondary     // 终止色
WorkingLabelColor    = fgBase

Header.LogoGradFromColor = secondary
Header.LogoGradToColor   = primary
```

#### `header.go` · 渲染

```go
func (h *header) refresh() {
    name := "oMo code"
    if isHyper { name = "HYPERmocode" }
    h.compactLogo = t.Header.Charm.Render(charm) + " " +
        styles.ApplyBoldForegroundGrad(
            t.Header.LogoGradCanvas,
            name,
            t.Header.LogoGradFromColor,
            t.Header.LogoGradToColor,
        ) + " "
}
```

#### HUD 行（拼接其他信息）

```go
func renderHeaderHUD(t *styles.Styles, m *UI, availWidth int) string {
    // 左侧：时间 + logo
    left := t.Header.Charm.Render(timeStr) + " " + h.logo

    // 中间：cwd + LSP + context battery
    details := []string{
        common.PrettyPath(m.session.CWD),
        t.Status.Info.Render("go:"+lspStatus),
        renderContextBattery(t, m.contextUsed, m.contextMax),
        // ...
    }
    middle := strings.Join(details, t.Header.Separator.Render(" │ "))

    // 右侧：时长
    right := t.Header.Duration.Render(formatDuration(time.Since(m.startedAt)))

    return fitHeaderSegments(left, middle, right, availWidth)
}

func renderContextBattery(t *styles.Styles, used, max int) string {
    pct := used * 10 / max
    if pct > 10 { pct = 10 }
    return t.Header.ContextIcon.Render("ctx ") +
        t.Header.BatteryFull.Render(strings.Repeat("█", pct)) +
        t.Header.BatteryEmpty.Render(strings.Repeat("░", 10-pct))
}
```

### 流光的两种风格

#### 静态渐变（mocode 风格）

```go
// 文字从左到右颜色平滑过渡
"oMo code" → 从 primary（Bok 绿）渐变到 secondary（Dolly 紫）
```

#### 动态流光（`internal/util/anim/anim.go`）

```go
// 字符"逐字随机切换"的扫描效果
type Anim struct {
    frames       [][]string  // [step][cell] 预渲染
    cyclingFrames [][]string
    step         int32
    initialized  bool
    birthOffsets []time.Duration
}

func (a *Anim) Step() tea.Cmd {
    return tea.Tick(time.Second/20, func(time.Time) tea.Msg {
        return anim.StepMsg{ID: a.id}
    })
}

// 关键：3 stop 渐变（A→B→A→B），offset 滚动造成"移动"感
ramp := make([]color.Color, width*3)
for i := range ramp {
    t := float64(i) / float64(width*3)
    if t < 0.5 {
        ramp[i] = c1.BlendHcl(c2, t*2)
    } else {
        ramp[i] = c2.BlendHcl(c1, (t-0.5)*2)
    }
}
```

### 设计意图

1. **`uniseg.NewGraphemes`**：正确处理 emoji 和组合字符（避免 emoji 被切两半导致乱码）。
2. **`lipgloss.Blend1D`**：返回每段的颜色切片，**保持 ANSI 安全**（不跨越字节）。
3. **流光 vs 静态**：标题用静态（稳定美观），spinner/工作指示器用流光（动态吸引注意）。
4. **`Hcl` 色彩空间**：动画用 HCL 而非 RGB 混合，避免中间色变得灰暗。

### 复用模板

```go
// 任何"线性渐变文字"场景
func gradientText(input string, from, to color.Color, bold bool) string {
    return styles.ApplyForegroundGrad(
        lipgloss.NewStyle(), input, bold, from, to,
    )
}

// 任何"扫描光带"场景（输入框/进度条/loading）
func shimmerText(width int, frame int, colors ...color.Color) string {
    ramp := lipgloss.Blend1D(width*3, colors...)
    offset := frame % width
    cells := make([]string, width)
    for i := range cells {
        cells[i] = lipgloss.NewStyle().
            Foreground(ramp[offset+i]).
            Render("━")  // 或 "▌"、"█"等
    }
    return strings.Join(cells, "")
}
```

---

## 附录：6 个技巧的共同设计模式

| 模式 | 应用 |
|------|------|
| **语义字段名** | `Header.LogoGradFromColor` 而非 `Color1` |
| **能力通过接口** | `Tree` 的 `Enumerator` 是函数式接口，便于自定义 |
| **预渲染缓存** | 渐变字符按 width 缓存，避免每帧重算 |
| **数据驱动** | Panel/Tree 的渲染只接收 string 数据，组件是"壳" |
| **动画降级** | 静态渐变 + 流光动画两套方案，匹配不同重要级 |