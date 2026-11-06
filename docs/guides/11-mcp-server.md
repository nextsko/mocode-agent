# 11 · 原生 MCP Server 与客户端集成

> 状态：**已实现并端到端验证**（`mocode mcp serve` → DeepSeek Harness `mcp__mocode__run` → 真实模型）
> 相关：[10-mcp-configuration.md](10-mcp-configuration.md)（mocode 作为 MCP **客户端**）
> 代码：`internal/transport/mcp`（协议层）、`internal/transport/cmd/mcp.go`（命令与 Runner）

---

## 1. 为什么做原生实现

mocode 之前只能作为 MCP **客户端**（在 `mocode.json` 的 `mcp` 段里挂别人的 MCP server）。
要把 mocode 自己暴露给编辑器或 Agent Harness，常见做法是写一个 node/python 的 stdio 桥接脚本：
外部 MCP 客户端 → 桥接进程 → 再调用 `mocode run`。

桥接有三个问题：多一个运行时依赖、多一层协议转发、`cwd`/环境/密钥要重新拼一遍。

原生实现把这件事收进二进制：**一个 flag 级别的入口，直接说 MCP**。

```bash
mocode mcp serve
```

stdin/stdout 走 newline-delimited JSON-RPC 2.0，不依赖 node、不额外安装任何东西。

## 2. 用法

```bash
# 由 MCP 客户端拉起（正常情况，不要手动跑）
mocode mcp serve

# 冒烟测试：握手 + 列工具
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | mocode mcp serve
```

启动时的工作目录决定默认项目与 provider 配置（与 `mocode run` 的配置解析完全一致）。

### 暴露的工具

| 工具 | 参数 | 说明 |
|---|---|---|
| `run` | `prompt`（必填）、`cwd`（可选） | 执行一次非交互 prompt，返回最终回答；等价于 `mocode run --quiet`，但输出被捕获而不是打到 stdout |
| `models` | 无 | 列出 `provider/model` 形式的已配置模型 |

`cwd` 用来切换到另一个项目：server 会为该目录懒加载并缓存一个 workspace（同一个目录复用同一个进程内 `app.App`，退出时统一 `Shutdown`）。

### 协议细节

- JSON-RPC 2.0，**每行一个 JSON**；stdout 只允许协议帧，日志一律走 stderr。
- 方法：`initialize`、`notifications/initialized`、`ping`、`tools/list`、`tools/call`。
- 协议版本：客户端请求的版本若在支持列表（`2024-11-05` … `2026-07-28`）内则原样回显，否则回落到 `2025-06-18`。
- `tools/call` 的失败用结果里的 `isError: true` 表达（MCP 约定），而不是 JSON-RPC error；未知方法才回 `-32601`。
- 单次工具输出上限 1 MiB，超出按 UTF-8 边界截断并追加提示。
- 请求串行处理；`ctx` 在 server 退出时取消。

## 3. 客户端配置

### DeepSeek Harness（本地实测）

Harness 内置 `@deepseek-ai/dsh-mcp-client`，在 profile 的 `cordis.patch.yml` 里插入一行即可：

```yaml
- insert:
    - id: mcp-client-mocode
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        transport: stdio
        serverName: mocode
        command: /path/to/bin/mocode
        args:
          - mcp
          - serve
        cwd: /your/default/project
        toolCallTimeoutMs: 900000
        env:
          MOCODE_DNS_SERVER: 223.5.5.5        # 无 /etc/resolv.conf 的主机需要
          TOKENRHYTHM_API_KEY: '...'           # 见下面的密钥说明
```

要点：

1. **工具名是 `mcp__<serverName>__<tool>`**，即 `mcp__mocode__run` / `mcp__mocode__models`。
2. Harness 会从子进程环境里**过滤掉**名字含 `KEY` / `PASSWORD` / `SECRET` / `TOKEN` 的变量（还有 `DSH_*`），所以密钥必须显式写在 `config.env` 里，或改为从文件读取。
3. `cwd` 建议显式给一个常用项目目录；否则子进程继承 Harness 的工作目录。
4. 配置改动后 Harness 会热加载（profile 变更会触发），无需重启引擎。

### 任意 MCP 客户端（Claude Desktop / Cline / 自研）

```json
{
  "mcpServers": {
    "mocode": {
      "command": "/path/to/bin/mocode",
      "args": ["mcp", "serve"],
      "cwd": "/path/to/project",
      "env": { "MOCODE_DNS_SERVER": "223.5.5.5" }
    }
  }
}
```

### 和 `mocode.json` 的 `mcp` 段的关系

两者方向相反，互不冲突：

| 段/命令 | 角色 |
|---|---|
| `mocode.json` → `mcp` | mocode 作为**客户端**，消费别人的 MCP server |
| `mocode mcp serve` | mocode 作为**服务端**，被别人消费 |

## 4. 配置与密钥

- provider 配置照旧从 `mocode.json` 读（`$ENV_VAR` 语法可用），所以 `mocode mcp serve` 的进程必须能拿到对应环境变量。
- 如果客户端会清洗环境变量（如 Harness），在客户端的 `env` 里显式传。
- 不要把密钥硬编码进仓库内的配置文件；放在本机 profile/客户端配置里（文件权限 600）。
- 无 `/etc/resolv.conf` 的主机（Android/Termux、精简容器）需要 `MOCODE_DNS_SERVER=<dns>:53`，否则任何网络请求都会 `lookup ... on [::1]:53: connection refused`。

## 5. 排错

| 现象 | 原因 | 处理 |
|---|---|---|
| 客户端报 `unauthorized` / 401 | 进程里没有 API key（客户端清洗了 `*KEY*` 或未传） | 在客户端 `env` 里显式传，或确认 `$ENV_VAR` 已设置 |
| `no providers configured` | 启动目录找不到 `mocode.json`，或 provider 被禁用 | 用 `-c` 指目录，或在 `cwd` 里放配置 |
| 一调用就返回 `[::1]:53 ... connection refused` | 缺 `/etc/resolv.conf` | 设 `MOCODE_DNS_SERVER` |
| 客户端一直等不到响应 | stdout 被日志污染 / 客户端超时太短 | 确认 stderr 才有日志；抬高客户端 `toolCallTimeoutMs`（建议 ≥ 10 分钟） |
| `cwd "..." is not a directory` | `run.cwd` 不存在 | 传真实存在的绝对路径 |
| 工具列表为空 | 初始化握手失败（协议版本或传输不匹配） | 先用第 2 节的 `printf` 冒烟测试验证协议 |

## 6. 设计说明

```
transport/cmd/mcp.go        # cobra `mcp serve`；实现 mcp.Runner（workspace 缓存、RunNonInteractive）
transport/mcp/server.go     # 纯协议层：JSON-RPC 帧、initialize、tools/list、tools/call
transport/mcp/server_test.go# 表驱动单测（fake Runner，无网络）
```

- 协议层不 import workspace，只依赖一个三方法的 `Runner` 接口 —— 分层规则（`transport → core`）不受影响，也方便单测。
- workspace 按目录懒加载 + 缓存：同一目录的多次调用复用同一个 `app.App`，避免每次都重建 LSP/MCP 连接。
- `run` 复用 `App.RunNonInteractive`，与 CLI `mocode run` 是同一条执行路径，不存在两套行为。

## 7. 验证记录

- `go test ./internal/transport/mcp/...` 全绿（握手、工具列表、run 成功/失败、未知方法、坏行忽略）。
- 用 `@modelcontextprotocol/client`（与 Harness 同一个 SDK）连原生二进制：`initialize` → `tools/list` → `models` → `run "只回复两个字母：OK"` 返回 `OK`。
- 接入 DeepSeek Harness 后，由 subagent 实际调用 `mcp__mocode__run`，返回 `OK`、无错误。
