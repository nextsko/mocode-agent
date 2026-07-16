# 10 · TTY 输入处理 + 后台任务观测（Hermes-Agent × Claude Code 深度调研）

> 两块内容：
> 1. **TTY 用户输入的展示和处理** —— 确认提示、密码输入、IM 选择、AskUserQuestion、多行编辑、Vim 模式、Bracketed Paste、SIGINT/EOF
> 2. **后台任务的观测和追踪** —— ProcessRegistry、状态机、Push/Pull 双模型、通知层级、cleanup
>
> 目的：提炼可落地到 mocode 的设计模式。

---

## 一、Hermes-Agent 的 TTY 输入处理

### 1.1 总体架构（双 runtime）

```
┌─────────────────────────────────────────────┐
│  Classic CLI: prompt_toolkit（Python）       │  旧版 CLI
│  - getpass / prompt / confirmation         │
│  - 内置 multi-line / history / completion   │
└─────────────────────────────────────────────┘

┌─────────────────────────────────────────────┐
│  TUI: hermes-ink（自研 Ink/React-like）      │  新版 TUI
│  - 全组件化                                  │
│  - 复用 voice.py 键盘映射合约                 │
└─────────────────────────────────────────────┘
```

**关键合约**（`hermes_cli/voice.py:30-82`）：
```python
# 两个 runtime 共享同一套 keybinding
_VOICE_MOD_ALIASES = {
    "ctrl": "c-",
    "control": "c-",
    "alt": "a-",
    "option": "a-",
    "opt": "a-",
}
# 保留字符：ctrl c/d/l, alt c/d/l (macOS)
```

### 1.2 TTY 检测（双层防护）

```python
# hermes_cli/console_engine.py:1828-1850
def run_console_repl(*, stdin=None, stdout=None, stderr=None, interactive=None):
    stdin = stdin or sys.stdin
    interactive = bool(getattr(stdin, "isatty", lambda: False)())
    if interactive:
        # TTY 模式
    else:
        # pipe / file 模式

# agent/display.py:1075-1081
def _is_tty(self) -> bool:
    try:
        return hasattr(self._out, 'isatty') and self._out.isatty()
    except (ValueError, OSError):
        return False

# prompt_toolkit 包装检测（避免双渲染）
def _is_patch_stdout_proxy(self) -> bool:
    return isinstance(self._out, prompt_toolkit.patch_stdout.StdoutProxy)
```

### 1.3 三层 SIGINT 处理（Windows 兼容性关键）

#### Layer 1: defer-and-redeliver（避免 Windows TerminateProcess）

```python
# tools/environments/file_sync.py:282-316
def _sync_back_once(self, lock_path: Path) -> None:
    on_main_thread = threading.current_thread() is threading.main_thread()
    deferred_sigint = []
    original_handler = None
    if on_main_thread:
        original_handler = signal.getsignal(signal.SIGINT)
        def _defer_sigint(signum, frame):
            deferred_sigint.append((signum, frame))
        signal.signal(signal.SIGINT, _defer_sigint)
    try:
        self._sync_back_locked(lock_path)
    finally:
        if on_main_thread and original_handler is not None:
            signal.signal(signal.SIGINT, original_handler)
            if deferred_sigint:
                # 用 signal.raise_signal，不是 os.kill（Windows 走 TerminateProcess 退码 2）
                signal.raise_signal(signal.SIGINT)
```

**关键洞察**：Windows 的 `os.kill(pid, SIGINT)` 会触发 `TerminateProcess`（exit code 2），而不是 raise `KeyboardInterrupt`。必须用 `signal.raise_signal()`。

#### Layer 2: MCP watchdog 转发

```python
# tools/mcp_stdio_watchdog.py:156-167
def _forward_shutdown(signum, frame):
    _terminate_process_group(proc)
    sys.exit(128 + signum)
signal.signal(signal.SIGTERM, _forward_shutdown)
signal.signal(signal.SIGINT, _forward_shutdown)
```

#### Layer 3: 异步 loop signal handler

```python
# hermes_cli/proxy/server.py:274-285
loop = asyncio.get_running_loop()
for sig in (signal.SIGINT, signal.SIGTERM):
    try:
        loop.add_signal_handler(sig, stop_event.set)
    except NotImplementedError:
        # Windows / 受限环境，Ctrl+C 仍会 raise KeyboardInterrupt
        pass
```

### 1.4 危险命令审批的 Fail-Closed Guard

**这是 hermes 最重要的设计之一**（防回归测试 #15216）：

```python
# tests/tools/test_approval.py:1797-1835
class TestFailClosedUnderPromptToolkit:
    """当 prompt_toolkit 拥有 terminal 但当前 thread 没注册 approval callback 时，
    prompt_dangerous_approval() 必须 deny-fast，而不是 fallthrough 到 input()——
    否则会 deadlock，因为用户按键会被 PT 的 raw-mode stdin 截获，不会传给 input()。"""
```

**审批选项**（`gateway/platforms/api_server.py:71-74`）：
```python
def _approval_event_choices(*, smart_denied: bool, allow_permanent: bool) -> list[str]:
    if smart_denied:
        return ["once", "deny"]
    return ["once", "session", "always", "deny"] if allow_permanent else ["once", "session", "deny"]
```

### 1.5 密码输入（双 fallback）

```python
# hermes_cli/setup_whatsapp_cloud.py:165-187
def _prompt(message, default=None, secret=False):
    if secret and sys.stdin.isatty():
        import getpass
        raw = getpass.getpass(f"{message}{suffix} (input hidden): ").strip()
    else:
        raw = input(f"{message}{suffix}: ").strip()
    except (EOFError, KeyboardInterrupt):
        print()
        return ""
```

**关键**：**只有 stdin 是真的 TTY 才用 `getpass`**，避免 piped context 卡死。

### 1.6 多行输入 + Bracketed Paste

TUI 文档（`website/docs/user-guide/tui.md:57-59`）：
> Composer affordances — inline paste-collapse for long snippets, `Cmd+V` / `Ctrl+V` text paste with clipboard-image fallback, **bracketed-paste safety**, image/file-path attachment normalization.

**键盘绑定**：
- `Ctrl+G` / `Ctrl+X Ctrl+E` — 打开 `$EDITOR` 多行编辑
- `Ctrl+V` / `Cmd+V` — 文本粘贴 → OSC52 → clipboard image fallback
- `Ctrl+X` — 实时 session 切换
- `/` — 浮动 slash 自动补全

### 1.7 跨线程打印协调

**问题**：bg thread 调用 print 会和 PT 的 input area redraw 赛跑，导致输出被埋在 prompt 后。

**修复**（`tests/cli/test_cprint_bg_thread.py`）：
```python
# 跨线程打印必须经 run_in_terminal
app.loop.call_soon_threadsafe(app.run_in_terminal, print_func)
```

**专门为了**：自进化的后台 review "💾 Self-improvement review: …" 摘要能浮到用户眼前。

---

## 二、Claude Code 的 TTY 输入处理

### 2.1 Ink/React 组件架构

**不是 `readline` 模式，而是组件化的 TUI 编辑器**：

```
PromptInput/
├── PromptInput.tsx                  主组件
├── PromptInputFooter.tsx            底部状态栏
├── PromptInputFooterLeftSide.tsx    vim 模式 / mode 指示
├── PromptInputFooterSuggestions.tsx 补全建议
├── PromptInputHelpMenu.tsx          帮助菜单
└── PromptInputModeIndicator.tsx     模式指示
```

**核心状态模型**：
```ts
{
    value: string,
    cursorPosition: number,
    inputMode: 'normal' | 'vim' | 'search',
    vimMode: 'INSERT' | 'NORMAL',
    history: string[],
    suggestions: Suggestion[],
    selectedSuggestion: number,
    isPasting: boolean,
    isSearching: boolean,
}
```

### 2.2 多行编辑（逻辑 buffer 模式）

**关键设计**：text 与 terminal 渲染解耦

```text
\ + Enter   快速换行（不发送）
Shift+Enter 换行
Ctrl+J      换行
Enter       提交
```

```
逻辑 buffer（source of truth）
    ↓ 映射
渲染行/列（visual layout）
    ↓ cursor
terminal 显示
```

**优势**：
- newline 插入不立即提交
- cursor 跨软换行移动
- resize 时重新计算 layout 但不动 logical cursor offset
- 删除/选择与 Ink layout 解耦

### 2.3 Vim 模式（显式状态机）

```ts
type VimMode = 'INSERT' | 'NORMAL'

// INSERT 模式
- 字符插入、删除、navigation、newline、提交

// NORMAL 模式
- Vim 导航和编辑命令
- h/j/k/l, word/line movement, dd/yy/p 等

// 切换
Esc          INSERT → NORMAL
i, a, o, ... NORMAL → INSERT
```

**Footer 显示当前 vim mode**（行为 + UI 同步）。

### 2.4 Bracketed Paste（防误触发）

```text
ESC [ 200 ~   paste start
ESC [ 201 ~   paste end
```

**Hook 状态**：
```ts
isPasting: boolean
```

**好处**：
- 粘贴的换行不会立即提交
- 粘贴的 tab/控制序列不会激活快捷键
- 大粘贴 → 切换到 paste 状态/placeholder（避免大内容渲染卡顿）

### 2.5 Paste-Image（剪贴板图片）

`usePasteImage.ts` 工作流：

```
1. 检测 image paste
   ↓
2. 写到临时文件
   ↓
3. 在 input 中插入 placeholder
   ↓
4. 文件关联到 user message
   ↓
5. 上传失败 → 不污染 text input
```

**关键**：image 数据**不直接嵌入**到可见 terminal 文本，input 只含 lightweight marker。

### 2.6 Ctrl+C / Ctrl+D 的状态机

```
Ctrl+C:
- 取消 active generation / tool
- 取消 active search
- 清空 current input / 退出 transient mode
- 连续 2 次 Ctrl+C 退出 REPL

Ctrl+D:
- buffer 为空 → 退出
- 否则按 input mode 处理
```

**重要**：modal（permission / AskUserQuestion）必须**优先截获**这些键，避免误提交/终止。

### 2.7 History Navigation

```text
↑ / ↓       buffer 起止位置 → 切换 history
search mode 替换普通 history navigation
失败匹配   → footer 显示
```

**特性**：100 个最近 unique prompts，重复折叠到最新。

### 2.8 Autocompletion（context-sensitive）

```text
@path       文件/路径补全
/command    slash command 补全
```

**架构分离**：suggestion 计算 / selected index / 渲染 / 按键 / 插入替换范围 各自独立。

**Fullscreen 模式**：建议面板升级为全屏 overlay，避免被 footer 截断。

### 2.9 AskUserQuestion / Permissions 的 Modal 设计

```ts
type AskState = {
    questionIndex: number,
    selectedOption: number,
    selectedOptions: Set<number>,  // 多选
    customInput: string,
    focusedElement: string,
}
```

**核心原则**：

```
✅ 暂时拥有键盘 focus
✅ 阻断普通 prompt 提交
✅ 确定性的答案 routing
✅ 提交前 validation
✅ cancellation 处理
✅ 多问题导航
✅ user prompt 和 tool authorization 严格分离
```

### 2.10 IME 支持（中日韩）

```text
compositionstart    开始输入
compositionupdate   候选更新
compositionend      提交
```

**关键**：submit / 快捷键**在 composition 期间必须禁用**，否则输入中文时按 Enter 会同时提交 IME 候选和发送消息。

### 2.11 Terminal Resize

```ts
// useTerminalSize.ts
export function useTerminalSize(): TerminalSize {
    const size = useContext(TerminalSizeContext)
    if (!size) throw new Error('must be used within Ink App')
    return size
}
```

**影响范围**：wrapping、cursor 位置、suggestion 宽度、footer layout、窄终端模式、fullscreen overlay。

### 2.12 Focus Management

```
prompt input (默认)
  ↓ ↑↓
suggestion popup (navigation keys)
help menu (own keys)
history search (text + results)
permission dialog (approval keys)
AskUserQuestion (option selection)
agent/task view (replaces prompt)
```

**显式状态机**（footer 接收 `tasksSelected`, `teamsSelected`, `tmuxSelected`, `isSearching`, `isPasting` 等）。

### 2.13 非 TTY / print mode

- 禁用所有交互式 dialogs
- 自动 `-p` flag 启用
- 输出一次性 dump 不重绘

---

## 三、Hermes-Agent 的后台任务观测追踪

### 3.1 四大后台任务系统

```
1. Delegation     (delegate_task)
2. Cron           (cron/jobs.py + cron/scheduler.py)
3. Background terminal (terminal(background=true, notify_on_complete=True))
4. TUI cron delivery (via /cron slash command)
```

### 3.2 ProcessRegistry（核心注册表）

```python
# tools/process_registry.py
class ProcessRegistry:
    _running: dict               # 活跃进程
    _finished: dict              # 完成进程
    completion_queue: queue.Queue  # 完成事件队列
    _completion_consumed: set    # 幂等保护
    _poll_observed: set          # poll skip-once
```

**drain_notifications() 语义**：
```python
# 关键：ownership-filtered drain
results = process_registry.drain_notifications(
    session_key="OWNER",
    owns_event=lambda e: e.get("session_key") == "OWNER",
)

# 反例测试：未授权 drain 不能消费 restored events
def test_unfiltered_drain_never_consumes_restored_events():
    reg.completion_queue.put(_delegation_event(session_key="DEAD_SESSION", restored=True))
    results = reg.drain_notifications()  # 无 filter
    assert results == []
    assert reg.completion_queue.qsize() == 1  # 还在 queue 里
```

### 3.3 Async Delivery Capability Check

```python
# gateway/session_context.py:96-115
_SESSION_ASYNC_DELIVERY: ContextVar = ContextVar(
    "HERMES_SESSION_ASYNC_DELIVERY", default=_UNSET
)

def async_delivery_supported() -> bool:
    """当 active session 被 stateless adapter（API server）绑定时，
    没办法把通知 route 回 agent。返回 False。"""
    # 默认 _UNSET → supported（CLI/cron/test 兼容）
    # API server 显式 opt-out
```

**工具用法**：
```python
# tools/terminal_tool.py
if background and (notify_on_complete or watch_patterns):
    if not _async_ok():
        # Stateless HTTP API — refuse the promise
        notify_on_complete = False
        watch_patterns = None
        result_data["notify_unsupported"] = (
            "notify_on_complete is not available on this endpoint"
            " (stateless HTTP API). Use process(poll/wait)."
        )
```

### 3.4 notify_on_complete vs watch_patterns 冲突解决

```python
# tools/terminal_tool.py:2053-2077
def _resolve_notification_flag_conflict(*, notify_on_complete, watch_patterns, background):
    """两个 flag 都设会产生重复通知——只保留 notify_on_complete。"""
    if background and notify_on_complete and watch_patterns:
        return None, "watch_patterns ignored because notify_on_complete=True"
    return watch_patterns, ""
```

### 3.5 后台进程通知配置

```yaml
# config.yaml
display:
  background_process_notifications: all  # all | result | error
```

或者环境变量：
```
HERMES_BACKGROUND_NOTIFICATIONS=all
```

**3 个档位**：
- `all`：running-output 更新 + 最终消息（默认）
- `result`：仅最终完成消息
- `error`：仅非零退出码消息

### 3.6 Silent-Background Hint（防误用）

```python
# tools/terminal_tool.py:2502-2524
if background and not notify_on_complete and not watch_patterns:
    result_data["hint"] = (
        "background=true without notify_on_complete=true means "
        "this process runs SILENTLY — you will not be told when "
        "it exits. Re-launch with notify_on_complete=true, or "
        "call process(action='poll') / process(action='wait')."
    )
```

**背景**：2026-05 #31231 incident —— CI poller 跑绿但 agent 不知道。

### 3.7 委托所有权（防止跨 session 误消费）

```python
# tests/tools/test_restored_delegation_ownership.py
def test_owns_event_callback_beats_restored_flag():
    reg = ProcessRegistry.__new__(ProcessRegistry)
    reg.completion_queue.put(_delegation_event(session_key="OWNER", restored=True))
    results = reg.drain_notifications(
        owns_event=lambda e: e.get("session_key") == "OWNER"
    )
    assert len(results) == 1
```

**关键**：跨 session 恢复的事件带 `restored=True` 标记，必须用 `owns_event` callback 证明所有权才能消费。

### 3.8 Cron Auto-Delivery（per-job ContextVars）

```python
# gateway/session_context.py:116-121
_CRON_AUTO_DELIVER_PLATFORM: ContextVar
_CRON_AUTO_DELIVER_CHAT_ID: ContextVar
_CRON_AUTO_DELIVER_THREAD_ID: ContextVar

# cron/scheduler.py:2813-2816
# 用 ContextVars 而非 os.environ，避免并发 job 互相覆盖
from gateway.session_context import set_session_vars, clear_session_vars
```

### 3.9 API Server 后台 Run 追踪

```python
# gateway/platforms/api_server.py:966-1005
class APIServer:
    _run_streams: Dict[str, asyncio.Queue]           # 输出流
    _run_streams_created: Dict[str, float]            # TTL
    _run_stream_subscribers: set[str]                  # SSE 消费者
    _active_run_agents: Dict[str, Any]
    _active_run_tasks: Dict[str, asyncio.Task]
    _stopping_run_ids: set[str]                         # 协作停止
    _run_statuses: Dict[str, Dict]                      # dashboard
    _run_approval_sessions: Dict[str, str]
    _pending_agent_requests: int                        # 关闭 drain
```

**Stop endpoint**（`api_server.py:5174-5240`）：
```
POST /v1/runs/{run_id}/stop
  → 设置 run status = stopping
  → 标记 unregister
  → 允许 graceful teardown
```

### 3.10 Subagent Approval Callback 继承

**致命陷阱**：subagent worker threads **不继承 `threading.local()` callback**。

```python
# tools/delegate_tool.py:60-72
# 修复：用 ThreadPoolExecutor(initializer=...) 给每个 worker 装 callback
ThreadPoolExecutor(
    initializer=_set_subagent_approval_cb,
    initargs=(cb,)
)

# 配置（来自 config.yaml）
delegation:
  subagent_auto_approve: false  # 默认 safe（叶子工具黑名单）
  # true                    # opt-in YOLO for cron/batch
```

---

## 四、Claude Code 的后台任务观测追踪

### 4.1 Bash 后台机制

```text
Ctrl+B       把当前 Bash 调用后台化
tmux 用户    按 Ctrl+B 两次（tmux prefix 占用）
```

**核心特性**：
- 异步执行
- **立即返回 unique task ID**
- **输出写到文件**（不污染主 transcript）
- 用 `BashOutput` 或 `Read` 拉取

### 4.2 BashOutput Tool

```ts
export type BashOutputToolInput = Partial<TaskOutputInput> & {
    bash_id?: string,
    filter?: string,    // 可选过滤
}

export type BashOutputToolOutput = string
```

**API**：
```text
BashOutput(bash_id)
BashOutput(bash_id, filter)
```

### 4.3 Subagent 后台机制

```json
{
    "run_in_background": true
}
```

**生命周期**：
```
created → running → completed | failed | stopped
```

### 4.4 Push + Pull 双模型（关键设计）

#### Push 路径（notification）

```ts
const MAX_VISIBLE_NOTIFICATIONS = 3

// 溢出时折叠
<task-notification>
<summary>+N more tasks completed</summary>
<status>completed</status>
</task-notification>
```

**触发事件**：
- task started
- task completed
- task failed
- task stopped
- 进度/状态简短消息
- queued task notifications

#### Pull 路径（on-demand）

```
详细 Bash output
filtered output
task inspection
completed sub-agent 结果
running jobs 检查
```

**优势**：notification 保持 compact，详细 output 按需拉取。

### 4.5 `/tasks` vs 任务清单（重要区分）

```text
Ctrl+T  → TaskCreate/TaskList/TaskUpdate 的 checklist
/tasks  → running shells + sub-agents 视图
```

**架构意义**：planning tasks ≠ process-monitoring records。

### 4.6 Agent/Team View

```ts
viewingAgentTaskId  // 状态字段
```

**关键行为**：
- 进入 agent view → 隐藏 leader notifications
- 保证 transcript 焦点
- 返回主 prompt → 恢复 notifications

```tsx
if (viewingAgent || messages === null) {
    return null
}
```

### 4.7 Footer 集成

```tsx
<PromptInputFooter>
    <CoordinatorTaskPanel />
    <Notifications ... />
</PromptInputFooter>
```

**Footer 接收状态**：
```tsx
{
    tasksSelected: boolean,
    teamsSelected: boolean,
    teammateFooterIndex: number,
    tmuxSelected: boolean,
    onOpenTasksDialog: () => void,
}
```

### 4.8 资源限制

```text
5 GB output cap            输出超过 5GB 终止 task
automatic cleanup on exit  退出时清理后台 task
memory-pressure reaping    可选 OS 内存压力 reap
CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1  完全禁用
CLAUDE_CODE_DISABLE_BG_SHELL_PRESSURE_REAP=1  禁用内存 reap
```

### 4.9 五状态机（独立 task）

```
pending → running → completed | failed | stopped
                ↓
            (cancelled)
```

### 4.10 五层状态域（架构清晰）

```
1. Prompt/input state      buffer, focus, suggestions, modal
2. Conversation state      transcript messages, tool calls, streaming
3. Background task registry  task ID, status, output location
4. Checklist/task list     TaskCreate/TaskList/TaskUpdate 持久化
5. Agent/team state        identity, parent/child, selected
```

---

## 五、对比表

### 5.1 TTY 输入处理

| 维度 | Hermes-Agent | Claude Code |
|------|--------------|-------------|
| 引擎 | prompt_toolkit + 自研 Ink | Ink/React |
| TTY 检测 | 双层 `_is_tty` + `_is_patch_stdout_proxy` | 自动（Ink context） |
| Vim 模式 | ❌（CLI 层） | ✅ INSERT/NORMAL |
| 多行编辑 | ✅ 自动 | ✅ 显式 |
| Bracketed paste | ✅ | ✅ |
| Paste image | ✅ getpass + OSC52 | ✅ usePasteImage |
| SIGINT 处理 | 3 层（defer/Win compat/MCP 转发） | 状态机 |
| Fail-closed guard | ✅ PT 死锁防护 | ✅ Modal focus 截获 |
| 跨线程 print | `loop.call_soon_threadsafe` | N/A（Ink 单 thread） |
| IME 支持 | ⚠️ 基础 | ✅ composition start/update/end |
| 历史导航 | ✅（100 unique） | ✅（100 unique 折叠） |
| Autocomplete | ✅ / + @ | ✅ / + @ |
| 密码输入 | ✅ `getpass` + TTY guard | N/A |

### 5.2 后台任务观测

| 维度 | Hermes-Agent | Claude Code |
|------|--------------|-------------|
| 中心注册表 | `ProcessRegistry` | Task registry（分散组件） |
| 任务 ID | ✅ | ✅ |
| 输出存储 | 文件 | 文件 |
| Push notification | ✅ 3 档（all/result/error） | ✅ MAX_VISIBLE=3 |
| Pull 模型 | ✅ `process(action='poll/wait')` | ✅ `BashOutput(bash_id)` |
| Async delivery 能力检查 | ✅ ContextVar capability | ⚠️ 默认支持 |
| 冲突解决 | notify vs watch 自动 mutex | N/A |
| Silent background hint | ✅ | ⚠️ 隐含 |
| Restore ownership | ✅ `restored=True` + `owns_event` callback | ⚠️ 不明显 |
| Cron per-job ContextVar | ✅ 避免 os.environ clobber | N/A |
| Approval 继承 | ✅ ThreadPoolExecutor initializer | ⚠️ Modal 截获 |
| 资源限制 | ⚠️ 隐含 | ✅ 5GB cap + reap |
| Checklist vs /tasks | ⚠️ 单体系 | ✅ 明确分离 |
| Agent view 隐藏 leader notif | N/A | ✅ `viewingAgentTaskId` |
| 多层状态域 | ⚠️ Registry + DB | ✅ 5 层 |

---

## 六、可落地到 mocode 的改进

### 6.1 高优先级（必做）

| 改进 | 工作量 | 借鉴 |
|------|--------|------|
| **TTY 检测双层防护** | 1h | Hermes `_is_tty` + `_is_patch_stdout_proxy` |
| **Fail-closed permission guard** | 4h | Hermes fail-closed test #15216 |
| **SIGINT defer + Windows compat** | 4h | Hermes 3 层 SIGINT |
| **后台任务 ID + 文件输出** | 4h | Claude Code BashOutput |
| **Push + Pull 双模型** | 6h | Hermes ProcessRegistry + CC notification |
| **Silent background hint** | 1h | Hermes fail-fast |
| **Approval callback 跨线程继承** | 4h | Hermes ThreadPoolExecutor initializer |
| **Push notification MAX_VISIBLE=3** | 1h | Claude Code overflow 折叠 |

### 6.2 中优先级（应该做）

| 改进 | 工作量 | 借鉴 |
|------|--------|------|
| **Vim mode** | 6h | Claude Code `INSERT/NORMAL` |
| **IME composition 感知** | 3h | Claude Code composition events |
| **Bracketed paste 检测** | 2h | CC `ESC[200~` |
| **Restore ownership 验证** | 4h | Hermes `owns_event` callback |
| **Async delivery capability** | 3h | Hermes `_SESSION_ASYNC_DELIVERY` |
| **5GB output cap** | 2h | Claude Code resource limit |
| **Cron per-job ContextVar** | 3h | Hermes `_CRON_AUTO_DELIVER_*` |

### 6.3 低优先级（有时间）

| 改进 | 工作量 | 借鉴 |
|------|--------|------|
| **Checklist vs `/tasks` 明确分离** | 8h | Claude Code |
| **Agent view 隐藏 leader notification** | 4h | Claude Code |
| **5 层状态域拆分** | 16h | Claude Code |
| **8 个外部 memory provider 适配器** | 40h+ | Hermes |

---

## 七、mocode 现状差距

| 能力 | mocode | Hermes | Claude Code |
|------|--------|--------|-------------|
| Background bash | ✅ `job_input/output/kill` | ✅ | ✅ |
| Background sub-agent | ✅ Agent tool | ✅ | ✅ |
| TTY detection | ⚠️ 基础 | ✅ 双层 | ✅ |
| Push notification | ❌ | ✅ | ✅ MAX=3 |
| Pull via ID | ✅ `job_output` | ✅ `process(poll/wait)` | ✅ `BashOutput` |
| Fail-closed approval | ⚠️ 基础 dialog | ✅ 测试 | ✅ Modal 截获 |
| Vim mode | ❌ | ❌ | ✅ |
| Bracketed paste | ⚠️ Bubble Tea textarea | ⚠️ | ✅ |
| IME | ⚠️ Bubble Tea | ⚠️ | ✅ |
| SIGINT defer | ❌（直接退出） | ✅ 3 层 | ⚠️ |
| Async delivery check | ❌ | ✅ | ❌ |
| Approval callback inherit | ❌ | ✅ ThreadPool | ⚠️ |
| Restore ownership | ❌ | ✅ | ❌ |

**最大缺口**：

1. **后台任务没有 ID-based 状态机**（mocode 用 bash-like job_id 但缺生命周期）
2. **没有 Push notification 折叠机制**（>3 个就乱）
3. **没有 fail-closed permission guard**（TTY 死锁风险）
4. **SIGINT 处理简单**（Windows 不友好）

---

## 八、mocode 落地路线图

### 8.1 Phase 1（1 周，TTY 安全）

```go
// internal/ui/util/tty.go
func IsTTY(w io.Writer) bool {
    if f, ok := w.(*os.File); ok {
        return terminal.IsTerminal(int(f.Fd()))
    }
    return false
}

// internal/ui/util/sigint.go
// SIGINT defer + Windows 兼容
func WithDeferredSIGINT(fn func() error) error {
    // 保存 handler
    // 装自定义 handler（缓存信号）
    // 执行 fn
    // 还原 handler
    // 重发缓存的信号（用 syscall.Kill + 0 signal 或 raise）
}
```

### 8.2 Phase 2（1 周，后台任务 ID 化）

```go
// internal/core/jobs/registry.go
type ProcessRegistry struct {
    mu          sync.RWMutex
    running     map[string]*Job
    finished    map[string]*Job
    notifyChan  chan Notification
}

type Job struct {
    ID       string
    Cmd      string
    Status   string  // "running" | "completed" | "failed" | "killed"
    Output   *OutputBuffer
    ExitCode int
    StartedAt time.Time
    EndedAt   time.Time
}

// Push notification 限流
const MAX_VISIBLE_NOTIFICATIONS = 3
```

### 8.3 Phase 3（1 周，权限 fail-closed）

```go
// internal/core/permission/failclosed.go
func PromptDangerousApproval(ctx, request) (Decision, error) {
    // 1. 检查是否在 PT-style 框架里
    if ui.HasInputOwnership() {
        // 2. 是否有 callback？
        cb := getApprovalCallback()
        if cb == nil {
            // fail-closed
            return Decision{Behavior: "deny", Reason: "no callback registered"}, nil
        }
        return cb(request)
    }
    // 3. 正常路径
    return ui.ShowApprovalDialog(request)
}
```

### 8.4 Phase 4（2 周，Vim mode + IME + Bracketed paste）

```go
// internal/ui/editor/vim.go
type VimMode int
const (
    VimInsert VimMode = iota
    VimNormal
)

// internal/ui/editor/ime.go
type IMEState struct {
    Composing  bool
    Preedit    string
    StartPos   int
}

// internal/ui/editor/paste.go
func HandleBracketedPaste(data []byte) {
    if bytes.HasPrefix(data, []byte("\x1b[200~")) {
        // bracketed paste
    }
}
```

---

## 九、关键设计模式（可复用模板）

### 模板 1 · TTY 检测 + Capability Check

```go
type Capability struct {
    TTY        bool
    AsyncOK    bool
    HasCallback bool
}

func DetectCapability() Capability {
    return Capability{
        TTY:        IsTTY(os.Stdout),
        AsyncOK:    !isStatelessAdapter(),
        HasCallback: getApprovalCallback() != nil,
    }
}
```

### 模板 2 · Async Delivery Refusal

```go
func MaybeRunBackground(cmd, opts) {
    if opts.Background && opts.NotifyOnComplete {
        if !capability.AsyncOK {
            opts.NotifyOnComplete = false
            // 提示用户改用 poll/wait
        }
    }
    exec.Run(cmd, opts)
}
```

### 模板 3 · Push Notification with Overflow

```go
const MAX_VISIBLE = 3

type NotificationStack struct {
    items []Notification
}

func (s *NotificationStack) Push(n Notification) {
    s.items = append(s.items, n)
    if len(s.items) > MAX_VISIBLE {
        // 折叠为 "+N more"
        overflow := len(s.items) - MAX_VISIBLE
        s.items = s.items[len(s.items)-MAX_VISIBLE:]
        s.items = append(s.items, OverflowNotification{N: overflow})
    }
}
```

### 模板 4 · SIGINT Defer

```go
func WithDeferredSIGINT(fn func() error) error {
    original := signal.Notify(make(chan os.Signal, 1), syscall.SIGINT)
    defer signal.Reset(syscall.SIGINT)
    
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT)
    
    errCh := make(chan error, 1)
    go func() { errCh <- fn() }()
    
    select {
    case err := <-errCh:
        return err
    case <-sigCh:
        // 缓存信号，等 fn 完成后重发
        <-errCh
        signal.Reset(syscall.SIGINT)
        syscall.Kill(syscall.Getpid(), syscall.SIGINT)
        return nil
    }
}
```

### 模板 5 · Approval Callback 继承

```go
// 父线程装 callback
func SetApprovalCallback(cb func(req) Decision) {
    approvalCallback.Store(cb)
}

// Worker thread 通过 initializer 继承
func NewApprovalCallbackBoundExecutor(cb func(req) Decision) *Executor {
    return &Executor{
        initializer: func() {
            approvalCallback.Store(cb)  // 每个 worker 都装上
        },
    }
}
```

---

## 十、核心洞察总结

### 10.1 TTY 输入的 3 个核心难点

1. **死锁防护**：interactive 工具（prompt_toolkit/Ink）拥有 stdin 时，普通 `input()` 会死锁——必须 fail-closed
2. **Windows 兼容**：`os.kill(pid, SIGINT)` 在 Windows 走 TerminateProcess，必须用 `signal.raise_signal`
3. **跨线程安全**：bg thread 调 print 必须经 `loop.call_soon_threadsafe`，否则被 prompt redraw 覆盖

### 10.2 后台任务的 4 个核心难点

1. **Pull vs Push**：notification 保持 compact，详情按需拉取——**不要把大输出塞进 context**
2. **Ownership 验证**：跨 session 恢复的事件必须用 callback 证明所有权
3. **Capability 检查**：stateless adapter（API server）不支持 async delivery，必须 fail-fast
4. **Silent background**：bg=true 但没 notify 是个常见 footgun，必须显式 hint

### 10.3 mocode 当前最大缺口

| 缺口 | 风险 |
|------|------|
| 没有后台任务 ID 化状态机 | 用户无法可靠追踪 running job |
| 没有 Push notification 折叠 | >3 个 task 时 UI 乱 |
| 没有 fail-closed permission | TTY 死锁风险 |
| SIGINT 直接退出 | Windows 不友好（TerminateProcess 退码 2） |

**建议优先顺序**：
1. SIGINT defer（Windows compat 优先）
2. 后台任务 ID 化（用户感知最强）
3. Push notification 折叠（视觉清爽）
4. Fail-closed permission（稳定性）
5. Vim mode + IME（高级功能）

---

## 十一、参考链接

| 项目 | URL |
|------|-----|
| Hermes Agent | github.com/NousResearch/hermes-agent |
| Hermes Approval Test | tests/tools/test_approval.py:1797-1835 |
| Hermes SIGINT Defer | tools/environments/file_sync.py:282-316 |
| Hermes ProcessRegistry | tools/process_registry.py |
| Hermes Async Delivery | gateway/session_context.py:96-115 |
| Claude Code PromptInput | src/components/PromptInput/PromptInput.tsx |
| Claude Code BashOutput | types/toolTypes.ts |
| Claude Code /tasks | docs/interactive-mode.md |
| Claude Code docs mirror | github.com/ericbuess/claude-code-docs |
| Better Clawd fork | github.com/x1xhlol/better-clawd |

---

## 十二、mocode 改进 Checklist（按 ROI 排序）

### 立即做（< 1 天，每项 < 4h）

- [ ] TTY 双层检测（1h）
- [ ] SIGINT defer（3h）
- [ ] Push notification MAX_VISIBLE=3（1h）
- [ ] Silent background hint（1h）

### 一周内（每项 < 8h）

- [ ] 后台任务 ProcessRegistry（6h）
- [ ] Fail-closed permission guard（4h）
- [ ] Async delivery capability check（3h）
- [ ] Approval callback 跨线程继承（4h）

### 一月内（核心功能）

- [ ] Bracketed paste 检测（2h）
- [ ] IME composition 感知（3h）
- [ ] Vim mode（6h）
- [ ] 5GB output cap（2h）

### 长期

- [ ] Checklist vs `/tasks` 明确分离（8h）
- [ ] Agent view 隐藏 leader notif（4h）
- [ ] 5 层状态域拆分（16h）