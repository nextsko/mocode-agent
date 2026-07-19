# Mocode Slash Command 系统架构分析

> 分析时间：2026-06-05  
> 来源仓库：https://github.com/package-register/mocode

---

## ⚠️ 澄清：两套命令系统

Mocode 实际上有**两套独立的命令系统**，都叫"slash command"，但机制完全不同：

| 系统 | 触发方式 | 本质 |
|------|---------|------|
| **Slash Completions** | 输入 `/` 时在输入框下方弹出补全列表 | **聊天输入增强** |
| **Command Palette** | `Ctrl+Shift+P` 打开独立对话框 | **独立命令面板** |

两者最终都调用 `m.sendMessage()` 或 `m.runMCPPrompt()`，所以从用户视角效果相同。但理解分开很重要。

---

## 系统一：Slash Completions（输入补全）

**文件：** `internal/ui/model/completions/slash.go`（Bubble Tea completions 框架）

### 触发链路

```
用户输入 "/" 或继续输入字符
  ↓
Bubble Tea TextInput 触发 textInputMsg
  ↓
m.Update() 收到 msg (type = textInputMsg)
  ↓
completions.Component 管理生命周期
  ↓
m.getSlashCompletions() 被调用（返回 completions.SlashGroup 列表）
  ↓
返回 groups（含 agentItems/sessionItems/toolItems/customItems 等）
  ↓
completions.Component.RenderPopup() 渲染下拉补全浮层
```

### 核心数据结构：`SlashCompletionValue`

```go
type SlashCompletionValue struct {
    Command  string              // 显示文本，如 "/code"
    Desc     string              // 描述文本
    Msg      tea.Msg             // 选中后要发送的 Bubble Tea Message
    Filter   string              // 过滤用，可选
}
```

### 自定义命令补全（关键代码）

```go
// internal/ui/model/ui.go ~ 3440 行
func (m *UI) getSlashCompletions(...) []completions.SlashGroup {
    // ... agentItems, sessionItems, toolItems, settingsItems, helpItems, adminItems ...

    // 自定义命令组
    if len(m.customCommands) > 0 {
        customItems := make([]completions.SlashCompletionValue, 0, len(m.customCommands))
        for _, cmd := range m.customCommands {
            customItems = append(customItems, completions.SlashCompletionValue{
                Command: slashLabelFromCommandID(cmd.ID),  // 如 "/mycmd"
                Desc:    slashDescFromContent(cmd.Content),
                Msg: dialog.ActionRunCustomCommand{        // ← 选中后发此 Message
                    Content:   cmd.Content,
                    Arguments: cmd.Arguments,
                },
            })
        }
        groups = append(groups, completions.SlashGroup{Label: "Custom", Items: customItems})
    }
    return groups
}
```

### 用户选择补全后的执行链路

```
用户从下拉列表选中一个自定义命令项
  ↓
completions.Component 发送 msg（就是 SlashCompletionValue.Msg）
  ↓
m.Update() 收到 msg
  ↓
case dialog.ActionRunCustomCommand:
  ├─ 若有 Arguments 且 Args == nil → 弹出参数对话框 ArgumentsDialog
  └─ 若无 Arguments 或已有 Args
      ├─ substituteArgs() 将 $ARG_NAME 占位符替换为实际值
      └─ m.sendMessage(content) → 发给 Agent 处理
  ↓
m.dialog.CloseFrontDialog()
```

### `sendMessage` 实现

```go
// internal/ui/model/ui.go ~ 3718 行
func (m *UI) sendMessage(content string, ...) tea.Cmd {
    // 1. 创建 Session（若还没有）
    if !m.hasSession() {
        newSession, _ := m.com.Workspace.CreateSession(ctx, "New Session")
        m.session = &newSession
        cmds = append(cmds, m.loadSession(newSession.ID))
    }
    // 2. 追踪已读文件
    ctx := context.Background()
    cmds = append(cmds, func() tea.Msg {
        for _, path := range m.sessionFileReads {
            m.com.Workspace.FileTrackerRecordRead(ctx, m.session.ID, path)
            m.com.Workspace.LSPStart(ctx, path)
        }
        return nil
    })
    // 3. 调用 Agent
    cmds = append(cmds, func() tea.Msg {
        err := m.com.Workspace.AgentRun(ctx, sessionID, content, attachments...)
        return nil  // 错误通过 InfoMsg 返回
    })
    return tea.Batch(cmds...)
}
```

---

## 系统二：Command Palette（命令面板）

**文件：** `internal/ui/dialog/commands.go`

### 触发链路

```
用户按 Ctrl+Shift+P
  ↓
tea.KeyMsg 被捕获
  ↓
c.list.SetSelected() + c.setCommandItems(c.selected)
  ↓
refreshRegistry() 构建 CommandDescriptor 列表
  ↓
显示扁平化命令列表（支持模糊搜索）
```

### 三层注册机制

```
CommandProvider 接口（provider 端）
  ├─ ProviderInfo:  {ID, Name, Kind}  — 元信息
  └─ Commands(ctx) []CommandDescriptor  — 返回命令列表

CommandRegistry（聚合层）
  ├─ 聚合多个 CommandProvider
  └─ Commands(ctx) → 合并所有 providers 的 descriptor

StaticCommandProvider（具体实现）
  ├─ 内嵌 ProviderInfo
  └─ Items: []CommandDescriptor — 硬编码命令列表
```

### `refreshRegistry()` 实际注册内容

```go
func (c *Commands) refreshRegistry() {
    c.registry = capability.NewCommandRegistry(
        // 内置命令：系统级
        capability.StaticCommandProvider{
            Info:  ProviderInfo{ID: "builtin", ...},
            Items: c.defaultCommandDescriptors(),   // /plan, /code, /switch_mode...
        },
        // Session 操作：会话级
        capability.StaticCommandProvider{
            Info:  ProviderInfo{ID: "session-actions", ...},
            Items: c.sessionCommandDescriptors(),  // summarize, export_markdown...
        },
        // 自定义命令：用户定义
        capability.StaticCommandProvider{
            Info:  ProviderInfo{ID: "custom", ...},
            Items: c.customCommandDescriptors(),    // 来自 config
        },
        // MCP Prompts：第三方工具
        capability.StaticCommandProvider{
            Info:  ProviderInfo{ID: "mcp-prompts", ...},
            Items: c.mcpPromptCommandDescriptors(), // 来自 MCP servers
        },
    )
}
```

### 用户选择命令后的链路

```
用户按 Enter / Right 选中命令
  ↓
item.Action() 被调用 → 返回 tea.Msg
  ↓
c.HandleMsg(msg) 返回 Action
  ↓
c.Update() 处理 Action → 修改 model 状态
  ↓
主 Update 收到 dialog.ActionClose{} → 关闭面板
```

### Custom Command 的 Action 构造

```go
// internal/ui/dialog/commands.go ~ 709 行
func (c *Commands) customCommandDescriptors() []CommandDescriptor {
    for _, cmd := range c.customCommands {
        action := ActionRunCustomCommand{
            Content:   cmd.Content,
            Arguments: cmd.Arguments,
        }
        descriptors = append(descriptors, CommandDescriptor{
            ID:        "custom_" + cmd.ID,
            Title:     cmd.Name,
            Category:  CommandCategoryUser,  // ← UI 分类
            Arguments: cmd.Arguments,
            Risk:      RiskLevelRead,
            Action:    action,  // ← 命令选中后执行此 action
        })
    }
}
```

### MCP Prompt 的 Action 构造

```go
func (c *Commands) mcpPromptCommandDescriptors() []CommandDescriptor {
    for _, cmd := range c.mcpPrompts {
        action := ActionRunMCPPrompt{
            Title:       cmd.Title,
            Description: cmd.Description,
            PromptID:    cmd.PromptID,
            ClientID:    cmd.ClientID,
            Arguments:   cmd.Arguments,
        }
        // ...
    }
}
```

### UI Model 处理 Action

```go
// internal/ui/model/ui.go ~ 1714 行
case dialog.ActionRunCustomCommand:
    if len(msg.Arguments) > 0 && msg.Args == nil {
        // 弹出参数对话框
        argsDialog := dialog.NewArguments(m.com, ..., msg.Arguments, msg)
        m.dialog.OpenDialog(argsDialog)
        break
    }
    content := substituteArgs(msg.Content, msg.Args)
    cmds = append(cmds, m.sendMessage(content))  // ← 发给 Agent
    m.dialog.CloseFrontDialog()

case dialog.ActionRunMCPPrompt:
    if len(msg.Arguments) > 0 && msg.Args == nil {
        // 弹出参数对话框
        argsDialog := dialog.NewArguments(...)
        m.dialog.OpenDialog(argsDialog)
        break
    }
    cmds = append(cmds, m.runMCPPrompt(msg.ClientID, msg.PromptID, msg.Args))
```

### `runMCPPrompt` 实现（MCP 提示词执行）

```go
// internal/ui/model/ui.go ~ 4947 行
func (m *UI) runMCPPrompt(clientID, promptID string, args map[string]string) tea.Cmd {
    load := func() tea.Msg {
        prompt, err := m.com.Workspace.GetMCPPrompt(clientID, promptID, args)
        if err != nil { return util.ReportError(err)() }
        if prompt == "" { return nil }
        return sendMessageMsg{Content: prompt}  // ← 转为内部 message
    }
    cmds := []tea.Cmd{m.dialog.StartLoading(), load, closeDialogMsg}
    return tea.Sequence(cmds...)
}
```

---

## 统一执行图（两系统的汇合点）

```
┌─────────────────────────────────────────────────────────────┐
│                     用户操作                                  │
│  输入 "/" → 选择补全项    Ctrl+Shift+P → 选择命令项           │
└──────────────────────┬──────────────────────────────────────┘
                       ↓
┌──────────────────────────────────────────────────────────────┐
│  Bubble Tea Message（TeaMsg）                                  │
│  case dialog.ActionRunCustomCommand                          │
│  case dialog.ActionRunMCPPrompt                              │
└──────────────────────┬───────────────────────────────────────┘
                       ↓
┌──────────────────────────────────────────────────────────────┐
│  UI Model (ui.go)                                             │
│                                                              │
│  ActionRunCustomCommand 处理:                                 │
│    ├─ 有参数对话框？ → 弹 ArgumentsDialog                     │
│    └─ substituteArgs() → m.sendMessage(content)              │
│                                                              │
│  ActionRunMCPPrompt 处理:                                     │
│    ├─ 有参数对话框？ → 弹 ArgumentsDialog                     │
│    └─ m.runMCPPrompt(clientID, promptID, args)              │
└──────────────────────┬───────────────────────────────────────┘
                       ↓
┌──────────────────────────────────────────────────────────────┐
│  m.sendMessage() 或 m.runMCPPrompt()                          │
│                                                              │
│  sendMessage:                                                 │
│    ├─ Workspace.CreateSession()（必要时）                     │
│    ├─ Workspace.AgentRun(ctx, sessionID, content)            │
│    └─ Workspace.LSPStart()（文件追踪）                        │
│                                                              │
│  runMCPPrompt:                                                │
│    ├─ Workspace.GetMCPPrompt(clientID, promptID, args)     │
│    └─ → sendMessageMsg{Content: prompt}                       │
└──────────────────────┬───────────────────────────────────────┘
                       ↓
┌──────────────────────────────────────────────────────────────┐
│  Workspace.AgentRun()                                          │
│    └─ → Agent Loop 处理消息                                   │
└───────────────────────────────────────────────────────────────┘
```

---

## 参数替换机制

```go
// internal/ui/model/ui.go ~ 1745 行
func substituteArgs(content string, args map[string]string) string {
    for name, value := range args {
        placeholder := "$" + name
        content = strings.ReplaceAll(content, placeholder, value)
    }
    return content
}
```

自定义命令内容中的 `$ARG_NAME` 会被替换为用户实际输入的值。

---

## 自定义命令配置来源

`m.customCommands` 在 `ui.go` 中通过以下途径注入：

```go
// internal/ui/model/ui.go
m.customCommands = cfg.CustomCommands
m.mcpPrompts = ws.GetMCPPrompts()  // 从 workspace 读取 MCP prompts
```

配置文件 `config.GlobalConfig()` 中定义 `CustomCommands` 列表：

```go
// internal/config/config.go
type CustomCommand struct {
    ID       string
    Name     string
    Content  string  // 命令内容（含 $ARG 占位符）
    Arguments []Argument
}
```

---

## 风险等级（Risk Level）

| 级别 | 说明 | 来源 |
|------|------|------|
| `RiskLevelRead` | 只读，无副作用 | 普通命令 |
| `RiskLevelWrite` | 写操作 | Session export 等 |
| `RiskLevelNetwork` | 网络操作 | MCP prompts |
| `RiskLevelDangerous` | 高风险操作 | 未使用 |

`CommandRegistry` 提供 `FilterByRisk()` 方法，供 UI 按用户权限级别过滤命令。

---

## 相关文件索引

| 文件 | 作用 |
|------|------|
| `internal/ui/model/completions/slash.go` | Slash 补全浮层组件 |
| `internal/ui/model/ui.go` | 主 UI Model，`getSlashCompletions`、`sendMessage`、`runMCPPrompt` |
| `internal/ui/dialog/commands.go` | Command Palette UI，`CommandRegistry` 构造 |
| `internal/ui/dialog/actions.go` | 所有 `Action*` struct 定义 |
| `internal/ui/dialog/arguments.go` | 参数对话框 |
| `internal/capability/command.go` | `CommandRegistry`、`CommandProvider` 接口定义 |
| `internal/config/config.go` | `CustomCommand` 配置结构 |
| `internal/commands/` | 命令参数解析、占位符处理 |