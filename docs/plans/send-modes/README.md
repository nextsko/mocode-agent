# send-modes — 发送三模式（排队 / 引导注入 / 强制发送）

> 状态：已完成（2026-09-13）
> 创建：2026-09-13
> 范围：`core/agent/lifecycle`（inject.go / dispatcher）、coordinator、workspace、ui/model（三键位）

## 语义

| 模式 | 键位 | 行为 | 空闲时 |
|------|------|------|--------|
| queue（排队） | `enter` | FIFO 入队，当前 turn 结束后串行执行（既有默认语义） | 立即执行 |
| inject（引导） | `ctrl+enter` | 持久化为 user 消息 + 缓冲；`prepareStep` 在模型**下一步**前置 `<user_guidance>` system——运行中即时转向，不占队列、不触发新 turn | 立即执行 |
| force（强制） | `alt+enter` | 打断当前 turn（`ErrForceKick` cancel-cause）+ 插队首；调度器无缝续跑强制消息（区别于 Esc 全停） | 立即执行 |

## 关键设计

1. **inject 的双写闭环**：Inject 时持久化（transcript 完整）+ prepareStep drain 前置（模型即时可见）。此前"step 注入"因默认注入竞态被废（commit 16c3492）——本设计以显式模式回归：csync.Map 原子 Update（新增方法）swap-drain，无竞态窗口。
2. **force 的 cancel-cause 区分**：`context.WithCancelCause` + `ErrForceKick`；dispatcher 的 err 分支检测 cause——kick 则转 end-of-turn 语义（pop 队首续跑），Esc 取消（cause=nil）保持全停。Summarize 的 cancel 也统一 cause 化。
3. 链路：`SessionAgent`/`Coordinator`/`Workspace` 接口各 +2（`Inject`/`ForceRun`），UI 三键位 + toast 反馈 + keymap help。

## 测试（lifecycle/inject_test.go）

- Inject 持久化 + 缓冲 drain 恰好一次；空参校验
- ForceRun 空闲直跑；忙碌打断续跑（scriptedModel gate 验证 URGENT 消息执行 + 会话终态空闲）

## 已知边界

- inject 不带附件（纯文本引导）；force 带附件
- wechat 等远程会话未接三模式（走默认 queue）
