# ACP：让 mocode 成为任何编辑器的 Agent

`mocode acp` 以 stdio 提供 **Agent Client Protocol v1**（JSON-RPC 2.0，换行分帧），
可直接被 Zed 等 ACP 客户端拉起。

## 启动

```bash
mocode acp            # 客户端负责stdio
mocode acp --yolo     # 自动批准所有权限（仅限可信环境）
```

客户端配置示例：

```json
{"command": "mocode", "args": ["acp"]}
```

## 已实现的方法

| 类别 | 方法 |
|---|---|
| 握手 | `initialize`（能力协商）、`authenticate`、`logout` |
| 会话 | `session/new`、`session/load`、`session/resume`、`session/list`、`session/delete`、`session/close` |
| 回合 | `session/prompt`、`session/cancel` |
| 设置 | `session/set_mode`（mocode 的 agent 模式）、`session/set_config_option`（模型选择） |
| 流式更新 | `agent_message_chunk`、`agent_thought_chunk`、`tool_call`、`tool_call_update`、`available_commands_update`、`current_mode_update`、`config_option_update` |
| 权限 | 工具需要授权时向客户端发 `session/request_permission`，支持 allow_once / allow_always / reject |
| 命令 | 启动会话后推送 `available_commands_update`，来自 mocode 的 slash 命令注册表 |

权限回调与工具调用流都接在真实执行路径上：客户端批准后 mocode 的工具照常执行，
结果通过 `tool_call_update` 回传。

## 真实端到端验证

```bash
# 1) 基础会话：initialize → session/new → prompt("只回复 OK")
#    输出：chunk "OK"、stopReason=end_turn、commands/session/list 正常
# 2) 权限链路：prompt 要求运行 shell，客户端回答 allow_once
#    输出：request_permission(bash) → 工具执行 → 回答包含 ACP_OK
```

测试在 `internal/transport/acp/acp_test.go`（协议/取消/权限/多块 prompt）
与 `rpc_test.go`（双向请求、通知）中；真实链路用 Node 最小客户端验证。

## 当前边界（诚实说明）

- `session/new` 的 `mcpServers` 暂不接入：mocode 使用自身配置里的 MCP 服务，
  不会为每个 ACP 会话动态挂载客户端提供的 MCP server；
- `session/load` 只恢复会话元数据，不回放历史消息；
- 客户端 fs / terminal / elicitation 方法尚未被本地工具调用（工具在进程内执行），
  协议层保留了这些能力的位置；
- 未发送 `plan` 更新（mocode 的 todo 尚未映射到 ACP plan）。

这些是下一步的明确缺口，而非“已支持”的模糊表述。
