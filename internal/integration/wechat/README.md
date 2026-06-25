# Mocode WeChat 总管系统

## 架构

```
微信消息
    │
    ▼
┌─────────────────────┐
│  Slash 命令拦截层    │  /status, /list, /models, /help...
│  (绕过 agent)        │  → 直接执行，直接回复
└─────────┬───────────┘
          │ 非 slash 消息
          ▼
┌─────────────────────┐
│  消息去重 + 媒体下载 │  SHA-256 去重 / AES 解密 / 存盘
└─────────┬───────────┘
          │
          ▼
┌─────────────────────┐
│  总管 Agent          │  理解意图，决定路由
│  (butler session)    │  → session_send / session_create
└─────────┬───────────┘
          │
    ┌─────┼─────┬─────┐
    ▼     ▼     ▼     ▼
  SessA  SessB  SessC  ...
  (各自独立运行，完成后通知)
```

## Slash 命令（绕过 Agent，直接执行）

| 命令 | 说明 |
|------|------|
| `/help` | 帮助手册 |
| `/status` | 系统状态（内存/协程/模型/会话数） |
| `/list` | 会话列表 |
| `/models` | 可用模型列表 |
| `/model <provider/model>` | 切换模型 |
| `/test model <provider/model>` | 测试模型连通性 |
| `/screenshot` | 截图并发送到微信 |
| `/send <path>` | 发送文件到微信（>5MB 自动压缩） |

## 总管工具

| 工具 | 说明 |
|------|------|
| `session_list` | 列出所有会话 |
| `session_create` | 创建新会话（指定工作目录） |
| `session_switch` | 切换活跃会话 |
| `session_status` | 查看会话详细状态 |
| `session_send` | 向指定会话发送任务（异步） |
| `session_summary` | 获取会话摘要 |
| `session_delete` | 删除会话 |

## 核心组件

| 文件 | 职责 |
|------|------|
| `channel.go` | WeChat 频道（登录/消息收发/媒体下载/去重） |
| `slash.go` | Slash 命令拦截层 |
| `butler.go` | 总管工具集 |
| `butler_prompt.go` | 总管 system prompt |
| `router.go` | 意图识别路由 |
| `session_manager.go` | 多会话管理（per-session workdir） |
| `task_queue.go` | 异步任务队列（并行执行 + 状态追踪） |
| `media_store.go` | 媒体文件生命周期管理 |
| `manager.go` | 多账号管理 |

## 使用流程

### 1. 登录
```bash
# 通过 TUI 或 Admin 面板登录微信
# 或通过 Gateway 模式自动登录
```

### 2. 使用 Slash 命令
```
/help          → 查看帮助
/status        → 查看系统状态
/list          → 查看会话列表
/models        → 查看可用模型
/model xxx     → 切换模型
```

### 3. 通过总管管理会话
```
"帮我看看 neuron 那个项目"  → 总管路由到 neuron 会话
"在 school 目录新建会话"    → 总管创建新会话
"所有会话状态怎么样"        → 总管聚合汇报
```

### 4. 发送文件/截图
```
/screenshot               → 截图发送
/send C:/path/to/file     → 文件发送（大文件自动压缩）
```

## 配置注入

Channel 的 `SlashConfig` 需要由 Gateway/Admin 层注入：

```go
ch.slashCfg = wechat.SlashConfig{
    CurrentModel: func() string { return "xiaomi-coding/mimo-v2.5-pro" },
    SmallModel:   func() string { return "kimi-coding/kimi-for-coding" },
    ListModels:   func() []string { /* 返回所有模型 */ },
    SwitchModel:  func(provider, model string) error { /* 切换模型 */ },
    TestModel:    func(provider, model string) error { /* 测试连通性 */ },
}
```
