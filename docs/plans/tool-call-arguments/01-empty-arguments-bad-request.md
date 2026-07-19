# 部分模型报 `tool_calls.function.arguments is required` 根因分析与修复计划

> 记录时间：2026-07-20
> 触发现场：使用 **Step 3.7 Flash** 模型开启 Yolo mode 多轮工具调用，第二轮请求时 provider 返回 `400 Bad Request: tool_calls.function.arguments is required`
> 状态：根因已定位，修复方案设计中
> 跟踪状态：🟡 **开放中**（待修复 → 待回归验证 → 待上游 PR）

---

## 📌 TL;DR

| 维度 | 摘要 |
|------|------|
| 现象 | 多轮工具调用第二轮被 provider 拒绝 |
| 错误信息 | `400 Bad Request: tool_calls.function.arguments is required` |
| 触发模型 | Step 3.7 Flash；可能波及所有产生空 `arguments` 的 OpenAI 兼容模型 |
| 触发 provider | StepFun、自部署 vLLM strict mode 等 OpenAI 兼容网关 |
| 根因 | fantasy v0.38.1 的 `Generate` 路径透传空 `arguments`（流式路径会归一化为 `"{}"`） |
| 影响范围 | 历史 JSONL 已可能污染旧会话 |
| 修复策略 | 在 Mocode 侧 PrepareStep 钩子里归一化 + 上游 PR |
| 跟踪链接 | 本文件作为唯一跟踪记录 |

---

## 🐛 现象

第二轮模型请求直接被 provider 拒绝：

```
ERROR  Bad Request
tool_calls.function.arguments is required

X bad request: tool_calls.function.arguments is required
```

第一轮（以及任何带 `tool_calls` 的 assistant 历史消息）都正常，错误只出现在**该 assistant 消息被重新带回下一轮请求**的瞬间。

复现条件：
- 模型：Step 3.7 Flash（走 OpenAI 兼容协议）
- 工具：任意会真正被调用的工具（不只是声明）
- 模式：开启 Yolo（多步）即必现，单轮对话偶现

用户体感：
- 第一轮能完成工具调用
- 第二轮无论模型想说什么，provider 直接 400
- agent 永远卡在第一轮 ↔ 第二轮之间
- UI 提示 "bad request"，看不到根因

---

## 🔬 根因链路（逐步追踪）

### 第 1 步：模型返回了空 `arguments`

某些 OpenAI 兼容模型（已观察到的有 Step 3.7 Flash）在调用意图很弱、或工具 schema 没有强制参数时，会发出：

```json
{
  "id": "call_xxx",
  "type": "function",
  "function": {
    "name": "todos",
    "arguments": ""
  }
}
```

这是模型侧的不规范行为，但它本身对宽松的 provider（OpenAI 官方、Anthropic）无害。

**Step 3.7 Flash 的具体行为**（推理）：
- 当思考模型得出"应该调用 X，但 X 没有参数"的结论时
- 它倾向于省略 `arguments` 字段
- 这是 prompt-tuning 训练副产物，与 OpenAI 严格格式不符

### 第 2 步：fantasy 库的流式 / 非流式处理不对称

仓库依赖 `charm.land/fantasy v0.38.1`。在 `providers/openai/language_model.go` 中存在两条独立路径：

#### 路径 A：`Generate`（非流式）—— **有 bug**

文件：`go/pkg/mod/charm.land/fantasy@v0.38.1/providers/openai/language_model.go:273-281`

```go
for _, tc := range choice.Message.ToolCalls {
    toolCallID := tc.ID
    content = append(content, fantasy.ToolCallContent{
        ProviderExecuted: false,
        ToolCallID:       toolCallID,
        ToolName:         tc.Function.Name,
        Input:            tc.Function.Arguments,   // ← 可能是 ""
    })
}
```

`tc.Function.Arguments` 直接透传，没有归一化。

#### 路径 B：`Stream`（流式）—— **已正确处理**

文件：同文件，行 430、516

```go
// 流结束阶段：关闭未完成的 tool call
if tc.arguments == "" {
    tc.arguments = "{}"   // ← 规范化为合法 JSON
    toolCalls[idx] = tc
}
```

流式路径在所有未完成 tool call 收尾时把空 arguments 替换为 `"{}"`。**这条规则没在非流式路径上重复** —— 这是 fantasy 库的内部不一致。

#### 路径 B 也只在"完整收尾"时生效

仔细看 fantasy v0.38.1 的 stream 实现（L380-L520），以下情形**仍然可能漏掉**：
- 流中途网络断开（提前关闭）
- provider 没发 `finish_reason: tool_calls` 而改为 `length`/`stop`
- 多 tool call 并行且只有部分完成

也就是说流式路径也不是 100% 安全，但至少**收尾时会修补**。Mocode 主循环走的就是流式。

### 第 3 步：Mocode 把 assistant 消息原样持久化

文件：`internal/core/agent/agent.go`、JSONL store

`message.Message.ToAIMessage()`（或 `preparePrompt` 内部对应逻辑）把 `ToolCallContent` 映射为本地 `ToolCall`，其中 `Input string` 对应 JSONL 字段 `"input"`。

空 `Input` 即 `"input": ""` 落到 JSONL，**没有任何地方补齐**。

### 第 4 步：下一轮请求时回传给 provider

文件：`go/pkg/mod/charm.land/fantasy@v0.38.1/providers/openai/language_model_hooks.go:508-518`

```go
assistantMsg.ToolCalls = append(assistantMsg.ToolCalls,
    openai.ChatCompletionMessageToolCallUnionParam{
        OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
            ID:   toolCallPart.ToolCallID,
            Type: "function",
            Function: openai.ChatCompletionMessageFunctionToolCallParam{
                Name:      toolCallPart.ToolName,
                Arguments: toolCallPart.Input,   // ← 仍然是 ""
            },
        },
    })
```

`toolCallPart.Input` 直接来自上一轮 assistant 的输入，没有 `if "" → "{}"` 的兜底。

最终 HTTP body 形如：

```json
{
  "tool_calls": [{
    "id": "call_xxx",
    "type": "function",
    "function": { "name": "todos", "arguments": "" }
  }]
}
```

### 第 5 步：严格 provider 直接 400

OpenAI 官方把空 `arguments` 当 `{}` 处理；而 StepFun、部分自部署 vLLM/Ollama 兼容网关会做 schema 校验，看到 `arguments: ""` 就拒绝：

```
400 Bad Request
tool_calls.function.arguments is required
```

至此错误形成完整闭环。

---

## ⚠️ 为什么"有些模型"会触发

| 类别 | provider | 对空 `arguments` 行为 | 触发概率 |
|---|---|---|---|
| OpenAI 官方 | `api.openai.com` | 接受为空 | 0% |
| Anthropic | `api.anthropic.com` | 接受为空 | 0% |
| Bedrock | `bedrock-runtime.*` | 接受为空 | 0% |
| **StepFun / Step 3.7 Flash** | `api.stepfun.com` | **拒绝** | **100%** |
| 自部署 vLLM (strict mode) | 自定义网关 | 拒绝 | 高 |
| LM Studio / Ollama 默认 | `localhost:*` | 接受为空 | 0% |
| openrouter (passthrough) | 各上游决定 | 取决于上游 | 中 |
| deepseek (官方) | `api.deepseek.com` | 接受为空 | 0%（观察） |

行为差异完全在 provider 侧 —— 我们无法改变上游校验规则，只能保证**自己送出去的 JSON 永远合法**。

---

## 🩺 Mocode 当前已有旁路

`internal/core/agent/templates/coder.md.tpl:134` 已把这条错误写入已知失败模式：

```markdown
- Treat validation errors such as "missing required parameter",
  "old_string not found", or "tool_calls.function.arguments is required"
  as failed actions. Do not claim progress from them; rebuild the call
  with valid arguments and retry through a distinct remediation path.
```

但这是**给模型看的指令**，依赖模型在错误后自我纠正（重发 tool call）。实际上当请求被拒绝时，模型连修正机会都没有 —— provider 已经 400 了，整个请求是 `agent.Stream` 内部的 error 事件，模型根本没有收到原始 4xx 响应体。

---

## 🎯 修复方向

### 方案 A：在发送前规范化（推荐 · 短期）

在 Mocode 把 assistant 消息送出前，把所有 `ToolCallContent.Input == ""` 替换为 `"{}"`。

#### 候选插入点（按优先级）

1. **`internal/core/agent/agent_lifecycle.go` 的 PrepareStep 钩子**
   - 文件：155 行 `preparePrompt` → 197 行 `workaroundProviderMediaLimitations` → 201 行 `filterEmptyContentMessages`
   - 位置：在 `filterEmptyContentMessages` 之前插入新归一化函数
   - 优势：覆盖所有走主循环的请求，包括 Stream 和 Generate 路径
   - 风险：低；纯本地内存操作，不改持久化

2. **`internal/core/agent/agent_helpers.go` 新增工具函数**
   - 与 `filterEmptyContentMessages`、`filterOrphanedToolResults` 同级
   - 签名草案：
     ```go
     func normalizeEmptyToolCallArguments(
         msgs []fantasy.Message,
     ) []fantasy.Message
     ```

#### 实现骨架

```go
// normalizeEmptyToolCallArguments rewrites any assistant ToolCallContent whose
// Input is empty to "{}". Some OpenAI-compatible providers (e.g. StepFun)
// reject requests where tool_calls.function.arguments is ""; fantasy's
// Generate path passes the raw value through, so we patch it here.
func normalizeEmptyToolCallArguments(msgs []fantasy.Message) []fantasy.Message {
    for i := range msgs {
        if msgs[i].Role != fantasy.MessageRoleAssistant {
            continue
        }
        for j, part := range msgs[i].Content {
            tc, ok := fantasy.AsMessagePart[fantasy.ToolCallContent](part)
            if !ok {
                continue
            }
            if tc.Input == "" {
                msgs[i].Content[j] = fantasy.ToolCallContent{
                    ProviderExecuted: tc.ProviderExecuted,
                    ToolCallID:       tc.ToolCallID,
                    ToolName:         tc.ToolName,
                    Input:            "{}",
                    ProviderMetadata: tc.ProviderMetadata,
                }
            }
        }
    }
    return msgs
}
```

调用点：

```go
// agent_lifecycle.go:197 之后插入
prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, largeModel)
prepared.Messages = normalizeEmptyToolCallArguments(prepared.Messages)
prepared.Messages = filterEmptyContentMessages(prepared.Messages)
```

#### 关键设计取舍

| 取舍点 | 选项 | 决策 |
|---|---|---|
| 归一化为 `"{}"` 还是 `"null"` | `{}` vs `null` | `{}`：与 fantasy 流式路径一致；与 OpenAI 官方行为一致 |
| 改 fantasy.Message 还是 message.Message | fantasy（运行时）vs message（持久化） | fantasy：影响发送而不污染历史；保留原始错误数据供调试 |
| 遇到 `Invalid: true` 是否跳过 | 跳过 vs 同样归一化 | 同样归一化：无效 tool call 也可能在历史里导致 400 |
| 是否同时归一化 message.Message.ToolCalls | 是 vs 否 | 否：让原始 `"input":""` 留在 JSONL 便于排查；只在发送前打补丁 |
| 是否记录 metric/日志 | 每次 vs 首次 | 每轮统计 N 次归一化（info 级）便于识别模型质量 |

### 方案 B：拦截 400 错误并自我修复（兜底 · 不推荐首选）

在 `internal/core/agent/toolutil/retry.go` 的 `DefaultShouldRetry` 增加对
`tool_calls.function.arguments is required` 的识别，触发一次"压缩历史 + 重发"。

不推荐作首选：
- 需要先识别错误（grep 字符串脆弱，error 包装层可能丢失原文）
- 用户体感是"刚才那一步白做"
- 治标不治本

### 方案 C：上游修复 fantasy 库（长期）

向 `charm.land/fantasy` 提一个 PR，让 `Generate` 路径在 `tc.Function.Arguments == ""` 时也赋 `"{}"`。**最快、最对的位置**。

短期我们用方案 A，长期推动方案 C。

#### 提案 PR 草稿（可参照）

**标题**: `fix(openai): normalize empty tool call arguments in Generate path`

**改动**: `providers/openai/language_model.go:273-281`

```diff
 for _, tc := range choice.Message.ToolCalls {
     toolCallID := tc.ID
+    args := tc.Function.Arguments
+    if args == "" {
+        args = "{}"
+    }
     content = append(content, fantasy.ToolCallContent{
         ProviderExecuted: false,
         ToolCallID:       toolCallID,
         ToolName:         tc.Function.Name,
-        Input:            tc.Function.Arguments,
+        Input:            args,
     })
 }
```

**测试**: 在 `openaicompat_test.go` 添加 fixture：mock provider 返回 `arguments: ""`，验证 `Generate` 产物 `ToolCallContent.Input == "{}"`。

### 方案 D：Mocode 侧 fallback（防御性）

在 agent 层对 `agent.Stream` 返回的 error 做错误码识别：
- 如果是 4xx 且 body 包含 `tool_calls.function.arguments is required`
- 则本地修复最近一条 assistant 的 tool call（把空 arguments 改 `{}`）
- 重试一次

实施成本中等，可作为 A 的冗余，但优先级低于 A。

---

## 📐 测试矩阵

### 单元测试 `internal/core/agent/agent_helpers_test.go::TestNormalizeEmptyToolCallArguments`

| 输入 | 期望输出 |
|---|---|
| assistant 含 `Input: ""` | `Input: "{}"` |
| assistant 含 `Input: "{}"` | 不变 |
| assistant 含 `Input: "{\"a\":1}"` | 不变 |
| assistant 无 tool call | 不变 |
| user / tool / system 消息 | 不变 |
| 多 part 中只有一个为空 | 仅替换那一个 |
| `Invalid: true` + `Input: ""` | 仍然替换为 `"{}"` |
| `ProviderExecuted: true` + `Input: ""` | 仍然替换为 `"{}"` |
| 嵌套结构 `extraContent` | 不动 |

### 回归测试

构造一个 mock OpenAI provider：
- 第一轮：返回 `arguments: ""` 的 tool call
- 第二轮：捕获实际发出的 HTTP body
- 断言：`tool_calls[0].function.arguments == "{}"`

### 端到端测试

用真实 StepFun 凭据跑：
- Yolo mode + multi-tool
- 10 轮对话
- 断言没有 400

### 现有测试回归

`go test ./internal/core/agent/...` 全绿（特别是 `agent_helpers_test.go` 现有用例）。

---

## 🔖 相关源码位置（带行号）

### 仓库源码

| 文件 | 行 | 角色 |
|---|---|---|
| `internal/core/agent/agent.go` | 191 | `workaroundProviderMediaLimitations` 注释 |
| `internal/core/agent/agent_convert.go` | 64 | `workaroundProviderMediaLimitations` 定义 |
| `internal/core/agent/agent_helpers.go` | 53-70 | 同级过滤函数范例 |
| `internal/core/agent/agent_lifecycle.go` | 155 | `preparePrompt` 调用 |
| `internal/core/agent/agent_lifecycle.go` | 167 | `agent.Stream` 主调用点（用户轮） |
| `internal/core/agent/agent_lifecycle.go` | 197 | `workaroundProviderMediaLimitations` |
| `internal/core/agent/agent_lifecycle.go` | 201 | `filterEmptyContentMessages` |
| `internal/core/agent/agent_lifecycle.go` | 616 | 标题生成 `preparePrompt` |
| `internal/core/agent/agent_lifecycle.go` | 640 | 标题生成 `agent.Stream` |
| `internal/core/agent/agent_prompt.go` | 33 | `preparePrompt` 函数定义 |
| `internal/core/agent/agent_prompt.go` | 192, 201 | 其他 Stream 调用 |
| `internal/core/agent/candidate/llmselector.go` | 62 | candidate 选择走 `agent.Generate` |
| `internal/core/agent/failover/failover.go` | 80-160 | failover 包装层 |
| `internal/core/agent/toolutil/callback.go` | - | 工具调用包装 |
| `internal/core/agent/templates/coder.md.tpl` | 134 | 已记录但仅靠模型自我纠正 |
| `internal/domain/session/message/content.go` | 102-109 | `ToolCall` 持久化结构 |
| `internal/domain/session/message/content.go` | 377-391 | `AppendToolCallInput` 增量拼装 |

### 上游 fantasy v0.38.1（go mod cache）

| 文件 | 行 | 角色 |
|---|---|---|
| `providers/openai/language_model.go` | 273-281 | `Generate` 路径透传空 arguments（**bug 起点**） |
| `providers/openai/language_model.go` | 380-402 | Stream 路径对新 tool call delta 的容忍 |
| `providers/openai/language_model.go` | 430-433 | Stream 中先关闭的 tool call 归一化 |
| `providers/openai/language_model.go` | 516-519 | Stream 收尾阶段归一化 |
| `providers/openai/language_model_hooks.go` | 508-518 | 出站 assistant 消息序列化 |
| `content.go` | 165-178 | `AsMessagePart[T]` 类型断言辅助 |
| `content.go` | 428-444 | `ToolCallContent` 定义 |

---

## 🛠 实施步骤（推荐顺序）

1. **创建分支** `fix/openai-empty-tool-call-arguments`
2. **新增函数** `normalizeEmptyToolCallArguments` 于 `agent_helpers.go`
3. **接入钩子** 在 `agent_lifecycle.go:201` 之前调用
4. **写单测** 覆盖上面 9 个用例
5. **本地验证**
   ```bash
   go test ./internal/core/agent/... -run TestNormalizeEmptyToolCallArguments -v
   go test ./...  # 全量回归
   go build -buildvcs=false .
   ```
6. **真实环境验证** 用 Step 3.7 Flash 跑一轮真实对话，验证不再 400
7. **日志埋点** info 级日志记录归一化次数（便于观察模型质量）
8. **更新模板** 把 `coder.md.tpl:134` 关于 `tool_calls.function.arguments` 的提示降级或移除（既然已经在发送前修好）
9. **更新 AGENTS.md** 在 "Provider Behavior Observations" 表里追加 Step 3.7 Flash
10. **提交 commit**：`fix(agent): normalize empty tool call arguments before sending`
11. **提 PR 到上游** `charm.land/fantasy`，附 reproduction 与 fix 草稿
12. **合入后续** 把上游修复 cherry-pick 或升级 fantasy 版本

---

## 📊 验证 checklist（修复完成后）

- [ ] 单测全部通过
- [ ] `go test ./...` 全绿
- [ ] `go build` 无错误
- [ ] `task lint:fix` 无警告
- [ ] 用 Step 3.7 Flash 跑 10 轮真实 Yolo 模式，无 400
- [ ] JSONL 历史中保留原始 `input: ""`（调试可见）
- [ ] 运行时日志中能观察到归一化次数
- [ ] coder.md.tpl 中相应提示已移除或注释为"已修复"
- [ ] AGENTS.md "Provider Behavior Observations" 已追加
- [ ] 上游 PR 已创建

---

## 🧪 调试日志范式（修复前复现用）

开启 `mocode logs --lines=200` 配合如下 grep 过滤：

```bash
# 看实际发送给 provider 的 body
grep -E '"arguments":""' ~/.local/share/mocode/projects/**/logs/*.jsonl

# 看 fantasy 是否做了归一化
grep -E 'toolCalls.*arguments' ~/.local/share/mocode/logs/mocode.log

# 看 400 出现时的完整堆栈
grep -B2 -A5 'tool_calls.function.arguments is required' ~/.local/share/mocode/logs/mocode.log
```

修复后预期：
- JSONL 中 `input: ""` 仍然存在（保留调试信号）
- 运行时日志出现 `normalized empty tool call arguments for X tool calls`
- HTTP body 中 `arguments` 永远是 `"{}"` 或更长字符串

---

## 📈 历史会话补救方案（可选）

如果担心历史 JSONL 中已有 `"input": ""`，可选方案：

### 方案 X：一次性迁移脚本

写 `scripts/fix-empty-tool-call-args/main.go`：
- 遍历 `%LOCALAPPDATA%/mocode/projects/**/*.jsonl`
- 找到 `tool_calls[].input == ""`
- 替换为 `{}`
- 备份原文件 → `.jsonl.bak`
- 输出统计报告

实施成本：1 小时。收益：避免老会话第二轮回放时仍然 400。

**触发条件**：当出现 5+ 用户报告同样问题后启动此脚本。

---

## 🏷️ 跟踪标签

本计划使用以下元数据方便后续脚本追踪：

```
topic=provider-compat
severity=medium (对话死锁，但不会丢数据)
component=core/agent
upstream=charm.land/fantasy@v0.38.1
trigger-model=step-3.7-flash
trigger-provider=stepfun
fix-strategy=normalize-on-send
fix-locations=internal/core/agent/agent_helpers.go, internal/core/agent/agent_lifecycle.go
upstream-pr=TBD
```

---

## 🔄 状态变更记录

| 日期 | 状态 | 变更 |
|---|---|---|
| 2026-07-20 | 🟡 开放中 | 根因定位完成，方案设计中 |
| TBD | 🟢 修复中 | 开始实施 |
| TBD | ✅ 已修复 | 验证通过，关闭跟踪 |
| TBD | 📤 已上游 | PR 提交 |

---

## 📎 相关 issue / 笔记链接

- `docs/plans/agentic-fetch/01-context-canceled-failure.md` —— 类似的 provider-side 错误分析模板
- `AGENTS.md` "Provider Behavior Observations" —— 后续追加 Step 3.7 Flash 行
- 未来 `docs/dev-notes/empty-arguments.md` —— 修复完成后转正式 dev note

---

## ❓ 未决问题

1. **上游是否同意合入**：需要先建 PR 看反馈。
2. **是否同时归一化 message.Message 持久化层**：暂定否，但需团队 review。
3. **是否引入 metric 暴露给用户**：暂定仅内部日志，后续考虑暴露到 mocode_info。
4. **历史 JSONL 补救脚本**：何时启动？提议等 5+ 报告再启动。
5. **方案 D（4xx 拦截重试）是否需要**：等修复后再评估。