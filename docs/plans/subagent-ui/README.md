# subagent-ui — 子代理渲染改造（prime-agent 式摘要行）

> 状态：已完成（2026-09-13）
> 创建：2026-09-13
> 范围：`internal/ui/chat/agent.go`（+ 同主题的 bg-jobs-fix 三个可选项）
> 依据：prime-agent（PrimeIntellect-ai，已克隆 `tmp/prime-agent`）`subagent-summary-line.ts` / `interactive-mode.ts`

## 原则（prime-agent 实证）

1. **聊天流里子代理只占一行摘要**：counts（running/idle/inactive）+ open 提示，永不展开过程细节
2. **详情与聊天流解耦**：独立视图承载，聊天布局高度恒定
3. **loader 只跟主 agent 流式状态**，子代理状态由摘要行承载

## mocode 痛点定位

`chat/agent.go AgentToolRenderContext.RenderTool`：
- 运行中：`tree.Root(header)` 逐个挂 nested tool 节点 → 子代理 N 次工具调用 = N 节点持续增高，主聊天流被撑爆
- 完成后：嵌套树依然全量保留 + 追加完整 markdown body → 历史消息回顾时长屏垃圾
- 其余工具均已接入 `ExpandedContent` 折叠，唯独 Agent 嵌套树没有折叠路径

## 改造（KISS，复用现有 ExpandedContent 机制）

| 状态 | 现状 | 改为 |
|------|------|------|
| running & nested=0 | header + statusSummary + spinner | 不变 |
| running & nested>0 | header + 全量嵌套树 + 摘要 | header + **单行 `N tool calls · statusSummary`** + spinner（不渲染树） |
| done & !expanded | 全量嵌套树 + markdown body | header + 单行摘要（`N tool calls · 最终状态`）+ body（markdown，已有 responseContextHeight 折叠） |
| done & expanded | 同上 | 完整嵌套树 + 全文 body（审计视图，保留现路径） |

AgenticFetchToolRenderContext 同构改造。

## 验收

| # | 项 | 证据 |
|---|----|------|
| A1 | running 状态渲染行数与 nested 数量无关（≤5 行） | 渲染单测：构造 50 nested tools，断言输出行数 |
| A2 | done 默认视图无嵌套树节点字符（roundedEnumerator 产物） | 单测断言 |
| A3 | expanded 视图保留完整树 | 单测断言 |
| A4 | build/vet/test 全绿 | 命令输出 |

## 附带（bg-jobs-fix 可选项，同批实施）

- **P6**：`RunInBackground` 快检 `sleep(250ms)` → 显式 2s 窗口（waitLoop select done/timeout）
- **通知分级**：`MOCODE_BACKGROUND_NOTIFICATIONS=all|result|error`（默认 all）；result=单行摘要注入；error=仅 failed/killed 注入。落点 `agent/background_notify.go` 过滤
- **ID 纪元**：manager 初始化时生成 base36 时间前缀，job ID = `<epoch>-<hex>`，消除重启复用

## 相关

- [[../bg-jobs-fix/README]]
- prime-agent 参考：`packages/coding-agent/src/modes/interactive/components/subagent-summary-line.ts`

## 实施记录（2026-09-13）

| 项 | 文件 | 变更 |
|----|------|------|
| 摘要模式 | `chat/agent.go` | Agent/AgenticFetch：默认视图（running 与 done）渲染 header + `N tool calls · statusSummary` 单行 + spinner；嵌套树仅在 `ExpandedContent`（审计视图）渲染；done 的 body 走既有 markdown 折叠 |
| P6 yield 窗口 | `tools/core/shell/bash.go`、`background.go` | RunInBackground 快检 `sleep(250ms)` → `WaitFor(2s)` 事件等待（新导出 `WaitFor`）；响应文案改推送语义 |
| 通知分级 | `agent/background_notify.go` | `MOCODE_BACKGROUND_NOTIFICATIONS=all\|result\|error`：all=完整块、result=单行摘要、error=仅 failed/killed |
| ID 纪元 | `background.go` | job ID = `<base36分钟>-<hex序号>`，跨进程重启零复用 |
| 落盘修复 | `background.go` | defer 顺序调整：审计文件在 `close(done)` **之前**关闭（Windows 删除打开文件会失败） |
| 测试 | `chat/agent_test.go`（新增）等 | A1 running 50 nested ≤10 行；A2 done 默认无树；A3 expanded 保树；分级三档；ID 前缀。同步修订 2 个锁定旧树渲染行为的既有测试 |

## 追加批次（2026-09-13，同日第二批）

| 项 | 文件 | 变更 |
|----|------|------|
| Ctrl+B promote | `background.go`、`bash.go`、`ui_keys.go` | `PromotePendingBash()` 一次性广播信号；同步 waitLoop 增加 promote 分支（保留 job 注册、立即返回后台 ID）；TUI 全局 ctrl+b（agent busy 时）触发——Claude Code Ctrl+B 语义，tmux 用户按两次 |
| 子代理计数状态行 | `model/chat.go`、`ui_agent_control.go` | `CountRunningAgentTools()` 遍历统计 in-flight Agent 工具调用；并行子代理 >1 时状态行前缀 `N agents running · executing X...`（prime-agent roster 精髓） |
| 系统通知克制 | `ui_agent_control.go` | OS 级通知仅弹 failed/killed；completed 经 turn 注入 + 聊天卡片已覆盖，弹窗留给异常 |
| flaky 修复 | `background_test.go` | `TestBackgroundShell_Status_Killed` 容忍 Windows 进程收尾抖动（单跑三次证实偶发，与改动无关） |

验证：全仓 build + vet + 五包 test 全绿。

验证：全仓 `go build` + `go vet ./...` + 五包 `go test` 全绿。UI 行为由渲染单测断言；真机 TUI 视觉效果待实际会话确认。
