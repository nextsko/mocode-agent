---
name: runtime
description: >-
  Use when 使用 @assistant-ui/react 的 runtime 系统：创建 runtime（useLocalRuntime + ChatModelAdapter、
  useExternalStoreRuntime 接 Redux/Zustand、useRemoteThreadListRuntime、useAssistantTransportRuntime）、
  挂载 AssistantRuntimeProvider，或读写 thread / message / composer / attachment 状态；覆盖
  useAui / useAuiState / useAuiEvent 与 v0.15 属性访问器（aui.thread、aui.message、aui.composer），
  s.optional.<scope>、capabilities、adapters（attachment / speech / dictation / suggestion / feedback /
  history）、realtime voice 与核心类型。用于排查 provider 内 "Cannot read property of undefined"、
  状态不更新等问题。多线程列表 UI 与会话切换另见 thread-list 相关约定。
---

# assistant-ui Runtime

Runtime 是 assistant-ui 的状态与命令中枢。**始终以 [assistant-ui.com/llms.txt](https://www.assistant-ui.com/llms.txt) 为最新 API 来源。**

## Runtime 层级

```
AssistantRuntime
├── ThreadListRuntime          # 多会话
│   └── ThreadListItemRuntime
└── ThreadRuntime              # 当前会话
    ├── ComposerRuntime        # 输入
    └── MessageRuntime[]       # 每条消息
        └── MessagePartRuntime[]
```

## 选型

| Runtime | 何时用 | 谁持有消息 |
|---------|--------|-----------|
| `useLocalRuntime` | 只需提供 `ChatModelAdapter.run`，要编辑/重载/分支/取消开箱即用 | assistant-ui |
| `useExternalStoreRuntime` | 消息已在 Redux/Zustand/自有 store，外部为唯一真相源 | 你的 store |
| `useRemoteThreadListRuntime` | 需要远端持久化的多会话列表 | 远端 + 本地缓存 |
| `useAssistantTransportRuntime` | 后端流式推送“完整状态”而非消息增量 | 你的状态对象 |

> 外部系统是消息真相源 → `ExternalStoreRuntime`；只想接模型、由库管消息 → `LocalRuntime`。

## 状态访问（现代 API）

```tsx
import { useAui, useAuiState, useAuiEvent } from "@assistant-ui/react";

const api = useAui();
const messages = useAuiState((s) => s.thread.messages);
const isRunning = useAuiState((s) => s.thread.isRunning);
```

- 用**选择器**订阅，避免整树重渲染：`s.thread.messages.length` 优于 `s.thread.messages`，更优于 `s`。
- 拆分组件，让 `isRunning` 与 `messages` 各自订阅、独立重渲染。

## Scope 访问器是属性（0.15+）

```tsx
aui.thread.getState();              // 属性，不是调用
aui.threads.switchToNewThread();
aui.thread.composer().send();       // composer() 是 thread scope 的方法
aui.thread.message({ index: 0 });   // message() 接选择器对象，不是裸 index
```

- 选到未挂载的 scope 不再抛错；先用 `aui.message.source != null` 判断可用性。
- `source`、`query`、`name` 为保留属性，永远不会解析成 scope 方法。

## 常用操作

```tsx
thread.append({ role: "user", content: [{ type: "text", text: "Hello" }] });
thread.startRun({ parentId: null });
thread.cancelRun();

const message = aui.thread.message({ index: 0 });
message.reload();
message.switchToBranch({ position: "next" });
const edit = message.composer();          // 编辑走消息自己的 edit composer
edit.beginEdit(); edit.setText("Updated"); edit.send();
```

## 事件

绝大多数事件已废弃，**优先从状态派生**（首帧与回放都正确；事件回调不会）。

```tsx
useAuiState((s) => s.thread.isRunning);        // 代替 thread.runStart/runEnd
useAuiState((s) => s.thread.composer.text);    // 代替 composer.send
```

仍非废弃：`thread.modelContextUpdate`（模型上下文不在 state 里）、`composer.attachmentAddError`。

## 可选 scope

组件可能渲染在 provider 之外时，用 `s.optional.<scope>` 读，得到 `undefined` 而不是抛错：

```tsx
const partType = useAuiState((s) => s.optional.part?.type);
```

命令式等价写法：`aui.<scope>.source != null`。

## Capabilities

```tsx
const caps = useAuiState((s) => s.thread.capabilities);
```

包含 `switchToBranch`、`switchBranchDuringRun`、`edit`、`reload`、`delete`、`cancel`、`unstable_copy`、`speech`、`dictation`、`voice`、`attachments`、`feedback`、`queue`。注意是 `unstable_copy`/`speech`，不是 `copy`/`speak`。runtime 依据你提供的回调/adapter 推导能力，而非显式开关。

## Adapters

在 runtime 的 `adapters` map 注册，跨 runtime 工厂通用：

| key | 作用 |
|-----|------|
| `attachments` | 接受文件：`add`→PendingAttachment、`send`→CompleteAttachment |
| `speech` | TTS，`WebSpeechSynthesisAdapter` 或自定义 |
| `dictation` | STT，`WebSpeechDictationAdapter` |
| `suggestion` / `feedback` / `history` | 建议、反馈、历史持久化 |
| `voice` | 实时语音，注册后 `capabilities.voice` 自动为 true |

附件错误通过 `composer.attachmentAddError` 事件上报（`no-adapter` / `not-accepted` / `adapter-error`），不要抛进渲染树。

## 核心类型

- `ThreadMessage`：`role: "user" | "assistant" | "system"` + `content: MessagePart[]` + `createdAt`。
- `MessageStatus` 是**对象判别式**：`{ type: "running" } | { type: "requires-action", reason } | { type: "complete", reason } | { type: "incomplete", reason, error? }`。
- `MessagePart`：`text | image | tool-call | reasoning | source | file`；tool-call 含 `toolCallId`、`toolName`、`args`、`argsText`、`result?`。
- `ChatModelRunResult`：`{ content, status?, metadata? }`，流式时逐个 yield 部分 content。

## 常见坑

| 现象 | 处理 |
|------|------|
| "Cannot read property of undefined" | hook 必须在 `AssistantRuntimeProvider` 内；未挂载 scope 用 `s.optional.<scope>` 或 `aui.<scope>.source != null` |
| 旧 hook 导入失败 | `useAssistantRuntime`/`useThread`/`useMessage`/`useComposer` 等在 0.15 **已移除**，改用 `useAui`/`useAuiState` |
| 状态不更新 | 用选择器订阅，别订阅整个 state |
| messages 为空 | 确认 runtime 已配置、API 响应格式正确 |

## 相关技能

- `streaming`：wire protocol 与 `ChatModelRunResult` 分片。
- `observability`：服务端 route 的 trace 接入。
