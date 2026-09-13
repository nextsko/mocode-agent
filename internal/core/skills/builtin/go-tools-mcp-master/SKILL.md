---
name: go-tools-mcp-master
description: >-
  Use when 开发或审查 Go MCP Server（基于 github.com/mark3labs/mcp-go v0.32.0），或为 go-tools 仓库 7 个 MCP 项目做技术选型：NewMCPServer 与 ServerOption、stdio/SSE/StreamableHTTP 三种 transport、工厂列表注册表、NewTool + WithXxx + Required/Enum schema、CallToolRequest 提参辅助（optInt 必须兼容 float64）、textResult/errResult 约定、--book 自描述，以及路径沙箱（AbsResolve/EvalSymlinks/isInside）、命令白名单 + 参数黑名单、破坏性命令正则、SQL 只读、危险端点 deny-list、SSRF DialContext 拦截、fail-closed、原子写入与按 rune 截断。
---

# go-tools MCP 开发指南

**源仓库**：`go-tools`（7 个 Go MCP 服务）。**核心库**：`github.com/mark3labs/mcp-go v0.32.0`。**统一入口**：`--version / --book / --help / 无参数 serve`。

## 何时用

- 新建 Go MCP Server、查 mcp-go API、学习安全 MCP 设计模式（路径沙箱 / 命令白名单 / 破坏性命令拦截）。
- 参考 7 个项目做技术选型，或编写工具的参数校验与安全防护。

## 统一架构

```
cmd/<service>/main.go        入口：CLI + server 启动
internal/tools/              工具注册 + handler
internal/<domain>/           领域逻辑（纯函数优先）
internal/security/           安全校验（纯函数，零外部依赖）
```

依赖单向向下；handler 保持薄：提参 → 校验 → 调纯函数 → 格式化。可测逻辑全下沉纯函数。

## Server 与 Transport

```go
s := server.NewMCPServer("xxx-mcp", Version,
    server.WithToolCapabilities(false), // 关闭 listChanged 通知
    server.WithInstructions("优先调用高频工具；写操作要求绝对路径。"),
    server.WithRecovery(),              // 建议默认开启，防 handler panic
)
```

- **stdio（默认）**：`server.ServeStdio(s).Error()`；**业务日志必须写 stderr**，否则污染协议流；`--version/--book/--help` 必须在 ServeStdio 前 return。
- **SSE**：`server.NewSSEServer(s, WithSSEEndpoint("/sse"), WithMessageEndpoint("/message")).Start(":8080")`，配合 `signal.NotifyContext` + `Shutdown`。
- **StreamableHTTP**：`server.NewStreamableHTTPServer(s, WithEndpointPath("/mcp"))`，单端点，与 SSE 二选一。

## 工具注册：工厂列表

`All()` 同时服务协议注册与 `--book` 自描述；新增工具 = 写工厂 + 列表加一行。

```go
type Item struct { Tool mcp.Tool; Handler server.ToolHandlerFunc }

func All() []Item {
    factories := []func() (mcp.Tool, server.ToolHandlerFunc){SearchTool, ReadTool}
    items := make([]Item, 0, len(factories))
    for _, f := range factories { t, h := f(); items = append(items, Item{t, h}) }
    return items
}

func RegisterAll(s *server.MCPServer) {
    for _, it := range All() { s.AddTool(it.Tool, it.Handler) }
}
```

其他注册 API：`AddTools(...)`、`SetTools(...)`（覆盖）、`DeleteTools(names...)`。

## Tool 定义

```go
tool := mcp.NewTool("search_docs",
    mcp.WithDescription("全文检索笔记。\n输入：query... 输出：..."),
    mcp.WithString("query", mcp.Required(), mcp.Description("关键词"), mcp.MinLength(1)),
    mcp.WithNumber("limit", mcp.Description("上限"), mcp.DefaultNumber(10), mcp.Min(1), mcp.Max(50)),
    mcp.WithString("scope", mcp.Enum("title", "body", "all"), mcp.DefaultString("all")),
)
```

`PropertyOption`：`Required / Description / Enum / DefaultString|Number|Bool / MinLength|MaxLength / Pattern / Min|Max / Items`，以及 `Title / ReadOnlyHint / DestructiveHint / IdempotentHint / OpenWorldHint`。

关键点：

- `Required()` 是把属性名追加进 `InputSchema.Required`，必须写在对应 `WithXxx` 内；**服务端不强制**，运行时仍需 `if s == ""` 兜底。
- `Enum` 只是协议层校验，客户端良莠不齐，运行时仍需自检。
- 工具名 / 属性名发布后不要改名（改名 = 删除）；新字段先加 Optional + Default。
- `Description` 写清四件事：做什么 / 输入格式 / 输出格式 / 何时调用。

## 请求与结果

```go
func handler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    name, _ := req.GetString("name", "")
    port, _ := req.GetInt("port", 0) // 兼容 JSON 数字
    return textResult("✅ ..."), nil
}
```

- **业务错误** → `errResult("❌ ..."), nil`（让 LLM 看见并自我纠正）；**协议级错误** → `nil, fmt.Errorf(...)`（找不到工具、DB 失败）。
- 结果构造：`mcp.NewToolResultText` / `NewToolResultError(f)` / `NewToolResultImage(data, mime)`。

### 提参辅助（统一放 `internal/tools/args.go`）

```go
optString(req, "key")       // 缺省 ""
optBool(req, "key")         // 缺省 false
optInt(req, "key")          // 兼容 float64 / int / json.Number
optStringSlice(req, "key")  // 兼容 []string / []any / 单字符串
hasKey(req, "key")          // 区分"未传"与"传了 false/0"
```

**`optInt` 必须兼容 `float64`**：JSON 数字一律先解析为 float64，`args[k].(int)` 必然 panic。用 `hasKey` 区分未传与显式默认值，勿在 handler 内手写类型断言。

### context 注入有状态对象

```go
type serverKey struct{}
func WithServer(ctx context.Context, srv *Server) context.Context {
    return context.WithValue(ctx, serverKey{}, srv)
}
// 注册时包装注入；handler 内 srv, ok := ctx.Value(serverKey{}).(*Server)
```

## --book 自描述

`--book` 输出 JSON（`name / version / configTemplate / tools / notes / mcp`），Agent 读后自动生成 mcp.json。`configTemplate` 里敏感字段留空并标注用环境变量。

## 安全清单（6 类）

### 命令执行

- 只用 `exec.CommandContext(ctx, exe, args...)`（参数数组），**绝不** `sh -c` / `cmd /c` 拼串。
- 命令白名单（可枚举）；解释器参数黑名单：`node {-e,--eval}`、`python {-c,--command}`、`ruby -e`、`sh -c`。
- 破坏性命令正则拦截（宁可误报）：`rm -rf /` 关键目录、`mkfs`、`dd if=`、`shutdown|reboot|halt|poweroff`、fork bomb、`curl|sh`、`chmod 777 /`。

### 路径沙箱（三层）

1. 段级过滤：拒绝 `..` 和 `.git`。
2. `AbsResolve` = `filepath.Abs` + `filepath.EvalSymlinks`（文件不存在时回退解析父目录）。
3. `isInside(base, path)`：**必须** `strings.HasPrefix(path, base+os.PathSeparator)`，否则 `/foo` 误判含 `/foobar`；Windows 比较前 `ToLower` 归一化。

读写不对称：读宽松（段过滤 + 前缀检查，允许符号链接）；写 / 删严格（canonicalize + EvalSymlinks + isInside）。Windows 超长路径加 / 剥 `\\?\` 前缀需配对。

### SQL / 数据

- 强制只读：必须 `SELECT`/`WITH` 开头，且不得含 `INSERT/UPDATE/DELETE/DROP/ALTER/CREATE/TRUNCATE/ATTACH/PRAGMA/VACUUM/...`（词边界匹配）。
- 参数化查询，禁止字符串拼接。
- 透传内核 API 时用危险端点 deny-list（如 `removeNotebook`、`forwardProxy`）。

### 网络

- SSRF 在 `http.Transport.DialContext` 拨号时拦截（防 DNS rebinding），**不是**请求前查 host。
- 内网 IP 黑名单：RFC 1918、`169.254/16`、loopback、`fc00::/7`、multicast、unspecified；重定向每跳重查；响应体 `io.LimitReader` 限流。
- 密钥走环境变量，不写 config 文件。

### 并发 / 资源

- 并发限制的检查 + 创建 + 置位放同一锁临界区（防 TOCTOU）。
- 后台任务用 `context.Background()`，不用请求 ctx。
- 长输出按 rune 截断（避免半字符）；超时 0 表示无超时但有 24h 兜底。

### 信息泄露

- 错误严格脱敏，不回显内部绝对路径 / 用户名 / token；拒绝执行时只说"不允许执行"，不列出完整白名单（防探测）。

## 决策：allow-list vs deny-list

| 场景 | 模型 | 原因 |
|------|------|------|
| 命令 | allow-list | 可枚举、变化少 |
| SSH 主机 | allow-list（fail-closed） | 空配置即拒绝全部，漏放代价大 |
| 解释器参数 | deny-list（按命令） | 参数空间无限，危险参数可枚举 |
| 内核 API 端点 | deny-list | 新端点默认可用，只禁已知危险 |
| 破坏性命令 | deny-list（正则） | 任意 shell 无法枚举 |
| 内网 IP | deny-list | 保留段已知 |

fail-closed 是安全默认：用户忘配时拒绝而非放行。

## 可复用设计模式

| 模式 | 一句话 | 参考项目 |
|------|--------|---------|
| 工厂列表 | 加工具 = 工厂 + 列表一行 | 全部 |
| ctx 注入 Server | 空结构体 key 注入有状态对象 | obsidian / siyuan |
| 异步索引 + ready | 先 serve 后索引，`EnsureReady` 阻塞等待 | obsidian |
| 读写不对称 | 读宽松写严格，写时才 EvalSymlinks | obsidian |
| `--book` 自描述 | server 自产 JSON 交 Agent 注册 | 全部 |
| 冷热工具分层 | Description 引导优先选高频工具 | siyuan |
| 后台任务三件套 | submit + status + output + context.Background | ssh-mcp |
| 三级降级管线 | direct → compress → chunk | vision-mcp |
| 接口驱动解耦 | core 定 interface，外围实现注入 | vision-mcp |
| 多层暂存区保护 | smart_commit 多层校验 + 失败 halt | git-commit-mcp |
| exec 安全回退 | 纯库优先，exec 参数数组兜底 | git-commit-mcp |

## 可靠性要点

- 读懂第三方 CLI 退出码语义（如 es.exe 退出码 1 = 无结果，属合法，主动吞掉）。
- 原子写入：写 `.tmp` 再 `os.Rename`（同文件系统内原子），失败清理 tmp。
- 容错不 panic（如编码转码三层兜底：UTF-8 → GBK → 原样返回，不丢数据）。
- 集成测试用 `t.Skipf` 优雅跳过缺失依赖；配置三优先级 defaults < YAML < env < CLI flag，找不到配置用内置默认兜底。

## 相关技能

- `security-and-hardening`：通用安全基线与审查清单，与本页安全清单互补。
- `rig-core-llm-integration`：MCP Server 内的 LLM 调用与接口驱动解耦（role/format 白名单、统一 JSON envelope）。
- `observability`：为 server 增加 tracing / 日志。
