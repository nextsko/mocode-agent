# bg-jobs-fix — 后台任务执行机制修正

> 状态：进行中
> 创建：2026-09-13
> 范围：`internal/core/shellruntime/shell/background.go`、`internal/core/tools/core/shell/{bash,job_output,job_kill,job_input}.go`
> 依据：`C:\Users\16143\projects\self\tmp\00-后台任务机制对比分析.md`（对比 codex / opencode / gemini-cli / crush / Claude Code）

## 目标

修正 mocode 后台任务机制的已确认缺陷（P1–P10，见分析报告），使内存有硬上限、等待事件驱动、任务有会话归属、输出被净化。

## 量化验收

| # | 验收项 | 证据 |
|---|--------|------|
| A1 | 单个后台 job 输出缓冲内存 ≤ 1 MiB（head+tail 各半，中间丢弃有 omission 标记） | 新增单测：写 2 MiB 后 retained ≤ 1 MiB、String 含 omission 标记、Tail 行对齐 |
| A2 | bash 同步等待路径事件驱动（`select <-done`），无 ticker 轮询 | 代码审阅 + 现有测试回归 |
| A3 | 后台 job 记录 SessionID；job_output/job_kill/job_input 跨 session 访问被拒 | 新增单测：非属主 session 操作返回 not found |
| A4 | 后台 job 环境强制 `NO_COLOR=1 TERM=dumb PAGER=cat GIT_PAGER=cat GH_PAGER=cat` | 单测：Start 后 shell.GetEnv 含净化变量 |
| A5 | auto_background_after 默认与上限均为 30s | 单测/常量断言 |
| A6 | `go build ./...`、`go vet`（涉改包）、涉改包 `go test` 全绿 | 命令输出 |

## 进度

- [x] 01-core-fixes-plan.md（本轮实施计划，含 P2 完成推送的设计草稿）
- [x] 代码修正：P3 waitLoop 事件驱动
- [x] 代码修正：P1 head-tail buffer
- [x] 代码修正：P5 env 净化
- [x] 代码修正：P7 clamp 30s
- [x] 代码修正：P4 session 归属
- [x] 验证（A6）：`go build ./...` + vet + 两包 `go test` 全绿（2026-09-13）
- [x] 顺手修复预存失败：`TestBackgroundShell_GetTailOutput` 依赖 Windows 缺失的 `seq`，改纯 bash 内置循环
- [x] P2 完成推送（第一批，2026-09-13）：通知管道 + UI 系统通知 + 待注入队列 API；模型侧 turn 注入待接
- [x] P9 KillAll 分级：复用 grace→force 升级（`terminateAndWait`）
- [x] P2 完成推送（第二批，2026-09-13）：`coordinator.Run` 注入 `<background_jobs_completed>` 块（MAX_VISIBLE=3 折叠，`agent/background_notify.go`）
- [x] 失控守护：单流总产出 ≥2 GiB 自动 cancel（`OutputCapped` 状态上报，测试可经 `SetOutputKillBytes` 缩小阈值）
- [x] 总开关：`MO_CODE_DISABLE_BACKGROUND_TASKS=1` 禁止显式后台 + 同步路径永不转后台
- [x] P5 输出落盘：`SetOutputDir`（app 启动接 `<data>/background-jobs/`），stdout/stderr tee 到 `<unixts>-<id>.{out,err}`，保留期清理随 Cleanup

## 剩余

- ~~P6 快速失败检测~~（2026-09-13 已完成：RunInBackground 显式 2s yield 窗口替代 sleep(250ms)，见 subagent-ui 主题）
- ~~通知分级~~（2026-09-13 已完成：`MOCODE_BACKGROUND_NOTIFICATIONS=all|result|error`）
- ~~ID 换 ULID~~（2026-09-13 已完成：纪元前缀 `<base36分钟>-<hex>`，重启零复用）

## 实施记录（2026-09-13）

| 项 | 文件 | 变更 |
|----|------|------|
| P1 | `internal/core/shellruntime/shell/background.go` | `syncBuffer` 重写为 1 MiB head+tail 有界缓冲（零值可用），`Len()`=观测总量，`Tail()` 无省略时跨 head+tail 取尾 |
| P3 | 同上 + `tools/core/shell/bash.go` | 新增 `Done() <-chan struct{}`；waitLoop 改 `select <-Done()`，删 100ms ticker |
| P4 | 同上 + `job_{output,kill,input}.go` | `BackgroundShellOptions.SessionID` + `BelongsTo()`；三工具跨 session 访问按 not found 拒绝 |
| P5 | `background.go` | 非 TTY 后台 job 注入 `NO_COLOR/TERM=dumb/PAGER/GIT_PAGER/GH_PAGER`（TTY 豁免） |
| P7 | `bash.go` | `DefaultAutoBackgroundAfter` 60→30，新增 `MaxAutoBackgroundAfter=30` clamp，工具描述同步 |
| 测试 | `background_fix_test.go`（新增） | A1 有界性/omission/行对齐、A3 归属、A4 净化 env、Done 通道 |
| P2 一期 | `notify/notify.go`、`background.go`、`app.go`、`ui_agent_control.go`、`bash.tpl` | `TypeBackgroundJobCompleted` 事件；`Options.OnComplete` + manager 级 `SetOnJobComplete` + `DrainCompletedNotifications(sessionID)`（上限 64，按 session 过滤）；`InitCoderAgent` 粘合 Publish；UI 系统通知；工具描述改为「完成自动推送，勿轮询」。注意：完成钩子在 `close(done)` 前触发，`Status()` 终态判定兼容 `completedAt` |
| P9 | `background.go` | 抽出 `terminateAndWait`（grace 750ms → force 500ms → ctx 兜底），KillAll 复用 |
| P2 二期 | `agent/background_notify.go`、`coordinator.go` | Run 开头 Drain 本 session 终态通知，格式化为 `<background_jobs_completed>` 前缀注入 prompt；MAX_VISIBLE=3，超出折叠 "+N earlier" |
| 失控守护 | `background.go` | `syncBuffer.killLimit`（默认 2 GiB/流）→ `go cancel()` 异步触发一次；`JobStatus.OutputCapped` 上报；`SetOutputKillBytes` 测试钩子 |
| 总开关 | `background.go`、`bash.go` | `BackgroundTasksDisabled()` 读 `MO_CODE_DISABLE_BACKGROUND_TASKS`；显式后台报错、同步路径 timeout 分支续等不转后台 |
| P5 落盘 | `background.go`、`app.go` | `SetOutputDir` + `openJobOutputFiles`（0o700/0o600，`<unixts>-<id>.{out,err}`）+ `io.MultiWriter` tee + 终态 `closeFile`；Cleanup/Remove 删文件；失败降级纯内存 |

## 相关文件

- [[01-core-fixes-plan]]
- 上游参考：tmp/codex `codex-rs/core/src/unified_exec/head_tail_buffer.rs`
- mocode 既有调研：`docs/design/10-tty-input-and-bg-tasks.md`
