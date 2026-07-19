# WeChat Butler 架构

## 文档说明

本文记录 Mocode 中 WeChat 集成的完整实现链路。当前 WeChat 功能基于微信 iLink Bot 平台，实现为一个“总管（Butler）系统”，覆盖从协议层到 Agent 执行层的全链路。

---

## 1. 入口层：Gateway Runtime

**文件**：`internal/integration/wechat/gateway/runtime.go`

启动流程：

1. `NewWeChatGateway(opts)` 创建网关，持有 `wechat.Default()` 单例 `Channel`。
2. `Start(ctx)` 执行：
   - 设置 session 持久化路径；
   - 注入 `SlashConfig`（模型查询/切换/测试回调）；
   - 初始化 Butler：`g.wc.InitButler(&gatewayButlerWorkspace{g.ws})`；
   - 注册 `SetAgentHandler`：处理非 slash 消息的核心 handler；
   - 未登录时走 QR 扫码登录；
   - 调用 `g.wc.Run(ctx)` 进入长轮询。

**关键接口**：`gatewayButlerWorkspace` 实现了 `ButlerWorkspace` 接口，把真实的 `workspace.Workspace` 方法桥接给 WeChat 层，避免 WeChat 包反向依赖 transport。

---

## 2. 协议层：iLink Bot SDK

**目录**：`internal/integration/wechat/sdk/`

核心文件：

- `sdk/bot.go`：Bot 客户端。
- `sdk/types.go`：消息类型定义。
- `sdk/internal/protocol/api.go`：HTTP API 调用。
- `sdk/internal/crypto/`：AES-128-ECB 加解密，用于媒体文件。
- `sdk/internal/auth/login.go`：QR 登录流程。

主要 API 端点：

| 端点 | 用途 |
|------|------|
| `https://ilinkai.weixin.qq.com/ilink/bot/get_bot_qrcode` | 获取二维码 |
| `.../get_qrcode_status` | 轮询扫码状态 |
| `.../getupdates` | 长轮询拉取消息 |
| `.../sendmessage` | 发送消息 |
| `.../getconfig` | 获取 typing_ticket |
| `.../sendtyping` | 输入状态指示 |
| `.../getuploadurl` | 获取上传 URL |
| CDN `download/upload` | 媒体文件下载/上传 |

媒体加密流程：

- **上传**：原始数据 → AES-128-ECB 加密 → 上传 CDN → 返回 `encrypted_query_param`。
- **下载**：通过 `encrypted_query_param` 下载密文 → AES-128-ECB 解密 → 原始文件。
- 图片/视频/文件/语音都有对应的 `MediaType` 与消息结构。

---

## 3. 频道层：Channel

**文件**：`internal/integration/wechat/channel.go`

`Channel` 是核心编排层，负责：

- Bot 生命周期（登录/运行/停止）；
- 用户消息去重（SHA-256，24 小时窗口）；
- 媒体下载与本地存储；
- Slash 命令拦截；
- Butler/Agent handler 分发；
- 用户级锁（防止并发消息乱序）；
- 输入状态指示器（typing keepalive，每 5 秒发送一次）。

`handleMessage` 的主要步骤：

1. 获取用户级锁；
2. Slash 命令拦截，命中则直接回复，绕过 Agent；
3. 去重检查；
4. 下载消息中的媒体附件；
5. 提取引用消息文本；
6. 构建 `effectiveText`，包含原始文本、媒体路径和引用文本；
7. 优先走 Butler 路由；若未初始化则回退到 `agentFn`；
8. 回复发送，失败时重试 3 次。

---

## 4. 路由层：Slash 命令

**文件**：`internal/integration/wechat/slash.go`

Slash 命令完全绕过 Agent，直接操作 Channel 或 Workspace：

| 命令 | 说明 |
|------|------|
| `/help` | 帮助手册 |
| `/status` | 内存/协程/模型/会话数 |
| `/list` | 绑定会话列表 |
| `/models` | 可用模型列表 |
| `/model <provider/model>` | 切换大模型 |
| `/test model <provider/model>` | 测试连通性 |
| `/screenshot` | 桌面截图并发送 |
| `/send <path>` | 文件发送，大于 5MB 自动 gzip 压缩 |

---

## 5. 路由层：Butler（总管）

**文件**：`internal/integration/wechat/butler.go`、`butler_prompt.go`

Butler 是一个 LLM 驱动的会话路由器，每个微信用户有独立的 Butler session。

工作流程：

1. 确保 Butler session 已初始化；首次使用需注入 system prompt。
2. 构建 prompt：
   - 列出当前可用会话（ID/标题/创建时间）；
   - 注入用户当前绑定会话 ID；
   - 附加用户消息文本。
3. 调用 `Workspace.AgentRun(ctx, sessID, prompt)`。
4. 轮询 `ListMessages` 等待新的 assistant 回复。
5. 超时时间设置为 5 分钟。

Butler system prompt 的核心约定：

- 身份：用户在微信上的唯一 AI 接口；
- 能力：可调用 bash、view、write、grep、glob、agent、think、web_fetch 等工具；
- 行为：简洁、中文、主动判断会话归属；
- 会话管理命令：`/new`、`/switch`、`/delete`。

---

## 6. 会话管理层

**文件**：`internal/integration/wechat/session_manager.go`、`session_bind.go`

存在两套 session 机制：

| 机制 | 文件 | 用途 |
|------|------|------|
| WeChat Channel 绑定 | `channel.go` + `session_bind.go` | 持久化 userID 到 mocode sessionID 的映射 |
| SessionManager | `session_manager.go` | 多用户 Butler 会话管理，支持异步任务队列 |

`SessionManager` 的职责：

- 每个用户维护一个 `WeChatSession`，包含 WorkDir、MocodeID、状态等；
- `SubmitTask` 将任务提交到 `TaskQueue` 异步执行；
- `executeTask` 调用真实的 `Workspace.AgentRun`；
- `onTaskComplete` 向微信用户发送完成通知。

`session_bind.go` 维护一个以 `wx:<userID>` 为 key 的绑定表，支持：

- `GetSession` / `SetSession` / `DelSession`；
- `ClearBindingsForSessionID`：删除某个 session 对应的所有用户绑定；
- 持久化到 `sessions.json`。

---

## 7. 异步任务层：TaskQueue

**文件**：`internal/integration/wechat/task_queue.go`

- 每个任务默认 30 分钟超时；
- 支持 Cancel；
- 完成后调用 `TaskNotifier`；
- 状态流转：`pending` → `running` → `completed` / `failed` / `cancelled`。

---

## 8. 多账号管理层：AccountManager

**文件**：`internal/integration/wechat/manager.go`

支持多微信账号，核心能力：

- 每个账号对应一个 `Channel`；
- 默认账号切换；
- 持久化 registry 与 credentials；
- 提供 Start/Stop/Reconnect/Delete 账号能力；
- 账号数据目录位于 `infra.WeChatDir()`。

---

## 9. 媒体管理：MediaStore

**文件**：`internal/integration/wechat/media_store.go`

- 支持 scope 的媒体文件生命周期管理；
- 自动清理过期文件，默认 24 小时；
- scope 分为 `inbound` 和 `outbound`；
- 提供 Store/Resolve/ReleaseAll/CleanExpired 接口。

---

## 10. 核心服务层对接

**文件**：`internal/domain/messenger/messenger.go`、`internal/integration/wechat/messenger_adapter.go`

依赖方向：

```
core/agent/tools/plugins → domain/messenger（port）
                            ↑
integration/wechat → messenger_adapter.go（adapter）
```

`messenger_adapter.go` 将 `AccountManager` / `Channel` 适配为 `messenger.Messenger` 接口，核心代码只依赖接口，不直接 import WeChat 包。

---

## 11. 完整消息链路

```
用户微信消息
    ↓
iLink API 长轮询 (sdk/bot.go → protocol/api.go)
    ↓
Bot.parseMessage → IncomingMessage
    ↓
Channel.OnMessage 回调
    ↓
Channel.handleMessage:
  ├─ Slash 命令? → 直接回复
  ├─ 去重?
  ├─ 下载媒体?
  └─ Butler / agentFn
        ↓
  Butler.Handle:
    ├─ ensureSession (创建 Butler session)
    ├─ 注入 system prompt
    ├─ buildUserPrompt (会话列表 + 绑定信息 + 用户消息)
    └─ Workspace.AgentRun(ctx, sessID, prompt)
          ↓
      core/agent/coordinator.Run → LLM 调用
          ↓
      waitForAssistantReply (轮询 ListMessages)
          ↓
      回复文本 → Channel.replyWithRetry → Bot.sendText → iLink API
          ↓
      微信用户收到回复
```

---

## 12. 关键设计模式

1. **单例 Channel**：`Default()` 提供全局单例，Gateway 模式使用。
2. **依赖注入**：`SlashConfig`、`ButlerWorkspace` 由上层注入，避免循环依赖。
3. **用户级锁**：`userLocks sync.Map` 保证同一用户消息串行处理。
4. **上下文 token 管理**：Bot 内部维护 `contextTokens`，回复时复用。
5. **去重 + 持久化**：message hash 持久化到文件，重启不丢。
6. **Typing 保活**：`StartTyping` 每 5 秒发送一次 typing 指示。
