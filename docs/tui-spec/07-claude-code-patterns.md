# 07 · Claude Code 优秀设计模式吸收

> 来源：anthropics/claude-code 官方实现 + 各 fork（SymHarix、Janlaywss、shareAI-lab）+ 公开设计分析（codeagents/wenshao）
> 目标：提炼 Claude Code 在 **subagent / team / AskUserQuestion / 权限提示** 4 大领域的最佳实践，转化为 mocode 可落地的设计。

---

## 一、Subagent 渲染架构

### Claude Code 的做法

Claude Code **不创建独立 tmux pane** 渲染 subagent，而是把子 agent 的活动**嵌入到父 agent 的 `Agent` tool card 里**作为内联子块。

#### 事件流（来自 `app-messages.js:1030-1040`）

```js
case "subagent_activity":
    updateSubagentActivity(msg.parentToolId, msg.text);  // 流式文本增量
    break;
case "subagent_tool":
    addSubagentToolEntry(msg.parentToolId, msg.toolName, msg.toolId, msg.text);
    break;
case "subagent_done":
    markSubagentDone(msg.parentToolId, msg.status, msg.summary, msg.usage);
    break;
```

**核心设计**：子 agent 用父 agent 的 `toolId` 作为 DOM scoping key，所有子活动都渲染到父 tool card 内。

#### 三种 Subagent 模式

```ts
// 1. 普通 subagent：指定 subagent_type，独立 context
{
    agentType: 'general-purpose',
    tools: ['Bash', 'Read', 'Edit'],
    maxTurns: 50,
    model: 'inherit',
}

// 2. Fork subagent：复用父 context（缓存友好）
{
    agentType: 'fork',
    tools: ['*'],
    maxTurns: 200,
    model: 'inherit',
    permissionMode: 'bubble',  // 权限提示冒泡到父终端
}

// 3. 后台 async 启动（run_in_background: true）
{
    run_in_background: true,
    // 立即返回 { status: "async_launched" }
    // 父 agent 继续做事，子 agent 完成时通知
}
```

#### Fork 的核心价值

**Cache-friendly message prefix**：子 agent 复用父对话历史的前缀，命中 prompt cache，节省成本和延迟。

### mocode 现状 vs 改进

**当前**（`chat/agent.go`）：
```go
// 已经做对了 70%：nested tools + tree 渲染
childTools := tree.Root(header)
for _, nested := range r.agent.nestedTools {
    childTools.Child(nested.Render(width))
}
```

**改进点**：

#### 1. 三种 subagent 模式支持

```go
type AgentType string
const (
    AgentGeneralPurpose AgentType = "general-purpose"
    AgentFork           AgentType = "fork"
    AgentAsync          AgentType = "async"
)

type AgentParams struct {
    Type          AgentType `json:"type"`
    Description   string    `json:"description"`  // 3-5 词任务摘要
    Prompt        string    `json:"prompt"`       // 完整指令
    RunInBackground bool    `json:"run_in_background"`
    Tasks         []Task    `json:"tasks,omitempty"`
    // ... fork 模式特有字段
    ParentSessionID string `json:"parent_session_id,omitempty"`
}
```

#### 2. 三态视觉区分

```go
func (a *AgentToolMessageItem) statusIcon(sty *styles.Styles) string {
    switch {
    case a.isPending() && !a.HasResult():
        return sty.Tool.IconPending.Render("◌")  // 排队
    case a.isPending() && a.HasResult():
        return sty.Tool.IconPending.Render("●")  // 运行中
    case a.HasResult() && a.Status == ToolSuccess:
        return sty.Tool.IconSuccess.Render("✓")  // 完成
    case a.HasResult() && a.Status == ToolError:
        return sty.Tool.IconError.Render("✗")    // 失败
    case a.isCanceled():
        return sty.Tool.IconCancelled.Render("–") // 取消
    }
}
```

#### 3. Header 简化（description 而非完整 prompt）

```go
// Claude Code 风格：3-5 词任务摘要
header := fmt.Sprintf("Task: %s", truncateWords(a.params.Description, 5))
// 而不是
header := fmt.Sprintf("Agent: %s", truncate(a.params.Prompt, 80))
```

#### 4. Fork 模式的特殊渲染

```go
func (a *AgentToolMessageItem) RenderCompact(width int) string {
    if a.params.Type == AgentFork {
        // Fork：显示"继承自父上下文"徽章
        return sty.Tool.AgentForkBadge.Render("⟳ fork · shared context") + "\n" + a.nestedRender(width)
    }
    return a.nestedRender(width)
}
```

---

## 二、Team / 并行 Agent 模式

### Claude Code 的 3 种渲染模式

```ts
// 来自 useSwarmBanner.ts
if (insideTmux === false && !inProcessMode && !nativePanes) {
    return { text: `tmux -L ${socket} attach`, bgColor: ... }
    // 模式 A：外部 tmux 多 pane
}
if ((insideTmux || inProcessMode || nativePanes) && viewedTeammate) {
    return { text: `@${agentName}`, bgColor: ... }
    // 模式 B/C：内嵌 + 团队视图
}
```

### "Agents View" dashboard（Claude Code 2.1.202）

**核心设计**：
- **Live 阶段**：活跃 agents 渲染到 compact live panel
- **Committed 阶段**：完成结果 commit 到 `<Static>` 历史，不重绘
- **路由条件**：仅当 `agent call count >= 2 && all calls are agents && !pending confirmation` 时显示

**导航**：`←` 打开 agents 视图（dashboard 形式）

**折叠策略**：
> "Surplus idle agents now collapse into an expandable summary row"（多余 idle agents 折叠到可展开摘要行）

### mocode 现状 vs 改进

#### 改进 1 · Live + Static 双层渲染

```go
// 当前：所有内容都重绘（panel_view.go:103-108）
func (v *View) RenderMain(width int) string {
    // 全部渲染
}

// 改进：区分活跃和已完成
type AgentPanelView struct {
    mu        sync.RWMutex
    live      map[string]*panel.Panel  // 活跃 agents
    committed []string                   // 已完成摘要
    visible   bool
}

func (v *AgentPanelView) AddLive(panel *panel.Panel) {
    v.mu.Lock()
    defer v.mu.Unlock()
    v.live[panel.ID] = panel
}

func (v *AgentPanelView) Commit(panelID string, summary string) {
    v.mu.Lock()
    defer v.mu.Unlock()
    if p, ok := v.live[panelID]; ok {
        delete(v.live, panelID)
        v.committed = append(v.committed, summary)
    }
}
```

#### 改进 2 · Aggregate Rows 而非字面 panes

```go
// 紧凑行布局
type AgentRow struct {
    Name      string
    Status    string  // running | queued | idle | blocked | done | failed
    Activity  string  // "scanning auth middleware"
    Elapsed   time.Duration
    Result    string  // 完成时填充
}

// 渲染
func renderAgentRows(rows []AgentRow, sty *styles.Styles, width int) string {
    var b strings.Builder
    for _, r := range rows {
        // 固定列宽防止抖动
        nameCol := padRight(r.Name, NAME_COL_WIDTH)
        statusGlyph := statusIcon(r.Status, sty)
        activity := r.Activity
        b.WriteString(fmt.Sprintf("%s %s %s %s\n",
            statusGlyph, nameCol, activity, formatElapsed(r.Elapsed)))
    }
    return b.String()
}

const NAME_COL_WIDTH = 26  // 固定名宽
```

#### 改进 3 · Detail Dialog（选中 agent 后）

```go
// 按 Enter 打开选中 agent 的详情
type AgentDetailDialog struct {
    com       *common.Common
    agentID   string
    tools     []ToolMessageItem
    summary   string
    help      help.Model
}

func (d *AgentDetailDialog) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    rc := dialog.NewRenderContext(d.com.Styles, area.Dx())
    rc.Title = fmt.Sprintf("Agent: %s", d.agentID)
    rc.AddPart(d.summary)
    rc.AddPart(strings.Repeat("─", area.Dx()-4))
    rc.AddPart(renderNestedTools(d.tools, area.Dx()-4))
    rc.AddPart(d.help.View())
    return dialog.DrawCenter(rc, scr, area)
}
```

#### 改进 4 · 状态正交化（关键洞察）

```go
// Claude Code 强调：execution / task / interaction / communication 四态独立
type AgentRuntimeState struct {
    Execution string  // running | idle | missing
    Task      string  // queued | assigned | blocked | done | failed
    Interaction string // none | permission | question
    Communication int  // unread count
    LastUpdate time.Time
}
```

#### 改进 5 · 折叠策略（>6 agents 时）

```go
func (v *AgentPanelView) Render(width int) string {
    if len(v.live) > 6 {
        // 折叠：显示 5 个 + "N more..."
        var b strings.Builder
        i := 0
        for id, p := range v.live {
            if i >= 5 {
                b.WriteString(fmt.Sprintf("... and %d more agents (press 'e' to expand)\n", len(v.live)-5))
                break
            }
            b.WriteString(renderAgentRow(p, width))
            i++
        }
        return b.String()
    }
    return v.renderAll(width)
}
```

---

## 三、AskUserQuestion 组件

### Claude Code 设计

#### 数据 Schema

```ts
interface Question {
    question: string;       // 完整问题
    header: string;         // 短标签 ≤12 字符（如 "Auth method"）
    options: Array<{
        label: string;       // 简短标签
        description: string; // 权衡说明
        preview?: string;    // 可选预览内容
    }>;
    multiSelect: boolean;
}
```

#### UI 布局（两栏 + 导航条）

```
┌─ 1. Auth Method ── 2. Database ── [Submit] ─┐  ← 导航条（多问题切换）
│                                              │
│   ┌─ LEFT (30 cols) ─┐  ┌─ RIGHT ──────┐    │
│   │ ❯ 1. JWT         │  │ Preview:     │    │
│   │   Stateless, ... │  │ JWT details  │    │
│   │                  │  │              │    │
│   │   2. Session     │  │ Pros:        │    │
│   │   Stateful, ...  │  │ - Simple     │    │
│   │                  │  │ Cons:        │    │
│   │   3. OAuth       │  │ - No revoke  │    │
│   │   Third party    │  │              │    │
│   └──────────────────┘  └──────────────┘    │
│                                              │
│ Enter select · ↑↓ navigate · Tab switch · Esc│
└──────────────────────────────────────────────┘
```

#### 渲染代码（PreviewQuestionView.tsx:271-326）

```tsx
const LEFT_PANEL_WIDTH = 30;
const GAP = 4;
const previewMaxWidth = columns - LEFT_PANEL_WIDTH - GAP;

<Box flexDirection="column" marginTop={1} tabIndex={0} autoFocus onKeyDown={handleKeyDown}>
    {/* 多问题导航 */}
    <QuestionNavigationBar
        questions={questions}
        currentQuestionIndex={currentQuestionIndex}
        answers={answers}
    />
    
    {/* 左右两栏 */}
    <Box flexDirection="row" flexWrap="wrap">
        {/* 左：选项 */}
        <Box width={LEFT_PANEL_WIDTH} flexDirection="column">
            {options.map((option, index) => (
                <Box key={option.label}>
                    {isFocused ? <Text color="suggestion">▸</Text> : <Text> </Text>}
                    <Text dimColor> {index + 1}.</Text>
                    <Text color={isSelected ? 'success' : isFocused ? 'suggestion' : undefined}
                          bold={isFocused}>
                        {' '}{option.label}
                    </Text>
                    {isSelected && <Text color="success"> ✓</Text>}
                </Box>
            ))}
        </Box>
        
        {/* 右：预览 */}
        <Box flexGrow={1} width={previewMaxWidth} flexDirection="column">
            <Text>{focusedOption.preview}</Text>
        </Box>
    </Box>
    
    {/* 帮助栏 */}
    <Text color="inactive" dimColor>
        Enter to select · ↑/↓ to navigate · n to add notes
        {questions.length > 1 && '· Tab to switch questions'} · Esc to cancel
    </Text>
</Box>
```

### mocode 落地：`dialog/ask_user_question.go`

#### Schema

```go
package dialog

type AskUserQuestionRequest struct {
    Questions []Question `json:"questions"`
}

type Question struct {
    Question    string   `json:"question"`     // 完整问题文本
    Header      string   `json:"header"`       // 短标签（≤12 字符）
    Options     []Option `json:"options"`
    MultiSelect bool     `json:"multiSelect"`
}

type Option struct {
    Label       string `json:"label"`
    Description string `json:"description"`
    Preview     string `json:"preview,omitempty"`
}

const AskUserQuestionID = "ask_user_question"
```

#### 组件结构

```go
type AskUserQuestion struct {
    com          *common.Common
    questions    []Question
    answers      []map[int]bool  // 每个问题的选中索引集合
    currentQ     int             // 当前问题索引
    focusedOpt   int             // 当前选项索引
    notes        []string        // 每个问题的备注
    isNotesMode  bool
    width        int
    help         help.Model
}

func (d *AskUserQuestion) ID() string { return AskUserQuestionID }

// Action 返回
type AskUserQuestionResult struct {
    Answers map[string][]string `json:"answers"`  // question → selected labels
    Notes   map[string]string   `json:"notes"`
}
```

#### HandleMsg

```go
func (d *AskUserQuestion) HandleMsg(msg tea.Msg) dialog.Action {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "up", "k":
            if d.focusedOpt > 0 { d.focusedOpt-- }
        case "down", "j":
            if d.focusedOpt < len(d.questions[d.currentQ].Options)-1 {
                d.focusedOpt++
            }
        case " ":
            // 多选切换
            q := &d.questions[d.currentQ]
            if q.MultiSelect {
                if d.answers[d.currentQ] == nil {
                    d.answers[d.currentQ] = make(map[int]bool)
                }
                d.answers[d.currentQ][d.focusedOpt] = !d.answers[d.currentQ][d.focusedOpt]
            }
        case "tab":
            // 切换问题
            if d.currentQ < len(d.questions)-1 {
                d.currentQ++
                d.focusedOpt = 0
            }
        case "shift+tab":
            if d.currentQ > 0 {
                d.currentQ--
                d.focusedOpt = 0
            }
        case "enter":
            // 提交或进入下一个问题
            if d.currentQ == len(d.questions)-1 {
                return d.buildResult()
            }
            d.currentQ++
            d.focusedOpt = 0
        case "n":
            d.isNotesMode = !d.isNotesMode
        case "esc":
            return dialog.ActionClose{}
        }
    }
    return nil
}
```

#### Draw（两栏布局）

```go
func (d *AskUserQuestion) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    sty := d.com.Styles
    const (
        leftPanelWidth = 30
        gap            = 4
    )
    width := area.Dx() - 4  // dialog border
    rightWidth := width - leftPanelWidth - gap

    // 导航条（多问题）
    var nav strings.Builder
    for i, q := range d.questions {
        if i == d.currentQ {
            nav.WriteString(sty.Dialog.QuestionTabActive.Render(fmt.Sprintf(" %d. %s ", i+1, q.Header)))
        } else {
            nav.WriteString(sty.Dialog.QuestionTabInactive.Render(fmt.Sprintf(" %d. %s ", i+1, q.Header)))
        }
        nav.WriteString(" ")
    }
    nav.WriteString(sty.Dialog.QuestionTabSubmit.Render(" [Submit] "))

    // 问题文本
    q := d.questions[d.currentQ]
    questionText := sty.Dialog.QuestionTitle.Render(q.Question)

    // 左栏：选项
    var left strings.Builder
    for i, opt := range q.Options {
        isFocused := i == d.focusedOpt
        isSelected := d.answers[d.currentQ][i]
        
        pointer := " "
        if isFocused { pointer = "❯" }
        
        glyph := "○"  // 单选默认
        if q.MultiSelect {
            if isSelected { glyph = "☑" } else { glyph = "☐" }
        }
        
        labelColor := sty.Dialog.OptionLabel
        if isFocused { labelColor = sty.Dialog.OptionFocused }
        if isSelected { labelColor = sty.Dialog.OptionSelected }
        
        left.WriteString(fmt.Sprintf("%s %s %s\n",
            sty.Dialog.OptionPointer.Render(pointer),
            labelColor.Render(glyph+" "+opt.Label),
            sty.Dialog.OptionDescription.Render(opt.Description),
        ))
    }

    // 右栏：预览
    var right strings.Builder
    focused := q.Options[d.focusedOpt]
    if focused.Preview != "" {
        right.WriteString(sty.Dialog.PreviewTitle.Render("Preview:") + "\n")
        right.WriteString(focused.Preview)
    }

    // 拼接
    full := questionText + "\n" +
            nav.String() + "\n\n" +
            lipgloss.JoinHorizontal(lipgloss.Top,
                left.String(),
                strings.Repeat(" ", gap),
                right.String(),
            ) + "\n\n" +
            d.help.View()
    
    rc := dialog.NewRenderContext(sty, area.Dx())
    rc.Title = "Question"
    rc.AddPart(full)
    return dialog.DrawCenter(rc, scr, area)
}
```

#### 关键设计点

1. **导航条**：多问题用 tabs 形式展示当前/全部问题
2. **数字快捷键**：`1` `2` `3` 直接跳到对应选项
3. **空格键 toggle**：多选时空格切换选中状态
4. **Enter 智能提交**：最后一个问题 enter 才提交，否则跳下一题
5. **Esc 取消**：永远允许取消（不要强制回答）
6. **预览面板**：右侧显示当前 focus 选项的预览（如果提供）

#### Styles 扩展

```go
// styles/styles.go
type DialogStyles struct {
    // ... 已有
    QuestionTitle        lipgloss.Style  // "Choose an option:"
    QuestionTabActive    lipgloss.Style  // 当前问题 tab
    QuestionTabInactive  lipgloss.Style  // 其他问题 tab
    QuestionTabSubmit    lipgloss.Style  // 提交按钮
    OptionPointer        lipgloss.Style  // ❯ / 空格
    OptionLabel          lipgloss.Style  // 选项文字
    OptionFocused        lipgloss.Style  // focus 时的颜色
    OptionSelected       lipgloss.Style  // 选中时的颜色（绿色）
    OptionDescription    lipgloss.Style  // 描述文字
    PreviewTitle         lipgloss.Style  // "Preview:"
}
```

---

## 四、Permission Dialog 设计

### Claude Code 的权限模式

```ts
type PermissionMode = 
    | 'default'           // 手动批准
    | 'acceptEdits'       // 自动批准 Edit/Write
    | 'bypassPermissions' // 全部跳过
    | 'dontAsk'           // 不询问（deny）
    | 'plan'              // plan 模式（先 plan 后执行）
    | 'auto';             // classifier 模式
```

### Permission Result Schema

```ts
type PermissionResult = {
    behavior: 'allow' | 'deny',
    updatedInput?: object,
    updatedPermissions?: PermissionUpdate[],
    toolUseID?: string,
    decisionClassification?: 
        | 'user_temporary'    // allow-once
        | 'user_permanent'    // allow-always
        | 'user_reject',      // deny
    message?: string,         // deny 时
    interrupt?: boolean,     // 是否中断 agent
}
```

### 三种分类

| 分类 | 语义 | 持久化 |
|------|------|--------|
| `user_temporary` | 只这一次 | 否 |
| `user_permanent` | 加入 allowlist | 是 |
| `user_reject` | 加入 denylist | 是 |

### mocode `dialog/permissions.go` 增强

**当前**已支持三选项（Allow / Allow for Session / Deny），但可以参考 Claude Code 增强：

#### 1. 工具特定的详细信息

```go
type PermissionRequest struct {
    AgentName   string                 `json:"agent_name"`   // 谁在请求
    ToolName    string                 `json:"tool_name"`
    ToolInput   map[string]any         `json:"tool_input"`
    Risk        string                 `json:"risk"`         // 风险评估
    Rule        *PermissionRule        `json:"rule,omitempty"` // 已有的匹配规则
    DecisionClass string               `json:"decision_class"` // 临时/永久/拒绝
}
```

#### 2. 渲染：标题 + 命令 + 风险摘要

```
┌─ Permission Required ────────────────────────────┐
│ Agent: api-reviewer                              │
│ Tool: Bash                                       │
│ Command: npm test                                │
│ Risk: Network access · Modifies 0 files         │
│                                                  │
│   Allow once   Allow for session   Deny          │
└──────────────────────────────────────────────────┘
```

#### 3. `bubble` 模式（subagent 权限冒泡）

```go
// subagent 权限冒泡：父 agent 的 terminal 收到子 agent 的权限请求
type PermissionBubble struct {
    parentSessionID string
    childSessionID  string
    request         PermissionRequest
}

// 在 UI 中：标记 "via subagent: <childAgentName>"
func (d *Permissions) titleWithBubble() string {
    if d.request.BubbleFrom != "" {
        return fmt.Sprintf("Permission (via %s)", d.request.BubbleFrom)
    }
    return "Permission Required"
}
```

#### 4. 自动模式（classifier）

```go
// mocode 后续可加：classifier 自动决策
type AutoModeDecision struct {
    Allow    bool
    Reason   string
    Confidence float64
}

// UI 展示
func (d *Permissions) renderAutoBadge(sty *styles.Styles) string {
    if d.mode == AutoMode {
        return sty.Status.Warning.Render("⚠ auto-mode: classifier decided to ask anyway")
    }
    return ""
}
```

---

## 五、工具并发执行（isConcurrencySafe）

### Claude Code 模式

```ts
isConcurrencySafe(input) {
    return isReadOnlyCommand(input.command)  // 默认 false（fail-closed）
}

StreamingToolExecutor.parallelize(tools.filter(t => t.isConcurrencySafe(input)))
```

### mocode 改进

**当前**（`core/agent/coordinator.go`）：所有工具调用顺序执行

**改进**：

```go
// tools.go
type ToolConcurrency interface {
    IsConcurrencySafe() bool
}

// 注册时声明
type BashTool struct{}
func (b *BashTool) IsConcurrencySafe() bool {
    return false  // 默认 fail-closed
}

// 特殊工具：read-only 命令
type GlobTool struct{}
func (g *GlobTool) IsConcurrencySafe() bool { return true }

type GrepTool struct{}
func (g *GrepTool) IsConcurrencySafe() bool { return true }

// coordinator 中：
func (c *Coordinator) executeTools(tools []ToolCall) {
    var parallel []ToolCall
    var sequential []ToolCall
    for _, t := range tools {
        if t.IsConcurrencySafe() {
            parallel = append(parallel, t)
        } else {
            sequential = append(sequential, t)
        }
    }
    
    // 并行执行 read-only 工具
    if len(parallel) > 0 {
        c.executeParallel(parallel)
    }
    // 串行执行 mutation 工具
    for _, t := range sequential {
        c.executeSequential(t)
    }
}
```

### Edit-after-Read 约束

```go
// tool context 追踪
type ToolContext struct {
    ReadFileTimestamps map[string]time.Time  // 文件路径 → read 时间
}

func (c *ToolContext) ValidateEdit(path string) error {
    if t, ok := c.ReadFileTimestamps[path]; ok {
        if time.Since(t) < 1*time.Hour {
            return nil  // 最近 read 过，允许 edit
        }
    }
    return errors.New("must Read file before Edit")
}
```

---

## 六、Idle Agent 折叠

### Claude Code 设计

> "Fixed idle subagents vanishing from the agent panel while other subagents were still working; surplus idle agents now collapse into an expandable summary row"

### mocode 改进：动态 panel 折叠

```go
// panel/view.go
type View struct {
    mu      sync.RWMutex
    live    []*Panel  // 活跃
    idle    []*Panel  // 空闲（可折叠）
    visible bool
}

func (v *View) Render(width int) string {
    var b strings.Builder
    for _, p := range v.live {
        b.WriteString(p.RenderRow(width))
        b.WriteString("\n")
    }
    
    // 折叠 idle
    if len(v.idle) > 0 {
        if v.idleExpanded {
            b.WriteString("─ idle agents ─\n")
            for _, p := range v.idle {
                b.WriteString(p.RenderRow(width))
                b.WriteString("\n")
            }
        } else {
            b.WriteString(fmt.Sprintf("... %d idle agents (press 'e' to expand)\n", len(v.idle)))
        }
    }
    return b.String()
}
```

---

## 七、teammate 颜色分配（@agentName 徽章）

### Claude Code 设计

```ts
const AGENT_COLORS = [...]  // 颜色池
function getAgentColor(name: string): string {
    const hash = simpleHash(name)
    return AGENT_COLORS[hash % AGENT_COLORS.length]
}

// Banner 渲染
banner.text = `@${agentName}`
banner.bgColor = getAgentColor(agentName)
```

### mocode 改进：AgentBadge 组件

```go
// styles/styles.go
type MessagesStyles struct {
    AgentBadgeBase lipgloss.Style
    AgentBadgeColor [8]color.Color  // 颜色池
}

func (s *Styles) AgentBadge(name string) string {
    h := fnv.New64a()
    h.Write([]byte(name))
    idx := h.Sum64() % 8
    
    bg := s.Messages.AgentBadgeColor[idx]
    return lipgloss.NewStyle().
        Background(bg).
        Foreground(lipgloss.Color("#000")).
        Bold(true).
        Padding(0, 1).
        Render(fmt.Sprintf("@%s", name))
}

// 使用
header := u.com.Styles.AgentBadge(agentName) + " " + description
```

### Subagent 头部自动应用

```go
func (a *AgentToolMessageItem) header(sty *styles.Styles, width int) string {
    var b strings.Builder
    b.WriteString(sty.Tool.ToolHeader.Render("Task"))
    b.WriteString(" ")
    b.WriteString(sty.AgentBadge(a.parentAgentName))  // ← 新增
    b.WriteString(" ")
    b.WriteString(sty.Tool.TaskTitle.Render(a.description))
    return b.String()
}
```

---

## 八、Footer actions（Chat about this）

### Claude Code 设计

AskUserQuestion 的 footer 有两个特殊选项：
- "Chat about this"（footerIndex=0）— 切换到自由文本输入
- "Skip interview and plan immediately"（footerIndex=1，仅 plan 模式）

### mocode 改进

```go
type AskUserQuestion struct {
    // ...
    footerActions []FooterAction
}

type FooterAction struct {
    Label string
    Key   string
    Action dialog.Action
}

// 在 Draw 中追加
func (d *AskUserQuestion) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    // ... 主内容
    
    // Footer actions
    for _, action := range d.footerActions {
        line += fmt.Sprintf("\n%s %s",
            sty.Help.ShortKey.Render(action.Key),
            action.Label,
        )
    }
    
    // HandleMsg 中处理
    case "ctrl+g":
        return d.footerActions[0].Action  // Chat about this
}
```

---

## 九、TodoWrite 任务管理

### Claude Code 模式

```ts
TodoWrite: { verb: 'Update Todos', arg: null, body: 'todo' }

// 渲染：状态列表
type TodoItem = {
    content: string,
    status: 'pending' | 'in_progress' | 'completed',
    activeForm?: string,  // 进行中的动词短语
}
```

### mocode `chat/todos.go` 增强

**当前**：已实现 TodoWrite 渲染

**可改进**：增加 `activeForm` 字段

```go
type TodoItem struct {
    Content    string `json:"content"`
    Status     string `json:"status"`
    ActiveForm string `json:"active_form"`  // "Fixing auth bug..."
}

// 渲染
func renderTodos(items []TodoItem, sty *styles.Styles) string {
    for _, item := range items {
        var glyph string
        var activeText string
        switch item.Status {
        case "completed":
            glyph = "✓"
        case "in_progress":
            glyph = "▶"
            if item.ActiveForm != "" {
                activeText = sty.Tool.TodoActiveForm.Render(" (" + item.ActiveForm + ")")
            }
        case "pending":
            glyph = "○"
        }
        // ...
    }
}
```

---

## 十、可视化效果 checklist

### 颜色约定（直接采用 Claude Code 的）

```go
var AgentStatusGlyphs = map[string]struct {
    Glyph  string
    Color  color.Color
}{
    "running":     {"●", cyan},
    "queued":      {"◌", gray},
    "idle":        {"○", white},
    "blocked":     {"!", yellow},
    "permission":  {"?", magenta},
    "completed":   {"✓", green},
    "failed":      {"✗", red},
    "missing":     {"–", dimGray},
}
```

### Fixed column width 防止抖动

```go
const (
    NAME_COL_WIDTH     = 26
    ACTIVITY_COL_WIDTH = 32
)

// 截断/补齐
func padRight(s string, w int) string {
    if ansi.StringWidth(s) >= w { return ansi.Truncate(s, w, "…") }
    return s + strings.Repeat(" ", w-ansi.StringWidth(s))
}

func padLeft(s string, w int) string {
    if ansi.StringWidth(s) >= w { return ansi.Truncate(s, w, "…") }
    return strings.Repeat(" ", w-ansi.StringWidth(s)) + s
}
```

### Keyboard navigation 完整集

| 按键 | 上下文 | 行为 |
|------|--------|------|
| `↑/↓` 或 `j/k` | AskUserQuestion | 选项切换 |
| `space` | AskUserQuestion (multi) | toggle 选中 |
| `tab` | AskUserQuestion (多问题) | 下一题 |
| `shift+tab` | AskUserQuestion (多问题) | 上一题 |
| `enter` | AskUserQuestion | 下一题/提交 |
| `1-9` | AskUserQuestion | 直接选中第 N 项 |
| `n` | AskUserQuestion | 进入 notes 模式 |
| `←` | 主对话 | 打开 agents 视图 |
| `esc` | Dialog | 取消/关闭 |
| `e` | Agents panel | 展开/折叠 idle |
| `o` | Agents panel | 在 tmux 打开 agent |
| `d` | Agents panel | 详情 |
| `l` | Agents panel | 日志 |
| `k` | Agents panel | 取消 |

---

## 十一、汇总：可落地的改进清单

| 改进项 | 工作量 | 优先级 |
|--------|--------|--------|
| Agent 三态（general/fork/async）渲染 | 2h | ⭐⭐⭐⭐ |
| Fork 模式徽章（`⟳ fork`） | 0.5h | ⭐⭐⭐ |
| AskUserQuestion 完整组件 | 4h | ⭐⭐⭐⭐⭐ |
| AgentDetailDialog（选中查看详情） | 2h | ⭐⭐⭐⭐ |
| Live + Committed 双层 panel 渲染 | 3h | ⭐⭐⭐⭐ |
| Idle agent 折叠 | 1h | ⭐⭐⭐ |
| AgentBadge 颜色分配 | 1h | ⭐⭐⭐ |
| isConcurrencySafe 工具并行 | 4h | ⭐⭐⭐ |
| Edit-after-Read 约束 | 2h | ⭐⭐ |
| Permission bubble mode（subagent） | 3h | ⭐⭐⭐ |
| TodoWrite activeForm | 1h | ⭐⭐ |
| Fixed-width columns 防抖动 | 0.5h | ⭐⭐⭐⭐ |

---

## 十二、复用代码模板

### 模板 1 · Agent Header with Badge

```go
func agentHeader(sty *styles.Styles, params AgentParams, parent string) string {
    var b strings.Builder
    
    // 1. Status icon
    b.WriteString(sty.Tool.StatusGlyph(params.Status))
    
    // 2. Verb
    b.WriteString(sty.Tool.TaskVerb.Render("Task"))
    
    // 3. Parent agent badge (if nested)
    if parent != "" {
        b.WriteString(" ")
        b.WriteString(sty.AgentBadge(parent))
    }
    
    // 4. Description (3-5 words)
    desc := truncateWords(params.Description, 5)
    b.WriteString(" ")
    b.WriteString(sty.Tool.TaskTitle.Render(desc))
    
    // 5. Type badge (general / fork / async)
    if params.Type != "" {
        b.WriteString(" ")
        b.WriteString(sty.Tool.TypeBadge.Render(params.Type))
    }
    
    return b.String()
}
```

### 模板 2 · Aggregate Row Renderer

```go
func renderAgentRows(agents []AgentSummary, sty *styles.Styles, width int) string {
    nameW := NAME_COL_WIDTH
    activityW := width - nameW - 10  // 留出 status + elapsed
    
    var lines []string
    for _, a := range agents {
        glyph := AgentStatusGlyphs[a.Status]
        line := strings.Join([]string{
            glyph.Glyph,
            padRight(a.Name, nameW),
            padRight(truncate(a.Activity, activityW), activityW),
            formatElapsed(a.Elapsed),
        }, " ")
        lines = append(lines, lipgloss.NewStyle().Foreground(glyph.Color).Render(line))
    }
    return strings.Join(lines, "\n")
}
```

### 模板 3 · AskUserQuestion 两栏布局

```go
func renderTwoPanel(
    left string, leftWidth int,
    right string, gap int,
    sty *styles.Styles,
) string {
    leftBox := lipgloss.NewStyle().Width(leftWidth).Render(left)
    rightBox := lipgloss.NewStyle().
        Width(/* remaining */).
        PaddingLeft(gap).
        Render(right)
    return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
}
```

### 模板 4 · Status Glyph Map

```go
// styles/icons.go
var StatusGlyphs = map[string]struct{ Glyph, Color string }{
    "running":    {"●", "#8be9fd"},
    "queued":     {"◌", "#6272a4"},
    "idle":       {"○", "#f8f8f2"},
    "blocked":    {"!", "#f1fa8c"},
    "permission": {"?", "#ff79c6"},
    "completed":  {"✓", "#50fa7b"},
    "failed":     {"✗", "#ff5555"},
    "missing":    {"–", "#44475a"},
}
```

---

## 十三、与 mocode 现有架构的契合度

| 借鉴点 | mocode 现有 | 改造难度 | 价值 |
|--------|-----------|----------|------|
| 嵌套 subagent 树渲染 | ✅ 已实现 | 低（增量） | 高 |
| Panel 树多任务 | ✅ 已实现 | 低 | 中 |
| AskUserQuestion | ❌ 未实现 | 中 | 极高 |
| Permission bubble | ❌ 未实现 | 中 | 高 |
| 工具并发 | ❌ 顺序 | 中 | 中 |
| Agent badge 颜色 | ⚠️ 部分 | 低 | 中 |
| Live/Committed 双层 | ❌ 单层 | 低 | 中 |
| Idle 折叠 | ❌ 全部显示 | 低 | 中 |

**总结**：mocode 已有 60% 的 subagent 渲染基础，**最大缺口是 AskUserQuestion**——建议优先实现。