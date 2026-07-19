# 05 · 组件扩展 / 删除 / 测试方法论

> 核心问题：新增组件最快路径？slash 命令怎么加？删除组件要清理什么？怎么测试单个组件或整个 TUI？

---

## 一、新增组件的标准 SOP

### 1.1 场景分类

| 场景 | 落到位置 | 复杂度 |
|------|----------|--------|
| 工具调用结果展示 | `chat/<tool>.go` | ★★ |
| 工具调用参数类型 | `chat/<tool>.go` + `tools.go` 注册 | ★★ |
| 新的 slash 命令 | `dialog/commands.go` + `setCommandItems` | ★ |
| 新的 Dialog（弹窗） | `dialog/<name>.go` + `ui_dialogs.go` 注册 | ★★★ |
| 新的消息类型 | `chat/<name>.go` + `messages.go` 注册 | ★★★ |
| 新的布局区块 | `ui_layout.go` + `Styles` 加字段 | ★★ |
| 新的全局状态 | `ui.go` 加字段 + Update 加 case | ★ |

---

### 1.2 通用检查清单（无论新增什么）

```
□ 是否需要 styles 字段？ → styles/styles.go 加分组
□ 是否需要布局矩形？    → ui_layout.go 加字段
□ 是否需要焦点切换？    → uiFocusState 加常量
□ 是否需要对话框入口？  → dialog/dialog.go + ui_dialogs.go
□ 是否需要键盘绑定？    → ui_keys.go 加 case
□ 是否需要状态枚举？    → ui.go 加常量
□ 是否需要 pubsub 订阅？→ Update 加 case
□ 是否需要持久化？      → store 加表
□ 是否需要测试？        → 加 *_test.go
```

---

## 二、新增 slash 命令（最快路径 · 15 行）

### 场景：新增 `/login <token>` 命令

#### Step 1：在 `dialog/commands.go` 的 `defaultCommands` 中加一项

```go
func (c *Commands) defaultCommands() []list.Item {
    items := []list.Item{
        // ... 已有项
        list.NewCommandItem(
            c.com.Styles,
            "login",
            "Login",
            "",
            dialog.ActionCustomCommand{
                Content: "login",
                Arguments: []string{},  // 占位
            },
        ).WithDescription("Login with API token"),
    }
    return items
}
```

#### Step 2：在 `ui_dialogs.go` 的 `handleDialogAction` 中加 case

```go
case dialog.ActionCustomCommand:
    if action.Content == "login" {
        return m, m.openLoginDialog()  // 打开登录对话框
    }
```

#### Step 3：实现 `openLoginDialog`

```go
func (m *UI) openLoginDialog() tea.Cmd {
    return func() tea.Msg {
        return openDialogMsg{dialog: dialogs.NewLoginDialog(m.com)}
    }
}
```

**总耗时**：15 分钟，新增代码 < 50 行。

---

## 三、新增工具渲染器（中等路径 · 100 行）

### 场景：新增 `read_pdf` 工具的专属渲染器

#### Step 1：在 `chat/pdf.go` 创建渲染器

```go
package chat

import (
    "charm.land/lipgloss/v2"
    "github.com/nextsko/mocode-agent/internal/ui/styles"
    "github.com/nextsko/mocode-agent/internal/ui/anim"
    "github.com/nextsko/mocode-agent/internal/message"
)

type PDFToolMessageItem struct {
    *baseToolMessageItem
    preview string  // 缓存的预览文本
}

func (p *PDFToolMessageItem) Render(width int) string {
    sty := p.com.Styles
    parts := []string{
        p.header(sty, "PDF", width),
        sty.Tool.Body.Render(p.preview),
    }
    return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

#### Step 2：在 `chat/tools.go` 的 `NewToolMessageItem` 注册

```go
func NewToolMessageItem(com *common.Common, msg *message.Message, call message.ToolCall) ToolMessageItem {
    base := &baseToolMessageItem{ /* ... */ }
    switch call.Name {
    // ...
    case "read_pdf":
        return &PDFToolMessageItem{
            baseToolMessageItem: base,
            preview: call.Result.Content[:min(500, len(call.Result.Content))],
        }
    }
}
```

#### Step 3：在 `tools_render.go` 加共享工具

```go
func toolOutputPDFContent(sty *styles.Styles, content string, width int) string {
    // 截断 + 高亮 + 行号
}
```

**总耗时**：30 分钟，新增代码 ~100 行。

---

## 四、新增 Dialog（复杂路径 · 300 行）

### 场景：新增"项目设置"对话框

#### Step 1：定义 ID 常量

```go
// dialog/settings.go
const SettingsID = "settings"
```

#### Step 2：实现 Dialog 接口

```go
type Settings struct {
    com   *common.Common
    list  *list.FilterableList  // 设置项列表
    input textinput.Model       // 当前编辑的值
    help  help.Model
}

func (s *Settings) ID() string { return SettingsID }

func (s *Settings) HandleMsg(msg tea.Msg) dialog.Action {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "esc":
            return dialog.ActionClose{}
        case "enter":
            return s.commit()
        }
    }
    s.list, _ = s.list.Update(msg)
    return nil
}

func (s *Settings) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    rc := dialog.NewRenderContext(s.com.Styles, area.Dx())
    rc.Title = "Settings"
    rc.AddPart(s.list.View())
    rc.AddPart(s.help.View())
    return dialog.DrawCenter(rc, scr, area)
}
```

#### Step 3：在 `ui_dialogs.go` 注册

```go
// openDialog 中加 case
case dialog.SettingsID:
    d = dialogs.NewSettings(m.com)

// ID 常量列表中加
var dialogIDs = []string{
    dialogs.SettingsID,  // 新增
    // ...
}
```

#### Step 4：在 `ui_keys.go` 加快捷键

```go
case key.Matches(msg, m.keyMap.Settings):
    return m, m.openDialog(dialog.SettingsID)
```

#### Step 5：在 `model/ui.go` 的 KeyMap 加绑定

```go
type KeyMap struct {
    // ...
    Settings key.Binding
}

func DefaultKeyMap() KeyMap {
    return KeyMap{
        // ...
        Settings: key.NewBinding(key.WithKeys("ctrl+,"), key.WithHelp("ctrl+,", "settings")),
    }
}
```

**总耗时**：2 小时，新增代码 ~300 行。

---

## 五、删除组件的标准 SOP

### 5.1 删除清单（以删除 `wechat_*` Dialog 为例）

```
□ 1. dialog/ 目录删除 wechat_*.go
□ 2. ui_dialogs.go 删除:
   - openDialog 中的 case wechat_*
   - dialogIDs 列表中的 wechat_*
□ 3. ui.go 删除:
   - wechat 相关字段（如有）
□ 4. ui_keys.go 删除:
   - wechat 相关快捷键
□ 5. integration/wechat/ 整个包标记为废弃或删除
□ 6. styles/styles.go 删除:
   - WeChat* 样式字段
□ 7. docs 删除相关章节
□ 8. 测试删除
```

### 5.2 检测"漏改"的 4 种方法

```bash
# 1. 全局搜索 ID 字符串
grep -r "wechat" --include="*.go"

# 2. 全局搜索 import 路径
grep -r "package-register/mocode/internal/integration/wechat" --include="*.go"

# 3. 全局搜索 Style 字段
grep -r "WeChat" --include="*.go" --include="*.md"

# 4. 编译检查
go build ./...
go vet ./...
```

### 5.3 软删除 vs 硬删除

| 方式 | 适用 | 副作用 |
|------|------|--------|
| 软删除（disable=true） | 实验性功能 | 保留代码，用户不显示 |
| 硬删除 | 确认废弃 | 代码消失，commit 历史可回溯 |

mocode 风格用 `disable: true`：

```go
type Dialog struct {
    // ...
    Disabled bool
}
```

---

## 六、测试方法论

### 6.1 测试层级

```
┌──────────────────────────────────────────────┐
│ Level 4: E2E 测试                            │  启动整个 TUI，模拟按键，截屏对比
├──────────────────────────────────────────────┤
│ Level 3: 集成测试                            │  多个组件协作
├──────────────────────────────────────────────┤
│ Level 2: 组件测试                            │  单个组件渲染输出
├──────────────────────────────────────────────┤
│ Level 1: 单元测试                            │  纯函数、helper
└──────────────────────────────────────────────┘
```

### 6.2 Level 1: 单元测试（无需启动 Bubble Tea）

适用：纯函数、helper、render 函数、style helper

```go
// chat/tools_helpers_test.go
func TestRoundedEnumerator(t *testing.T) {
    tests := []struct {
        name      string
        lPadding  int
        width     int
        index     int
        isLast    bool
        want      string
    }{
        {"first child", 2, 4, 0, false, "  ├── "},
        {"last child",  2, 4, 1, true,  "  ╰── "},
        {"width=0 default", 1, 0, 0, false, " ├── "},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            enum := roundedEnumerator(tt.lPadding, tt.width)
            children := mockChildren(2)
            got := enum(children, tt.index)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### 6.3 Level 2: 组件测试（用 `teatest`）

适用：单个组件的渲染输出，模拟输入事件

```go
// chat/assistant_test.go
func TestAssistantMessageItem_Render(t *testing.T) {
    msg := &message.Message{
        Role:    message.Assistant,
        Content: []message.Content{{Type: message.TextContent, Text: "Hello, **world**!"}},
    }
    sty := styles.Default()
    com := &common.Common{Styles: sty}

    item := NewAssistantMessageItem(com, msg)

    // 渲染并比对 golden
    output := item.Render(80)
    assert.Contains(t, output, "Hello")
    assert.Contains(t, output, "world")  // markdown 已渲染为 ANSI
}
```

### 6.4 Level 3: 集成测试（多组件协作）

```go
// dialog/permissions_test.go
func TestPermissionsDialog_Flow(t *testing.T) {
    // 1. 创建 dialog
    sty := styles.Default()
    com := &common.Common{Styles: sty}
    perm := NewPermissionsDialog(com, mockPermissionRequest())

    // 2. 模拟用户操作
    perm.Update(tea.KeyMsg{Type: tea.KeyRight})  // 选项切换
    assert.Equal(t, AllowForSession, perm.selected)

    perm.Update(tea.KeyMsg{Type: tea.KeyEnter})  // 确认
    action := perm.HandleMsg(tea.KeyMsg{Type: tea.KeyEnter})

    // 3. 验证返回的 Action
    allowAction, ok := action.(ActionPermissionAllow)
    require.True(t, ok)
    assert.True(t, allowAction.ForSession)
}
```

### 6.5 Level 4: E2E 测试（teatest + golden file）

```go
// ui_test.go
func TestUI_OnboardingFlow(t *testing.T) {
    // 1. 构造 UI
    com := setupCommon(t)
    ui := New(com)

    // 2. 用 teatest 启动
    tm := teatest.NewTestModel(t, ui, teatest.WithInitialTermSize(120, 40))

    // 3. 模拟操作
    tm.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
    tm.Type("/provider openai")
    tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

    // 4. 获取最终状态
    tm.Quit()
    final := tm.FinalModel(t, teatest.WithFinalTimeout(2*time.Second)).(*UI)

    // 5. 验证状态
    assert.Equal(t, uiLanding, final.state)
}
```

### 6.6 Golden File 测试（快照渲染）

```go
import "github.com/hexops/autogold/v2"

func TestDialog_RenderGolden(t *testing.T) {
    sty := styles.FromskoPantera()  // 固定主题
    com := &common.Common{Styles: sty}
    d := NewCommandsDialog(com)
    d.SetSize(image.Rect(0, 0, 84, 24))

    got := d.Render(84)

    autogold.EqualFile(t, got, "testdata/commands.golden")
}
```

更新 golden：`go test -update ./...` 或 `autogold -update`

### 6.7 调试技巧

#### 启用渲染缓存调试

```bash
MOCODE_RENDER_CACHE_DEBUG=1 go run .
```

输出每帧的 cache hit/miss 数。

#### 启用 UI 渲染调试

```bash
MOCODE_UI_DEBUG=true go run .
```

每帧画一个随机色方块，颜色变化频率反映渲染速率。

#### 启用 Anim 调试

```bash
ANIM_DEBUG=true go run .
```

打印每个 anim step 的耗时。

---

## 七、CI 中的 TUI 测试

```yaml
# .github/workflows/tui-test.yml
name: TUI Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.4'

      # 单元 + 集成测试
      - run: go test -race -failfast ./internal/ui/...

      # Golden file 测试
      - run: go test -race -failfast -update=false ./internal/ui/... -tags=golden

      # E2E（teatest，需要真实 PTY）
      - run: xvfb-run -a go test -race -failfast ./internal/ui/e2e/...

      # 视觉回归（可选：截图比对）
      - run: go run ./cmd/snapshot --compare
```

---

## 八、关键测试覆盖盲区

mocode 当前测试覆盖薄弱点：

| 模块 | 覆盖率 | 建议 |
|------|--------|------|
| `dialog/*` | 0% | 添加 HandleMsg 流程测试 |
| `model/ui.go` | 极低 | 拆出可测的子函数 |
| `panel/` | 0% | Tree 渲染 + 切换测试 |
| `list/` | 0% | 滚动边界测试 |
| 渲染输出（golden） | 0% | 添加 golden file |

Crush 在 `internal/agent/agenttest/` 提供了测试友好的 Coordinator 注入，可以参考。

---

## 九、组件迁移指南（从 mocode → 自有项目）

### 9.1 可直接复用的部分

```
✅ styles/        → 完全可复用（仅依赖 lipgloss）
✅ grad.go        → 完全可复用
✅ common/        → 大部分可复用（需重命名包）
✅ list/list.go   → 可复用
✅ util/anim/     → 可复用
```

### 9.2 需要重写的部分

```
⚠️ chat/         → 业务消息模型不同，需要适配
⚠️ dialog/       → Action 类型不同
⚠️ model/ui.go   → 业务逻辑差异大
⚠️ panel/        → 简单，可保留
```

### 9.3 不应复用的部分

```
❌ integration/wechat/   → 业务特定
❌ core/agent/evolution/ → 业务特定
```

---

## 十、扩展性设计 checklist（设计新组件时）

```
□ 1. 是否需要 styles 字段？ → styles.go 加分组
□ 2. 是否有布局依赖？ → layout 加矩形字段
□ 3. 是否需要缓存？ → width-keyed 缓存
□ 4. 是否需要动画？ → 用 anim.Anim
□ 5. 是否有 error 状态？ → ToolStatus 模式
□ 6. 是否需要测试覆盖？ → unit + integration
□ 7. 是否影响布局计算？ → ui_layout.go 更新
□ 8. 是否需要 pubsub？ → Update 加 case
□ 9. 是否需要持久化？ → store 加表
□ 10. 是否需要快捷键？ → ui_keys.go + KeyMap
```

---

## 十一、最快的"抄底"路径（5 步新增组件）

```
Step 1: 定义 ID + 类型（5 min）
  └─> const XXXID = "xxx"
  └─> type XXXDialog struct {...}

Step 2: 实现 Dialog 接口（30 min）
  └─> ID() string
  └─> HandleMsg(msg) Action
  └─> Draw(scr, area) *Cursor

Step 3: 注册到 Overlay（5 min）
  └─> ui_dialogs.go: openDialog 加 case
  └─> 加快捷键

Step 4: 加 styles 字段（10 min）
  └─> styles/styles.go 加分组

Step 5: 写测试（30 min）
  └─> dialog/xxx_test.go
  └─> testdata/xxx.golden

总计：~80 分钟，从 0 到可发布的 Dialog
```
