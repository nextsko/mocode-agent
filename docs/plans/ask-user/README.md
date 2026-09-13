# ask-user — AskUserQuestion 工具移植（crush 官方实现）

> 状态：已完成（2026-09-13）
> 创建：2026-09-13
> 范围：core（question 服务/工具/注册/app 桥）+ UI（inline_editor + 8 个 question 组件 + 路由）
> 依据：crush 官方 `internal/question`、`internal/agent/tools/question.go`、`internal/ui/dialog/question_*.go`（同源 fork 直接移植 + 适配）

## 事实

- mocode **没有** AskUser 工具（fork 后缺失），模型只能借 permission dialog 或纯文本问问题
- crush 官方体系：pubsub 阻塞式 question.Service（镜像 permission 通道）+ 4 种题型（yes_no/single/multi/free_text）+ tabbed 批量表单 + confirm 页；LLM 友好校验（上限、可行动错误信息）；取消 → StopTurn
- 布局吸收点：批量表单为**对话框内 tab 切换**（不纵向堆叠），聊天流零污染——与 prime-agent 摘要行同理

## 批次 A：core 链路

| 步 | 文件 | 动作 |
|----|------|------|
| A1 | `internal/core/question/question.go` | 拷 crush，import 改 `nextsko/.../util/pubsub`（uuid 已在 go.mod） |
| A2 | `internal/core/tools/core/question/{question.go,question.md}` | 拷 crush 工具，`GetSessionFromContext`→`toolutil.`，注册类型校验不变 |
| A3 | `internal/core/tools/registry.go` | Dependencies 增 QuestionSvc；工具列表注册 |
| A4 | `internal/core/app/app.go` | App 持有 QuestionService；setupEvents 桥 `question.Request`/`question.Notification` → app.events |
| A5 | `internal/transport/workspace/{workspace.go,app_workspace.go}` | 接口增 `QuestionAnswer/QuestionCancel`，实现转发 service |

## 批次 B：UI

| 步 | 文件 | 动作 |
|----|------|------|
| B1 | `internal/ui/dialog/inline_editor.go` | 拷 crush（mocode 缺此依赖） |
| B2 | `internal/ui/dialog/question_*.go` ×8 | 拷 crush，按编译错误适配 styles/common |
| B3 | `internal/ui/model/ui.go` | 事件路由：`question.Request`→打开表单+系统通知；`question.Notification`→关闭 |
| B4 | `internal/ui/model/ui_dialogs.go` | Answer/Cancel 走 `m.com.Workspace.Question*` |
| B5 | `internal/ui/styles` | 按需补 `Dialog.Question*` 字段（对齐 crush） |

## 验收

1. `go build ./...` + vet 全绿
2. core：question 服务单测（Ask/Answer/Cancel/校验，拷 crush 测试适配）
3. 工具注册可见（registry 测试/registry_startable 风格）
4. UI 组件测试：crush `question_choice_base_test.go` 移植通过

## 风险

- crush UI 与 mocode styles 漂移 → 编译驱动适配
- wechat 等非 TUI 消费者无订阅者时 Ask 挂起 → 沿用 crush 语义（ctx 取消可解），暂不加超时

## 实施记录（2026-09-13，全量完成）

| 批 | 交付 | 关键适配 |
|----|------|----------|
| A | `core/question`（服务+5 测试）、`tools/core/question`（工具+md）、registry `questionPlugin`（`ToolDeps.Questions`）、coordinator 传参、app `Questions` 字段 + 双事件桥、workspace `QuestionAnswer/QuestionCancel` | pubsub 路径、`toolutil.GetSessionFromContext`、config `allToolNames` 登记（task agent 只读白名单自动排除） |
| B | `dialog/inline_editor.go` + 8 个 `question_*.go`（~2900 行）、`question_dialog.go` 适配器（挂 mocode dialog 栈）、ui.go 路由（Request→开表单+系统通知；Notification→关闭）、`question_choice_base_test.go` 移植 | styles 补 `Editor.Question*` ×12、`Tab` struct（uv 边框）、`Tool.WarnTag/WarnMessage`、`Button.Hovered`；common 补 `LockMarkdownRenderer`、`ButtonHitCompositor/HitButtonIndex`（button.go 整体升级）、`WheelScrollable`；crush 的 activeInline 机制 → mocode `QuestionDialog` 包装器 |

验证：全仓 build + vet + 九包 test 全绿。已知限制：多客户端并发抢答由 Notification 广播兜底（crush 同语义）；sub-agent 调 question 会占全局唯一 pending 位（罕见，ctx 取消可解）。
