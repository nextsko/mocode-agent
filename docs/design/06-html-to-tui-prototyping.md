# 06 · HTML → TUI 原型建模方法

> 核心问题：能不能先用 HTML 快速画原型，再迁移到 TUI？如何系统地建立映射？

---

## 一、为什么不直接写 TUI？

| 维度 | HTML/CSS | TUI (Go + Lip Gloss) |
|------|----------|----------------------|
| 视觉迭代速度 | ⚡⚡⚡⚡⚡ | ⚡ |
| 调试便利性 | 浏览器 DevTools | 重启/日志 |
| 布局表达 | flex/grid（声明式） | image.Rectangle（手动） |
| 样式调试 | 实时预览 | 重编译 |
| 设计协作 | 设计师友好 | 仅工程师 |
| 终端兼容 | 不需要 | 多终端碎片 |

**核心矛盾**：TUI 的开发循环太慢，先 HTML 出视觉再迁移是**最实用**的工程实践。

---

## 二、3 种主流 HTML→TUI 路径

### 路径 A · 截图注入（最简单）

**流程**：HTML → 截图 → Kitty 图形协议

```html
<!-- prototype.html -->
<div class="chat-list">
  <div class="msg user">Fix the bug</div>
  <div class="msg assistant">
    <div class="thinking">Let me look at the file...</div>
    <div class="content">Found it! <code>auth.go:42</code></div>
  </div>
</div>
```

**Go 侧加载**：

```go
import "github.com/charmbracelet/mosaic"

func (u *UI) loadHTMLPreview(path string) tea.Cmd {
    return func() tea.Msg {
        opts := mosaic.DefaultOptions()
        opts.Terminal = mosaic.Kitty
        img, _ := mosaic.Open(path, opts)
        return imageReadyMsg{image: img}
    }
}

func (u *UI) renderImagePreview(scr uv.Screen, area image.Rectangle) {
    if u.previewImage != nil {
        u.previewImage.Draw(scr, area, uv.ResizeFit)
    }
}
```

**适用**：视觉提案、design review、高保真截图。

---

### 路径 B · 元素映射（推荐 · 系统化）

**核心思想**：把 HTML DOM 节点**一对一映射**到 TUI 组件，每个 DOM 节点对应一个 Go struct。

```
HTML                              TUI
─────────────────────────────────────────
<div>                  ←→        Box
<p>, <span>            ←→        TextBlock
<button>               ←→        Button
<input>                ←→        TextInput
<ul>, <ol>             ←→        ListView
<li>                   ←→        ListItem
<table>                ←→        Table
<img>                  ←→        ImageBlock (Kitty)
<pre><code>            ←→        CodeBlock
flexbox                ←→        flex.Layout
grid                   ←→        grid.Layout
```

#### 元素映射器（设计稿）

```go
package html2tui

type Node struct {
    Tag      string
    Classes  []string
    Attrs    map[string]string
    Children []*Node
    Text     string
}

// HTML → DOM 树
func Parse(html string) (*Node, error) {
    doc, err := html.Parse(strings.NewReader(html))
    if err != nil { return nil, err }
    return convertNode(doc), nil
}

// DOM 树 → TUI 节点树
func (n *Node) ToTUI() tui.Node {
    switch n.Tag {
    case "div":
        if hasClass(n, "chat-msg") { return newChatMsg(n) }
        if hasClass(n, "sidebar") { return newSidebar(n) }
        return newBox(n)
    case "p", "span":
        return newTextBlock(n)
    case "button":
        return newButton(n)
    // ...
    }
}
```

#### 关键样式映射

```go
// 简易 CSS → Lip Gloss 映射
func styleFromCSS(css string) lipgloss.Style {
    style := lipgloss.NewStyle()
    for _, prop := range parseProps(css) {
        switch prop.Key {
        case "color":
            style = style.Foreground(lipgloss.Color(prop.Value))
        case "background", "background-color":
            style = style.Background(lipgloss.Color(prop.Value))
        case "padding":
            t, r, b, l := parsePadding(prop.Value)
            style = style.Padding(t, r, b, l)
        case "border":
            style = style.Border(lipgloss.RoundedBorder())
        case "font-weight":
            if prop.Value == "bold" { style = style.Bold(true) }
        case "text-align":
            style = style.Align(parseAlign(prop.Value))
        }
    }
    return style
}
```

**适用**：复杂布局快速原型，迁移成本低。

---

### 路径 C · 渲染管线（最完整）

把 HTML 渲染成一个**树形 TUI 描述**，再用统一的 Renderer 解释：

```json
{
    "type": "flex",
    "direction": "column",
    "children": [
        {
            "type": "header",
            "content": "🚀 my-tui v0.1"
        },
        {
            "type": "flex",
            "direction": "row",
            "children": [
                {
                    "type": "sidebar",
                    "width": 30,
                    "sections": ["session", "files", "lsp"]
                },
                {
                    "type": "chat",
                    "grow": 1,
                    "messages": [...]
                }
            ]
        },
        {
            "type": "editor",
            "height": 3
        }
    ]
}
```

#### 解释器

```go
type TUIBuilder struct {
    sty *styles.Styles
}

func (b *TUIBuilder) Build(spec *Spec) tea.Model {
    return b.buildNode(spec)
}

func (b *TUIBuilder) buildFlex(spec *FlexSpec) *FlexComponent {
    children := []tea.Model{}
    for _, c := range spec.Children {
        children = append(children, b.buildNode(c))
    }
    return NewFlexComponent(spec.Direction, children, spec.Sizes)
}
```

**适用**：设计系统化、多终端复用。

---

## 三、推荐的工程实践

### 3.1 双轨工作流

```
设计师/PM         工程师
  │                  │
  ▼                  ▼
[HTML/CSS 原型]   [Go + Lip Gloss 实现]
  │                  │
  ├─ Figma 截图     ├─ 内置 dev mode（dev.go）
  │                  │
  ▼                  ▼
   视觉对齐          像素级 1:1 实现
```

### 3.2 视觉对齐工具

**开发期**：浏览器 + 终端并排展示

```
┌─ terminal ──────┐    ┌─ browser ──────┐
│  TUI 实际渲染   │ ←→ │  HTML 原型      │
│                 │    │                 │
└─────────────────┘    └─────────────────┘
```

**CI 期**：golden file + 截图 diff

```yaml
# 截图比对（用 vhs 或 gotty）
- run: |
    go build -o /tmp/tui .
    vhs record.tape  # 录制 TUI 输出到 GIF
    diff recording.gif expected.gif
```

### 3.3 设计 token 共享

**HTML 端**：
```css
:root {
    --color-primary: #bd93f9;
    --color-bg: #0e0e10;
    --border-radius: 4px;
    --spacing: 8px;
}
```

**Go 端**：
```go
var Colors = struct {
    Primary lipgloss.Color
    BG      lipgloss.Color
}{
    Primary: lipgloss.Color("#bd93f9"),
    BG:      lipgloss.Color("#0e0e10"),
}
```

**同步机制**：从 HTML CSS 提取 → 生成 Go 代码

```bash
# extract-css-tokens.js → tokens.go
node extract-css-tokens.js prototype.css > ../internal/ui/styles/tokens.go
```

---

## 四、DOM 到 TUI 的逻辑建模方法

### 4.1 5 条核心映射规则

| HTML | TUI | 差异说明 |
|------|-----|----------|
| `width: 100%` | 占满父容器宽度 | TUI 必须显式计算 |
| `height: 100vh` | 占满剩余垂直空间 | TUI 需用 layout.Fill() |
| `display: flex; flex-direction: row` | `layout.Horizontal` | 用 ultraviolet layout |
| `display: grid` | `layout.Split` 或 Panel 树 | Grid 在 TUI 难做 |
| `overflow: scroll` | `list.List`（lazy render） | TUI 必须虚拟化 |

### 4.2 复杂组件的对应关系

| HTML 模式 | TUI 实现 |
|-----------|----------|
| Modal dialog | `dialog.Overlay` |
| Toast / Notification | `Toast` 单例 |
| Dropdown | `completions` 自动补全 |
| Tabs | `list` + 状态切换 |
| Carousel | Panel 切换 + 动画 |
| Progress bar | `█ ░` 字符拼接 |
| Avatar | 首字母 + 背景色 |
| Tooltip | status bar 临时显示 |
| Loading spinner | `anim.Anim` |
| Tree view | `lipgloss/v2/tree` |

### 4.3 不应映射的 HTML 元素

- `<video>` / `<audio>` → TUI 无对应，转化为 Kitty image
- `<canvas>` → TUI 无 2D 上下文，转化为自定义渲染
- `<iframe>` → TUI 无嵌套 view，转化为 Modal
- `<form>` 复杂表单 → 拆为多个 Dialog

---

## 五、实战：迁移 `prototype.html` 到 TUI

### Step 1：HTML 原型

```html
<div class="tui-app">
  <header class="tui-header">
    <span class="logo">🚀 oMo code</span>
    <span class="status">ctx ████░░ 40%</span>
  </header>
  <main class="tui-main">
    <aside class="tui-sidebar">
      <h3>Files</h3>
      <ul>
        <li>internal/auth.go <span class="add">+12</span></li>
        <li>internal/api.go <span class="del">-3</span></li>
      </ul>
    </aside>
    <section class="tui-chat">
      <div class="msg user">Fix the login bug</div>
      <div class="msg assistant">
        <pre><code>func login() {...}</code></pre>
      </div>
    </section>
  </main>
  <footer class="tui-editor">
    <textarea placeholder="Type your message..."></textarea>
  </footer>
</div>
```

### Step 2：写对应的 Go 组件

```go
// 顶栏
func (u *UI) drawHeader(scr uv.Screen, area image.Rectangle) {
    left := styles.ApplyBoldForegroundGrad(
        u.com.Styles.Header.LogoGradCanvas,
        "🚀 oMo code",
        u.com.Styles.Header.LogoGradFromColor,
        u.com.Styles.Header.LogoGradToColor,
    )
    right := u.renderContextBattery()
    line := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
    uv.NewStyledString(line).Draw(scr, area)
}

// 侧栏
func (u *UI) drawSidebar(scr uv.Screen, area image.Rectangle) {
    sty := u.com.Styles
    var b strings.Builder
    b.WriteString(sty.Sidebar.SectionTitle.Render("Files") + "\n")
    for _, f := range u.session.Files {
        line := fmt.Sprintf("  %s %s\n", f.Path, renderFileStats(f))
        b.WriteString(line)
    }
    uv.NewStyledString(b.String()).Draw(scr, area)
}

// 主聊天区
func (u *UI) drawChat(scr uv.Screen, area image.Rectangle) {
    u.chat.Draw(scr, area)  // 用 chat 包
}

// 编辑器
func (u *UI) drawEditor(scr uv.Rectangle) {
    uv.NewStyledString(u.textarea.View()).Draw(scr, u.layout.editor)
}
```

### Step 3：布局映射

```go
// HTML flexbox  →  TUI image.Rectangle
func (u *UI) generateLayout(w, h int) uiLayout {
    l := uiLayout{}

    // header: fixed 1 row
    l.header = image.Rect(0, 0, w, 1)

    // sidebar: 30 cols (HTML: width: 30ch)
    // chat: flex: 1 (HTML: flex-grow: 1)
    sidebarW := 30
    l.sidebar = image.Rect(0, 1, sidebarW, h-3)
    l.chat    = image.Rect(sidebarW, 1, w, h-3)

    // editor: 3 rows
    l.editor  = image.Rect(0, h-3, w, h-1)

    return l
}
```

---

## 六、可用的辅助工具

### 6.1 已有工具

| 工具 | 用途 | URL |
|------|------|-----|
| **vhs** | 录制 TUI 输出为 GIF/MP4 | github.com/charmbracelet/vhs |
| **glamour** | Markdown → ANSI | github.com/charmbracelet/glamour |
| **mosaic** | 图像 → Kitty/iTerm 协议 | github.com/charmbracelet/mosaic |
| **gum** | 交互 shell prompt | github.com/charmbracelet/gum |
| **freeze** | 生成代码截图 | github.com/charmbracelet/freeze |

### 6.2 自建 HTML 解析器（如需）

```go
import "golang.org/x/net/html"

func ParseHTML(r io.Reader) (*tui.Tree, error) {
    doc, err := html.Parse(r)
    if err != nil { return nil, err }
    
    root := &tui.Tree{}
    walk(doc, func(n *html.Node) {
        if n.Type == html.ElementNode {
            tuiNode := convertElement(n)
            attachToTree(root, tuiNode)
        }
    })
    return root, nil
}
```

### 6.3 截图比对（视觉回归）

```bash
# 用 vhs 录制
vhs assets/tapes/help.tape

# 内容
Output assets/gifs/help.gif
Source assets/source/help.go

# 对比
cmp assets/gifs/help.gif expected/help.gif
```

---

## 七、设计原则

### 7.1 保持 1:1 视觉对应

```
HTML 原型中：
  - 顶部 1 行 header
  - 左侧 30 列 sidebar
  - 主聊天区
  - 底部 3 行 editor

TUI 实现必须：
  - 同样的 4 个区域
  - 同样的相对尺寸
  - 同样的视觉权重
```

### 7.2 接受能力差距

| 能力 | HTML | TUI |
|------|------|-----|
| 鼠标 hover | ✅ | ❌（要主动追踪） |
| 渐变 | ✅ | ⚠️（单向字符渐变） |
| 阴影 | ✅ | ❌ |
| 圆角 | ✅ | ⚠️（用 Unicode 字符） |
| 动效 | ✅ | ⚠️（字符级动画） |
| 抗锯齿字体 | ✅ | ❌ |

**TUI 设计哲学**：**用 ASCII/Unicode 字符本身作为"图形"**。

### 7.3 颜色饱和度调整

TUI 终端颜色有限（256 色或 true color），HTML 中的颜色需**预先映射**：

```js
// HTML → TUI 调色板
const palette = {
    '#bd93f9': 'magenta',     // primary
    '#ff79c6': 'pink',
    '#50fa7b': 'green',
    '#f1fa8c': 'yellow',
    '#0e0e10': 'black',       // bg
};
```

---

## 八、完整工作流示例

```
1. 设计师出 HTML 原型（半天）
   └─> prototype.html（视觉稿）
   └─> tokens.css（颜色/间距）

2. 工程师提取 token（10 min）
   └─> tokens.go（Go 常量）

3. 工程师实现组件（1-2 天）
   └─> 复用 mocode 的 styles/anim/grad 库
   └─> 自己写业务组件

4. 视觉对齐（1-2 小时）
   └─> TUI 截图 vs HTML 截图
   └─> 微调 padding/颜色

5. 回归测试（30 min）
   └─> Golden file
   └─> vhs 录制
```

**总计**：2-3 天从 0 到完整组件，比纯 TUI 开发节省 50% 时间。

---

## 九、避坑指南

### 9.1 不要追求像素级 1:1

TUI 的字号、间距、字体在不同终端差异巨大。**目标是视觉一致**，不是像素一致。

### 9.2 不要用 HTML 的复杂选择器

```css
/* ❌ 难映射 */
.sidebar > .section:nth-child(2) > .item:hover { ... }

/* ✅ 易映射 */
.sidebar-section-2-item-hover { ... }
```

### 9.3 颜色一致性靠 token，不靠手填

```go
// ❌ 错：硬编码颜色
style := lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9"))

// ✅ 对：语义化
style := lipgloss.NewStyle().Foreground(u.com.Styles.Primary)
```

### 9.4 布局不要嵌套太深

```
HTML:           TUI:
                │
body          ┌─┴─┐
└ main        │UI │
  └ sidebar  ├─┴──┤
    └ list   │sidebar │
              │  └ list│
              └───────┘
```

TUI 嵌套越深性能越差（每个矩形都要计算）。**最多 3 层**。

---

## 十、推荐工具栈

```
HTML 原型：      VSCode + Live Server
HTML 解析：      golang.org/x/net/html
样式生成：       简易 CSS parser → Go const
组件开发：       Bubble Tea v2 + Lip Gloss v2 + Ultraviolet
截图比对：       vhs + go test -update
文档同步：       Godoc + Markdown
```

---

## 十一、未来方向

### 11.1 实时 HTML→TUI 转换

```bash
# dev mode：监听 HTML 变化，实时更新 TUI
fswatch prototype.html | xargs -I {} go run cmd/dev-tui/main.go
```

### 11.2 WebAssembly 双端

未来可能：用 Go 写 TUI，编译到 WASM 在浏览器运行，同一套代码双端。

### 11.3 AI 自动迁移

```
HTML + CSS  →  LLM  →  TUI Go 代码
```

但目前 LLM 对 TUI 生成的代码质量参差，需人工 review。

---

## 十二、总结

> **HTML 是 TUI 的"高保真原型"工具，不是替代品。**

| 阶段 | 推荐工具 | 产出 |
|------|----------|------|
| 视觉稿 | HTML/CSS | 截图 |
| Token | CSS → Go 常量 | tokens.go |
| 组件 | Bubble Tea + Lip Gloss | 组件代码 |
| 对齐 | vhs / 截图 diff | 一致性 |
| 测试 | teatest + golden | 回归保护 |

**黄金法则**：HTML 让设计师和工程师在视觉上对齐；TUI 保证性能和终端兼容性。