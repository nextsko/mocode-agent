# 01 — 核心修正实施计划（P1/P3/P4/P5/P7 + P2 设计）

> 状态：执行中
> 创建：2026-09-13
> 范围/目标：落地分析报告 ROI 前六项；P2（完成推送）仅出设计
> 依赖：无（首批）
> 证据：`tmp/00-后台任务机制对比分析.md` §3 吸收清单
> 触达模块：`shell/background.go`、`shell/background_test.go`、`tools/core/shell/bash.go`、`tools/core/shell/job_{output,kill,input}.go`

## P3 · waitLoop 事件驱动（吸收：自查/codex 事件模型）

`bash.go` 同步等待循环现为 100ms ticker 轮询 `IsDone()`。改：

1. `BackgroundShell` 新增 `Done() <-chan struct{}`（只读暴露 done）。
2. waitLoop 的 select 分支改 `case <-bgShell.Done(): break waitLoop`，删除 ticker。

验收 A2。

## P1 · syncBuffer → head-tail 有界缓冲（吸收：codex HeadTailBuffer）

`background.go` 的 `syncBuffer` 内部重写为 head+tail 双段：

- 常量 `backgroundOutputCapBytes = 1 << 20`（1 MiB），head 预算 50%、tail 环形缓冲 50%。
- `Write/WriteString` 契约不变（返回 len(p)），溢出仅丢中间并累计 `omitted`。
- `String()` = head + `\n... [N bytes omitted] ...\n` + tail（无省略时无标记）。
- `Len()` 语义 = 观测总字节数（供 truncated 判断、StdoutBytes 状态）。
- `Tail(n)` 只在 tail 段内取，保留行对齐；不足 n 返回全部 tail。
- `IdleMs`/`lastWriteMs` 保留。

新增测试：上限、omission 标记、Tail 对齐、total 计数。验收 A1。

## P5 · 输出净化 env（吸收：codex UNIFIED_EXEC_ENV）

`BackgroundShellManager.Start` 中对后台 shell 注入（经 `shell.SetEnv` 覆盖）：
`NO_COLOR=1`、`TERM=dumb`、`PAGER=cat`、`GIT_PAGER=cat`、`GH_PAGER=cat`。
仅后台 job 注入（同步命令行为保持现状，避免扩大影响面）。验收 A4。

## P7 · auto_background_after 收敛 30s（吸收：codex yield clamp）

- `DefaultAutoBackgroundAfter` 60 → 30。
- 新增 `MaxAutoBackgroundAfter = 30`，对参数 clamp；参数 description 同步改为 "(default: 30, max: 30)"。
- 同步路径最长阻塞 agent turn 从 60s 降为 30s。验收 A5。

## P4 · session 归属（吸收：gemini-cli per-session ownership）

- `BackgroundShellOptions` 增加 `SessionID string`；`BackgroundShell` 记录之。
- `bash.go` 两处 `Start` 传 `sessionID`（已强制非空）。
- `job_output/job_kill/job_input`：取 `toolutil.GetSessionFromContext(ctx)`，与 job 的 SessionID 不符时按 `background shell not found` 拒绝（不泄露存在性）。
- UI（`ui_background.go`）只读展示，暂不校验。验收 A3。

## P2 · 完成推送（本轮只设计，不实现）

吸收 opencode `injectBackgroundResult` + Claude Code task-notification：

1. `BackgroundShell` 增加 `OnComplete func(JobStatus)` 回调（Start 时注入）或 manager 级订阅 channel。
2. app 层订阅：job 终态时生成摘要（ID、command、exit code、tail 输出前 2 KiB），折叠规则 MAX_VISIBLE=3（超出合并为 "+N more"）。
3. 注入通道：复用 app_message_debounce 或 agent 的下一轮 system 注入点（具体落点实施时定）。
4. bash 工具描述改为「完成后会自动收到通知，勿轮询 job_output」。

风险：注入点耦合 agent loop；需防重放（终态只通知一次）。

## 验证

```powershell
go build ./...
go vet ./internal/core/shellruntime/shell/... ./internal/core/tools/core/shell/...
go test ./internal/core/shellruntime/shell/... ./internal/core/tools/core/shell/...
```

Windows 下 TTY 用例自动跳过（build tag `//go:build linux`）。
