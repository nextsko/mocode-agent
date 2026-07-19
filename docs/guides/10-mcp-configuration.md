# MCP 配置指南

> 整理时间：2026-07-20  
> 范围：如何查看当前已配置的 MCP、如何新增/修改、以及如何接入 Z.ai 的远程 MCP。

---

## 1. 当前配置了什么 MCP

当前 MCP 配置位于 `mocode.json` 的顶层 `mcp` 字段。  
你当前的 `mocode.json` 中**没有配置任何 MCP server**，`mcp` 字段不存在。

查看方法：

```bash
# 方式 1：直接查看配置文件
cat mocode.json | jq '.mcp'

# 方式 2：在 TUI 中查看
/mcps
```

`/mcps` 会打开 MCP 管理面板，列出已配置的 server、连接状态、工具数。

---

## 2. mocode 支持的 MCP 连接类型

`mocode-schema.json` 定义了 3 种类型，全部在 `mcp.<name>` 下配置：

| 类型 | 必填字段 | 示例用途 |
|---|---|---|
| `stdio` | `command`, `args` | 本地进程，如 `npx -y @modelcontextprotocol/server-filesystem` |
| `sse` | `url` | Server-Sent Events，如某些远程 MCP |
| `http` | `url` | HTTP/JSON-RPC，如 Z.ai 远程 MCP |

通用可选字段：`env`、`disabled`、`disabled_tools`、`timeout`、`headers`。

---

## 3. 配置 Z.ai 的 MCP

Z.ai 提供的是一个**远程 HTTP MCP**，无需本地安装 npm 包。

### 3.1 环境变量

需要 `ZAI_API_KEY`：

```bash
# Windows PowerShell
$env:ZAI_API_KEY = "你的Z.ai API Key"

# 或写入系统环境变量后重启终端
setx ZAI_API_KEY "你的Z.ai API Key"
```

也可以把 key 直接写在 `mcp` 配置的 `env` 里，见下方示例。

### 3.2 在 mocode.json 中添加 Z.ai MCP

在 `mocode.json` 的顶层添加 `mcp` 字段：

```json
{
  "mcp": {
    "zai-web-search": {
      "type": "http",
      "url": "https://api.z.ai/api/mcp/web_search_prime/mcp",
      "env": {
        "ZAI_API_KEY": "你的Z.ai API Key"
      },
      "disabled_tools": [],
      "timeout": 30
    },
    "web-search-prime": {
      "type": "http",
      "url": "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp",
      "headers": {
        "Authorization": "Bearer <YOUR_BIGMODEL_API_TOKEN>"
      }
    }
  }
}
```

字段说明：

- `type: "http"`：使用 JSON-RPC over HTTP
- `url`：Z.ai 官方提供的 MCP 端点
- `env.ZAI_API_KEY`：认证密钥；也可以不写在这里，依赖系统环境变量
- `disabled_tools`：如需禁用某个 Z.ai 工具，把工具名加入数组
- `timeout`：连接超时秒数，默认 15，建议远程服务调大到 30
- `headers.Authorization`：备用认证头，格式 `Bearer <token>`。**强烈建议优先用 `env` / 系统环境变量承载 token，不要把真实 API Key 直接写入配置文件并提交到仓库**。若曾在文档中以明文示例贴出过你的 token，请立即到 Z.ai 控制台轮换（rotate）。

### 3.3 重载 MCP

配置写入后，在 TUI 中执行：

```
/reload-mcp
```

或只重载 Z.ai：

```
/reload-mcp zai-web-search
```

重载后，Z.ai 的工具会以 `mcp_` 前缀出现在可用工具列表中。

---

## 4. 验证是否生效

1. 在 TUI 输入 `/mcps`，确认 `zai-web-search` 状态为 connected
2. 让模型调用 `web_search_prime`（或带 `mcp_` 前缀的工具名）
3. 如返回搜索结果，说明链路正常

如果失败，检查：

- `ZAI_API_KEY` 是否正确且未过期
- 网络能否访问 `api.z.ai`
- `/reload-mcp` 后的错误提示

---

## 5. 安全提示

- **不要把 `mocode.json` 提交到公开仓库**，里面可能包含 API Key
- 建议优先使用系统环境变量 `ZAI_API_KEY`，而非写在配置文件里
- 如需在团队共享配置，使用 `env` 注入或 `mocoderc` / 项目级 `.mocode.json` 作用域
