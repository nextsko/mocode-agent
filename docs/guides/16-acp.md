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
| 会话 | `session/new`、`session/load`（回放历史）、`session/resume`、`session/list`、`session/delete`、`session/close` |
| MCP | `session/new` 的 MCP servers 按会话挂载：stdio / HTTP / SSE（env、headers 支持；关闭时移除） |
| 回合 | `session/prompt`、`session/cancel` |
| 设置 | `session/set_mode`（mocode 的 agent 模式）、`session/set_config_option`（模型选择） |
| 流式更新 | `agent_message_chunk`、`agent_thought_chunk`、`tool_call`、`tool_call_update`（pending → completed/failed）、`plan`、`available_commands_update`、`current_mode_update`、`config_option_update` |
| 权限 | 工具需要授权时向客户端发 `session/request_permission`，四种 kind：allow_once / allow_always / reject_once / reject_always |
| 命令 | 启动会话后推送 `available_commands_update`，来自 mocode 的 slash 命令注册表 |
| 计划 | agent 调用 `todos` 工具维护计划时，实时映射为 `plan` 更新（同一份 session.Todos，不另造数据源） |

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

## 与官方/主流实现对齐

- `session/load` 按规范先回放历史（user/agent chunk），再响应请求；
- stdio MCP 按规范支持，会话结束清理；HTTP/SSE 未声明能力；
- 取消会同时终止挂起的 `session/request_permission`（返回 cancelled）；
- 权限选项对齐官方 kind 与示例 optionId（allow-once / allow-always / reject-*）。

## 当前边界（诚实说明）

- 客户端 fs / terminal / elicitation 方法尚未被本地工具调用（工具在进程内执行）；
- `reject_always` 在**会话内**记住拒绝（同一工具后续不再询问），但不跨会话持久化；
- `session/load` 现在会回放文本 + tool_call / tool_result，但不重放 reasoning。

这些是下一步的明确缺口，而非“已支持”的模糊表述。
