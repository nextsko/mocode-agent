---
name: session-viz
description: >-
  Use when 需要可视化 mocode/andy-code 的会话结构、项目分布、消息流与事件链，或导出结构化 JSON。读取 session.db 与 projects/ 目录，用骨架解析 + 多种视图（tree/projects/flow/dump）呈现会话元数据、消息、工具产物、记忆与项目分类。只读、零副作用，支持增量扫描与损坏文件容错。
---

# 会话可视化（Session Visualization）

把本地 session 存储渲染成可读的结构视图，并支持导出 JSON 供二次分析。

> 数据结构与字段来源见 `references/schema.md`（SQLite schema）与 `references/architecture.md`（7 层架构与数据流）。涉及观测/追踪规范时联动 `observability`；导出文档的组织交给 `docs-rulebook`。

## 1. Skill 元数据规范

每个 skill 的 frontmatter 应包含：

```yaml
---
name: "{skill-name}"           # kebab-case, 全局唯一
description: "{一句话描述}"      # 中文，不超过 80 字
scope: "global|project"        # 作用域
active: true|false             # 是否默认激活
load_mode: "on_demand|always"  # 加载策略
priority: 1-100                # 优先级，越高越优先加载
tags: [tag1, tag2]             # 标签数组
version: "1.0.0"               # 语义版本
tools_required: []             # 所需工具列表
---
```

## 2. 目录结构

```
skills/session-viz/
├── SKILL.md              # 入口
├── references/
│   ├── architecture.md   # 架构参考
│   └── schema.md         # SQLite schema
    └── api.md            # Python API 参考
├── scripts/
│   ├── session_viz.py    # 主可视化脚本（Rich 渲染）
│   ├── session_parser.py # 纯解析模块（无第三方依赖）
│   └── dump_json.py      # JSON 导出工具
└── examples/
    └── sample_output.txt # 示例输出
```

## 3. 触发规则

| 触发词 | 行为 |
|--------|------|
| `session viz` / `session 可视化` | 运行完整可视化 |
| `session tree` | 仅架构树 |
| `session projects` | 仅项目分析 |
| `session flow` | 仅消息流 |
| `session dump` | 导出 JSON |

## 4. 解析接口（`session_parser.py`）

无状态解析器，只读 `session.db` + `projects/` 目录：

```python
class SessionParser:
    def parse(self) -> SessionChain:
        """返回完整 SessionChain 数据结构"""

@dataclass
class SessionChain:
    sessions: list[SessionRecord]
    messages: list[MessageRecord]
    artifacts: list[ArtifactRecord]
    projects: list[ProjectMeta]
    agents: list[AgentConfig]
    events: list[EventRecord]

@dataclass
class SessionRecord:
    session_id: str
    cwd: str
    mode: str | None
    saved_at: datetime
    source_seq: int
    message_count: int

@dataclass
class ProjectMeta:
    name: str           # 项目显示名 (display_name)
    store_id: str       # projects/ 子目录名
    project_path: str   # 实际路径
    category: str       # "real" | "protocol" | "agent-runner" | ...
    event_count: int
    conv_count: int
```

## 5. 存储模型要点

- **7 层架构**：Root config → SQLite → `projects/` 文件存储 → `agents/` → `back/` 指令库 → skills → logs/snapshots。
- **双存储策略**：SQLite 存结构化元数据（sessions/messages/artifacts/memories/traces），文件系统存事件流与大块数据。
- **Hash 目录隔离**：`projects/{name}--{hash}/` 保证多实例不冲突，内含 `project.json` + `event-log/*.jsonl` + `conversations/`。
- **Event Sourcing**：`event-log/*.jsonl` 记录完整事件历史。
- **核心表**：`sessions`(元数据)、`messages`(role: user/assistant/tool/status, 有 FTS)、`artifacts`(工具产物, `artifact_kind` 如 `tool:run_command`)、`memories`(reflection)、`selection_traces`(memory_selection)。
- **关系**：`sessions 1─N messages / artifacts / selection_traces / workflow_verdicts`；`sessions 0..1─N memories (source_session_id)`。
- **项目分类**：`real` / `state-test` / `temp` / `agent-runner` / `protocol-*` / `resume-*` / `snapshot-sync` / `history` 等。

## 6. 可视化输出规范

- **终端模式**：Rich 表格 / 树 / Panel，支持 ANSI 颜色。
- **JSON 导出**：`session_chain_dump.json`，包含全部结构化数据。
- **HTML 模式（未来）**：生成可交互的 D3.js 力导向图。

数据流（供渲染参考）：

```
User Input (CLI / TUI / WeChat / Scheduler)
  → Gateway → Agent Dispatcher (bot.toml: autonomy, mode routing)
      ├─ Skills Selection ← memory traces
      ├─ Prompt Loading   ← Global.md + AGENTS.md + 项目文件
      ├─ Agent Activation ← agents/*.toml
      └─ Tool Execution   ← MCP servers + native tools
  → session.db (sessions/messages/artifacts/memories)
  → projects/{name}--{hash}/ (project.json + event-log + conversations)
```

## 7. 质量标准

- ✅ 所有数据源使用**只读**连接，零 side-effect。
- ✅ 支持增量扫描（跳过不存在 / 已损坏的文件）。
- ✅ 错误处理：损坏文件记 warning 但继续解析，不中断整体输出。
- ✅ 命令行参数支持子视图与 JSON 导出。
- ✅ 输出的项目分类、消息计数、事件计数与实际库内记录一致。
