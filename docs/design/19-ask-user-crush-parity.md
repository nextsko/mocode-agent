# 19 · ask-user 对齐上游 Crush —— 让「问用户」不挂死、看得见、扛得住并发

> 状态：P0-1 / P0-2 / P1（G1-G6）已实现，G7 待做
> 上游参照：[charmbracelet/crush](https://github.com/charmbracelet/crush) @ `d05fb53`（本地克隆：`~/research/crush`）
> 本仓库参照：`internal/core/question`、`internal/core/tools/core/question`、`internal/ui/dialog/question_*.go`

---

## 1. 为什么要做这件事

mocode 已经有 `question` 工具（问用户要澄清、要确认、要选项），界面组件也基本是上游的移植。但把它当成一个**完整能力**审视时，对照上游 Crush 的最新形态，差距集中在 7 处，其中一处是**会挂死的功能性缺陷**：

> `mocode run` / `mocode mcp serve` / WeChat 网关 / 子代理 这些**非交互通道**里，模型一旦调用 `question` 工具，调用会一直阻塞等待「有人回答」——而这些通道里根本没有人能回答。
> MCP 包装器（`~/tools/mocode-mcp.js`）默认超时 **900 秒**，也就是说一次误问就能把一个 MCP 请求挂 15 分钟。

所以本轮目标不是「重写 ask-user」，而是**对齐上游已经趟过的坑**，让这个能力在 mocode 的多传输架构下真正安全可用。

---

## 2. 上游 Crush 的 ask-user 是怎么做的

### 2.1 一次问答的完整链路

```
LLM 调用 question 工具
        │  (blocking, 直到有人回答)
        ▼
question.Service.Ask(ctx, Request)          internal/question/question.go
        │  pubsub.Publish(CreatedEvent, req)
        ▼
TUI 收到事件 → 打开 inline editor 表单        internal/ui/model/ui.go
        │  用户作答 / 取消
        ▼
question.Service.Answer(batchID, answers)   → 唤醒 Ask 的 channel
        │  同时 Publish(Notification) 让其它客户端关掉自己的表单
        ▼
工具返回给 LLM：格式化文本 + 结构化 metadata
        │
        ▼
聊天区渲染：问题文本 + 答案 + 备注          internal/ui/chat/question.go
```

要点：**服务层是 pubsub + 阻塞 channel**，UI 只是订阅者；模型这一侧始终是「同步阻塞直到拿到答案」。

### 2.2 领域模型（`internal/question`）

| 类型 | 作用 |
|---|---|
| `Type` | `yes_no` / `single_choice` / `multi_choice` / `free_text` |
| `Choice` | `{id, label, description}` |
| `Question` | `{id, type, label, question, description, choices}` |
| `Answer` | `{question_id, selected_ids, fill_in_text, yes, notes}` |
| `Request` | `{id, session_id, tool_call_id, questions[], confirm_title, confirm_description}` |
| `Notification` | `{batch_id, session_id}`——批次解决后广播，让**没有作答的客户端**关掉自己的表单 |
| `QuestionNoteKey` | `"_question"`——备注归属约定：问题级备注用这个 key，选项级备注用选项 id |

硬限制（服务端 `Validate()` 强制，错误文案写给 LLM 看）：
`MaxQuestions=5`、`MaxChoices=5`、`MaxQuestionLength=240`、`MaxDescriptionLength=600`、`MaxChoiceLabelLength=200`、`MaxChoiceDescriptionLength=200`。
选择型问题**至少 2 个选项**；每题**必须有 description**。

### 2.3 LLM 契约（`internal/agent/tools/question.md`）

工具描述本身就是一份「怎么问问题」的规范，要点：

- 始终给 `questions` 数组；**1 个 → 无标签页**，**多个 → 标签页表单 + 结尾确认页**
- `yes_no` 只用于「肯定/否定」的命题；两个并列选项要用 `single_choice`（即使恰好两个选项）
- 选择型问题**自带自由填写项**，禁止自己再加「其他/自定义」选项
- 多题时 `confirm_title` / `confirm_description` 必填：确认页文案要「假装已经知道用户会怎么选」来写
- 不该用的场景：读代码/文档就能答的、要权限的场景（走权限系统）、信息类问题

### 2.4 TUI 呈现：inline editor，不是模态弹窗

上游把问答表单放在**输入框所在区域**（`m.activeInline`），而不是叠加一个模态对话框：

- 聊天区获得焦点时，表单**折叠**成一行（`DrawCollapsed`，提示 `answer questions`），高度不足时也会折叠
- 编辑器获得焦点时展开为完整表单，聊天区被挤下去
- 焦点从输入框切到表单，`esc` 取消整批问题

多题时的**标签页**：

- 标签标题取 `label`，没给就取问题文本前 3 个词（`shortLabel`）
- `[` / `]` 或 `ctrl+left` / `ctrl+right` 切页，循环
- 宽度不够时按比例截断；极窄时退化为「单标签 + N/M 计数」
- 标签页高度按**所有页的最大高度**算，切页不跳版
- 答完一页自动跳下一页；多题批次最后一页固定是 **Confirm**

### 2.5 选项列表、free-text 填写与备注

- 选项列表：`↑/↓`（或 `j/k`）移动并**循环**，`1-9` 数字键快速选中
- 列表**末尾永远有一行 free-text 填写项**（`Something else?`），单选时它本身就是答案，多选时是补充
- 鼠标：hover 高亮、点击选中、滚轮滚动且保持填写行可见
- 备注（notes）：`alt+n` 打开备注编辑器，`enter` 保存；**问题级备注**与**选项级备注**分开存（键分别是 `_question` 和选项 id）

### 2.6 键位表（上游）

| 场景 | 键 | 作用 |
|---|---|---|
| 表单 | `[` / `]` | 上/下一个标签页 |
| 表单 | `esc` | 取消整批问题 |
| 是/否 | `←/→` 或 `h/l` | 切换 Yes/No |
| 是/否 | `y` / `n` | 直接回答 |
| 是/否/选择 | `enter` | 确认（备注编辑器打开时是「保存备注」） |
| 单选 | `1-9` | 快速选中第 N 项 |
| 多选 | `space` | 勾选/取消 |
| 多选 | `enter` | 完成 |
| 自由文本 | `enter` | 提交 |
| 自由文本 | `shift+enter` / `ctrl+j` | 换行 |
| 任意 | `alt+n` | 给当前问题/选项加备注 |
| 确认页 | `y` / `n` / `←/→` | 确认 / 打回修改 |

### 2.7 两个容易被忽略的工程约束

**(a) 只给交互式运行，而且不给子代理**

上游在 coordinator 里显式加了：

```go
// Interactive-only, and never given to subagents; effectiveToolNames is what decides both.
if slices.Contains(allowed, tools.QuestionToolName) {
    allTools = append(allTools, tools.NewQuestionTool(c.questions))
}
```

并且 `app.InitCoderAgentNonInteractive()` 会以 `interactive=false` 建 agent——非交互运行里**根本没有这个工具**，模型不会去问一个没人能答的问题。

**(b) 多客户端作答**

- `Answer(batchID, answers)` / `Cancel(batchID)`：按批次 id 寻址，**多个 session 可以各自挂着一个待答批次**
- `take(batchID)`：在锁内摘走批次，使调用者成为**唯一** resolver，不会出现两个应答者抢一个 channel
- `Pending()`：客户端重连/上报 session 时，把**未决批次补发**回去
- `Notification{batch_id, session_id}`：批次解决后广播，其它客户端据此关掉自己的表单
- 后端另有 `AnswerQuestion` / `CancelQuestion`（`internal/backend/question.go`、`internal/server/`），让编辑器等**外部客户端**代答

### 2.8 聊天区的呈现

上游 `internal/ui/chat/question.go` 给 question 工具做了专用渲染器：

- 标题行：工具名 + 问题摘要（单题取问题文本，多题取「第一题 (+N more)」）
- 正文：每题一行 `✓ <问题> <答案>`，答案可能是 `Yes` / `No` / `Skipped` / 选项标签 / 自由文本
- 备注以 `╰ <备注>` 挂在对应问题下面
- 数据来自工具返回的 **metadata**（`QuestionResponseMetadata{questions, answers}`），不是解析文本

---

## 3. 现状对照：mocode 已经对齐的部分（**不要重写**）

逐文件归一化 diff（仅替换 import 路径与 `"_question"` 常量）后的结果：

| 上游文件 | mocode 文件 | 差异 |
|---|---|---|
| `internal/agent/tools/question.md` | `internal/core/tools/core/question/question.md` | **0 行**（逐字节一致） |
| `internal/agent/tools/question.go` | `internal/core/tools/core/question/question.go` | 端口适配（fantasy → `contract.Tool`），逻辑一致 |
| `internal/ui/dialog/question_form.go` | `internal/ui/dialog/question_form.go` + `question_form_draw.go` | **0 行**（mocode 拆成两文件） |
| `internal/ui/dialog/question_choice_base.go` | 同名 | **0 行** |
| `internal/ui/dialog/question_confirm.go` | 同名 | **0 行** |
| `internal/ui/dialog/question_editor.go` | 同名 | **0 行**（notes + fill-in） |
| `internal/ui/dialog/question_yesno.go` / `_single.go` / `_multi.go` / `_freetext.go` | 同名 | **0 行** |
| `internal/ui/dialog/question.go`（表单适配器） | `internal/ui/dialog/question_dialog.go` | mocode 额外做了 dialog stack 适配 |
| `internal/ui/model/ui.go`（打开表单） | `internal/ui/model/ui_question.go` | 同构，mocode 用 dialog stack 而非 inline |

**结论：界面层不用动。** 本轮只补「服务层语义」与「渲染/可达性」两块。

---

## 4. 差距清单

| # | 差距 | 证据 | 影响 | 级别 |
|---|---|---|---|---|
| G1 | **非交互通道没有做 interactive 限定** | 上游 `app.go:930 InitCoderAgentNonInteractive` + `coordinator.go:1146` 显式只在 interactive 时注册；mocode `internal/core/tools/registry.go:376` 只判断 `deps.Questions != nil`，且 `BuildFiltered` 全仓**无调用点** | `mocode run` / MCP / WeChat / 子代理里模型问问题 = **永久阻塞** | **P0** |
| G2 | **服务层只支持单个待答批次** | mocode `internal/core/question/question.go` 用单个 `pendingID` + 单 channel；上游用 `pending map[string]*pendingBatch` | 多 session 并发提问时，第二个请求覆盖第一个，第一个永远等不到 | **P0** |
| G3 | **`Answer`/`Cancel` 不按批次寻址** | mocode `Answer(answers) bool` / `Cancel() bool`；上游 `Answer(batchID, answers)` / `Cancel(batchID)` | 同 G2；且无法做「谁解决了哪个批次」的通知 | **P0** |
| G4 | **`Notification` 缺 `SessionID`** | mocode `Notification{BatchID}`；上游 `{BatchID, SessionID}` | 客户端无法判断该关掉哪个会话的表单 | P1 |
| G5 | **缺 `Pending()` 与 `QuestionNoteKey`** | mocode 无 `Pending()`；`_question` 字面量散落在 `question_yesno.go`、工具格式化处 | 客户端重连后未决批次丢失；备注归属靠魔法字符串 | P1 |
| G6 | **聊天区没有 question 专用渲染器** | mocode `internal/ui/chat/` 无 question 文件 → 走 `generic.go` 兜底；上游有 `chat/question.go` | 聊天记录里看不到问题与答案，只能看到一坨工具 JSON | P1 |
| G7 | **非 TUI 客户端没有作答入口** | 上游有 `internal/backend/question.go` + server API；mocode 只有 `Workspace.QuestionAnswer(answers)`（无 batchID），admin/ACP 无对应接口 | 外部客户端（编辑器、CI、微信）无法代答 | P2 |

附带清理：mocode 的 question 包注释仍写着「Only one question can be pending at a time … no correlation IDs are needed」，与已经批次化的 `Request.Questions` 矛盾（G2/G3 修完要一并改掉）。

---

## 5. 设计

### 5.1 P0-1：交互式限定（**先做这个，它止住挂死**）

**决策**：把「能不能问用户」变成**构建工具时的一个显式开关**，而不是靠「有没有人订阅」的隐式约定。

1. `internal/core/tools/tools.go`（`ToolDeps`）新增字段：

   ```go
   // Interactive reports whether a human can answer questions raised by
   // tools. Non-interactive transports (run/MCP/ACP/subagents) must leave
   // it false so the question tool is never registered.
   Interactive bool
   ```

2. `internal/core/tools/registry.go` 的 `questionPlugin.Build` 改为：

   ```go
   func (questionPlugin) Build(_ context.Context, deps ToolDeps) []fantasy.AgentTool {
       if deps.Questions == nil || !deps.Interactive {
           return nil
       }
       return []fantasy.AgentTool{deps.wrapPort(agentquestion.NewQuestionPortTool(deps.Questions))}
   }
   ```

3. `internal/core/agent/coordinator/coordinator_tools.go`：把 coordinator 自身的 interactive 状态写进 `ToolDeps.Interactive`。
   - 主 agent：来自 app 构造 coordinator 时传入
   - **子代理**：一律 `false`（对齐上游「never given to subagents」）

4. `internal/core/app/app.go`：
   - `RunNonInteractive` 路径建 agent 时置 `Interactive=false`
   - TUI 路径置 `true`

5. **兜底**：`question.Service.Ask` 已有 `ctx.Done()` 分支，务必确认所有非交互调用点都带**有限 deadline**；再加一道保险——非交互场景即便误注册，也应该在超时后返回明确错误而不是挂死。

**验收**：`mocode run "问用户：选 A 还是 B"` 必须**立刻失败或跳过**，而不是挂住。

### 5.2 P0-2 + P1-4/P1-5：批次寻址（把上游的并发模型搬过来）

**决策**：**整段移植上游 `internal/question/question.go` 的批次语义**，不自己发明。

服务层改造点（`internal/core/question/question.go`）：

```go
type Service interface {
    pubsub.Subscriber[Request]
    SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[Notification]

    // Ask publishes questions and blocks until the user answers.
    Ask(ctx context.Context, req Request) ([]Answer, error)

    // Answer resolves the pending batch with the given ID.
    // An empty ID names the only pending batch.
    Answer(batchID string, answers []Answer) bool

    // Cancel cancels the pending batch with the given ID.
    Cancel(batchID string) bool

    // Pending returns requests awaiting an answer.
    Pending() []Request
}
```

- 内部状态：`pending map[string]*pendingBatch`，每个批次带自己的 `answers chan` 与 `cancelled chan`
- `take(batchID)`：锁内摘走批次，保证唯一 resolver；空 id 且只有一个待答批次时按「唯一批次」处理
- `Ask` 为多题批次补默认确认文案（`"Ready to go?"` / `"Review your answers above and confirm."`）
- `Notification` 增加 `SessionID`
- 新增常量 `QuestionNoteKey = "_question"`，并把 UI 与工具里的 `"_question"` 字面量替换掉

**接口变更连带**（编译期会暴露，一次改干净）：

| 位置 | 变更 |
|---|---|
| `internal/transport/workspace/workspace.go` | `QuestionAnswer(answers)` → `QuestionAnswer(batchID string, answers []question.Answer) bool`；`QuestionCancel()` → `QuestionCancel(batchID string) bool` |
| `internal/transport/workspace/app_workspace.go` | 同步实现 |
| `internal/ui/model/ui_question.go` | 调用时传 `batch.ID` / `form.BatchID` |
| `internal/ui/dialog/question_*.go` | `"_question"` → `question.QuestionNoteKey`（3 处） |
| `internal/core/tools/core/question/question.go` | `"_question"` → `question.QuestionNoteKey` |

**注意**：`Workspace` 是对外接口，改签名属于**破坏性变更**，需要同步更新所有实现与调用点（TUI、admin、微信网关、测试）。

### 5.3 P1-6：聊天区渲染器

**决策**：照上游 `internal/ui/chat/question.go` 移植（179 行），但走 mocode 自己的 `chat` 包接口。

1. 工具层把结构化结果放进 metadata（`contract.ToolResult` 已有 `Metadata map[string]any`，且 `agenttooladapter` 会把它序列化进 `resp.Metadata`，持久化到 `message.ToolResult.Metadata`）：

   ```go
   type QuestionResponseMetadata struct {
       Questions []question.Question `json:"questions"`
       Answers   []question.Answer   `json:"answers"`
   }
   // ...
   return contract.WithMetadata(contract.Text(b.String()), "question", meta), nil
   ```

2. 新增 `internal/ui/chat/question.go`：`QuestionToolMessageItem` + `QuestionToolRenderContext`
   - 标题行复用 mocode 的 `toolHeader`
   - 正文：从 `result.Metadata` 反序列化 `QuestionResponseMetadata`，渲染「✓ 问题 答案」+ `╰ 备注`
   - 老会话没有 metadata 时，降级成按行截断的纯文本（对齐上游的兼容分支）
3. 在 `internal/ui/chat/tools.go` 的工具路由表里登记 `question`，别再走 `generic.go` 兜底。

### 5.4 P2-7：非 TUI 客户端作答通道（本轮**可选**）

若要开放 admin/ACP 代答，最小实现是：workspace 层暴露 `QuestionAnswer(batchID, answers)` / `QuestionCancel(batchID)`（5.2 已经做了），
admin 起一个只读 `GET /api/questions/pending` + `POST /api/questions/answer`。
**本轮先不做**，但 5.2 的接口改造要保证将来接得上。

### 5.5 不在范围内

- 不改上游移植过来的 8 个 UI 组件（已逐字节一致）
- 不改 `question.md` 工具契约（上游逐字节一致，改了反而与上游分叉）
- 不做「非交互时把问题降级成模型自问自答」——那是产品决策，另议

---

## 6. 交付物

```
internal/core/question/question.go            批次寻址 + Pending() + Notification.SessionID + QuestionNoteKey
internal/core/tools/tools.go                  ToolDeps.Interactive
internal/core/tools/registry.go               questionPlugin.Build 交互式限定
internal/core/agent/coordinator/coordinator_tools.go        ToolDeps.Interactive 接线
internal/core/agent/coordinator/coordinator_subagent.go     子代理强制非交互
internal/core/app/app.go                      RunNonInteractive / TUI 的 interactive 取值
internal/core/tools/core/question/question.go  metadata 载荷 + QuestionNoteKey
internal/ui/chat/question.go                  新增：聊天区渲染器
internal/ui/chat/tools.go                     登记 question 渲染器
internal/ui/dialog/question_yesno.go          "_question" → 常量
internal/ui/dialog/question_editor.go         "_question" → 常量（如有）
internal/ui/model/ui_question.go              传 batchID
internal/transport/workspace/workspace.go     接口签名变更
internal/transport/workspace/app_workspace.go 实现同步
docs/design/19-ask-user-crush-parity.md       本文档
```

---

## 7. 核验清单

```bash
# 1. 编译
go build -buildvcs=false ./...

# 2. 定向测试
go test ./internal/core/question/... ./internal/core/tools/core/question/... ./internal/ui/chat/... ./internal/ui/dialog/... ./internal/transport/workspace/...

# 3. 非交互不会挂死（**本轮最重要的验收**）
timeout 60 ./bin/mocode run "先问用户一个问题：继续还是停止？" ; echo "exit=$?"
# 期望：快速返回（跳过提问或明确报错），**绝不能** timeout 124

# 4. 交互式仍然能问（人工在 TUI 里验）
./bin/mocode
# 期望：出现标签页/确认页表单；回答后模型拿到答案

# 5. 聊天区呈现
# 期望：聊天记录里显示「✓ <问题> <答案>」与备注，而不是工具 JSON

# 6. 格式与静态检查
gofmt -l . | grep -v '^$'
go vet ./internal/...
```

**并发回归**（G2/G3 的核心断言）：同时起两个 session 各自提问，**互不覆盖**，且各自的答案回到正确的批次。

---

## 8. 明确不做

- 不重写 UI 组件（已与上游一致）
- 不改工具契约文案（`question.md`）
- 不引入「非交互自动应答」策略
- 不改本仓库之外的上游仓库

---

## 附录 A：上游源码索引（复核用）

| 关注点 | 上游文件 |
|---|---|
| 领域模型 + 批次服务 | `internal/question/question.go`（370 行） |
| 工具实现 | `internal/agent/tools/question.go`（210 行） |
| 工具契约（LLM 侧规范） | `internal/agent/tools/question.md` |
| 标签页表单 | `internal/ui/dialog/question_form.go`（793 行） |
| 选项列表基类 | `internal/ui/dialog/question_choice_base.go`（717 行） |
| 确认页 | `internal/ui/dialog/question_confirm.go`（380 行） |
| 备注 + fill-in | `internal/ui/dialog/question_editor.go`（276 行） |
| 题型组件 | `question_yesno.go` / `question_single.go` / `question_multi.go` / `question_freetext.go` |
| 聊天区渲染 | `internal/ui/chat/question.go`（179 行） |
| 交互式限定 | `internal/app/app.go:926-963`、`internal/agent/coordinator.go:1145-1150` |
| TUI 接线 | `internal/ui/model/ui.go:6006-6047` |
| 后端作答 | `internal/backend/question.go`、`internal/server/question.go` |

## 附录 B：怎样重新跑一遍本次对比

```bash
git clone --depth=1 https://github.com/charmbracelet/crush.git ~/research/crush

# 归一化后逐文件 diff（替换 import 路径与 "_question" 常量）
norm() {
  sed -e 's#github.com/charmbracelet/crush/internal/question#Q#g' \
      -e 's#github.com/nextsko/mocode-agent/internal/core/question#Q#g' \
      -e 's#question.QuestionNoteKey#NOTEKEY#g' \
      -e 's#"_question"#NOTEKEY#g' "$1"
}
diff <(norm ~/workspaces/mocode-task/internal/ui/dialog/question_form.go) \
     <(norm ~/research/crush/internal/ui/dialog/question_form.go)
```

---

## 附录 C：实现记录（本轮）

### 已落地

| 差距 | 落点 |
|---|---|
| G1 | `ToolDeps.Interactive`（`internal/core/tools/registry.go`）+ `questionPlugin.Build` 交互式限定 + `App.WithInteractive()` 接线 + 子代理一律 false |
| G2 / G3 | `internal/core/question/question.go` 整段对齐上游：pending map、`take(batchID)`、`Answer(batchID, answers)` / `Cancel(batchID)` |
| G4 | `Notification.SessionID` |
| G5 | `QuestionNoteKey` 常量 + `Service.Pending()` |
| G6 | `internal/ui/chat/question.go` 渲染器 + `internal/ui/chat/tools.go` 路由 + 工具侧 `QuestionResponseMetadata` |

### 与设计的偏差（有意为之）

1. **`ToolDeps` 定义在 `registry.go` 而非 `tools.go`。** mocode 把 `ToolDeps` 放在
   `internal/core/tools/registry.go`，`tools.go` 只是门面包。字段加在真实定义处，
   门面包另外补了 `core/question` 的类型再导出。
2. **交互式开关用 `app.WithInteractive()` 选项，而不是上游的
   `InitCoderAgentNonInteractive()`。** mocode 的 `app.New` 内部就会建 coordinator，
   事后无法再决定工具集；选项在建 coordinator 之前生效，语义等价。
3. **`contract.WithMetadata` 只有两参版本**（载荷挂在 `Metadata["result"]` 下），
   adapter 会把单键 metadata 拍平，所以最终 `message.ToolResult.Metadata` 就是
   `{"questions":…,"answers":…}`，与上游一致。
4. **多了一道兜底：非交互 App 的 question service 带 2 分钟上限**
   （`question.NewServiceWithTimeout`）。工具本来就不注册，这只是防止接线失误时挂死。
5. **TUI 的问题通知按批次匹配**（`BatchID` 相同才关表单），上游是「收到任意通知就关」。

### 仍然不做

- G7（非 TUI 客户端作答通道）、5.5 节列出的所有不做项。

### 核验

```bash
go build -buildvcs=false ./...
go test ./internal/core/question/... ./internal/core/tools/core/question/... \
        ./internal/ui/chat/... ./internal/ui/dialog/... ./internal/transport/workspace/...
gofmt -l .
```

> 第 3 条「非交互不会挂死」需要真实 LLM 往返，本机沙箱无外网出口时无法端到端验证；
> 改为用单元测试锁定闸门本身（`internal/core/tools/registry_question_test.go`、
> `internal/core/agent/coordinator/coordinator_tools_test.go`）。
