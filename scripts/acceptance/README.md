# ACP 真实验收脚本

这些脚本用 Node 拉起真实 `mocode acp`，覆盖：

| 脚本 | 验证 |
|---|---|
| `acp_acceptance.js` | initialize、new、set_mode、set_config_option、prompt、cancel、load 文本/工具回放、resume、close、delete、reject_always |
| `acp_load.js` | session/load 回放 |
| `acp_plan.js` | todos → ACP plan 更新 |
| `acp_mcp_stdio.js` | 会话级 stdio MCP 挂载与调用 |
| `acp_mcp_http.js` + `http_mcp_server.js` | HTTP MCP |
| `acp_mcp_sse.js` + `sse_mcp_server.js` | SSE MCP |
| `acp_fs.js` | 客户端虚拟文件系统读写 |

运行前提：本机已配置 provider 并设置 `TOKENRHYTHM_API_KEY`（或对应环境变量），
构建 `bin/mocode` 后执行，例如：

```bash
node scripts/acceptance/acp_acceptance.js ./bin/mocode /tmp/acp-work
node scripts/acceptance/acp_mcp_http.js ./bin/mocode /tmp/acp-work 18765
```

`acp_acceptance.js` 不使用 `--yolo`，因此权限请求会真实经过 `session/request_permission`。
