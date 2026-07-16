# 08 · Team 模式架构深度解析

> 来源：anthropics/claude-code 官方文档 + 反编译 fork（x1xhlol/better-clawd, shareAI-lab, ruflo）+ 各对比工具（multi-agent-shogun, agent-kanban, gnap）
> 目标：把 Team 模式的整套架构（含任务调度、消息通信、UI 渲染）拆解清楚，并给出 mocode 的落地路径。

---

## 一、为什么需要 Team 模式（vs Subagent）

### 决策流程

```
需要并行？ ──No──→ Task tool（单 subagent）
       │ Yes
       ↓
子任务会触碰同一文件？ ──Yes──→ 串行 Task 或事后 merge
       │ No
       ↓
每个子任务 >30 分钟 / 需要独立上下文？ ──No──→ 并行 Subagent
       │ Yes
       ↓
需要双向中途协调？ ──No──→ 后台任务（TaskUpdate + 自动轮询）
       │ Yes
       ↓
  ★ TEAM 模式 ★
  + 文件分区 + 计划审批 + 验证角色 + 权限预批准
```

### Subagent vs Team 对比表

| 维度 | Subagent（Task tool） | Team Mode |
|------|----------------------|-----------|
| **生命周期** | 一次性，用完即销毁 | 持续空闲循环，可被唤醒 |
| **通信** | 返回结论字符串 | 异步 inbox，随时收发 |
| **上下文** | 完全隔离 | 共享任务列表，通过 inbox 通信 |
| **拓扑** | 1 leader + 偶发 subagent | 1 Lead + N Teammates |
| **Spawn 成本** | 1 个 model session | N 个并发 session + context budget |
| **文件锁** | 不需要 | `proper-lockfile`，重试 10 次 |
| **使用场景** | 串行 / 单 session 内并行 | 触碰不同文件的子系统，>30min/epic |

---

## 二、Claude Code Team 的完整架构

### 2.1 启用与配置

```json
// ~/.claude/settings.json
{
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  }
}
```

```typescript
// src/utils/config.ts
teammateMode?: 'auto' | 'tmux' | 'in-process'  // 默认 'auto'
teammateDefaultModel?: string | null             // null = leader's model
```

### 2.2 进程模型（关键架构）

**每个 teammate 是一个独立的 Claude Code 进程**：
- 独立的 conversation context
- 独立的工作目录
- 独立的 tool permissions
- **不是 in-process**（这点和 subagent 不同）

#### 三种 Spawn 后端

| 后端 | 设置 | 机制 | 优势 |
|------|------|------|------|
| **In-process** | `teammateMode: 'in-process'` | AsyncLocalStorage context 隔离 + daemon 线程 | 快速、无 UI、随 parent 死 |
| **tmux** | `'tmux'` 或 `'auto'` | tmux session + 每个 pane 一个完整终端 | 真实终端、断开后保留 |
| **iTerm2** | `'auto'` (macOS) | `it2` CLI + Python API | macOS 原生 pane |
| **后台** | `run_in_background: true` | detached 一次性 | 简单场景 |

### 2.3 磁盘状态布局（核心）

```
~/.claude/                              # 全局（GLOBAL-ONLY，跨 session 持久化）
├── teams/
│   └── {teamName}/
│       ├── team.json                   # team 元数据（成员、模型、leader）
│       └── inboxes/
│           ├── lead.json               # JSON 数组消息
│           ├── researcher.json         # 文件锁保护
│           ├── implementer.json        # 重试 10 次
│           └── ...
└── tasks/                              # 全局（跨 session、跨 agent）
    └── {task-list-id}/
        ├── tasks.json                  # 任务图（pending/in_progress/completed）
        └── ...
```

**设计原则**：
- **文件即消息总线**：所有通信通过 JSON 文件
- **文件锁保护**：防止并发写入冲突
- **跨 session 持久**：可恢复中断的 team

### 2.4 任务生命周期

```
pending → in_progress → completed
   ↑           ↓
   └── blockedBy deps (addBlockedBy / addBlocks)
```

任务字段：
```typescript
{
    subject: string,
    description: string,
    activeForm: string,           // "Fixing auth bug..."
    owner: string,                // 哪个 teammate 拥有
    blockedBy: string[],          // 依赖哪些任务
    blocks: string[],             // 阻塞哪些任务
    status: 'pending' | 'in_progress' | 'completed' | 'failed',
}
```

### 2.5 15 种消息类型（teammateMailbox.ts）

| 类型 | 方向 | 用途 |
|------|------|------|
| `plain text` | 双向 | 普通聊天 |
| `idle_notification` | Teammate→Lead | 这一轮干完，等任务 |
| `shutdown_request` | Lead→Teammate | 请求关闭 |
| `shutdown_response` | Teammate→Lead | 批准/拒绝 + reason |
| `plan_approval_request` | Teammate→Lead | 提交计划待批准 |
| `plan_approval_response` | Lead→Teammate | 批准/拒绝 + feedback |
| `permission_request` | Teammate→Lead | 权限请求（冒泡） |
| `wake` | Lead→Teammate | 唤醒空闲的 teammate |
| `retry_wake` | Lead→Teammate | 重试唤醒 |
| `task_assignment` | Lead→Teammate | 分配任务 |
| `task_completed` | Teammate→Lead | 任务完成 |
| `task_failed` | Teammate→Lead | 任务失败 |

### 2.6 Lead 如何知道任务完成

**两种机制**：

1. **Idle notification** — Teammate 完成这一轮，发送 `idle_notification`
   - Lead 的 `useInboxPoller` 每 1 秒轮询一次 inbox
   - 把消息注入 Lead 的 history

2. **Task 状态转换** — `TaskUpdate status=completed` 写入状态文件
   - 任何 agent 可通过 `TaskList` 读取
   - 最终一致性（eventual consistency）

### 2.7 验证步骤

**没有 first-class verifier tool**。验证通过以下方式之一：

1. **Verification subagent_type** — 分配一个只读 teammate 做检查
2. **Plan approval** — Teammate 必须先 plan → Lead 批准 → 才执行
3. **人工 /review** — 用户手动介入

**Anthropic 警告**：
> "task verifier must be nearly perfect, otherwise Claude will solve the wrong problem."

---

## 三、UI 渲染模式（CC 3 种）

### 3.1 Swarm Banner（输入区上方）

```ts
// useSwarmBanner.ts
if (insideTmux === false && !inProcessMode && !nativePanes) {
    // 模式 A：外部 tmux
    return { text: `tmux -L ${socket} attach`, bgColor: ... }
}

if ((insideTmux || inProcessMode || nativePanes) && viewedTeammate) {
    // 模式 B/C：内嵌 + 团队视图
    return { text: `@${agentName}`, bgColor: getAgentColor(agentName) }
}
```

### 3.2 Agents View（← 键打开）

- **Live 阶段**：活跃 agents 渲染到 compact live panel
- **Committed 阶段**：完成结果 commit 到 `<Static>` 历史（不重绘）
- **路由条件**：`agent call count >= 2 && all calls are agents && !pending confirmation`

### 3.3 Idle 折叠

> "Surplus idle agents now collapse into an expandable summary row"

- 超过 6 个空闲 → 折叠成 "N idle agents" 摘要行
- 按 `e` 展开

### 3.4 颜色分配

```ts
const AGENT_COLORS = [...]  // 颜色池

function getAgentColor(name: string): string {
    const hash = simpleHash(name)
    return AGENT_COLORS[hash % AGENT_COLORS.length]
}
```

- 按 spawn 顺序循环分配颜色
- 同一 teammate 在整个 session 颜色稳定

---

## 四、Task Tool vs Team Mode 决策详解

### 4.1 何时用 Task tool（subagent）

```
✅ 单次查询：让子 agent 跑个 bash 命令返回结果
✅ 并行只读：批量跑多个 grep / glob / read
✅ 隔离上下文：复杂任务拆解成独立 context
✅ 一问一答：子 agent 不需要再交流
```

### 4.2 何时用 Team Mode

```
✅ 多子系统并行：前端 + 后端 + 测试 + 文档同时推进
✅ 长时任务：每个 teammate 工作 >30 分钟
✅ 需要中途协调：Lead 看到部分结果后调整其他 teammate
✅ 文件隔离清晰：每个 teammate 负责不同目录
✅ 计划审批：teammate 提交 plan → Lead 批准 → 执行
```

### 4.3 何时不用 Team

```
❌ 简单顺序任务（用 subagent 即可）
❌ 文件冲突无法避免（用单 agent 串行）
❌ 需要精确 token 预算（Team 浪费 3-15x tokens）
❌ 单次会话（Team 配置成本太高）
```

---

## 五、其他工具的 Team 实现对比

### 5.1 multi-agent-shogun（最实战）

**架构**：tmux + YAML queues + `inbox_write.sh`

```
殿 (Lord) ← 人类
  ↓ command
将軍 (Shogun) — 写 YAML，立即返回
  ↓ inbox_write.sh
家老 (Karo) — 拆分子任务
  ↓ inbox_write.sh + parallel
足軽 (Ashigaru) × N — 在 tmux pane 执行
  ↑ YAML 报告 + inbox_write
家老 汇总 → dashboard.md
将軍 读 dashboard → 下一 Wave
```

**目录结构**：
```
~/.multi-agent-shogun/
├── queue/
│   ├── shogun_to_karo.yaml
│   ├── tasks/
│   │   ├── ashigaru1.yaml      # assigned|blocked|done|failed
│   │   └── pending.yaml
│   └── ntfy_inbox.yaml
├── dashboard.md                # 给 Lord 看的只读状态
└── scripts/
    └── inbox_write.sh          # 投递原语
```

**关键设计**：
- **Inbox 到 Shogun 被禁止** — 防止打断 Lord 输入
- **Karo 只更新 dashboard.md**
- **Zero coordination API cost** — `inbox_write.sh` 就是 `cat > file`

### 5.2 agent-kanban（多 runtime）

- Leader-worker 看板
- 支持 Claude Code / Codex / Gemini CLI 等多种 runtime
- 工人通过 `claim` 命令认领卡片
- 加密身份（每个 agent 一对 key）

### 5.3 gnap（Git-Native）

**无 orchestrator 进程**，git repo 本身就是看板：
```
todo/      # 待办任务文件
doing/     # agent 移到这里 + 写状态
done/      # 完成
failed/
```

机制：
- 新任务 = `git mv todo/foo.md doing/foo.md`
- 冲突检测 = git 的合并冲突语义
- 通信 = 共享分支的 commits

**优势**：零基础设施、版本化、可审计
**劣势**：粒度粗、迭代慢

### 5.4 对比表

| 工具 | 任务状态 | "完成" 信号 | 任务分配 | 通信方式 | 适用 |
|------|---------|------------|---------|---------|------|
| **CC Teams** | JSON + 文件锁 | `idle_notification` + `TaskUpdate completed` | Lead 分或自取 | 15 种类型 inbox 消息 | 子系统分解 |
| **shogun** | YAML 队列 + dashboard.md | status: done | Karo 分 | inbox_write.sh | 10+ 并行，混合 CLI |
| **agent-kanban** | board.json + 锁 | done status | Worker claim | 卡片 + 评论 | 混合 runtime |
| **gnap** | git 目录 + commit | `git mv` 到 done/ | 拉模式（先到先得） | 共享分支 commit | 零基础设施 |
| **Aider Architect** | in-process 上下文 | 管道返回 | 硬编码顺序 | prompt handoff | SWE-bench 单任务 |

---

## 六、mocode Team 模式落地设计

### 6.1 架构总览

基于 mocode 现有架构（mocode Go + Bubble Tea v2），设计如下：

```
┌──────────────────────────────────────────────────┐
│                  Leader Process                    │
│  ┌─────────────────────────────────────────────┐ │
│  │  internal/core/team/coordinator.go          │ │
│  │  - 任务调度器                                │ │
│  │  - 消息路由                                  │ │
│  │  - 心跳监控                                  │ │
│  └─────────────────────────────────────────────┘ │
│                  ↓                                │
│  ┌─────────────────────────────────────────────┐ │
│  │  internal/core/team/mailbox.go              │ │
│  │  - 文件 inbox（每个 teammate 一个）          │ │
│  │  - 15 种消息类型                             │ │
│  │  - 文件锁（flock）                          │ │
│  └─────────────────────────────────────────────┘ │
│                  ↓                                │
│  ┌─────────────────────────────────────────────┐ │
│  │  internal/core/team/taskboard.go            │ │
│  │  - JSON 任务图                               │ │
│  │  - 状态转换                                  │ │
│  │  - 依赖关系                                  │ │
│  └─────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────┘
         │
         │ tmux / subprocess
         ↓
┌──────────────────────────────────────────────────┐
│  Teammate 1 Process  │  Teammate 2 Process  │ ... │
│  (独立 Go 子进程)      │  (独立 Go 子进程)     │     │
└──────────────────────────────────────────────────┘
```

### 6.2 状态目录

```
~/.local/share/mocode/
└── teams/
    └── {teamName}/
        ├── team.json                 # 团队元数据
        ├── inboxes/
        │   ├── leader.json
        │   ├── teammate-1.json
        │   └── teammate-2.json
        └── tasks/
            └── {taskListID}/
                └── tasks.json
```

### 6.3 核心数据结构

```go
// internal/core/team/types.go
package team

type Team struct {
    Name      string            `json:"name"`
    Lead      string            `json:"lead"`
    Teammates []*Teammate       `json:"teammates"`
    Models    map[string]string `json:"models"`  // name → model
    CreatedAt time.Time         `json:"created_at"`
}

type Teammate struct {
    Name      string    `json:"name"`
    Model     string    `json:"model"`
    SpawnMode string    `json:"spawn_mode"`  // "in-process" | "tmux" | "subprocess"
    Color     string    `json:"color"`       // hex
    State     string    `json:"state"`       // "running" | "idle" | "missing"
    SessionID string    `json:"session_id"`
    CreatedAt time.Time `json:"created_at"`
}

type Task struct {
    ID          string    `json:"id"`
    Subject     string    `json:"subject"`
    Description string    `json:"description"`
    ActiveForm  string    `json:"active_form"`
    Owner       string    `json:"owner"`        // teammate name
    Status      string    `json:"status"`       // "pending" | "in_progress" | "completed" | "failed"
    BlockedBy   []string  `json:"blocked_by"`   // task IDs
    Blocks      []string  `json:"blocks"`       // task IDs
    Result      string    `json:"result,omitempty"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

### 6.4 消息类型

```go
// internal/core/team/messages.go
package team

type MessageType string

const (
    MsgPlainText          MessageType = "plain_text"
    MsgIdleNotification   MessageType = "idle_notification"
    MsgShutdownRequest    MessageType = "shutdown_request"
    MsgShutdownResponse   MessageType = "shutdown_response"
    MsgPlanApprovalReq    MessageType = "plan_approval_request"
    MsgPlanApprovalResp   MessageType = "plan_approval_response"
    MsgPermissionRequest  MessageType = "permission_request"
    MsgWake               MessageType = "wake"
    MsgRetryWake          MessageType = "retry_wake"
    MsgTaskAssignment     MessageType = "task_assignment"
    MsgTaskCompleted      MessageType = "task_completed"
    MsgTaskFailed         MessageType = "task_failed"
)

type Message struct {
    ID        string      `json:"id"`
    Type      MessageType `json:"type"`
    From      string      `json:"from"`
    To        string      `json:"to"`        // teammate name or "*" for broadcast
    Content   string      `json:"content,omitempty"`
    Metadata  any         `json:"metadata,omitempty"`  // 各种消息的附加字段
    Timestamp time.Time   `json:"timestamp"`
}
```

### 6.5 Inbox 实现（mailbox.go）

```go
package team

import (
    "encoding/json"
    "os"
    "path/filepath"
    "github.com/gofrs/flock"
)

type Mailbox struct {
    dir   string
    locks map[string]*flock.Flock
}

func NewMailbox(teamDir string) *Mailbox {
    inboxDir := filepath.Join(teamDir, "inboxes")
    os.MkdirAll(inboxDir, 0755)
    return &Mailbox{
        dir:   inboxDir,
        locks: make(map[string]*flock.Flock),
    }
}

// 发送消息（带文件锁）
func (m *Mailbox) Send(to string, msg Message) error {
    lock := m.lockFor(to)
    if err := lock.Lock(); err != nil {
        return err
    }
    defer lock.Unlock()
    
    path := m.path(to)
    
    // 读现有消息
    var msgs []Message
    if data, err := os.ReadFile(path); err == nil {
        json.Unmarshal(data, &msgs)
    }
    
    msgs = append(msgs, msg)
    
    data, _ := json.MarshalIndent(msgs, "", "  ")
    return os.WriteFile(path, data, 0644)
}

// 读取并清空 inbox（teammate 用）
func (m *Mailbox) Read(to string) ([]Message, error) {
    lock := m.lockFor(to)
    if err := lock.RLock(); err != nil {
        return nil, err
    }
    defer lock.Unlock()
    
    path := m.path(to)
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) { return nil, nil }
        return nil, err
    }
    
    var msgs []Message
    if err := json.Unmarshal(data, &msgs); err != nil {
        return nil, err
    }
    
    // 清空
    os.WriteFile(path, []byte("[]"), 0644)
    return msgs, nil
}

// 轮询模式（lead 用，每秒）
func (m *Mailbox) Poll(to string) ([]Message, error) {
    // 检查是否有新消息（通过 mtime）
    // 如果有，调用 Read
    info, err := os.Stat(m.path(to))
    if err != nil { return nil, nil }
    if time.Since(info.ModTime()) > 0 {
        return m.Read(to)
    }
    return nil, nil
}

func (m *Mailbox) lockFor(name string) *flock.Flock {
    if lock, ok := m.locks[name]; ok { return lock }
    lock := flock.New(filepath.Join(m.dir, name+".lock"))
    m.locks[name] = lock
    return lock
}

func (m *Mailbox) path(name string) string {
    return filepath.Join(m.dir, name+".json")
}
```

### 6.6 TaskBoard 实现（taskboard.go）

```go
package team

import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
    "github.com/gofrs/flock"
)

type TaskBoard struct {
    path string
    mu   sync.Mutex
    lock *flock.Flock
}

func NewTaskBoard(teamDir, taskListID string) *TaskBoard {
    dir := filepath.Join(teamDir, "tasks", taskListID)
    os.MkdirAll(dir, 0755)
    return &TaskBoard{
        path: filepath.Join(dir, "tasks.json"),
        lock: flock.New(filepath.Join(dir, "tasks.lock")),
    }
}

func (tb *TaskBoard) List() ([]*Task, error) {
    tb.mu.Lock()
    defer tb.mu.Unlock()
    
    if err := tb.lock.RLock(); err != nil { return nil, err }
    defer tb.lock.Unlock()
    
    data, err := os.ReadFile(tb.path)
    if err != nil {
        if os.IsNotExist(err) { return []*Task{}, nil }
        return nil, err
    }
    var tasks []*Task
    json.Unmarshal(data, &tasks)
    return tasks, nil
}

func (tb *TaskBoard) Claim(taskID, teammate string) error {
    return tb.update(taskID, func(t *Task) {
        t.Owner = teammate
        t.Status = "in_progress"
        t.UpdatedAt = time.Now()
    })
}

func (tb *TaskBoard) Complete(taskID, result string) error {
    return tb.update(taskID, func(t *Task) {
        t.Status = "completed"
        t.Result = result
        t.UpdatedAt = time.Now()
    })
}

func (tb *TaskBoard) Fail(taskID, reason string) error {
    return tb.update(taskID, func(t *Task) {
        t.Status = "failed"
        t.Result = reason
        t.UpdatedAt = time.Now()
    })
}

func (tb *TaskBoard) update(taskID string, fn func(*Task)) error {
    tb.mu.Lock()
    defer tb.mu.Unlock()
    
    if err := tb.lock.Lock(); err != nil { return err }
    defer tb.lock.Unlock()
    
    tasks, _ := tb.List()
    for _, t := range tasks {
        if t.ID == taskID {
            fn(t)
            break
        }
    }
    data, _ := json.MarshalIndent(tasks, "", "  ")
    return os.WriteFile(tb.path, data, 0644)
}
```

### 6.7 Team Coordinator

```go
// internal/core/team/coordinator.go
package team

type Coordinator struct {
    team      *Team
    mailbox   *Mailbox
    taskBoard *TaskBoard
    teammates map[string]*TeammateProcess  // 进程句柄
    pollTicker *time.Ticker
    mu        sync.RWMutex
}

func NewCoordinator(teamDir, name string) (*Coordinator, error) {
    team := &Team{Name: name, CreatedAt: time.Now()}
    if err := saveTeam(teamDir, team); err != nil {
        return nil, err
    }
    
    c := &Coordinator{
        team:      team,
        mailbox:   NewMailbox(teamDir),
        taskBoard: NewTaskBoard(teamDir, "default"),
        teammates: make(map[string]*TeammateProcess),
    }
    
    // 启动 inbox 轮询（1Hz）
    c.pollTicker = time.NewTicker(1 * time.Second)
    go c.pollInboxLoop()
    
    return c, nil
}

// 分配任务
func (c *Coordinator) AssignTask(taskID, toTeammate string) error {
    if err := c.taskBoard.Claim(taskID, toTeammate); err != nil {
        return err
    }
    return c.mailbox.Send(toTeammate, Message{
        Type:    MsgTaskAssignment,
        From:    "lead",
        To:      toTeammate,
        Content: taskID,
    })
}

// 轮询 inbox
func (c *Coordinator) pollInboxLoop() {
    for range c.pollTicker.C {
        msgs, _ := c.mailbox.Read("lead")
        for _, msg := range msgs {
            c.handleMessage(msg)
        }
    }
}

func (c *Coordinator) handleMessage(msg Message) {
    switch msg.Type {
    case MsgIdleNotification:
        // teammate 空闲，记录状态
        c.markIdle(msg.From)
    case MsgTaskCompleted:
        // task 完成，确认
        c.taskBoard.Complete(msg.Metadata.(string), "")
    case MsgTaskFailed:
        c.taskBoard.Fail(msg.Metadata.(string), msg.Content)
    case MsgPermissionRequest:
        // 冒泡到 lead UI
        c.bubblePermission(msg)
    }
}
```

### 6.8 UI：Team Dashboard

```go
// internal/ui/panel/team_dashboard.go
type TeamDashboard struct {
    com   *common.Common
    team  *team.Team
    tasks []*team.Task
    width int
}

func (d *TeamDashboard) Render(width int) string {
    sty := d.com.Styles
    
    // 1. Header: team name + member count
    header := fmt.Sprintf("Team: %s · %d teammates · %d tasks",
        d.team.Name, len(d.team.Teammates), len(d.tasks))
    
    // 2. Aggregate rows（每个 teammate 一行）
    var rows []string
    rows = append(rows, header)
    
    for _, t := range d.team.Teammates {
        rows = append(rows, renderTeammateRow(t, sty, width))
    }
    
    // 3. Task summary
    var pending, inProgress, completed int
    for _, task := range d.tasks {
        switch task.Status {
        case "pending":     pending++
        case "in_progress": inProgress++
        case "completed":   completed++
        }
    }
    rows = append(rows, "")
    rows = append(rows, fmt.Sprintf("Tasks: %d pending · %d in_progress · %d completed",
        pending, inProgress, completed))
    
    return strings.Join(rows, "\n")
}

func renderTeammateRow(t *team.Teammate, sty *styles.Styles, width int) string {
    const nameW = 26
    glyph, color := statusGlyph(t.State, sty)
    line := fmt.Sprintf("%s %s %s",
        lipgloss.NewStyle().Foreground(color).Render(glyph),
        padRight(t.Name, nameW),
        t.State,
    )
    return line
}
```

---

## 七、关键设计决策（与 Claude Code 对齐）

### 7.1 文件作为消息总线

**为什么不用 Redis / DB / Message Queue？**

| 优势 | 原因 |
|------|------|
| 零基础设施 | 用户不需要装 Redis |
| 可观察 | `cat` 文件就能看到所有消息 |
| 可调试 | 任何文本编辑器都能看 |
| 持久化 | 默认就跨 session 持久 |
| 多 host | 文件 NFS 共享即可 |

**代价**：需要文件锁。

### 7.2 文件锁 vs Channel

**CC 用 `proper-lockfile`（Node.js）**，mocode Go 用 `gofrs/flock`：
- POSIX `flock()` 系统调用
- 自动重试（最多 10 次，每次 100ms）
- 进程退出自动释放

### 7.3 1Hz 轮询 vs Watch

CC Lead 用 1Hz 轮询 inbox：
- 简单可预测
- 不依赖文件系统事件
- 适合大多数场景

**优化**：teammate 端可以用 fsnotify watch，但 leader 端 polling 已足够。

### 7.4 任务分配：分 vs 自取

两种模式都支持：

```go
// 模式 A：Lead 主动分
c.AssignTask(taskID, "researcher")

// 模式 B：teammate 自取（idle_notification 触发）
func (c *Coordinator) onTeammateIdle(name string) {
    task := c.findUnblockedPendingTask()
    if task != nil {
        c.AssignTask(task.ID, name)
    }
}
```

### 7.5 文件分区策略

避免冲突的硬性规则：

```yaml
# team config
file_partition:
  - teammate: "frontend"
    paths: ["src/components/", "src/styles/"]
  - teammate: "backend"
    paths: ["src/api/", "src/services/"]
  - teammate: "tester"
    paths: ["tests/", "**/*.test.ts"]
```

### 7.6 Plan Approval 流程

```go
// teammate 必须先 plan，lead 批准后才执行
type PlanApprovalRequest struct {
    TaskID    string   `json:"task_id"`
    Plan      string   `json:"plan"`        // markdown
    Files     []string `json:"files"`       // 将要修改的文件
    RiskLevel string   `json:"risk_level"`  // "low" | "medium" | "high"
}

func (c *Coordinator) requestPlanApproval(taskID, plan string, files []string) {
    c.mailbox.Send("lead", Message{
        Type: MsgPlanApprovalReq,
        Metadata: PlanApprovalRequest{
            TaskID:    taskID,
            Plan:      plan,
            Files:     files,
            RiskLevel: "medium",
        },
    })
}

// lead UI 收到后弹 plan 审批 dialog
// 批准后发回 MsgPlanApprovalResp
```

---

## 八、Teammate 子进程的实现

### 8.1 进程模型

```go
type TeammateProcess struct {
    name   string
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout io.ReadCloser
    stderr io.ReadCloser
    state  string
}

func (c *Coordinator) SpawnTeammate(name, model string) error {
    cmd := exec.Command("mocode",
        "--teammate",                  // 进入 teammate 模式
        "--team", c.team.Name,
        "--name", name,
        "--model", model,
        "--color", assignColor(name),
    )
    
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    stderr, _ := cmd.StderrPipe()
    
    // 设置环境变量
    cmd.Env = append(os.Environ(),
        "MOCODE_TEAM_NAME=" + c.team.Name,
        "MOCODE_TEAMMATE_NAME=" + name,
    )
    
    if err := cmd.Start(); err != nil {
        return err
    }
    
    c.teammates[name] = &TeammateProcess{
        name:   name,
        cmd:    cmd,
        stdin:  stdin,
        stdout: stdout,
        stderr: stderr,
        state:  "starting",
    }
    
    // 启动 goroutine 监控
    go c.monitorTeammate(name, stdout, stderr)
    
    return nil
}

func (c *Coordinator) monitorTeammate(name string, stdout, stderr io.Reader) {
    // 解析 teammate 输出，识别 idle / done / error 事件
    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "[MOCODE_EVENT]") {
            // 解析事件
            event := parseEvent(line)
            switch event.Type {
            case "idle":
                c.mailbox.Send("lead", Message{
                    Type: MsgIdleNotification,
                    From: name,
                })
                c.teammates[name].state = "idle"
            case "task_done":
                c.taskBoard.Complete(event.TaskID, event.Result)
            }
        }
    }
}
```

### 8.2 Teammate 端启动

```go
// cmd/teammate/main.go
func main() {
    teamName := os.Getenv("MOCODE_TEAM_NAME")
    myName := os.Getenv("MOCODE_TEAMMATE_NAME")
    
    mailbox := team.NewMailbox(teamDir(teamName))
    
    // 启动 inbox 监听
    go func() {
        for {
            time.Sleep(1 * time.Second)
            msgs, _ := mailbox.Read(myName)
            for _, msg := range msgs {
                handleIncomingMessage(msg)
            }
        }
    }()
    
    // 启动 agent 主循环
    // ...
    
    // 完成后发 idle_notification
    mailbox.Send("lead", Message{
        Type: MsgIdleNotification,
        From: myName,
    })
    
    // 等待下一轮
    select {}
}
```

---

## 九、UI 渲染：Team Mode

### 9.1 Agents View Dialog（按下 `←`）

```go
type AgentsViewDialog struct {
    com       *common.Common
    team      *team.Team
    tasks     []*team.Task
    selected  int
    list      *list.FilterableList
    help      help.Model
}

func (d *AgentsViewDialog) ID() string { return "agents_view" }

func (d *AgentsViewDialog) Draw(scr uv.Screen, area image.Rectangle) *tea.Cursor {
    sty := d.com.Styles
    width := min(120, area.Dx() - 4)
    
    rc := dialog.NewRenderContext(sty, width)
    rc.Title = fmt.Sprintf("Team: %s", d.team.Name)
    
    // Aggregate teammates
    rc.AddPart(renderTeammateAggregate(d.team, sty, width))
    
    // Spacer
    rc.AddPart("")
    
    // Task summary
    rc.AddPart(renderTaskSummary(d.tasks, sty))
    
    // Selected detail
    if d.selected < len(d.team.Teammates) {
        rc.AddPart("")
        rc.AddPart(renderTeammateDetail(d.team.Teammates[d.selected], sty, width))
    }
    
    rc.AddPart("")
    rc.AddPart(d.help.View())
    
    return dialog.DrawCenter(rc, scr, area)
}
```

### 9.2 Teammate Row 渲染

```go
func renderTeammateAggregate(team *team.Team, sty *styles.Styles, width int) string {
    const (
        nameW   = 26
        stateW  = 12
        activityW = 40
    )
    
    var lines []string
    for _, t := range team.Teammates {
        glyph, color := statusGlyph(t.State, sty)
        
        line := fmt.Sprintf("%s %s %s %s",
            lipgloss.NewStyle().Foreground(color).Render(glyph),
            padRight(t.Name, nameW),
            padRight(t.State, stateW),
            padRight(activityW, activityW),
        )
        lines = append(lines, line)
    }
    
    return strings.Join(lines, "\n")
}

func statusGlyph(state string, sty *styles.Styles) (string, color.Color) {
    switch state {
    case "running":     return "●", color("#8be9fd")
    case "idle":        return "○", color("#6272a4")
    case "missing":     return "–", color("#44475a")
    case "blocked":     return "!", color("#f1fa8c")
    case "permission":  return "?", color("#ff79c6")
    }
    return "?", color("#ff5555")
}
```

### 9.3 Live + Committed 双层

```go
type AgentPanelView struct {
    mu        sync.RWMutex
    live      map[string]*TeammateRow
    committed []string  // 已完成摘要
    visible   bool
}

func (v *AgentPanelView) Render(width int) string {
    if !v.visible { return "" }
    
    var b strings.Builder
    
    // Live 阶段
    for _, row := range v.live {
        b.WriteString(row.Render(width))
        b.WriteString("\n")
    }
    
    // 折叠 idle
    if len(v.live) > 6 {
        b.WriteString(fmt.Sprintf("... and %d more agents (press 'e' to expand)\n", len(v.live)-5))
    }
    
    // Committed 阶段（只渲染一次，不在每帧重绘）
    if len(v.committed) > 0 {
        b.WriteString("\n─── History ───\n")
        for _, summary := range v.committed {
            b.WriteString(summary)
            b.WriteString("\n")
        }
    }
    
    return b.String()
}
```

### 9.4 Idle 折叠

```go
func (v *AgentPanelView) renderWithFold(width int) string {
    liveList := sortedLive(v.live)
    var shown []*TeammateRow
    var hidden int
    
    for i, row := range liveList {
        if i < 5 {
            shown = append(shown, row)
        } else {
            hidden++
        }
    }
    
    var b strings.Builder
    for _, row := range shown {
        b.WriteString(row.Render(width))
        b.WriteString("\n")
    }
    if hidden > 0 {
        if v.expanded {
            // 展开全部
            for _, row := range liveList[5:] {
                b.WriteString(row.Render(width))
                b.WriteString("\n")
            }
        } else {
            b.WriteString(fmt.Sprintf("... and %d more (press 'e' to expand)\n", hidden))
        }
    }
    return b.String()
}
```

---

## 十、可落地的改进清单

| 改进项 | 工作量 | 优先级 |
|--------|--------|--------|
| Team 数据结构 + 文件锁 mailbox | 4h | ⭐⭐⭐⭐⭐ |
| TaskBoard 实现 | 3h | ⭐⭐⭐⭐⭐ |
| Teammate 子进程 spawn | 6h | ⭐⭐⭐⭐ |
| AgentsViewDialog（← 键） | 4h | ⭐⭐⭐⭐⭐ |
| Live + Committed 双层 panel | 3h | ⭐⭐⭐⭐ |
| Idle agent 折叠 | 1h | ⭐⭐⭐ |
| Aggregate row renderer | 2h | ⭐⭐⭐⭐ |
| Plan approval flow | 4h | ⭐⭐⭐ |
| Permission bubble | 3h | ⭐⭐⭐ |
| File partitioning config | 2h | ⭐⭐ |
| Team color assignment | 1h | ⭐⭐⭐⭐ |
| Dashboard 持久化 + 恢复 | 3h | ⭐⭐⭐ |

---

## 十一、复用代码模板

### 模板 1 · Mailbox 实现

```go
// 复用：internal/core/team/mailbox.go
// 核心：文件锁 + JSON 数组追加 + 1Hz 轮询
```

### 模板 2 · TaskBoard

```go
// 复用：internal/core/team/taskboard.go
// 核心：JSON 任务图 + claim/complete/fail 状态转换
```

### 模板 3 · Aggregate Row

```go
// 复用：renderTeammateRow (固定列宽)
// 模式：NAME_COL_WIDTH = 26 防止抖动
```

### 模板 4 · Color Assignment

```go
// 复用：FNV hash → 颜色池
// func agentColor(name string) color.Color { ... }
```

### 模板 5 · Status Glyph

```go
// 复用：statusGlyph(state string) (string, color.Color)
// 模式：running=●, idle=○, blocked=!, permission=?, completed=✓, failed=✗
```

---

## 十二、与 mocode 现有架构的契合

| 借鉴点 | mocode 现状 | 改造难度 | 价值 |
|--------|-----------|----------|------|
| Mailbox 文件通信 | ❌ 无 | 中（新增包） | 极高 |
| TaskBoard | ⚠️ TodoWrite 部分有 | 低（扩展） | 极高 |
| Teammate 进程 | ❌ 无 | 高（需要 subprocess） | 极高 |
| AgentsViewDialog | ❌ 无 | 中（新建 dialog） | 高 |
| Live/Committed | ❌ Panel 单层 | 低（Panel 改） | 中 |
| Color 分配 | ❌ 无 | 低（新增函数） | 中 |
| Aggregate Row | ⚠️ Panel 树有 | 低（仿写） | 高 |
| 文件分区 | ❌ 无 | 低（配置） | 中 |

**mocode 落地路径**：

1. **第一步**（必须先做）：实现 `mailbox.go` + `taskboard.go` 两个基础包
2. **第二步**：实现 `coordinator.go`，整合 Lead 端逻辑
3. **第三步**：实现 `teammate` 子进程入口
4. **第四步**：实现 `AgentsViewDialog`
5. **第五步**：实现 Plan approval + permission bubble
6. **第六步**：性能优化（idle 折叠、Live/Committed 双层）

总工作量预估：**~30 小时**（2 个工程师 2 天），但能获得 Claude Code Team 同级别的能力。